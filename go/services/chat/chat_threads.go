package chat

import (
	"context"
	"net/http"
	"ollitex/go/pbhttp"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

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
