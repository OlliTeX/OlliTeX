// routes_wired.go — D5: wires the 33 ported HttpController handlers into the
// exact Router.js table (server.js middleware order: longerTimeout →
// express.json → metrics.http → routes → error dispatch (wire.Dispatch)).
package server

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	pherr "ollitex/go/services/project-history/internal/errors"
	"ollitex/go/services/project-history/internal/httpcontroller"
)

// Deps injects the controller bundle (nil → stubs, as before).
type Deps struct {
	C *httpcontroller.Controller
	// HTTPMetrics — vendor Metrics.http.monitor hook (method, status, durMs).
	HTTPMetrics func(method, path string, status int, durMs int64)
}

// NewWiredServer returns the route table with handlers wired to the
// controller when d.C != nil.
func (s *Server) NewWiredServer(d *Deps) http.Handler {
	if d == nil || d.C == nil {
		return s.Routes()
	}
	c := d.C
	mux := http.NewServeMux()

	write := func(w http.ResponseWriter, r *http.Request, fn func() (*httpcontroller.Result, error)) {
		res, err := fn()
		if err != nil {
			Dispatch(w, r, err, nil)
			return
		}
		code := res.Status
		if code == 0 {
			code = 200
		}
		if res.Headers != nil {
			for k, v := range res.Headers {
				w.Header().Set(k, toStr(v))
			}
		}
		if res.Body != nil {
			w.Header().Set("Content-Type", "application/octet-stream")
			w.WriteHeader(code)
			_, _ = w.Write(res.Body)
			return
		}
		if res.JSON != nil {
			if m, ok := res.JSON.(map[string]any); ok {
				jsonBody(w, code, m)
				return
			}
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.WriteHeader(code)
			_ = json.NewEncoder(w).Encode(res.JSON)
			return
		}
		sendStatus(w, code)
	}

	body := func(r *http.Request) map[string]any {
		defer r.Body.Close()
		dec := json.NewDecoder(r.Body)
		out := map[string]any{}
		_ = dec.Decode(&out)
		return out
	}

	qInt := func(r *http.Request, name string) *int {
		v := r.URL.Query().Get(name)
		if v == "" {
			return nil
		}
		n, err := strconv.Atoi(v)
		if err != nil {
			return nil
		}
		return &n
	}
	qInt64 := func(r *http.Request, name string) (int64, bool) {
		v := r.URL.Query().Get(name)
		if v == "" {
			return 0, false
		}
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return 0, false
		}
		return n, true
	}
	qBool := func(r *http.Request, name string) bool {
		return r.URL.Query().Get(name) == "true"
	}
	seg := func(r *http.Request, name string) string {
		return r.PathValue(name)
	}

	// --- Router.js exact order -------------------------------------------------
	mux.HandleFunc("POST /project", func(w http.ResponseWriter, r *http.Request) {
		b := body(r)
		var hid *string
		if h, ok := b["historyId"].(string); ok && h != "" {
			hid = &h
		}
		write(w, r, func() (*httpcontroller.Result, error) {
			return c.InitializeProject(hid)
		})
	})
	mux.HandleFunc("DELETE /project/{project_id}", func(w http.ResponseWriter, r *http.Request) {
		p := seg(r, "project_id")
		write(w, r, func() (*httpcontroller.Result, error) { return c.DeleteProject(p) })
	})
	mux.HandleFunc("GET /project/{project_id}/snapshot", func(w http.ResponseWriter, r *http.Request) {
		p := seg(r, "project_id")
		write(w, r, func() (*httpcontroller.Result, error) { return c.GetLatestSnapshot(p) })
	})
	mux.HandleFunc("GET /project/{project_id}/diff", func(w http.ResponseWriter, r *http.Request) {
		p := seg(r, "project_id")
		pathname := r.URL.Query().Get("pathname")
		from := qInt(r, "from")
		to := qInt(r, "to")
		write(w, r, func() (*httpcontroller.Result, error) {
			if from == nil || to == nil {
				return nil, errBadRequestInt("from/to are required ints")
			}
			return c.GetDiff(p, pathname, *from, *to)
		})
	})
	mux.HandleFunc("GET /project/{project_id}/filetree/diff", func(w http.ResponseWriter, r *http.Request) {
		p := seg(r, "project_id")
		from := qInt(r, "from")
		to := qInt(r, "to")
		write(w, r, func() (*httpcontroller.Result, error) {
			if from == nil || to == nil {
				return nil, errBadRequestInt("from/to are required ints")
			}
			return c.GetFileTreeDiff(p, *from, *to)
		})
	})
	mux.HandleFunc("GET /project/{project_id}/updates", func(w http.ResponseWriter, r *http.Request) {
		p := seg(r, "project_id")
		before := qInt(r, "before")
		minCount := qInt(r, "min_count")
		write(w, r, func() (*httpcontroller.Result, error) { return c.GetUpdates(p, before, minCount) })
	})
	mux.HandleFunc("GET /project/{project_id}/changes-in-chunk", func(w http.ResponseWriter, r *http.Request) {
		p := seg(r, "project_id")
		sSince := r.URL.Query().Get("since")
		n, err := strconv.Atoi(sSince)
		if err != nil {
			sendStatus(w, 400)
			return
		}
		write(w, r, func() (*httpcontroller.Result, error) { return c.GetChangesInChunkSince(p, n) })
	})
	mux.HandleFunc("GET /project/{project_id}/version", func(w http.ResponseWriter, r *http.Request) {
		p := seg(r, "project_id")
		write(w, r, func() (*httpcontroller.Result, error) { return c.LatestVersion(p) })
	})
	mux.HandleFunc("POST /project/{project_id}/flush", func(w http.ResponseWriter, r *http.Request) {
		p := seg(r, "project_id")
		req := &httpcontroller.FlushRequest{
			ProjectID:  p,
			Debug:      qBool(r, "debug"),
			Bisect:     qBool(r, "bisect"),
			Background: qBool(r, "background"),
		}
		write(w, r, func() (*httpcontroller.Result, error) { return c.FlushProject(req) })
	})
	mux.HandleFunc("GET /project/{project_id}/resync-pending", func(w http.ResponseWriter, r *http.Request) {
		p := seg(r, "project_id")
		write(w, r, func() (*httpcontroller.Result, error) { return c.GetResyncPending(p) })
	})
	mux.HandleFunc("GET /project/{project_id}/debug-info", func(w http.ResponseWriter, r *http.Request) {
		p := seg(r, "project_id")
		write(w, r, func() (*httpcontroller.Result, error) { return c.GetDebugInfo(p) })
	})
	mux.HandleFunc("POST /project/{project_id}/resync", func(w http.ResponseWriter, r *http.Request) {
		p := seg(r, "project_id")
		b := body(r)
		q := &httpcontroller.ResyncQuery{
			ProjectID:             p,
			Force:                 qBool(r, "force") || boolAny(b["force"]),
			RecoverCorruptedFiles: qBool(r, "recoverCorruptedFiles") || boolAny(b["recoverCorruptedFiles"]),
		}
		if o, ok := b["origin"].(map[string]any); ok {
			q.Origin = o
		}
		if m, ok := b["historyRangesMigration"].(string); ok {
			q.HistoryRangesMigration = m
		}
		write(w, r, func() (*httpcontroller.Result, error) { return c.ResyncProject(q) })
	})
	mux.HandleFunc("GET /project/{project_id}/dump", func(w http.ResponseWriter, r *http.Request) {
		p := seg(r, "project_id")
		count := qInt(r, "count")
		write(w, r, func() (*httpcontroller.Result, error) { return c.DumpProject(p, count) })
	})
	// labels
	mux.HandleFunc("GET /project/{project_id}/labels", func(w http.ResponseWriter, r *http.Request) {
		p := seg(r, "project_id")
		write(w, r, func() (*httpcontroller.Result, error) { return c.GetLabels(p) })
	})
	mux.HandleFunc("POST /project/{project_id}/labels", func(w http.ResponseWriter, r *http.Request) {
		p := seg(r, "project_id")
		b := body(r)
		q := &httpcontroller.CreateLabelRequest{ProjectID: p}
		// userId = params.user_id || body.user_id (vendor dual-read)
		if uid := seg(r, "user_id"); uid != "" {
			q.UserID = uid
		} else if uid, ok := b["user_id"].(string); ok {
			q.UserID = uid
		}
		q.Version = intInt(b["version"])
		q.Comment, _ = b["comment"].(string)
		q.CreatedAt = b["created_at"]
		q.ValidateExists = !boolAnyFalse(b["validate_exists"]) // default true
		write(w, r, func() (*httpcontroller.Result, error) { return c.CreateLabel(q) })
	})
	mux.HandleFunc("DELETE /project/{project_id}/user/{user_id}/labels/{label_id}", func(w http.ResponseWriter, r *http.Request) {
		write(w, r, func() (*httpcontroller.Result, error) {
			return c.DeleteLabelForUser(seg(r, "project_id"), seg(r, "user_id"), seg(r, "label_id"))
		})
	})
	mux.HandleFunc("DELETE /project/{project_id}/labels/{label_id}", func(w http.ResponseWriter, r *http.Request) {
		write(w, r, func() (*httpcontroller.Result, error) {
			return c.DeleteLabel(seg(r, "project_id"), seg(r, "label_id"))
		})
	})
	mux.HandleFunc("POST /user/{from_user}/labels/transfer/{to_user}", func(w http.ResponseWriter, r *http.Request) {
		write(w, r, func() (*httpcontroller.Result, error) {
			return c.TransferLabels(seg(r, "from_user"), seg(r, "to_user"))
		})
	})
	// version pair (distinct segment counts)
	mux.HandleFunc("GET /project/{project_id}/version/{version}/{pathname...}", func(w http.ResponseWriter, r *http.Request) {
		v, _ := strconv.Atoi(seg(r, "version"))
		write(w, r, func() (*httpcontroller.Result, error) {
			return c.GetFileSnapshot(seg(r, "project_id"), v, seg(r, "pathname"))
		})
	})
	mux.HandleFunc("GET /project/{project_id}/version/{version}", func(w http.ResponseWriter, r *http.Request) {
		v, _ := strconv.Atoi(seg(r, "version"))
		write(w, r, func() (*httpcontroller.Result, error) {
			return c.GetProjectSnapshot(seg(r, "project_id"), v)
		})
	})
	mux.HandleFunc("GET /project/{project_id}/ranges/version/{version}/{pathname...}", func(w http.ResponseWriter, r *http.Request) {
		v, _ := strconv.Atoi(seg(r, "version"))
		write(w, r, func() (*httpcontroller.Result, error) {
			return c.GetRangesSnapshot(seg(r, "project_id"), v, seg(r, "pathname"))
		})
	})
	mux.HandleFunc("GET /project/{project_id}/metadata/version/{version}/{pathname...}", func(w http.ResponseWriter, r *http.Request) {
		v, _ := strconv.Atoi(seg(r, "version"))
		write(w, r, func() (*httpcontroller.Result, error) {
			return c.GetFileMetadataSnapshot(seg(r, "project_id"), v, seg(r, "pathname"))
		})
	})
	mux.HandleFunc("GET /project/{project_id}/paths/version/{version}", func(w http.ResponseWriter, r *http.Request) {
		v, _ := strconv.Atoi(seg(r, "version"))
		write(w, r, func() (*httpcontroller.Result, error) {
			return c.GetPathsAtVersion(seg(r, "project_id"), v)
		})
	})
	mux.HandleFunc("POST /project/{project_id}/force", func(w http.ResponseWriter, r *http.Request) {
		p := seg(r, "project_id")
		clear := qBool(r, "clear")
		write(w, r, func() (*httpcontroller.Result, error) { return c.ForceDebugProject(p, clear) })
	})
	// router: GET /project/:history_id/blob/:hash
	mux.HandleFunc("GET /project/{history_id}/blob/{hash}", func(w http.ResponseWriter, r *http.Request) {
		write(w, r, func() (*httpcontroller.Result, error) {
			return c.GetProjectBlob(seg(r, "history_id"), seg(r, "hash"))
		})
	})
	mux.HandleFunc("POST /project/{project_id}/clone", func(w http.ResponseWriter, r *http.Request) {
		p := seg(r, "project_id")
		b := body(r)
		dst, _ := b["targetProjectId"].(string)
		write(w, r, func() (*httpcontroller.Result, error) {
			out := c.CloneProject(p, dst)
			return &httpcontroller.Result{Body: out.Body, Status: 200}, nil
		})
	})
	mux.HandleFunc("GET /status/failures", func(w http.ResponseWriter, r *http.Request) {
		write(w, r, func() (*httpcontroller.Result, error) { return c.GetFailures() })
	})
	mux.HandleFunc("GET /status/failures-full", func(w http.ResponseWriter, r *http.Request) {
		write(w, r, func() (*httpcontroller.Result, error) { return c.GetFailuresFull() })
	})
	mux.HandleFunc("GET /status/queue", func(w http.ResponseWriter, r *http.Request) {
		write(w, r, func() (*httpcontroller.Result, error) { return c.GetQueueCounts() })
	})
	mux.HandleFunc("POST /retry/failures", func(w http.ResponseWriter, r *http.Request) {
		q := &httpcontroller.RetryFailuresQuery{}
		q.FailureType = r.URL.Query().Get("failureType")
		if t, ok := qInt64(r, "timeout"); ok {
			q.Timeout = t
		} else {
			q.Timeout = 300
		}
		if l := qInt(r, "limit"); l != nil {
			q.Limit = *l
		} else {
			q.Limit = 100
		}
		q.CallbackURL = r.URL.Query().Get("callbackUrl")
		req := &httpcontroller.Req{Headers: headers(r)}
		write(w, r, func() (*httpcontroller.Result, error) {
			return c.RetryFailures(q, req)
		})
	})
	mux.HandleFunc("POST /flush/old", func(w http.ResponseWriter, r *http.Request) {
		q := &httpcontroller.FlushOldQuery{}
		if v, ok := qInt64(r, "maxAge"); ok {
			q.MaxAge = v
		} else {
			q.MaxAge = 6 * 3600
		}
		if v, ok := qInt64(r, "queueDelay"); ok {
			q.QueueDelay = v
		} else {
			q.QueueDelay = 100
		}
		if l := qInt(r, "limit"); l != nil {
			q.Limit = *l
		} else {
			q.Limit = 1000
		}
		if t, ok := qInt64(r, "timeout"); ok {
			q.Timeout = t
		} else {
			q.Timeout = 60 * 1000
		}
		q.Background = qBool(r, "background")
		write(w, r, func() (*httpcontroller.Result, error) { return c.FlushOld(q) })
	})
	mux.HandleFunc("GET /status", func(w http.ResponseWriter, r *http.Request) {
		textPlain(w, "project-history is up")
	})
	mux.HandleFunc("GET /check_lock", func(w http.ResponseWriter, r *http.Request) {
		code := c.CheckLock()
		if code == 200 {
			textPlain(w, "OK")
			return
		}
		sendStatus(w, code)
	})
	mux.HandleFunc("GET /health_check", func(w http.ResponseWriter, r *http.Request) {
		code := c.HealthCheck()
		if code == 200 {
			textPlain(w, "OK")
			return
		}
		sendStatus(w, code)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		sendStatus(w, 404)
	})
	return mux
}

// ---- helpers --------------------------------------------------------------

func toStr(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	if b, ok := v.(bool); ok {
		if b {
			return "true"
		}
		return "false"
	}
	if n, ok := v.(int); ok {
		return strconv.Itoa(n)
	}
	return ""
}

func boolAny(v any) bool {
	b, _ := v.(bool)
	return b
}

// boolAnyFalse — vendor `validate_exists` default(true): absent key → true.
func boolAnyFalse(v any) bool {
	if b, ok := v.(bool); ok {
		return b
	}
	return true // key absent/undefined → default true (zod .default(true))
}

func intInt(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	}
	return 0
}

func headers(r *http.Request) map[string]string {
	out := map[string]string{}
	for k, v := range r.Header {
		if len(v) > 0 {
			out[k] = v[0]
		}
	}
	return out
}

var _ = strings.ToUpper

// errBadRequestInt — missing/invalid required int query param (the vendor
// zod schema rejects the request with 400 via parseReq).
func errBadRequestInt(msg string) error {
	return pherr.BadRequest(msg)
}
