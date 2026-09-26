package realtime

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

// protocolVersion — WebsocketController.PROTOCOL_VERSION (pin: 2). Bumping
// this forces a full page refresh in client that track it.
const protocolVersion = 2

// serverPingInterval — Router SERVER_PING_INTERVAL (pin: 15s), only for
// websocket clients whose handshake query carries esh + ssp.
// serverPingInterval — Router SERVER_PING_INTERVAL (pin: 15s); var so
// tests can compress it.
var serverPingInterval = 15 * time.Second

// clientRefreshDelay — WebsocketController.CLIENT_REFRESH_DELAY (pin: 1s):
// getConnectedUsers refreshes the live room and reads after this delay.
// clientRefreshDelay — WebsocketController.CLIENT_REFRESH_DELAY (pin: 1s).
var clientRefreshDelay = time.Second

var base64URL = base64.RawURLEncoding

// publicID — "P." + 15 random bytes in base64url (20 chars), the Node
// base64id.generateId() shape (observed live: "P.PpenaKTTzleNPGjQAAAN").
func publicID() string {
	b := make([]byte, 15)
	_, _ = rand.Read(b)
	return "P." + base64URL.EncodeToString(b)
}

// Client — one connected socket.io client (session), mirroring the Node
// client.ol_context + transport state the Router/WebsocketController read.
type Client struct {
	ID        string // socket.io session id (handshake token, not public)
	PublicID  string // "P."+20 chars, safe to broadcast
	Transport string // "websocket" | "xhr-polling"
	RemoteIP  string
	UserAgent string

	// handshake query (Node: client.handshake.query)
	Query map[string]string
	// esh + ssp present on the ws → serverPing enabled (Router rule).
	UseServerPing bool
	IsDebugging   bool // ?debugging=...

	// identity / join
	User         *sessionUser
	Presence     presenceUser
	JoinProject  string         // "" until joined
	Privilege    string         // joinProjectResult.privilegeLevel
	Restricted   bool           // isRestrictedUser
	ProjectModel map[string]any // joined project model (for doc grants + views)
	docAccess    map[string]bool

	PingID      atomic.Int64
	PongID      atomic.Int64
	connectedAt time.Time

	// transport hooks (wired by server.go)
	mu       sync.Mutex
	writeFn  func(pkt string) bool // write one encoded packet; false = gone
	closed   bool
	bus      *Bus
	lastSeen atomic.Int64
}

// Bus — the overleaf real-time application core (Node Router.js +
// WebsocketController.js + ConnectedUsersManager.js + DrainManager.js,
// single-instance: the redis pub/sub load balancer collapses into direct
// in-process delivery, but the wire protocol and redis keys stay 1:1).
type Bus struct {
	Sessions *SessionResolver
	Web      *WebAPI
	Flush    *FlushAPI
	Redis    RedisLike
	Host     string // HOSTNAME (debug.getHostname + debug response)
	Log      *slog.Logger

	mu        sync.RWMutex
	clients   map[string]*Client // session id → client
	byProject map[string]map[string]*Client
	pres      *presence
	drainRate atomic.Int64 // 0 = no drain; >0 clients/s (as int, rate<1 → 1)
	drainMu   sync.Mutex
	drainStop chan struct{}
}

// Options — construction dependencies (all injectable for tests).
type Options struct {
	Sessions *SessionResolver
	Web      *WebAPI
	Flush    *FlushAPI
	Redis    RedisLike
	Log      *slog.Logger
}

func New(opts Options) *Bus {
	host, _ := os.Hostname()
	if h := os.Getenv("HOSTNAME"); h != "" {
		host = h
	}
	log := opts.Log
	if log == nil {
		log = slog.Default()
	}
	return &Bus{
		Sessions:  opts.Sessions,
		Web:       opts.Web,
		Flush:     opts.Flush,
		Redis:     opts.Redis,
		Host:      host,
		Log:       log,
		clients:   map[string]*Client{},
		byProject: map[string]map[string]*Client{},
		pres:      &presence{r: opts.Redis},
	}
}

// ---------------------------------------------------------------------------
// Session lifecycle
// ---------------------------------------------------------------------------

