package chat

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// mongoStore is the production Store: a 1:1 re-implementation of the Node
// service's queries. Each method documents the exact Node call it mirrors so
// the two can be diffed.
//
// Collections (Node services/chat/app/js/mongodb.js): rooms, messages, users,
// projects, notifications, notificationsPreferences, emailNotifications —
// all in the shared database (default 'sharelatex').
type mongoStore struct {
	rooms         *mongo.Collection
	messages      *mongo.Collection
	users         *mongo.Collection
	projects      *mongo.Collection
	prefs         *mongo.Collection
	notifications *mongo.Collection
	emailNotifs   *mongo.Collection
}

// NewMongoStore builds a Store over the shared database (1:1 with the Node
// service's db collections).
func NewMongoStore(client *mongo.Client, db string) Store {
	d := client.Database(db)
	return &mongoStore{
		rooms:         d.Collection("rooms"),
		messages:      d.Collection("messages"),
		users:         d.Collection("users"),
		projects:      d.Collection("projects"),
		prefs:         d.Collection("notificationsPreferences"),
		notifications: d.Collection("notifications"),
		emailNotifs:   d.Collection("emailNotifications"),
	}
}

// ---- rooms (Node: ThreadManager) ---------------------------------------------

// inValues converts an ObjectID slice to the driver's explicit $in value:
// bson.D{{"$in": [...]}}. NOTE: a bare slice as a filter value is NOT an
// implicit $in on MongoDB 8.3.7 ($match sees a bare array and matches
// nothing) — the operator MUST be explicit, exactly like the Node filters.
func inValues(ids []primitive.ObjectID) bson.D {
	return bson.D{{Key: "$in", Value: toAnySlice(ids)}}
}

// toAnySlice converts an ObjectID slice to a []any (the $in list value).
func toAnySlice(ids []primitive.ObjectID) []any {
	out := make([]any, len(ids))
	for i, id := range ids {
		out[i] = id
	}
	return out
}

// FindOrCreateRoom — Node findOrCreateThread:
//
//	global:  findOneAndUpdate({project_id, thread_id:{$exists:false}},
//	                                 {$set:{project_id}}, {upsert:true, returnDocument:'after'})
//	thread:  findOneAndUpdate({project_id, thread_id}, {$set:{project_id, thread_id}}, ...)
func (s *mongoStore) FindOrCreateRoom(ctx context.Context, projectID primitive.ObjectID, threadID *primitive.ObjectID) (*Room, error) {
	var filter bson.D
	var set bson.D
	if threadID == nil {
		filter = bson.D{{Key: "project_id", Value: projectID}, {Key: "thread_id", Value: bson.D{{Key: "$exists", Value: false}}}}
		set = bson.D{{Key: "project_id", Value: projectID}}
	} else {
		filter = bson.D{{Key: "project_id", Value: projectID}, {Key: "thread_id", Value: *threadID}}
		set = bson.D{{Key: "project_id", Value: projectID}, {Key: "thread_id", Value: *threadID}}
	}
	room := &Room{}
	err := s.rooms.FindOneAndUpdate(ctx, filter,
		bson.D{{Key: "$set", Value: set}},
		options.FindOneAndUpdate().SetUpsert(true).SetReturnDocument(options.After),
	).Decode(room)
	if err != nil {
		return nil, err
	}
	return room, nil
}

// FindRoom — Node findThread (minus the MissingThreadError raise).
func (s *mongoStore) FindRoom(ctx context.Context, projectID primitive.ObjectID, threadID *primitive.ObjectID) (*Room, error) {
	filter := bson.D{{Key: "project_id", Value: projectID}}
	if threadID == nil {
		filter = append(filter, bson.E{Key: "thread_id", Value: bson.D{{Key: "$exists", Value: false}}})
	} else {
		filter = append(filter, bson.E{Key: "thread_id", Value: *threadID})
	}
	room := &Room{}
	if err := s.rooms.FindOne(ctx, filter).Decode(room); err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, nil
		}
		return nil, err
	}
	return room, nil
}

