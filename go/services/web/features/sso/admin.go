package sso

// admin.go — /admin/sso control API (overleaf-fed SSOAdminRouter +
// SSOAdminController parity) + test endpoints.
//
// Node pins:
//   - GET  /admin/sso/config            → masked config (200) / 500
//   - POST /admin/sso/config            → sanitize + save (upsert) →
//     {success:true,message:"Configuration saved."} / 400 {error:...}
//   - POST /admin/sso/provider          → add default provider (id
//     random-15, order max+1, name "{TYPE} Provider N",
//     buttonLabel "Log in with {name}") → {success,provider}
//   - DELETE /admin/sso/provider/:id    → {success:true}
//   - POST /admin/sso/providers/reorder → {id,order}[] → {success:true}
//   - POST /admin/sso/test/ldap         → {success,message}
//   - POST /admin/sso/test/attr-filter  → {success,matchedRole,
//     matchedRow(1-based),matchedAttribute} / {success:false,message}
//   - POST /admin/sso/test/provider/:id → OIDC discovery probe or SAML
//     metadata probe.
//
// Masking (Node _maskConfig/_maskProvider): ldap.bindCredentials,
// provider.{clientSecret,privateKey,decryptionPvk},
// spMetadata.{privateKey,publicCert} ⇒ '••••••••'; save restores
// masks from the stored doc (sanitize parity, '••••••••' with no stored
// value ⇒ field cleared).

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"ollitex/go/services/web/core"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	mongooptions "go.mongodb.org/mongo-driver/v2/mongo/options"
)

const maskSentinel = "••••••••"

// FeatureAdmin — the admin surface (register after Feature()).
func FeatureAdmin(a *core.App) core.Feature {
	return core.Feature{
		Name: "sso-admin",
		Routes: []core.Route{
			{Method: "GET", Pattern: mustRegexp(`^/admin/sso/config$`), Handler: adminGetConfig(a)},
			{Method: "POST", Pattern: mustRegexp(`^/admin/sso/config$`), Handler: adminSaveConfig(a)},
			{Method: "POST", Pattern: mustRegexp(`^/admin/sso/provider$`), Handler: adminAddProvider(a)},
			{Method: "DELETE", Pattern: mustRegexp(`^/admin/sso/provider/([A-Za-z0-9][A-Za-z0-9_-]{1,63})$`), Handler: adminDeleteProvider(a)},
			{Method: "POST", Pattern: mustRegexp(`^/admin/sso/providers/reorder$`), Handler: adminReorder(a)},
			{Method: "POST", Pattern: mustRegexp(`^/admin/sso/test/ldap$`), Handler: adminTestLdap(a)},
			{Method: "POST", Pattern: mustRegexp(`^/admin/sso/test/attr-filter$`), Handler: adminTestAttrFilter(a)},
			{Method: "POST", Pattern: mustRegexp(`^/admin/sso/test/provider/([A-Za-z0-9][A-Za-z0-9_-]{1,63})$`), Handler: adminTestProvider(a)},
			{Method: "GET", Pattern: mustRegexp(`^/admin/sso/cert-expiry$`), Handler: adminCertExpiry(a)},
		},
	}
}

func mustRegexp(s string) *regexp.Regexp { return regexp.MustCompile(s) }

// ---- config load/save ----

func ssoDBOr500(a *core.App, cxt *core.Cxt, res *core.Res) (*mongo.Database, bool) {
	if a.Mongo == nil {
		res.JSON(500, []byte(`{"error":"No database available"}`))
		return nil, false
	}
	db, err := a.Mongo.DB(cxt.Req.Context())
	if err != nil {
		res.JSON(500, []byte(`{"error":"Failed to connect to database"}`))
		return nil, false
	}
	return db, true
}

func adminGate(a *core.App, cxt *core.Cxt, res *core.Res) bool {
	return a.RequireSiteAdmin(cxt, res)
}

