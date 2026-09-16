package llmsettings

import (
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"

	"strings"

	"ollitex/go/services/web/core"
)

// ---------- shared response helpers (Node key order, hand-built JSON) ----------

func jres(code int, res *core.Res, v any) {
	res.JSON(code, []byte(jsonWrite(v)))
}

func jobj(pairs ...any) obj {
	out := obj{}
	for i := 0; i+1 < len(pairs); i += 2 {
		k, _ := pairs[i].(string)
		out = out.set(k, pairs[i+1])
	}
	return out
}

// gate — Node requireUserSettingsAllowed.
func (f *fs) gate(res *core.Res) bool {
	if lEnv("LLM_ALLOW_USER_SETTINGS") == "true" {
		return true
	}
	jres(403, res, jobj(
		"ok", false,
		"error", "disabled",
		"message", "Bring-your-own LLM settings are disabled on this deployment",
	))
	return false
}

// internal500 — mirror Node expressify catch (500 + error JSON).
func (f *fs) internal500(res *core.Res, err error) {
	msg := ""
	if err != nil {
		msg = err.Error()
	}
	jres(500, res, jobj("ok", false, "error", "internal", "message", msg))
}

// blockedURL400 — Node assertPublicOr400.
func blockedURL400(res *core.Res, err error) {
	jres(400, res, jobj("error", "blocked-url", "message", err.Error()))
}

// ---------- loadProviders (legacy migration parity) ----------

// loadProviders — Node loadProviders(userId): filter rows, migrate legacy
// single-connection fields to row #0 (persisted), return the list.
func (f *fs) loadProviders(cxt *core.Cxt, u *userDoc) ([]map[string]any, error) {
	providers := make([]map[string]any, 0)
	for _, r := range u.llmProviders {
		m, ok := asMap(r)
		if !ok {
			continue
		}
		id := rID(m)
		isArr := asAnySlice(m["models"]) != nil
		if id != "" && isArr {
			providers = append(providers, m)
		}
	}
	hasURL := false
	urlStr := ""
	if s, ok := u.llmApiUrl.(string); ok && s != "" {
		hasURL = true
		urlStr = s
	}
	dup := false
	for _, r := range providers {
		if rID(r) == "legacy" || (r["name"] == "Imported settings" && r["baseUrl"] == urlStr) {
			dup = true
			break
		}
	}
	if hasURL && !dup {
		models := make([]any, 0)
		first := u.llmModelNames
		if first == nil {
			first = u.llmModels
		}
		if a := asAnySlice(first); len(a) > 0 {
			models = append(models, a...)
		}
		if s, ok := u.llmModelName.(string); ok && s != "" {
			inModels := false
			if a := asAnySlice(u.llmModels); len(a) > 0 {
				for _, e := range a {
					if e == s {
						inModels = true
					}
				}
			}
			if !inModels {
				models = append(models, s)
			}
		}
		cmList := make([]any, 0)
		if a := asAnySlice(u.llmCompletionModels); len(a) > 0 {
			cmList = append(cmList, a...)
		}
		if s, ok := u.llmCompletionModel.(string); ok && s != "" {
			inList := false
			if a := asAnySlice(u.llmCompletionModels); len(a) > 0 {
				for _, e := range a {
					if e == s {
						inList = true
					}
				}
			}
			if !inList {
				cmList = append(cmList, s)
			}
		}
		legacyType := ""
		if s, ok := u.llmApiType.(string); ok {
			for _, t := range providerTypes {
				if s == t {
					legacyType = t
				}
			}
		}
		pt := legacyType
		if pt == "" {
			pt = detectProviderType(urlStr)
		}
		km := ""
		if len(cmList) > 0 {
			if s, ok := cmList[0].(string); ok {
				km = s
			}
		}
		cmVal := ""
		if km != "" && containsAny(models, km) {
			cmVal = km
		} else if len(models) > 0 {
			if s, ok := models[0].(string); ok {
				cmVal = s
			}
		}
		row := map[string]any{
			"id":              "legacy",
			"name":            "Imported settings",
			"providerType":    pt,
			"baseUrl":         urlStr,
			"apiKey":          f.crypto.normalizeStored(secretOf(u.llmApiKey)),
			"models":          sliceCap(models, 100),
			"completionModel": cmVal,
			"enabled":         true,
			"createdAt":       epoch0ISO,
		}
		merged := make([]any, 0, 1+len(providers))
		merged = append(merged, orderedRowOf(row))
		for _, p := range providers {
			merged = append(merged, orderedRowOf(p))
		}
		if u.found {
			oid, err := primitive.ObjectIDFromHex(cxt.Sess.UserIDHex())
			if err == nil {
				coll, cerr := f.usersColl(cxt.Req.Context())
				if cerr == nil {
					_, _ = coll.UpdateOne(cxt.Req.Context(), bson.M{"_id": oid}, bson.M{"$set": bson.M{"llmProviders": merged}})
				}
			}
		}
		out := make([]map[string]any, 0, 1+len(providers))
		out = append(out, row)
		out = append(out, providers...)
		return out, nil
	}
	return providers, nil
}

func containsAny(a []any, v any) bool {
	for _, e := range a {
		if e == v {
			return true
		}
	}
	return false
}

func sliceCap(a []any, n int) []any {
	if len(a) > n {
		return a[:n]
	}
	return a
}

