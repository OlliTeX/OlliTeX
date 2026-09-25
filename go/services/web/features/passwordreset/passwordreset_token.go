package passwordreset

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/url"
	"ollitex/go/services/web/core"
	"ollitex/go/services/web/features/emailtemplates"
	"regexp"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"golang.org/x/crypto/bcrypt"
)

func generateAndEmailResetToken(cxt *core.Cxt, a *core.App, mail *core.Mail, tok *core.OneTimeTokens, email string) (string, error) {
	ctx, cancel := context.WithTimeout(cxt.Req.Context(), 10*time.Second)
	defer cancel()
	db, err := a.Mongo.DB(ctx)
	if err != nil {
		return "none", nil
	}
	var userDoc bson.M
	err = db.Collection("users").FindOne(ctx, bson.M{"$or": bson.A{
		bson.M{"email": email},
		bson.M{"emails.email": email},
	}}).Decode(&userDoc)
	if err != nil && err != mongo.ErrNoDocuments {
		return "none", nil
	}
	if err != nil {
		return "none", nil
	}
	primary, _ := userDoc["email"].(string)
	if primary != email {
		return "secondary", nil
	}
	userID := ""
	switch v := userDoc["_id"].(type) {
	case primitive.ObjectID:
		userID = v.Hex()
	}
	token, err := tok.New(ctx, "password", bson.M{"user_id": userID, "email": email})
	if err != nil {
		return "none", nil
	}
	// send the reset email (CE: EmailBuilder passwordResetRequested) —
	// now through the /hub-managed template registry (emailtemplates
	// "password-reset"; owner-rebranded default identical at app=OlliTeX)
	siteURL := cxt.SiteURL
	if siteURL == "" {
		siteURL = "http://localhost"
	}
	link := siteURL + "/user/password/set?passwordResetToken=" + token + "&email=" + url.QueryEscape(email)
	if mail != nil {
		tmpl, tmErr := emailtemplates.RenderFor(a, "password-reset",
			map[string]string{"app": appName(), "link": link})
		if tmErr != nil {
			log.Printf("webgo: password-reset mail render: %v", tmErr)
		} else {
			_ = mail.Send(email, tmpl.Subject, tmpl.Text, tmpl.HTML) // delivery failure → Node would 500; battery compares healthy sinks
		}
	}
	return "primary", nil
}

