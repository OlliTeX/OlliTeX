package notifications

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// healthCheck 1:1 with HealthCheckController.check.
//
// Node sequence (self-HTTP against 127.0.0.1:port):
//  1. makeNotification:  POST /user/:uid {key, messageOpts:{}, templateKey:'f4g5'}   (before the try)
//  2. userHasNotification: GET /user/:uid → assert a doc {key, user_id} is present
//  3. deleteNotification:  DELETE /user/:uid/notification/:id  then  DELETE /user/:uid {key}
//  4. success → res.sendStatus(200) ("OK"); any failure → res.sendStatus(500)
//  5. finally: db.notifications.deleteOne({ user_id: uid })   (only when the try block ran)
//
// Any reachable-HTTP failure (or Mongo failure surfaced as 5xx) → 500, exactly
// matching Node.
func (s *Server) healthCheck(w http.ResponseWriter, r *http.Request, _ map[string]string) {
	ctx := r.Context()
	uid := primitive.NewObjectID().Hex()
	nKey := "smoke-test-notification-" + primitive.NewObjectID().Hex()

	// (1) makeNotification — failure here skips the try/catch/finally, → 500.
	if st, _, e := s.doJSON(ctx, http.MethodPost, "/user/"+uid, map[string]any{
		"key":         nKey,
		"messageOpts": map[string]any{},
		"templateKey": "f4g5",
	}); e != nil || st != http.StatusOK {
		sendStatus(w, http.StatusInternalServerError)
		return
	}

	// (5) finally: deleteOne({user_id}) — runs when the try block executes.
	defer func() {
		cctx, cancel := context.WithTimeout(context.Background(), s.httpClient.Timeout)
		defer cancel()
		_ = s.store.DeleteOneByUser(cctx, mustObjectID(uid))
	}()

	// (2) userHasNotification — GET then locate the doc we just created.
	st, raw, e := s.doJSON(ctx, http.MethodGet, "/user/"+uid, nil)
	if e != nil || st != http.StatusOK {
		sendStatus(w, http.StatusInternalServerError)
		return
	}
	var arr []map[string]any
	if jerr := json.Unmarshal(raw, &arr); jerr != nil {
		sendStatus(w, http.StatusInternalServerError)
		return
	}
	var doc map[string]any
	for i := range arr {
		if k, _ := arr[i]["key"].(string); k == nKey {
			if u, _ := arr[i]["user_id"].(string); u == uid {
				doc = arr[i]
				break
			}
		}
	}
	if doc == nil {
		sendStatus(w, http.StatusInternalServerError)
		return
	}
	nID, _ := doc["_id"].(string)
	dKey, _ := doc["key"].(string)

	// (3) deleteNotification — by id, then by key (both must be 200 "OK").
	if st, _, e := s.doJSON(ctx, http.MethodDelete, "/user/"+uid+"/notification/"+nID, nil); e != nil || st != http.StatusOK {
		sendStatus(w, http.StatusInternalServerError)
		return
	}
	if st, _, e := s.doJSON(ctx, http.MethodDelete, "/user/"+uid, map[string]any{"key": dKey}); e != nil || st != http.StatusOK {
		sendStatus(w, http.StatusInternalServerError)
		return
	}

	// (4) success.
	sendStatus(w, http.StatusOK)
}

// doJSON issues a self-request against the service's own HTTP endpoint using the
// configured base URL (Node hard-codes http://127.0.0.1:<port>), carrying the
// same 5s request timeout Node applies via AbortSignal.timeout(5000).
func (s *Server) doJSON(ctx context.Context, method, path string, body any) (int, []byte, error) {
	var rd io.Reader
	if body != nil {
		b, merr := json.Marshal(body)
		if merr != nil {
			return 0, nil, merr
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, s.baseURL+path, rd)
	if err != nil {
		return 0, nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := s.httpClient.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer res.Body.Close()
	data, rerr := io.ReadAll(res.Body)
	if rerr != nil {
		return res.StatusCode, nil, rerr
	}
	return res.StatusCode, data, nil
}
