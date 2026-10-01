package sso

// audit.go — SSO audit rows (Node parity):
//
//   - SamlLog (modules/authentication/saml/app/src/SamlLog.mjs):
//     {user_id, samlProviderId, externalUserId, event, info, ip} →
//     collection 'samlLog'.
//   - auditSsoLoginDenied (ssoRoleEvaluator.mjs): UserAuditLog entry
//     'sso-login-denied' (ip, providerId, reason).
//   - login 'SAML login' / 'OIDC login' audit info (Node setAuditInfo).
//     The Go app writes userAuditLogEntries (repo convention).

import (
	"encoding/json"
	"time"

	"ollitex/go/services/web/core"

	"go.mongodb.org/mongo-driver/v2/bson"
)

var _ = json.Marshal
var _ = bson.M{}
var _ = time.Time{}

func jsonUnmarshal(raw []byte, v any) error {
	return json.Unmarshal(raw, v)
}

// samlLog — SamlLog.mjs parity: one row per SAML event (login
// success/fail/denied + logout).
func samlLog(a *core.App, cxt *core.Cxt, providerID, event, detail string) {
	if a == nil || a.Mongo == nil {
		return
	}
	db, err := a.Mongo.DB(cxt.Req.Context())
	if err != nil {
		return
	}
	row := bson.M{
		"samlProviderId": providerID,
		"event":          event,
		"info":           detail,
		"ip":             cxt.Req.RemoteAddr,
		"createdAt":      time.Now().UTC(),
	}
	if cxt.Sess != nil {
		if raw, ok := cxt.Sess.GetRaw("samlExtce"); ok && len(raw) > 0 {
			var ext map[string]any
			if jsonUnmarshal(raw, &ext) == nil {
				if nid, ok := ext["nameID"]; ok {
					row["externalUserId"] = nid
				}
			}
		} else if uid := cxt.Sess.UserIDHex(); uid != "" {
			row["user_id"] = uid
		}
	}
	_, _ = db.Collection("samlLog").InsertOne(cxt.Req.Context(), row)
}

// auditSsoDenied — Node auditSsoLoginDenied parity (UserAuditLog
// 'sso-login-denied').
func auditSsoDenied(a *core.App, cxt *core.Cxt, uid string, providerID, reason string) {
	if a == nil || a.Mongo == nil {
		return
	}
	db, err := a.Mongo.DB(cxt.Req.Context())
	if err != nil {
		return
	}
	row := bson.M{
		"operation": "sso-login-denied",
		"ip":        cxt.Req.RemoteAddr,
		"info":      bson.M{"providerId": providerID, "reason": bson.M{"method": reason}},
		"createdAt": time.Now().UTC(),
		"updatedAt": time.Now().UTC(),
	}
	if uid == "" {
		if s, ok := auditUserHex(cxt); ok {
			uid = s
		}
	}
	if uid != "" {
		row["userId"] = uid
		row["initiatorId"] = uid
	}
	_, _ = db.Collection("userAuditLogEntries").InsertOne(cxt.Req.Context(), row)
}

// auditEntryRow — generic userAuditLogEntries writer (Node
// UserAuditLogHandler.addEntry parity: {userId, operation, ip, info,
// initiatorId?, createdAt, updatedAt}).
func auditEntryRow(a *core.App, cxt *core.Cxt, uid, operation string, info bson.M) {
	if a == nil || a.Mongo == nil {
		return
	}
	db, err := a.Mongo.DB(cxt.Req.Context())
	if err != nil {
		return
	}
	row := bson.M{
		"operation": operation,
		"ip":        cxt.Req.RemoteAddr,
		"createdAt": time.Now().UTC(),
		"updatedAt": time.Now().UTC(),
	}
	if info != nil {
		row["info"] = info
	}
	if uid != "" {
		row["userId"] = uid
		row["initiatorId"] = uid
	}
	_, _ = db.Collection("userAuditLogEntries").InsertOne(cxt.Req.Context(), row)
}

// auditUserHex — session user id ("" when anonymous).
func auditUserHex(cxt *core.Cxt) (string, bool) {
	if cxt.Sess == nil {
		return "", false
	}
	uid := cxt.Sess.UserIDHex()
	return uid, uid != ""
}
