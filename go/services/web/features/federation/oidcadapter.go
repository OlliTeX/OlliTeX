// B-side OIDC provider — S4a: the 7-method Redis adapter (oracle:
// modules/federation/oidc/RedisOidcProviderAdapter.mjs, oidc-provider
// v9.12.2 memory_adapter contract, plan 05 §8.2).
//
// The adapter is the ONLY persistence the OP engine uses. Key
// layout (04 §5, 05 §8.2):
//
//	federation:oidc:<model>:<id>        -> JSON payload doc
//	federation:oidc:sub:<uid>           -> Session doc id (sub-index)
//	federation:oidc:usercode:<code>     -> DeviceCode doc id (CIBA)
//	federation:oidc:grant:<grantId>     -> SET of grantable doc keys
//	federation:oidc:client:<clientId>   -> SET of token doc keys (sweep)
//	federation:oidc:account:<a>:<cl>    -> SET of grant doc keys (v2)
package federation

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// grantable — the exact set from memory_adapter.js (token doc models,
// NOT the Grant record: a consent Grant must never be swept, 06 §174).
var grantable = map[string]bool{
	"AccessToken":                      true,
	"AuthorizationCode":                true,
	"RefreshToken":                     true,
	"DeviceCode":                       true,
	"BackchannelAuthenticationRequest": true,
	"PreAuthorizedCode":                true,
}

// OidcRedisSeam — the Redis subset the OP adapter needs. Production
// wires *core.RedisClient (SETNX EX / SADD / ...); tests a fake.
type OidcRedisSeam interface {
	GET(key string) (string, bool, error)
	SETEX(key, value string, sec int64) error
	SETPX(key, value string, ms int64) error
	SADD(key string, members ...string) error
	SREM(key string, members ...string) error
	SMEMBERS(key string) ([]string, error)
	SCARD(key string) (int64, error)
	DEL(key string) error
	PTTL(key string) (int64, error)
}

// SETNoTTL — the adapter's no-EX writes (v9 default-ttl Grant path).
// Fake-only; production always passes a TTL (v9 sets each model's ttl
// before save), so a nil TTL is a test-construction artifact.
type noTTL interface {
	SETNoTTL(key, value string) error
}

func (a *OidcAdapter) setNoTTL(key, value string) error {
	if r, ok := a.Redis.(noTTL); ok {
		return r.SETNoTTL(key, value)
	}
	// Production: the no-EX path is never exercised (v9 always sets a
	// model ttl); degrade to a 0-PX set that real Redis rejects — the
	// nil-TTL case is test-construction-only.
	return a.Redis.SETPX(key, value, 0)
}

// FindByAccountAndClient — Content-Bridge v2 (plan 09 §2.1): the live
// consent grant jti for (accountID, clientID), or "". Resolves each
// member of the account SET and returns the first LIVE (pttl != -2)
// doc whose payload still holds the same pair. Soft no-consent ("" on
// any miss; the export path re-checks the grant via a provider.Grant
// load, so a stale index degrades to fresh consent, mirroring v1).
func FindByAccountAndClient(r OidcRedisSeam, accountID, clientID string) (string, error) {
	members, err := r.SMEMBERS(OIDCAccountIndexKey(accountID, clientID))
	if err != nil {
		return "", nil
	}
	for _, memberKey := range members {
		raw, ok, _ := r.GET(memberKey)
		if !ok {
			continue
		}
		ttl, _ := r.PTTL(memberKey)
		if ttl == -2 {
			continue
		}
		var payload map[string]any
		if err := json.Unmarshal([]byte(raw), &payload); err != nil {
			continue
		}
		if fmt.Sprint(payload["accountId"]) != accountID || fmt.Sprint(payload["clientId"]) != clientID {
			continue
		}
		if jti, ok := payload["jti"]; ok {
			return fmt.Sprint(jti), nil
		}
	}
	return "", nil
}

func OIDCDocKey(modelName, id string) string {
	return "federation:oidc:" + modelName + ":" + id
}

func OIDCAccountIndexKey(accountID, clientID string) string {
	return "federation:oidc:account:" + accountID + ":" + clientID
}

func subKey(uid any) string { return "federation:oidc:sub:" + fmt.Sprint(uid) }

// OidcAdapter — one model's adapter (mirrors the Node factory
// (modelName) => instance; v9 calls the methods per doc op).
type OidcAdapter struct {
	ModelName string
	Redis     OidcRedisSeam
}

