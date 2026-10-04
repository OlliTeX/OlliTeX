package passwordreset

// POST /user/password/update — the logged-in "change password" contract
// (Node UserController.changePassword, router.mjs:350; the Go port was
// MISSING — 2026-10-05 route-retirement wave run caught it live:
// `Cannot POST /user/password/update` 404 while the parity spec pinned 200).
//
// Node contract (source port + CE pins):
//   - requireLogin (global web gate — routes without NoLogin).
//   - requirePermission('change-password'): default-true capability for
//     local-password users (the SSO modules register it as the non-default
//     case); CE users with a local password pass.
//   - rate limiter `changePassword` 10/60s per IP — 429 body
//     'Rate limit reached, please try again later'.
//   - body (strict, all optional): {currentPassword, newPassword1,
//     newPassword2}
//   - authenticate(currentPassword) fails → 400
//     {"message":"Current password is incorrect."}
//   - newPassword1 !== newPassword2 → 400
//     {"message":"New passwords do not match."}
//   - validatePassword (CE order too_short/too_long/invalid_character/
//     contains_email) → 400 {"message":{"type":"error","key":"..",
//     "text":".."}}
//   - new password == current → 400
//     {"message":"The password must be different."}
//   - success: 200 {"message":{"type":"success","email":"<email>","text":
//     "Your password has been changed successfully."}} + users.$set
//     hashedPassword (bcrypt) + $unset password + userAuditLogEntries
//     'update-password' + remove all OTHER sessions (the requesting one
//     stays) + expire the user's 'password' tokens.
//
// The standing pin (legacy-mysettings) sets a known fixture password,
// verifies a fresh context can log in with it, then restores — so the
// write path must update hashedPassword and keep the current session
// alive.

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"golang.org/x/crypto/bcrypt"

	"ollitex/go/services/web/core"
)

