package chat

import (
	"net/http"
	"ollitex/go/pbhttp"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

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
