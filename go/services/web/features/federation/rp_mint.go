// rp_mint.go — S10 (A-side RP): mirror-user upsert + project grant
// after a successful id_token (LOCKED mirror contract: the user doc's
// `federation.{origin,localName}` marker — the SAME shape the B-side
// resolveAnchorUser reads, s2s_actions.go).
//
// Seam discipline (same as s2s_actions.go): pure function seams the
// handler drives + production mongo glue. Unit tests fake the seams;
// the live leg rides the f2 dual-instance fixture.

package federation

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"

	"ollitex/go/services/web/core"
)

// MirrorResult — the resolved mirror user.
type MirrorResult struct {
	UserHex string
	Created bool
}

// rpFlow — the A-side RP flow seams (production glue via ProdRPFlow;
// unit tests inject fakes).
type rpFlow struct {
	HTTP *http.Client
	// Mirror — resolve-or-create the mirror user for (origin, localName).
	Mirror func(ctx context.Context, origin, localName, displayName string) (MirrorResult, error)
	// Grant — add the mirror user to the A project as collaborator.
	Grant func(ctx context.Context, projectID, userHex, addedByHex string) error
}

// rpRandomHex — n random bytes as hex (nonce/verifier/random digest).
func rpRandomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return ""
	}
	return hex.EncodeToString(b)
}

// ProdRPFlow — production seams over the site mongo (nil-safe: a nil
// app/mongo yields honest "no database" errors at call time, so the
// error legs stay unit-testable without a DB).
func ProdRPFlow(a *core.App) rpFlow {
	return rpFlow{Mirror: prodMirrorUser(a), Grant: prodGrantProject(a)}
}

// fedUserSuspended — the LOCKED suspended check (bool or "true" string
// — the resolveAnchorUser shape, s2s_actions.go:383-401).
func fedUserSuspended(doc bson.M) bool {
	switch s := doc["suspended"].(type) {
	case bool:
		return s
	case string:
		return s == "true"
	}
	return false
}

// prodMirrorUser — mongo mirror, in LOCKED resolution order:
//
//  1. exact `federation.{origin,localName}` marker → existing mirror
//     (suspended ⇒ error, LOCKED invitee-disabled semantics)
//  2. natural email `<localName>@<origin>` → LINK (set the federation
//     marker on the local account; no new user)
//  3. else CREATE: email `<localName>@<origin>`, random digest (the
//     mirror user never password-logins — overleaf's SAML mirror does
//     the same), role "user", displayName → firstName, federation
//     marker + createdAt.
//
// Idempotent: a re-invite of the same (origin, localName) always
// resolves to the SAME user hex (1 → 2 order).
func prodMirrorUser(a *core.App) func(ctx context.Context, origin, localName, displayName string) (MirrorResult, error) {
	return func(ctx context.Context, origin, localName, displayName string) (MirrorResult, error) {
		if a == nil || a.Mongo == nil {
			return MirrorResult{}, errors.New("federation rp: no database")
		}
		db, err := a.Mongo.DB(ctx)
		if err != nil {
			return MirrorResult{}, err
		}
		users := db.Collection("users")
		now := time.Now()
		email := strings.ToLower(strings.TrimSpace(localName)) + "@" + strings.ToLower(origin)
		if email == "@" || strings.Contains(email, " ") {
			return MirrorResult{}, errors.New("federation rp: bad localName/origin for mirror email")
		}

		// 1. exact mirror
		var doc bson.M
		err = users.FindOne(ctx, bson.M{
			"federation.origin":    origin,
			"federation.localName": localName,
		}).Decode(&doc)
		if err == nil {
			if fedUserSuspended(doc) {
				return MirrorResult{}, errors.New("federation rp: mirror-suspended")
			}
			return mirrorID(doc)
		}
		if !errors.Is(err, mongo.ErrNoDocuments) {
			return MirrorResult{}, err
		}
		// 2. natural email → link
		err = users.FindOne(ctx, bson.M{"email": email}).Decode(&doc)
		if err == nil {
			if fedUserSuspended(doc) {
				return MirrorResult{}, errors.New("federation rp: local user suspended")
			}
			if _, uerr := users.UpdateOne(ctx, bson.M{"_id": doc["_id"]}, bson.M{"$set": bson.M{
				"federation": bson.M{
					"origin":    origin,
					"localName": localName,
					"linkedAt":  now,
					"createdAt": now,
				},
			}}); uerr != nil {
				return MirrorResult{}, uerr
			}
			return mirrorID(doc)
		}
		if !errors.Is(err, mongo.ErrNoDocuments) {
			return MirrorResult{}, err
		}
		// 3. create
		newID := bson.NewObjectID()
		inv := bson.M{
			"_id":   newID,
			"email": email,
			"emails": bson.A{bson.M{
				"email":     email,
				"createdAt": now,
			}},
			"passwordDigest": rpRandomHex(32),
			"role":           "user",
			"firstName":      displayName,
			"lastName":       "",
			"createdAt":      now,
			"federation": bson.M{
				"origin":    origin,
				"localName": localName,
				"createdAt": now,
				"migrated":  false,
			},
		}
		if _, ierr := users.InsertOne(ctx, inv); ierr != nil {
			// concurrent create race → re-resolve by exact marker
			if derr := users.FindOne(ctx, bson.M{
				"federation.origin":    origin,
				"federation.localName": localName,
			}).Decode(&doc); derr == nil {
				if fedUserSuspended(doc) {
					return MirrorResult{}, errors.New("federation rp: mirror-suspended")
				}
				return mirrorID(doc)
			}
			return MirrorResult{}, ierr
		}
		return MirrorResult{UserHex: newID.Hex(), Created: true}, nil
	}
}

