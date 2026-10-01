package gsync

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"ollitex/go/services/web/core"
	"ollitex/go/services/web/features/projectlist"

	"go.mongodb.org/mongo-driver/v2/bson"
)

func gsObjectID(hex string) (bson.ObjectID, error) {
	return bson.ObjectIDFromHex(hex)
}

func gsLoadProject(cxt *core.Cxt, oid bson.ObjectID) (*bson.D, bool) {
	return projectlist.LoadProjectDoc(cxt.A, cxt, oid)
}

func gsOwnerRef(d *bson.D) string { return projectlist.OwnerRef(d) }

func gsUserEmail(cxt *core.Cxt, uidHex string) string {
	e := projectlist.UserEmail(cxt.A, cxt, uidHex)
	if e == "" {
		return "nobody@nowhere"
	}
	return e
}

func jsonMarshal(v any) ([]byte, error) { return json.Marshal(v) }

var gsHTTPClientOnce sync.Once
var gsHTTPClientV *http.Client

func gsHTTPClient() *http.Client {
	gsHTTPClientOnce.Do(func() {
		gsHTTPClientV = &http.Client{Timeout: 4 * time.Minute}
	})
	return gsHTTPClientV
}

func osRemoveAll(p string) { _ = os.RemoveAll(p) }

// gsWalkDir — walk dir (skipping .git), returning ImportEntry list.
func gsWalkDir(root string) ([]projectlist.ImportEntry, error) {
	out := []projectlist.ImportEntry{}
	err := filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			base := filepath.Base(p)
			if base == ".git" && p != root {
				return filepath.SkipDir
			}
			return nil
		}
		rel, rerr := filepath.Rel(root, p)
		if rerr != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if rel == "" || strings.HasPrefix(rel, ".git/") {
			return nil
		}
		b, berr := os.ReadFile(p)
		if berr != nil {
			return nil
		}
		out = append(out, projectlist.ImportEntry{Path: rel, Data: b})
		return nil
	})
	return out, err
}

// gsWriteFile — write a file into the work tree (export).
func gsWriteFile(root, rel string, data []byte) bool {
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return false
	}
	return os.WriteFile(p, data, 0o644) == nil
}

// gsAddServerConfig — Node _addServerConfig (servers bookkeeping only, no
// token).
func gsAddServerConfig(ctx context.Context, a *core.App, uid, provider, serverUrl, username string) error {
	return gsWithUserLock(uid, func() error {
		nurl := gsNormURL(serverUrl)
		cleanU := strings.TrimSpace(username)
		now := time.Now().UTC()
		doc := gsGetCredsDoc(ctx, a, uid)
		if doc == nil {
			doc = &gsCredsDoc{UserID: uid, CreatedAt: now}
		}
		if doc.Servers == nil {
			doc.Servers = map[string]map[string]any{}
		}
		if doc.Servers[provider] == nil {
			doc.Servers[provider] = map[string]any{}
		}
		sb := doc.Servers[provider][nurl]
		serverMap := map[string]any{}
		if m, ok := sb.(map[string]any); ok {
			if _, isLegacy := m["username"]; isLegacy {
				lu, _ := m["username"].(string)
				if lu != "" {
					serverMap[lu] = gsServerEntry{CreatedAt: now, LastUsedAt: now}
				}
			} else {
				for k, v := range m {
					serverMap[k] = v
				}
			}
		}
		if pe, ok := serverMap[cleanU].(gsServerEntry); ok && pe.CreatedAt.IsZero() {
			pe.CreatedAt = now
			serverMap[cleanU] = pe
		} else {
			serverMap[cleanU] = gsServerEntry{CreatedAt: now, LastUsedAt: now}
		}
		doc.Servers[provider][nurl] = serverMap
		return gsUpsertCredsDoc(ctx, a, doc)
	})
}

var _ = http.MethodGet
var _ = gsOAuthClientIDEnv
