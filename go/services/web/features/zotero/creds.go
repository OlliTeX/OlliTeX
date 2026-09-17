package zotero

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"sync"

	"go.mongodb.org/mongo-driver/bson"

	"ollitex/go/services/web/core"
	"ollitex/go/services/web/features/sitesettings"
)

// cipherBridge — the subset of the (unexported) sitesettings cipher type
// needed here. sitesettings.Open() returns *cipher; Go permits using an
// unexported type through an interface without naming it.
type cipherBridge interface {
	EncryptRaw(plain string) (string, error)
	DecryptRaw(tok string) (string, bool)
	DecryptText(tok string) (string, bool)
	EncryptText(plain string) (string, error)
}

var (
	zcipherOnce sync.Once
	zcipher     cipherBridge
	zcipherErr  error
)

func zcipherInst() (cipherBridge, error) {
	zcipherOnce.Do(func() {
		zcipher, zcipherErr = sitesettings.Open()
	})
	return zcipher, zcipherErr
}

// zoteroAPICreds — the decrypted user credentials (Node TokenManager
// .getCredentials): { apiKey, zoteroUserId }.
type zoteroAPICreds struct {
	APIKey       string `json:"apiKey"`
	ZoteroUserID string `json:"zoteroUserId"`
}

// userZoteroCreds reads user.refProviders.zotero.apiKeyEncrypted and
// decrypts it (shared OL_CEP-v3 cipher, RAW JSON-object payload — Node
// AccessTokenEncryptor.encryptJson). Any failure = "not linked" (Node
// treats decrypt errors as not connected).
func userZoteroCreds(ctx context.Context, a *core.App, uid string) (apiKey, zoteroUserID string, linked bool) {
	if a.Mongo == nil || uid == "" {
		return "", "", false
	}
	db, err := a.Mongo.DB(ctx)
	if err != nil {
		return "", "", false
	}
	var u struct {
		RefProviders *struct {
			Zotero *struct {
				APIKeyEncrypted string `bson:"apiKeyEncrypted"`
			} `bson:"zotero"`
		} `bson:"refProviders"`
	}
	if err := db.Collection("users").FindOne(ctx,
		bson.D{{Key: "_id", Value: mustObjectID(uid)}}).
		Decode(&u); err != nil {
		return "", "", false
	}
	if u.RefProviders == nil || u.RefProviders.Zotero == nil {
		return "", "", false
	}
	enc := u.RefProviders.Zotero.APIKeyEncrypted
	if enc == "" {
		return "", "", false
	}
	c, cerr := zcipherInst()
	if cerr != nil || c == nil {
		return "", "", false
	}
	plain, ok2 := c.DecryptRaw(enc)
	if !ok2 {
		return "", "", false
	}
	var creds zoteroAPICreds
	if jerr := json.Unmarshal([]byte(plain), &creds); jerr != nil {
		return "", "", false
	}
	if creds.APIKey == "" {
		return "", "", false
	}
	return creds.APIKey, creds.ZoteroUserID, true
}

// storeCreds — TokenManager.storeCredentials: encrypt
// JSON({apiKey, zoteroUserId:String}) and $set refProviders.zotero.
func storeCreds(ctx context.Context, a *core.App, uid, apiKey, zoteroUserID string) error {
	if a.Mongo == nil || uid == "" {
		return errors.New("cipher unavailable")
	}
	c, cerr := zcipherInst()
	if cerr != nil || c == nil {
		return errors.New("cipher unavailable")
	}
	payload, _ := json.Marshal(zoteroAPICreds{APIKey: apiKey, ZoteroUserID: zoteroUserID})
	enc, err := c.EncryptRaw(string(payload))
	if err != nil {
		return err
	}
	db, err := a.Mongo.DB(ctx)
	if err != nil {
		return err
	}
	_, err = db.Collection("users").UpdateOne(ctx,
		bson.D{{Key: "_id", Value: mustObjectID(uid)}},
		bson.D{{Key: "$set", Value: bson.D{
			{Key: "refProviders.zotero", Value: bson.D{{Key: "apiKeyEncrypted", Value: enc}}},
		}}})
	return err
}

// unsetUserZotero — Node unlinkAccount's doc step: $unset refProviders.zotero.
func unsetUserZotero(ctx context.Context, a *core.App, uid string) error {
	if a.Mongo == nil || uid == "" {
		return nil
	}
	db, err := a.Mongo.DB(ctx)
	if err != nil {
		return err
	}
	_, err = db.Collection("users").UpdateOne(ctx,
		bson.D{{Key: "_id", Value: mustObjectID(uid)}},
		bson.D{{Key: "$unset", Value: bson.D{{Key: "refProviders.zotero", Value: 1}}}})
	return err
}

// ---------- session: req.session.zoteroOAuth ----------

type zoteroOAuthSaved struct {
	token       string
	tokenSecret string
	isPopup     bool
}

func sessionGetMap(s *core.Session, key string) map[string]any {
	if s == nil {
		return nil
	}
	raw, ok := s.Doc[key]
	if !ok || len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var m map[string]any
	if json.Unmarshal(raw, &m) != nil {
		return nil
	}
	return m
}

// popZoteroOAuth — Node `const saved = req.session.zoteroOAuth;
// delete req.session.zoteroOAuth`.
func popZoteroOAuth(cxt *core.Cxt) *zoteroOAuthSaved {
	m := sessionGetMap(cxt.Sess, "zoteroOAuth")
	if cxt.Sess != nil {
		cxt.Sess.Del("zoteroOAuth") // persist-on-response, like the Node delete
	}
	if m == nil {
		return nil
	}
	out := &zoteroOAuthSaved{}
	if v, ok := m["token"].(string); ok {
		out.token = v
	}
	if v, ok := m["tokenSecret"].(string); ok {
		out.tokenSecret = v
	}
	if v, ok := m["isPopup"].(bool); ok {
		out.isPopup = v
	}
	return out
}

// sendOAuthCallbackHTML — Node oauthCallback's res.send(html) with a fresh
// nonce (volatile — NOT part of the gate parity surface; only reached after
// a successful live token exchange).
func sendOAuthCallbackHTML(res *core.Res, isPopup bool) {
	nonceBytes := make([]byte, 16)
	_, _ = rand.Read(nonceBytes)
	nonce := base64.StdEncoding.EncodeToString(nonceBytes)
	action := "location.href = '/user/settings#references'"
	if isPopup {
		action = "window.close()"
	}
	body := "\n" +
		"    <!doctype html>\n" +
		"    <html>\n" +
		"      <body>\n" +
		"        <script nonce=\"" + nonce + "\">\n" +
		"          const channel = new BroadcastChannel('zotero')\n" +
		"          channel.postMessage({ type: 'zotero-linked' })\n" +
		"          " + action + "\n" +
		"        </script>\n" +
		"      </body>\n" +
		"    </html>\n" +
		"  "
	res.HTML(200, body)
}
