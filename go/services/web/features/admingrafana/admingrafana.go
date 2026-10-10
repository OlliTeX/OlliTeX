// Package admingrafana — AG (owner 2026-10-09): the admin-gated,
// same-origin Grafana proxy.
//
// Owner directive (verbatim intent):
//   - "Grafana is running locally with Prometheus; use Grafana's embed URL"
//   - "Grafana embeds must not be accessible to non-admins"
//   - "Grafana auth via OlliTeX admin check"
//   - "do NOT ship a static Grafana admin token or anonymous access with a
//     static secret"
//
// Architecture (replaces the AJ-3 anonymous-kiosk design):
//
//	Browser ──▶ https://<host>/admin/grafana/<rest>
//	                   │  OlliTeX web service — SITE-ADMIN session gate
//	                   │  (a.RequireSiteAdmin, same as every /admin route)
//	                   │  injects server-side Basic auth (env; never shipped
//	                   │  to the client; anonymous access disabled in compose)
//	                   ▼
//	              http://ollitex-grafana:3000/grafana/<rest>
//
// The Grafana container runs GF_SERVER_ROOT_URL=https://<host>/admin/grafana/
// + GF_SERVER_SERVE_FROM_SUB_PATH=true, so every URL Grafana generates
// (static assets, /api/*, live/websocket) points back through this gated
// route. The public /grafana/ HAProxy edge is retired — on this origin
// Grafana is reachable exclusively behind the site-admin check.
package admingrafana

import (
	"bufio"
	"encoding/base64"
	"io"
	"net"
	"net/http"
	"net/url"
	"net/http/httputil"
	"os"
	"regexp"
	"strconv"
"strings"
	"time"

	"ollitex/go/services/web/core"
)

const (
	adminPathPrefix = "/admin/grafana"
	// GF_SERVER_ROOT_URL on the container is https://<host>/admin/grafana/,
	// i.e. Grafana serves under the /admin/grafana subpath — the proxy maps
	// /admin/grafana/<rest> onto the SAME upstream subpath (no /grafana
	// segment; that is the retired public edge and 301-redirects).
	upstreamSubPath = "/admin/grafana"
)

var grafanaPathRe = regexp.MustCompile(`^/admin/grafana(/.*)?$`)

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

// Feature — GET /admin/grafana[/<rest>] → admin-gated reverse proxy.
func Feature(a *core.App) core.Feature {
	target := envOr("GRAFANA_PROXY_TARGET", "http://ollitex-grafana:3000")
	user := envOr("GRAFANA_ADMIN_USER", "admin")
	pass := envOr("GRAFANA_ADMIN_PASSWORD", "ollitex")
	cred := base64.StdEncoding.EncodeToString([]byte(user + ":" + pass))

	targetURL, perr := url.Parse(target)
	if perr != nil {
		targetURL = &url.URL{Scheme: "http", Host: "ollitex-grafana:3000"}
	}

	rp := &httputil.ReverseProxy{
		Director: func(r *http.Request) {
			// /admin/grafana/<rest> → upstream /grafana/<rest>
			rest := strings.TrimPrefix(r.URL.Path, adminPathPrefix)
			r.URL.Scheme = targetURL.Scheme
			r.URL.Host = targetURL.Host
			r.URL.Path = upstreamSubPath + rest
			r.Host = targetURL.Host
		},
		ModifyResponse: func(h *http.Response) error {
			// Same-origin from the browser's point of view: strip any
			// frame-blocking directive the upstream adds (Grafana's own
			// CSP frame-ancestors already allows the app origin).
			h.Header.Del("X-Frame-Options")

			// The upstream serves under /grafana (GF_SERVER_ROOT_URL path)
			// while the browser must only ever use the GATED /admin/grafana
			// route — rewrite the two URL shapes Grafana emits so assets,
			// api, live and redirects all come back through the gate:
			if loc := h.Header.Get("Location"); loc != "" {
				h.Header.Set("Location", rewriteGrafanaURLs(loc))
			}
			if h.Body != nil && strings.Contains(h.Header.Get("Content-Type"), "text/html") {
				if b, rerr := io.ReadAll(io.LimitReader(h.Body, 8<<20)); rerr == nil {
					_ = h.Body.Close()
					h.Body = io.NopCloser(strings.NewReader(rewriteGrafanaURLs(string(b))))
					v := strconv.FormatInt(int64(len(b)), 10)
					h.Header.Set("Content-Length", v)
				}
			}
			return nil
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, perr error) {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.WriteHeader(http.StatusBadGateway)
			io.WriteString(w, "grafana proxy failed (502)")
		},
		Transport: &http.Transport{
			DialContext:         (&net.Dialer{Timeout: 5 * time.Second}).DialContext,
			MaxIdleConns:        32,
			IdleConnTimeout:     90 * time.Second,
			TLSHandshakeTimeout: 5 * time.Second,
			ResponseHeaderTimeout: 30 * time.Second,
		},
	}

	handler := func(cxt *core.Cxt, res *core.Res) {
		// 1) admin gate FIRST — no non-admin reach, no markup/token
		//    disclosure (RequireSiteAdmin emits the standard 403 shape).
		if !a.RequireSiteAdmin(cxt, res) {
			return
		}
		req := cxt.Req

		// 2) server-side Basic auth (env — never a client-visible token).
		req.Header.Set("Authorization", "Basic "+cred)
		req.Header.Set("X-Forwarded-Proto", "https")
		if id := req.Header.Get("X-Forwarded-For"); id != "" {
			req.Header.Set("X-Forwarded-For", id+", proxy")
		}

		// 3) websocket upgrade (Grafana live) — hand proxy; the
		//    ReverseProxy answers 502 to upgrades in net/http < 1.20 style
		//    and we need the hijacked pipes anyway.
		if isWebSocketUpgrade(req) && res.W != nil {
			if hj, ok := res.W.(http.Hijacker); ok {
				if proxyWebSocket(req, cred, hj) {
					return
				}
			}
		}

		// 4) everything else streams through (kiosk HTML, assets, /api/*,
		//    SSE): ReverseProxy on the raw writer.
		rp.ServeHTTP(res.W, req)
	}

	return core.Feature{
		Name: "admingrafana",
		Routes: []core.Route{
			{Method: "GET", Pattern: grafanaPathRe, Handler: handler},
		},
	}
}


