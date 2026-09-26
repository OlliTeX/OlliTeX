package realtime

import (
	"context"
	"crypto/rand"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

//go:embed static/socket.io.js
var socketioClientFS embed.FS

// Handshake timeouts advertised in the handshake body (Node manager
// defaults, captured live: "S:60:60:websocket,xhr-polling").
const (
	heartbeatTimeoutSec = 60
	closeTimeoutSec     = 60
	serverHeartbeat     = 25 * time.Second // server heartbeat interval
	sessionStale        = 60 * time.Second // close if silent this long
	pollDuration        = 20 * time.Second // 0.9 'polling duration'
)

// Server — the socket.io 0.9 transport layer + the overleaf ops HTTP API on
// one port (Node real-time app.js: express + socketIO.listen on :3026).
type Server struct {
	Bus *Bus
	Log *slog.Logger

	wsUpgrader websocket.Upgrader

	mu         sync.Mutex
	handshakes map[string]*handshake // sid → session
	active     map[string]*wsTransport
	polls      map[string]*pollSession
}

type handshake struct {
	sid       string
	query     map[string]string
	remoteIP  string
	userAgent string
	joined    bool
}

// pollSession — the outbound queue shared by the session's transports:
// websocket's read loop and the xhr-polling GET loop both drain this; the
// bus writes into it. One queue per session keeps frame order intact.
type pollSession struct {
	mu     sync.Mutex
	queue  []string
	wake   chan struct{} // closed + replaced to signal
	closed bool
}

func newPollSession() *pollSession {
	return &pollSession{wake: make(chan struct{})}
}

func (p *pollSession) push(pkt string) {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return
	}
	p.queue = append(p.queue, pkt)
	w := p.wake
	p.wake = make(chan struct{})
	close(w)
	p.mu.Unlock()
}

func (p *pollSession) popOne(timeout time.Duration) (string, bool) {
	p.mu.Lock()
	var wake <-chan struct{}
	if len(p.queue) == 0 {
		wake = p.wake
	}
	p.mu.Unlock()
	if wake == nil {
		p.mu.Lock()
		defer p.mu.Unlock()
		if len(p.queue) == 0 { // re-check
			return "", false
		}
		pkt := p.queue[0]
		p.queue = p.queue[1:]
		return pkt, true
	}
	select {
	case <-wake:
	case <-time.After(timeout):
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.queue) == 0 {
		return "", false
	}
	pkt := p.queue[0]
	p.queue = p.queue[1:]
	return pkt, true
}

func (p *pollSession) closeQueue() {
	p.mu.Lock()
	p.closed = true
	w := p.wake
	p.wake = make(chan struct{})
	p.queue = nil
	p.closed = false // keep usable if a transport re-binds (shouldn't happen)
	p.mu.Unlock()
	close(w)
}

// NewServer — wire a Bus into an HTTP server.
func NewServer(b *Bus) *Server {
	log := b.Log
	if log == nil {
		log = slog.Default()
	}
	return &Server{
		Bus:        b,
		Log:        log,
		wsUpgrader: websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }},
		handshakes: map[string]*handshake{},
		active:     map[string]*wsTransport{},
		polls:      map[string]*pollSession{},
	}
}

// ---------------------------------------------------------------------------
// HTTP surface
// ---------------------------------------------------------------------------

// Routes — install all handlers (Node app.js + Router + HttpController +
// HttpApiController).
func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/socket.io/socket.io.js", s.serveClientJS)
	mux.HandleFunc("/socket.io/", s.socketio)
	mux.HandleFunc("/socket.io", s.socketioRoot)

	mux.HandleFunc("/", s.rootHealth)
	mux.HandleFunc("/status", s.status)
	mux.HandleFunc("/health_check/redis", s.healthRedis)
	mux.HandleFunc("/clients/", s.clientByID)
	mux.HandleFunc("/clients", s.listClients)
	mux.HandleFunc("/project/", s.projectAPI)
	mux.HandleFunc("/drain", s.drain)
	mux.HandleFunc("/client/", s.disconnectClient)
	mux.HandleFunc("/debug/events", s.debugEvents)
	return mux
}