// ThreadRooms — Node findAllThreadRooms: {project_id, thread_id:{$exists:true}}.
func (s *mongoStore) ThreadRooms(ctx context.Context, projectID primitive.ObjectID) ([]*Room, error) {
	return s.findRooms(ctx, bson.D{{Key: "project_id", Value: projectID}, {Key: "thread_id", Value: bson.D{{Key: "$exists", Value: true}}}})
}

// AllRooms — Node findAllThreadRoomsAndGlobalThread: {project_id}.
func (s *mongoStore) AllRooms(ctx context.Context, projectID primitive.ObjectID) ([]*Room, error) {
	return s.findRooms(ctx, bson.D{{Key: "project_id", Value: projectID}})
}

// RoomsByThreadIDs — Node findThreadsById: {project_id, thread_id:{$in: [...]}}.
func (s *mongoStore) RoomsByThreadIDs(ctx context.Context, projectID primitive.ObjectID, threadIDs []primitive.ObjectID) ([]*Room, error) {
	return s.findRooms(ctx, bson.D{{Key: "project_id", Value: projectID}, {Key: "thread_id", Value: inValues(threadIDs)}})
}

func (s *mongoStore) findRooms(ctx context.Context, filter bson.D) ([]*Room, error) {
	cur, err := s.rooms.Find(ctx, filter)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	var out []*Room
	if err := cur.All(ctx, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ResolveThread — Node ThreadManager.resolveThread:
// updateOne({project_id, thread_id}, {$set: {resolved: {user_id, ts: new Date()}}})
func (s *mongoStore) ResolveThread(ctx context.Context, projectID, threadID, userID primitive.ObjectID) error {
	_, err := s.rooms.UpdateOne(ctx,
		bson.D{{Key: "project_id", Value: projectID}, {Key: "thread_id", Value: threadID}},
		bson.D{{Key: "$set", Value: bson.D{
			{Key: "resolved", Value: bson.D{
				{Key: "user_id", Value: userID},
				{Key: "ts", Value: time.Now()},
			}},
		}}},
	)
	return err
}

// ReopenThread — Node ThreadManager.reopenThread: $unset resolved.
func (s *mongoStore) ReopenThread(ctx context.Context, projectID, threadID primitive.ObjectID) error {
	_, err := s.rooms.UpdateOne(ctx,
		bson.D{{Key: "project_id", Value: projectID}, {Key: "thread_id", Value: threadID}},
		bson.D{{Key: "$unset", Value: bson.D{{Key: "resolved", Value: true}}}},
	)
	return err
}

// DeleteRoom — Node db.rooms.deleteOne({_id}).
func (s *mongoStore) DeleteRoom(ctx context.Context, id primitive.ObjectID) error {
	_, err := s.rooms.DeleteOne(ctx, bson.D{{Key: "_id", Value: id}})
	return err
}

// DeleteRoomsByProject — Node deleteAllThreadsInProject.
func (s *mongoStore) DeleteRoomsByProject(ctx context.Context, projectID primitive.ObjectID) error {
	_, err := s.rooms.DeleteMany(ctx, bson.D{{Key: "project_id", Value: projectID}})
	return err
}

// InsertRoom — duplicateThread / cloneThreads insertOne (full document).
func (s *mongoStore) InsertRoom(ctx context.Context, room *Room) error {
	doc := bson.D{{Key: "_id", Value: room.ID}, {Key: "project_id", Value: room.ProjectID}}
	if room.ThreadID != nil {
		doc = append(doc, bson.E{Key: "thread_id", Value: *room.ThreadID})
	}
	if room.Resolved != nil {
		doc = append(doc, bson.E{Key: "resolved", Value: bson.D{
			{Key: "user_id", Value: room.Resolved.UserID},
			{Key: "ts", Value: room.Resolved.TS},
		}})
	}
	_, err := s.rooms.InsertOne(ctx, doc)
	return err
}

// ResolvedThreadIDs — Node getResolvedThreadIds:
// find({project_id, thread_id:{$exists:true}, resolved:{$exists:true}}).
func (s *mongoStore) ResolvedThreadIDs(ctx context.Context, projectID primitive.ObjectID) ([]primitive.ObjectID, error) {
	rooms, err := s.findRooms(ctx, bson.D{
		{Key: "project_id", Value: projectID},
		{Key: "thread_id", Value: bson.D{{Key: "$exists", Value: true}}},
		{Key: "resolved", Value: bson.D{{Key: "$exists", Value: true}}},
	})
	if err != nil {
		return nil, err
	}
	out := make([]primitive.ObjectID, 0, len(rooms))
	for _, r := range rooms {
		if r.ThreadID != nil {
			out = append(out, *r.ThreadID)
		}
	}
	return out, nil
}

// ---- messages (Node: MessageManager) ------------------------------------------

// FetchMessage — Node MessageManager.getMessage (minus the MissingMessageError).
func (s *mongoStore) FetchMessage(ctx context.Context, roomID, msgID primitive.ObjectID) (*Message, error) {
	msg := &Message{}
	err := s.messages.FindOne(ctx, bson.D{{Key: "_id", Value: msgID}, {Key: "room_id", Value: roomID}}).Decode(msg)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, nil
		}
		return nil, err
	}
	return msg, nil
}