func mirrorID(doc bson.M) (MirrorResult, error) {
	id, ok := doc["_id"].(bson.ObjectID)
	if !ok {
		return MirrorResult{}, errors.New("federation rp: user doc without object id")
	}
	return MirrorResult{UserHex: id.Hex(), Created: false}, nil
}

// prodGrantProject — the mirror user joins the A project as
// collaborator (Node: grant via collaborator — LOCKED decision 11).
//
// Shape follows the projects.collaborators entries the rest of the Go
// app reads (user/permission/added/addedBy); $addToSet is idempotent
// for a re-invite.
func prodGrantProject(a *core.App) func(ctx context.Context, projectID, userHex, addedByHex string) error {
	return func(ctx context.Context, projectID, userHex, addedByHex string) error {
		if a == nil || a.Mongo == nil {
			return errors.New("federation rp: no database")
		}
		if len(projectID) != 24 {
			return errors.New("federation rp: bad project id")
		}
		pid, err := bson.ObjectIDFromHex(projectID)
		if err != nil {
			return errors.New("federation rp: bad project id")
		}
		uid, err := bson.ObjectIDFromHex(userHex)
		if err != nil {
			return errors.New("federation rp: bad user id")
		}
		db, err := a.Mongo.DB(ctx)
		if err != nil {
			return err
		}
		projects := db.Collection("projects")
		if err := projects.FindOne(ctx, bson.M{"_id": pid}).Err(); err != nil {
			if errors.Is(err, mongo.ErrNoDocuments) {
				return errors.New("federation rp: project not found")
			}
			return err
		}
		by := bson.M{}
		if len(addedByHex) == 24 {
			if ob, oerr := bson.ObjectIDFromHex(addedByHex); oerr == nil {
				by = bson.M{"addedBy": ob}
			}
		}
		entry := bson.M{
			"user":       uid,
			"permission": "collaborator",
			"role":       "collaborator",
			"added":      time.Now(),
		}
		for k, v := range by {
			entry[k] = v
		}
		n, err := projects.UpdateOne(ctx, bson.M{"_id": pid}, bson.M{
			"$addToSet": bson.M{"collaborators": entry},
		})
		if err != nil {
			return err
		}
		_ = n
		return nil
	}
}
