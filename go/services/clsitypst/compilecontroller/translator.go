// translator.go converts the parsed (schema-shared) compile request into
// the *compilemanager.Request (Node: the RequestParser.parse response +
// `request.project_id = ...; request.user_id = ...`, then the
// compilemanager consumption).
//
// D9 typst surface vs the tex Request: no check/flags/draft/png2pdf/
// enableCheckpoint (parsed but typst-ignored, D9 — NOT carried into the
// request) and no HRW (v1: IsCompileFromHistory is parsed + IGNORED by
// the compile path; the value is echoed via BaseHistoryVersion).
package compilecontroller

import (
	"time"

	requestparser "ollitex/go/services/clsitypst/requestparser"
	rw "ollitex/go/services/clsitypst/resourcewriter"

	compilemanager "ollitex/go/services/clsitypst/compilemanager"
)

// toRequest ports the Node parse -> request field mapping.
func toRequest(parsed requestparser.ParsedResponse, projectID, userID string) *compilemanager.Request {
	return &compilemanager.Request{
		ProjectID: projectID,
		UserID:    userID,
		// D9: parsed (schema shared) but IGNORED by the compile path
		// (v1 typst has no HRW).
		IsCompileFromHistory: parsed.IsCompileFromHistory,
		MetricsOpts: compilemanager.MetricsOpts{
			Path:    parsed.MetricsOpts.Path,
			Method:  parsed.MetricsOpts.Method,
			Compile: parsed.MetricsOpts.Compile,
		},
		Compiler:  parsed.Compiler, // D9: typst-only (parser-validated).
		ImageName: str(parsed.ImageName),
		Timeout:   parsed.Timeout,
		// Node CompileManager.js L130: compileGroup: request.compileGroup
		// (the parsed compileGroup reaches the typst runner).
		CompileGroup: compileGroupOf(parsed.CompileGroup),
		// Node dead-tail parity (D9/D16): unreachable for typst (the
		// runner rethrows on failure) but the field is consumed.
		StopOnFirstError:     parsed.StopOnFirstError,
		CompileFromClsiCache: parsed.CompileFromClsiCache, // D6 (clsi-parity).
		EnablePdfCaching:     parsed.EnablePdfCaching,
		PdfCachingMinChunk:   num64(parsed.PdfCachingMinChunkSize),
		BuildID:              ptrStr(parsed.BuildID),
		// D9 echo: mirrors Node's `request.baseHistoryVersion` echo into
		// the success envelope (v1 has no HRW; the value is a plain echo).
		BaseHistoryVersion: parsed.BaseHistoryVersion,
		// The RW sync (the only v1 sync path, D3): resources verbatim.
		RWResources:      buildRWResources(parsed),
		SyncType:         str(parsed.SyncType),
		SyncState:        str(parsed.SyncState),
		RootResourcePath: parsed.RootResourcePath,
	}
}

// buildRWResources mirrors the Node resource -> resourcewriter.Request
// resource translation (Node: the resources verbatim; Go: the typed RW
// resource, content bytes + modified time where provided).
func buildRWResources(parsed requestparser.ParsedResponse) []rw.Resource {
	out := make([]rw.Resource, len(parsed.Resources))
	for i, r := range parsed.Resources {
		out[i] = rw.Resource{
			Path:        r.Path,
			URL:         str(r.URL),
			FallbackURL: str(r.FallbackURL),
			Content:     []byte(str(r.Content)),
			Modified:    modifiedTime(r.Modified),
		}
	}
	return out
}

// compileGroupOf narrows the parsed *string (Node: request.compileGroup is
// undefined when the body carries no compileGroup; the Go form "" — the
// dockerrunner treats "" as the default mount).
func compileGroupOf(cg *string) string {
	if cg == nil {
		return ""
	}
	return *cg
}

// --- narrowing helpers (requestparser already validated the input) ---------

func str(v interface{}) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func ptrStr(v interface{}) *string {
	if s, ok := v.(string); ok {
		return &s
	}
	return nil
}

func num64(v interface{}) int64 {
	switch x := v.(type) {
	case float64:
		return int64(x)
	case int:
		return int64(x)
	case int64:
		return x
	}
	return 0
}

func modifiedTime(v interface{}) *time.Time {
	if ms, ok := v.(float64); ok {
		t := time.UnixMilli(int64(ms)).UTC()
		return &t
	}
	return nil
}