// InsertMessage — Node createMessage: insertOne({content, room_id, user_id,
// timestamp}) (+ _id).
func (s *mongoStore) InsertMessage(ctx context.Context, roomID, userID primitive.ObjectID, content any, ts int64) (primitive.ObjectID, error) {
	doc := bson.D{
		{Key: "_id", Value: primitive.NewObjectID()},
		{Key: "content", Value: jsonable(content)},
		{Key: "room_id", Value: roomID},
		{Key: "user_id", Value: userID},
		{Key: "timestamp", Value: ts},
	}
	res, err := s.messages.InsertOne(ctx, doc)
	if err != nil {
		return primitive.ObjectID{}, err
	}
	id, ok := res.InsertedID.(primitive.ObjectID)
	if !ok {
		return primitive.ObjectID{}, err
	}
	return id, nil
}

// RoomMessages — Node getMessages:
// find({room_id[, timestamp:{$lt:before}]}).sort({timestamp:-1}).limit(limit)
// (Node `if (before)`: 0/undefined → no filter; cursor.limit(0) → no limit).
func (s *mongoStore) RoomMessages(ctx context.Context, roomID primitive.ObjectID, limit int, before int64) ([]*Message, error) {
	filter := bson.D{{Key: "room_id", Value: roomID}}
	if before != 0 {
		filter = append(filter, bson.E{Key: "timestamp", Value: bson.D{{Key: "$lt", Value: before}}})
	}
	opts := options.Find().SetSort(bson.D{{Key: "timestamp", Value: -1}})
	if limit > 0 {
		opts.SetLimit(int64(limit))
	}
	cur, err := s.messages.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	var out []*Message
	if err := cur.All(ctx, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// MessagesInRooms — Node findAllMessagesInRooms: find({room_id:{$in: [...],}}).
func (s *mongoStore) MessagesInRooms(ctx context.Context, roomIDs []primitive.ObjectID) ([]*Message, error) {
	cur, err := s.messages.Find(ctx, bson.D{{Key: "room_id", Value: inValues(roomIDs)}})
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	var out []*Message
	if err := cur.All(ctx, &out); err != nil {
		return nil, err
	}
	if out == nil {
		out = []*Message{}
	}
	return out, nil
}

// UpdateMessage — Node updateMessage:
// updateOne({_id, room_id[, user_id]}, {$set:{content, edited_at}})
// and returns res.modifiedCount === 1.
func (s *mongoStore) UpdateMessage(ctx context.Context, roomID, msgID primitive.ObjectID, userID *primitive.ObjectID, content any, ts int64) (bool, error) {
	filter := bson.D{{Key: "_id", Value: msgID}, {Key: "room_id", Value: roomID}}
	if userID != nil {
		filter = append(filter, bson.E{Key: "user_id", Value: *userID})
	}
	res, err := s.messages.UpdateOne(ctx, filter, bson.D{{Key: "$set", Value: bson.D{
		{Key: "content", Value: jsonable(content)},
		{Key: "edited_at", Value: ts},
	}}})
	if err != nil {
		return false, err
	}
	return res.ModifiedCount == 1, nil
}

// DeleteMessage — Node deleteMessage (deleteOne; no-match is not an error).
func (s *mongoStore) DeleteMessage(ctx context.Context, roomID, msgID primitive.ObjectID) error {
	_, err := s.messages.DeleteOne(ctx, bson.D{{Key: "_id", Value: msgID}, {Key: "room_id", Value: roomID}})
	return err
}

// DeleteUserMessage — Node deleteUserMessage.
func (s *mongoStore) DeleteUserMessage(ctx context.Context, userID, roomID, msgID primitive.ObjectID) error {
	_, err := s.messages.DeleteOne(ctx, bson.D{
		{Key: "_id", Value: msgID},
		{Key: "user_id", Value: userID},
		{Key: "room_id", Value: roomID},
	})
	return err
}

// DeleteMessagesInRoom — Node deleteAllMessagesInRoom.
func (s *mongoStore) DeleteMessagesInRoom(ctx context.Context, roomID primitive.ObjectID) error {
	_, err := s.messages.DeleteMany(ctx, bson.D{{Key: "room_id", Value: roomID}})
	return err
}

// DeleteMessagesInRooms — Node deleteAllMessagesInRooms.
func (s *mongoStore) DeleteMessagesInRooms(ctx context.Context, roomIDs []primitive.ObjectID) error {
	_, err := s.messages.DeleteMany(ctx, bson.D{{Key: "room_id", Value: inValues(roomIDs)}})
	return err
}

// CountMessages — Node countDocuments({room_id}).
func (s *mongoStore) CountMessages(ctx context.Context, roomID primitive.ObjectID) (int64, error) {
	return s.messages.CountDocuments(ctx, bson.D{{Key: "room_id", Value: roomID}})
}

// FirstMessage — Node _loadThreadAuthorId:
// find({room_id}).sort({timestamp:1}).limit(1) (nil on absence).
func (s *mongoStore) FirstMessage(ctx context.Context, roomID primitive.ObjectID) (*Message, error) {
	msg := &Message{}
	err := s.messages.FindOne(ctx, bson.D{{Key: "room_id", Value: roomID}},
		options.FindOne().SetSort(bson.D{{Key: "timestamp", Value: 1}})).Decode(msg)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, nil
		}
		return nil, err
	}
	return msg, nil
}

