package dropboxinterface

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
)

// DropboxClient performs Dropbox API v2 operations.
type DropboxClient struct {
	Cfg         DropboxConfig
	AccessToken string
}

func NewDropboxClient(cfg DropboxConfig, accessToken string) (*DropboxClient, error) {
	if accessToken == "" {
		return nil, errors.New("Missing or invalid access token")
	}
	cfg.withDefaults()
	return &DropboxClient{Cfg: cfg, AccessToken: accessToken}, nil
}

// dbxCall performs a JSON API call and returns the body + status.
func (c *DropboxClient) dbxCall(ctx context.Context, url string, header http.Header, body []byte) (int, []byte, error) {
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, rd)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.AccessToken)
	if header != nil {
		for k, vs := range header {
			for _, v := range vs {
				req.Header.Set(k, v)
			}
		}
	}
	if body == nil && rd == nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.Cfg.HTTPClient.Do(req)
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

// apiError parses a Dropbox error body ({"error":{...}}) and returns a
// classified *DropboxError via mapDropboxError.
func (c *DropboxClient) apiError(status int, body []byte) *DropboxError {
	var env struct {
		Error *dropboxErrorDetails `json:"error"`
	}
	if err := json.Unmarshal(body, &env); err == nil && env.Error != nil {
		code, msg, ecode := mapDropboxError(status, env.Error)
		return dropboxStatus(code, msg, ecode)
	}
	code, msg, ecode := mapDropboxError(status, nil)
	return dropboxStatus(code, msg, ecode)
}

// Check verifies auth (1:1).
func (c *DropboxClient) Check(ctx context.Context) (string, error) {
	status, body, err := c.dbxCall(ctx, c.Cfg.APIBase+"/users/get_current_account", nil, nil)
	if err != nil {
		return "", err
	}
	if status < 200 || status >= 300 {
		return "", c.apiError(status, body)
	}
	var r struct {
		AccountID string `json:"account_id"`
	}
	if jerr := json.Unmarshal(body, &r); jerr != nil {
		return "", jerr
	}
	return r.AccountID, nil
}

// dbxListEntry is one Dropbox file entry from list_folder.
type dbxListEntry struct {
	Name           string  `json:"name"`
	PathDisplay    string  `json:"path_display"`
	Tag            string  `json:".tag"`
	Size           int64   `json:"size"`
	Hash           *string `json:"hash"`
	ContentHash    *string `json:"content_hash"`
	ServerModified string  `json:"server_modified"`
	ID             string  `json:"id"`
	Rev            *string `json:"rev"`
}

// List returns the entries for a path with cursor pagination (1:1).
func (c *DropboxClient) List(ctx context.Context, p string, recursive bool) (map[string]interface{}, error) {
	cursor := ""
	all := []DropboxEntry{}
	for {
		reqBody := map[string]interface{}{
			"path":               p,
			"recursive":          recursive,
			"include_media_info": false,
			"include_deleted":    false,
			"limit":              1000,
		}
		if cursor != "" {
			reqBody["cursor"] = cursor
		} else {
			reqBody["cursor"] = nil
		}
		b, jerr := json.Marshal(reqBody)
		if jerr != nil {
			return nil, jerr
		}
		status, body, err := c.dbxCall(ctx, c.Cfg.APIBase+"/files/list_folder", nil, b)
		if err != nil {
			return nil, err
		}
		if status < 200 || status >= 300 {
			return nil, c.apiError(status, body)
		}
		var r struct {
			Entries []dbxListEntry `json:"entries"`
			Cursor  string         `json:"cursor"`
			HasMore bool           `json:"has_more"`
		}
		if jerr := json.Unmarshal(body, &r); jerr != nil {
			return nil, jerr
		}
		for _, e := range r.Entries {
			rel := strings.TrimPrefix(e.PathDisplay, "/")
			if rel == "" {
				rel = e.Name
			}
			typ := "file"
			if e.Tag == "folder" {
				typ = "folder"
			}
			var mtime *string
			if e.ServerModified != "" {
				if t, perr := parseISO8601(e.ServerModified); perr == nil {
					s := t.UTC().Format("2006-01-02T15:04:05.000Z")
					mtime = &s
				}
			}
			all = append(all, DropboxEntry{
				RelativePath: rel,
				Name:         e.Name,
				Type:         typ,
				Size:         e.Size,
				Binary:       true,
				Checksum:     nil,
				Hash:         e.Hash,
				ContentHash:  e.ContentHash,
				Mtime:        mtime,
				DropboxID:    e.ID,
				Rev:          e.Rev,
			})
		}
		if !r.HasMore {
			break
		}
		cursor = r.Cursor
	}
	return map[string]interface{}{"entries": all, "has_more": false}, nil
}

// Download returns base64 content; 404 -> notFound (1:1).
func (c *DropboxClient) Download(ctx context.Context, p string) (string, bool, error) {
	arg, _ := json.Marshal(map[string]string{"path": p})
	h := http.Header{}
	h.Set("Dropbox-API-Arg", string(arg))
	h.Set("Content-Type", "application/octet-stream")
	status, body, err := c.dbxCall(ctx, c.Cfg.ContentBase+"/files/download", h, nil)
	if err != nil {
		return "", false, err
	}
	if status < 200 || status >= 300 {
		if st := dropboxStatusOf(c.apiError(status, body)); st == 404 {
			return "", true, nil
		}
		return "", false, c.apiError(status, body)
	}
	return base64.StdEncoding.EncodeToString(body), false, nil
}