// adminGetConfig — GET /admin/sso/config (masked).
func adminGetConfig(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		if !adminGate(a, cxt, res) {
			return
		}
		db, ok := ssoDBOr500(a, cxt, res)
		if !ok {
			return
		}
		cfg := db.Collection("ssoConfigs").FindOne(cxt.Req.Context(),
			bson.D{{Key: "_id", Value: ssoConfigID}})
		if cfg.Err() != nil {
			res.JSON(200, []byte(`{"ldap":null,"providers":[],"spMetadata":null}`))
			return
		}
		var doc bson.M
		if derr := cfg.Decode(&doc); derr != nil {
			res.JSON(200, []byte(`{"ldap":null,"providers":[],"spMetadata":null}`))
			return
		}
		masked := maskConfigDoc(doc)
		b, _ := json.Marshal(masked)
		res.JSON(200, b)
	}
}

// adminSaveConfig — POST /admin/sso/config (sanitize + upsert).
func adminSaveConfig(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		if !adminGate(a, cxt, res) {
			return
		}
		db, ok := ssoDBOr500(a, cxt, res)
		if !ok {
			return
		}
		var body map[string]any
		b, rerr := io.ReadAll(io.LimitReader(cxt.Req.Body, 4<<20))
		if rerr != nil || len(b) == 0 {
			res.JSON(400, []byte(`{"error":"Invalid configuration"}`))
			return
		}
		if uerr := json.Unmarshal(b, &body); uerr != nil {
			res.JSON(400, []byte(`{"error":"Invalid configuration"}`))
			return
		}
		// existing doc for mask restore
		var existing map[string]any
		if r := db.Collection("ssoConfigs").FindOne(cxt.Req.Context(),
			bson.D{{Key: "_id", Value: ssoConfigID}}); r.Err() == nil {
			_ = r.Decode(&existing)
		}
		sanitized := sanitizeConfigDoc(body, existing)
		if _, uperr := db.Collection("ssoConfigs").ReplaceOne(cxt.Req.Context(),
			bson.D{{Key: "_id", Value: ssoConfigID}}, sanitized,
			mongooptions.Replace().SetUpsert(true)); uperr != nil {
			res.JSON(500, []byte(`{"error":"Failed to save SSO configuration"}`))
			return
		}
		res.JSON(200, []byte(`{"success":true,"message":"Configuration saved."}`))
	}
}

// adminAddProvider — POST /admin/sso/provider.
func adminAddProvider(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		if !adminGate(a, cxt, res) {
			return
		}
		db, ok := ssoDBOr500(a, cxt, res)
		if !ok {
			return
		}
		var body struct {
			Type string `json:"type"`
		}
		if b, _ := io.ReadAll(io.LimitReader(cxt.Req.Body, 1<<20)); b != nil {
			_ = json.Unmarshal(b, &body)
		}
		if body.Type != "oidc" && body.Type != "saml" {
			res.JSON(400, []byte(`{"error":"Invalid provider type"}`))
			return
		}
		col := db.Collection("ssoConfigs")
		cfg := loadRawConfig(cxt, col)
		if cfg == nil {
			cfg = bson.M{"_id": ssoConfigID, "providers": []any{}}
		}
		providers, _ := cfg["providers"].([]any)
		maxOrder := -1
		sameType := 0
		for _, pr := range providers {
			pm, _ := pr.(bson.M)
			if pm == nil {
				continue
			}
			if t, _ := pm["type"].(string); t == body.Type {
				sameType++
			}
			if o, ok := pm["order"].(int32); ok && int(o) > maxOrder {
				maxOrder = int(o)
			} else if of, ok := pm["order"].(float64); ok && int(of) > maxOrder {
				maxOrder = int(of)
			}
		}
		name := fmt.Sprintf("%s Provider %d", strings.ToUpper(body.Type), sameType+1)
		provider := bson.M{
			"id":          randID(),
			"type":        body.Type,
			"name":        name,
			"enabled":     false,
			"order":       int32(maxOrder + 1),
			"buttonLabel": "Log in with " + name,
		}
		if body.Type == "oidc" {
			provider["scope"] = "openid profile email"
		}
		providers = append(providers, provider)
		cfg["providers"] = providers
		if _, uerr := col.ReplaceOne(cxt.Req.Context(),
			bson.D{{Key: "_id", Value: ssoConfigID}}, cfg,
			mongooptions.Replace().SetUpsert(true)); uerr != nil {
			res.JSON(500, []byte(`{"error":"Failed to add provider"}`))
			return
		}
		masked := maskProviderDoc(provider)
		out := struct {
			Success  bool   `json:"success"`
			Provider bson.M `json:"provider"`
		}{true, masked}
		b, _ := json.Marshal(out)
		res.JSON(200, b)
	}
}

