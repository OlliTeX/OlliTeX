package chat

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sort"
	"time"

	"ollitex/go/pbhttp"

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
type threadData struct {
	Messages   []clientMessage `json:"messages"`
	Resolved   *bool           `json:"resolved,omitempty"`
	ResolvedAt *string         `json:"resolved_at,omitempty"`
	ResolvedBy *string         `json:"resolved_by_user_id,omitempty"`
}

// formatISO: Node JSON.stringify(Date) → UTC milliseconds with Z.
func formatISO(t time.Time) string {
	return t.UTC().Format("2006-01-02T15:04:05.000Z")
}

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
func (s *Server) internal(w http.ResponseWriter, err error) {
	s.logf("chat: internal error: %v", err)
	pbhttp.WriteJSON(w, http.StatusInternalServerError, struct {
		Message string `json:"message"`
	}{"Internal error: " + err.Error()})
}

// ---- body schemas --------------------------------------------------------------

// objectReceivedIssue is the single issue Node reports when a strictObject
// body schema receives a JSON array (observed).
const objectReceivedArray = `Invalid input: expected object, received array at "body"`

// sendBody validates { user_id: objectId, content: messageContent } (strict).
// fatal=true means the 500 body-parse response was already written.
func (s *Server) sendBody(w http.ResponseWriter, r *http.Request) (userID, content string, issues []string, fatal bool) {
	bp, done := s.readBodyClassified(w, r)
	if done && bp.kind == bodyFatal {
		return "", "", nil, true
	}
	if bp.kind == bodyArray {
		return "", "", []string{objectReceivedArray}, false
	}
	issues = []string{}
	userID, _ = requiredObjectIDField(bp.fields, "user_id", "body.user_id", &issues)
	content, _ = contentField(bp.fields, &issues)
	unknownBodyKeys(bp, map[string]bool{"user_id": true, "content": true}, &issues)
	return userID, content, issues, false
}

// editBody validates { content, userId? } (strict).
func (s *Server) editBody(w http.ResponseWriter, r *http.Request) (content, userID string, issues []string, fatal bool) {
	bp, done := s.readBodyClassified(w, r)
	if done && bp.kind == bodyFatal {
		return "", "", nil, true
	}
	if bp.kind == bodyArray {
		return "", "", []string{objectReceivedArray}, false
	}
	issues = []string{}
	content, _ = contentField(bp.fields, &issues)
	if u, present := optionalObjectIDField(bp.fields, "userId", "body.userId", &issues); present {
		userID = u
	}
	unknownBodyKeys(bp, map[string]bool{"content": true, "userId": true}, &issues)
	return content, userID, issues, false
}

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
func (s *Server) sendGlobalMessage(w http.ResponseWriter, r *http.Request, params map[string]string) {
	pIssues := checkParams(paramSpec{params["projectId"], "params.projectId"})
	userID, content, bIssues, fatal := s.sendBody(w, r)
	if fatal {
		return
	}
	if !finishValidation(w, pIssues, bIssues) {
		return
	}
	s.sendCore(w, r, params["projectId"], nil, userID, content)
}

// sendMessage: POST /project/:projectId/thread/:threadId/messages
func (s *Server) sendMessage(w http.ResponseWriter, r *http.Request, params map[string]string) {
	pIssues := checkParams(
		paramSpec{params["projectId"], "params.projectId"},
		paramSpec{params["threadId"], "params.threadId"},
	)
	userID, content, bIssues, fatal := s.sendBody(w, r)
	if fatal {
		return
	}
	if !finishValidation(w, pIssues, bIssues) {
		return
	}
	tid, _ := primitive.ObjectIDFromHex(params["threadId"])
	s.sendCore(w, r, params["projectId"], &tid, userID, content)
}

