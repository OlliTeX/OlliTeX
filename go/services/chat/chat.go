// Package chat is the Go 1:1 drop-in replacement for services/chat (Node,
// port 3010): the same HTTP API and the same shared MongoDB collections
// (`rooms`, `messages`) plus the cross-service reads/writes of the Node
// NotificationsManager against `projects`, `users`, `notificationsPreferences`,
// `notifications` and `emailNotifications`.
//
// Route table, status codes, response shapes, validation error strings and
// the per-service body-limit overflow behaviour are mirrored 1:1 from:
//
//	services/chat/app.js
//	services/chat/app/js/Features/Messages/MessageHttpController.js
//	services/chat/app/js/Features/Messages/MessageHttpSchemas.js
//	services/chat/app/js/Features/Messages/MessageManager.js
//	services/chat/app/js/Features/Messages/MessageFormatter.js
//	services/chat/app/js/Features/Threads/ThreadManager.js
//	services/chat/app/js/Features/Notifications/NotificationsManager.js
package chat

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

const GlobalThread = "GLOBAL" // Node: ThreadManager.GLOBAL_THREAD

// Resolved mirrors a `rooms.resolved` subdocument (ts is a bson Date).
type Resolved struct {
	UserID primitive.ObjectID `bson:"user_id"`
	TS     time.Time          `bson:"ts"`
}

// Room mirrors a document in the `rooms` collection.
//   - ThreadID is nil for the project's global room (Node stores global rooms
//     without a thread_id field).
//   - Resolved is nil when the thread is (currently) unresolved.
type Room struct {
	ID        primitive.ObjectID  `bson:"_id"`
	ProjectID primitive.ObjectID  `bson:"project_id"`
	ThreadID  *primitive.ObjectID `bson:"thread_id"`
	Resolved  *Resolved           `bson:"resolved"`
}

// Message mirrors a document in the `messages` collection. Content is kept
// generic: the Node schema constrains it to a string, but the formatter passes
// whatever is stored back to the client.
type Message struct {
	ID        primitive.ObjectID `bson:"_id"`
	RoomID    primitive.ObjectID `bson:"room_id"`
	UserID    primitive.ObjectID `bson:"user_id"`
	Content   any                `bson:"content"`
	Timestamp int64              `bson:"timestamp"` // epoch millis (Node Date.now())
	EditedAt  *int64             `bson:"edited_at,omitempty"`
}

// ProjectRefs is the projection of `projects` read by the Node
// NotificationsManager (_loadProject). NOTE the intentional `collaberator_refs`
// field name: the Node code contains that typo, and 1:1 means keeping it —
// the real `collaborator_refs` field is NOT read.
type ProjectRefs struct {
	Owner    *primitive.ObjectID  `bson:"owner_ref"`
	Collab   []primitive.ObjectID `bson:"collaberator_refs"` // Node typo, 1:1
	ReadOnly []primitive.ObjectID `bson:"readOnly_refs"`
	AccessRW []primitive.ObjectID `bson:"tokenAccessReadAndWrite_refs"`
	AccessRO []primitive.ObjectID `bson:"tokenAccessReadOnly_refs"`
	Name     *string              `bson:"name"`
}

// UserNames is the `users` projection read by _loadSenderName.
type UserNames struct {
	FirstName *string `bson:"first_name"`
	LastName  *string `bson:"last_name"`
	Email     *string `bson:"email"`
}

// Pair is a cloneThreads mapping entry {from, to} (Node source→target room ids).
type Pair struct {
	From primitive.ObjectID
	To   primitive.ObjectID
}

