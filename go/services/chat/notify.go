package chat

import (
	"context"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// DEFAULT_NOTIFICATION_PREFERENCES (Node: NotificationsManager) — all true.
var defaultPreferences = []string{
	"commentOnOwnProject",
	"commentOnInvitedProject",
	"repliesOnAuthoredThread",
	"repliesOnParticipatingThread",
	"commentResolvedOnAuthoredThread",
	"commentResolvedOnParticipatingThread",
	"commentReopenedOnAuthoredThread",
	"commentReopenedOnParticipatingThread",
	"trackedChangesOnOwnProject",
	"trackedChangesOnInvitedProject",
	"trackChangesAcceptedOnAuthoredChange",
	"trackChangesRejectedOnAuthoredChange",
}

// prefBool mirrors _normalizePreferences: key missing → true, else Boolean(v).
func prefBool(raw map[string]any, key string) bool {
	v, ok := raw[key]
	if !ok {
		return true
	}
	if b, ok := v.(bool); ok {
		return b
	}
	return false // JS Boolean(non-bool non-undefined) — only booleans occur in practice;
	// keep the conservative reading (undefined → true handled above).
}

// senderName mirrors _loadSenderName:
// [first_name, last_name].filter(Boolean).join(' ') || email || 'Someone'.
func senderName(u *UserNames) string {
	if u == nil {
		return "Someone"
	}
	parts := []string{}
	if u.FirstName != nil && *u.FirstName != "" {
		parts = append(parts, *u.FirstName)
	}
	if u.LastName != nil && *u.LastName != "" {
		parts = append(parts, *u.LastName)
	}
	names := ""
	for i, p := range parts {
		if i > 0 {
			names += " "
		}
		names += p
	}
	if names != "" {
		return names
	}
	if u.Email != nil && *u.Email != "" {
		return *u.Email
	}
	return "Someone"
}

// notifyThreadMessage mirrors createThreadMessageNotifications 1:1 — the
// fire-and-forget batch scheduled after a persisted chat message. It is
// best-effort: every lookup failure is logged and the already-determined 201
// response is unaffected (Node: .catch(log)).
//
// (The Node guard `if (thread.thread_id === GLOBAL_THREAD) return` is a dead
// branch for real room documents — thread_id is an ObjectId or absent, never
// the string 'GLOBAL' — so the fan-out actually runs for global threads too.
// We mirror the behaviour: no early return.)
func (s *Server) notifyThreadMessage(ctx context.Context, rawProjectID string, thread *Room, messageID primitive.ObjectID, senderID primitive.ObjectID) {
	s.notify(ctx, rawProjectID, thread, messageID, senderID)
}

func (s *Server) notify(ctx context.Context, rawProjectID string, thread *Room, messageID primitive.ObjectID, senderID primitive.ObjectID) {
	projectID, _ := primitive.ObjectIDFromHex(rawProjectID)
	senderHex := senderID.Hex()

	project, err := s.store.ProjectRefs(ctx, projectID)
	if err != nil {
		s.logf("chat: notification lookup failed (project): %v", err)
		return
	}
	if project == nil {
		s.logf("chat: project %s not found for notification creation", projectID.Hex())
		return
	}

	// projectUserIds: [owner, ...collaborators, ...readOnly, ...tokenAccessRW,
	// ...tokenAccessRO] filtered Boolean, deduped, sender excluded, JS Set
	// (insertion) order.
	var projectUserIDs []primitive.ObjectID
	seen := map[string]bool{}
	addUser := func(id *primitive.ObjectID) {
		if id == nil {
			return
		}
		hex := id.Hex()
		if seen[hex] || hex == senderHex {
			return
		}
		seen[hex] = true
		projectUserIDs = append(projectUserIDs, *id)
	}
	addUser(project.Owner)
	for i := range project.Collab {
		addUser(&project.Collab[i])
	}
	for i := range project.ReadOnly {
		addUser(&project.ReadOnly[i])
	}
	for i := range project.AccessRW {
		addUser(&project.AccessRW[i])
	}
	for i := range project.AccessRO {
		addUser(&project.AccessRO[i])
	}
	if len(projectUserIDs) == 0 {
		return
	}

	prefs, err := s.store.ProjectPrefs(ctx, projectID, projectUserIDs)
	if err != nil {
		s.logf("chat: notification lookup failed (prefs): %v", err)
		return
	}
	muted, err := s.store.MutedUserIDs(ctx, projectUserIDs)
	if err != nil {
		s.logf("chat: notification lookup failed (muted): %v", err)
		return
	}
	sender, err := s.store.UserNames(ctx, senderID)
	if err != nil {
		s.logf("chat: notification lookup failed (sender): %v", err)
		return
	}

	total, err := s.store.CountMessages(ctx, thread.ID)
	if err != nil {
		s.logf("chat: notification lookup failed (count): %v", err)
		return
	}
	isComment := total == 1 // includes the just-created message
	templateKey := "notification_reply_on_project"
	if isComment {
		templateKey = "notification_comment_on_project"
	}

	var threadAuthorHex string
	if !isComment {
		first, err := s.store.FirstMessage(ctx, thread.ID)
		if err != nil {
			s.logf("chat: notification lookup failed (author): %v", err)
			return
		}
		if first != nil {
			threadAuthorHex = first.UserID.Hex()
		}
	}

	participants := map[string]bool{}
	users, err := s.store.DistinctMessageUsers(ctx, thread.ID)
	if err != nil {
		s.logf("chat: notification lookup failed (participants): %v", err)
		return
	}
	for _, u := range users {
		participants[u.Hex()] = true
	}

	ownerHex := ""
	if project.Owner != nil {
		ownerHex = project.Owner.Hex()
	}

	var recipients []primitive.ObjectID
	for _, rid := range projectUserIDs {
		rhex := rid.Hex()
		if muted[rhex] {
			continue
		}
		prefRaw, _ := prefs[rhex]
		isOwner := rhex == ownerHex
		notify := false
		if isComment {
			if isOwner {
				notify = prefBool(prefRaw, "commentOnOwnProject")
			} else {
				notify = prefBool(prefRaw, "commentOnInvitedProject")
			}
		} else {
			isThreadAuthor := rhex == threadAuthorHex
			isParticipant := participants[rhex]
			notify = (isThreadAuthor && prefBool(prefRaw, "repliesOnAuthoredThread")) ||
				(!isThreadAuthor && isParticipant && prefBool(prefRaw, "repliesOnParticipatingThread"))
		}
		if notify {
			recipients = append(recipients, rid)
		}
	}
	if len(recipients) == 0 {
		return
	}

	projectName := "project"
	if project.Name != nil && *project.Name != "" {
		projectName = *project.Name
	}
	prefix := "project-reply"
	if isComment {
		prefix = "project-comment"
	}
	threadIDString := ""
	if thread.ThreadID != nil {
		threadIDString = thread.ThreadID.Hex() // Node: thread.thread_id?.toString() || ''
	}
	messageOpts := map[string]any{
		"projectId":   rawProjectID, // Node: projectId.toString() (raw param hex)
		"projectName": projectName,
		"userName":    senderName(sender),
		"threadId":    threadIDString,
	}
	for _, rid := range recipients {
		key := prefix + "-" + rawProjectID + "-" + threadIDString + "-" + messageID.Hex()
		if err := s.store.UpsertNotification(ctx, rid, key, messageOpts, templateKey); err != nil {
			s.logf("chat: notification upsert failed: %v", err)
			return
		}
	}

	emailOpts := map[string]any{
		"projectId":   rawProjectID,
		"projectName": projectName,
		"userName":    senderName(sender),
		"threadId":    threadIDString,
		"isComment":   isComment,
	}
	for _, rid := range recipients {
		if err := s.store.UpsertEmailNotification(ctx, rid, projectID, emailOpts); err != nil {
			s.logf("chat: email notification upsert failed: %v", err)
			return
		}
	}
}