// DistinctMessageUsers — Node distinct('user_id', {room_id}) + filter(Boolean).
func (s *mongoStore) DistinctMessageUsers(ctx context.Context, roomID primitive.ObjectID) ([]primitive.ObjectID, error) {
	vals, err := s.messages.Distinct(ctx, "user_id", bson.D{{Key: "room_id", Value: roomID}})
	if err != nil {
		return nil, err
	}
	out := make([]primitive.ObjectID, 0, len(vals))
	for _, v := range vals {
		if id, ok := v.(primitive.ObjectID); ok {
			out = append(out, id)
		}
	}
	return out, nil
}

// ---- cross-service (Node: NotificationsManager) --------------------------------

// ProjectRefs — Node _loadProject: findOne({_id}, projection) over `projects`.
func (s *mongoStore) ProjectRefs(ctx context.Context, id primitive.ObjectID) (*ProjectRefs, error) {
	doc := &ProjectRefs{}
	err := s.projects.FindOne(ctx, bson.D{{Key: "_id", Value: id}}).Decode(doc)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, nil
		}
		return nil, err
	}
	return doc, nil
}

// UserNames — Node _loadSenderName user lookup: findOne({_id}, projection).
func (s *mongoStore) UserNames(ctx context.Context, id primitive.ObjectID) (*UserNames, error) {
	doc := &UserNames{}
	err := s.users.FindOne(ctx, bson.D{{Key: "_id", Value: id}}).Decode(doc)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, nil
		}
		return nil, err
	}
	return doc, nil
}

