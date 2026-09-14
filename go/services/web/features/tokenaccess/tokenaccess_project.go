package tokenaccess

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"io"
	"ollitex/go/services/web/core"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// ---------- project helpers (shared with P3/P4 authorization) ----------

type projectDoc struct {
	ID                primitive.ObjectID `bson:"_id"`
	Name              string             `bson:"name"`
	OwnerRef          string             `bson:"owner_ref"`
	Tokens            map[string]any     `bson:"tokens"`
	PublicAccessLevel string             `bson:"publicAccesLevel"`
	RWRefs            []string           `bson:"tokenAccessReadAndWrite_refs"`
	RORefs            []string           `bson:"tokenAccessReadOnly_refs"`
	CollabRefs        []string           `bson:"collaberator_refs"`
	ReadonlyNamedRefs []string           `bson:"readOnly_refs"`
}

func (p *projectDoc) tokenRW() string { s, _ := p.Tokens["readAndWrite"].(string); return s }

func (p *projectDoc) tokenRO() string { s, _ := p.Tokens["readOnly"].(string); return s }

func (p *projectDoc) rwPrefix() string { s, _ := p.Tokens["readAndWritePrefix"].(string); return s }

func (p *projectDoc) inRefs(refs []string, uid string) bool {
	for _, r := range refs {
		if r == uid {
			return true
		}
	}
	return false
}

func findProjectByToken(a *core.App, ctx context.Context, rw bool, token string) (*projectDoc, bool) {
	db, err := a.Mongo.DB(ctx)
	if err != nil {
		return nil, false
	}
	var pd projectDoc
	if rw {
		prefix := digitPrefix(token)
		if err := db.Collection("projects").FindOne(ctx,
			bson.M{"tokens.readAndWritePrefix": prefix}).Decode(&pd); err != nil {
			return nil, false
		}
		// Node: crypto.timingSafeEqual(token, project.tokens.readAndWrite)
		// (length mismatch → Node throws → treated as no match here)
		if len(token) != len(pd.tokenRW()) ||
			subtle.ConstantTimeCompare([]byte(token), []byte(pd.tokenRW())) != 1 {
			return nil, false
		}
	} else {
		if err := db.Collection("projects").FindOne(ctx,
			bson.M{"tokens.readOnly": token}).Decode(&pd); err != nil {
			return nil, false
		}
	}
	return &pd, true
}

func findProject(a *core.App, ctx context.Context, idHex string) (*projectDoc, bool) {
	oaid, err := primitive.ObjectIDFromHex(idHex)
	if err != nil {
		return nil, false
	}
	db, err := a.Mongo.DB(ctx)
	if err != nil {
		return nil, false
	}
	var pd projectDoc
	if err := db.Collection("projects").FindOne(ctx, bson.M{"_id": oaid}).Decode(&pd); err != nil {
		return nil, false
	}
	return &pd, true
}

func digitPrefix(s string) string {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return s[:i]
		}
	}
	return s
}

// privilegeRank mirrors the CE privilege ordering for the "user already
// has higher or same privilege" shortcut.

// privilegeRank mirrors the CE privilege ordering for the "user already
// has higher or same privilege" shortcut.
func privilegeRank(uid string, p *projectDoc) string {
	if uid == "" {
		return "none"
	}
	if p.OwnerRef == uid {
		return "owner"
	}
	if p.inRefs(p.CollabRefs, uid) || p.inRefs(p.RWRefs, uid) {
		return "readAndWrite"
	}
	if p.inRefs(p.RORefs, uid) || p.inRefs(p.ReadonlyNamedRefs, uid) {
		return "readOnly"
	}
	return "none"
}

func rankNum(lvl string) int {
	switch lvl {
	case "owner":
		return 3
	case "readAndWrite":
		return 2
	case "readOnly":
		return 1
	}
	return 0
}

// ---------- checkAndGet (CE parity) ----------

type checkResult struct {
	project  *projectDoc
	notFound bool
	action   string
}

func checkAndGet(a *core.App, cxt *core.Cxt, token string, rw bool) checkResult {
	ctx, cancel := context.WithTimeout(cxt.Req.Context(), 8*time.Second)
	defer cancel()
	uid := ""
	if cxt.Sess != nil {
		_, uid = core.PassportUser(cxt.Sess)
	}
	anon := uid == ""

	p, ok := findProjectByToken(a, ctx, rw, token)
	if !ok || p == nil {
		// CE: Settings.overleaf absent → [null, null, {action:'404'}]
		return checkResult{notFound: true}
	}
	enabled := p.PublicAccessLevel == "tokenBased"
	if !enabled {
		// Node checks the token gate right after the lookup — before the
		// anon/authed branches and the higher-privilege shortcut.
		return checkResult{notFound: true}
	}
	if anon {
		if rw {
			// CE: ANONYMOUS_READ_AND_WRITE_ENABLED false → deny
			if cxt.Sess != nil {
				cxt.Sess.Set("postLoginRedirect", "/restricted")
			}
			return checkResult{project: p, action: actAnonRwDenied}
		}
		// anonymous readOnly grant (CE: allowed) → session token access
		if cxt.Sess != nil {
			m := map[string]string{}
			if raw, okk := cxt.Sess.GetRaw("anonTokenAccess"); okk {
				_ = json.Unmarshal(raw, &m)
			}
			m[p.ID.Hex()] = token
			cxt.Sess.Set("anonTokenAccess", m)
		}
		return checkResult{project: p, action: actAnonRoGrant}
	}
	// logged in
	got := privilegeRank(uid, p)
	target := "readOnly"
	if rw {
		target = "readAndWrite"
	}
	if rankNum(got) >= rankNum(target) {
		return checkResult{project: p, action: actHigherAccess}
	}
	return checkResult{project: p}
}

// ---------- grant ----------

// ---------- grant ----------

func readGrantBody(cxt *core.Cxt) (confirmed bool, hashPrefix string) {
	var b struct {
		ConfirmedByUser bool   `json:"confirmedByUser"`
		TokenHashPrefix string `json:"tokenHashPrefix"`
	}
	raw, _ := io.ReadAll(io.LimitReader(cxt.Req.Body, 1<<20))
	_ = json.Unmarshal(raw, &b)
	return b.ConfirmedByUser, b.TokenHashPrefix
}
