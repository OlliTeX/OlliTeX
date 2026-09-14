package chat

import (
	"fmt"
	"io"
	"net/http"
	"ollitex/go/pbhttp"
	"sort"
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// ---- client-side shapes (Node: MessageFormatter) -----------------------------

// clientMessage mirrors formatMessageForClientSide: {_id → id hex}, and only
// id/content/timestamp/user_id(+edited_at) are emitted.
type clientMessage struct {
	ID        string `json:"id"`
	Content   any    `json:"content"`
	Timestamp int64  `json:"timestamp"`
	UserID    string `json:"user_id"`
	EditedAt  *int64 `json:"edited_at,omitempty"`
}

// sendResponse is the 201 body: formatted message + room_id (= the RAW
// projectId path param, exactly as the Node `message.room_id = projectId`).

// sendResponse is the 201 body: formatted message + room_id (= the RAW
// projectId path param, exactly as the Node `message.room_id = projectId`).
type sendResponse struct {
	ID        string `json:"id"`
	Content   any    `json:"content"`
	Timestamp int64  `json:"timestamp"`
	UserID    string `json:"user_id"`
	EditedAt  *int64 `json:"edited_at,omitempty"`
	RoomID    string `json:"room_id"`
}

// threadData mirrors the grouped thread object; field order matches Node
// ({messages:[], resolved?, resolved_at?, resolved_by_user_id?}).

// threadData mirrors the grouped thread object; field order matches Node
// ({messages:[], resolved?, resolved_at?, resolved_by_user_id?}).
type threadData struct {
	Messages   []clientMessage `json:"messages"`
	Resolved   *bool           `json:"resolved,omitempty"`
	ResolvedAt *string         `json:"resolved_at,omitempty"`
	ResolvedBy *string         `json:"resolved_by_user_id,omitempty"`
}

// formatISO: Node JSON.stringify(Date) → UTC milliseconds with Z.

// formatISO: Node JSON.stringify(Date) → UTC milliseconds with Z.
func formatISO(t time.Time) string {
	return t.UTC().Format("2006-01-02T15:04:05.000Z")
}

// jsonable converts stored (bson/native) values into pure JSON values, the
// way Node's driver hands BSON back to JSON.stringify (ObjectId → hex, etc.).

// jsonable converts stored (bson/native) values into pure JSON values, the
// way Node's driver hands BSON back to JSON.stringify (ObjectId → hex, etc.).
func jsonable(v any) any {
	switch x := v.(type) {
	case nil:
		return nil
	case primitive.ObjectID:
		return x.Hex()
	case time.Time:
		return formatISO(x)
	case []primitive.ObjectID:
		out := make([]any, 0, len(x))
		for _, id := range x {
			out = append(out, id.Hex())
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, val := range x {
			out[k] = jsonable(val)
		}
		return out
	case []any:
		out := make([]any, 0, len(x))
		for _, val := range x {
			out = append(out, jsonable(val))
		}
		return out
	}
	return v
}

// formatMessage is formatMessageForClientSide 1:1.

// formatMessage is formatMessageForClientSide 1:1.
func formatMessage(m *Message) clientMessage {
	return clientMessage{
		ID:        m.ID.Hex(),
		Content:   jsonable(m.Content),
		Timestamp: m.Timestamp,
		UserID:    m.UserID.Hex(),
		EditedAt:  m.EditedAt,
	}
}

// groupMessages mirrors groupMessagesByThreads 1:1: threads keyed by the
// room's thread_id hex (lowercase, from the stored ObjectId); messages in
// arrival order then stable-sorted ascending by timestamp; the thread's
// resolved state copied from the room.
//
// Node throws a TypeError (→ 500) if a room without thread_id ever reaches
// grouping; we return an error the dispatcher converts to the same 500.

// groupMessages mirrors groupMessagesByThreads 1:1: threads keyed by the
// room's thread_id hex (lowercase, from the stored ObjectId); messages in
// arrival order then stable-sorted ascending by timestamp; the thread's
// resolved state copied from the room.
//
// Node throws a TypeError (→ 500) if a room without thread_id ever reaches
// grouping; we return an error the dispatcher converts to the same 500.
func groupMessages(rooms []*Room, messages []*Message) (map[string]*threadData, error) {
	roomsByID := make(map[string]*Room, len(rooms))
	for _, r := range rooms {
		roomsByID[r.ID.Hex()] = r
	}
	threads := make(map[string]*threadData)
	getThread := func(room *Room) (*threadData, error) {
		if room.ThreadID == nil {
			return nil, fmt.Errorf("Cannot read properties of undefined (reading 'toString')")
		}
		key := room.ThreadID.Hex()
		if t, ok := threads[key]; ok {
			return t, nil
		}
		t := &threadData{Messages: []clientMessage{}}
		if room.Resolved != nil {
			tru := true
			t.Resolved = &tru
			ts := formatISO(room.Resolved.TS)
			t.ResolvedAt = &ts
			hex := room.Resolved.UserID.Hex()
			t.ResolvedBy = &hex
		}
		threads[key] = t
		return t, nil
	}
	for _, m := range messages {
		room, ok := roomsByID[m.RoomID.Hex()]
		if !ok {
			continue
		}
		t, err := getThread(room)
		if err != nil {
			return nil, err
		}
		t.Messages = append(t.Messages, formatMessage(m))
	}
	for _, t := range threads {
		sort.SliceStable(t.Messages, func(i, j int) bool {
			return t.Messages[i].Timestamp < t.Messages[j].Timestamp
		})
	}
	return threads, nil
}

// ---- shared helpers -----------------------------------------------------------

// ---- shared helpers -----------------------------------------------------------

func (s *Server) status(w http.ResponseWriter, r *http.Request, _ map[string]string) {
	// Node: res.send('chat is alive') → 200, text/html (observed).
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = io.WriteString(w, "chat is alive")
}

type paramSpec struct {
	raw  string
	path string
}

// checkParams collects zz.objectId path-param issues in schema order (it does
// not write — the handler classifies params+body together, like Node's
// parseReq single classified error).

// checkParams collects zz.objectId path-param issues in schema order (it does
// not write — the handler classifies params+body together, like Node's
// parseReq single classified error).
func checkParams(specs ...paramSpec) []string {
	var issues []string
	for _, sp := range specs {
		if !objectIDHex.MatchString(sp.raw) {
			issues = append(issues, objectIDIssue(sp.path))
		}
	}
	return issues
}

// finishValidation writes the single classified error, Node 1:1: any param
// issue → 404 with ALL issues (params then body, schema order); else body
// issues → 400. Returns false when the response was written.

// finishValidation writes the single classified error, Node 1:1: any param
// issue → 404 with ALL issues (params then body, schema order); else body
// issues → 400. Returns false when the response was written.
func finishValidation(w http.ResponseWriter, paramIssues, bodyIssues []string) bool {
	if len(paramIssues) > 0 {
		writeValidationError(w, http.StatusNotFound, append(append([]string{}, paramIssues...), bodyIssues...))
		return false
	}
	if len(bodyIssues) > 0 {
		writeValidationError(w, http.StatusBadRequest, bodyIssues)
		return false
	}
	return true
}

// internal writes the Node 500 handler shape:
// res.status(500).json({ message: `Internal error: ${err.message}` }).

// internal writes the Node 500 handler shape:
// res.status(500).json({ message: `Internal error: ${err.message}` }).
func (s *Server) internal(w http.ResponseWriter, err error) {
	s.logf("chat: internal error: %v", err)
	pbhttp.WriteJSON(w, http.StatusInternalServerError, struct {
		Message string `json:"message"`
	}{"Internal error: " + err.Error()})
}

// ---- body schemas --------------------------------------------------------------

// objectReceivedIssue is the single issue Node reports when a strictObject
// body schema receives a JSON array (observed).

// ---- body schemas --------------------------------------------------------------

// objectReceivedIssue is the single issue Node reports when a strictObject
// body schema receives a JSON array (observed).
const objectReceivedArray = `Invalid input: expected object, received array at "body"`

// sendBody validates { user_id: objectId, content: messageContent } (strict).
// fatal=true means the 500 body-parse response was already written.

// threadsBody validates { threads: objectId[] } (strict).
func (s *Server) threadsBody(w http.ResponseWriter, r *http.Request) (threads []string, issues []string, fatal bool) {
	bp, done := s.readBodyClassified(w, r)
	if done && bp.kind == bodyFatal {
		return nil, nil, true
	}
	if bp.kind == bodyArray {
		return nil, []string{objectReceivedArray}, false
	}
	issues = []string{}
	t, ok := threadsField(bp.fields, &issues)
	unknownBodyKeys(bp, map[string]bool{"threads": true}, &issues)
	if !ok {
		return nil, issues, false
	}
	return t, issues, false
}

// ---- globals -----------------------------------------------------------------

// getGlobalMessages: GET /project/:projectId/messages

// ---- globals -----------------------------------------------------------------

// getGlobalMessages: GET /project/:projectId/messages
func (s *Server) getGlobalMessages(w http.ResponseWriter, r *http.Request, params map[string]string) {
	pIssues := checkParams(paramSpec{params["projectId"], "params.projectId"})
	var qIssues []string
	qm := validateQuery(r, &qIssues)
	if !finishValidation(w, pIssues, qIssues) {
		return
	}
	ctx := r.Context()
	pid, _ := primitive.ObjectIDFromHex(params["projectId"])
	room, err := s.store.FindOrCreateRoom(ctx, pid, nil) // GLOBAL_THREAD room
	if err != nil {
		s.internal(w, err)
		return
	}
	msgs, err := s.store.RoomMessages(ctx, room.ID, qm.limit, qm.before)
	if err != nil {
		s.internal(w, err)
		return
	}
	out := make([]clientMessage, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, formatMessage(m))
	}
	pbhttp.WriteJSON(w, http.StatusOK, out)
}

// getGlobalMessage: GET /project/:projectId/messages/:messageId

// getGlobalMessage: GET /project/:projectId/messages/:messageId
func (s *Server) getGlobalMessage(w http.ResponseWriter, r *http.Request, params map[string]string) {
	if !finishValidation(w, checkParams(
		paramSpec{params["projectId"], "params.projectId"},
		paramSpec{params["messageId"], "params.messageId"},
	), nil) {
		return
	}
	ctx := r.Context()
	pid, _ := primitive.ObjectIDFromHex(params["projectId"])
	mid, _ := primitive.ObjectIDFromHex(params["messageId"])

	room, err := s.store.FindRoom(ctx, pid, nil) // findThread(GLOBAL)
	if err != nil {
		s.internal(w, err)
		return
	}
	if room == nil {
		sendStatus(w, http.StatusNotFound) // MissingThreadError
		return
	}
	msg, err := s.store.FetchMessage(ctx, room.ID, mid)
	if err != nil {
		s.internal(w, err)
		return
	}
	if msg == nil {
		sendStatus(w, http.StatusNotFound) // MissingMessageError
		return
	}
	pbhttp.WriteJSON(w, http.StatusOK, formatMessage(msg))
}

// sendGlobalMessage: POST /project/:projectId/messages
