package notifications

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"time"

	"ollitex/go/mongoh"
	"ollitex/go/pbhttp"
)

// Config bundles the knobs read from the environment, mirroring the Node service.
type Config struct {
	// Host is the listen host (Node: LISTEN_ADDRESS || '127.0.0.1').
	Host string
	// Port is the listen port (Node: fixed 3042 — no env override).
	Port int
	// MongoURI is the connection string (see mongoh.DefaultURI / Node config).
	MongoURI string
	// DB is the logical database name (Node: mongoClient.db() → 'sharelatex').
	DB string
	// Collection is the notifications collection name (fixed 'notifications').
	Collection string
}

// WithDefaults applies the Node 1:1 env defaults.
func (c *Config) WithDefaults() {
	if c.Host == "" {
		c.Host = envOr("LISTEN_ADDRESS", "127.0.0.1")
	}
	if c.Port == 0 {
		c.Port = 3042 // Node has no env override for the notifications port.
	}
	if c.MongoURI == "" {
		c.MongoURI = envOrChain([]string{"MONGO_CONNECTION_STRING", "OVERLEAF_MONGO_URL"}, "")
		if c.MongoURI == "" {
			c.MongoURI = "mongodb://" + envOr("MONGO_HOST", "127.0.0.1") + "/sharelatex"
		}
	}
	if c.DB == "" {
		c.DB = mongoh.DBFromURI(c.MongoURI, "sharelatex")
	}
	if c.Collection == "" {
		c.Collection = "notifications"
	}
}

func envOrChain(keys []string, def string) string {
	for _, k := range keys {
		if v := os.Getenv(k); v != "" {
			return v
		}
	}
	return def
}

func envOr(k, def string) string {
	if v := getenv(k); v != "" {
		return v
	}
	return def
}

// getenv is a seam so unit tests can override env reads without the process env.
var getenv = getRealEnv

// Server is the notifications HTTP service (1:1 with the Node controller).
type Server struct {
	store      Store
	host       string
	port       int
	bodyLimit  int64
	httpClient *http.Client
	baseURL    string // self-URL used by health_check (Node: http://127.0.0.1:port)
}

// NewServer builds a notifications Server over the given Store.
func NewServer(store Store, cfg Config) *Server {
	cfg.WithDefaults()
	return &Server{
		store:      store,
		host:       cfg.Host,
		port:       cfg.Port,
		bodyLimit:  100 * 1024, // express.json() default '100kb'
		httpClient: &http.Client{Timeout: 5 * time.Second},
		baseURL:    "http://127.0.0.1:" + itoa(cfg.Port),
	}
}

// Router returns the service's http.Handler with the Node 1:1 route table.
//
// Route table (order mirrors services/notifications/app.ts):
//
//	POST   /user/:user_id
//	GET    /user/:user_id
//	DELETE /user/:user_id/notification/:notification_id
//	DELETE /user/:user_id
//	DELETE /key/:key
//	GET    /key/:key/count
//	DELETE /key/:key/bulk
//	GET    /status
//	GET    /health_check
//
// Unmatched requests reproduce Express exactly: GET → 404 "Not Found"
// (app.get('*') → res.sendStatus(404)); any other method → 404
// "Cannot <METHOD> <path>" (Express' built-in final handler).
func (s *Server) Router() http.Handler {
	// Node's notifications app.ts has a global handleApiError → res.sendStatus(500),
	// so an express.json() body-overflow (default 100kb) surfaces as 500
	// "Internal Server Error" (not the 413 the other four services return).
	return pbhttp.LimitBodyWith(http.HandlerFunc(s.dispatch), s.bodyLimit, http.StatusInternalServerError, "Internal Server Error")
}

// ---- routing (Express-equivalent (method+path) matching) -------------------

// handlerFunc is a route handler bound to the parsed path params.
type handlerFunc func(w http.ResponseWriter, r *http.Request, params map[string]string)

type route struct {
	method  string
	segs    []string // leading ':' = path param
	handler handlerFunc
}

func (s *Server) routes() []route {
	return []route{
		{"POST", []string{"user", ":user_id"}, s.addNotification},
		{"GET", []string{"user", ":user_id"}, s.getUserNotifications},
		{"DELETE", []string{"user", ":user_id", "notification", ":notification_id"}, s.removeNotificationId},
		{"DELETE", []string{"user", ":user_id"}, s.removeNotificationKey},
		{"DELETE", []string{"key", ":key"}, s.removeNotificationByKeyOnly},
		{"GET", []string{"key", ":key", "count"}, s.countNotificationsByKeyOnly},
		{"DELETE", []string{"key", ":key", "bulk"}, s.deleteUnreadNotificationsByKeyOnlyBulk},
		{"GET", []string{"status"}, s.status},
		{"GET", []string{"health_check"}, s.healthCheck},
	}
}

// dispatch mirrors Express: the first route whose method AND path match wins;
// otherwise the method-specific 404 body is produced.
func (s *Server) dispatch(w http.ResponseWriter, r *http.Request) {
	segz := pathSegments(r.URL.Path)
	for _, rt := range s.routes() {
		if rt.method != r.Method {
			continue
		}
		params, ok := matchPath(rt.segs, segz)
		if ok {
			rt.handler(w, r, params)
			return
		}
	}
	if r.Method == http.MethodGet {
		// Express: app.get('*') → res.sendStatus(404) → 404 "Not Found"
		sendStatus(w, http.StatusNotFound)
		return
	}
	// Express built-in final handler: 404 "Cannot <METHOD> <path>" (no trailing newline)
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusNotFound)
	_, _ = w.Write([]byte("Cannot " + r.Method + " " + r.URL.Path))
}

func pathSegments(p string) []string {
	if len(p) > 1 && strings.HasSuffix(p, "/") {
		p = p[:len(p)-1] // Express non-strict: trailing slash is optional
	}
	if p == "" || p == "/" {
		return []string{""}
	}
	return strings.Split(strings.TrimPrefix(p, "/"), "/")
}

func matchPath(route, req []string) (map[string]string, bool) {
	if len(route) != len(req) {
		return nil, false
	}
	params := map[string]string{}
	for i, rs := range route {
		if strings.HasPrefix(rs, ":") {
			params[rs[1:]] = req[i]
		} else if rs != req[i] {
			return nil, false
		}
	}
	return params, true
}

// ---- response helpers (Express-equivalent) --------------------------------

// sendStatus mirrors express res.sendStatus(code): body = http.StatusText(code).
func sendStatus(w http.ResponseWriter, code int) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(code)
	_, _ = w.Write([]byte(http.StatusText(code)))
}

// jsonValidationError mirrors validation-tools handleValidationError:
// res.status(code).json({ error: <zod message>, statusCode: code }).
//
// Params (URL path) failures → 404; body/request failures → 400. The `error`
// string shape matches the Zod/zod-validation-error style of the Node output.
func jsonValidationError(w http.ResponseWriter, code int, field, message string) {
	pbhttp.WriteJSON(w, code, map[string]any{
		"error":      field + ": " + message,
		"statusCode": code,
	})
}

func writeJSONStatus(w http.ResponseWriter, code int, v any) {
	pbhttp.WriteJSON(w, code, v)
}

func readJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	if r.Body == nil {
		return false
	}
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(dst); err != nil {
		jsonValidationError(w, http.StatusBadRequest, "body", "invalid JSON: "+err.Error())
		return false
	}
	return true
}