// adminDeleteProvider — DELETE /admin/sso/provider/:id.
func adminDeleteProvider(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		if !adminGate(a, cxt, res) {
			return
		}
		db, ok := ssoDBOr500(a, cxt, res)
		if !ok {
			return
		}
		id := ""
		if cxt.Params != nil {
			id = cxt.Params["1"]
		}
		col := db.Collection("ssoConfigs")
		cfg := loadRawConfig(cxt, col)
		if cfg == nil {
			res.JSON(200, []byte(`{"success":true}`))
			return
		}
		providers, _ := cfg["providers"].([]any)
		var kept []any
		for _, pr := range providers {
			pm, _ := pr.(bson.M)
			if pm != nil && pm["id"] == id {
				continue
			}
			kept = append(kept, pr)
		}
		cfg["providers"] = kept
		if _, uerr := col.ReplaceOne(cxt.Req.Context(),
			bson.D{{Key: "_id", Value: ssoConfigID}}, cfg,
			mongooptions.Replace().SetUpsert(true)); uerr != nil {
			res.JSON(500, []byte(`{"error":"Failed to delete provider"}`))
			return
		}
		res.JSON(200, []byte(`{"success":true}`))
	}
}

// adminReorder — POST /admin/sso/providers/reorder.
func adminReorder(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		if !adminGate(a, cxt, res) {
			return
		}
		db, ok := ssoDBOr500(a, cxt, res)
		if !ok {
			return
		}
		var body struct {
			Providers []struct {
				ID    string `json:"id"`
				Order int    `json:"order"`
			} `json:"providers"`
		}
		if b, _ := io.ReadAll(io.LimitReader(cxt.Req.Body, 1<<20)); b == nil {
			res.JSON(400, []byte(`{"error":"Invalid data"}`))
			return
		} else {
			if uerr := json.Unmarshal(b, &body); uerr != nil || len(body.Providers) == 0 {
				res.JSON(400, []byte(`{"error":"Invalid data"}`))
				return
			}
		}
		col := db.Collection("ssoConfigs")
		cfg := loadRawConfig(cxt, col)
		if cfg == nil {
			res.JSON(400, []byte(`{"error":"Invalid data"}`))
			return
		}
		providers, _ := cfg["providers"].([]any)
		byID := map[string]any{}
		for _, pr := range providers {
			pm, _ := pr.(bson.M)
			if pm != nil {
				if s, ok := pm["id"].(string); ok {
					byID[s] = pr
				}
			}
		}
		for _, o := range body.Providers {
			if p, ok := byID[o.ID].(bson.M); ok {
				p["order"] = int32(o.Order)
			}
		}
		if _, uerr := col.ReplaceOne(cxt.Req.Context(),
			bson.D{{Key: "_id", Value: ssoConfigID}}, cfg,
			mongooptions.Replace().SetUpsert(true)); uerr != nil {
			res.JSON(500, []byte(`{"error":"Failed to reorder providers"}`))
			return
		}
		res.JSON(200, []byte(`{"success":true}`))
	}
}

// ---- test endpoints ----

