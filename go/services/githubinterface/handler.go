package githubinterface

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	pbhttp "ollitex/go/pbhttp"
)

// --- HTTP server (1:1 routes + concurrency + service token) -----------------

type GHIHandlers struct {
	Cfg GHIConfig
	G   *GHI
}

func NewGHIHandlers(cfg GHIConfig) *GHIHandlers {
	cfg.withDefaults()
	return &GHIHandlers{Cfg: cfg, G: NewGHI(cfg)}
}

func ghiTimingEqual(a, b string) bool {
	f, g := len(a), len(b)
	if f == 0 || g == 0 || f != g {
		return false
	}
	var diff byte
	for i := 0; i < f; i++ {
		diff |= a[i] ^ b[i]
	}
	return diff == 0
}

func ghiCreds(v map[string]interface{}) (serverUrl, username, password string) {
	if s, ok := v["server_url"].(string); ok {
		serverUrl = strings.TrimRight(s, "/")
	}
	if s, ok := v["username"].(string); ok {
		username = s
	}
	if s, ok := v["token"].(string); ok && s != "" {
		password = s
	} else if s, ok := v["password"].(string); ok {
		password = s
	}
	return
}

func ghiStr(v map[string]interface{}, key string) string {
	if s, ok := v[key].(string); ok {
		return s
	}
	return ""
}

func ghiInt(v map[string]interface{}, key string, def int) int {
	switch t := v[key].(type) {
	case float64:
		return int(t)
	case string:
		if n, e := strconv.Atoi(t); e == nil {
			return n
		}
	}
	return def
}

func NewGHIHandlerMux(cfg GHIConfig) http.Handler {
	cfg.withDefaults()
	handlers := &GHIHandlers{Cfg: cfg, G: NewGHI(cfg)}
	sem := make(chan struct{}, cfg.MaxOps)
	auth := func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if cfg.ServiceToken == "" {
				next(w, r)
				return
			}
			provided := r.Header.Get("X-Service-Token")
			if provided == "" {
				if a := r.Header.Get("Authorization"); len(a) > 7 && strings.ToLower(a[:7]) == "bearer " {
					provided = strings.TrimSpace(a[7:])
				}
			}
			if !ghiTimingEqual(cfg.ServiceToken, provided) {
				msg := "missing service token"
				if provided != "" {
					msg = "invalid service token"
				}
				if cfg.ServiceToken != "" {
					pbhttp.WriteJSONErr(w, 401, map[string]string{"error": msg})
					return
				}
			}
			next(w, r)
		}
	}
	route := func(name string) http.HandlerFunc {
		return auth(func(w http.ResponseWriter, r *http.Request) {
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
				handlers.dispatch(name, w, r)
			default:
				pbhttp.WriteJSONErr(w, 503, map[string]string{"error": "service busy; try again shortly"})
			}
		})
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		pbhttp.WriteJSON(w, 200, map[string]string{"status": "ok", "service": "githubinterface"})
	})
	mux.HandleFunc("/check", route("check"))
	mux.HandleFunc("/clone", route("clone"))
	mux.HandleFunc("/push", route("push"))
	mux.HandleFunc("/pull", route("pull"))
	mux.HandleFunc("/commit", route("commit"))
	mux.HandleFunc("/log", route("log"))
	mux.HandleFunc("/status", route("status"))
	mux.HandleFunc("/orgs", route("orgs"))
	mux.HandleFunc("/create-repo", route("create-repo"))
	mux.HandleFunc("/list-repos", route("list-repos"))
	mux.HandleFunc("/can-push", route("can-push"))
	mux.HandleFunc("/branch-head", route("branch-head"))
	mux.HandleFunc("/commits", route("commits"))
	// 1:1 with Node express.json({ limit: '10mb' }).
	return pbhttp.LimitBody(mux, 10<<20)
}