func (s *Server) serveClientJS(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/socket.io/socket.io.js" {
		http.NotFound(w, r)
		return
	}
	b, err := socketioClientFS.ReadFile("static/socket.io.js")
	if err != nil {
		http.Error(w, "500: client bundle missing", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/javascript; charset=UTF-8")
	w.Header().Set("Cache-Control", "public, max-age=31536000")
	s.cors(w, r)
	w.Write(b)
}

// rootHealth — Node app.js: 200 (or 503 while draining) for LB checks.
func (s *Server) rootHealth(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	if s.Bus.draining() {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (s *Server) status(w http.ResponseWriter, r *http.Request) {
	if s.Bus.draining() {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	w.Write([]byte("real-time is alive"))
}

func (s *Server) healthRedis(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if _, err := s.Bus.Redis.Get(ctx, "__realtime_health__"); err != nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (s *Server) listClients(w http.ResponseWriter, r *http.Request) {
	s.writeJSON(w, http.StatusOK, s.Bus.ListClients())
}

func (s *Server) clientByID(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/clients/")
	v, ok := s.Bus.GetClient(id)
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	s.writeJSON(w, http.StatusOK, v)
}

// projectAPI — /project/:pid/count-connected-clients (GET) and
// /project/:pid/message/:name (POST, generic room relay).
func (s *Server) projectAPI(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/project/")
	parts := strings.SplitN(path, "/", 3)
	if len(parts) < 1 || parts[0] == "" {
		http.NotFound(w, r)
		return
	}
	pid := parts[0]
	if len(parts) == 2 && parts[1] == "count-connected-clients" {
		n, err := s.Bus.CountConnected(pid)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		s.writeJSON(w, http.StatusOK, map[string]any{"nConnectedClients": n})
		return
	}
	if len(parts) >= 3 && parts[1] == "message" && r.Method == "POST" {
		name := parts[2]
		if name == "" {
			http.NotFound(w, r)
			return
		}
		body, _ := io.ReadAll(io.LimitReader(r.Body, 5<<20))
		if _, err := s.Bus.SendRoomMessage(pid, name, json.RawMessage(body)); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		return
	}
	http.NotFound(w, r)
}

// drain — Node: POST /drain?rate=N → DrainManager.startDrain; 204.
func (s *Server) drain(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	rate := 4.0
	if v := r.URL.Query().Get("rate"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			rate = f
		}
	}
	s.Log.Info("setting client drain rate", "rate", rate)
	s.Bus.StartDrain(rate)
	w.WriteHeader(http.StatusNoContent)
}

// disconnectClient — Node: POST /client/:id/disconnect → 404 or 204.
func (s *Server) disconnectClient(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/client/")
	parts := strings.SplitN(rest, "/", 2)
	if len(parts) != 2 || parts[1] != "disconnect" || r.Method != "POST" {
		http.NotFound(w, r)
		return
	}
	if !s.Bus.isConnected(parts[0]) {
		s.Log.Debug("api: client already disconnected", "clientId", parts[0])
		w.WriteHeader(http.StatusNotFound)
		return
	}
	s.Log.Info("api: requesting client disconnect", "clientId", parts[0])
	s.forceDisconnect(parts[0])
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) debugEvents(w http.ResponseWriter, r *http.Request) {
	n := r.URL.Query().Get("count")
	s.Log.Info("starting debug mode", "count", n)
	fmt.Fprintf(w, "debug mode will log next %s events\n", n)
}

// ---------------------------------------------------------------------------
// socket.io resource paths (0.9 manager regexp, overleaf resource "/socket.io"):
//
//	/socket.io/1                    handshake
//	/socket.io/1/websocket          404 (no sid)
//	/socket.io/1/websocket/SID      websocket transport
//	/socket.io/1/xhr-polling/SID    polling GET/POST
// ---------------------------------------------------------------------------

func (s *Server) socketioRoot(w http.ResponseWriter, r *http.Request) { http.NotFound(w, r) }

func (s *Server) socketio(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/socket.io")
	path = strings.Trim(path, "/")
	if path == "" {
		http.NotFound(w, r)
		return
	}
	// strip protocol segment + rest
	parts := strings.SplitN(path, "/", 3)
	if len(parts) == 0 || parts[0] != "1" {
		http.Error(w, "404 not found", http.StatusNotFound)
		return
	}
	switch {
	case len(parts) == 1:
		s.handshake(w, r)
	case len(parts) >= 2 && parts[1] == "websocket" && len(parts) == 3:
		s.wsOpen(w, r, parts[2])
	case len(parts) >= 2 && parts[1] == "xhr-polling" && len(parts) == 3:
		s.polling(w, r, parts[2])
	default:
		http.Error(w, "404 not found", http.StatusNotFound)
	}
}

// handshake — GET /socket.io/1 → body "SID:60:60:websocket,xhr-polling".
func (s *Server) handshake(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	sid := newSessionID()
	q := map[string]string{}
	for k, v := range r.URL.Query() {
		if len(v) > 0 {
			q[k] = v[0]
		}
	}
	hs := &handshake{
		sid:       sid,
		query:     q,
		remoteIP:  remoteIP(r),
		userAgent: r.UserAgent(),
	}
	s.mu.Lock()
	s.handshakes[sid] = hs
	ps := newPollSession()
	s.polls[sid] = ps
	s.mu.Unlock()

	w.Header().Set("Content-Type", "text/plain; charset=UTF-8")
	s.cors(w, r)
	fmt.Fprintf(w, "%s:%d:%d:websocket,xhr-polling", sid, heartbeatTimeoutSec, closeTimeoutSec)
}

// wsOpen — websocket transport open.
func (s *Server) wsOpen(w http.ResponseWriter, r *http.Request, sid string) {
	hs := s.session(sid)
	if hs == nil {
		http.Error(w, "401 not authorized", http.StatusUnauthorized)
		return
	}
	if r.URL.Query().Get("disconnect") == "1" {
		s.forceDisconnect(sid)
		w.WriteHeader(http.StatusOK)
		return
	}
	conn, err := s.wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		s.Log.Debug("ws upgrade failed", "sid", sid, "err", err)
		return
	}
	conn.SetReadLimit(4 << 20)
	ps := s.pollSessionFor(sid)
	t := &wsTransport{sid: sid, server: s, conn: conn, ps: ps}

	s.mu.Lock()
	if old := s.active[sid]; old != nil {
		old.retire()
	}
	s.active[sid] = t
	first := !hs.joined
	hs.joined = true
	s.mu.Unlock()

	// 1:1 frame order (observed live): connect echo FIRST, then the
	// 'connection' handler (join flow) runs and answers asynchronously.
	if first {
		ps.push(EncodeConnect())
		go s.joinFlow(hs, r, t)
	}

	go t.readLoop()
	go t.writeLoop()
	go t.heartbeatLoop()
}

// joinFlow — Router 'connection' handler (async join; keeps the socket open).
func (s *Server) joinFlow(hs *handshake, r *http.Request, t *wsTransport) {
	deliver := func(pkt string) bool {
		t.ps.push(pkt)
		return true
	}
	s.Bus.Connect(r, hs.sid, "websocket", hs.remoteIP, hs.userAgent, hs.query, deliver)
}

// polling — xhr-polling transport (GET flush/long-poll, POST packet).
func (s *Server) polling(w http.ResponseWriter, r *http.Request, sid string) {
	if s.session(sid) == nil {
		http.Error(w, "401 not authorized", http.StatusUnauthorized)
		return
	}
	if r.URL.Query().Get("disconnect") == "1" {
		s.forceDisconnect(sid)
		w.WriteHeader(http.StatusOK)
		return
	}
	ps := s.pollSessionFor(sid)
	if r.Method == "POST" {
		body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		for _, line := range strings.Split(strings.TrimRight(string(body), "\r\n"), "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			s.handlePacket(sid, line)
		}
		w.WriteHeader(http.StatusOK)
		return
	}
	if r.Method != "GET" {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	pkt, ok := ps.popOne(pollDuration)
	w.Header().Set("Content-Type", "text/plain; charset=UTF-8")
	s.cors(w, r)
	if ok {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(pkt))
		return
	}
	// 0.9: poll timeout → noop packet (transport.js pollTimeout handler).
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(encodeParts(TypeNoop, "", false, "", "")))
}

// ---------------------------------------------------------------------------
// websocket transport
// ---------------------------------------------------------------------------

type wsTransport struct {
	sid    string
	server *Server
	conn   *websocket.Conn
	ps     *pollSession
	client *Client // bound after Bus.Connect

	retireOnce sync.Once
}

func (t *wsTransport) writeLoop() {
	for {
		pkt, ok := t.ps.popOne(100 * time.Millisecond)
		if !ok {
			continue
		}
		_ = t.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
		if err := t.conn.WriteMessage(websocket.TextMessage, []byte(pkt+"\n")); err != nil {
			t.server.Log.Debug("ws write failed, closing", "sid", t.sid, "err", err)
			t.terminate()
			return
		}
	}
}

func (t *wsTransport) readLoop() {
	for {
		_ = t.conn.SetReadDeadline(time.Now().Add(120 * time.Second))
		_, data, err := t.conn.ReadMessage()
		if err != nil {
			t.server.Log.Debug("ws read loop ended", "sid", t.sid, "err", err)
			t.terminate()
			return
		}
		t.server.handleFrame(t.sid, string(data))
	}
}

// retire — just close the socket (used when a newer transport takes over).
func (t *wsTransport) retire() {
	t.retireOnce.Do(func() { _ = t.conn.Close() })
}

// terminate — socket close + session teardown.
func (t *wsTransport) terminate() {
	t.retire()
	t.server.onTransportClosed(t.sid)
}

// heartbeatLoop — per-transport: server heartbeat (25s) + stale-session
// watchdog (60s idle). Stops when this transport is no longer the active
// one (replaced or closed).
func (t *wsTransport) heartbeatLoop() {
	tk := time.NewTicker(serverHeartbeat)
	defer tk.Stop()
	for range tk.C {
		if at, ok := t.server.activeTransport(t.sid); !ok || at != t {
			return
		}
		cl := t.server.Bus.clientBySid(t.sid)
		if cl == nil {
			return // rejected / never joined
		}
		if age := cl.lastSeenAge(); age > sessionStale {
			t.server.Log.Info("heartbeat timeout, closing session", "sid", t.sid, "age", age)
			t.terminate()
			return
		}
		t.ps.push(EncodeHeartbeat())
	}
}

// ---------------------------------------------------------------------------
// session lifecycle glue
// ---------------------------------------------------------------------------

func (s *Server) session(sid string) *handshake {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.handshakes[sid]
}

func (s *Server) pollSessionFor(sid string) *pollSession {
	s.mu.Lock()
	defer s.mu.Unlock()
	ps := s.polls[sid]
	if ps == nil {
		ps = newPollSession()
		s.polls[sid] = ps
	}
	return ps
}

// handleFrame — newline-split inbound packets (0.9 framing).
func (s *Server) handleFrame(sid, frame string) {
	for _, line := range strings.Split(strings.TrimRight(frame, "\r\n"), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		s.handlePacket(sid, line)
	}
}

func (s *Server) handlePacket(sid, line string) {
	p, ok := Decode(line)
	if !ok {
		s.Log.Debug("dropping malformed packet", "sid", sid, "packet", truncate(line, 120))
		return
	}
	cl := s.Bus.clientBySid(sid)
	if cl == nil {
		// pre-join packets: nothing to dispatch (the overleaf client emits
		// none before joinProjectResponse).
		return
	}
	s.Bus.Dispatch(cl, p)
}

// onTransportClosed — the active ws transport for a session went away:
// Node transport close → manager onClientDisconnect (one active transport
// per session in 0.9; the overleaf client opens a single ws).
func (s *Server) onTransportClosed(sid string) {
	s.mu.Lock()
	if cur, ok := s.active[sid]; ok && cur.sid == sid {
		delete(s.active, sid)
	}
	s.mu.Unlock()
	s.Bus.Close(sid)
}

// forceDisconnect — ?disconnect=1 / POST /client/:id/disconnect.
func (s *Server) forceDisconnect(sid string) {
	s.Log.Info("forced disconnect", "sid", sid)
	if t, ok := s.activeTransport(sid); ok {
		t.retire()
	}
	s.onTransportClosed(sid)
}

func (s *Server) activeTransport(sid string) (*wsTransport, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.active[sid]
	return t, ok
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func remoteIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		return strings.TrimSpace(parts[0])
	}
	host := r.RemoteAddr
	if i := strings.LastIndexByte(host, ':'); i > 0 {
		return host[:i]
	}
	return host
}

func (s *Server) cors(w http.ResponseWriter, r *http.Request) {
	if o := r.Header.Get("Origin"); o != "" {
		w.Header().Set("Access-Control-Allow-Origin", o)
		w.Header().Set("Access-Control-Allow-Credentials", "true")
	}
}

func (s *Server) writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	enc.Encode(v)
}

func newSessionID() string {
	b := make([]byte, 10)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