func secretOf(v any) string {
	s, _ := v.(string)
	return s
}

// ---------- publicRow (Node exact shape/order) ----------

func publicRow(row map[string]any) obj {
	baseUrl := ""
	if s, ok := row["baseUrl"].(string); ok {
		baseUrl = s
	}
	cm := ""
	if s, ok := row["completionModel"].(string); ok {
		cm = s
	}
	enabled := true
	if b, ok := row["enabled"].(bool); ok {
		enabled = b
	}
	hasKey := false
	if s, ok := row["apiKey"].(string); ok && s != "" {
		hasKey = true
	}
	createdAt := epoch0ISO
	if s, ok := row["createdAt"].(string); ok && s != "" {
		createdAt = s
	}
	models := make([]any, 0)
	if a := asAnySlice(row["models"]); len(a) > 0 {
		models = a
	}
	id := ""
	if s, ok := row["id"].(string); ok {
		id = s
	}
	name := ""
	if s, ok := row["name"].(string); ok {
		name = s
	}
	pt := ""
	if s, ok := row["providerType"].(string); ok {
		pt = s
	}
	return jobj(
		"id", id,
		"name", name,
		"providerType", pt,
		"baseUrl", baseUrl,
		"hasKey", hasKey,
		"models", models,
		"completionModel", cm,
		"enabled", enabled,
		"createdAt", createdAt,
	)
}

// orderedRowOf — stored row (Node key order for persisted rows).
// Returns bson.D so the mongo driver stores an ordered, BSON-typed
// document. NOTE: ojson `obj`/`kv` values must never reach a Mongo write —
// `kv` fields are unexported and the driver would encode each as `{}`
// (observed corruption: llmProviders [[{},...]])
func orderedRowOf(m map[string]any) bson.D {
	keys := []string{"id", "name", "providerType", "baseUrl", "apiKey", "models", "completionModel", "enabled", "createdAt", "updatedAt"}
	seen := map[string]bool{}
	out := bson.D{}
	for _, k := range keys {
		if v, ok := m[k]; ok {
			out = append(out, bson.E{Key: k, Value: v})
			seen[k] = true
		}
	}
	// extra keys (defensive) in stored order
	for _, e := range mapKeys(m) {
		if !seen[e] {
			out = append(out, bson.E{Key: e, Value: m[e]})
			seen[e] = true
		}
	}
	return out
}

func mapKeys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// ---------- GET/POST /user/llm-providers ----------

func (f *fs) getProviders(cxt *core.Cxt, res *core.Res) {
	if !f.gate(res) {
		return
	}
	u, err := f.loadUser(cxt.Req.Context(), cxt.Sess.UserIDHex())
	if err != nil {
		f.internal500(res, err)
		return
	}
	rows, err := f.loadProviders(cxt, u)
	if err != nil {
		f.internal500(res, err)
		return
	}
	list := make([]any, 0, len(rows))
	for _, r := range rows {
		list = append(list, publicRow(r))
	}
	jres(200, res, jobj("ok", true, "providers", list, "maxProviders", 10))
}

func (f *fs) addProvider(cxt *core.Cxt, res *core.Res) {
	if !f.gate(res) {
		return
	}
	b := f.readBody(cxt)
	parsed, issues := validateRow(b.objV, b.isArr)
	if len(issues) > 0 {
		jres(400, res, jobj("ok", false, "error", "invalid", "details", issuesDetail(issues)))
		return
	}
	if err := assertPublicLlmBaseUrl(parsed.baseUrl); err != nil {
		blockedURL400(res, err)
		return
	}
	u, err := f.loadUser(cxt.Req.Context(), cxt.Sess.UserIDHex())
	if err != nil {
		f.internal500(res, err)
		return
	}
	existingRaw := u.llmProviders
	if len(existingRaw) >= maxProvidersPerUser {
		jres(400, res, jobj("ok", false, "error", "limit", "message", "Maximum of 10 providers"))
		return
	}
	// dedupe models (Node [...new Set(parsed.data.models)])
	seen := map[string]bool{}
	dedup := make([]any, 0, len(parsed.models))
	for _, m := range parsed.models {
		if !seen[m] {
			seen[m] = true
			dedup = append(dedup, m)
		}
	}
	rowId := makeRowId()
	rowObj := jobj(
		"name", parsed.name,
		"providerType", parsed.providerType,
		"baseUrl", parsed.baseUrl,
		"apiKey", "",
		"models", dedup,
		"completionModel", parsed.completionModel,
		"enabled", parsed.enabled,
	)
	row := rowObjToMap(rowObj)
	row["id"] = rowId
	apiKey := ""
	if parsed.apiKey != "" {
		apiKey = f.crypto.encrypt(parsed.apiKey)
	}
	rowObj = rowObj.set("apiKey", apiKey)
	row["apiKey"] = apiKey
	row["id"] = rowId
	row["createdAt"] = nowISO()
	// migrated rows for the first-add case (Node: existing.length === 0)
	merged := make([]any, 0)
	if len(existingRaw) == 0 {
		if migrated, merr := f.loadProviders(cxt, u); merr == nil {
			for _, r := range migrated {
				mm := mapKeysCopy(r)
				rid := mm["id"]
				if s, ok := rid.(string); ok && s == "legacy" {
					mm["id"] = makeRowId()
				}
				if ak, ok := mm["apiKey"].(string); ok {
					mm["apiKey"] = f.crypto.normalizeStored(ak)
				}
				merged = append(merged, orderedRowOf(mm))
			}
		}
	}
	for _, r := range existingRaw {
		mm, ok := asMap(r)
		if ok {
			merged = append(merged, orderedRowOf(mm))
		}
	}
	merged = append(merged, orderedRowOf(row))
	// WS5: atomic cap guard ($expr size < 10)
	db, err := f.app.Mongo.DB(cxt.Req.Context())
	if err != nil {
		f.internal500(res, err)
		return
	}
	oid, err := primitive.ObjectIDFromHex(cxt.Sess.UserIDHex())
	if err != nil {
		f.internal500(res, err)
		return
	}
	coll := db.Collection("users")
	cond := bson.M{
		"_id": oid,
		"$expr": bson.D{
			{Key: "$lt", Value: bson.A{
				bson.D{{Key: "$size", Value: bson.D{{Key: "$ifNull", Value: bson.A{"$llmProviders", bson.A{}}}}}},
				maxProvidersPerUser,
			}},
		},
	}
	ug, err := coll.UpdateOne(cxt.Req.Context(), cond, bson.M{"$set": bson.M{"llmProviders": merged}})
	if err != nil {
		f.internal500(res, err)
		return
	}
	if ug.MatchedCount == 0 {
		jres(400, res, jobj("ok", false, "error", "limit", "message", "Maximum of 10 providers"))
		return
	}
	jres(201, res, jobj("ok", true, "provider", publicRow(rowFinal(rowId, row))))
}

