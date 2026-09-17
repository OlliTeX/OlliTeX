package mendeley

import (
	"context"
	"errors"
	"os"
	"strings"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"

	"ollitex/go/services/web/core"
)

// mendeleySettings — Node getMendeleySettings (MendeleySection.mjs):
//
//	section = getSection('mendeley', Settings)   (stored site_settings wins;
//	                                              may throw → fallback)
//	enabled      = Boolean(section?.enabled ?? true)
//	clientId     = section?.clientId || Settings.mendeley?.clientID || ''
//	clientSecret = section?.clientSecret || ''      (decrypted at rest; a
//	                                              stored '' is falsy → '')
//	callbackURL  = Settings.mendeley?.callbackURL || '/user/mendeley'
//
// The Node `Settings.mendeley` seed comes from the MENDELEY_* env
// (settings.js / genScript) — read the same env in Go.
type mendeleySettings struct {
	Enabled      bool
	ClientID     string
	ClientSecret string
	CallbackURL  string
}

func envNonEmpty(k string) string {
	return strings.TrimSpace(os.Getenv(k))
}

func mendeleyEnvSeed() (clientID, callback string) {
	clientID = envNonEmpty("MENDELEY_CLIENT_ID")
	callback = envNonEmpty("MENDELEY_CALLBACK_URL")
	return
}

func getMendeleySettings(ctx context.Context, a *core.App) (mendeleySettings, error) {
	seedID, seedCB := mendeleyEnvSeed()
	fb := mendeleySettings{
		Enabled:      true,
		ClientID:     seedID,
		ClientSecret: "",
		CallbackURL:  orDefault(seedCB, "/user/mendeley"),
	}
	if a.Mongo == nil {
		return fb, nil
	}
	db, err := a.Mongo.DB(ctx)
	if err != nil {
		return fb, err
	}
	var doc struct {
		Mendeley any `bson:"mendeley"`
	}
	err = db.Collection("site_settings").FindOne(ctx,
		bson.D{{Key: "_id", Value: "global"}}).Decode(&doc)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return fb, nil
		}
		return fb, err
	}
	if doc.Mendeley == nil {
		return fb, nil
	}
	var sec map[string]any
	switch v := doc.Mendeley.(type) {
	case primitive.D:
		tmp := map[string]any{}
		for _, kv := range v {
			tmp[kv.Key] = kv.Value
		}
		sec = tmp
	case primitive.M:
		sec = v
	case map[string]any:
		sec = v
	default:
		return fb, nil // section not an object → Node section?.x → undefined
	}
	s := fb // start from the env seed; stored values win where truthy
	if val, ok2 := sec["enabled"]; ok2 {
		if b, ok3 := val.(bool); ok3 {
			s.Enabled = b
		}
	}
	if val, ok2 := sec["clientId"]; ok2 {
		if str, ok3 := val.(string); ok3 && str != "" {
			s.ClientID = str
		}
	}
	if val, ok2 := sec["clientSecret"]; ok2 {
		if str, ok3 := val.(string); ok3 && str != "" {
			s.ClientSecret = str
		}
	}
	return s, nil
}

func orDefault(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

func isServiceConfigured(m mendeleySettings) bool {
	return m.ClientID != "" && m.ClientSecret != ""
}