// Connect — first transport open for a fresh handshake (Node: transport
// open → manager fires the default-namespace 'connection'): run the join
// flow. deliver is the transport write sink for THIS packet stream (the
// server passes the session queue push); it is captured before any
// asynchronous delivery so no packet can be lost. The connect echo ("1::")
// is pushed into the same queue by the transport layer before this runs
// (server.go), preserving the observed frame order.
func (b *Bus) Connect(r *http.Request, sid, transport, remoteIP, userAgent string, query map[string]string, deliver func(string) bool) *Client {
	cl := &Client{
		ID:          sid,
		PublicID:    publicID(),
		Transport:   transport,
		RemoteIP:    remoteIP,
		UserAgent:   userAgent,
		Query:       query,
		connectedAt: time.Now(),
		bus:         b,
	}
	cl.mu.Lock()
	cl.writeFn = deliver
	cl.mu.Unlock()
	cl.lastSeen.Store(time.Now().UnixMilli())
	cl.UseServerPing = transport == "websocket" && query["esh"] != "" && query["ssp"] != ""
	cl.IsDebugging = query["debugging"] != ""

	b.mu.Lock()
	if b.drainRate.Load() > 0 {
		b.mu.Unlock()
		cl.deliver(encodeRejected("retry"))
		cl.close()
		return cl
	}
	b.clients[sid] = cl
	b.mu.Unlock()
	if cl.IsDebugging {
		b.Log.Info("client connected (debug)", "publicId", cl.PublicID, "clientId", sid)
	} else {
		b.Log.Debug("client connected", "publicId", cl.PublicID, "clientId", sid, "transport", transport)
	}

	pid := query["projectId"]
	// Identity from the session — Node Router order: session validity is
	// checked before the projectId validation (SessionSockets hook fires
	// first) — an invalid session gets {message:"invalid session"} even if
	// the projectId is also bad.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	u, ok := b.Sessions.User(ctx, r)
	if !ok {
		// Node error path: "could not look up session by key" →
		// connectionRejected {message:"invalid session"} + disconnect.
		b.Log.Warn("invalid session", "sid", sid, "remoteIp", remoteIP)
		cl.deliver(encodeRejected("invalid session"))
		b.remove(sid)
		cl.close()
		return cl
	}
	anonToken, _ := b.Sessions.AnonToken(ctx, r, pid)
	cl.User = u
	cl.Presence = presenceUser{ID: u.ID, First: u.FirstName, Last: u.LastName, Email: u.Email}

	if pid == "" || !isObjectID(pid) {
		msg := "missing/bad ?projectId=... query flag on handshake"
		b.Log.Warn("connection rejected", "reason", msg, "transport", transport)
		cl.deliver(encodeRejected(msg))
		b.remove(sid)
		cl.close()
		return cl
	}

	b.Log.Info("user joining project", "userId", u.ID, "projectId", pid,
		"clientId", sid, "remoteIp", remoteIP, "userAgent", userAgent)

	res, err := b.Web.Join(ctx, pid, u.ID, anonToken)
	if err != nil {
		b.Log.Error("join failed", "err", err, "projectId", pid, "userId", u.ID)
		cl.deliver(encodeRejected("error"))
		b.remove(sid)
		cl.close()
		return cl
	}
	if res.rejectMessage != "" {
		b.Log.Info("join rejected", "message", res.rejectMessage, "projectId", pid, "userId", u.ID)
		cl.deliver(encodeRejected(res.rejectMessage))
		b.remove(sid)
		cl.close()
		return cl
	}
	cl.JoinProject = pid
	cl.Privilege = res.privilegeLevel
	cl.Restricted = res.isRestricted
	cl.ProjectModel = res.project
	cl.docAccess = grantedDocs(res.project)

	if err := b.pres.markConnected(ctx, pid, cl.PublicID, cl.Presence); err != nil {
		b.Log.Warn("markConnected failed", "err", err, "projectId", pid)
	}
	b.mu.Lock()
	m := b.byProject[pid]
	if m == nil {
		m = map[string]*Client{}
		b.byProject[pid] = m
	}
	m[sid] = cl
	b.mu.Unlock()

	b.Log.Debug("user joined project", "userId", u.ID, "projectId", pid,
		"privilegeLevel", res.privilegeLevel, "isTokenMember", res.isTokenMember, "isInvitedMember", res.isInvited)

	// joinProjectResponse — {publicId, project, permissionsLevel, protocolVersion}
	body, _ := json.Marshal(map[string]any{
		"publicId":         cl.PublicID,
		"project":          res.project,
		"permissionsLevel": res.privilegeLevel,
		"protocolVersion":  protocolVersion,
	})
	cl.deliver(encodeEventData(EvJoinProjectResponse, body))

	if cl.UseServerPing {
		go b.serverPingLoop(cl)
	}
	return cl
}