// Find — the stored payload doc (v9 find returns the payload itself,
// NOT a wrapped doc). nil,nil on miss.
func (a *OidcAdapter) Find(id string) (map[string]any, error) {
	raw, ok, err := a.Redis.GET(OIDCDocKey(a.ModelName, id))
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return nil, err
	}
	return payload, nil
}

// FindByUID — Session-only sub-index (session.js verify(stored)).
func (a *OidcAdapter) FindByUID(uid string) (map[string]any, error) {
	raw, ok, err := a.Redis.GET("federation:oidc:sub:" + uid)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}
	return a.Find(raw)
}

// FindByUserCode — CIBA (disabled but cheap; unknown codes -> nil).
func (a *OidcAdapter) FindByUserCode(userCode string) (map[string]any, error) {
	raw, ok, err := a.Redis.GET("federation:oidc:usercode:" + userCode)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}
	return a.Find(raw)
}

// Upsert — the ONLY persistence point (base_model save()). expiresIn in
// SECONDS (v9 contract, plan 05 §8.6); nil -> no-EX (fake keeps it).
func (a *OidcAdapter) Upsert(id string, payload map[string]any, expiresIn *int64) error {
	key := OIDCDocKey(a.ModelName, id)
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if expiresIn != nil {
		if err := a.Redis.SETEX(key, string(raw), *expiresIn); err != nil {
			return err
		}
	} else {
		if err := a.setNoTTL(key, string(raw)); err != nil {
			return err
		}
	}
	if uid, ok := payload["uid"]; ok && uid != nil {
		if expiresIn != nil {
			if err := a.Redis.SETEX(subKey(uid), id, *expiresIn); err != nil {
				return err
			}
		} else {
			if err := a.setNoTTL(subKey(uid), id); err != nil {
				return err
			}
		}
	}
	if userCode, ok := payload["userCode"]; ok && userCode != nil {
		if expiresIn != nil {
			if err := a.Redis.SETEX("federation:oidc:usercode:"+fmt.Sprint(userCode), id, *expiresIn); err != nil {
				return err
			}
		} else {
			if err := a.setNoTTL("federation:oidc:usercode:"+fmt.Sprint(userCode), id); err != nil {
				return err
			}
		}
	}
	if grantable[a.ModelName] {
		if grantID, ok := payload["grantId"]; ok && grantID != nil {
			if err := a.Redis.SADD("federation:oidc:grant:"+fmt.Sprint(grantID), key); err != nil {
				return err
			}
		}
	}
	// Account secondary index (content-bridge v2, plan 09 §2.1): a Grant
	// doc owns one (accountId, clientId) pair.
	if a.ModelName == "Grant" {
		if accountID, ok := payload["accountId"]; ok && accountID != nil {
			if clientID, ok2 := payload["clientId"]; ok2 && clientID != nil {
				acctKey := OIDCAccountIndexKey(fmt.Sprint(accountID), fmt.Sprint(clientID))
				if err := a.Redis.SADD(acctKey, key); err != nil {
					return err
				}
			}
		}
	}
	// Client sweep index (04 §5 killOutstandingCodes): TOKEN docs only —
	// gated on GRANTABLE, NOT on clientId presence (a Grant has a client
	// id but is a record, not a token).
	if grantable[a.ModelName] {
		if clientID, ok := payload["clientId"]; ok && clientID != nil {
			setKey := "federation:oidc:client:" + fmt.Sprint(clientID)
			if err := a.Redis.SADD(setKey, key); err != nil {
				return err
			}
		}
	}
	return nil
}

// RevokeByGrantId — the revocation cascade: drop every grantable doc
// holding grantId (also the minting-client SET membership).
func (a *OidcAdapter) RevokeByGrantId(grantId string) error {
	setKey := "federation:oidc:grant:" + grantId
	members, err := a.Redis.SMEMBERS(setKey)
	if err != nil {
		return err
	}
	for _, memberKey := range members {
		raw, ok, err := a.Redis.GET(memberKey)
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		var payload map[string]any
		if err := json.Unmarshal([]byte(raw), &payload); err != nil {
			continue
		}
		if uid, ok := payload["uid"]; ok && uid != nil {
			_ = a.Redis.DEL(subKey(uid))
		}
		if userCode, ok := payload["userCode"]; ok && userCode != nil {
			_ = a.Redis.DEL("federation:oidc:usercode:" + fmt.Sprint(userCode))
		}
		if grantable[a.ModelName] {
			if clientID, ok := payload["clientId"]; ok && clientID != nil {
				clientSet := "federation:oidc:client:" + fmt.Sprint(clientID)
				_ = a.Redis.SREM(clientSet, memberKey)
				n, _ := a.Redis.SCARD(clientSet)
				if n == 0 {
					_ = a.Redis.DEL(clientSet)
				}
			}
		}
		_ = a.Redis.DEL(memberKey)
	}
	return a.Redis.DEL(setKey)
}

