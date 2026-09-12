package chat

import (
	"context"
	"sort"
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// memStore is a deterministic in-memory Store mirroring the Mongo-backed
// semantics used by the handlers (natural order, upsert, no-match ≠ error),
// so the HTTP contract tests exercise the real handler logic.
type memStore struct {
	mu            chan struct{} // simple serialization (tests are single-goroutine)
	rooms         []*Room
	messages      []*Message
	projects      map[string]*ProjectRefs
	users         map[string]*UserNames
	prefs         []map[string]any
	notifications []map[string]any // accumulated upserts
	emailNotifs   []map[string]any
}

func newMemStore() *memStore {
	return &memStore{
		mu:       make(chan struct{}, 1),
		projects: map[string]*ProjectRefs{},
		users:    map[string]*UserNames{},
	}
}

func (m *memStore) roomByID(id primitive.ObjectID) *Room {
	for _, r := range m.rooms {
		if r.ID == id {
			return r
		}
	}
	return nil
}

func (m *memStore) FindOrCreateRoom(ctx context.Context, projectID primitive.ObjectID, threadID *primitive.ObjectID) (*Room, error) {
	m.mu <- struct{}{}
	defer func() { <-m.mu }()
	match := func(r *Room) bool {
		if r.ProjectID != projectID {
			return false
		}
		if threadID == nil {
			return r.ThreadID == nil
		}
		return r.ThreadID != nil && *r.ThreadID == *threadID
	}
	for _, r := range m.rooms {
		if match(r) {
			return r, nil
		}
	}
	room := &Room{
		ID:        primitive.NewObjectID(),
		ProjectID: projectID,
		ThreadID:  threadID,
	}
	m.rooms = append(m.rooms, room)
	return room, nil
}

func (m *memStore) FindRoom(ctx context.Context, projectID primitive.ObjectID, threadID *primitive.ObjectID) (*Room, error) {
	room, _ := m.FindOrCreateRoomLocked(projectID, threadID)
	return room, nil
}

// FindOrCreateRoomLocked is FindOrCreateRoom without creating (pure lookup),
// used by FindRoom.
func (m *memStore) FindOrCreateRoomLocked(projectID primitive.ObjectID, threadID *primitive.ObjectID) (*Room, bool) {
	m.mu <- struct{}{}
	defer func() { <-m.mu }()
	match := func(r *Room) bool {
		if r.ProjectID != projectID {
			return false
		}
		if threadID == nil {
			return r.ThreadID == nil
		}
		return r.ThreadID != nil && *r.ThreadID == *threadID
	}
	for _, r := range m.rooms {
		if match(r) {
			return r, true
		}
	}
	return nil, false
}

func (m *memStore) ThreadRooms(ctx context.Context, projectID primitive.ObjectID) ([]*Room, error) {
	var out []*Room
	for _, r := range m.rooms {
		if r.ProjectID == projectID && r.ThreadID != nil {
			out = append(out, r)
		}
	}
	return out, nil
}

func (m *memStore) AllRooms(ctx context.Context, projectID primitive.ObjectID) ([]*Room, error) {
	var out []*Room
	for _, r := range m.rooms {
		if r.ProjectID == projectID {
			out = append(out, r)
		}
	}
	return out, nil
}

func (m *memStore) RoomsByThreadIDs(ctx context.Context, projectID primitive.ObjectID, threadIDs []primitive.ObjectID) ([]*Room, error) {
	set := map[string]bool{}
	for _, id := range threadIDs {
		set[id.Hex()] = true
	}
	var out []*Room
	for _, r := range m.rooms {
		if r.ProjectID == projectID && r.ThreadID != nil && set[r.ThreadID.Hex()] {
			out = append(out, r)
		}
	}
	return out, nil
}

func (m *memStore) ResolveThread(ctx context.Context, projectID, threadID, userID primitive.ObjectID) error {
	room, _ := m.FindOrCreateRoomLocked(projectID, &threadID)
	if room == nil {
		return nil // no-match updateOne: not an error in Node either
	}
	room.Resolved = &Resolved{UserID: userID, TS: time.Now()}
	return nil
}

func (m *memStore) ReopenThread(ctx context.Context, projectID, threadID primitive.ObjectID) error {
	room, _ := m.FindOrCreateRoomLocked(projectID, &threadID)
	if room == nil {
		return nil
	}
	room.Resolved = nil
	return nil
}

func (m *memStore) DeleteRoom(ctx context.Context, id primitive.ObjectID) error {
	for i, r := range m.rooms {
		if r.ID == id {
			m.rooms = append(m.rooms[:i], m.rooms[i+1:]...)
			return nil
		}
	}
	return nil
}

func (m *memStore) DeleteRoomsByProject(ctx context.Context, projectID primitive.ObjectID) error {
	var kept []*Room
	for _, r := range m.rooms {
		if r.ProjectID != projectID {
			kept = append(kept, r)
		}
	}
	m.rooms = kept
	return nil
}

func (m *memStore) InsertRoom(ctx context.Context, room *Room) error {
	m.mu <- struct{}{}
	defer func() { <-m.mu }()
	m.rooms = append(m.rooms, room)
	return nil
}

func (m *memStore) ResolvedThreadIDs(ctx context.Context, projectID primitive.ObjectID) ([]primitive.ObjectID, error) {
	var out []primitive.ObjectID
	for _, r := range m.rooms {
		if r.ProjectID == projectID && r.ThreadID != nil && r.Resolved != nil {
			out = append(out, *r.ThreadID)
		}
	}
	return out, nil
}

func (m *memStore) FetchMessage(ctx context.Context, roomID, msgID primitive.ObjectID) (*Message, error) {
	for _, msg := range m.messages {
		if msg.ID == msgID && msg.RoomID == roomID {
			return msg, nil
		}
	}
	return nil, nil
}

func (m *memStore) InsertMessage(ctx context.Context, roomID, userID primitive.ObjectID, content any, ts int64) (primitive.ObjectID, error) {
	m.mu <- struct{}{}
	defer func() { <-m.mu }()
	id := primitive.NewObjectID()
	m.messages = append(m.messages, &Message{
		ID:        id,
		Content:   content,
		RoomID:    roomID,
		UserID:    userID,
		Timestamp: ts,
	})
	return id, nil
}

func (m *memStore) RoomMessages(ctx context.Context, roomID primitive.ObjectID, limit int, before int64) ([]*Message, error) {
	var out []*Message
	for _, msg := range m.messages {
		if msg.RoomID == roomID {
			if before != 0 && msg.Timestamp >= before {
				continue
			}
			out = append(out, msg)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Timestamp > out[j].Timestamp })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func (m *memStore) MessagesInRooms(ctx context.Context, roomIDs []primitive.ObjectID) ([]*Message, error) {
	set := map[string]bool{}
	for _, id := range roomIDs {
		set[id.Hex()] = true
	}
	var out []*Message
	for _, msg := range m.messages {
		if set[msg.RoomID.Hex()] {
			out = append(out, msg)
		}
	}
	return out, nil
}

func (m *memStore) UpdateMessage(ctx context.Context, roomID, msgID primitive.ObjectID, userID *primitive.ObjectID, content any, ts int64) (bool, error) {
	for _, msg := range m.messages {
		if msg.ID == msgID && msg.RoomID == roomID {
			if userID != nil && msg.UserID != *userID {
				return false, nil
			}
			msg.Content = content
			msg.EditedAt = &ts
			return true, nil
		}
	}
	return false, nil
}

func (m *memStore) DeleteMessage(ctx context.Context, roomID, msgID primitive.ObjectID) error {
	for i, msg := range m.messages {
		if msg.ID == msgID && msg.RoomID == roomID {
			m.messages = append(m.messages[:i], m.messages[i+1:]...)
			return nil
		}
	}
	return nil
}

func (m *memStore) DeleteUserMessage(ctx context.Context, userID, roomID, msgID primitive.ObjectID) error {
	for i, msg := range m.messages {
		if msg.ID == msgID && msg.UserID == userID && msg.RoomID == roomID {
			m.messages = append(m.messages[:i], m.messages[i+1:]...)
			return nil
		}
	}
	return nil
}

func (m *memStore) DeleteMessagesInRoom(ctx context.Context, roomID primitive.ObjectID) error {
	var kept []*Message
	for _, msg := range m.messages {
		if msg.RoomID != roomID {
			kept = append(kept, msg)
		}
	}
	m.messages = kept
	return nil
}

func (m *memStore) DeleteMessagesInRooms(ctx context.Context, roomIDs []primitive.ObjectID) error {
	set := map[string]bool{}
	for _, id := range roomIDs {
		set[id.Hex()] = true
	}
	var kept []*Message
	for _, msg := range m.messages {
		if !set[msg.RoomID.Hex()] {
			kept = append(kept, msg)
		}
	}
	m.messages = kept
	return nil
}

func (m *memStore) CountMessages(ctx context.Context, roomID primitive.ObjectID) (int64, error) {
	var n int64
	for _, msg := range m.messages {
		if msg.RoomID == roomID {
			n++
		}
	}
	return n, nil
}

func (m *memStore) FirstMessage(ctx context.Context, roomID primitive.ObjectID) (*Message, error) {
	var best *Message
	for _, msg := range m.messages {
		if msg.RoomID != roomID {
			continue
		}
		if best == nil || msg.Timestamp < best.Timestamp {
			best = msg
		}
	}
	return best, nil
}

func (m *memStore) DistinctMessageUsers(ctx context.Context, roomID primitive.ObjectID) ([]primitive.ObjectID, error) {
	seen := map[string]bool{}
	var out []primitive.ObjectID
	for _, msg := range m.messages {
		if msg.RoomID == roomID && !seen[msg.UserID.Hex()] {
			seen[msg.UserID.Hex()] = true
			out = append(out, msg.UserID)
		}
	}
	return out, nil
}

// ---- cross-service -------------------------------------------------------------

func (m *memStore) ProjectRefs(ctx context.Context, id primitive.ObjectID) (*ProjectRefs, error) {
	return m.projects[id.Hex()], nil
}

func (m *memStore) UserNames(ctx context.Context, id primitive.ObjectID) (*UserNames, error) {
	return m.users[id.Hex()], nil
}

func (m *memStore) ProjectPrefs(ctx context.Context, projectID primitive.ObjectID, userIDs []primitive.ObjectID) (map[string]map[string]any, error) {
	want := map[string]bool{}
	for _, id := range userIDs {
		want[id.Hex()] = true
	}
	out := map[string]map[string]any{}
	for _, doc := range m.prefs {
		uid, _ := doc["user_id"].(primitive.ObjectID)
		pid, _ := doc["project_id"].(primitive.ObjectID)
		if want[uid.Hex()] && pid == projectID {
			out[uid.Hex()] = doc
		}
	}
	return out, nil
}

func (m *memStore) MutedUserIDs(ctx context.Context, userIDs []primitive.ObjectID) (map[string]bool, error) {
	want := map[string]bool{}
	for _, id := range userIDs {
		want[id.Hex()] = true
	}
	out := map[string]bool{}
	for _, doc := range m.prefs {
		uid, _ := doc["user_id"].(primitive.ObjectID)
		_, hasPID := doc["project_id"].(primitive.ObjectID)
		mute, _ := doc["muteAllNotifications"].(bool)
		if want[uid.Hex()] && (!hasPID) && mute {
			out[uid.Hex()] = true
		}
	}
	return out, nil
}

func (m *memStore) UpsertNotification(ctx context.Context, userID primitive.ObjectID, key string, messageOpts map[string]any, templateKey string) error {
	for _, n := range m.notifications {
		if id, _ := n["user_id"].(primitive.ObjectID); id == userID && n["key"] == key {
			n["messageOpts"] = messageOpts
			n["templateKey"] = templateKey
			return nil
		}
	}
	m.notifications = append(m.notifications, map[string]any{
		"user_id": userID, "key": key, "messageOpts": messageOpts, "templateKey": templateKey,
	})
	return nil
}

func (m *memStore) UpsertEmailNotification(ctx context.Context, recipient, projectID primitive.ObjectID, opts map[string]any) error {
	now := time.Now()
	for _, e := range m.emailNotifs {
		if id, _ := e["recipient_id"].(primitive.ObjectID); id == recipient {
			if pid, _ := e["project_id"].(primitive.ObjectID); pid == projectID {
				e["opts"] = opts
				e["scheduledAt"] = now
				e["updatedAt"] = now
				return nil
			}
		}
	}
	m.emailNotifs = append(m.emailNotifs, map[string]any{
		"recipient_id": recipient, "project_id": projectID, "emailType": "projectNotification",
		"opts": opts, "scheduledAt": now, "updatedAt": now,
	})
	return nil
}

var _ Store = (*memStore)(nil)
