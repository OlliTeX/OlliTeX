// translator.go projects a requestparser.ParsedResponse onto the typed
// compilemanager.Request. Node mutates the parsed object in place
// (project_id/user_id) and hands it to CompileManager.doCompileWithLock;
// the Go controller does the same projection once per request.
//
// HRW vs RW (Node: isCompileFromHistory):
//
//   - HRW (rawChangeOperations present): *histwriter.Request carries
//     RawSnapshot/GlobalBlobs/RawChangeOperations/BaseHistoryVersion (the
//     same JSON shape requestparser decoded, just typed).
//   - RW (otherwise): []resourcewriter.Resource built from parsed.Resources.
//
// requestparser already type-checked / defaulted everything it validates
// (the Go form of the Node attribute/type validation); this file only
// narrows interface{} to the compilemanager's typed surface. The HRW raw
// snapshot/blobs/change-ops fields are decoded JSON, so they are
// re-marshalled into a stable JSON key (no type-assert of arbitrary
// interface{} shapes) — mirroring Node, where the values are plain JSON.
package compilecontroller

import (
	"encoding/json"
	"time"

	histwriter "ollitex/go/services/clsitex/historyresourcewriter"
	"ollitex/go/services/clsitex/requestparser"
	"ollitex/go/services/clsitex/resourcewriter"

	compilemanager "ollitex/go/services/clsitex/compilemanager"
)

// toRequest projects a parsed compile body onto the compile request.
// projectID/userID mirror Node's `request.project_id = params.project_id`
// and `if (params.user_id != null) request.user_id = params.user_id`.
func toRequest(parsed requestparser.ParsedResponse, projectID, userID string) *compilemanager.Request {
	req := &compilemanager.Request{
		ProjectID:            projectID,
		UserID:               userID,
		IsCompileFromHistory: parsed.IsCompileFromHistory,
		MetricsOpts: compilemanager.MetricsOpts{
			Path:    parsed.MetricsOpts.Path,
			Method:  parsed.MetricsOpts.Method,
			Compile: parsed.MetricsOpts.Compile,
		},
		Compiler:         parsed.Compiler,
		ImageName:        str(parsed.ImageName),
		Timeout:          parsed.Timeout,
		Flags:            flagsOf(parsed.Flags),
		StopOnFirstError: parsed.StopOnFirstError,
		Check:            strPtr(parsed.Check),
		Draft:            parsed.Draft,
		Png2pdf:          parsed.Png2pdf,
		CompileGroup:     strPtrNil(parsed.CompileGroup),
		SyncType:         str(parsed.SyncType),
		SyncState:        str(parsed.SyncState),
		RootResourcePath: parsed.RootResourcePath,
		EnableCheckpoint: parsed.EnableCheckpoint,
		EnablePdfCaching: parsed.EnablePdfCaching,
		// EditID is a dead field (never read); Node's request carries
		// editorId separately, consumed by the controller (notify).
		EditID:               nil,
		BuildID:              ptrStr(parsed.BuildID),
		CompileFromClsiCache: parsed.CompileFromClsiCache,
		PdfCachingMinChunk:   num64(parsed.PdfCachingMinChunkSize),
	}
	if parsed.IsCompileFromHistory {
		req.HRW = buildHRW(parsed)
	} else {
		req.RWResources = buildRWResources(parsed)
	}
	return req
}

// buildHRW carries the history sub-request (Node: the same request object
// drives HistoryResourceWriter.promises.syncResourcesToDisk).
func buildHRW(parsed requestparser.ParsedResponse) *histwriter.Request {
	return &histwriter.Request{
		BaseHistoryVersion:  int(num64(parsed.BaseHistoryVersion)),
		RawSnapshot:         snapshotOf(parsed.RawSnapshot),
		GlobalBlobs:         globalBlobsOf(parsed.GlobalBlobs),
		RawChangeOperations: rawChangeOpsOf(parsed.RawChangeOperations),
		PopulateClsiCache:   parsed.PopulateClsiCache,
		Png2pdf:             parsed.Png2pdf,
		HistoryID:           str(parsed.HistoryID),
		FilestoreBlobPrefix: str(parsed.FilestoreBlobPrefix),
		ClSIPerfVariant:     str(parsed.ClSIPerfVariant),
		Draft:               parsed.Draft,
		RootResourcePath:    parsed.RootResourcePath,
		CompileGroup:        strPtrNil(parsed.CompileGroup),
		MetricsPath:         parsed.MetricsOpts.Path,
	}
}

// buildRWResources carries the resource list (Node: request.resources,
// each {path, modified?, url?, fallbackURL?, content?}).
func buildRWResources(parsed requestparser.ParsedResponse) []resourcewriter.Resource {
	out := make([]resourcewriter.Resource, len(parsed.Resources))
	for i, r := range parsed.Resources {
		out[i] = resourcewriter.Resource{
			Path:        r.Path,
			URL:         str(r.URL),
			FallbackURL: str(r.FallbackURL),
			Content:     []byte(str(r.Content)),
			Modified:    modifiedTime(r.Modified),
		}
	}
	return out
}

// --- narrowing helpers (requestparser already validated the input) -------------

func str(v interface{}) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func strPtr(v interface{}) *string {
	if s, ok := v.(string); ok {
		return &s
	}
	return nil
}

func strPtrNil(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func ptrStr(v interface{}) *string {
	if s, ok := v.(string); ok {
		return &s
	}
	return nil
}

func flagsOf(v interface{}) []string {
	out := []string{}
	if arr, ok := v.([]interface{}); ok {
		for _, x := range arr {
			if s, ok := x.(string); ok {
				out = append(out, s)
			}
		}
	}
	return out
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

// modifiedTime ports the _parseResource modified->Date coercion: the
// requestparser normalized to epoch MS (float64); the RW resource takes
// a *time.Time (absent when the field was missing).
func modifiedTime(v interface{}) *time.Time {
	if ms, ok := v.(float64); ok {
		t := time.UnixMilli(int64(ms)).UTC()
		return &t
	}
	return nil
}

// snapshotOf/globalBlobsOf/rawChangeOpsOf re-marshal decoded JSON into the
// HRW typed surface (requestparser stored the raw-decoded interface{}).
func snapshotOf(v interface{}) map[string]any {
	if v == nil {
		return nil
	}
	var m map[string]any
	if e := jsonRound(v, &m); e != nil {
		return nil
	}
	return m
}

func globalBlobsOf(v interface{}) []string {
	if v == nil {
		return nil
	}
	var out []string
	if e := jsonRound(v, &out); e != nil {
		return nil
	}
	return out
}

func rawChangeOpsOf(v interface{}) [][]map[string]any {
	if v == nil {
		return nil
	}
	var out [][]map[string]any
	if e := jsonRound(v, &out); e != nil {
		return nil
	}
	return out
}

func jsonRound(v, out interface{}) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, out)
}