// sendCore is the Node _sendMessage body: findOrCreateThread → createMessage →
// fire-and-forget notifications → 201 {id, content, timestamp, user_id, room_id}.
func (s *Server) sendCore(w http.ResponseWriter, r *http.Request, rawProjectID string, threadID *primitive.ObjectID, userID, content string) {
	ctx := r.Context()
	pid, _ := primitive.ObjectIDFromHex(rawProjectID)
	sender, _ := primitive.ObjectIDFromHex(userID)

	room, err := s.store.FindOrCreateRoom(ctx, pid, threadID)
	if err != nil {
		s.internal(w, err)
		return
	}
	ts := s.nowMs()
	msgID, err := s.store.InsertMessage(ctx, room.ID, sender, content, ts)
	if err != nil {
		s.internal(w, err)
		return
	}
	// Node: NotificationsManager.createThreadMessageNotifications(...).catch(log)
	// — best-effort; a failure is logged and never affects the 201.
	s.notifyThreadMessage(ctx, rawProjectID, room, msgID, sender)

	pbhttp.WriteJSON(w, http.StatusCreated, sendResponse{
		ID:        msgID.Hex(),
		Content:   content,
		Timestamp: ts,
		UserID:    sender.Hex(),
		RoomID:    rawProjectID, // Node: message.room_id = projectId (raw param)
	})
}

