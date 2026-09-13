package core

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

// OneTimeTokens — CE OneTimeTokens parity (mongo collection `tokens`).
// Shape pinned from the live store:
//
//	{ _id, use, token (64 hex), data {user_id,email},
//	  createdAt (UTC), expiresAt (UTC, +1h), peekCount?, usedAt? }
//
// Peek: filter use+token+expiresAt>now, peekCount `$not:{$gte:4}`
// (the Node form — admits missing peekCount), then $inc peekCount in one
// FindOneAndUpdate — the battery pins peekCount==1 after the first peek.
// Expire $sets usedAt; the doc is kept (Node's shape) and dead via the
// expiresAt filter.
type OneTimeTokens struct{ DB *MongoLazy }

// NewOneTimeTokens wires the tokens helper to the app mongo facade.
func NewOneTimeTokens(m *MongoLazy) *OneTimeTokens { return &OneTimeTokens{DB: m} }

// TokenUnknownErr marks the unknown/expired/peeks-out family — the
// password flow maps it to the Node 404 "token-expired" body.
var TokenUnknownErr = errors.New("one-time token unknown")

// coll resolves the `tokens` collection handle.
func (o *OneTimeTokens) coll(ctx context.Context) *mongo.Collection {
	db, err := o.DB.DB(ctx)
	if err != nil {
		panic(err) // unreachable in the wired deployments
	}
	return db.Collection("tokens")
}

// New inserts a fresh token (64 hex chars), returns it. data carries
// user_id + email (the CE password-reset shape).
func (o *OneTimeTokens) New(ctx context.Context, use string, data bson.M) (string, error) {
	tok := make([]byte, 32)
	if _, err := rand.Read(tok); err != nil {
		return "", err
	}
	token := hex.EncodeToString(tok)
	now := time.Now().UTC()
	_, err := o.coll(ctx).InsertOne(ctx, bson.M{
		"use":       use,
		"token":     token,
		"data":      data,
		"createdAt": now,
		"expiresAt": now.Add(time.Hour),
	})
	return token, err
}

// Peek returns the token's data plus the remaining peek budget, or
// TokenUnknownErr when it is unknown, expired, or out of peeks (the
// password flow maps all three onto the same Node 404 — battery-confirmed
// indistinguishable in CE).
func (o *OneTimeTokens) Peek(ctx context.Context, use, token string) (bson.M, int, error) {
	filter := bson.M{
		"use":       use,
		"token":     token,
		"expiresAt": bson.M{"$gt": time.Now().UTC()},
		"peekCount": bson.M{"$not": bson.M{"$gte": 4}},
	}
	var out struct {
		Data      bson.M `bson:"data"`
		PeekCount int    `bson:"peekCount"`
	}
	err := o.coll(ctx).FindOneAndUpdate(ctx, filter,
		bson.M{"$inc": bson.M{"peekCount": 1}}).Decode(&out)
	if err != nil {
		return nil, 0, TokenUnknownErr
	}
	return out.Data, 4 - out.PeekCount, nil
}

// Expire marks the token used (Node's `expire`: $set usedAt — the doc
// lingers, battery-pinned).
func (o *OneTimeTokens) Expire(ctx context.Context, use, token string) {
	_, _ = o.coll(ctx).UpdateOne(ctx,
		bson.M{"use": use, "token": token},
		bson.M{"$set": bson.M{"usedAt": time.Now().UTC()}})
}

// ObjectIdHex converts a mongo id (ObjectID or hex string) to hex —
// token data.user_id is stored as a string (pinned from the fixture doc).
func ObjectIdHex(v any) string {
	switch t := v.(type) {
	case primitive.ObjectID:
		return t.Hex()
	case string:
		return t
	case []byte:
		return string(t)
	}
	return ""
}