// adminTestLdap — POST /admin/sso/test/ldap.
func adminTestLdap(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		if !adminGate(a, cxt, res) {
			return
		}
		db, ok := ssoDBOr500(a, cxt, res)
		if !ok {
			return
		}
		cfg := loadSSOConfig(db, cxt)
		p := cfg.Ldap
		if p == nil || !p.Enabled {
			res.JSON(200, []byte(`{"success":false,"message":"LDAP is not enabled"}`))
			return
		}
		// With a test user (body {username|email, password}): a full
		// authenticate (dial→bind→search→bind-as-user). Otherwise a
		// connectivity + service-account bind probe (Node _testLDAP parity:
		// bind and return success/failure).
		username, password := "", ""
		if bb, _ := io.ReadAll(io.LimitReader(cxt.Req.Body, 1<<20)); len(bb) > 0 {
			var body struct {
				Username string `json:"username"`
				Email    string `json:"email"`
				Password string `json:"password"`
			}
			_ = json.Unmarshal(bb, &body)
			username = body.Username
			if username == "" {
				username = body.Email
			}
			password = body.Password
		}
		if username != "" && password != "" {
			if _, aerr := ldapAuthenticate(cxt.Req.Context(), p, username, password); aerr != nil {
				res.JSON(200, []byte(`{"success":false,"message":"LDAP connection failed: invalid user or password"}`))
				return
			}
			res.JSON(200, []byte(`{"success":true,"message":"LDAP connection successful. User authenticated."}`))
			return
		}
		if perr := ldapProbe(p); perr != nil {
			res.JSON(200, []byte(`{"success":false,"message":"LDAP connection failed"}`))
			return
		}
		res.JSON(200, []byte(`{"success":true,"message":"LDAP connection successful"}`))
	}
}

// adminTestAttrFilter — POST /admin/sso/test/attr-filter.
func adminTestAttrFilter(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		if !adminGate(a, cxt, res) {
			return
		}
		var body struct {
			Attribute string         `json:"attribute"`
			Values    any            `json:"values"`
			Match     string         `json:"match"`
			Role      string         `json:"role"`
			CaseSens  *bool          `json:"caseSensitive"`
			Profile   map[string]any `json:"profile"`
		}
		if b, _ := io.ReadAll(io.LimitReader(cxt.Req.Body, 1<<20)); b != nil {
			_ = json.Unmarshal(b, &body)
		}
		if body.Attribute == "" {
			res.JSON(200, []byte(`{"success":false,"message":"attribute is required"}`))
			return
		}
		vals := []string{}
		switch v := body.Values.(type) {
		case string:
			vals = append(vals, v)
		case []any:
			for _, x := range v {
				if s, ok := x.(string); ok {
					vals = append(vals, s)
				}
			}
		}
		if len(vals) == 0 && body.Match != "regex" {
			res.JSON(200, []byte(`{"success":false,"message":"values is required (or match=regex)"}`))
			return
		}
		cs := true
		if body.CaseSens != nil {
			cs = *body.CaseSens
		}
		row := AttrRule{
			Attribute:     body.Attribute,
			Values:        vals,
			Match:         body.Match,
			Role:          "guest",
			CaseSensitive: cs,
		}
		prof := ssoProfile{}
		for k, v := range body.Profile {
			prof[k] = v
		}
		matched := rowMatches(row, prof)
		res.JSON(200, []byte(fmt.Sprintf(
			`{"success":true,"matched":%v,"matchedRole":"guest","matchedRow":%d,"matchedAttribute":%q}`,
			matched, func() int {
				if matched {
					return 1
				}
				return 0
			}(), row.Attribute)))
	}
}

