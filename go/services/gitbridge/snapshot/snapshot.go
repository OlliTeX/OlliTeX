// Package snapshot ports snapshot/* (wire layer) and the snapshot base:
//
//	getdoc        GET  {apiBase}docs/{project}
//	getsavedvers  GET  {apiBase}docs/{project}/saved_vers
//	getforversion GET  {apiBase}docs/{project}/snapshots/{versionId}
//	push          POST {apiBase}docs/{project}/snapshots
//
// plus PostbackManager/PostbackPromise (postback.go) and the facade
// (facade.go). NetSnapshotApi maps HTTP statuses 1:1 to Java's
// Request.getResult() (401/403 -> Forbidden, 404 -> MissingRepository,
// 409 projectHasDotGit, 429 rate-limit).
package snapshot

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"ollitex/go/services/gitbridge/data"
	"ollitex/go/services/gitbridge/giterrors"
)

// ---------------------------------------------------------------------------
// API base URL
// Java: SnapshotAPIRequest.BASE_URL; Config enforces trailing "/", and
// BASE_URL = baseURL + "docs/". Java's URL = BASE_URL + projectName + apiCall
// (NO separator: e.g. ".../docs/" + "proj" + "/snapshots").
// ---------------------------------------------------------------------------

var apiBaseURL string

// SetAPIBaseURL (Java: SnapshotAPIRequest.setBaseURL). apiBaseUrl must be
// the config value WITHOUT trailing slash handled here: Java does
// `BASE_URL + "docs/"` and relies on the config value (which itself may or
// may not end with "/"). Config's fromJSON adds "/" to apiBaseUrl, so the
// wire path is "{config apiBaseUrl}docs/{project}".
func SetAPIBaseURL(u string) {
	if !strings.HasSuffix(u, "/") {
		u += "/"
	}
	apiBaseURL = u + "docs/"
}

func projectURL(project, apiCall string) string {
	return apiBaseURL + project + apiCall
}

// ---------------------------------------------------------------------------
// Wire types
// ---------------------------------------------------------------------------

// User ports getsavedvers/WLUser (Anonymous default).
type User struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

// GetDocResult ports snapshot/getdoc/GetDocResult.
// Wire (200): {"status":401|403} or
//
//	{"latestVerId":int, "latestVerAt":"ISO"?, "latestVerBy":{"email","name"} | null}
type GetDocResult struct {
	VersionID int
	CreatedAt string // "" when absent (v2-import edge case, PR #50)
	Name      string
	Email     string
	Status    int  // 401/403 when the success response carries a status
	Invalid   bool // status 404 (Java: InvalidProjectException on getVersionID)
	Forbidden bool // status 401/403
}

// ParseGetDocResponse ports GetDocResult.fromJSON — exported so the
// SnapshotApiFacade and tests can construct results without firing HTTP.
func ParseGetDocResponse(status *int, latestVerId *int, latestVerAt *string, latestVerBy *User) *GetDocResult {
	r := &GetDocResult{}
	if status != nil {
		r.Status = *status
		switch *status {
		case 401, 403:
			r.Forbidden = true
		case 404:
			r.Invalid = true
		default:
			// Java: unknown get-doc error code -> IllegalArgumentException
			return nil
		}
		return r
	}
	if latestVerId != nil {
		r.VersionID = *latestVerId
	}
	if latestVerAt != nil {
		r.CreatedAt = *latestVerAt
	}
	if latestVerBy != nil {
		r.Name = latestVerBy.Name
		r.Email = latestVerBy.Email
	}
	return r
}

// parseGetDocResponse = ParseGetDocResponse (backward-compat alias for the
// NetSnapshotApi internal call sites).
func parseGetDocResponse(status *int, latestVerId *int, latestVerAt *string, latestVerBy *User) *GetDocResult {
	return ParseGetDocResponse(status, latestVerId, latestVerAt, latestVerBy)
}

// VersionIDErr ports GetDocResult.getVersionID: returns the version, or an
// InvalidProjectException for a 404 result.
func (r *GetDocResult) VersionIDErr() (int, error) {
	if r.Invalid {
		return 0, &giterrors.GetDocInvalidProjectException{}
	}
	return r.VersionID, nil
}

// SnapshotFile ports snapshot/getforversion/SnapshotFile.
// Wire: a TWO-ITEM ARRAY [contents, path].
type SnapshotFile struct {
	Contents []byte
	Path     string
}