// Close — the transport went away (ws close, heartbeat timeout, ?disconnect=1,
// client 'disconnect' packet, or forced). Runs the Node leaveProject flow.
// Idempotent.
func (b *Bus) Close(sid string) {
	b.mu.Lock()
	cl := b.clients[sid]
	if cl == nil {
		b.mu.Unlock()
		return
	}
	delete(b.clients, sid)
	pid := cl.JoinProject
	if pid != "" {
		if m := b.byProject[pid]; m != nil {
			delete(m, sid)
			if len(m) == 0 {
				delete(b.byProject, pid)
			}
		}
	}
	b.mu.Unlock()
	if cl.closed {
		return
	}
	cl.closed = true

	if pid == "" {
		return
	}
	b.Log.Info("client leaving project", "projectId", pid, "userId", func() string {
		if cl.User != nil {
			return cl.User.ID
		}
		return ""
	}(), "clientId", sid)

	b.broadcast(pid, sid, encodeEvent1(EvClientTrackingDisc, cl.PublicID))

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := b.pres.disconnect(ctx, pid, cl.PublicID); err != nil {
		b.Log.Error("error marking client as disconnected", "err", err)
	}

	b.mu.RLock()
	remaining := len(b.byProject[pid])
	b.mu.RUnlock()
	if remaining == 0 && b.Flush != nil {
		// Node: setTimeout(500ms) → DocumentUpdaterManager.flushProjectToMongoAndDelete
		go func() {
			time.Sleep(500 * time.Millisecond)
			fctx, fcancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer fcancel()
			b.Flush.Flush(fctx, pid)
		}()
	}
}

// remove — de-register a client (join rejection / rejected-connect path).
func (b *Bus) remove(sid string) {
	b.mu.Lock()
	var pid string
	if cl := b.clients[sid]; cl != nil {
		pid = cl.JoinProject
	}
	delete(b.clients, sid)
	if pid != "" {
		if m := b.byProject[pid]; m != nil {
			delete(m, sid)
		}
	}
	b.mu.Unlock()
}

// ---------------------------------------------------------------------------
// Packet dispatch (Node Router's client.on(...) handlers, F2-scoped set)
// ---------------------------------------------------------------------------

func (b *Bus) Dispatch(cl *Client, p Packet) {
	cl.touch()
	switch p.Type {
	case TypeDisconnect:
		b.closeSid(cl.ID)
	case TypeHeartbeat:
		// (server-side: just alive-touch; the server also echoes per
		// transport below)
	case TypeEvent:
		b.handleEvent(cl, p)
	case TypeAck:
		// The overleaf client never acks a server emit (server events are
		// sent without callbacks); drop like Node (unknown ack id).
	default:
		// connect/json/message/noop — nothing the overleaf client emits.
	}
}

