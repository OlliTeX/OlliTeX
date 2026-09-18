package trackchanges

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"ollitex/go/services/web/core"
)

// ---- downstream service bases (Node settings parity) ------------------------

func tcHostOr(env, def string) string {
	h := os.Getenv(env)
	if h == "" {
		h = def
	}
	return "http://" + h
}

func tcChatBase() string {
	if v := os.Getenv("WEB_CHAT_URL"); v != "" {
		return v
	}
	return tcHostOr("CHAT_HOST", "127.0.0.1") + ":3010"
}

func tcDuBase() string {
	if v := os.Getenv("WEB_DOCUPDATER_URL"); v != "" {
		return v
	}
	h := os.Getenv("DOCUPDATER_HOST")
	if h == "" {
		h = os.Getenv("DOCUMENT_UPDATER_HOST")
	}
	if h == "" {
		h = "127.0.0.1"
	}
	return "http://" + h + ":3003"
}

func tcDocstoreBase() string {
	if v := os.Getenv("WEB_DOCSTORE_URL"); v != "" {
		return v
	}
	return tcHostOr("DOCSTORE_HOST", "127.0.0.1") + ":3016"
}

// tcCall — one downstream request; ok=false for transport errors AND non-2xx
// (both map to Node's next(err) → rendered 500 page). 5s timeout ≈ Node
// REQUEST_TIMEOUT_MS default.
func tcCall(ctx context.Context, method, url, body string) ([]byte, bool) {
	client := &http.Client{Timeout: 5 * time.Second}
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, rd)
	if err != nil {
		return nil, false
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, false
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, false
	}
	return b, true
}

// ---- users (getPersonalInfo + formatPersonalInfo parity) ---------------------
//
// Node: getPersonalInfo = users.findOne({_id}, {_id,first_name,last_name,email});
// formatPersonalInfo = {id, first_name?, last_name?, email?} with falsy
// values OMITTED (Node: if (user[key])), null user → {}.

func tcPersonalJSON(ctx context.Context, a *core.App, uidHex string) (string, bool) {
	if a == nil || a.Mongo == nil {
		return "", false
	}
	uidHex = strings.ToLower(strings.TrimSpace(uidHex))
	if !tcHex24.MatchString(uidHex) {
		return "{}", true // Node: getUser(null-ish/invalid) → null → {}
	}
	oid, err := primitive.ObjectIDFromHex(uidHex)
	if err != nil {
		return "{}", true
	}
	db, err := a.Mongo.DB(ctx)
	if err != nil {
		return "", false
	}
	var d bson.D
	err = db.Collection("users").FindOne(ctx,
		bson.D{{Key: "_id", Value: oid}},
		options.FindOne().SetProjection(bson.D{
			{Key: "_id", Value: 1},
			{Key: "first_name", Value: 1},
			{Key: "last_name", Value: 1},
			{Key: "email", Value: 1},
		})).Decode(&d)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return "{}", true
		}
		return "", false
	}
	var b strings.Builder
	b.WriteString(`{"id":"`)
	b.WriteString(oid.Hex())
	b.WriteByte('"')
	get := func(key string) string {
		for _, e := range d {
			if e.Key == key {
				if s, ok := e.Value.(string); ok {
					return s
				}
			}
		}
		return ""
	}
	add := func(key, val string) {
		if val == "" {
			return // falsy → omitted
		}
		s, _ := json.Marshal(val)
		b.WriteString(`,"` + key + `":`)
		b.Write(s)
	}
	add("first_name", get("first_name"))
	add("last_name", get("last_name"))
	add("email", get("email"))
	b.WriteByte('}')
	return b.String(), true
}

// ---- editor-events room publish ----------------------------------------------

var (
	tcEventCounter uint64
	tcEventID      = func() string {
		b := make([]byte, 4)
		_, _ = rand.Read(b)
		return fmt.Sprintf("%x", b)
	}()
)

// tcEmitRoom — EditorRealTimeController.emitToRoom: channel `editor-events`
// (publishOnIndividualChannels unset in this deployment); blob
// {room_id,message,payload,_id} — key order as Node's JSON.stringify.
func tcEmitRoom(a *core.App, roomID, message string, payload []interface{}) {
	if a == nil || a.Redis == nil {
		return
	}
	n := atomic.AddUint64(&tcEventCounter, 1)
	host, _ := os.Hostname()
	if host == "" {
		host = "web"
	}
	pl, _ := json.Marshal(payload)
	blob, _ := json.Marshal(struct {
		RoomID  string          `json:"room_id"`
		Message string          `json:"message"`
		Payload json.RawMessage `json:"payload"`
		ID      string          `json:"_id"`
	}{roomID, message, pl, "web:" + host + ":" + tcEventID + "-" + fmt.Sprint(n)})
	_ = a.Redis.Publish("editor-events", string(blob))
}

// ---- rate limiters (SHARED redis keys — Node limiter names) -------------------

// tcLimitRunner — RateLimiterMiddleware contract: client = logged-in user id
// (else IP); over limit → 429 with Node's exact write() body (express
// .write() sets NO Content-Type header).
type tcLimitRunner struct {
	limReads  *core.RateLimiter // track-changes-reads  60/min
	limWrites *core.RateLimiter // track-changes-writes 20/min
}

func (r *tcLimitRunner) clientID(cxt *core.Cxt) string {
	if cxt != nil && cxt.Sess != nil {
		if uid := cxt.Sess.UserIDHex(); uid != "" {
			return uid
		}
	}
	if cxt != nil {
		return core.ClientIP(cxt.Req)
	}
	return "unknown"
}

func (r *tcLimitRunner) reads(cxt *core.Cxt, res *core.Res) bool {
	if r == nil || r.limReads == nil {
		return true
	}
	if !r.limReads.Consume(r.clientID(cxt)) {
		core.Send429(res, "Rate limit reached, please try again later")
		return false
	}
	return true
}

func (r *tcLimitRunner) writes(cxt *core.Cxt, res *core.Res) bool {
	if r == nil || r.limWrites == nil {
		return true
	}
	if !r.limWrites.Consume(r.clientID(cxt)) {
		core.Send429(res, "Rate limit reached, please try again later")
		return false
	}
	return true
}
