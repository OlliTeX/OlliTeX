package zotero

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"ollitex/go/services/web/core"
)

// ZOTERO_API_URL (Node ZoteroApiClient.mjs). Node uses a 60s request
// timeout; 35s sits inside the handler ctx budget. Only the linked paths
// reach the network — the gate never does.
const zoteroAPIURL = "https://api.zotero.org"

var zoteroHTTP = &http.Client{Timeout: 35 * time.Second}

func buildHeaders(apiKey string) http.Header {
	h := http.Header{}
	h.Set("Zotero-API-Version", "3")
	h.Set("Zotero-API-Key", apiKey)
	h.Set("User-Agent", "Overleaf-CEP-Zotero")
	return h
}

// normalizeAPIStatus mirrors Node's normalizeApiError status mapping:
// 403→'Access denied', 404→'Not found', 429→'Rate limit exeeded' [sic],
// otherwise 'RefProvider request error' status-passthrough.
func normalizeAPIStatus(status int) apiErr {
	switch status {
	case 403:
		return apiErr{status: 403, msg: "Access denied"}
	case 404:
		return apiErr{status: 404, msg: "Not found"}
	case 429:
		return apiErr{status: 429, msg: "Rate limit exeeded"}
	default:
		return apiErr{status: status, msg: "RefProvider request error"}
	}
}

func zoteroGetJSON(ctx context.Context, apiKey, path string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, zoteroAPIURL+path, nil)
	if err != nil {
		return nil, apiErr{status: 500, msg: "Something wrong with RefProvider request"}
	}
	req.Header = buildHeaders(apiKey)
	resp, err := zoteroHTTP.Do(req)
	if err != nil {
		return nil, apiErr{status: 504, msg: "RefProvider request timed out"}
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return nil, normalizeAPIStatus(resp.StatusCode)
	}
	return body, nil
}

// ---------- linked-path API (not exercised by the gate) ----------

// zoteroCheckKey — Node getConnectionStatus's live branch: GET /keys/{key}.
func zoteroCheckKey(ctx context.Context, apiKey string) error {
	_, err := zoteroGetJSON(ctx, apiKey, "/keys/"+url.PathEscape(apiKey))
	return err
}

// zoteroRevokeKey — Node unlinkAccount's DELETE /keys/{key} (errors are
// swallowed by the caller — Node logs and continues).
func zoteroRevokeKey(ctx context.Context, apiKey string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete,
		zoteroAPIURL+"/keys/"+url.PathEscape(apiKey), nil)
	if err != nil {
		return err
	}
	req.Header = buildHeaders(apiKey)
	resp, err := zoteroHTTP.Do(req)
	if err != nil {
		return err
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	return nil
}

type zoteroGroup struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// zoteroGroupList — Node getGroupsForUser:
// GET /users/{uid}/groups → [{id:String(id), name: data.name || 'Group <id>'}].
func zoteroGroupList(ctx context.Context, apiKey, zoteroUserID string) ([]zoteroGroup, error) {
	body, err := zoteroGetJSON(ctx, apiKey,
		"/users/"+url.PathEscape(zoteroUserID)+"/groups")
	if err != nil {
		return nil, err
	}
	var raw []struct {
		ID   any `json:"id"`
		Data *struct {
			Name string `json:"name"`
		} `json:"data"`
	}
	if jerr := json.Unmarshal(body, &raw); jerr != nil {
		return nil, apiErr{status: 500, msg: "Something wrong with RefProvider request"}
	}
	out := make([]zoteroGroup, 0, len(raw))
	for _, g := range raw {
		gid := fmt.Sprint(g.ID)
		name := "Group " + gid
		if g.Data != nil && g.Data.Name != "" {
			name = g.Data.Name
		}
		out = append(out, zoteroGroup{ID: gid, Name: name})
	}
	return out, nil
}

func zoteroGroupsJSON(ctx context.Context, apiKey, zoteroUserID string) ([]byte, error) {
	list, err := zoteroGroupList(ctx, apiKey, zoteroUserID)
	if err != nil {
		return nil, err
	}
	out := make([]byte, 0, 64)
	out = append(out, '[')
	for i, g := range list {
		if i > 0 {
			out = append(out, ',')
		}
		out = append(out, `{"id":"`+jsonString(g.ID)+`","name":"`+jsonString(g.Name)+`"}`...)
	}
	out = append(out, ']')
	return out, nil
}