func registerPasswordUpdate(a *core.App) func(*core.Cxt, *core.Res) {
	lim := core.NewRateLimiter(a.Redis, "changePassword", 10, 60)
	respond := func(res *core.Res, code int, body string) {
		res.W.Header().Set("Content-Type", "application/json; charset=utf-8")
		res.W.WriteHeader(code)
		_, _ = io.WriteString(res.W, body)
	}

	return func(cxt *core.Cxt, res *core.Res) {
		if lim != nil && !lim.Consume(core.ClientIP(cxt.Req)) {
			core.Send429(res, "Rate limit reached, please try again later")
			return
		}

		var in struct {
			CurrentPassword string `json:"currentPassword"`
			NewPassword1    string `json:"newPassword1"`
			NewPassword2    string `json:"newPassword2"`
		}
		body, rerr := io.ReadAll(io.LimitReader(cxt.Req.Body, 1<<20))
		if rerr != nil {
			respond(res, 400, `{"message":"Bad Request"}`)
			return
		}
		if len(bytes.TrimSpace(body)) > 0 {
			if jerr := json.Unmarshal(body, &in); jerr != nil {
				respond(res, 400, `{"message":"Bad Request"}`)
				return
			}
		}

		// requireLogin is the global web gate (route has no NoLogin); a
		// logged-in session still missing a passport user 401s (Node
		// requirePermission with no user).
		_, uid := core.PassportUser(cxt.Sess)
		if uid == "" {
			respond(res, 401, `{"message":"Bad Request"}`)
			return
		}
		uidObj, oerr := bson.ObjectIDFromHex(uid)
		if oerr != nil {
			respond(res, 401, `{"message":"Bad Request"}`)
			return
		}
		if in.NewPassword1 != in.NewPassword2 {
			respond(res, 400, `{"message":"New passwords do not match."}`)
			return
		}

		ctx, cancel := context.WithTimeout(cxt.Req.Context(), 20*time.Second)
		defer cancel()
		db, err := a.Mongo.DB(ctx)
		if err != nil {
			respond(res, 500, `{"message":"An error has occurred while performing your request."}`)
			return
		}
		var userDoc struct {
			ObjectID       bson.ObjectID `bson:"_id"`
			Email          string        `bson:"email"`
			HashedPassword string        `bson:"hashedPassword"`
		}
		if err := db.Collection("users").FindOne(ctx, bson.M{"_id": uidObj}).Decode(&userDoc); err != nil {
			respond(res, 500, `{"message":"An error has occurred while performing your request."}`)
			return
		}

		// authenticate(currentPassword): bcrypt compare.
		if userDoc.HashedPassword == "" ||
			bcrypt.CompareHashAndPassword([]byte(userDoc.HashedPassword), []byte(sanitizePw(in.CurrentPassword))) != nil {
			respond(res, 400, `{"message":"Current password is incorrect."}`)
			return
		}
		// CE must-differ (PasswordMustBeDifferentError → 400).
		if in.NewPassword1 == in.CurrentPassword {
			respond(res, 400, `{"message":"The password must be different."}`)
			return
		}
		// validatePassword (CE order) → InvalidPasswordError 400 shape.
		switch code, text := validatePassword(sanitizePw(in.NewPassword1), userDoc.Email); code {
		case 1:
			respond(res, 400, `{"message":{"type":"error","key":"too-short","text":"`+text+`"}}`)
		case 2:
			respond(res, 400, `{"message":{"type":"error","key":"too-long","text":"`+text+`"}}`)
		case 3:
			respond(res, 400, `{"message":{"type":"error","key":"invalid-character","text":"`+text+`"}}`)
		case 4:
			respond(res, 400, `{"message":{"type":"error","key":"no-email","text":"`+text+`"}}`)
		}

		hash, herr := bcryptHash(in.NewPassword1, bcryptRounds())
		if herr != nil {
			respond(res, 500, `{"message":"An error has occurred while performing your request."}`)
			return
		}
		ures, uerr := db.Collection("users").UpdateOne(ctx, bson.M{"_id": userDoc.ObjectID}, bson.M{
			"$set":   bson.M{"hashedPassword": hash},
			"$unset": bson.M{"password": true},
		})
		if uerr != nil || ures == nil || ures.MatchedCount == 0 {
			respond(res, 500, `{"message":"An error has occurred while performing your request."}`)
			return
		}

		// audit 'update-password' (Node: userAuditLogEntries init).
		now := time.Now().UTC()
		_, _ = db.Collection("userAuditLogEntries").InsertOne(ctx, bson.M{
			"userId":    userDoc.ObjectID,
			"operation": "update-password",
			"initiator": userDoc.ObjectID,
			"ip":        core.ClientIP(cxt.Req),
			"createdAt": now,
			"updatedAt": now,
		})

		// expire the user's 'password' tokens (reset links die on change).
		if tdb, terr := a.Mongo.DB(ctx); terr == nil {
			_, _ = tdb.Collection("tokens").UpdateMany(ctx,
				bson.M{"use": "password", "data.user_id": uid, "usedAt": bson.M{"$exists": false}},
				bson.M{"$set": bson.M{"usedAt": now}})
		}

		// removeSessionsFromRedis(user, EXCLUDING the requesting session):
		// every sess:* doc whose passport.user._id/user._id matches.
		if cxt.Sess != nil && a.Redis != nil {
			if keys, serr := a.Redis.SCAN("sess:*", 500); serr == nil {
				needle := []byte(uid)
				for _, k := range keys {
					if cxt.Sess.SessID != "" && strings.HasSuffix(k, cxt.Sess.SessID) {
						continue // keep the current session alive
					}
					val, ok, gerr := a.Redis.GET(k)
					if !ok || gerr != nil || !bytes.Contains([]byte(val), needle) {
						continue
					}
					_ = a.Redis.DEL(k)
				}
			}
		}

		respond(res, 200, `{"message":{"type":"success","email":"`+userDoc.Email+`","text":"Your password has been changed successfully."}}`)
	}
}