func (b *Bus) handleEvent(cl *Client, p Packet) {
	switch p.Name {
	case EvClientTrackingGetUsers:
		b.handleGetConnectedUsers(cl, p)
	case EvClientTrackingUpdate:
		b.handleUpdatePosition(cl, p)
	case EvClientPong:
		b.handleClientPong(cl, p)
	case "debug":
		b.handleDebug(cl, p)
	case EvDebugGetHostname:
		b.handleDebugGetHostname(cl, p)
	case EvReconnectGracefully, EvConnectionRejected, EvJoinProjectResponse,
		EvClientTrackingUpdated, EvClientTrackingDisc, EvClientTrackingRefresh,
		EvServerPing:
		// server-originated names; a client echo is dropped (Node: no listener)
	case "joinDoc", "leaveDoc", "applyOtUpdate":
		// RETIRED OT text-sync RPCs (F2 client hard-cut). Node kept the
		// handlers for in-flight clients; the Go bus drops them — no F2-era
		// client emits them, and the Yjs path is the collab service.
		b.Log.Debug("dropping retired OT event", "event", p.Name, "clientId", cl.ID)
	default:
		b.Log.Debug("dropping unknown bus event", "event", p.Name, "clientId", cl.ID)
	}
}

// --- clientTracking.getConnectedUsers -------------------------------------

func (b *Bus) handleGetConnectedUsers(cl *Client, p Packet) {
	if cl.JoinProject == "" {
		if p.Ack {
			cl.deliverEncodeAck(p.ID, json.RawMessage(`"NotJoinedError"`))
		}
		return
	}
	if cl.Restricted {
		// Node: restricted users get an empty list.
		if p.Ack {
			cl.deliverEncodeAck(p.ID, json.RawMessage(`[null,[]]`))
		}
		return
	}
	pid := cl.JoinProject

	// Node: WebsocketLoadBalancer.emitToRoom(pid, 'clientTracking.refresh')
	// → every instance refreshes its live clients; single instance: touch
	// our own. (Other instances in a multi-replica fleet would be told via
	// the redis pub/sub; D28a single-instance parity.)
	b.mu.RLock()
	room := b.byProject[pid]
	ids := make([]string, 0, len(room))
	for id := range room {
		ids = append(ids, id)
	}
	b.mu.RUnlock()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	for _, id := range ids {
		b.mu.RLock()
		c := b.clients[id]
		b.mu.RUnlock()
		if c != nil && c.JoinProject == pid {
			b.pres.refresh(ctx, pid, c.PublicID)
		}
	}
	go func() {
		time.Sleep(clientRefreshDelay)
		users, err := b.pres.getConnected(ctx, pid)
		if err != nil {
			b.Log.Error("problem getting clients in project", "err", err, "projectId", pid)
			if p.Ack {
				cl.deliverEncodeAck(p.ID, json.RawMessage(`[`+string(errorRaw(err))+`]`))
			}
			return
		}
		if p.Ack {
			cl.deliverEncodeAck(p.ID, json.RawMessage(`[null,`+string(mustJSON(users))+`]`))
		}
	}()
}

// --- clientTracking.updatePosition ----------------------------------------

func (b *Bus) handleUpdatePosition(cl *Client, p Packet) {
	if cl.JoinProject == "" {
		return // Node: NotJoinedError → _handleError → (client sent no cb: silently)
	}
	// cursorData schema: {row?, number; column?, number; doc_id?, string|null}
	var cur struct {
		Row    *float64 `json:"row"`
		Column *float64 `json:"column"`
		DocID  *string  `json:"doc_id"`
	}
	ok := len(p.Args) > 0 && json.Unmarshal(p.Args[0], &cur) == nil
	docRef := ""
	if ok && cur.DocID != nil {
		docRef = *cur.DocID
	}
	if !ok || docRef == "" {
		// Node: _assertClientCanAccessDoc(client, docId) without a granted
		// doc → "silently ignoring unauthorized updateClientPosition".
		if p.Ack {
			cl.deliverEncodeAck(p.ID, nil)
		}
		return
	}
	if !cl.docAccess[docRef] {
		b.Log.Debug("silently ignoring unauthorized updateClientPosition", "clientId", cl.ID, "doc_id", docRef)
		if p.Ack {
			cl.deliverEncodeAck(p.ID, nil)
		}
		return
	}
	pid := cl.JoinProject
	// Anonymous users: enriched broadcast only, not stored (Node parity).
	userName := ""
	if cl.User != nil && cl.User.ID != "" && cl.User.ID != "anonymous-user" {
		userName = cl.Presence.First + " " + cl.Presence.Last
		if userName == "  " {
			userName = cl.Presence.First
		}
		if userName == "" {
			userName = cl.Presence.Last
		}
	}
	payload := map[string]any{
		"id":     cl.PublicID,
		"doc_id": docRef,
		"name":   userName,
	}
	if ok && cur.Row != nil {
		payload["row"] = *cur.Row
	}
	if ok && cur.Column != nil {
		payload["column"] = *cur.Column
	}
	if cl.User != nil && cl.User.ID != "" {
		payload["user_id"] = cl.User.ID
	}
	if cl.User != nil && cl.User.Email != "" {
		payload["email"] = cl.User.Email
	}
	if cl.User != nil && cl.User.ID != "" && cl.User.ID != "anonymous-user" {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		if err := b.pres.updatePosition(ctx, pid, cl.PublicID, cl.Presence, cursorFields{DocID: docRef, Row: cur.Row, Column: cur.Column}); err != nil {
			b.Log.Warn("cursor update failed", "err", err)
		}
	}
	// WebsocketLoadBalancer.emitToRoom(pid, 'clientTracking.clientUpdated', cursorData)
	// → delivered to every client in the room (Node LB semantics: everyone;
	// the client self-filters by socket.publicId).
	body, _ := json.Marshal(payload)
	b.broadcast(pid, "", encodeEventData(EvClientTrackingUpdated, body))
	if p.Ack {
		cl.deliverEncodeAck(p.ID, nil)
	}
}