// rewriteGrafanaURLs maps the upstream's self-published /grafana/ URLs onto
// the gated /admin/grafana/ route: absolute <scheme>://<host>/grafana/...
// and quoted relative "/grafana/... (href/src/url(...) literals). Already-gated
// /admin/grafana/ occurrences are left untouched (the absolute-URL and
// leading-quote anchors do not match them).
func rewriteGrafanaURLs(in string) string {
	host := "psintern.neuro.uni-bremen.de"
	if v := os.Getenv("INTERNAL_DOMAIN"); v != "" {
		host = v
	}
	// absolute: <scheme>://<host>/grafana/...  (any scheme, incl. ws/wss)
	in = strings.ReplaceAll(in, "://"+host+"/grafana/", "://"+host+"/admin/grafana/")
	// quoted relative literals: href="/grafana/...  src='/grafana/...  url(/grafana/...
	in = strings.ReplaceAll(in, `"/grafana/`, `"/admin/grafana/`)
	in = strings.ReplaceAll(in, `'/grafana/`, `'/admin/grafana/`)
	in = strings.ReplaceAll(in, "url(/grafana/", "url(/admin/grafana/")
	return in
}

func isWebSocketUpgrade(r *http.Request) bool {
	if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		return false
	}
	return strings.Contains(strings.ToLower(r.Header.Get("Connection")), "upgrade")
}

// proxyWebSocket — minimal RFC6455 handshake proxy for /grafana/api/live.
// Returns true when the hijacked pipes were taken (handler must not write).
func proxyWebSocket(req *http.Request, cred string, hj http.Hijacker) bool {
	target := envOr("GRAFANA_PROXY_TARGET", "http://ollitex-grafana:3000")
	rest := strings.TrimPrefix(req.URL.Path, adminPathPrefix)
	upstream := target + upstreamSubPath + rest

	clientConn, clientBuf, herr := hj.Hijack()
	if herr != nil {
		return false
	}
	defer clientConn.Close()

	up, err := url.Parse(upstream)
	if err != nil {
		clientBuf.WriteString("HTTP/1.1 502 Bad Gateway\r\n\r\n")
		clientBuf.Flush()
		return true
	}

	dialer := &net.Dialer{Timeout: 5 * time.Second}
	serverConn, err := dialer.DialContext(req.Context(), "tcp", up.Host)
	if err != nil {
		clientBuf.WriteString("HTTP/1.1 502 Bad Gateway\r\n\r\n")
		clientBuf.Flush()
		return true
	}
	defer serverConn.Close()

	// --- client handshake → upstream ----------------------------------
	req.Header.Set("Authorization", "Basic "+cred)
	req.Header.Set("Host", up.Host)
	if werr := req.Write(serverConn); werr != nil {
		return true
	}

	// --- upstream upgrade response → client ----------------------------
	br := bufio.NewReader(serverConn)
	resp, rerr := http.ReadResponse(br, req)
	if rerr != nil || resp.StatusCode != http.StatusSwitchingProtocols {
		if resp != nil {
			io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
			resp.Body.Close()
		}
		clientBuf.WriteString("HTTP/1.1 502 Bad Gateway\r\n\r\n")
		clientBuf.Flush()
		return true
	}
	clientBuf.WriteString(resp.Status + "\r\n")
	for k, vs := range resp.Header {
		for _, v := range vs {
			clientBuf.WriteString(k + ": " + v + "\r\n")
		}
	}
	clientBuf.WriteString("\r\n")
	if ferr := clientBuf.Flush(); ferr != nil {
		return true
	}

	// --- bidirectional pump until either end dies ----------------------
	go func() { _, _ = io.Copy(serverConn, clientConn) }()
	if _, cerr := io.Copy(clientConn, resp.Body); cerr == nil {
		// clean close on the client side
		if cw, ok := clientConn.(interface{ CloseWrite() error }); ok {
			_ = cw.CloseWrite()
		}
	}
	resp.Body.Close()
	return true
}