func (h *GHIHandlers) dispatch(name string, w http.ResponseWriter, r *http.Request) {
	var m map[string]interface{}
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&m)
	}
	if m == nil {
		m = map[string]interface{}{}
	}
	serverUrl, username, password := ghiCreds(m)
	switch name {
	case "check":
		code, res := h.G.Check(r.Context(), serverUrl, username, password)
		pbhttp.WriteJSON(w, code, res)
	case "clone":
		code, res := h.G.Clone(r.Context(), ghiStr(m, "repo_url"), ghiOr(ghiStr(m, "ref"), "HEAD"), ghiStr(m, "target_dir"), serverUrl, username, password)
		pbhttp.WriteJSON(w, code, res)
	case "push":
		code, res := h.G.Push(r.Context(), ghiStr(m, "dir"), ghiOr(ghiStr(m, "remote"), "origin"), ghiOr(ghiStr(m, "ref"), "HEAD"), username, password)
		pbhttp.WriteJSON(w, code, res)
	case "pull":
		code, res := h.G.Pull(r.Context(), ghiStr(m, "dir"), ghiOr(ghiStr(m, "remote"), "origin"), ghiOr(ghiStr(m, "ref"), "HEAD"), username, password)
		pbhttp.WriteJSON(w, code, res)
	case "commit":
		authorName, authorEmail := "", ""
		if a, ok := m["author"].(map[string]interface{}); ok {
			if s, ok := a["name"].(string); ok {
				authorName = s
			}
			if s, ok := a["email"].(string); ok {
				authorEmail = s
			}
		}
		var files []string
		if fl, ok := m["files"].([]interface{}); ok {
			for _, f := range fl {
				if fm, ok := f.(map[string]interface{}); ok {
					if p, ok := fm["path"].(string); ok && p != "" {
						files = append(files, p)
					}
				}
			}
		}
		code, res := h.G.Commit(r.Context(), ghiStr(m, "dir"), files, ghiStr(m, "message"), authorName, authorEmail, username)
		pbhttp.WriteJSON(w, code, res)
	case "log":
		q := r.URL.Query()
		limit, _ := strconv.Atoi(ghiFirstNonEmpty(q.Get("limit"), "50"))
		page, _ := strconv.Atoi(ghiFirstNonEmpty(q.Get("page"), "1"))
		code, res := h.G.Log(r.Context(), q.Get("dir"), ghiOr(q.Get("ref"), "HEAD"), limit, page)
		pbhttp.WriteJSON(w, code, res)
	case "status":
		code, res := h.G.Status(r.Context(), r.URL.Query().Get("dir"))
		pbhttp.WriteJSON(w, code, res)
	case "orgs":
		code, res := h.G.Orgs(r.Context(), serverUrl, username, password)
		pbhttp.WriteJSON(w, code, res)
	case "create-repo":
		isPublic, _ := m["is_public"].(bool)
		code, res := h.G.CreateRepo(r.Context(), serverUrl, username, password, ghiStr(m, "name"), ghiStr(m, "description"), isPublic, ghiStr(m, "org"))
		pbhttp.WriteJSON(w, code, res)
	case "list-repos":
		code, res := h.G.ListRepos(r.Context(), serverUrl, username, password)
		pbhttp.WriteJSON(w, code, res)
	case "can-push":
		code, res := h.G.CanPush(r.Context(), serverUrl, ghiStr(m, "repo"), username, password)
		pbhttp.WriteJSON(w, code, res)
	case "branch-head":
		code, res := h.G.BranchHead(r.Context(), serverUrl, ghiStr(m, "repo"), ghiStr(m, "branch"), username, password)
		pbhttp.WriteJSON(w, code, res)
	case "commits":
		code, res := h.G.Commits(r.Context(), serverUrl, ghiStr(m, "repo"), ghiStr(m, "branch"), ghiStr(m, "since"), ghiInt(m, "limit", 50), username, password)
		pbhttp.WriteJSON(w, code, res)
	}
}
