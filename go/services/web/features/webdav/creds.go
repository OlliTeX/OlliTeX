// Credential + sync-state storage (P6.9 webdav).
//
// Node collections (container-verified):
//
//	webdavusercredentials: { userId: ObjectId, credentials: <cipher string> }
//	  — save = findOneAndUpdate({userId}, {$set:{credentials}}, {upsert:true})
//	  — remove = deleteOne({userId})
//	webdavsyncprojectstates: { projectId: string, ...state fields }
//	  — upsert by string projectId (Node W-STR normalization: always string)
//	  — delete by projectId string
//	  — disconnect removes only docs with ownerId === userId (H6 scope)

package webdav

import (
	"context"
	"strings"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/options"

	"ollitex/go/services/web/core"
)

// wdCredsColl/wdStatesColl — MONGOOSE pluralized collection names (all
// lowercase), NOT the camelCase model names. Verified against the live Node
// (Mongoose.model("WebdavUserCredentials") → collection "webdavusercredentials").
const (
	wdCredsColl  = "webdavusercredentials"
	wdStatesColl = "webdavsyncprojectstates"
)

// wdCredsDocExists — true when a credentials doc exists for uid (regardless
// of decryptability). Drives the status handler's undecryptable branch
// (Node: stored-credentials-invalid error key).
func wdCredsDocExists(ctx context.Context, a *core.App, uid string) bool {
	if a.Mongo == nil || uid == "" {
		return false
	}
	db, err := a.Mongo.DB(ctx)
	if err != nil {
		return false
	}
	n, err := db.Collection(wdCredsColl).CountDocuments(ctx, bson.D{{Key: "userId", Value: uid}})
	if err != nil {
		return false
	}
	return n > 0
}

// wdGetCreds returns the decrypted credentials JSON for uid ("",false when
// absent or undecryptable — Node's live behavior: the status route degrades
// corrupted tokens to "not linked").
//
// STORED SHAPE (live Node, mongoose, verified): userId is stored as a HEX
// STRING (req.user._id arrives stringified in the running process), NOT an
// ObjectId — Go must store/query strings for cross-stack interop.
func wdGetCreds(ctx context.Context, a *core.App, uid string) (string, bool) {
	if a.Mongo == nil || uid == "" {
		return "", false
	}
	db, err := a.Mongo.DB(ctx)
	if err != nil {
		return "", false
	}
	var rec struct {
		Credentials string `bson:"credentials"`
	}
	if err := db.Collection(wdCredsColl).FindOne(ctx, bson.D{{Key: "userId", Value: uid}}).Decode(&rec); err != nil {
		return "", false
	}
	if rec.Credentials == "" {
		return "", false
	}
	plain, ok := wdDecrypt(rec.Credentials)
	if !ok {
		return "", false
	}
	return plain, true
}

// wdSaveCreds — Node WebdavCredentials.save(userId, credentials): encrypt
// the given plaintext JSON object, upsert the doc.
func wdSaveCreds(ctx context.Context, a *core.App, uid, plainJSON string) error {
	if a.Mongo == nil || uid == "" {
		return errWdCipher
	}
	enc, err := wdEncrypt(plainJSON)
	if err != nil {
		return err
	}
	db, err := a.Mongo.DB(ctx)
	if err != nil {
		return err
	}
	_, err = db.Collection(wdCredsColl).UpdateOne(ctx,
		bson.D{{Key: "userId", Value: uid}},
		bson.D{{Key: "$set", Value: bson.D{{Key: "credentials", Value: enc}}}},
		options.Update().SetUpsert(true),
	)
	return err
}

// wdRemoveCreds — Node remove(userId): drop the state docs owned by the
// user, then the credential doc.
func wdRemoveCreds(ctx context.Context, a *core.App, uid string) error {
	if a.Mongo == nil || uid == "" {
		return nil
	}
	db, err := a.Mongo.DB(ctx)
	if err != nil {
		return err
	}
	// H6 scope: only docs with ownerId === uid (ownerId stored as the same
	// hex string Node stores userId as).
	cur, err := db.Collection(wdStatesColl).Find(ctx, bson.D{{Key: "ownerId", Value: uid}})
	if err != nil {
		return err
	}
	var pids []string
	for cur.Next(ctx) {
		var s struct {
			ProjectID string `bson:"projectId"`
		}
		if cur.Decode(&s) == nil && s.ProjectID != "" {
			pids = append(pids, s.ProjectID)
		}
	}
	cur.Close(ctx)
	for _, pid := range pids {
		if _, err := db.Collection(wdStatesColl).DeleteOne(ctx, bson.D{{Key: "projectId", Value: pid}}); err != nil {
			return err
		}
	}
	_, err = db.Collection(wdCredsColl).DeleteOne(ctx, bson.D{{Key: "userId", Value: uid}})
	return err
}

// wdHasCreds — presence check (the link/import gate: credentials record
// exists; decryption failure still counts as "present" for Node's
// `credentials || {}` flow only where baseUrl is read — the link route
// destructures decrypted fields, so an undecryptable token behaves like
// absent for the baseUrl check. Keep it simple: presence = record found).
func wdHasCredsRecord(ctx context.Context, a *core.App, uid string) bool {
	if a.Mongo == nil || uid == "" {
		return false
	}
	db, err := a.Mongo.DB(ctx)
	if err != nil {
		return false
	}
	n, err := db.Collection(wdCredsColl).CountDocuments(ctx, bson.D{{Key: "userId", Value: uid}})
	if err != nil {
		return false
	}
	return n > 0
}

// wdGetState / wdCreateState / wdRemoveState / wdStateKeys — the
// webdavSyncProjectStates surface used by the route handlers (state read,
// link write, unlink delete).

func wdGetState(ctx context.Context, a *core.App, projectID string) (bson.D, bool) {
	if a.Mongo == nil || projectID == "" {
		return nil, false
	}
	db, err := a.Mongo.DB(ctx)
	if err != nil {
		return nil, false
	}
	var d bson.D
	err = db.Collection(wdStatesColl).FindOne(ctx, bson.D{{Key: "projectId", Value: strings.ToLower(projectID)}}).Decode(&d)
	if err != nil {
		return nil, false
	}
	return d, true
}

func wdCreateState(ctx context.Context, a *core.App, projectID string, fields []bson.E) error {
	if a.Mongo == nil {
		return errWdCipher
	}
	db, err := a.Mongo.DB(ctx)
	if err != nil {
		return err
	}
	pid := strings.ToLower(projectID)
	_, err = db.Collection(wdStatesColl).UpdateOne(ctx,
		bson.D{{Key: "projectId", Value: pid}},
		bson.D{
			{Key: "$set", Value: bson.D(fields)},
			{Key: "$setOnInsert", Value: bson.D{{Key: "projectId", Value: pid}}},
		},
		options.Update().SetUpsert(true),
	)
	return err
}

func wdRemoveState(ctx context.Context, a *core.App, projectID string) error {
	if a.Mongo == nil {
		return nil
	}
	db, err := a.Mongo.DB(ctx)
	if err != nil {
		return err
	}
	_, err = db.Collection(wdStatesColl).DeleteOne(ctx, bson.D{{Key: "projectId", Value: strings.ToLower(projectID)}})
	return err
}

func wdCountStates(ctx context.Context, a *core.App) (int, error) {
	if a.Mongo == nil {
		return 0, nil
	}
	db, err := a.Mongo.DB(ctx)
	if err != nil {
		return 0, err
	}
	n, err := db.Collection(wdStatesColl).CountDocuments(ctx, bson.D{})
	return int(n), err
}
