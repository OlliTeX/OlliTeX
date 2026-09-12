// Package notifications is the Go 1:1 replacement of the Node notifications
// service (services/notifications, port 3042). It is a standalone HTTP service
// backed by the shared Overleaf MongoDB `notifications` collection.
//
// The Node service (app.ts + app/js/Notifications.js + NotificationsController.ts)
// is a small CRUD over one collection. This port mirrors its routes, request
// validation, status codes, response shapes, and — critically for a drop-in —
// the exact MongoDB document shape and operations, so a document written by the
// Go service is indistinguishable from one written by the Node service and vice
// versa (verified by the Node⇄Go interop test against a live mongod).
package notifications

import (
	"context"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// Store abstracts the `notifications` collection. It isolates the HTTP layer
// from MongoDB so the routing/validation/serialisation logic is unit-testable
// against an in-memory Store (memStore), while the real mongo-backed behaviour
// lives in mongoStore and is covered by the integration + interop tests.
//
// Every method name maps 1:1 onto a function in the Node Notifications.js.
type Store interface {
	// GetUserNotifications — UserNotifications(userId):
	//   db.notifications.find({user_id, templateKey:{$exists:true}}).toArray()
	GetUserNotifications(ctx context.Context, userID primitive.ObjectID) ([]primitive.M, error)

	// CountByUserKey — _countExistingNotifications(userId, notification):
	//   db.notifications.count({user_id, key})
	CountByUserKey(ctx context.Context, userID primitive.ObjectID, key string) (int64, error)

	// Upsert — the upsert half of addNotification:
	//   db.notifications.updateOne({user_id, key}, {$set: doc}, {upsert:true})
	// setDoc is the full {$set} document (caller builds it, matching Node).
	Upsert(ctx context.Context, filter primitive.M, setDoc primitive.M) error

	// UnsetByID — removeNotificationId(userId, notificationId):
	//   db.notifications.updateOne({user_id, _id}, {$unset:{templateKey, messageOpts}})
	UnsetByID(ctx context.Context, userID, id primitive.ObjectID) error

	// UnsetByUserKey — removeNotificationKey(userId, key):
	//   db.notifications.updateOne({user_id, key}, {$unset:{templateKey}})
	UnsetByUserKey(ctx context.Context, userID primitive.ObjectID, key string) error

	// UnsetByKeyOnly — removeNotificationByKeyOnly(key):
	//   db.notifications.updateOne({key}, {$unset:{templateKey}})
	UnsetByKeyOnly(ctx context.Context, key string) error

	// CountByKeyOnly — countNotificationsByKeyOnly(key):
	//   db.notifications.countDocuments({key, templateKey:{$exists:true}})
	CountByKeyOnly(ctx context.Context, key string) (int64, error)

	// DeleteManyByKeyOnly — deleteUnreadNotificationsByKeyOnlyBulk(key):
	//   db.notifications.deleteMany({key, templateKey:{$exists:true}}).deletedCount
	DeleteManyByKeyOnly(ctx context.Context, key string) (int64, error)

	// DeleteOneByUser — HealthCheckController.cleanupNotifications:
	//   db.notifications.deleteOne({user_id})
	DeleteOneByUser(ctx context.Context, userID primitive.ObjectID) error
}

// ---- in-memory fake (unit tests) -------------------------------------------
//
// memStore reproduces the observable MongoDB semantics the service relies on
// (upsert by user_id+key, $unset templateKey, $exists:true filtering) so the
// HTTP contract and serialisation are tested deterministically without Mongo.
//
// Document shape mirrors Node exactly:
//
//	{ _id, user_id, key, templateKey?, messageOpts?, expires? }
type memStore struct {
	docs  map[string]primitive.M // keyed by user_idHex + "|" + key
	order []string
}

func newMemStore() *memStore {
	return &memStore{docs: map[string]primitive.M{}}
}

func memKey(userID primitive.ObjectID, key string) string {
	return userID.Hex() + "|" + key
}

func (m *memStore) GetUserNotifications(_ context.Context, userID primitive.ObjectID) ([]primitive.M, error) {
	out := make([]primitive.M, 0)
	for _, k := range m.order {
		d := m.docs[k]
		if oid, ok := d["user_id"].(primitive.ObjectID); ok && oid == userID {
			if _, hasTK := d["templateKey"]; hasTK { // templateKey: {$exists: true}
				out = append(out, copyDoc(d))
			}
		}
	}
	return out, nil
}

func (m *memStore) CountByUserKey(_ context.Context, userID primitive.ObjectID, key string) (int64, error) {
	_, ok := m.docs[memKey(userID, key)]
	if ok {
		return 1, nil
	}
	return 0, nil
}

func (m *memStore) Upsert(_ context.Context, filter, setDoc primitive.M) error {
	userID, _ := filter["user_id"].(primitive.ObjectID)
	key, _ := filter["key"].(string)
	k := memKey(userID, key)
	var base primitive.M
	if existing, ok := m.docs[k]; ok {
		base = copyDoc(existing)
	} else {
		base = primitive.M{"_id": primitive.NewObjectID()}
		m.order = append(m.order, k)
	}
	for field, val := range setDoc {
		base[field] = val
	}
	m.docs[k] = base
	return nil
}

func (m *memStore) UnsetByID(_ context.Context, userID, id primitive.ObjectID) error {
	for _, k := range m.order {
		d := m.docs[k]
		did, _ := d["_id"].(primitive.ObjectID)
		_uid, _ := d["user_id"].(primitive.ObjectID)
		if did == id && _uid == userID {
			delete(d, "templateKey")
			delete(d, "messageOpts")
		}
	}
	return nil
}

func (m *memStore) UnsetByUserKey(_ context.Context, userID primitive.ObjectID, key string) error {
	k := memKey(userID, key)
	if d, ok := m.docs[k]; ok {
		delete(d, "templateKey")
	}
	return nil
}

func (m *memStore) UnsetByKeyOnly(_ context.Context, key string) error {
	for _, k := range m.order {
		d := m.docs[k]
		if dk, _ := d["key"].(string); dk == key {
			delete(d, "templateKey")
			break // updateOne: only the first matching document
		}
	}
	return nil
}

func (m *memStore) CountByKeyOnly(_ context.Context, key string) (int64, error) {
	var n int64
	for _, k := range m.order {
		d := m.docs[k]
		if dk, _ := d["key"].(string); dk == key {
			if _, hasTK := d["templateKey"]; hasTK {
				n++
			}
		}
	}
	return n, nil
}

func (m *memStore) DeleteManyByKeyOnly(_ context.Context, key string) (int64, error) {
	var n int64
	for _, k := range m.order {
		d := m.docs[k]
		if dk, _ := d["key"].(string); dk == key {
			if _, hasTK := d["templateKey"]; hasTK {
				n++
			}
		}
	}
	// deleteMany: remove all matches (readable/unread both removed? Node filters
	// templateKey $exists in the filter, so only unread docs are deleted).
	if n > 0 {
		write := make([]string, 0, len(m.order))
		for _, k := range m.order {
			d := m.docs[k]
			dk, _ := d["key"].(string)
			_, hasTK := d["templateKey"]
			if dk == key && hasTK {
				delete(m.docs, k)
			} else {
				write = append(write, k)
			}
		}
		m.order = write
	}
	return n, nil
}

func (m *memStore) DeleteOneByUser(_ context.Context, userID primitive.ObjectID) error {
	write := make([]string, 0, len(m.order))
	deleted := false
	for _, k := range m.order {
		d := m.docs[k]
		uid, _ := d["user_id"].(primitive.ObjectID)
		if uid == userID && !deleted {
			delete(m.docs, k)
			deleted = true
		} else {
			write = append(write, k)
		}
	}
	m.order = write
	return nil
}

func copyDoc(d primitive.M) primitive.M {
	out := make(primitive.M, len(d))
	for k, v := range d {
		out[k] = v
	}
	return out
}

var _ Store = (*memStore)(nil)
