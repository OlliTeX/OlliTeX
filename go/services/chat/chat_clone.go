package chat

import (
	"context"
	"net/http"
	"ollitex/go/pbhttp"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

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