// --- serverPing / clientPong ------------------------------------------------

func (b *Bus) serverPingLoop(cl *Client) {
	t := time.NewTicker(serverPingInterval)
	defer t.Stop()
	for range t.C {
		if cl.closed {
			return
		}
		if cl.PongID.Load() != cl.PingID.Load() {
			b.Log.Warn("no client response to last ping",
				"pingId", cl.PingID.Load(), "pongId", cl.PongID.Load())
		}
		n := cl.PingID.Add(1)
		body, _ := json.Marshal([]any{n, time.Now().UnixMilli(), cl.Transport, cl.ID})
		cl.deliver(encodeEventData(EvServerPing, body))
	}
}
func (b *Bus) handleClientPong(cl *Client, p Packet) {
	if len(p.Args) == 0 {
		return
	}
	var got int64
	if err := json.Unmarshal(p.Args[0], &got); err != nil {
		b.Log.Warn("clientPong: bad receivedPingId", "clientId", cl.ID)
		return
	}
	cl.PongID.Store(got)
	if got != cl.PingID.Load() {
		b.Log.Warn("clientPong id mismatch", "receivedPingId", got, "pingId", cl.PingID.Load(), "clientId", cl.ID)
	}
}

// --- debug channels (Router: debug + debug.getHostname) --------------------

func (b *Bus) handleDebug(cl *Client, p Packet) {
	var data json.RawMessage
	if len(p.Args) > 0 {
		data = p.Args[0]
	}
	if data == nil {
		data = json.RawMessage(`null`)
	}
	resp := map[string]any{
		"serverTime": time.Now().UnixMilli(),
		"data":       json.RawMessage(data),
		"client": map[string]any{
			"publicId":    cl.PublicID,
			"remoteIp":    cl.RemoteIP,
			"userAgent":   cl.UserAgent,
			"connected":   !cl.closed,
			"connectedAt": cl.connectedAt.UnixMilli(),
		},
		"server": map[string]any{"hostname": b.Host},
	}
	if p.Ack {
		body, _ := json.Marshal(resp)
		cl.deliverEncodeAck(p.ID, json.RawMessage("["+string(body)+"]"))
	}
}

func (b *Bus) handleDebugGetHostname(cl *Client, p Packet) {
	if p.Ack {
		body, _ := json.Marshal(b.Host)
		cl.deliverEncodeAck(p.ID, json.RawMessage("["+string(body)+"]"))
	}
}

// ---------------------------------------------------------------------------
// Ops surface (Node HttpController / HttpApiController / DrainManager / app.js)
// ---------------------------------------------------------------------------