// deleteGlobalMessage: DELETE /project/:projectId/messages/:messageId — 204,
// no existence check (findOrCreateThread + deleteOne are no-ops).
func (s *Server) deleteGlobalMessage(w http.ResponseWriter, r *http.Request, params map[string]string) {
	if !finishValidation(w, checkParams(
		paramSpec{params["projectId"], "params.projectId"},
		paramSpec{params["messageId"], "params.messageId"},
	), nil) {
		return
	}
	ctx := r.Context()
	pid, _ := primitive.ObjectIDFromHex(params["projectId"])
	mid, _ := primitive.ObjectIDFromHex(params["messageId"])
	room, err := s.store.FindOrCreateRoom(ctx, pid, nil)
	if err != nil {
		s.internal(w, err)
		return
	}
	if err := s.store.DeleteMessage(ctx, room.ID, mid); err != nil {
		s.internal(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// editGlobalMessage: POST /project/:projectId/messages/:messageId/edit
func (s *Server) editGlobalMessage(w http.ResponseWriter, r *http.Request, params map[string]string) {
	pIssues := checkParams(
		paramSpec{params["projectId"], "params.projectId"},
		paramSpec{params["messageId"], "params.messageId"},
	)
	content, userID, bIssues, fatal := s.editBody(w, r)
	if fatal {
		return
	}
	if !finishValidation(w, pIssues, bIssues) {
		return
	}
	s.editCore(w, r, params["projectId"], nil, params["messageId"], content, userID)
}

// editMessage: POST .../thread/:threadId/messages/:messageId/edit
func (s *Server) editMessage(w http.ResponseWriter, r *http.Request, params map[string]string) {
	pIssues := checkParams(
		paramSpec{params["projectId"], "params.projectId"},
		paramSpec{params["threadId"], "params.threadId"},
		paramSpec{params["messageId"], "params.messageId"},
	)
	content, userID, bIssues, fatal := s.editBody(w, r)
	if fatal {
		return
	}
	if !finishValidation(w, pIssues, bIssues) {
		return
	}
	tid, _ := primitive.ObjectIDFromHex(params["threadId"])
	s.editCore(w, r, params["projectId"], &tid, params["messageId"], content, userID)
}

// editCore mirrors edit*Message: findOrCreateThread + updateMessage;
// modifiedCount !== 1 → res.sendStatus(404), else 204.
func (s *Server) editCore(w http.ResponseWriter, r *http.Request, rawProjectID string, threadID *primitive.ObjectID, rawMessageID, content, rawUserID string) {
	ctx := r.Context()
	pid, _ := primitive.ObjectIDFromHex(rawProjectID)
	mid, _ := primitive.ObjectIDFromHex(rawMessageID)
	room, err := s.store.FindOrCreateRoom(ctx, pid, threadID)
	if err != nil {
		s.internal(w, err)
		return
	}
	var userID *primitive.ObjectID
	if rawUserID != "" {
		u, _ := primitive.ObjectIDFromHex(rawUserID)
		userID = &u // Node: if (userId) query.user_id = new ObjectId(userId)
	}
	found, err := s.store.UpdateMessage(ctx, room.ID, mid, userID, content, s.nowMs())
	if err != nil {
		s.internal(w, err)
		return
	}
	if !found {
		sendStatus(w, http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---- threads ------------------------------------------------------------------

// getThreads: GET /project/:projectId/threads
func (s *Server) getThreads(w http.ResponseWriter, r *http.Request, params map[string]string) {
	if !finishValidation(w, checkParams(paramSpec{params["projectId"], "params.projectId"}), nil) {
		return
	}
	ctx := r.Context()
	pid, _ := primitive.ObjectIDFromHex(params["projectId"])
	rooms, err := s.store.ThreadRooms(ctx, pid)
	if err != nil {
		s.internal(w, err)
		return
	}
	out, err := s.threadsPayload(ctx, rooms)
	if err != nil {
		s.internal(w, err)
		return
	}
	pbhttp.WriteJSON(w, http.StatusOK, out)
}

// generateThreadData: POST /project/:projectId/generate-thread-data
func (s *Server) generateThreadData(w http.ResponseWriter, r *http.Request, params map[string]string) {
	pIssues := checkParams(paramSpec{params["projectId"], "params.projectId"})
	threads, bIssues, fatal := s.threadsBody(w, r)
	if fatal {
		return
	}
	if !finishValidation(w, pIssues, bIssues) {
		return
	}
	ctx := r.Context()
	pid, _ := primitive.ObjectIDFromHex(params["projectId"])
	ids := make([]primitive.ObjectID, 0, len(threads))
	for _, t := range threads {
		id, _ := primitive.ObjectIDFromHex(t)
		ids = append(ids, id)
	}
	rooms, err := s.store.RoomsByThreadIDs(ctx, pid, ids)
	if err != nil {
		s.internal(w, err)
		return
	}
	out, err := s.threadsPayload(ctx, rooms)
	if err != nil {
		s.internal(w, err)
		return
	}
	pbhttp.WriteJSON(w, http.StatusOK, out)
}

// threadsPayload: rooms → messages → groupMessagesByThreads (Node 1:1).
func (s *Server) threadsPayload(ctx context.Context, rooms []*Room) (map[string]*threadData, error) {
	roomIDs := make([]primitive.ObjectID, 0, len(rooms))
	for _, r := range rooms {
		roomIDs = append(roomIDs, r.ID)
	}
	msgs, err := s.store.MessagesInRooms(ctx, roomIDs)
	if err != nil {
		return nil, err
	}
	return groupMessages(rooms, msgs)
}

// getThread: GET /project/:projectId/thread/:threadId
func (s *Server) getThread(w http.ResponseWriter, r *http.Request, params map[string]string) {
	if !finishValidation(w, checkParams(
		paramSpec{params["projectId"], "params.projectId"},
		paramSpec{params["threadId"], "params.threadId"},
	), nil) {
		return
	}
	ctx := r.Context()
	pid, _ := primitive.ObjectIDFromHex(params["projectId"])
	tid, _ := primitive.ObjectIDFromHex(params["threadId"])
	room, err := s.store.FindRoom(ctx, pid, &tid) // findThread
	if err != nil {
		s.internal(w, err)
		return
	}
	if room == nil {
		sendStatus(w, http.StatusNotFound) // MissingThreadError
		return
	}
	msgs, err := s.store.MessagesInRooms(ctx, []primitive.ObjectID{room.ID})
	if err != nil {
		s.internal(w, err)
		return
	}
	threads, err := groupMessages([]*Room{room}, msgs)
	if err != nil {
		s.internal(w, err)
		return
	}
	// Node: threads[threadId] looks up the RAW param string against the
	// lowercase stored-hex keys — case-different params therefore 404.
	thread, ok := threads[params["threadId"]]
	if !ok {
		sendStatus(w, http.StatusNotFound)
		return
	}
	pbhttp.WriteJSON(w, http.StatusOK, thread)
}

// deleteThread: DELETE /project/:projectId/thread/:threadId — findOrCreate,
// delete room + its messages, 204.
func (s *Server) deleteThread(w http.ResponseWriter, r *http.Request, params map[string]string) {
	if !finishValidation(w, checkParams(
		paramSpec{params["projectId"], "params.projectId"},
		paramSpec{params["threadId"], "params.threadId"},
	), nil) {
		return
	}
	ctx := r.Context()
	pid, _ := primitive.ObjectIDFromHex(params["projectId"])
	tid, _ := primitive.ObjectIDFromHex(params["threadId"])
	room, err := s.store.FindOrCreateRoom(ctx, pid, &tid)
	if err != nil {
		s.internal(w, err)
		return
	}
	if err := s.store.DeleteRoom(ctx, room.ID); err != nil {
		s.internal(w, err)
		return
	}
	if err := s.store.DeleteMessagesInRoom(ctx, room.ID); err != nil {
		s.internal(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// getResolvedThreadIds: GET /project/:projectId/resolved-thread-ids
func (s *Server) getResolvedThreadIds(w http.ResponseWriter, r *http.Request, params map[string]string) {
	if !finishValidation(w, checkParams(paramSpec{params["projectId"], "params.projectId"}), nil) {
		return
	}
	ctx := r.Context()
	pid, _ := primitive.ObjectIDFromHex(params["projectId"])
	ids, err := s.store.ResolvedThreadIDs(ctx, pid)
	if err != nil {
		s.internal(w, err)
		return
	}
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, id.Hex())
	}
	pbhttp.WriteJSON(w, http.StatusOK, struct {
		ResolvedThreadIDs []string `json:"resolvedThreadIds"`
	}{out})
}

// destroyProject: DELETE /project/:projectId — messages (all rooms incl.
// global) then rooms, 204.
func (s *Server) destroyProject(w http.ResponseWriter, r *http.Request, params map[string]string) {
	if !finishValidation(w, checkParams(paramSpec{params["projectId"], "params.projectId"}), nil) {
		return
	}
	ctx := r.Context()
	pid, _ := primitive.ObjectIDFromHex(params["projectId"])
	rooms, err := s.store.AllRooms(ctx, pid)
	if err != nil {
		s.internal(w, err)
		return
	}
	ids := make([]primitive.ObjectID, 0, len(rooms))
	for _, r := range rooms {
		ids = append(ids, r.ID)
	}
	if err := s.store.DeleteMessagesInRooms(ctx, ids); err != nil {
		s.internal(w, err)
		return
	}
	if err := s.store.DeleteRoomsByProject(ctx, pid); err != nil {
		s.internal(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---- thread messages -----------------------------------------------------------

// getThreadMessage: GET .../thread/:threadId/messages/:messageId
func (s *Server) getThreadMessage(w http.ResponseWriter, r *http.Request, params map[string]string) {
	if !finishValidation(w, checkParams(
		paramSpec{params["projectId"], "params.projectId"},
		paramSpec{params["threadId"], "params.threadId"},
		paramSpec{params["messageId"], "params.messageId"},
	), nil) {
		return
	}
	ctx := r.Context()
	pid, _ := primitive.ObjectIDFromHex(params["projectId"])
	tid, _ := primitive.ObjectIDFromHex(params["threadId"])
	mid, _ := primitive.ObjectIDFromHex(params["messageId"])
	room, err := s.store.FindRoom(ctx, pid, &tid)
	if err != nil {
		s.internal(w, err)
		return
	}
	if room == nil {
		sendStatus(w, http.StatusNotFound)
		return
	}
	msg, err := s.store.FetchMessage(ctx, room.ID, mid)
	if err != nil {
		s.internal(w, err)
		return
	}
	if msg == nil {
		sendStatus(w, http.StatusNotFound)
		return
	}
	pbhttp.WriteJSON(w, http.StatusOK, formatMessage(msg))
}

// deleteMessage: DELETE .../thread/:threadId/messages/:messageId — 204, no
// existence check.
func (s *Server) deleteMessage(w http.ResponseWriter, r *http.Request, params map[string]string) {
	if !finishValidation(w, checkParams(
		paramSpec{params["projectId"], "params.projectId"},
		paramSpec{params["threadId"], "params.threadId"},
		paramSpec{params["messageId"], "params.messageId"},
	), nil) {
		return
	}
	ctx := r.Context()
	pid, _ := primitive.ObjectIDFromHex(params["projectId"])
	tid, _ := primitive.ObjectIDFromHex(params["threadId"])
	mid, _ := primitive.ObjectIDFromHex(params["messageId"])
	room, err := s.store.FindOrCreateRoom(ctx, pid, &tid)
	if err != nil {
		s.internal(w, err)
		return
	}
	if err := s.store.DeleteMessage(ctx, room.ID, mid); err != nil {
		s.internal(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// deleteUserMessage: DELETE .../thread/:threadId/user/:userId/messages/:messageId
// (params schema order: projectId, threadId, messageId, userId — the zod
// .extend() chain order).
func (s *Server) deleteUserMessage(w http.ResponseWriter, r *http.Request, params map[string]string) {
	if !finishValidation(w, checkParams(
		paramSpec{params["projectId"], "params.projectId"},
		paramSpec{params["threadId"], "params.threadId"},
		paramSpec{params["messageId"], "params.messageId"},
		paramSpec{params["userId"], "params.userId"},
	), nil) {
		return
	}
	ctx := r.Context()
	pid, _ := primitive.ObjectIDFromHex(params["projectId"])
	tid, _ := primitive.ObjectIDFromHex(params["threadId"])
	uid, _ := primitive.ObjectIDFromHex(params["userId"])
	mid, _ := primitive.ObjectIDFromHex(params["messageId"])
	room, err := s.store.FindOrCreateRoom(ctx, pid, &tid)
	if err != nil {
		s.internal(w, err)
		return
	}
	if err := s.store.DeleteUserMessage(ctx, uid, room.ID, mid); err != nil {
		s.internal(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// resolveThread: POST .../thread/:threadId/resolve {user_id} — 204 (no
// existence check — the Node updateOne is a no-op on a missing room).
func (s *Server) resolveThread(w http.ResponseWriter, r *http.Request, params map[string]string) {
	pIssues := checkParams(
		paramSpec{params["projectId"], "params.projectId"},
		paramSpec{params["threadId"], "params.threadId"},
	)
	bp, done := s.readBodyClassified(w, r)
	if done && bp.kind == bodyFatal {
		return
	}
	issues := []string{}
	var userID string
	if bp.kind == bodyArray {
		issues = append(issues, objectReceivedArray)
	} else {
		userID, _ = requiredObjectIDField(bp.fields, "user_id", "body.user_id", &issues)
		unknownBodyKeys(bp, map[string]bool{"user_id": true}, &issues)
	}
	if !finishValidation(w, pIssues, issues) {
		return
	}
	ctx := r.Context()
	pid, _ := primitive.ObjectIDFromHex(params["projectId"])
	tid, _ := primitive.ObjectIDFromHex(params["threadId"])
	uid, _ := primitive.ObjectIDFromHex(userID)
	if err := s.store.ResolveThread(ctx, pid, tid, uid); err != nil {
		s.internal(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// reopenThread: POST .../thread/:threadId/reopen — 204 (no existence check).
func (s *Server) reopenThread(w http.ResponseWriter, r *http.Request, params map[string]string) {
	if !finishValidation(w, checkParams(
		paramSpec{params["projectId"], "params.projectId"},
		paramSpec{params["threadId"], "params.threadId"},
	), nil) {
		return
	}
	ctx := r.Context()
	pid, _ := primitive.ObjectIDFromHex(params["projectId"])
	tid, _ := primitive.ObjectIDFromHex(params["threadId"])
	if err := s.store.ReopenThread(ctx, pid, tid); err != nil {
		s.internal(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---- cross-project --------------------------------------------------------------

type dupResult struct {
	DuplicateID *string `json:"duplicateId,omitempty"`
	Error       *string `json:"error,omitempty"`
}

func dupNotFound() dupResult {
	str := "not found"
	return dupResult{Error: &str}
}

func dupUnknown() dupResult {
	str := "unknown"
	return dupResult{Error: &str}
}

// duplicateCommentThreads: POST /project/:projectId/duplicate-comment-threads
// {threads: [id...]} — per-id {duplicateId} | {error:'not found'|'unknown'},
// 200 {newThreads}.
func (s *Server) duplicateCommentThreads(w http.ResponseWriter, r *http.Request, params map[string]string) {
	pIssues := checkParams(paramSpec{params["projectId"], "params.projectId"})
	threads, bIssues, fatal := s.threadsBody(w, r)
	if fatal {
		return
	}
	if !finishValidation(w, pIssues, bIssues) {
		return
	}
	ctx := r.Context()
	pid, _ := primitive.ObjectIDFromHex(params["projectId"])
	result := map[string]dupResult{}
	for _, raw := range threads {
		tid, _ := primitive.ObjectIDFromHex(raw)
		old, err := s.store.FindRoom(ctx, pid, &tid)
		if err != nil {
			result[raw] = dupUnknown()
			continue
		}
		if old == nil {
			result[raw] = dupNotFound()
			continue
		}
		dupID, copyErr := s.duplicateRoomCopy(ctx, old)
		if copyErr != nil {
			// Node: non-MissingThreadError → {error:'unknown'} (logged)
			s.logf("chat: error duplicating thread: %v", copyErr)
			result[raw] = dupUnknown()
			continue
		}
		result[raw] = dupResult{DuplicateID: &dupID}
	}
	pbhttp.WriteJSON(w, http.StatusOK, struct {
		NewThreads map[string]dupResult `json:"newThreads"`
	}{result})
}

// cloneCommentThreads: POST /project/:projectId/clone-comment-threads
// {targetProjectId} — cloneThreads (room copies, same thread_id, new _id) and
// message copies, 204.
func (s *Server) cloneCommentThreads(w http.ResponseWriter, r *http.Request, params map[string]string) {
	pIssues := checkParams(paramSpec{params["projectId"], "params.projectId"})
	bp, done := s.readBodyClassified(w, r)
	if done && bp.kind == bodyFatal {
		return
	}
	issues := []string{}
	var target string
	if bp.kind == bodyArray {
		issues = append(issues, objectReceivedArray)
	} else {
		target, _ = requiredObjectIDField(bp.fields, "targetProjectId", "body.targetProjectId", &issues)
		unknownBodyKeys(bp, map[string]bool{"targetProjectId": true}, &issues)
	}
	if !finishValidation(w, pIssues, issues) {
		return
	}
	s.cloneCore(w, r, params["projectId"], target)
}

func (s *Server) cloneCore(w http.ResponseWriter, r *http.Request, rawSource, rawTarget string) {
	ctx := r.Context()
	src, _ := primitive.ObjectIDFromHex(rawSource)
	dst, _ := primitive.ObjectIDFromHex(rawTarget)

	// ThreadManager.cloneThreads: source rooms with thread_id present (natural
	// order) → insert copies {...room, _id: new, project_id: target},
	// collecting {from, to}.
	rooms, err := s.store.ThreadRooms(ctx, src)
	if err != nil {
		s.internal(w, err)
		return
	}
	var pairs []Pair
	for _, room := range rooms {
		to := primitive.NewObjectID()
		clone := &Room{
			ID:        to,
			ProjectID: dst,
			ThreadID:  room.ThreadID, // Node: {...room} keeps the same thread_id
			Resolved:  room.Resolved, // Node: {...room} keeps resolved when present
		}
		if err := s.store.InsertRoom(ctx, clone); err != nil {
			s.internal(w, err)
			return
		}
		pairs = append(pairs, Pair{From: room.ID, To: to})
	}
	// promiseMapWithLimit(10, ...): duplicateRoomToOtherRoom(from, to)
	for _, p := range pairs {
		if err := s.copyRoomMessagesTo(ctx, p.From, p.To); err != nil {
			s.internal(w, err)
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

// duplicateRoomCopy mirrors ThreadManager.duplicateThread +
// MessageManager.duplicateRoomToOtherRoom: insert the duplicate room (new id +
// new thread_id, resolved copied) and copy the source room's messages without
// edited_at. Returns the new room's thread_id hex (the Node duplicateId).
func (s *Server) duplicateRoomCopy(ctx context.Context, old *Room) (string, error) {
	newThread := primitive.NewObjectID()
	var resolvedCopy *Resolved
	if old.Resolved != nil {
		resolvedCopy = &Resolved{UserID: old.Resolved.UserID, TS: old.Resolved.TS}
	}
	room := &Room{
		ID:        primitive.NewObjectID(),
		ProjectID: old.ProjectID, // Node: project_id: room.project_id (same project)
		ThreadID:  &newThread,
		Resolved:  resolvedCopy,
	}
	if err := s.store.InsertRoom(ctx, room); err != nil {
		return "", err
	}
	if err := s.copyRoomMessagesTo(ctx, old.ID, room.ID); err != nil {
		return "", err
	}
	return newThread.Hex(), nil
}

// copyRoomMessagesTo mirrors duplicateRoomToOtherRoom: copy the source room's
// messages (natural order) to the target, dropping _id and edited_at. A source
// with no messages is a no-op (Node early return).
func (s *Server) copyRoomMessagesTo(ctx context.Context, from, to primitive.ObjectID) error {
	msgs, err := s.store.MessagesInRooms(ctx, []primitive.ObjectID{from})
	if err != nil {
		return err
	}
	if len(msgs) == 0 {
		return nil
	}
	for _, m := range msgs {
		if _, err := s.store.InsertMessage(ctx, to, m.UserID, m.Content, m.Timestamp); err != nil {
			return err
		}
	}
	return nil
}
