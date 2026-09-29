package projectinspection

// request pipeline (reference parity):
//
//	rate limit (6/min/project) -> authz read gate -> body validation ->
//	snapshot build (docs + files + bibs) -> worker exec -> result envelope
//
// error contract (reference ProjectInspectionErrors.mjs, pinned):
//
//	400 {"error":"INVALID_ENTRY_POINT","message":...}
//	413 {"error":"PROJECT_TOO_LARGE","message":...}
//	504 {"error":"ANALYSIS_TIMEOUT","message":...}
//	499 {"error":"ANALYSIS_CANCELLED","message":...}
//	500 {"error":"PROJECT_INSPECTION_ERROR","message":...}

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"ollitex/go/services/web/core"
)

var (
	pj         = `[a-fA-F0-9]{24}`
	analyzePat = regexp.MustCompile(
		`^/project/(?P<1>` + pj + `)/(?i:project-inspection)/(?i:analyze)/?$`)
	validRootExt = regexp.MustCompile(`\.(tex|typ)$`)
	bibExt       = regexp.MustCompile(`\.bib$`)
	hex24        = regexp.MustCompile(`^[a-fA-F0-9]{24}$`)
)

// Feature is the core.Feature registration (cmd/web: app.RegisterFeature).
func Feature(a *core.App) core.Feature {
	s := newSvc(a)
	return core.Feature{
		Name: "project-inspection",
		Routes: []core.Route{
			{Method: "POST", Pattern: analyzePat, Handler: s.analyze},
		},
	}
}

// ---- config (env + defaults — reference names kept 1:1) ------------------

type cfg struct {
	NodeBin       string
	Worker        string
	Timeout       time.Duration
	MaxSource     int64
	MaxBib        int64
	MaxTotalBib   int64
	DocstoreBase  string
	V1HistoryBase string
	V1HistoryUser string
	V1HistoryPass string
	MaxEntities   int64
}

func getenv(key, dflt string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return dflt
}

func envInt64(key string, dflt int64) int64 {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
			return n
		}
	}
	return dflt
}

func cfgFromEnv() cfg {
	return cfg{
		NodeBin:       getenv("PROJECT_INSPECTION_NODE_BIN", "node"),
		Worker: getenv("PROJECT_INSPECTION_WORKER",
			"/overleaf/services/web/modules/project-inspection/dist/analyze-worker.cjs"),
		Timeout:     time.Duration(envInt64("PROJECT_INSPECTION_TIMEOUT_MS", 30_000)) * time.Millisecond,
		MaxSource:   envInt64("PROJECT_INSPECTION_MAX_SOURCE_BYTES", 25<<20),
		MaxBib:      envInt64("PROJECT_INSPECTION_MAX_BIB_BYTES", 6<<20),
		MaxTotalBib: envInt64("PROJECT_INSPECTION_MAX_TOTAL_BIB_BYTES", 25<<20),
		DocstoreBase: strings.TrimSuffix(
			getenv("WEB_DOCSTORE_URL", "http://127.0.0.1:3016"), "/"),
		V1HistoryBase: strings.TrimSuffix(
			getenv("V1_HISTORY_URL", "http://127.0.0.1:3050"), "/"),
		V1HistoryUser: os.Getenv("V1_HISTORY_USER"),
		V1HistoryPass: os.Getenv("V1_HISTORY_PASSWORD"),
		MaxEntities:   envInt64("MAX_ENTITIES_PER_PROJECT", 2000),
	}
}

// piError carries the reference error contract (code + HTTP status).
type piError struct {
	Code    string
	Message string
	Status  int
}

func (e *piError) Error() string { return e.Code + ": " + e.Message }

func errInvalidEntry(msg string) *piError {
	if msg == "" {
		msg = "One or more entry points are invalid"
	}
	return &piError{"INVALID_ENTRY_POINT", msg, http.StatusBadRequest}
}

func errTooLarge(msg string) *piError {
	if msg == "" {
		msg = "The project is too large to analyze safely"
	}
	return &piError{"PROJECT_TOO_LARGE", msg, http.StatusRequestEntityTooLarge}
}

var (
	errTimeout   = &piError{"ANALYSIS_TIMEOUT", "Project inspection timed out", http.StatusGatewayTimeout}
	errCancelled = &piError{"ANALYSIS_CANCELLED", "Project inspection was cancelled", 499}
	errInternal  = func(msg string) *piError {
		if msg == "" {
			msg = "Project inspection failed"
		}
		return &piError{"PROJECT_INSPECTION_ERROR", msg, http.StatusInternalServerError}
	}
)

func writePIError(res *core.Res, err error) {
	var pe *piError
	if !errors.As(err, &pe) {
		pe = errInternal(err.Error())
	}
	res.JSON(pe.Status, []byte(fmt.Sprintf(
		`{"error":%q,"message":%q}`, pe.Code, pe.Message)))
}