// Upload creates/updates a file (1:1, incl. mode/rev + mute; conflict -> 412).
func (c *DropboxClient) Upload(ctx context.Context, p, contentBase64 string, mode string, rev string) (map[string]interface{}, error) {
	content, err := base64.StdEncoding.DecodeString(contentBase64)
	if err != nil {
		return nil, err
	}
	arg := map[string]interface{}{
		"path": p,
		"mode": mode,
		"mute": true,
	}
	if mode == "update" && rev != "" {
		arg["mode"] = map[string]interface{}{"update": rev}
	}
	argJSON, _ := json.Marshal(arg)
	h := http.Header{}
	h.Set("Dropbox-API-Arg", string(argJSON))
	h.Set("Content-Type", "application/octet-stream")
	status, body, err := c.dbxCall(ctx, c.Cfg.ContentBase+"/files/upload", h, content)
	if err != nil {
		return nil, err
	}
	if status < 200 || status >= 300 {
		return nil, c.apiError(status, body)
	}
	var r struct {
		Rev string `json:"rev"`
		ID  string `json:"id"`
	}
	if jerr := json.Unmarshal(body, &r); jerr != nil {
		return nil, jerr
	}
	return map[string]interface{}{"success": true, "revision": r.Rev, "dropbox_id": r.ID}, nil
}

// Delete removes a path; 404 -> notFound (1:1).
func (c *DropboxClient) Delete(ctx context.Context, p string) (bool, error) {
	b, _ := json.Marshal(map[string]string{"path": p})
	status, body, err := c.dbxCall(ctx, c.Cfg.APIBase+"/files/delete_v2", nil, b)
	if err != nil {
		return false, err
	}
	if status < 200 || status >= 300 {
		if st := dropboxStatusOf(c.apiError(status, body)); st == 404 {
			return true, nil
		}
		return false, c.apiError(status, body)
	}
	return false, nil
}

// CreateDirectory creates a folder; 409 -> created=false (1:1).
func (c *DropboxClient) CreateDirectory(ctx context.Context, p string) (bool, error) {
	b, _ := json.Marshal(map[string]string{"path": p})
	status, body, err := c.dbxCall(ctx, c.Cfg.APIBase+"/files/create_folder_v2", nil, b)
	if err != nil {
		return false, err
	}
	if status < 200 || status >= 300 {
		if st := dropboxStatusOf(c.apiError(status, body)); st == 409 {
			return false, nil
		}
		return false, c.apiError(status, body)
	}
	return true, nil
}

// Move renames/moves a path (1:1).
func (c *DropboxClient) Move(ctx context.Context, src, dst string) (map[string]interface{}, error) {
	b, _ := json.Marshal(map[string]string{"from_path": src, "to_path": dst})
	status, body, err := c.dbxCall(ctx, c.Cfg.APIBase+"/files/move_v2", nil, b)
	if err != nil {
		return nil, err
	}
	if status < 200 || status >= 300 {
		return nil, c.apiError(status, body)
	}
	var r struct {
		Metadata struct {
			PathDisplay string `json:"path_display"`
		} `json:"metadata"`
	}
	if jerr := json.Unmarshal(body, &r); jerr != nil {
		return nil, jerr
	}
	return map[string]interface{}{"success": true, "old_path": src, "new_path": r.Metadata.PathDisplay}, nil
}

// GetMetadata returns file metadata (1:1).
func (c *DropboxClient) GetMetadata(ctx context.Context, p string) (map[string]interface{}, error) {
	b, _ := json.Marshal(map[string]interface{}{"path": p, "include_deleted": false})
	status, body, err := c.dbxCall(ctx, c.Cfg.APIBase+"/files/get_metadata", nil, b)
	if err != nil {
		return nil, err
	}
	if status < 200 || status >= 300 {
		return nil, c.apiError(status, body)
	}
	var r struct {
		PathDisplay    string  `json:"path_display"`
		Name           string  `json:"name"`
		Tag            string  `json:".tag"`
		Size           int64   `json:"size"`
		Rev            *string `json:"rev"`
		ID             string  `json:"id"`
		ServerModified string  `json:"server_modified"`
	}
	if jerr := json.Unmarshal(body, &r); jerr != nil {
		return nil, jerr
	}
	typ := "file"
	if r.Tag == "folder" {
		typ = "folder"
	}
	var mtime string
	if r.ServerModified != "" {
		if t, perr := parseISO8601(r.ServerModified); perr == nil {
			mtime = t.UTC().Format("2006-01-02T15:04:05.000Z")
		}
	}
	return map[string]interface{}{
		"path_display": r.PathDisplay,
		"name":         r.Name,
		"type":         typ,
		"size":         r.Size,
		"rev":          r.Rev,
		"dropbox_id":   r.ID,
		"mtime":        mtime,
	}, nil
}