func (f SnapshotFile) Size() int64 { return int64(len(f.Contents)) }

// SnapshotAttachment ports snapshot/getforversion/SnapshotAttachment.
// Wire: a TWO-ITEM ARRAY [url, path].
type SnapshotAttachment struct {
	URL  string
	Path string
}

// SnapshotData ports snapshot/getforversion/SnapshotData.
// Wire: {"srcs": [[contents, path],...], "atts": [[url, path],...]}.
type SnapshotData struct {
	Srcs []SnapshotFile
	Atts []SnapshotAttachment
}

// SnapshotInfo ports getsavedvers/SnapshotInfo
// Wire: {"versionId":int,"comment":str,"user":{"email","name"},"createdAt":ISO}
// CreatedAt is the raw ISO string per wire (the v2-import edge case may omit
// it, so it is parsed leniently by data.ParseSnapshotCreated).
type SnapshotInfo struct {
	VersionID int    `json:"versionId"`
	Comment   string `json:"comment"`
	User      User   `json:"user"`
	CreatedAt string `json:"createdAt"`
}

// PushResult ports snapshot/push/PushResult.
type PushResult struct {
	WasSuccessful bool
}

// ---------------------------------------------------------------------------
// NetSnapshotApi (ports bridge/snapshot/NetSnapshotApi + base/Request
// status mapping)
// ---------------------------------------------------------------------------

type NetSnapshotApi struct{}