// scopeFromQuery — Node _scopeFromQuery:
// { kind: libraryKind==='group' ? 'group' : 'user', id: library || ” }.
type pickerScope struct {
	kind string // 'user' | 'group'
	id   string
}

func scopeFromQuery(q url.Values) pickerScope {
	kind := "user"
	if q.Get("libraryKind") == "group" {
		kind = "group"
	}
	return pickerScope{kind: kind, id: q.Get("library")}
}

func scopeBasePath(scope pickerScope, zoteroUserID, leaf string) string {
	if scope.kind == "group" {
		return "/groups/" + url.PathEscape(scope.id) + leaf
	}
	return "/users/" + url.PathEscape(zoteroUserID) + leaf
}

// appendLibrariesAndGroups — Node getLibrariesForPicker: main library entry
// first, then group libraries ({id, kind:'group', name}).
func appendLibrariesAndGroups(mainBody []byte, groups []zoteroGroup) []byte {
	var add []byte
	for _, g := range groups {
		add = append(add,
			`,{"id":"`+jsonString(g.ID)+`","kind":"group","name":"`+jsonString(g.Name)+`"}`...)
	}
	// mainBody ends with ']' → splice the group entries before it.
	return append(append(mainBody[:len(mainBody)-1], add...), ']')
}

// zoteroCollectionsJSON — Node getCollectionsForPicker:
// GET /<scope>collections?limit=200 → [{key, name: data.name || key}].
// Node swallows API errors into an empty array (kept here, except an
// explicit 404 which the picker layer maps to 409 zotero_not_linked).
func zoteroCollectionsJSON(ctx context.Context, apiKey string, scope pickerScope, zoteroUserID string) ([]byte, error) {
	body, err := zoteroGetJSON(ctx, apiKey,
		scopeBasePath(scope, zoteroUserID, "/collections")+"?limit=200")
	if err != nil {
		if ae, ok := err.(apiErr); ok && ae.status == 404 {
			return nil, ae
		}
		return []byte("[]"), nil
	}
	var raw []struct {
		Key  string `json:"key"`
		Data *struct {
			Name string `json:"name"`
		} `json:"data"`
	}
	if jerr := json.Unmarshal(body, &raw); jerr != nil {
		return []byte("[]"), nil
	}
	out := make([]byte, 0, 32)
	out = append(out, '[')
	for i, c := range raw {
		if i > 0 {
			out = append(out, ',')
		}
		name := c.Key
		if c.Data != nil && c.Data.Name != "" {
			name = c.Data.Name
		}
		out = append(out, `{"key":"`+jsonString(c.Key)+`","name":"`+jsonString(name)+`"}`...)
	}
	out = append(out, ']')
	return out, nil
}