// adminTestProvider — POST /admin/sso/test/provider/:id.
func adminTestProvider(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		if !adminGate(a, cxt, res) {
			return
		}
		db, ok := ssoDBOr500(a, cxt, res)
		if !ok {
			return
		}
		pid := ""
		if cxt.Params != nil {
			pid = cxt.Params["1"]
		}
		cfg := loadSSOConfig(db, cxt)
		var oidcP *OIDCProvider
		var samlP *SAMLProvider
		if cfg != nil {
			for i := range cfg.Providers {
				var oo OIDCProvider
				if uerr := bson.Unmarshal(cfg.Providers[i], &oo); uerr == nil && oo.ID == pid {
					oidcP = &oo
				}
				var ss SAMLProvider
				if uerr := bson.Unmarshal(cfg.Providers[i], &ss); uerr == nil && ss.ID == pid {
					samlP = &ss
				}
			}
		}
		if oidcP != nil {
			// OIDC discovery probe (Node _testOIDCProvider).
			ctx, cancel := context.WithTimeout(cxt.Req.Context(), 10*time.Second)
			defer cancel()
			disc, err := fetchOIDCDiscovery(ctx, oidcP.Issuer)
			if err != nil {
				res.JSON(200, []byte(fmt.Sprintf(`{"success":false,"message":%q}`, jsonQuote(err.Error()))))
				return
			}
			res.JSON(200, []byte(fmt.Sprintf(
				`{"success":true,"message":"OIDC discovery successful. Issuer: %s","details":{"authorization_endpoint":%q,"token_endpoint":%q,"userinfo_endpoint":%q}}`,
				jsonQuote(disc.Issuer), disc.Auth, disc.Token, disc.Userinfo)))
			return
		}
		if samlP != nil {
			// SAML metadata probe (fetch + signature verify + cert
			// extract — Node _testSAMLProvider; Go: metadata fetch +
			// basic well-formedness check here, full sig verify in the
			// IdP cert import).
			url := samlP.EntryPoint
			if url == "" {
				res.JSON(200, []byte(`{"success":false,"message":"No entry point or metadata URL configured"}`))
				return
			}
			ctx, cancel := context.WithTimeout(cxt.Req.Context(), 10*time.Second)
			defer cancel()
			req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				res.JSON(200, []byte(fmt.Sprintf(`{"success":false,"message":"SAML metadata fetch failed: %s"}`, jsonQuote(err.Error()))))
				return
			}
			defer resp.Body.Close()
			if resp.StatusCode != 200 {
				res.JSON(200, []byte(fmt.Sprintf(`{"success":false,"message":"SAML metadata fetch failed: HTTP %d"}`, resp.StatusCode)))
				return
			}
			data, _ := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
			if meta, perr := parseSAMLIdPMetadata(data); perr == nil {
				res.JSON(200, []byte(fmt.Sprintf(
					`{"success":true,"message":"SAML metadata fetched (parsed)","details":{"entityID":%q,"sso":%q,"slo":%q,"certCount":%d}}`,
					jsonQuote(meta.EntityID), jsonQuote(meta.SSO), jsonQuote(meta.SLO), meta.CertCount)))
				return
			}
			if !bytes.Contains(data, []byte("EntityDescriptor")) {
				res.JSON(200, []byte(`{"success":false,"message":"SAML metadata: no EntityDescriptor element"}`))
				return
			}
			res.JSON(200, []byte(`{"success":true,"message":"SAML metadata fetched (EntityDescriptor present)"}`))
			return
		}
		res.JSON(200, []byte(`{"success":false,"message":"Provider not found"}`))
	}
}

// adminCertExpiry — GET /admin/sso/cert-expiry: classify every SSO certificate
// (SP cert + IdP certs) against the warn window; warnCount > 0 means at least
// one is expired/expiring (the boot sweep logs this the same way).
func adminCertExpiry(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		if !adminGate(a, cxt, res) {
			return
		}
		rows, warn := ssoCertExpiry(a, cxt)
		payload, _ := json.Marshal(map[string]any{
			"certificates": rows,
			"warnCount":    warn,
			"warnDays":     ssoCertExpiryWarnDays(),
		})
		res.JSON(200, payload)
	}
}

// ---- masking / sanitizing (Node _maskConfig/_sanitizeConfig parity) ----
func maskConfigDoc(doc bson.M) bson.M {
	out := bson.M{}
	for k, v := range doc {
		out[k] = v
	}
	if ldap, ok := toMap(out["ldap"]); ok {
		ldap["bindCredentials"] = maskSentinel
		out["ldap"] = ldap
	}
	if providers, ok := out["providers"].([]any); ok {
		for i, pr := range providers {
			if pm, ok2 := toMap(pr); ok2 {
				providers[i] = maskProviderDoc(pm)
			}
		}
	}
	if sp, ok := toMap(out["spMetadata"]); ok {
		if v, _ := sp["privateKey"].(string); v != "" {
			sp["privateKey"] = maskSentinel
		}
		if v, _ := sp["publicCert"].(string); v != "" {
			sp["publicCert"] = maskSentinel
		}
		out["spMetadata"] = sp
	}
	return out
}

func maskProviderDoc(p bson.M) bson.M {
	out := bson.M{}
	for k, v := range p {
		out[k] = v
	}
	for _, f := range []string{"clientSecret", "privateKey", "decryptionPvk"} {
		if vm, ok := toMap(out[f]); ok {
			out[f] = vm
		}
		if v, _ := out[f].(string); v != "" {
			out[f] = maskSentinel
		}
	}
	return out
}

