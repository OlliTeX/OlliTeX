// postback.go — ports PostbackHandler.java + PostbackContents.java (the
// Overleaf-app push postback: POST /api/<project>/<postbackKey>/postback).
//
// Java wire (PostbackHandler.handle, /api context; pathInContext used):
//
//	method != POST || !target.endsWith("postback") → fall-through (false).
//	else (CT set FIRST — it survives onto the 500 grids below, live-verified
//	pb3/pb_unknown = 500 + "Content-Type: application/json"):
//	  1. contents = body string                 (IOException → 500 grid)
//	  2. parts = target.split("/") (Java: trailing empties dropped);
//	     parts.length < 4 → ServletException → 500 grid
//	  3. projectName = parts[1]; postbackKey = parts[2]
//	  4. PostbackContents (constructor runs fromJSON):
//	       code = "code" field (missing/null → "");
//	       code == "upToDate"  → versionID = "latestVerId" int
//	                   (missing key → NPE, non-number → NumberFormatException
//	                    → RuntimeException → 500 grid)
//	       else builder: "outOfDate"/"invalidFiles"/"invalidProject"/"error"
//	                    → the four SnapshotPostExceptions;
//	                    any other code → UnexpectedPostbackException
//	                    (ctor wraps it in RuntimeException → 500 grid, NOT 409)
//	  5. processPostback:
//	       promise unknown for the project → UnexpectedPostbackException
//	           → 409 {"code":"unexpectedPostback"}\n   (live: pb_nop, pb_noperr)
//	       else promise fulfilled → 200 {"code":"success"}\n
//
// All 500s are the ProductionErrorHandler 28B grid body
// `{"message":"HTTP error 500"}` with CT application/json (set in step 0).

package server

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"ollitex/go/services/gitbridge/giterrors"
	"ollitex/go/services/gitbridge/util"
	"ollitex/go/services/gitbridge/wglog"
)

// handlePostback ports PostbackHandler.handle. Returns true when the request
// was handled (a response was written); false → the next /api step runs.
func (s *Server) handlePostback(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, "postback") {
		return false
	}
	// Java puts the CT header BEFORE the parse/branches: the 409 / 200 and
	// every 500 grid below all carry application/json.
	w.Header().Set("Content-Type", "application/json")

	body, err := io.ReadAll(r.Body)
	if err != nil {
		return postbackError500(w, "IOException when handling postback to target: "+err.Error())
	}

	// parts = pathInContext.split("/"). Java getPathInContext is the path
	// WITHOUT the "/api" context mount (live: "/api/<P>/postback" → the grid
	// 500 via len<4; "/api/<P>/<key>/postback" → parts[1]/[2] project/key).
	contextPath := strings.TrimPrefix(r.URL.Path, "/api")
	parts := util.SplitURIPath(contextPath)
	if len(parts) < 4 {
		// Java: `throw new ServletException()` (no message) → Jetty 500
		// grid (CT preserved; live: pb3 → 500 28B application/json).
		return postbackError500(w, "ServletException when handling postback to target (path has < 4 segments)")
	}
	projectName := parts[1]
	postbackKey := parts[2]
	wglog.Debug("PostbackHandler: %s <- %s", r.Method, r.URL.RequestURI())

	// PostbackContents: `new Gson().fromJson(contents, JsonElement.class)`
	// then .getAsJsonObject() — unparsable / non-object body → NPE or
	// IllegalStateException → propagates (no matching catch) → Jetty 500.
	var obj map[string]interface{}
	if err := json.Unmarshal(body, &obj); err != nil {
		return postbackError500(w, "JsonSyntaxException when handling postback to target: "+err.Error())
	}
	code, _ := obj["code"].(string) // Java getCodeFromResponse: missing/null → ""
	var versionID int
	var exception error
	switch code {
	case "upToDate":
		// Java: responseObject.get("latestVerId").getAsInt() — missing key →
		// NPE, non-number → NumberFormatException → 500 grid.
		v, ok := obj["latestVerId"].(float64)
		if !ok {
			return postbackError500(w, "latestVerId missing or non-numeric")
		}
		versionID = int(v)
	case "outOfDate":
		exception = &giterrors.OutOfDateException{}
	case "invalidFiles":
		exception = giterrors.NewInvalidFilesException(body)
	case "invalidProject":
		exception = giterrors.NewInvalidProjectException(body)
	case "error":
		exception = &giterrors.UnexpectedErrorException{}
	default:
		// Java: SnapshotPostExceptionBuilder.build throws
		// UnexpectedPostbackException for any other code; the ctor wraps it
		// in RuntimeException → re-thrown → Jetty 500 (NOT the 409 below;
		// live: pb_unknown {"code":"bogus"} → 500 28B CT json).
		return postbackError500(w, "RuntimeException when handling postback to target (unknown code "+code+")")
	}

	var postErr error
	if exception == nil {
		postErr = s.br.PostbackReceivedSuccessfully(projectName, postbackKey, versionID)
	} else {
		postErr = s.br.PostbackReceivedWithException(projectName, postbackKey, exception)
	}
	var upe giterrors.UnexpectedPostbackException
	if errors.As(postErr, &upe) {
		// No promise for this project (or no matching project) → 409 with
		// body `body + "\n"` (compact Gson JSON + newline; live pb_nop =
		// 30B `{"code":"unexpectedPostback"}\n`).
		w.WriteHeader(http.StatusConflict)
		w.Write([]byte(`{"code":"unexpectedPostback"}` + "\n"))
		return true
	}
	if postErr != nil {
		return postbackError500(w, postErr.Error())
	}
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"code":"success"}` + "\n"))
	return true
}

// postbackError500 ports the Jetty-exception path of PostbackHandler: the
// exception propagates to the Jetty handler → ProductionErrorHandler 500 grid
// (28B body, no Pragma) — and the application/json CT set before parsing is
// preserved on the wire (live: pb3, pb_unknown).
func postbackError500(w http.ResponseWriter, reason string) (handled bool) {
	wglog.Warn("PostbackHandler 500: %s", reason)
	w.WriteHeader(http.StatusInternalServerError)
	w.Write(productionErrorBody(500))
	return true
}