// connectedClientView — Node HttpController._getConnectedClientView.
type connectedClientView struct {
	ClientID      string   `json:"client_id"`
	ProjectID     string   `json:"project_id"`
	UserID        string   `json:"user_id"`
	FirstName     string   `json:"first_name"`
	LastName      string   `json:"last_name"`
	Email         string   `json:"email"`
	ConnectedTime string   `json:"connected_time"`
	Rooms         []string `json:"rooms"`
}

func (b *Bus) clientView(cl *Client) connectedClientView {
	v := connectedClientView{
		ClientID:      cl.ID,
		ProjectID:     cl.JoinProject,
		Email:         "",
		ConnectedTime: cl.connectedAt.UTC().Format(time.RFC3339),
		Rooms:         []string{},
	}
	if cl.JoinProject != "" {
		v.Rooms = append(v.Rooms, cl.JoinProject)
	}
	if cl.User != nil {
		v.UserID = cl.User.ID
		v.FirstName = cl.Presence.First
		v.LastName = cl.Presence.Last
		v.Email = cl.User.Email
	}
	return v
}

func (b *Bus) ListClients() []connectedClientView {
	b.mu.RLock()
	defer b.mu.RUnlock()
	out := []connectedClientView{}
	for _, cl := range b.clients {
		out = append(out, b.clientView(cl))
	}
	return out
}

func (b *Bus) GetClient(sid string) (*connectedClientView, bool) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	cl, ok := b.clients[sid]
	if !ok {
		return nil, false
	}
	v := b.clientView(cl)
	return &v, true
}

func (b *Bus) CountConnected(pid string) (int64, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return b.pres.count(ctx, pid)
}

// Reconnect — ask one client to reconnect gracefully (drain primitive).
func (b *Bus) Reconnect(cl *Client) {
	if cl.closed {
		return
	}
	b.Log.Debug("Asking client to reconnect gracefully", "clientId", cl.ID)
	cl.deliver(encodeEvent0(EvReconnectGracefully))
}

// StartDrain — Node DrainManager.startDrain (rate per second, 0 = noop).
func (b *Bus) StartDrain(rate float64) {
	if rate <= 0 {
		return
	}
	n := int64(rate)
	if n < 1 {
		n = 1
	}
	old := b.drainRate.Swap(n)
	if old == n && b.drainStop != nil {
		return
	}
	b.drainMu.Lock()
	if b.drainStop != nil {
		close(b.drainStop)
	}
	stop := make(chan struct{})
	b.drainStop = stop
	b.drainMu.Unlock()

	interval := time.Second
	if rate < 1 {
		interval = time.Duration(float64(time.Second) / rate)
		n = 1
	}
	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				if b.drainAll(int(n)) {
					return
				}
			}
		}
	}()
}

func (b *Bus) StopDrain() {
	if b.drainRate.Swap(0) == 0 {
		return
	}
	b.drainMu.Lock()
	if b.drainStop != nil {
		close(b.drainStop)
		b.drainStop = nil
	}
	b.drainMu.Unlock()
}

func (b *Bus) draining() bool { return b.drainRate.Load() > 0 }

// drainAll — reconnectN clients; true when every client has been asked.
func (b *Bus) drainAll(n int) bool {
	b.mu.RLock()
	all := make([]*Client, 0, len(b.clients))
	for _, cl := range b.clients {
		all = append(all, cl)
	}
	b.mu.RUnlock()
	asked := 0
	for _, cl := range all {
		b.Reconnect(cl)
		asked++
		if asked >= n {
			if asked < len(all) {
				return false
			}
			return true
		}
	}
	b.Log.Info("All clients have been told to reconnectGracefully")
	return true
}

// SendRoomMessage — Node HttpApiController.sendMessage → LB emitToRoom:
// POST /project/:pid/message/:name → event :name to the room, args = the
// body (array as-is, object as single arg).
func (b *Bus) SendRoomMessage(pid, name string, body json.RawMessage) (int, error) {
	b.mu.RLock()
	room := b.byProject[pid]
	targets := make([]*Client, 0, len(room))
	for _, cl := range room {
		targets = append(targets, cl)
	}
	b.mu.RUnlock()
	if len(targets) == 0 {
		return 0, nil
	}
	var args []json.RawMessage
	{
		trimmed := trimJSON(body)
		if len(trimmed) > 0 && trimmed[0] == '[' {
			if err := json.Unmarshal(trimmed, &args); err != nil {
				return 0, err
			}
		} else if len(trimmed) > 0 {
			args = []json.RawMessage{trimmed}
		}
	}
	if args == nil {
		args = []json.RawMessage{}
	}
	b.mu.RLock()
	argsCopy := make([]json.RawMessage, len(args))
	copy(argsCopy, args)
	b.mu.RUnlock()
	pkt, err := EncodeEventJSON(name, argsCopy)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, cl := range targets {
		if cl.deliver(pkt) {
			n++
		}
	}
	return n, nil
}