// setNewPassword is the POST /user/password/set handler constructor.
func setNewPassword(a *core.App, mail *core.Mail, tok *core.OneTimeTokens) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		body, _ := io.ReadAll(io.LimitReader(cxt.Req.Body, 1<<20))
		var in struct {
			Email              string `json:"email"`
			Password           string `json:"password"`
			PasswordResetToken string `json:"passwordResetToken"`
		}
		// Node contract (pinned live): zod first — a MISSING required key
		// answers 400 with the zod issue string; a present-but-empty value
		// hits the handler's invalid-password branch. Pointer map
		// distinguishes absence from null/empty.
		var raw map[string]*string
		if json.Unmarshal(body, &raw) == nil {
			if raw["password"] == nil {
				res.W.Header().Set("Content-Type", "application/json; charset=utf-8")
				res.W.WriteHeader(400)
				_, _ = io.WriteString(res.W, `{"error":"Validation error: Invalid input: expected string, received undefined at \"body.password\"","statusCode":400}`)
				return
			}
			if raw["passwordResetToken"] == nil {
				res.W.Header().Set("Content-Type", "application/json; charset=utf-8")
				res.W.WriteHeader(400)
				_, _ = io.WriteString(res.W, `{"error":"Validation error: Invalid input: expected string, received undefined at \"body.passwordResetToken\"","statusCode":400}`)
				return
			}
		}
		_ = json.Unmarshal(body, &in)
		if in.PasswordResetToken == "" || in.Password == "" {
			res.W.Header().Set("Content-Type", "application/json; charset=utf-8")
			res.W.WriteHeader(400)
			_, _ = io.WriteString(res.W, `{"message":{"key":"invalid-password"}}`)
			return
		}

		email := parseEmail(in.Email)
		if code, text := validatePassword(in.Password, email); code != 0 {
			key := "password-too-short"
			switch code {
			case 2:
				key = "password-too-long"
			case 3:
				key = "password-invalid-character"
			case 4:
				key = "password-contains-email"
			case 5:
				key = "password-not-set"
			}
			res.W.Header().Set("Content-Type", "application/json; charset=utf-8")
			res.W.WriteHeader(400)
			_, _ = fmt.Fprintf(res.W, `{"message":{"type":"error","key":%q,"text":%q}}`, key, text)
			return
		}
		token := strings.TrimSpace(in.PasswordResetToken)

		ctx, cancel := context.WithTimeout(cxt.Req.Context(), 12*time.Second)
		defer cancel()
		db, err := a.Mongo.DB(ctx)
		if err != nil {
			respondJSON(res, 500, `{"message":"An error has occurred while performing your request."}`)
			return
		}

		// getUserForPasswordResetToken (peek + user by main email + match)
		data, remainingPeeks, err := tok.Peek(ctx, "password", token)
		if err != nil || remainingPeeks <= 0 {
			respondJSON(res, 404, `{"message":{"key":"token-expired"}}`)
			return
		}
		dataEmail, _ := data["email"].(string)
		var userDoc struct {
			ObjectID primitive.ObjectID `bson:"_id"`
			Email    string             `bson:"email"`
		}
		umatch := false
		if e := db.Collection("users").FindOne(ctx, bson.M{"$or": bson.A{
			bson.M{"email": dataEmail},
			bson.M{"emails.email": dataEmail},
		}}).Decode(&userDoc); e == nil {
			umatch = true
			if uid := core.ObjectIdHex(data["user_id"]); uid != "" {
				if uid != userDoc.ObjectID.Hex() {
					umatch = false
				}
			}
		}
		if !umatch {
			respondJSON(res, 404, `{"message":{"key":"token-expired"}}`)
			return
		}
		_ = userDoc.Email

		// validate vs CURRENT password (must be different)
		var curDoc struct {
			Hash string `bson:"hashedPassword"`
		}
		_ = db.Collection("users").FindOne(ctx, bson.M{"_id": userDoc.ObjectID}).Decode(&curDoc)
		if curDoc.Hash != "" && bcrypt.CompareHashAndPassword([]byte(curDoc.Hash), []byte(sanitizePw(in.Password))) == nil {
			respondJSON(res, 400, `{"message":{"key":"password-must-be-different"}}`)
			return
		}

		// audit entry 'reset-password' (initiatorId may be absent — CE allows)
		initiator := ""
		if cxt.Sess != nil {
			_, uid := core.PassportUser(cxt.Sess)
			initiator = uid
		}
		newInit := bson.M{
			"userId":    userDoc.ObjectID,
			"operation": "reset-password",
			"ip":        core.ClientIP(cxt.Req),
			"info":      bson.M{"token": token[:min(4, len(token))]},
			"createdAt": time.Now().UTC(),
			"updatedAt": time.Now().UTC(),
		}
		if initiator != "" {
			newInit["initiatorId"] = initiator
		}
		_, _ = db.Collection("userAuditLogEntries").InsertOne(ctx, newInit)

		// _setUserPasswordInMongo: $set hashedPassword, $unset password
		hash, herr := bcryptHash(sanitizePw(in.Password), bcryptRounds())
		if herr != nil {
			respondJSON(res, 500, `{"message":"An error has occurred while performing your request."}`)
			return
		}
		upd := bson.M{
			"$set":   bson.M{"hashedPassword": hash},
			"$unset": bson.M{"password": true},
		}
		if _, uerr := db.Collection("users").UpdateOne(ctx, bson.M{"_id": userDoc.ObjectID}, upd); uerr != nil {
			respondJSON(res, 500, `{"message":"An error has occurred while performing your request."}`)
			return
		}
		// expire the token (usedAt)
		tok.Expire(ctx, "password", token)

		// removeSessionsFromRedis(user): drop every session of that user
		removeUserSessions(a, userDoc.ObjectID.Hex())

		// Node success path: res.sendStatus(200) → 200, body "OK",
		// content-type text/plain (pinned live: Node answers text/plain, not html).
		res.W.Header().Set("Content-Type", "text/plain; charset=utf-8")
		res.W.WriteHeader(200)
		_, _ = io.WriteString(res.W, "OK")
	}
}

// removeUserSessions scans the redis keyspace and dels session docs whose
// passport.user._id or user._id matches (Node UserSessionsManager parity).
func removeUserSessions(a *core.App, userIDHex string) {
	if a == nil || a.Redis == nil {
		return
	}
	keys, err := a.Redis.SCAN("sess:*", 500)
	if err != nil {
		return
	}
	re := regexp.MustCompile(`"_"?\s*[:=]\s*["\']?` + regexp.QuoteMeta(userIDHex))
	uidRe := regexp.MustCompile(`["_']?_id["']?\s*[:=]\s*["\']?([0-9a-f]{24})`)
	for _, k := range keys {
		val, ok, gerr := a.Redis.GET(k)
		if !ok || gerr != nil {
			continue
		}
		if m := uidRe.FindStringSubmatch(val); m != nil {
			if m[1] == userIDHex {
				_ = a.Redis.DEL(k)
			}
			continue
		}
		if re.MatchString(val) {
			_ = a.Redis.DEL(k)
		}
	}
}
