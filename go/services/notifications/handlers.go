package notifications

import (
	"io"
	"net/http"
	"os"
	"runtime/debug"
	"strconv"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

func getRealEnv(k string) string { return os.Getenv(k) }

func itoa(i int) string { return strconv.Itoa(i) }

// validObjectID 1:1 with validation-tools zz.objectId → ObjectId.isValid on a
// string input: a 24-character hexadecimal string.
func validObjectID(s string) bool {
	if len(s) != 24 {
		return false
	}
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') {
			return false
		}
	}
	return true
}

func mustObjectID(s string) primitive.ObjectID {
	id, _ := primitive.ObjectIDFromHex(s)
	return id
}

// ---- JSON serialisation (Node ⇄ Go document compatibility) -----------------
//
// The Go store returns driver-shaped values: primitive.ObjectID, time.Time
// (BSON dates), primitive.M (objects), primitive.D / bson.A / []any (arrays).
// Node's express res.json() renders ObjectId→hex string, Date→ISO-8601 string.
// sanitiseForJSON reproduces that so a document read by the Go service is
// byte-equivalent (as JSON) to one read by the Node service.
func sanitiseForJSON(v any) any {
	switch t := v.(type) {
	case primitive.ObjectID:
		return t.Hex()
	case time.Time:
		// JS toISOString() is always millisecond precision, UTC, 'Z' suffix.
		return t.UTC().Truncate(time.Millisecond).Format("2006-01-02T15:04:05.000Z")
	case primitive.M:
		out := make(map[string]any, len(t))
		for k, val := range t {
			out[k] = sanitiseForJSON(val)
		}
		return out
	case primitive.D:
		seen := map[string]bool{}
		out := make(map[string]any, len(t))
		for _, el := range t {
			k := el.Key
			out[k] = sanitiseForJSON(el.Value)
			seen[k] = true
		}
		return out
	case bson.A:
		out := make([]any, len(t))
		for i, val := range t {
			out[i] = sanitiseForJSON(val)
		}
		return out
	case []interface{}:
		out := make([]any, len(t))
		for i, val := range t {
			out[i] = sanitiseForJSON(val)
		}
		return out
	case map[string]interface{}:
		out := make(map[string]any, len(t))
		for k, val := range t {
			out[k] = sanitiseForJSON(val)
		}
		return out
	case int32:
		return int64(t)
	case int16:
		return int64(t)
	case int8:
		return int64(t)
	case uint64:
		return t
	default:
		return t
	}
}

func sanitiseDoc(d primitive.M) map[string]any {
	return sanitiseForJSON(d).(map[string]any)
}

// ---- endpoint handlers (1:1 with NotificationsController.ts) ---------------

// GET /status  →  res.send('notifications is up')
func (s *Server) status(w http.ResponseWriter, _ *http.Request, _ map[string]string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("notifications is up"))
}