// rowFinal — public-row source for the 201 (Node: {...parsed.data, id, models, apiKey, createdAt}).
func rowFinal(id string, stored map[string]any) map[string]any {
	out := mapKeysCopy(stored)
	out["id"] = id
	return out
}

func mapKeysCopy(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// ---------- POST /user/llm-providers/:id (update) ----------

func (f *fs) updateProvider(cxt *core.Cxt, res *core.Res) {
	if !f.gate(res) {
		return
	}
	rowId := cxt.Params["id"]
	if rowId == "" {
		jres(404, res, jobj("ok", false, "error", "not-found"))
		return
	}
	u, err := f.loadUser(cxt.Req.Context(), cxt.Sess.UserIDHex())
	if err != nil {
		f.internal500(res, err)
		return
	}
	providers, err := f.loadProviders(cxt, u)
	if err != nil {
		f.internal500(res, err)
		return
	}
	var current map[string]any
	for _, r := range providers {
		if rID(r) == rowId {
			current = r
			break
		}
	}
	if current == nil {
		jres(404, res, jobj("ok", false, "error", "not-found"))
		return
	}
	b := f.readBody(cxt)
	body := b.objV
	merged := mapKeysCopy(current)
	if v, ok := body.get("name"); ok {
		merged["name"] = v
	}
	if v, ok := body.get("providerType"); ok {
		merged["providerType"] = v
	}
	if v, ok := body.get("baseUrl"); ok {
		merged["baseUrl"] = v
	} else {
		cur, _ := current["baseUrl"].(string)
		merged["baseUrl"] = cur
	}
	if v, ok := body.get("models"); ok {
		merged["models"] = v
	}
	if v, ok := body.get("completionModel"); ok {
		merged["completionModel"] = v
	} else {
		cur, _ := current["completionModel"].(string)
		merged["completionModel"] = cur
	}
	if v, ok := body.get("enabled"); ok {
		merged["enabled"] = v
	}
	parsed, issues := validateObj(mapToObj(merged))
	if len(issues) > 0 {
		jres(400, res, jobj("ok", false, "error", "invalid", "details", issuesDetail(issues)))
		return
	}
	if err := assertPublicLlmBaseUrl(parsed.baseUrl); err != nil {
		blockedURL400(res, err)
		return
	}
	apiKey := f.crypto.normalizeStored(secretOf(current["apiKey"]))
	if truthy(mustGet(body, "clearApiKey")) {
		apiKey = ""
	} else if v, ok := body.get("apiKey"); ok {
		if s, isStr := v.(string); isStr && strings.TrimSpace(s) != "" {
			apiKey = f.crypto.encrypt(s)
		}
	}
	// rows: replace the matching id (or the 'legacy' twin) with the saved shape.
	storedList := u.llmProviders
	if len(storedList) == 0 {
		storedList = []any{}
	}
	newRow := map[string]any{
		"name":            parsed.name,
		"providerType":    parsed.providerType,
		"baseUrl":         parsed.baseUrl,
		"apiKey":          "",
		"models":          parsed.models,
		"completionModel": parsed.completionModel,
		"enabled":         parsed.enabled,
	}
	if rowId == "legacy" {
		newRow["id"] = makeRowId()
	} else {
		newRow["id"] = rowId
	}
	newRow["apiKey"] = apiKey
	newRow["createdAt"] = nowISO()
	rows := make([]any, 0, len(storedList))
	for _, r := range storedList {
		m, ok := asMap(r)
		id := ""
		name := ""
		if ok {
			id = rID(m)
			name, _ = m["name"].(string)
		}
		if ok && (id == rowId || id == "legacy") {
			rows = append(rows, orderedRowOf(newRow))
		} else {
			if ok {
				rows = append(rows, orderedRowOf(m))
			} else {
				rows = append(rows, r)
			}
			_ = name
		}
	}
	coll, err := f.usersColl(cxt.Req.Context())
	if err != nil {
		f.internal500(res, err)
		return
	}
	oid, err := primitive.ObjectIDFromHex(cxt.Sess.UserIDHex())
	if err != nil {
		f.internal500(res, err)
		return
	}
	if _, err := coll.UpdateOne(cxt.Req.Context(), bson.M{"_id": oid}, bson.M{"$set": bson.M{"llmProviders": rows}}); err != nil {
		f.internal500(res, err)
		return
	}
	respRow := mapKeysCopy(newRow)
	respRow["apiKey"] = apiKey
	jres(200, res, jobj("ok", true, "provider", publicRow(respRow)))
}

// truthy — Node `req.body?.clearApiKey` truthiness (non-nil, non-false, non-” ...).
func modelsNonEmpty(m map[string]any) bool {
	a, isA := m["models"].([]any)
	return isA && len(a) > 0
}

func truthy(v any) bool {
	switch t := v.(type) {
	case nil:
		return false
	case bool:
		return t
	case string:
		return t != ""
	}
	return true
}

// validateObj — rowSchema over a Go map (update's merged object).
func validateObj(o obj) (*rowField, []zissue) {
	return validateRow(o, false)
}

// mapToObj — ordered conversion (insertion order = merge order).
func mapToObj(m map[string]any) obj {
	out := obj{}
	keys := []string{"name", "providerType", "baseUrl", "apiKey", "models", "completionModel", "enabled"}
	for _, k := range keys {
		if v, ok := m[k]; ok {
			out = out.set(k, v)
		}
	}
	for _, e := range mapKeys(m) {
		if !out.has(e) {
			out = out.set(e, m[e])
		}
	}
	return out
}

// rowObjToMap — obj -> map (publicRow input shape).
func rowObjToMap(o obj) map[string]any {
	m := make(map[string]any, len(o))
	for _, e := range o {
		m[e.k] = e.v
	}
	return m
}

// ---------- POST /user/llm-providers/:id/delete ----------

func (f *fs) deleteProvider(cxt *core.Cxt, res *core.Res) {
	if !f.gate(res) {
		return
	}
	rowId := cxt.Params["id"]
	u, err := f.loadUser(cxt.Req.Context(), cxt.Sess.UserIDHex())
	if err != nil {
		f.internal500(res, err)
		return
	}
	providers, err := f.loadProviders(cxt, u)
	if err != nil {
		f.internal500(res, err)
		return
	}
	found := false
	for _, r := range providers {
		if rID(r) == rowId {
			found = true
			break
		}
	}
	if !found {
		jres(404, res, jobj("ok", false, "error", "not-found"))
		return
	}
	storedList := u.llmProviders
	remaining := make([]any, 0)
	for _, r := range storedList {
		m, ok := asMap(r)
		if !ok {
			remaining = append(remaining, r)
			continue
		}
		id := rID(m)
		name, _ := m["name"].(string)
		if id == rowId {
			continue
		}
		if rowId == "legacy" && name == "Imported settings" {
			continue
		}
		remaining = append(remaining, orderedRowOf(m))
	}
	targetIsLegacy := false
	for _, r := range providers {
		if rID(r) == rowId && (rID(r) == "legacy" || r["name"] == "Imported settings") {
			targetIsLegacy = true
		}
	}
	if rowId == "legacy" {
		for _, r := range providers {
			if r["name"] == "Imported settings" {
				targetIsLegacy = true
			}
		}
	}
	coll, err := f.usersColl(cxt.Req.Context())
	if err != nil {
		f.internal500(res, err)
		return
	}
	oid, err := primitive.ObjectIDFromHex(cxt.Sess.UserIDHex())
	if err != nil {
		f.internal500(res, err)
		return
	}
	update := bson.M{"$set": bson.M{"llmProviders": remaining}}
	if targetIsLegacy {
		update["$unset"] = bson.M{"llmApiUrl": 1, "llmApiType": 1, "llmApiKey": 1, "llmModelName": 1, "llmModels": 1, "llmModelNames": 1, "llmCompletionModel": 1, "llmCompletionModels": 1, "useOwnLLMSettings": 1}
	}
	if _, err := coll.UpdateOne(cxt.Req.Context(), bson.M{"_id": oid}, update); err != nil {
		f.internal500(res, err)
		return
	}
	jres(200, res, jobj("ok", true))
}

// ---------- POST /user/llm-providers/check ----------

func (f *fs) checkProviderConnection(cxt *core.Cxt, res *core.Res) {
	if !f.gate(res) {
		return
	}
	b := f.readBody(cxt)
	body := b.objV
	baseUrl := strOr(body.str("baseUrl"))
	apiKey := body.str("apiKey")
	providerType := body.str("providerType")
	model := body.str("model")
	if rowId := body.str("rowId"); rowId != "" {
		u, err := f.loadUser(cxt.Req.Context(), cxt.Sess.UserIDHex())
		if err != nil {
			f.internal500(res, err)
			return
		}
		providers, err := f.loadProviders(cxt, u)
		if err != nil {
			f.internal500(res, err)
			return
		}
		var row map[string]any
		for _, r := range providers {
			if rID(r) == rowId {
				row = r
				break
			}
		}
		if row == nil {
			jres(404, res, jobj("ok", false, "error", "not-found"))
			return
		}
		if baseUrl == "" {
			baseUrl, _ = row["baseUrl"].(string)
		}
		if providerType == "" {
			providerType, _ = row["providerType"].(string)
		}
		if apiKey == "" {
			if s, ok := row["apiKey"].(string); ok && s != "" {
				apiKey = f.crypto.storedToPlaintext(s)
			}
		}
	}
	if err := assertPublicLlmBaseUrl(baseUrl); err != nil {
		blockedURL400(res, err)
		return
	}
	if baseUrl == "" && providerType == "" {
		jres(400, res, jobj("ok", false, "error", "invalid", "details", "baseUrl or rowId is required"))
		return
	}
	requestedType := providerType
	if requestedType == "" {
		requestedType = detectProviderType(baseUrl)
	}
	started := time.Now()
	models, detType, success, fail := byoCheck(baseUrl, apiKey, requestedType, model)
	if !success {
		if fail != nil {
			if fail.details {
				// model-not-found shape
				jres(fail.status, res, jobj(
					"ok", false,
					"error", fail.code,
					"details", fail.message,
					"models", strAny(fail.models),
					"duration", durationMS(started),
				))
				return
			}
			jres(fail.status, res, jobj(
				"ok", false,
				"error", nonEmpty(fail.code, "llm-error"),
				"message", fail.message,
				"duration", durationMS(started),
			))
			return
		}
	}
	o := jobj(
		"ok", true,
		"message", "Connection successful",
		"models", strAny(models),
		"providerType", detType,
	)
	if detType != requestedType {
		o = o.set("detectedProviderType", detType)
	}
	o = o.set("duration", durationMS(started))
	jres(200, res, o)
}

func strOr(s string) string {
	if s == "" {
		return ""
	}
	return s
}

func strAny(a []string) []any {
	out := make([]any, 0, len(a))
	for _, s := range a {
		out = append(out, s)
	}
	return out
}

func nonEmpty(s, dflt string) string {
	if s != "" {
		return s
	}
	return dflt
}

// ---------- POST /user/llm-providers/scan ----------

func (f *fs) scanProviderModels(cxt *core.Cxt, res *core.Res) {
	if !f.gate(res) {
		return
	}
	b := f.readBody(cxt)
	body := b.objV
	baseUrl := body.str("baseUrl")
	apiKey := body.str("apiKey")
	providerType := body.str("providerType")
	if rowId := body.str("rowId"); rowId != "" {
		u, err := f.loadUser(cxt.Req.Context(), cxt.Sess.UserIDHex())
		if err != nil {
			f.internal500(res, err)
			return
		}
		providers, err := f.loadProviders(cxt, u)
		if err != nil {
			f.internal500(res, err)
			return
		}
		var row map[string]any
		for _, r := range providers {
			if rID(r) == rowId {
				row = r
				break
			}
		}
		if row == nil {
			jres(404, res, jobj("ok", false, "error", "not-found"))
			return
		}
		if baseUrl == "" {
			baseUrl, _ = row["baseUrl"].(string)
		}
		if providerType == "" {
			providerType, _ = row["providerType"].(string)
		}
		if apiKey == "" {
			if s, ok := row["apiKey"].(string); ok && s != "" {
				apiKey = f.crypto.storedToPlaintext(s)
			}
		}
	}
	if providerType == "" {
		providerType = detectProviderType(baseUrl)
	}
	if err := assertPublicLlmBaseUrl(baseUrl); err != nil {
		blockedURL400(res, err)
		return
	}
	if baseUrl == "" {
		jres(400, res, jobj("ok", false, "error", "invalid", "details", "baseUrl or rowId is required"))
		return
	}
	requestedType := providerType
	models, detType, success, fail := byoScan(baseUrl, apiKey, requestedType)
	if !success {
		if fail != nil {
			jres(fail.status, res, jobj(
				"ok", false,
				"error", nonEmpty(fail.code, "llm-error"),
				"message", fail.message,
			))
			return
		}
	}
	o := jobj(
		"ok", true,
		"models", strAny(models),
		"providerType", detType,
	)
	if detType != requestedType {
		o = o.set("detectedProviderType", detType)
	}
	jres(200, res, o)
}

// ---------- selected model ----------

func (f *fs) getSelectedModel(cxt *core.Cxt, res *core.Res) {
	u, err := f.loadUser(cxt.Req.Context(), cxt.Sess.UserIDHex())
	if err != nil {
		f.internal500(res, err)
		return
	}
	selected := ""
	if s, ok := u.selectedModel.(string); ok {
		selected = s
	}
	jres(200, res, jobj("ok", true, "selected", selected))
}

func (f *fs) saveSelectedModel(cxt *core.Cxt, res *core.Res) {
	b := f.readBody(cxt)
	value := ""
	if v, ok := b.objV.get("selected"); ok {
		if s, isStr := v.(string); isStr {
			value = strings.TrimSpace(s)
		}
	}
	if utf16Len(value) > 500 {
		jres(400, res, jobj("ok", false, "error", "bad-request", "message", "Model value too long"))
		return
	}
	if value != "" && !modelRefRe.MatchString(value) {
		jres(400, res, jobj("ok", false, "error", "bad-request", "message", "Invalid model reference"))
		return
	}
	coll, err := f.usersColl(cxt.Req.Context())
	if err != nil {
		f.internal500(res, err)
		return
	}
	oid, err := primitive.ObjectIDFromHex(cxt.Sess.UserIDHex())
	if err != nil {
		f.internal500(res, err)
		return
	}
	if _, err := coll.UpdateOne(cxt.Req.Context(), bson.M{"_id": oid}, bson.M{"$set": bson.M{"llmSelectedModel": value}}); err != nil {
		f.internal500(res, err)
		return
	}
	jres(200, res, jobj("ok", true, "selected", value))
}

// ---------- compliance rubrics ----------

func (f *fs) getUserCompliance(cxt *core.Cxt, res *core.Res) {
	if !featureFlags()["reviewEnabled"] {
		jres(200, res, jobj("ok", true, "rubrics", []any{}, "inherited", false))
		return
	}
	uid := cxt.Sess.UserIDHex()
	u, err := f.loadUser(cxt.Req.Context(), uid)
	if err != nil {
		f.internal500(res, err)
		return
	}
	mine := false
	rubrics := []any{}
	if a := asAnySlice(u.compRubrics); len(a) > 0 {
		mine = true
		rubrics = a
	}
	if !mine {
		s := readAdminFile(f.adminPath())
		rubrics = s.arr("complianceRubrics")
		if rubrics == nil {
			rubrics = []any{}
		}
	}
	jres(200, res, jobj("ok", true, "rubrics", rubrics, "inherited", !mine))
}

func (f *fs) saveUserCompliance(cxt *core.Cxt, res *core.Res) {
	if !featureFlags()["reviewEnabled"] {
		jres(403, res, jobj("ok", false, "error", "disabled", "message", "The compliance review feature is disabled"))
		return
	}
	b := f.readBody(cxt)
	var list any
	if v, ok := b.objV.get("rubrics"); ok {
		list = v
	}
	sanitized, ok := sanitizeComplianceRubrics(list)
	if !ok {
		jres(400, res, jobj("ok", false, "error", "bad-request", "message", "Invalid rubrics"))
		return
	}
	validateList := []any{}
	if a, ok := list.([]any); ok {
		validateList = a
	}
	if msg := validateComplianceRubrics(validateList); msg != "" {
		jres(400, res, jobj("ok", false, "error", "bad-request", "message", msg))
		return
	}
	stored := make([]any, 0, len(sanitized))
	respRubrics := make([]any, 0, len(sanitized))
	for _, r := range sanitized {
		stored = append(stored, bson.D{
			{Key: "id", Value: r["id"]},
			{Key: "name", Value: r["name"]},
			{Key: "guidelines", Value: r["guidelines"]},
			{Key: "scanPatterns", Value: r["scanPatterns"]},
		})
		respRubrics = append(respRubrics, jobj(
			"id", r["id"],
			"name", r["name"],
			"guidelines", r["guidelines"],
			"scanPatterns", r["scanPatterns"],
		))
	}
	coll, err := f.usersColl(cxt.Req.Context())
	if err != nil {
		f.internal500(res, err)
		return
	}
	oid, err := primitive.ObjectIDFromHex(cxt.Sess.UserIDHex())
	if err != nil {
		f.internal500(res, err)
		return
	}
	if _, err := coll.UpdateOne(cxt.Req.Context(), bson.M{"_id": oid}, bson.M{"$set": bson.M{"llmComplianceRubrics": stored}}); err != nil {
		f.internal500(res, err)
		return
	}
	jres(200, res, jobj("ok", true, "rubrics", respRubrics))
}

// ---------- user usage ----------

func (f *fs) userUsageSummary(cxt *core.Cxt, res *core.Res) {
	daysQ := cxt.Req.URL.Query().Get("days")
	days, nanErr := parseIntJS(daysQ)
	daysArg := 30
	if nanErr == nil {
		daysArg = days
	}
	uid := cxt.Sess.UserIDHex()
	summary, err := getUsageSummary(cxt.Req.Context(), f.app, uid, daysArg)
	if err != nil || summary == nil {
		jres(200, res, jobj("ok", false, "error", "unavailable"))
		return
	}
	o := jobj(
		"ok", true,
		"days", summary.days,
		"calls", summary.calls,
		"inputTokens", summary.inputTokens,
		"outputTokens", summary.outputTokens,
		"totalTokens", summary.totalTokens,
		"byDay", summary.byDay,
		"byAction", summary.byAction,
		"byModel", summary.byModel,
	)
	jres(200, res, o)
}

// ---------- grammar (LLMSettingsController getGrammarSettings / saveGrammarSettings)

// grammAvail — Node grammarAvailability(userId): readAdminSettings + envs +
// the user's BYO rows. Exact 5-key object.
func (f *fs) grammarAvailability(cxt *core.Cxt, u *userDoc) map[string]any {
	s := readAdminFile(f.adminPath())
	llmAdminEnabled := s.getBool("llmDisabledByAdmin") != true
	ltAvailable := s.getBool("languageToolDisabledByAdmin") != true &&
		(s.str("languageToolUrl") != "" ||
			lEnv("LANGUAGE_TOOL_URL") != "" ||
			lEnv("LANGUAGE_TOOL_HOST") != "" ||
			lEnv("LANGUAGE_TOOL_PORT") != "" ||
			lEnv("LANGUAGETOOL_URL") != "")
	llmServerConfigured := (s.str("llmApiUrl") != "" && s.str("llmApiKey") != "") ||
		(lEnv("LLM_API_URL") != "" && lEnv("LLM_API_KEY") != "")
	llmPersonalComplete := false
	if u != nil {
		for _, r := range u.llmProviders {
			m, ok := asMap(r)
			if !ok {
				continue
			}
			if rID(m) == "" {
				continue
			}
			en := true
			if b, ok := m["enabled"].(bool); ok {
				en = b
			}
			a, isArr := m["models"].([]any)
			if en && isArr && len(a) > 0 {
				llmPersonalComplete = true
				break
			}
		}
	}
	return map[string]any{
		"llmAdminEnabled":     llmAdminEnabled,
		"ltAvailable":         ltAvailable,
		"llmServerConfigured": llmServerConfigured,
		"llmAvailableForUser": llmAdminEnabled && (llmServerConfigured || llmPersonalComplete),
		"llmPersonalComplete": llmPersonalComplete,
	}
}

func availabilityObj(av map[string]any) obj {
	return jobj(
		"llmAdminEnabled", asBool(av["llmAdminEnabled"]),
		"ltAvailable", asBool(av["ltAvailable"]),
		"llmServerConfigured", asBool(av["llmServerConfigured"]),
		"llmAvailableForUser", asBool(av["llmAvailableForUser"]),
		"llmPersonalComplete", asBool(av["llmPersonalComplete"]),
	)
}

func asBool(v any) bool {
	b, _ := v.(bool)
	return b
}

// degradeGrammarMode — Node parity.
func degradeGrammarMode(mode string, av map[string]any) string {
	valid := false
	for _, m := range grammarModes {
		if mode == m {
			valid = true
		}
	}
	if !valid {
		return "default"
	}
	lt := asBool(av["ltAvailable"])
	llm := asBool(av["llmAvailableForUser"])
	switch mode {
	case "lt+llm":
		if lt && llm {
			return "lt+llm"
		}
		if lt {
			return "lt"
		}
		if llm {
			return "llm"
		}
		return "default"
	case "lt":
		if lt {
			return "lt"
		}
		return "default"
	case "llm":
		if llm {
			return "llm"
		}
		return "default"
	}
	return "default"
}

func (f *fs) getGrammarSettings(cxt *core.Cxt, res *core.Res) {
	uid := cxt.Sess.UserIDHex()
	u, err := f.loadUser(cxt.Req.Context(), uid)
	if err != nil {
		f.internal500(res, err)
		return
	}
	stored := map[string]any{}
	if m, ok := asMap(u.grammar); ok {
		stored = m
	}
	mode := "default"
	if s, ok := stored["mode"].(string); ok && s != "" {
		mode = s
	}
	llmModel := ""
	if s, ok := stored["llmModel"].(string); ok {
		llmModel = s
	}
	language := "auto"
	if s, ok := stored["language"].(string); ok && s != "" {
		language = s
	}
	blockedRules := make([]any, 0)
	if a, ok := stored["blockedRules"].([]any); ok {
		for _, e := range a {
			if s, isStr := e.(string); isStr && s != "" {
				blockedRules = append(blockedRules, s)
			}
		}
	}
	av := f.grammarAvailability(cxt, u)
	if asBool(av["llmServerConfigured"]) || asBool(av["llmPersonalComplete"]) {
		models, merr := grammarModelsList(f, cxt, u, av)
		if merr != nil {
			f.internal500(res, merr)
			return
		}
		jres(200, res, jobj(
			"mode", mode,
			"effectiveMode", degradeGrammarMode(mode, av),
			"llmModel", llmModel,
			"language", language,
			"blockedRules", blockedRules,
			"availability", availabilityObj(av),
			"models", models,
		))
		return
	}
	jres(200, res, jobj(
		"mode", mode,
		"effectiveMode", degradeGrammarMode(mode, av),
		"llmModel", llmModel,
		"language", language,
		"blockedRules", blockedRules,
		"availability", availabilityObj(av),
		"models", []any{},
	))
}

// grammarModelsList — Node model-picker building (site lane + BYO rows).
func grammarModelsList(f *fs, cxt *core.Cxt, u *userDoc, av map[string]any) ([]any, error) {
	models := make([]any, 0)
	if asBool(av["llmAdminEnabled"]) {
		if asBool(av["llmServerConfigured"]) {
			s := readAdminFile(f.adminPath())
			ids := make([]string, 0)
			if a := s.arr("allowedModels"); len(a) > 0 {
				for _, e := range a {
					if s2, isStr := e.(string); isStr {
						ids = append(ids, s2)
					}
				}
			}
			if len(ids) == 0 {
				ids = envModelList()
			}
			for _, id := range ids {
				if id != "" {
					models = append(models, jobj("id", id, "name", id, "isPersonal", false))
				}
			}
		}
		if asBool(av["llmPersonalComplete"]) {
			providers, err := f.loadProviders(cxt, u)
			if err != nil {
				return nil, err
			}
			for _, row := range providers {
				en := true
				if b, ok := row["enabled"].(bool); ok {
					en = b
				}
				if !en {
					continue
				}
				name := "BYO"
				if s2, ok := row["name"].(string); ok && s2 != "" {
					name = s2
				}
				if a := asAnySlice(row["models"]); len(a) > 0 {
					for _, modelId := range a {
						if s2, isStr := modelId.(string); isStr {
							models = append(models, jobj(
								"id", "u:"+rID(row)+":"+s2,
								"name", name+" — "+s2,
								"isPersonal", true,
							))
						}
					}
				}
			}
		}
	}
	return models, nil
}

// normalizeBlockedRules — Node parity (trim, slice 120, dedupe, cap 200).
func normalizeBlockedRules(input any) []any {
	var src []any
	switch t := input.(type) {
	case []any:
		src = t
	case string:
		src = []any{t}
	}
	out := make([]any, 0)
	seen := map[string]bool{}
	for _, raw := range src {
		s, isStr := raw.(string)
		if !isStr {
			continue
		}
		id := jsSlice(strings.TrimSpace(s), 120)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	if len(out) > 200 {
		out = out[:200]
	}
	return out
}

func (f *fs) saveGrammarSettings(cxt *core.Cxt, res *core.Res) {
	b := f.readBody(cxt)
	body := b.objV
	if v, ok := body.get("mode"); ok && v != nil {
		if s, isStr := v.(string); !isStr {
			// Node: GRAMMAR_MODES.includes(mode) — non-string is not in the list
			jres(400, res, jobj("success", false, "error", "Invalid grammar mode"))
			return
		} else {
			inList := false
			for _, m := range grammarModes {
				if s == m {
					inList = true
				}
			}
			if !inList {
				jres(400, res, jobj("success", false, "error", "Invalid grammar mode"))
				return
			}
		}
	}
	if v, ok := body.get("llmModel"); ok && v != nil && v != "" {
		s, isStr := v.(string)
		if !isStr || utf16Len(strings.TrimSpace(s)) > 500 || !modelRefRe.MatchString(strings.TrimSpace(s)) {
			jres(400, res, jobj("success", false, "error", "Invalid model reference"))
			return
		}
	}
	if v, ok := body.get("language"); ok && v != nil {
		s, isStr := v.(string)
		if !isStr || utf16Len(s) > 64 {
			jres(400, res, jobj("success", false, "error", "Invalid language"))
			return
		}
	}
	uid := cxt.Sess.UserIDHex()
	u, err := f.loadUser(cxt.Req.Context(), uid)
	if err != nil {
		f.internal500(res, err)
		return
	}
	stored := map[string]any{}
	if m, ok := asMap(u.grammar); ok {
		stored = m
	}
	modeVal, _ := body.get("mode")
	nextMode := ""
	if s, ok := modeVal.(string); ok && s != "" {
		nextMode = s
	} else if s, ok := stored["mode"].(string); ok {
		nextMode = s
	}
	if nextMode == "" {
		nextMode = "default"
	}
	var blockedRules []any
	if bv, ok := body.get("blockedRules"); ok {
		if bv == nil {
			blockedRules = []any{}
		} else if _, isStr := bv.(string); isStr {
			blockedRules = normalizeBlockedRules(bv)
		} else if _, isArr := bv.([]any); isArr {
			blockedRules = normalizeBlockedRules(bv)
		} else {
			blockedRules = []any{}
		}
	} else {
		blockedRules = normalizeBlockedRules(stored["blockedRules"])
	}
	av := f.grammarAvailability(cxt, u)
	effectiveMode := degradeGrammarMode(nextMode, av)
	llmModelOut := ""
	if lv, ok := body.get("llmModel"); ok {
		if lv == nil || lv == "" {
			llmModelOut = ""
		} else if s, isStr := lv.(string); isStr {
			llmModelOut = strings.TrimSpace(s)
		} else {
			llmModelOut = ""
		}
	}
	languageOut := ""
	if lgv, ok := body.get("language"); ok {
		if s, isStr := lgv.(string); isStr && s != "" {
			languageOut = s
		}
	}
	if languageOut == "" {
		if s, ok := stored["language"].(string); ok && s != "" {
			languageOut = s
		} else {
			languageOut = "auto"
		}
	}
	grammarDoc := jobj(
		"mode", nextMode,
		"llmModel", llmModelOut,
		"language", languageOut,
		"blockedRules", blockedRules,
	)
	if len(blockedRules) == 0 {
		grammarDoc = grammarDoc.set("blockedRules", []any{})
	}
	coll, err := f.usersColl(cxt.Req.Context())
	if err != nil {
		f.internal500(res, err)
		return
	}
	oid, err := primitive.ObjectIDFromHex(uid)
	if err != nil {
		f.internal500(res, err)
		return
	}
	if _, err := coll.UpdateOne(cxt.Req.Context(), bson.M{"_id": oid}, bson.M{"$set": bson.M{"grammar": grammarDoc}}); err != nil {
		jres(500, res, jobj("success", false, "error", "Failed to save grammar settings"))
		return
	}
	jres(200, res, jobj(
		"success", true,
		"mode", nextMode,
		"effectiveMode", effectiveMode,
		"degraded", effectiveMode != nextMode,
		"blockedRules", blockedRules,
		"availability", availabilityObj(av),
	))
}

// ---------- legacy 301 ----------

func (f *fs) userSettingsRedirect(cxt *core.Cxt, res *core.Res) {
	res.Redirect(cxt.Req, 301, "/hub#/mysettings.llm.general")
}