// Store is the data contract for the chat service. Methods mirror the Node
// ThreadManager / MessageManager / NotificationsManager calls; concrete
// implementations are the Mongo store (production) and the in-memory store
// (unit tests).
type Store interface {
	// --- rooms (Node: ThreadManager against db.rooms) ---

	// FindOrCreateRoom mirrors findOrCreateThread: findOneAndUpdate upsert with
	// returnDocument:'after'. Global room: {project_id, thread_id:{$exists:false}}
	// + $set project_id. Named thread: {project_id, thread_id} + $set both.
	FindOrCreateRoom(ctx context.Context, projectID primitive.ObjectID, threadID *primitive.ObjectID) (*Room, error)

	// FindRoom mirrors findThread (without the MissingThreadError raise).
	// Returns (nil, nil) when absent.
	FindRoom(ctx context.Context, projectID primitive.ObjectID, threadID *primitive.ObjectID) (*Room, error)

	// FetchMessage mirrors MessageManager.getMessage (without the
	// MissingMessageError raise); (nil, nil) when absent.
	FetchMessage(ctx context.Context, roomID, msgID primitive.ObjectID) (*Message, error)

	// ThreadRooms mirrors findAllThreadRooms: rooms with thread_id present,
	// natural (insertion) order.
	ThreadRooms(ctx context.Context, projectID primitive.ObjectID) ([]*Room, error)

	// AllRooms mirrors findAllThreadRoomsAndGlobalThread (project rooms, global
	// included), natural order.
	AllRooms(ctx context.Context, projectID primitive.ObjectID) ([]*Room, error)

	// RoomsByThreadIDs mirrors findThreadsById: {project_id, thread_id:{$in}}.
	RoomsByThreadIDs(ctx context.Context, projectID primitive.ObjectID, threadIDs []primitive.ObjectID) ([]*Room, error)

	// ResolveThread mirrors ThreadManager.resolveThread ($set resolved, no upsert;
	// a no-op when the room is absent — the route still returns 204).
	ResolveThread(ctx context.Context, projectID, threadID, userID primitive.ObjectID) error

	// ReopenThread mirrors ThreadManager.reopenThread ($unset resolved).
	ReopenThread(ctx context.Context, projectID, threadID primitive.ObjectID) error

	// DeleteRoom mirrors db.rooms.deleteOne({_id}).
	DeleteRoom(ctx context.Context, id primitive.ObjectID) error

	// DeleteRoomsByProject mirrors deleteAllThreadsInProject.
	DeleteRoomsByProject(ctx context.Context, projectID primitive.ObjectID) error

	// InsertRoom inserts a full room document (duplicateThread, cloneThreads).
	InsertRoom(ctx context.Context, room *Room) error

	// ResolvedThreadIDs mirrors getResolvedThreadIds (thread_id of resolved
	// rooms, natural order).
	ResolvedThreadIDs(ctx context.Context, projectID primitive.ObjectID) ([]primitive.ObjectID, error)

	// --- messages (Node: MessageManager against db.messages) ---

	// InsertMessage mirrors createMessage (insert + return the new id).
	InsertMessage(ctx context.Context, roomID, userID primitive.ObjectID, content any, ts int64) (primitive.ObjectID, error)

	// RoomMessages mirrors getMessages: {room_id[, timestamp:{$lt:before}]},
	// sort timestamp:-1, limit (before==0 means "no filter", matching the JS
	// truthiness in the Node code; limit<=0 means "no limit").
	RoomMessages(ctx context.Context, roomID primitive.ObjectID, limit int, before int64) ([]*Message, error)

	// MessagesInRooms mirrors findAllMessagesInRooms: {room_id:{$in}}, natural
	// order.
	MessagesInRooms(ctx context.Context, roomIDs []primitive.ObjectID) ([]*Message, error)

	// UpdateMessage mirrors updateMessage and returns modifiedCount == 1
	// (Node: found = res.modifiedCount === 1 → 404 otherwise).
	UpdateMessage(ctx context.Context, roomID, msgID primitive.ObjectID, userID *primitive.ObjectID, content any, ts int64) (bool, error)

	// DeleteMessage mirrors deleteMessage (deleteOne; absence is not an error).
	DeleteMessage(ctx context.Context, roomID, msgID primitive.ObjectID) error

	// DeleteUserMessage mirrors deleteUserMessage.
	DeleteUserMessage(ctx context.Context, userID, roomID, msgID primitive.ObjectID) error

	// DeleteMessagesInRoom mirrors deleteAllMessagesInRoom.
	DeleteMessagesInRoom(ctx context.Context, roomID primitive.ObjectID) error

	// DeleteMessagesInRooms mirrors deleteAllMessagesInRooms.
	DeleteMessagesInRooms(ctx context.Context, roomIDs []primitive.ObjectID) error

	// CountMessages mirrors the countDocuments in the notification fan-out.
	CountMessages(ctx context.Context, roomID primitive.ObjectID) (int64, error)

	// FirstMessage mirrors _loadThreadAuthorId (sort timestamp:1, limit 1);
	// (nil, nil) when the room has no messages.
	FirstMessage(ctx context.Context, roomID primitive.ObjectID) (*Message, error)

	// DistinctMessageUsers mirrors the distinct('user_id') call; non-matching
	// values are skipped (Node .filter(id => id?.toString())).
	DistinctMessageUsers(ctx context.Context, roomID primitive.ObjectID) ([]primitive.ObjectID, error)

	// --- cross-service (Node: NotificationsManager) ---

	// ProjectRefs mirrors _loadProject (projection, see ProjectRefs).
	// (nil, nil) when the project does not exist.
	ProjectRefs(ctx context.Context, id primitive.ObjectID) (*ProjectRefs, error)

	// UserNames mirrors _loadSenderName's user lookup; (nil, nil) when absent.
	UserNames(ctx context.Context, id primitive.ObjectID) (*UserNames, error)

	// ProjectPrefs mirrors _loadProjectPreferences: documents matching
	// {user_id:{$in}, project_id}; keyed by the user_id hex.
	ProjectPrefs(ctx context.Context, projectID primitive.ObjectID, userIDs []primitive.ObjectID) (map[string]map[string]any, error)

	// MutedUserIDs mirrors _loadMutedUserIds: {user_id:{$in}, project_id:null,
	// muteAllNotifications:true}; returns the muted user_id set (hex keys).
	MutedUserIDs(ctx context.Context, userIDs []primitive.ObjectID) (map[string]bool, error)

	// UpsertNotification mirrors the db.notifications.updateOne(...) upsert:
	// filter {user_id, key}, $set {user_id, key, messageOpts, templateKey}.
	UpsertNotification(ctx context.Context, userID primitive.ObjectID, key string, messageOpts map[string]any, templateKey string) error

	// UpsertEmailNotification mirrors the db.emailNotifications.updateOne(...)
	// upsert: filter {recipient_id, project_id, emailType:'projectNotification'},
	// $set {recipient_id, project_id, emailType, opts, scheduledAt, updatedAt}.
	UpsertEmailNotification(ctx context.Context, recipient, projectID primitive.ObjectID, opts map[string]any) error
}
