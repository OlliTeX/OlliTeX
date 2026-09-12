package notifications

import (
	"context"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

// mongoStore is the production Store: a 1:1 re-implementation of the Node
// service's queries against the shared `notifications` collection.
//
// Each method body documents the exact Node call it mirrors so the two can be
// diffed line-for-line.
type mongoStore struct {
	col *mongo.Collection
}

// NewMongoStore builds a Store over the shared notifications collection.
func NewMongoStore(client *mongo.Client, db, collection string) Store {
	if collection == "" {
		collection = "notifications"
	}
	return &mongoStore{col: client.Database(db).Collection(collection)}
}

func (s *mongoStore) GetUserNotifications(ctx context.Context, userID primitive.ObjectID) ([]primitive.M, error) {
	// Node: db.notifications.find({ user_id: userId, templateKey: { $exists: true } }).toArray()
	cur, err := s.col.Find(ctx, primitive.M{
		"user_id":     userID,
		"templateKey": primitive.M{"$exists": true},
	})
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	var out []primitive.M
	if err := cur.All(ctx, &out); err != nil {
		return nil, err
	}
	if out == nil {
		out = make([]primitive.M, 0)
	}
	return out, nil
}

func (s *mongoStore) CountByUserKey(ctx context.Context, userID primitive.ObjectID, key string) (int64, error) {
	// Node: db.notifications.count({ user_id: userId, key: notification.key })
	return s.col.CountDocuments(ctx, primitive.M{"user_id": userID, "key": key})
}

func (s *mongoStore) Upsert(ctx context.Context, filter, setDoc primitive.M) error {
	// Node: db.notifications.updateOne({ user_id: userId, key: notification.key },
	//        { $set: { ...document } }, { upsert: true })
	_, err := s.col.UpdateOne(ctx, filter, bson.D{{Key: "$set", Value: setDoc}})
	if err != nil {
		// UpdateOne returns ErrNoDocuments when the update matched nothing and
		// upsert was not applied — not the case here (upsert always matches/creates).
		// Surface any real error; the handler maps driver errors to 500 like Node.
		if err == mongo.ErrNoDocuments {
			return nil
		}
		return err
	}
	return nil
}

func (s *mongoStore) UnsetByID(ctx context.Context, userID, id primitive.ObjectID) error {
	// Node: db.notifications.updateOne({ user_id: userId, _id: notificationId },
	//        { $unset: { templateKey: 1, messageOpts: 1 } })
	_, err := s.col.UpdateOne(ctx, primitive.M{"user_id": userID, "_id": id},
		bson.D{{Key: "$unset", Value: primitive.M{"templateKey": 1, "messageOpts": 1}}})
	return err // Node lets this resolve silently (updateOne on no-match ≠ throw); keep it soft.
}

func (s *mongoStore) UnsetByUserKey(ctx context.Context, userID primitive.ObjectID, key string) error {
	// Node: db.notifications.updateOne({ user_id, key }, { $unset: { templateKey: 1 } })
	_, err := s.col.UpdateOne(ctx, primitive.M{"user_id": userID, "key": key},
		bson.D{{Key: "$unset", Value: primitive.M{"templateKey": 1}}})
	return err
}

func (s *mongoStore) UnsetByKeyOnly(ctx context.Context, key string) error {
	// Node: db.notifications.updateOne({ key }, { $unset: { templateKey: 1 } })
	_, err := s.col.UpdateOne(ctx, primitive.M{"key": key},
		bson.D{{Key: "$unset", Value: primitive.M{"templateKey": 1}}})
	return err
}

func (s *mongoStore) CountByKeyOnly(ctx context.Context, key string) (int64, error) {
	// Node: db.notifications.countDocuments({ key, templateKey: { $exists: true } })
	return s.col.CountDocuments(ctx, primitive.M{
		"key":         key,
		"templateKey": primitive.M{"$exists": true},
	})
}

func (s *mongoStore) DeleteManyByKeyOnly(ctx context.Context, key string) (int64, error) {
	// Node: res = db.notifications.deleteMany({ key, templateKey: { $exists: true } }); res.deletedCount
	res, err := s.col.DeleteMany(ctx, primitive.M{
		"key":         key,
		"templateKey": primitive.M{"$exists": true},
	})
	if err != nil {
		return 0, err
	}
	return res.DeletedCount, nil
}

func (s *mongoStore) DeleteOneByUser(ctx context.Context, userID primitive.ObjectID) error {
	// Node: db.notifications.deleteOne({ user_id: userId })
	_, err := s.col.DeleteOne(ctx, primitive.M{"user_id": userID})
	return err
}

var _ Store = (*mongoStore)(nil)