// POST /user/:user_id  →  addNotification → res.sendStatus(200/500)
func (s *Server) addNotification(w http.ResponseWriter, r *http.Request, params map[string]string) {
	userID := params["user_id"]
	if !validObjectID(userID) {
		jsonValidationError(w, http.StatusNotFound, "user_id", "invalid Mongo ObjectId")
		return
	}
	var body struct {
		Key         *string        `json:"key"`
		TemplateKey *string        `json:"templateKey"`
		MessageOpts map[string]any `json:"messageOpts"`
		ForceCreate *bool          `json:"forceCreate"`
		Expires     *string        `json:"expires"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	if body.Key == nil {
		jsonValidationError(w, http.StatusBadRequest, "key", "required")
		return
	}
	if body.TemplateKey == nil {
		jsonValidationError(w, http.StatusBadRequest, "templateKey", "required")
		return
	}
	forceCreate := body.ForceCreate != nil && *body.ForceCreate

	ctx := r.Context()
	count, err := s.store.CountByUserKey(ctx, mustObjectID(userID), *body.Key)
	if err != nil {
		slogf("addNotification count error", err)
		sendStatus(w, http.StatusInternalServerError)
		return
	}
	// Node: if (count !== 0 && !notification.forceCreate) return   (still 200)
	if count != 0 && !forceCreate {
		sendStatus(w, http.StatusOK)
		return
	}

	setDoc := primitive.M{
		"user_id":     mustObjectID(userID),
		"key":         *body.Key,
		"templateKey": *body.TemplateKey,
	}
	if body.MessageOpts != nil {
		setDoc["messageOpts"] = body.MessageOpts
	}
	if body.Expires != nil {
		t, perr := time.Parse(time.RFC3339, *body.Expires)
		if perr != nil {
			// Node: new Date(expires).toISOString() throws → 500
			slogf("addNotification invalid expires", perr)
			sendStatus(w, http.StatusInternalServerError)
			return
		}
		setDoc["expires"] = t
	}
	if uperr := s.store.Upsert(ctx, primitive.M{"user_id": mustObjectID(userID), "key": *body.Key}, setDoc); uperr != nil {
		slogf("addNotification upsert error", uperr)
		sendStatus(w, http.StatusInternalServerError)
		return
	}
	sendStatus(w, http.StatusOK)
}

// GET /user/:user_id  →  getUserNotifications → res.json([docs])
func (s *Server) getUserNotifications(w http.ResponseWriter, r *http.Request, params map[string]string) {
	userID := params["user_id"]
	if !validObjectID(userID) {
		jsonValidationError(w, http.StatusNotFound, "user_id", "invalid Mongo ObjectId")
		return
	}
	docs, err := s.store.GetUserNotifications(r.Context(), mustObjectID(userID))
	if err != nil {
		slogf("getUserNotifications error", err)
		sendStatus(w, http.StatusInternalServerError)
		return
	}
	out := make([]any, 0, len(docs))
	for _, d := range docs {
		out = append(out, sanitiseDoc(d))
	}
	writeJSONStatus(w, http.StatusOK, out)
}

// DELETE /user/:user_id/notification/:notification_id  →  removeNotificationId → 200
func (s *Server) removeNotificationId(w http.ResponseWriter, r *http.Request, params map[string]string) {
	userID := params["user_id"]
	nID := params["notification_id"]
	if !validObjectID(userID) || !validObjectID(nID) {
		jsonValidationError(w, http.StatusNotFound, "objectId param", "invalid Mongo ObjectId")
		return
	}
	if err := s.store.UnsetByID(r.Context(), mustObjectID(userID), mustObjectID(nID)); err != nil {
		slogf("removeNotificationId error", err)
		sendStatus(w, http.StatusInternalServerError)
		return
	}
	sendStatus(w, http.StatusOK)
}

// DELETE /user/:user_id  (body {key})  →  removeNotificationKey → 200
func (s *Server) removeNotificationKey(w http.ResponseWriter, r *http.Request, params map[string]string) {
	userID := params["user_id"]
	if !validObjectID(userID) {
		jsonValidationError(w, http.StatusNotFound, "user_id", "invalid Mongo ObjectId")
		return
	}
	var body struct {
		Key *string `json:"key"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	if body.Key == nil {
		jsonValidationError(w, http.StatusBadRequest, "key", "required")
		return
	}
	if err := s.store.UnsetByUserKey(r.Context(), mustObjectID(userID), *body.Key); err != nil {
		slogf("removeNotificationKey error", err)
		sendStatus(w, http.StatusInternalServerError)
		return
	}
	sendStatus(w, http.StatusOK)
}

// DELETE /key/:key  →  removeNotificationByKeyOnly → 200
func (s *Server) removeNotificationByKeyOnly(w http.ResponseWriter, r *http.Request, params map[string]string) {
	key := params["key"]
	if err := s.store.UnsetByKeyOnly(r.Context(), key); err != nil {
		slogf("removeNotificationByKeyOnly error", err)
		sendStatus(w, http.StatusInternalServerError)
		return
	}
	sendStatus(w, http.StatusOK)
}

// GET /key/:key/count  →  countNotificationsByKeyOnly → res.json({count})
func (s *Server) countNotificationsByKeyOnly(w http.ResponseWriter, r *http.Request, params map[string]string) {
	key := params["key"]
	count, err := s.store.CountByKeyOnly(r.Context(), key)
	if err != nil {
		slogf("countNotificationsByKeyOnly error", err)
		sendStatus(w, http.StatusInternalServerError)
		return
	}
	writeJSONStatus(w, http.StatusOK, map[string]any{"count": count})
}

// DELETE /key/:key/bulk  →  deleteUnreadNotificationsByKeyOnlyBulk → res.json({count})
func (s *Server) deleteUnreadNotificationsByKeyOnlyBulk(w http.ResponseWriter, r *http.Request, params map[string]string) {
	key := params["key"]
	count, err := s.store.DeleteManyByKeyOnly(r.Context(), key)
	if err != nil {
		slogf("deleteUnreadNotificationsByKeyOnlyBulk error", err)
		sendStatus(w, http.StatusInternalServerError)
		return
	}
	writeJSONStatus(w, http.StatusOK, map[string]any{"count": count})
}

// slogf is a minimal, panic-safe structured sink (the Node service logs via
// @overleaf/logger). It never affects the response.
func slogf(msg string, err error) {
	if err == nil {
		return
	}
	io.WriteString(os.Stderr, "\x1b[31mnotifications\x1b[0m "+msg+": "+err.Error()+"\r\n")
	_ = debug.Stack()
}
