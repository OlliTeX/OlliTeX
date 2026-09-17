package library

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"ollitex/go/services/web/core"
)

// ---------- models/LibraryReference.mjs ----------
//
// Schema: { user_id (string), key (string), type (string),
// fields: [{name, value}] (_id:false subdocs), searchBlob (string),
// trashedAt: Date|null, createdAt, updatedAt, occurrenceIndex (virtual,
// set per-response), _id ObjectId auto.
// MONGO_DEFAULTS: collection libraryreferences; indexes
// (user_id, key) and (user_id, trashedAt).

const collectionName = "libraryreferences"

// driver type shorthands (keep signatures short).
type primitiveObjectID = primitive.ObjectID

var NilObjectID = primitive.NilObjectID

func primitiveObjectIDFromHex(s string) (primitive.ObjectID, error) {
	return primitive.ObjectIDFromHex(s)
}

func findOptsLimitSort(n int64) *options.FindOptions {
	return options.Find().SetLimit(n).SetSort(bson.D{{Key: "_id", Value: 1}})
}

// trashRetention — Settings.bibLibrary.trashRetentionDays (server-ce
// default 30; node: `Settings.bibLibrary?.trashRetentionDays ?? 30` in
// LibraryManager.purgeTrashedReferences).
func trashRetentionDays() int {
	if v := os.Getenv("OVERLEAF_BIB_LIBRARY_TRASH_RETENTION_DAYS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return 30
}

func iso(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05.000Z") }

// toApiEntry — {key, type, fields:[{name, value ?? ”}], _id, occurrenceIndex,
// updatedAt|null, createdAt|null} — order EXACTLY as Node (occurrenceIndex
// is the in-RESPONSE position; the model marks it virtual).
func toApiEntry(doc map[string]any, occ int) string {
	fields := []string{}
	for _, f := range asAnySlice(doc["fields"]) {
		if fm, ok := f.(map[string]any); ok {
			v := ""
			if s, ok := fm["value"].(string); ok {
				v = s
			}
			fields = append(fields, `{"name":`+jstr(jsStr(fm["name"]))+`,"value":`+jstr(v)+`}`)
		}
	}
	updateAt, created := "null", "null"
	if t, ok := tsOf(doc["updatedAt"]); ok {
		updateAt = `"` + iso(t) + `"`
	}
	if t, ok := tsOf(doc["createdAt"]); ok {
		created = `"` + iso(t) + `"`
	}
	return `{"key":` + jstr(jsStr(doc["key"])) + `,"type":` + jstr(jsStr(doc["type"])) +
		`,"fields":[` + strings.Join(fields, ",") + `],"_id":` + jstr(jsStringOID(doc["_id"])) +
		`,"occurrenceIndex":` + strconv.Itoa(occ) +
		`,"updatedAt":` + updateAt + `,"createdAt":` + created + `}`
}

func jsStr(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

func jsStringOID(v any) string {
	switch t := v.(type) {
	case primitiveObjectID:
		return t.Hex()
	case string:
		return t
	}
	return ""
}
func tsOf(v any) (time.Time, bool) {
	switch t := v.(type) {
	case time.Time:
		return t, true
	case primitive.DateTime: // driver decodes BSON dates into interface{} as primitive.DateTime
		return t.Time(), true
	}
	return time.Time{}, false
}

// asAnySlice — driver-v1 decodes BSON arrays into interface{} as
// primitive.A (a NAMED slice type, so a plain .([]any) assertion FAILS — the
// same P6.4b lesson; always go through this helper).
func asAnySlice(v any) []any {
	switch t := v.(type) {
	case []any:
		return t
	case primitive.A: // type A []interface{}
		return []any(t)
	}
	return nil
}

// jstr — JSON string literal (Node JSON.stringify escaping subset:
// ASCII control chars + " and \).
func jstr(s string) string {
	if s == "" {
		return `""`
	}
	b := make([]byte, 0, len(s)+2)
	b = append(b, '"')
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '"' || c == '\\':
			b = append(b, '\\', c)
		case c < 0x20:
			switch c {
			case '\b':
				b = append(b, '\\', 'b')
			case '\f':
				b = append(b, '\\', 'f')
			case '\n':
				b = append(b, '\\', 'n')
			case '\r':
				b = append(b, '\\', 'r')
			case '\t':
				b = append(b, '\\', 't')
			default:
				b = append(b, []byte(fmt.Sprintf("\\u%04x", c))...)
			}
		default:
			b = append(b, c)
		}
	}
	b = append(b, '"')
	return string(b)
}

// ---------- LibraryManager.mjs ports ----------

type manager struct {
	app  *core.App
	db   *mongo.Database
	once sync.Once
	init error
}

func newManager(a *core.App) *manager {
	return &manager{app: a}
}

// getDB — first-call dial (MongoLazy tolerance: the shadow service boots
// green even if mongo is briefly unreachable — same as redis).
func (m *manager) getDB(ctx context.Context) error {
	m.once.Do(func() {
		if m.app.Mongo == nil {
			m.init = errors.New("mongo not wired")
			return
		}
		db, err := m.app.Mongo.DB(ctx)
		m.db, m.init = db, err
	})
	return m.init
}

func (m *manager) purgeTrashed(ctx context.Context, userID string) {
	if err := m.getDB(ctx); err != nil {
	}
	cutoff := time.Now().Add(-24 * time.Hour * time.Duration(trashRetentionDays()))
	_, _ = m.db.Collection(collectionName).DeleteMany(ctx, bson.M{
		"user_id": userID,
		"trashedAt": bson.M{
			"$ne": nil,
			"$lt": cutoff,
		},
	})
}

func trashedFilter(trashed bool) bson.M {
	if trashed {
		return bson.M{"trashedAt": bson.M{"$ne": nil}}
	}
	return bson.M{"trashedAt": nil}
}

func searchPredicates(tokens []string) bson.A {
	ands := bson.A{}
	for _, t := range tokens {
		ands = append(ands, bson.M{"searchBlob": bson.M{"$regex": escapeRegex(t)}})
	}
	return ands
}

func (m *manager) list(ctx context.Context, userID string, trashed bool, search string, cursor string, limit int) ([]map[string]any, string, error) {
	if err := m.getDB(ctx); err != nil {
		return nil, "", err
	}
	m.purgeTrashed(ctx, userID)
	filter := bson.M{"user_id": userID}
	for k, v := range trashedFilter(trashed) {
		filter[k] = v
	}
	if cursor != "" {
		if oid, err := primitiveObjectIDFromHex(cursor); err == nil && oid != NilObjectID {
			filter["_id"] = bson.M{"$gt": oid}
		}
	}
	if search != "" {
		if ands := searchPredicates(tokenizeSearchQuery(search)); len(ands) > 0 {
			filter["$and"] = ands
		}
	}
	c := m.db.Collection(collectionName)
	cursorDocs, err := c.Find(ctx, filter, findOptsLimitSort(int64(limit+1)))
	if err != nil {
		return nil, "", err
	}
	var docs []map[string]any
	if err := cursorDocs.All(ctx, &docs); err != nil {
		return nil, "", err
	}
	hasMore := len(docs) > limit
	if hasMore {
		docs = docs[:limit]
	}
	next := ""
	if hasMore {
		if id, ok := docs[limit-1]["_id"].(primitiveObjectID); ok {
			next = id.Hex()
		}
	}
	return docs, next, nil
}

func (m *manager) create(ctx context.Context, userID string, entries []map[string]any) ([]map[string]any, error) {
	if err := m.getDB(ctx); err != nil {
		return nil, err
	}
	now := time.Now()
	batch := make([]bson.D, 0, len(entries))
	norms := make([]normEntry, 0, len(entries))
	for _, e := range entries {
		nk, ntype, nfields := normalizeEntry(e["key"], e["type"], e["fields"])
		norms = append(norms, normEntry{key: nk, typ: ntype, fields: nfields})
		farr := make([]bson.D, 0, len(nfields))
		for _, f := range nfields {
			farr = append(farr, bson.D{{Key: "name", Value: f.Name}, {Key: "value", Value: f.Value}})
		}
		batch = append(batch, bson.D{
			{Key: "user_id", Value: userID},
			{Key: "key", Value: nk},
			{Key: "type", Value: ntype},
			{Key: "fields", Value: farr},
			{Key: "searchBlob", Value: entrySearchBlob(nk, ntype, nfields)},
			{Key: "trashedAt", Value: nil},
			{Key: "createdAt", Value: now},
			{Key: "updatedAt", Value: now},
		})
	}
	anyBatch := make([]any, 0, len(batch))
	for _, d := range batch {
		anyBatch = append(anyBatch, d)
	}
	ins, err := m.db.Collection(collectionName).InsertMany(ctx, anyBatch)
	if err != nil {
		return nil, err
	}
	// Node returns `created.map((doc, i) => toApiEntry(doc, i))` — exactly the
	// docs we just wrote; build them directly (ids in insertion order).
	out := make([]map[string]any, 0, len(ins.InsertedIDs))
	for i, id := range ins.InsertedIDs {
		oid, ok := id.(primitiveObjectID)
		if !ok {
			continue
		}
		n := norms[i]
		flds := make([]any, 0, len(n.fields))
		for _, f := range n.fields {
			flds = append(flds, map[string]any{"name": f.Name, "value": f.Value})
		}
		out = append(out, map[string]any{
			"key": n.key, "type": n.typ, "fields": flds,
			"_id": oid, "trashedAt": nil,
			"createdAt": now, "updatedAt": now,
		})
	}
	return out, nil
}

type normEntry struct {
	key    string
	typ    string
	fields []fieldOut
}

func (m *manager) matchKeys(ctx context.Context, userID string, keys []string) []string {
	if err := m.getDB(ctx); err != nil {
		return nil
	}
	uniq := []string{}
	seen := map[string]bool{}
	for _, k := range keys {
		if k == "" || seen[k] {
			continue
		}
		seen[k] = true
		uniq = append(uniq, k)
	}
	if len(uniq) == 0 {
		return nil
	}
	var docs []map[string]any
	cursorDocs, err := m.db.Collection(collectionName).Find(ctx,
		bson.M{"user_id": userID, "trashedAt": nil, "key": bson.M{"$in": uniq}})
	if err != nil {
		return nil
	}
	_ = cursorDocs.All(ctx, &docs)
	found := map[string]bool{}
	for _, d := range docs {
		if s, ok := d["key"].(string); ok {
			found[s] = true
		}
	}
	out := []string{}
	for _, k := range uniq {
		if found[k] {
			out = append(out, k)
		}
	}
	return out
}

type updateResult struct {
	doc    map[string]any
	reason string // "" ok | notFound | duplicateKey | <validation reason>
	dupKey string
	err    error
}

func (m *manager) update(ctx context.Context, userID, originalKey string, entry map[string]any) updateResult {
	res := updateResult{}
	now := time.Now()
	if err := m.getDB(ctx); err != nil {
		res.err = err
		return res
	}

	// Node defaults for validation: key ?? originalKey, type ?? ''
	keyVal := any(originalKey)
	if k, ok := entry["key"]; ok && k != nil {
		keyVal = k
	}
	typeVal := any("")
	if t, ok := entry["type"]; ok && t != nil {
		typeVal = t
	}
	if r := validateEntry(map[string]any{"key": keyVal, "type": typeVal, "fields": entry["fields"]}); r.reason != "ok" {
		res.reason = r.reason // validation → 400 message
		return res
	}

	filter := bson.M{"user_id": userID, "key": originalKey, "trashedAt": nil}
	var doc map[string]any
	err := m.db.Collection(collectionName).FindOne(ctx, filter).Decode(&doc)
	if err == mongo.ErrNoDocuments {
		res.reason = "notFound"
		return res
	}
	if err != nil {
		res.err = err
		return res
	}

	newKey := jsStringTrim(keyVal)
	if newKey != "" && newKey != jsStringTrim(doc["key"]) {
		var conflict map[string]any
		err := m.db.Collection(collectionName).FindOne(ctx,
			bson.M{"user_id": userID, "key": newKey, "trashedAt": nil, "_id": bson.M{"$ne": idOf(doc)}}).
			Decode(&conflict)
		if err == nil {
			res.reason = "duplicateKey"
			res.dupKey = newKey
			return res
		}
		if err != mongo.ErrNoDocuments {
			res.err = err
			return res
		}
	}

	// Node: normalizeLibraryEntry({ key: newKey, type, fields }) — key is the
	// trimmed newKey (validation already passed, so all strings).
	_, ntype, nfields := normalizeEntry(newKey, entry["type"], entry["fields"])
	fnorm := normEntry{key: newKey, typ: ntype, fields: nfields}
	farr := make([]bson.D, 0, len(fnorm.fields))
	for _, f := range fnorm.fields {
		farr = append(farr, bson.D{{Key: "name", Value: f.Name}, {Key: "value", Value: f.Value}})
	}
	set := bson.D{
		{Key: "key", Value: fnorm.key},
		{Key: "type", Value: fnorm.typ},
		{Key: "fields", Value: farr},
		{Key: "searchBlob", Value: entrySearchBlob(fnorm.key, fnorm.typ, fnorm.fields)},
		{Key: "updatedAt", Value: now},
	}
	if upd, err := m.db.Collection(collectionName).UpdateOne(ctx, filter, bson.D{{Key: "$set", Value: set}}); err != nil {
		res.err = err
		return res
	} else if upd.MatchedCount != 1 {
		res.reason = "notFound"
		return res
	}
	doc["key"] = fnorm.key
	doc["type"] = fnorm.typ
	doc["fields"] = toAnySlice(farr)
	doc["updatedAt"] = now
	res.doc = doc
	return res
}

func idOf(doc map[string]any) any {
	if v, ok := doc["_id"]; ok {
		return v
	}
	return ""
}

func toAnySlice(d []bson.D) []any {
	out := make([]any, 0, len(d))
	for _, f := range d {
		out = append(out, map[string]any{"name": f[0].Value, "value": f[1].Value})
	}
	return out
}

type deleteResult struct {
	count   int
	nothing bool  // → 400 nothing-to-delete
	err     error // → 500 fallback
}

// delete — EXACTLY Node deleteReferenceEntries (order matters):
//  1. purge trashed (retention)
//  2. ids given (array of non-empty strings) → valid oids, else return 0
//     permanent → deleteMany / soft → updateMany trashedAt = now
//  3. else search → no tokens → return 0; permanent / soft likewise
//  4. else → nothing-to-delete (400)
func (m *manager) delete(ctx context.Context, userID string, ids []string, search string, permanent bool) deleteResult {
	if err := m.getDB(ctx); err != nil {
		return deleteResult{}
	}
	m.purgeTrashed(ctx, userID)
	idList := ids // controller already filtered Array.isArray + strings
	if len(idList) > 0 {
		oids := validOIDs(idList)
		if len(oids) == 0 {
			return deleteResult{count: 0}
		}
		query := mergeD(bson.M{"user_id": userID, "trashedAt": nil},
			bson.M{"_id": bson.M{"$in": oids}})
		if permanent {
			n, err := m.db.Collection(collectionName).DeleteMany(ctx, query)
			if err != nil {
				return deleteResult{err: err}
			}
			return deleteResult{count: int(n.DeletedCount)}
		}
		n, err := m.db.Collection(collectionName).UpdateMany(ctx, query,
			bson.D{{Key: "$set", Value: bson.D{{Key: "trashedAt", Value: time.Now()}}}})
		if err != nil {
			return deleteResult{err: err}
		}
		return deleteResult{count: int(n.ModifiedCount)}
	}
	if search != "" {
		tokens := tokenizeSearchQuery(search)
		if len(tokens) == 0 {
			return deleteResult{count: 0}
		}
		query := mergeD(bson.M{"user_id": userID, "trashedAt": nil},
			bson.M{"$and": searchPredicates(tokens)})
		if permanent {
			n, err := m.db.Collection(collectionName).DeleteMany(ctx, query)
			if err != nil {
				return deleteResult{err: err}
			}
			return deleteResult{count: int(n.DeletedCount)}
		}
		n, err := m.db.Collection(collectionName).UpdateMany(ctx, query,
			bson.D{{Key: "$set", Value: bson.D{{Key: "trashedAt", Value: time.Now()}}}})
		if err != nil {
			return deleteResult{err: err}
		}
		return deleteResult{count: int(n.ModifiedCount)}
	}
	return deleteResult{nothing: true}
}

func (m *manager) restore(ctx context.Context, userID string, ids []string) (int, error) {
	if err := m.getDB(ctx); err != nil {
		return 0, err
	}
	oids := validOIDs(ids)
	n, err := m.db.Collection(collectionName).UpdateMany(ctx,
		bson.M{"user_id": userID, "trashedAt": bson.M{"$ne": nil}, "_id": bson.M{"$in": oids}},
		bson.D{{Key: "$set", Value: bson.D{{Key: "trashedAt", Value: nil}, {Key: "updatedAt", Value: time.Now()}}}})
	if err != nil {
		return 0, err
	}
	return int(n.ModifiedCount), nil
}

func (m *manager) count(ctx context.Context, userID string, trashed bool, search string) (int64, error) {
	if err := m.getDB(ctx); err != nil {
		return 0, err
	}
	filter := bson.M{"user_id": userID}
	for k, v := range trashedFilter(trashed) {
		filter[k] = v
	}
	if search != "" {
		if ands := searchPredicates(tokenizeSearchQuery(search)); len(ands) > 0 {
			filter["$and"] = ands
		}
	}
	return m.db.Collection(collectionName).CountDocuments(ctx, filter)
}

// downloadDocs — Node download's filter assembly (order matters):
//  1. base {user_id, trashed:null}
//  2. mode=exclusion + search → $nor(searchPredicates); no tokens → base only
//  3. else valid ids → + _id $in
//  4. else search → + search $and
//
// sorted _id asc, capped 200.
func (m *manager) downloadDocs(ctx context.Context, userID, mode, search string, ids []string) ([]map[string]any, error) {
	if err := m.getDB(ctx); err != nil {
		return nil, err
	}
	filter := bson.M{"user_id": userID, "trashedAt": nil}
	if mode == "exclusion" {
		if search != "" {
			if ands := searchPredicates(tokenizeSearchQuery(search)); len(ands) > 0 {
				filter["$nor"] = ands
			}
			return m.findSorted(ctx, filter)
		}
		return m.findSorted(ctx, filter)
	}
	oids := validOIDs(ids)
	if len(oids) > 0 {
		filter["_id"] = bson.M{"$in": oids}
	} else if search != "" {
		if ands := searchPredicates(tokenizeSearchQuery(search)); len(ands) > 0 {
			filter["$and"] = ands
		}
	}
	return m.findSorted(ctx, filter)
}

func (m *manager) findSorted(ctx context.Context, filter bson.M) ([]map[string]any, error) {
	if err := m.getDB(ctx); err != nil {
		return nil, err
	}
	cursorDocs, err := m.db.Collection(collectionName).Find(ctx, filter, findOptsLimitSort(200))
	if err != nil {
		return nil, err
	}
	var docs []map[string]any
	if err := cursorDocs.All(ctx, &docs); err != nil {
		return nil, err
	}
	return docs, nil
}

func (m *manager) suggestions(ctx context.Context, userID, base string, extraKeys []string) []string {
	if err := m.getDB(ctx); err != nil {
		return []string{}
	}
	root := sanitizeCitationKey(base)
	if root == "" {
		return []string{}
	}
	candidates := []string{root}
	for i := 'b'; i <= 'z'; i++ {
		candidates = append(candidates, root+string(i))
	}
	for i := 2; i <= 999; i++ {
		candidates = append(candidates, root+strconv.Itoa(i))
	}
	taken := map[string]bool{}
	for _, k := range extraKeys {
		if k != "" {
			taken[k] = true
		}
	}
	var docs []map[string]any
	cursorDocs, err := m.db.Collection(collectionName).Find(ctx,
		bson.M{"user_id": userID, "trashedAt": nil, "key": bson.M{"$in": candidates}})
	if err == nil {
		_ = cursorDocs.All(ctx, &docs)
		for _, d := range docs {
			if s, ok := d["key"].(string); ok {
				taken[s] = true
			}
		}
	}
	out := []string{}
	for _, c := range candidates {
		if !taken[c] {
			out = append(out, c)
		}
		if len(out) == 10 {
			break
		}
	}
	return out
}

// sanitizeKeyBase — Node: String(base ?? ”).toLowerCase().replace(/[^a-z0-9]/g, ”).slice(0, 64)
func sanitizeCitationKey(base string) string {
	s := strings.ToLower(base)
	b := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') {
			b = append(b, c)
		}
	}
	if len(b) > 64 {
		b = b[:64]
	}
	return string(b)
}

func validOIDs(ids []string) []primitiveObjectID {
	out := []primitiveObjectID{}
	for _, s := range ids {
		if oid, err := primitiveObjectIDFromHex(s); err == nil && oid != NilObjectID {
			out = append(out, oid)
		}
	}
	return out
}

func mergeD(a, b bson.M) bson.M {
	out := bson.M{}
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		out[k] = v
	}
	return out
}
