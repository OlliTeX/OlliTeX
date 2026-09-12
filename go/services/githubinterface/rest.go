package githubinterface

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// --- provider REST (1:1 with detectApiBase / apiHeaders / baseFamily) -------

func baseFamily(base string) string {
	if strings.HasSuffix(base, "/api/v4") {
		return "gitlab"
	}
	if strings.HasSuffix(base, "/api/v3") {
		return "github"
	}
	return "gitea"
}

func apiHeaders(token, base string) map[string]string {
	family := baseFamily(base)
	h := map[string]string{
		"Accept":       "application/json",
		"Content-Type": "application/json",
	}
	if token != "" {
		if family == "gitea" || family == "forgejo" {
			h["Authorization"] = "token " + token
		} else {
			h["Authorization"] = "Bearer " + token
		}
		if family == "gitlab" {
			h["PRIVATE-TOKEN"] = token
		}
	}
	return h
}

func detectApiBase(ctx context.Context, client *http.Client, serverUrl string) string {
	u := strings.TrimSpace(serverUrl)
	u = strings.TrimRight(u, "/")
	host := ""
	if parsed, err := url.Parse(u); err == nil {
		host = strings.ToLower(parsed.Hostname())
	}
	if strings.HasSuffix(host, "github.com") {
		return "https://api.github.com"
	}
	for _, suffix := range []string{"api/v4", "api/v1", "api/v3"} {
		base := u + "/" + suffix
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/user", nil)
		if err != nil {
			continue
		}
		req.Header.Set("Accept", "application/json")
		resp, err := client.Do(req)
		if err != nil {
			continue
		}
		resp.Body.Close()
		if resp.StatusCode == 200 || resp.StatusCode == 401 || resp.StatusCode == 403 {
			return base
		}
	}
	return u + "/api/v4"
}

func ghiHTTP(ctx context.Context, client *http.Client, method, urlStr string, headers map[string]string, body []byte) (int, []byte, error) {
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, urlStr, rd)
	if err != nil {
		return 0, nil, err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	b, rerr := io.ReadAll(resp.Body)
	if rerr != nil {
		return resp.StatusCode, b, rerr
	}
	return resp.StatusCode, b, nil
}