// ProjectPrefs — Node _loadProjectPreferences:
// notificationsPreferences.find({user_id:{$in}, project_id}) (raw docs keyed by
// user hex; normalisation happens in the handler, mirroring the Node split).
func (s *mongoStore) ProjectPrefs(ctx context.Context, projectID primitive.ObjectID, userIDs []primitive.ObjectID) (map[string]map[string]any, error) {
	cur, err := s.prefs.Find(ctx, bson.D{
		{Key: "user_id", Value: inValues(userIDs)},
		{Key: "project_id", Value: projectID},
	})
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	var docs []map[string]any
	if err := cur.All(ctx, &docs); err != nil {
		return nil, err
	}
	out := map[string]map[string]any{}
	for _, d := range docs {
		if uid, ok := d["_id"].(primitive.ObjectID); ok {
			_ = uid
		}
		if raw, ok := d["user_id"].(primitive.ObjectID); ok {
			out[raw.Hex()] = d
		}
	}
	return out, nil
}

// MutedUserIDs — Node _loadMutedUserIds:
// notificationsPreferences.find({user_id:{$in}, project_id:null, muteAllNotifications:true}).
func (s *mongoStore) MutedUserIDs(ctx context.Context, userIDs []primitive.ObjectID) (map[string]bool, error) {
	cur, err := s.prefs.Find(ctx, bson.D{
		{Key: "user_id", Value: inValues(userIDs)},
		{Key: "project_id", Value: nil},
		{Key: "muteAllNotifications", Value: true},
	})
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	var docs []bson.M
	if err := cur.All(ctx, &docs); err != nil {
		return nil, err
	}
	out := map[string]bool{}
	for _, d := range docs {
		if raw, ok := d["user_id"].(primitive.ObjectID); ok {
			out[raw.Hex()] = true
		}
	}
	return out, nil
}

// UpsertNotification — Node db.notifications.updateOne:
// filter {user_id, key}, $set {user_id, key, messageOpts, templateKey}, upsert.
func (s *mongoStore) UpsertNotification(ctx context.Context, userID primitive.ObjectID, key string, messageOpts map[string]any, templateKey string) error {
	opts := options.Update().SetUpsert(true)
	_, err := s.notifications.UpdateOne(ctx,
		bson.D{{Key: "user_id", Value: userID}, {Key: "key", Value: key}},
		bson.D{{Key: "$set", Value: bson.D{
			{Key: "user_id", Value: userID},
			{Key: "key", Value: key},
			{Key: "messageOpts", Value: messageOpts},
			{Key: "templateKey", Value: templateKey},
		}}},
		opts,
	)
	return err
}

// UpsertEmailNotification — Node db.emailNotifications.updateOne:
// filter {recipient_id, project_id, emailType}, $set {recipient_id, project_id,
// emailType, opts, scheduledAt, updatedAt}, upsert.
func (s *mongoStore) UpsertEmailNotification(ctx context.Context, recipient, projectID primitive.ObjectID, opts map[string]any) error {
	now := time.Now()
	upsert := options.Update().SetUpsert(true)
	_, err := s.emailNotifs.UpdateOne(ctx,
		bson.D{
			{Key: "recipient_id", Value: recipient},
			{Key: "project_id", Value: projectID},
			{Key: "emailType", Value: "projectNotification"},
		},
		bson.D{{Key: "$set", Value: bson.D{
			{Key: "recipient_id", Value: recipient},
			{Key: "project_id", Value: projectID},
			{Key: "emailType", Value: "projectNotification"},
			{Key: "opts", Value: opts},
			{Key: "scheduledAt", Value: now},
			{Key: "updatedAt", Value: now},
		}}},
		upsert,
	)
	return err
}

var _ Store = (*mongoStore)(nil)