// closeSid — client-initiated disconnect packet / forced close.
func (b *Bus) closeSid(sid string) {
	if cl := b.clientBySid(sid); cl != nil {
		cl.close()
	}
	b.Close(sid)
}

func (b *Bus) clientBySid(sid string) *Client {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.clients[sid]
}

// isConnected — the ops API's 404-vs-204 decision for /client/:id/disconnect.
func (b *Bus) isConnected(sid string) bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	cl := b.clients[sid]
	return cl != nil && !cl.closed
}

// ---------------------------------------------------------------------------
// delivery helpers
// ---------------------------------------------------------------------------

func (c *Client) touch() { c.lastSeen.Store(time.Now().UnixMilli()) }

// lastSeenAge — for the server's heartbeat watchdog.
func (c *Client) lastSeenAge() time.Duration {
	return time.Since(time.UnixMilli(c.lastSeen.Load()))
}

// deliver — write one encoded packet; false if the transport is gone.
func (c *Client) deliver(pkt string) bool {
	c.mu.Lock()
	fn := c.writeFn
	closed := c.closed
	c.mu.Unlock()
	if closed {
		return false
	}
	if fn == nil {
		if c.bus != nil {
			c.bus.Log.Warn("deliver: no write sink (transport gone)", "sid", c.ID)
		}
		return false
	}
	ok := fn(pkt)
	if !ok && c.bus != nil {
		c.bus.Close(c.ID)
	}
	return ok
}

// deliverEncodeAck — ack with already-encoded JSON args (argsRaw is a JSON
// array, e.g. `[null,[...users]]`; nil/"null" → bare id, wire "6:::<id>").
func (c *Client) deliverEncodeAck(id string, argsRaw json.RawMessage) {
	if id == "" {
		return
	}
	pkt, err := EncodeAckRaw(id, argsRaw)
	if err != nil {
		return
	}
	c.deliver(pkt)
}

func (c *Client) close() {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.closed = true
	c.mu.Unlock()
}

func (b *Bus) broadcast(pid, excludeSID, pkt string) {
	b.mu.RLock()
	room := b.byProject[pid]
	targets := make([]*Client, 0, len(room))
	for sid, cl := range room {
		if sid == excludeSID {
			continue
		}
		targets = append(targets, cl)
	}
	b.mu.RUnlock()
	for _, cl := range targets {
		cl.deliver(pkt)
	}
}

// encodeRejected / event helpers (compact encoders for repeated shapes).
func encodeRejected(message string) string {
	pkt, _ := EncodeEvent(EvConnectionRejected, map[string]any{"message": message})
	return pkt
}

func encodeEvent0(name string) string {
	pkt, _ := EncodeEvent(name)
	return pkt
}

func encodeEvent1(name string, s string) string {
	pkt, _ := EncodeEvent(name, s)
	return pkt
}

func encodeEventData(name string, argsJSON []byte) string {
	body := `{"name":` + jsonString(name) + `,"args":` + string(argsJSON) + `}`
	return encodeParts(TypeEvent, "", false, "", body)
}

func errorRaw(err error) json.RawMessage {
	b, _ := json.Marshal(fmt.Sprintf("%s", err))
	return b
}

func mustJSON(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage(`null`)
	}
	return b
}

func trimJSON(b json.RawMessage) json.RawMessage {
	out := b
	for len(out) > 0 && (out[0] == ' ' || out[0] == '\t' || out[0] == '\n' || out[0] == '\r') {
		out = out[1:]
	}
	return out
}