// zoteroItemsJSON — Node getItemsForPicker:
// GET /<scope>[/items | /_collections/{key}/items]?limit&start&sort&direction
// → { items: [{key,title,itemType,date,firstCreator}], total }.
func zoteroItemsJSON(ctx context.Context, apiKey string, scope pickerScope, zoteroUserID, collection string, limit, start int) ([]byte, error) {
	if limit <= 0 {
		limit = 100
	}
	if limit < 1 {
		limit = 1
	}
	if limit > 500 {
		limit = 500
	}
	if start < 0 {
		start = 0
	}
	itemsPath := "/items"
	if collection != "" {
		itemsPath = "/_collections/" + url.PathEscape(collection) + "/items"
	}
	q := url.Values{}
	q.Set("limit", strconv.Itoa(limit))
	q.Set("start", strconv.Itoa(start))
	q.Set("sort", "dateAdded")
	q.Set("direction", "desc")
	body, err := zoteroGetJSON(ctx, apiKey,
		scopeBasePath(scope, zoteroUserID, itemsPath)+"?"+q.Encode())
	if err != nil {
		if ae, ok := err.(apiErr); ok && ae.status == 404 {
			return nil, ae
		}
		return []byte(`{"items":[],"total":0}`), nil
	}
	var raw []struct {
		Key  string `json:"key"`
		Data *struct {
			Title    string `json:"title"`
			ItemType string `json:"itemType"`
			Date     string `json:"date"`
			Creators []struct {
				Name        string `json:"name"`
				FirstName   string `json:"firstName"`
				CreatorType string `json:"creatorType"`
			} `json:"creators"`
		} `json:"data"`
	}
	if jerr := json.Unmarshal(body, &raw); jerr != nil {
		return []byte(`{"items":[],"total":0}`), nil
	}
	items := make([]string, 0, len(raw))
	for _, it := range raw {
		creator, title, itemType, date := "", "", "", ""
		if it.Data != nil {
			title, itemType, date = it.Data.Title, it.Data.ItemType, it.Data.Date
			creators := it.Data.Creators
			if len(creators) > 0 {
				if itemType == "book" || itemType == "bookSection" {
					for _, e := range creators {
						if e.CreatorType == "editor" {
							creator = e.Name
							break
						}
					}
					if creator == "" {
						creator = creators[0].FirstName
						if creator == "" {
							creator = creators[0].Name
						}
					}
				} else {
					creator = creators[0].FirstName
					if creator == "" {
						creator = creators[0].Name
					}
				}
			}
		}
		items = append(items, `{"key":"`+jsonString(it.Key)+`","title":"`+jsonString(title)+
			`","itemType":"`+jsonString(itemType)+`","date":"`+jsonString(date)+
			`","firstCreator":"`+jsonString(creator)+`"}`)
	}
	return []byte(`{"items":[` + strings.Join(items, ",") + `],"total":` +
		strconv.Itoa(len(raw)) + "}"), nil
}

// zoteroItemsBibtex — Node getItemsBibtexForPicker + _fetchBibtexKeys:
// GET /<scope>/items/k1,k2?format=bibtex → string.
func zoteroItemsBibtex(ctx context.Context, apiKey string, scope pickerScope, zoteroUserID string, keys []string) (string, error) {
	if len(keys) > 200 {
		keys = keys[:200]
	}
	if len(keys) == 0 {
		return "", apiErr{status: 400, msg: "no items selected"}
	}
	path := scopeBasePath(scope, zoteroUserID, "/items") + "/" + strings.Join(keys, ",")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		zoteroAPIURL+path+"?format=bibtex", nil)
	if err != nil {
		return "", apiErr{status: 500, msg: "Something wrong with RefProvider request"}
	}
	req.Header = buildHeaders(apiKey)
	resp, err := zoteroHTTP.Do(req)
	if err != nil {
		return "", apiErr{status: 504, msg: "RefProvider request timed out"}
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return "", normalizeAPIStatus(resp.StatusCode)
	}
	return string(body), nil
}

// ---------- error-response helpers (controller error paths) ----------

// sendAPIErr — Node `err.info?.status || 500` + {message: err.message}.
func sendAPIErr(res *core.Res, err error) {
	ae, ok := err.(apiErr)
	if !ok {
		ae = apiErr{status: 500, msg: "Something wrong with RefProvider request"}
	}
	jsonMsg(res, ae.status, ae.msg)
}

// sendPickerErr — Node _pickerError: 404 → 409 zotero_not_linked,
// otherwise status + {message}.
func sendPickerErr(res *core.Res, err error) {
	ae, ok := err.(apiErr)
	if !ok {
		ae = apiErr{status: 500, msg: "Something wrong with RefProvider request"}
	}
	if ae.status == 404 {
		jsonMsg(res, 409, "zotero_not_linked")
		return
	}
	jsonMsg(res, ae.status, ae.msg)
}

// parseKeys — Node getPickerBibtex keys parse: split(','), trim +
// decodeURIComponent, drop empties.
func parseKeys(raw string) []string {
	var out []string
	for _, part := range strings.Split(raw, ",") {
		k := strings.TrimSpace(part)
		if k == "" {
			continue
		}
		if d, err := url.PathUnescape(k); err == nil {
			k = d
		}
		if k != "" {
			out = append(out, k)
		}
	}
	return out
}