// toMap — normalize map[string]any / bson.M → bson.M.
func toMap(v any) (bson.M, bool) {
	switch m := v.(type) {
	case bson.M:
		return m, true
	case map[string]any:
		out := bson.M{}
		for k, x := range m {
			out[k] = x
		}
		return out, true
	}
	return nil, false
}

func sanitizeConfigDoc(body, existing map[string]any) bson.M {
	out := bson.M{}
	for k, v := range body {
		out[k] = v
	}
	// normalize the nested containers to concrete map types the rest of
	// this function can work with (Node passes plain objects; Go's
	// json/decode gives map[string]any for API bodies):
	if prov, ok := out["providers"].([]any); ok {
		for i, pr := range prov {
			if pm, ok := toMap(pr); ok {
				prov[i] = pm
			}
		}
	}
	if ldap, ok := toMap(out["ldap"]); ok {
		if ldap["bindCredentials"] == maskSentinel {
			if exL, ok := toMap(existing["ldap"]); ok {
				if bc, _ := exL["bindCredentials"].(string); bc != "" {
					ldap["bindCredentials"] = bc
				} else {
					delete(ldap, "bindCredentials")
				}
			} else {
				delete(ldap, "bindCredentials")
			}
		}
		out["ldap"] = ldap
	}
	if providers, ok := out["providers"].([]any); ok {
		exProviders := []bson.M{}
		if exP, ok := existing["providers"].([]any); ok {
			for _, pr := range exP {
				if pm, ok := toMap(pr); ok {
					exProviders = append(exProviders, pm)
				}
			}
		}
		for i, pr := range providers {
			pm, ok := toMap(pr)
			if !ok {
				continue
			}
			id, _ := pm["id"].(string)
			var exP bson.M
			for _, ep := range exProviders {
				if ex, _ := ep["id"].(string); ex == id {
					exP = ep
					break
				}
			}
			for _, f := range []string{"clientSecret", "privateKey", "decryptionPvk"} {
				if pm[f] == maskSentinel {
					if exP != nil {
						if v, _ := exP[f].(string); v != "" {
							pm[f] = v
						}
					} else {
						delete(pm, f)
					}
				}
			}
			// attrFilter sanitize
			if af, ok := pm["attrFilter"].([]any); ok {
				cleaned := []bson.M{}
				for _, row := range af {
					rm, ok := toMap(row)
					if !ok {
						continue
					}
					if a, _ := rm["attribute"].(string); a == "" {
						continue
					}
					if r, _ := rm["role"].(string); r != "guest" && r != "blocked" {
						rm["role"] = "local"
					}
					m, _ := rm["match"].(string)
					if m != "includes" && m != "regex" {
						rm["match"] = "equals"
					}
					cleaned = append(cleaned, rm)
				}
				if len(cleaned) > 0 {
					pm["attrFilter"] = cleaned
				} else {
					delete(pm, "attrFilter")
				}
			}
			providers[i] = pm
		}
	}
	sp, spOK := toMap(out["spMetadata"])
	exSP, _ := toMap(existing["spMetadata"])
	if spOK {
		for _, f := range []string{"privateKey", "publicCert"} {
			if sp[f] == maskSentinel {
				if v, _ := exSP[f].(string); v != "" {
					sp[f] = v
				} else {
					delete(sp, f)
				}
			}
		}
		out["spMetadata"] = sp
	}
	return out
}

// ---- helpers ----

func loadRawConfig(cxt *core.Cxt, col *mongo.Collection) bson.M {
	r := col.FindOne(cxt.Req.Context(), bson.D{{Key: "_id", Value: ssoConfigID}})
	if r.Err() != nil {
		return nil
	}
	var doc bson.M
	if derr := r.Decode(&doc); derr != nil {
		return nil
	}
	return doc
}

func randID() string {
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, 15)
	for i := range b {
		b[i] = alphabet[(i*7+3)%len(alphabet)]
	}
	// mix in randomness
	seed := time.Now().UnixNano()
	for i := range b {
		b[i] = alphabet[(int(seed)+i*13)%len(alphabet)]
		seed = seed*31 + 17
	}
	return string(b)
}

func jsonQuote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

var _ = core.Route{}
