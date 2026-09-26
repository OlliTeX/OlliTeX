package sitesettings

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"ollitex/go/services/web/core"
)

func asBool(v any) bool  { b, _ := v.(bool); return b }
func asStr(v any) string { s, _ := v.(string); return s }

func removeKey(o Obj, key string) Obj {
	out := Obj{}
	for _, kv := range o {
		if kv.Key != key {
			out = append(out, kv)
		}
	}
	return out
}

// resolveConnSecret — the zotero/mendeley dedicated resolution
// (stored-encrypted wins; env seed second).
func resolveConnSecret(stored, seeds Obj, c *cipher, field, envKey string) string {
	if v, ok := ObjGet(stored, field); ok {
		if s, ok2 := v.(string); ok2 && s != "" {
			if dec, ok3 := c.decryptText(s); ok3 && dec != "" {
				return dec
			}
			if asBool(ObjGetD(seeds, "hasEnvSecret")) {
				return rawEnv(envKey)
			}
			return ""
		}
	}
	if asBool(ObjGetD(seeds, "hasEnvSecret")) {
		return rawEnv(envKey)
	}
	return ""
}

func ObjGetD(o Obj, key string) any {
	v, _ := ObjGet(o, key)
	return v
}

func rawEnv(k string) string { return envVarOr(k, "") }

// resolveSSOSecret — the generalized SSO loop (stored-encrypted wins;
// seed value second; "" otherwise).
func resolveSSOSecret(stored, seeds Obj, c *cipher, field string) string {
	resolved := ""
	if v, ok := ObjGet(stored, field); ok {
		if s, ok2 := v.(string); ok2 && s != "" {
			if dec, ok3 := c.decryptText(s); ok3 {
				resolved = dec
			}
		}
	}
	if resolved == "" {
		if sv, ok := ObjGet(seeds, field); ok {
			resolved = asStr(sv)
		}
	}
	return resolved
}

// mergeTemplates — getSection's per-key category merge.
func mergeTemplates(merged, seeds, stored Obj) Obj {
	svSeeds, _ := ObjGet(seeds, "categories")
	seedArr, _ := svSeeds.([]any)
	svStored, _ := ObjGet(stored, "categories")
	storedArr, _ := svStored.([]any)
	seedById := map[string]Obj{}
	order := []string{}
	for _, e := range seedArr {
		c, _ := e.(Obj)
		k := asStr(ObjGetD(c, "key"))
		seedById[k] = c
		if k != "" {
			order = append(order, k)
		}
	}
	storedById := map[string]Obj{}
	for _, e := range storedArr {
		c, _ := e.(Obj)
		k := asStr(ObjGetD(c, "key"))
		storedById[k] = c
		found := false
		for _, x := range order {
			if x == k {
				found = true
				break
			}
		}
		if k != "" && !found {
			order = append(order, k)
		}
	}
	out := make([]any, 0, len(order))
	for _, k := range order {
		m := MergeOrdered(seedById[k], storedById[k])
		m = ObjSet(m, "key", k)
		out = append(out, m)
	}
	merged = ObjSet(merged, "categories", out)
	merged = removeKey(merged, "publishable")
	return merged
}

// getSection — envSeeds + stored merge + per-section secret resolution.
func getSection(name string, sections map[string]Obj, c *cipher) Obj {
	seeds := seedObj(name)
	stored := sections[name]
	if stored == nil {
		stored = Obj{}
	}
	merged := MergeOrdered(seeds, stored)

	switch name {
	case "templates":
		merged = mergeTemplates(merged, seeds, stored)
	case "zotero":
		merged = ObjSet(merged, "clientSecret", resolveConnSecret(stored, seeds, c, "clientSecret", "ZOTERO_CLIENT_SECRET"))
		merged = removeKey(merged, "hasEnvSecret")
	case "mendeley":
		merged = ObjSet(merged, "clientSecret", resolveConnSecret(stored, seeds, c, "clientSecret", "MENDELEY_CLIENT_SECRET"))
		merged = removeKey(merged, "hasEnvSecret")
	}

	// Generalized SSO secret resolution — applies to every secret section
	// EXCEPT zotero (which handled its env fallback above and is excluded).
	if name != "zotero" {
		for _, f := range SECRET_FIELDS[name] {
			merged = ObjSet(merged, f, resolveSSOSecret(stored, seeds, c, f))
		}
	}
	return merged
}

// maskSecrets — set each secret field to ” (in place) and append
// <field>Set:boolean in SECRET_FIELDS order.
func maskSecrets(name string, section Obj) Obj {
	out := section
	for _, f := range SECRET_FIELDS[name] {
		hv := false
		if v, ok := ObjGet(out, f); ok {
			if s, ok2 := v.(string); ok2 && s != "" {
				hv = true
			}
		}
		out = ObjSet(out, f, "")
		out = append(out, KV{f + "Set", hv})
	}
	return out
}

// getSectionMasked — getSection + maskSecrets (the per-section value that
// res.json emits for every non-storage section).
func getSectionMasked(name string, sections map[string]Obj, c *cipher) Obj {
	// zotero/mendeley/SSO: mask uses the resolved plaintext to set <f>Set.
	return maskSecrets(name, getSection(name, sections, c))
}