// RevokeClientCodes — 04 §5 `killOutstandingCodes` / 03 §4.3:
// destroy every token doc minted for one client and DEL the sweep SET.
// Idempotent (empty index -> 0; expired docs are a no-op DEL). Grants
// NEVER enter the sweep (06 §174: revocation affects tokens, never the
// consent record).
func (a *OidcAdapter) RevokeClientCodes(clientID string) (int, error) {
	setKey := "federation:oidc:client:" + clientID
	members, err := a.Redis.SMEMBERS(setKey)
	if err != nil {
		return 0, err
	}
	for _, memberKey := range members {
		rest, ok := strings.CutPrefix(memberKey, "federation:oidc:")
		if !ok {
			_ = a.Redis.SREM(setKey, memberKey)
			continue
		}
		model, id, ok := strings.Cut(rest, ":")
		if !ok {
			_ = a.Redis.SREM(setKey, memberKey)
			continue
		}
		sub := &OidcAdapter{ModelName: model, Redis: a.Redis}
		_ = sub.Destroy(id)
	}
	_ = a.Redis.DEL(setKey)
	return len(members), nil
}

// Destroy — remove a doc + every secondary index it wrote (the single
// grant-doc removal path). Idempotent on missing docs; trims SETs to
// zero-cardinality (SCARD==0 -> DEL).
func (a *OidcAdapter) Destroy(id string) error {
	key := OIDCDocKey(a.ModelName, id)
	raw, ok, err := a.Redis.GET(key)
	if err != nil {
		return err
	}
	if err := a.Redis.DEL(key); err != nil {
		return err
	}
	if !ok {
		return nil
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return nil
	}
	if uid, ok := payload["uid"]; ok && uid != nil {
		_ = a.Redis.DEL(subKey(uid))
	}
	if userCode, ok := payload["userCode"]; ok && userCode != nil {
		_ = a.Redis.DEL("federation:oidc:usercode:" + fmt.Sprint(userCode))
	}
	if a.ModelName == "Grant" {
		if accountID, ok := payload["accountId"]; ok && accountID != nil {
			if clientID, ok2 := payload["clientId"]; ok2 && clientID != nil {
				acctKey := OIDCAccountIndexKey(fmt.Sprint(accountID), fmt.Sprint(clientID))
				_ = a.Redis.SREM(acctKey, key)
				n, _ := a.Redis.SCARD(acctKey)
				if n == 0 {
					_ = a.Redis.DEL(acctKey)
				}
			}
		}
	}
	if grantable[a.ModelName] {
		if grantID, ok := payload["grantId"]; ok && grantID != nil {
			gKey := "federation:oidc:grant:" + fmt.Sprint(grantID)
			_ = a.Redis.SREM(gKey, key)
			n, _ := a.Redis.SCARD(gKey)
			if n == 0 {
				_ = a.Redis.DEL(gKey)
			}
		}
		if clientID, ok := payload["clientId"]; ok && clientID != nil {
			clientSet := "federation:oidc:client:" + fmt.Sprint(clientID)
			_ = a.Redis.SREM(clientSet, key)
			n, _ := a.Redis.SCARD(clientSet)
			if n == 0 {
				_ = a.Redis.DEL(clientSet)
			}
		}
	}
	return nil
}

// Consume — single-use flag (memory_adapter consume):// payload.consumed = epochSec, re-set with remaining ms TTL, NOT deleted
// (the revocation cascade still needs the doc, 02 §7.5).
func (a *OidcAdapter) Consume(id string) error {
	key := OIDCDocKey(a.ModelName, id)
	raw, ok, err := a.Redis.GET(key)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return err
	}
	nowSec := time.Now().Unix()
	payload["consumed"] = nowSec
	enc, _ := json.Marshal(payload)
	ttlMs, _ := a.Redis.PTTL(key)
	if ttlMs > 0 {
		return a.Redis.SETPX(key, string(enc), ttlMs)
	}
	return a.Redis.SETPX(key, string(enc), 0)
}
