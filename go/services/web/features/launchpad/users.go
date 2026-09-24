package launchpad

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"strconv"
	"strings"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"golang.org/x/crypto/bcrypt"

	"ollitex/go/services/web/features/registrationpage"
)

// ---- random helpers (same shapes as registrationpage) --------------------

func randomUUID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "00000000-0000-0000-0000-000000000000"
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	h := hex.EncodeToString
	return h(b[0:4]) + "-" + h(b[4:6]) + "-" + h(b[6:8]) + "-" +
		h(b[8:10]) + "-" + h(b[10:16])
}

func bcryptRounds() int {
	if v := os.Getenv("BCRYPT_ROUNDS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 4 {
			return n
		}
	}
	return 12
}

// reversedHostname — Node: email.split('@')[1].split(”).reverse().join(”).
func reversedHostname(email string) string {
	i := strings.LastIndex(email, "@")
	hostname := email
	if i >= 0 && i+1 < len(email) {
		hostname = email[i+1:]
	}
	r := []rune(hostname)
	for a, b := 0, len(r)-1; a < b; a, b = a+1, b-1 {
		r[a], r[b] = r[b], r[a]
	}
	return string(r)
}

// ---- mongo primitives ------------------------------------------------------

// adminExists mirrors _atLeastOneAdminExists: any user with isAdmin:true.
func adminExists(ctx context.Context, db *mongo.Database) (bool, error) {
	var doc bson.M
	err := db.Collection("users").FindOne(ctx, bson.M{"isAdmin": true}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// userByEmail mirrors UserGetter.promises.getUserByAnyEmail
// (`email` field or any `emails[].email` entry).
func userByEmail(ctx context.Context, db *mongo.Database, email string) (bson.M, error) {
	var doc bson.M
	err := db.Collection("users").FindOne(ctx, bson.M{"$or": bson.A{
		bson.M{"email": email},
		bson.M{"emails.email": email},
	}}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return doc, nil
}

// ---- user creation (UserRegistrationHandler.registerNewUser family) -------

// ErrEmailAlreadyRegistered — Node throws OError('EmailAlreadyRegistered')
// when the email belongs to a non-holding user (registerNewUser); the
// launchpad controller does NOT catch it → express error middleware →
// 500 general/500 view (pinned path).
var errEmailAlreadyRegistered = errors.New("EmailAlreadyRegistered")

// createLocalAdminUser — Node: registerNewUser({email, password,
// analyticsId}) then User.updateOne({_id}, {$set:{isAdmin:true,
// emails:[{email, reversedHostname}]}}). Final document (mongoose shapes
// included): the Node-parity baseline doc (registrationpage.NewUserDoc —
// the same 42 static defaults every user-creation path stores) plus
// email / first_name (default: local part — createNewUser) /
// holdingAccount:false / analyticsId / hashedPassword (bcrypt) /
// emails[0] {email, createdAt, reversedHostname, _id} and isAdmin:true.
// last_name is ABSENT (userDetails does not carry it — local path).
func createLocalAdminUser(ctx context.Context, db *mongo.Database, email, password, analyticsID string) error {
	doc := registrationpage.NewUserDoc()
	doc["email"] = email
	doc["first_name"] = strings.SplitN(email, "@", 2)[0]
	doc["analyticsId"] = analyticsID
	doc["holdingAccount"] = false
	doc["isAdmin"] = true
	hash, herr := bcrypt.GenerateFromPassword([]byte(password), bcryptRounds())
	if herr != nil {
		return herr
	}
	doc["hashedPassword"] = string(hash)
	doc["emails"] = bson.A{bson.M{
		"email":            email,
		"reversedHostname": reversedHostname(email),
		"createdAt":        nowUTC(),
		"_id":              primitive.NewObjectID(),
	}}
	if _, ierr := db.Collection("users").InsertOne(ctx, doc); ierr != nil {
		return ierr
	}
	return nil
}

// createExternalAdminUser — Node: registerExternalAuthAdmin's create path
// (authMethod 'ldap' here): userDetails {email, password: randomHex64,
// first_name: email, last_name: ”, analyticsId} → createNewUser →
// updateOne $set {isAdmin:true, emails:[{email, reversedHostname,
// confirmedAt: Date.now()}]} + $unset hashedPassword. Final document =
// baseline + email / first_name (the FULL email — controller sets it) /
// last_name ("" — stored empty) / analyticsId / emails[0] {email,
// reversedHostname, confirmedAt (epoch ms), _id, createdAt} /
// isAdmin:true, NO hashedPassword.
func createExternalAdminUser(ctx context.Context, db *mongo.Database, email, analyticsID string) error {
	doc := registrationpage.NewUserDoc()
	doc["email"] = email
	doc["first_name"] = email
	doc["last_name"] = ""
	doc["analyticsId"] = analyticsID
	doc["holdingAccount"] = false
	doc["isAdmin"] = true
	doc["emails"] = bson.A{bson.M{
		"email":            email,
		"reversedHostname": reversedHostname(email),
		"confirmedAt":      timeNowMillis(),
		"createdAt":        nowUTC(),
		"_id":              primitive.NewObjectID(),
	}}
	if _, ierr := db.Collection("users").InsertOne(ctx, doc); ierr != nil {
		return ierr
	}
	return nil
}