// storageSectionOut — the controller's special storage GET shape:
// maskedStored fields, backend default, envManaged, appliesOn (envPath
// omitted to match the running build).
func storageSectionOut(name string, sections map[string]Obj, c *cipher) Obj {
	storage := getSection("storage", sections, c)
	masked := maskSecrets("storage", storage)
	hasStored := len(masked) > 0
	var base Obj
	if hasStored {
		base = masked
	} else {
		// stored empty → fall back to the managed env fragment's section.
		se := storageEnvSection()
		if se != nil {
			base = se
		} else {
			base = Obj{}
		}
	}
	out := Obj{}
	for _, kv := range base {
		out = append(out, kv)
	}
	if b := asStr(ObjGetD(base, "backend")); b == "" {
		out = ObjSet(out, "backend", "fs")
	} else {
		out = ObjSet(out, "backend", b)
	}
	if !hasStored {
		out = removeKey(out, "s3Secret") // Node: storageOut.s3Secret = undefined
	}
	out = append(out, KV{"envManaged", storageEnvExists()})
	out = append(out, KV{"appliesOn", "next container restart"})
	return out
}

// cleanSectionInput — preserve INPUT order ∩ known-keys.
func cleanSectionInput(name string, in Obj) (Obj, bool) {
	allowed, ok := SECTION_KNOWN_KEYS[name]
	if !ok || in == nil {
		return in, ok
	}
	allow := map[string]bool{}
	for _, k := range allowed {
		allow[k] = true
	}
	out := Obj{}
	for _, kv := range in {
		if allow[kv.Key] {
			out = append(out, kv)
		}
	}
	return out, true
}

// setSection — encrypt secrets (empty → keep stored), drop *Set, upsert
// the doc + updatedAt, then a best-effort snapshot.
func setSection(a *core.App, ctx context.Context, name string, value Obj, c *cipher) (bool, bool, error) {
	if a.Mongo == nil {
		return false, false, errNoDB
	}
	db, err := a.Mongo.DB(ctx)
	if err != nil {
		return false, false, err
	}
	stored := loadStoredSection(ctx, db, name)
	next := Obj{}
	for _, kv := range value {
		next = append(next, kv)
	}
	if v, ok := ObjGet(stored, "publishable"); ok && name == "misc" {
		if _, present := ObjGet(next, "publishable"); !present {
			next = ObjSet(next, "publishable", v)
		}
	}
	for _, f := range SECRET_FIELDS[name] {
		incoming, _ := ObjGet(next, f)
		s, okstr := incoming.(string)
		if !okstr || s == "" {
			kept, _ := ObjGet(stored, f)
			next = ObjSet(next, f, asStr(kept))
		} else {
			enc, err := c.encryptText(s)
			if err != nil {
				return false, false, err
			}
			next = ObjSet(next, f, enc)
		}
		next = removeKey(next, f+"Set")
	}
	update := bson.D{
		{Key: name, Value: objToBson(next)},
		{Key: "updatedAt", Value: time.Now()},
	}
	res, err := db.Collection("site_settings").UpdateOne(ctx,
		bson.D{{Key: "_id", Value: "global"}},
		bson.D{{Key: "$set", Value: update}},
		options.Update().SetUpsert(true))
	if err != nil {
		return false, false, err
	}
	writeSnapshot(ctx, db)
	return res.UpsertedCount == 1, res.ModifiedCount == 1, nil
}

var errNoDB = bsonErr("SiteSettings: database unavailable")

type bsonErr string

func (e bsonErr) Error() string { return string(e) }

// loadStoredSection reads one stored section (ordered) for setSection's
// "keep existing secret" path.
func loadStoredSection(ctx context.Context, db *mongo.Database, name string) Obj {
	sections := loadAllSections(ctx, db)
	return sections[name]
}

// objectToBson + anyToBson — ordered write-back.
func objToBson(o Obj) bson.D {
	d := bson.D{}
	for _, kv := range o {
		d = append(d, bson.E{Key: kv.Key, Value: anyToBson(kv.Val)})
	}
	return d
}

func anyToBson(v any) any {
	switch t := v.(type) {
	case Obj:
		return objToBson(t)
	case []any:
		out := make([]any, 0, len(t))
		for _, e := range t {
			out = append(out, anyToBson(e))
		}
		return out
	default:
		return v
	}
}

// writeSnapshot — best-effort full-doc snapshot (last 10). Never fails save.
func writeSnapshot(ctx context.Context, db *mongo.Database) {
	defer func() { recover() }()
	var doc bson.M
	if err := db.Collection("site_settings").
		FindOne(ctx, bson.D{{Key: "_id", Value: "global"}}).Decode(&doc); err != nil {
		return
	}
	coll := db.Collection("site_settings_snapshots")
	coll.InsertOne(ctx, bson.D{{Key: "at", Value: time.Now()}, {Key: "doc", Value: doc}})
	cursor, err := coll.Find(ctx, bson.D{}, options.Find().SetSort(bson.D{{Key: "at", Value: -1}}))
	if err != nil {
		return
	}
	var ids []any
	for cursor.Next(ctx) {
		var m bson.M
		if err := cursor.Decode(&m); err == nil {
			if id, ok := m["_id"]; ok {
				ids = append(ids, id)
			}
		}
	}
	_ = cursor.Close(ctx)
	if len(ids) > 10 {
		excess := ids[10:]
		coll.DeleteMany(ctx, bson.D{{Key: "_id", Value: bson.D{{Key: "$in", Value: excess}}}})
	}
}