// GetDoc ports NetSnapshotApi.getDoc (via Facade.getDoc, see facade.go).
func (api *NetSnapshotApi) GetDoc(_ *data.Oauth2, project string) (*GetDocResult, error) {
	body, sc, err := doGET(api, projectURL(project, ""))
	if err != nil {
		return nil, &giterrors.FailedConnectionException{}
	}
	if sc >= 400 {
		if e := statusError(sc, body); e != nil {
			return nil, e
		}
	}
	var wire struct {
		Status      *int    `json:"status"`
		LatestVerId *int    `json:"latestVerId"`
		LatestVerAt *string `json:"latestVerAt"`
		LatestVerBy *User   `json:"latestVerBy"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return nil, &giterrors.FailedConnectionException{}
	}
	return parseGetDocResponse(wire.Status, wire.LatestVerId, wire.LatestVerAt, wire.LatestVerBy), nil
}

// GetSavedVers ports NetSnapshotApi.getSavedVers.
func (api *NetSnapshotApi) GetSavedVers(_ *data.Oauth2, project string) ([]SnapshotInfo, error) {
	body, sc, err := doGET(api, projectURL(project, "/saved_vers"))
	if err != nil {
		return nil, &giterrors.FailedConnectionException{}
	}
	if sc >= 400 {
		if e := statusError(sc, body); e != nil {
			return nil, e
		}
		// (unreached: statusError returns non-nil for all 4xx/5xx handled by Java)
	}
	var out []SnapshotInfo
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, &giterrors.FailedConnectionException{}
	}
	return out, nil
}

// GetForVersion ports NetSnapshotApi.getForVersion.
func (api *NetSnapshotApi) GetForVersion(_ *data.Oauth2, project string, versionID int) (*SnapshotData, error) {
	body, sc, err := doGET(api, projectURL(project, "/snapshots/"+strconv.Itoa(versionID)))
	if err != nil {
		return nil, &giterrors.FailedConnectionException{}
	}
	if sc >= 400 {
		if e := statusError(sc, body); e != nil {
			return nil, e
		}
	}
	var wire struct {
		Srcs [][2]string `json:"srcs"`
		Atts [][2]string `json:"atts"`
	}
	if err := json.Unmarshal(body, &wire); err != nil {
		return nil, &giterrors.FailedConnectionException{}
	}
	sd := &SnapshotData{Srcs: []SnapshotFile{}, Atts: []SnapshotAttachment{}}
	for _, e := range wire.Srcs {
		sd.Srcs = append(sd.Srcs, SnapshotFile{Contents: []byte(e[0]), Path: e[1]})
	}
	for _, a := range wire.Atts {
		sd.Atts = append(sd.Atts, SnapshotAttachment{URL: a[0], Path: a[1]})
	}
	return sd, nil
}

// Push ports NetSnapshotApi.push (PushResult: "accepted" => success,
// "outOfDate" => not-successful). Java throws RuntimeException on unknown
// code; we return an error.
func (api *NetSnapshotApi) Push(_ *data.Oauth2, project string, body []byte) (*PushResult, error) {
	respBody, sc, err := doPOST(api, projectURL(project, "/snapshots"), body)
	if err != nil {
		return nil, &giterrors.FailedConnectionException{}
	}
	if sc >= 400 {
		if e := statusError(sc, respBody); e != nil {
			return nil, e
		}
	}
	var pr struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(respBody, &pr); err != nil {
		return nil, &giterrors.FailedConnectionException{}
	}
	switch pr.Code {
	case "accepted":
		return &PushResult{WasSuccessful: true}, nil
	case "outOfDate":
		return &PushResult{WasSuccessful: false}, nil
	default:
		return nil, fmt.Errorf("snapshot: unknown push response code %q", pr.Code)
	}
}

// doGET / doPOST: shared HTTP plumbing with status + body capture (Java
// Request.performGetRequest/performPostRequest + getResult).
func doGET(api *NetSnapshotApi, url string) (body []byte, sc int, err error) {
	client := &http.Client{Timeout: 3 * time.Minute}
	resp, e := client.Get(url)
	if e != nil {
		return nil, 0, e
	}
	defer resp.Body.Close()
	b, e := io.ReadAll(resp.Body)
	if e != nil {
		return nil, 0, e
	}
	return b, resp.StatusCode, nil
}

func doPOST(api *NetSnapshotApi, url string, body []byte) (respBody []byte, sc int, err error) {
	client := &http.Client{Timeout: 3 * time.Minute}
	req, e := http.NewRequest("POST", url, bytes.NewReader(body))
	if e != nil {
		return nil, 0, e
	}
	req.Header.Set("Content-Type", "application/json")
	resp, e := client.Do(req)
	if e != nil {
		return nil, 0, e
	}
	defer resp.Body.Close()
	rb, e := io.ReadAll(resp.Body)
	if e != nil {
		return nil, 0, e
	}
	return rb, resp.StatusCode, nil
}

// statusError ports the status mapping in Java Request.getResult, including
// the 404 (message/newRemote/newUrl) and 409 (code) body handling.
func statusError(sc int, body []byte) error {
	switch sc {
	case 401, 403:
		return &giterrors.ForbiddenException{}
	case 429:
		return &giterrors.MissingRepositoryException{DescriptionLines: []string{
			"Rate-limit exceeded. Please wait a while and try again.",
			"",
			"If this is unexpected, please contact us at support@overleaf.com, or",
			"see https://www.overleaf.com/learn/how-to/Git_integration for more information.",
		}}
	case 409:
		return conflictError(body)
	case 404:
		return notFoundError(body)
	default:
		if sc >= 400 && sc < 500 {
			return giterrors.GenericMissingRepositoryException()
		}
		return &giterrors.FailedConnectionException{}
	}
}

// conflictError ports the 409 branch of Java Request.getResult.
func conflictError(body []byte) error {
	var o struct {
		Code string `json:"code"`
	}
	if json.Unmarshal(body, &o) == nil && o.Code == "projectHasDotGit" {
		return &giterrors.MissingRepositoryException{DescriptionLines: []string{
			"Git access won't work when a project contains a folder named '.git'.",
			"Please remove any folder named '.git' from your project in Overleaf and try again.",
		}}
	}
	return &giterrors.MissingRepositoryException{DescriptionLines: []string{"Conflict: 409"}}
}

// notFoundError ports the 404 branch of Java Request.getResult.
func notFoundError(body []byte) error {
	var o struct {
		Message   *string `json:"message"`
		NewRemote *string `json:"newRemote"`
		NewUrl    *string `json:"newUrl"`
	}
	if json.Unmarshal(body, &o) == nil && o.Message != nil {
		switch *o.Message {
		case "Exported to v2":
			return &giterrors.MissingRepositoryException{DescriptionLines: giterrors.ExportV2Message(ptrStr(o.NewRemote))}
		case "Overleaf v1 is Deprecated":
			return &giterrors.MissingRepositoryException{DescriptionLines: giterrors.DeprecatedMessage(ptrStr(o.NewUrl))}
		}
	}
	return &giterrors.MissingRepositoryException{DescriptionLines: giterrors.GenReason()}
}

func ptrStr(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
