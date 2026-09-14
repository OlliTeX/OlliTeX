package registrationpage

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"ollitex/go/services/web/core"
	"os"
	"strconv"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"golang.org/x/crypto/bcrypt"
)

// doRegister — creates the user (or reuses a holding account), hashes the
// random password, mints the 7-day token and sends the activation mail.
func doRegister(a *core.App, mail *core.Mail, tok *core.OneTimeTokens, cxt *core.Cxt, email, first, last string) (out string) {
	defer func() {
		if out == "" {
			out = "error"
		}
	}()
	ctx, cancel := context.WithTimeout(cxt.Req.Context(), 12*time.Second)
	defer cancel()
	db, err := a.Mongo.DB(ctx)
	if err != nil {
		return "error"
	}
	coll := db.Collection("users")

	// existing? (Node getUserByAnyEmail: `email` or in `emails`)
	var ex bson.M
	err = coll.FindOne(ctx, bson.M{"$or": bson.A{
		bson.M{"email": email},
		bson.M{"emails.email": email},
	}}).Decode(&ex)
	if err != nil && err != mongo.ErrNoDocuments {
		return "error"
	}

	reuse := false
	reuseID := primitive.NilObjectID
	if err == nil {
		if holdingAccountFalse(ex) {
			return "taken"
		}
		// holding account (holdingAccount true) — Node reuses the existing user
		reuse = true
		if o, ok := ex["_id"].(primitive.ObjectID); ok {
			reuseID = o
		} else {
			return "error"
		}
	}

	return createAndMail(ctx, a, mail, tok, cxt, coll, reuse, reuseID, email, first, last)
}

func createAndMail(ctx context.Context, a *core.App, mail *core.Mail, tok *core.OneTimeTokens, cxt *core.Cxt, coll *mongo.Collection, reuse bool, reuseID primitive.ObjectID, email, first, last string) string {
	now := time.Now().UTC()
	pw := randomHex32()
	hash, herr := bcrypt.GenerateFromPassword([]byte(pw), bcryptRounds())
	if herr != nil {
		return "error"
	}
	id := reuseID
	if !reuse {
		id = primitive.NewObjectID()
	}

	doc := newUserDoc() // 42 static Node-parity defaults
	doc["_id"] = id
	doc["email"] = email
	doc["first_name"] = first
	doc["last_name"] = last
	doc["analyticsId"] = randomUUID()
	doc["hashedPassword"] = string(hash)
	doc["signUpDate"] = now
	doc["thirdPartyIdentifiers"] = bson.A{}
	doc["emails"] = bson.A{bson.M{
		"email":            email,
		"reversedHostname": reversedHostname(email),
		"createdAt":        now.Add(time.Millisecond),
		"_id":              primitive.NewObjectID(),
	}}

	if reuse {
		if _, uerr := coll.UpdateOne(ctx, bson.M{"_id": id}, bson.M{"$set": bson.M{
			"hashedPassword": string(hash),
			"first_name":     first,
			"last_name":      last,
		}}); uerr != nil {
			return "error"
		}
	} else {
		if _, ierr := coll.InsertOne(ctx, doc); ierr != nil {
			if mongo.IsDuplicateKeyError(ierr) {
				return "taken"
			}
			return "error"
		}
	}

	linkToken, terr := tok.NewWithExp(ctx, "password", bson.M{"user_id": id.Hex(), "email": email}, now.Add(oneWeekSec*time.Second))
	if terr != nil {
		return "error"
	}

	site := cxt.SiteURL
	if site == "" {
		site = "http://localhost"
	}
	link := site + "/user/activate?token=" + linkToken + "&user_id=" + id.Hex()
	subject := "Activate your " + appName() + " Account"
	text := "Hi,\n\n" +
		"Congratulations, you've just had an account created for you on " + appName() +
		" with the email address '" + email + "'.\n\n" +
		"Click here to set your password and log in:\n\n" +
		"Set password: " + link + "\n\n" +
		"If you have any questions or problems, please contact " + adminEmail() + "\n\n" +
		"Regards,\nThe " + appName() + " Team - " + site + "\n"
	html := `<p>Hi,</p>` +
		`<p>Congratulations, you've just had an account created for you on ` + appName() +
		` with the email address '` + email + `'.</p>` +
		`<p>Click here to set your password and log in:</p>` +
		`<p><a href="` + link + `">Set password</a></p>` +
		`<p>If you have any questions or problems, please contact ` + adminEmail() + `</p>` +
		`<p>Regards,<br/>The ` + appName() + ` Team - ` + site + `</p>`

	if mail != nil {
		if serr := mail.Send(email, subject, text, html); serr != nil {
			_, _ = coll.DeleteOne(ctx, bson.M{"_id": id})
			return "mailfail"
		}
	}
	return "success"
}

// ---- sign-up site-settings (stored section over env seeds; fail Open) ----

func holdingAccountFalse(doc bson.M) bool {
	h, ok := doc["holdingAccount"].(bool)
	return ok && !h
}

func parseEmail(email string) string {
	if email == "" || len(email) > 254 {
		return ""
	}
	s := strings.ToLower(strings.TrimSpace(email))
	if emailRe.MatchString(s) {
		return s
	}
	return ""
}

func randomHex32() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return strings.Repeat("0", 64)
	}
	return hex.EncodeToString(b)
}

func randomUUID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "00000000-0000-0000-0000-000000000000"
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	h := hex.EncodeToString
	return h(b[0:4]) + "-" + h(b[4:6]) + "-" + h(b[6:8]) + "-" + h(b[8:10]) + "-" + h(b[10:16])
}

func appName() string {
	if v := os.Getenv("OVERLEAF_APP_NAME"); v != "" {
		return v
	}
	if v := os.Getenv("OVERLEAF_APPNAME"); v != "" {
		return v
	}
	return "OlliTeX"
}

func adminEmail() string {
	if v := os.Getenv("OVERLEAF_ADMIN_EMAIL"); v != "" {
		return v
	}
	return "placeholder@example.com"
}

func bcryptRounds() int {
	if v := os.Getenv("BCRYPT_ROUNDS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 4 {
			return n
		}
	}
	return 12
}

func decodeToMap(cxt *core.Cxt) map[string]any {
	b, _ := io.ReadAll(io.LimitReader(cxt.Req.Body, 1<<20))
	out := map[string]any{}
	if len(b) == 0 {
		return out
	}
	_ = json.Unmarshal(b, &out)
	return out
}
