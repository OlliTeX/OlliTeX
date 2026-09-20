package instancestats

import (
	"context"
	"fmt"
	"log"
	"ollitex/go/services/web/core"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func getAlertConfig(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		if !a.RequireSiteAdmin(cxt, res) {
			return
		}
		ctx, cancel := context.WithTimeout(cxt.Req.Context(), 5*time.Second)
		defer cancel()
		db, err := a.Mongo.DB(ctx)
		if err != nil {
			res.SendStatus(500)
			return
		}
		doc := map[string]any{}
		if err := db.Collection("instanceStatAlertConfigs").
			FindOne(ctx, bson.D{{Key: "_id", Value: alertConfigID}}).Decode(&doc); err != nil {
			doc = map[string]any{} // no config doc → defaults (Node: config = null)
		}
		emails := configEmails(doc)
		first := ""
		if len(emails) > 0 {
			first = emails[0]
		}
		body := core.JSON(struct {
			AlertEmails        []string `json:"alertEmails"`
			AlertEmail         string   `json:"alertEmail"`
			DiskWarningPercent float64  `json:"diskWarningPercent"`
			RamWarningPercent  float64  `json:"ramWarningPercent"`
		}{emails, first, numberOr(doc["diskWarningPercent"], 90), numberOr(doc["ramWarningPercent"], 90)})
		res.JSON(200, body)
	}
}

// normalizeEmails — Node normalizeEmails(body).

// normalizeEmails — Node normalizeEmails(body).
func normalizeEmails(body map[string]any) ([]string, string) {
	var raw []string
	hasSa := false
	switch v := body["alertEmails"].(type) {
	case []any:
		hasSa = true
		for _, x := range v {
			if s, ok := x.(string); ok {
				raw = append(raw, s)
			}
		}
	case primitive.A:
		hasSa = true
		for _, x := range v {
			if s, ok := x.(string); ok {
				raw = append(raw, s)
			}
		}
	case []string:
		hasSa = true
		raw = v
	case string:
		hasSa = true
		raw = splitRe.Split(v, -1)
	}
	// Node: the legacy alertEmail string is used ONLY when alertEmails is
	// not an array and not a string.
	if !hasSa {
		if s, ok := body["alertEmail"].(string); ok {
			raw = splitRe.Split(s, -1)
		}
	}
	seen := map[string]bool{}
	emails := []string{}
	for _, s := range raw {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] {
			continue
		}
		if !validEmail(s) {
			return nil, fmt.Sprintf("Invalid email address: %s", s)
		}
		seen[s] = true
		emails = append(emails, s)
	}
	return emails, ""
}

// toFloat — JSON number → float; bool/string/nil → not-a-number
// (Node: typeof value !== 'number').

// parseAlertConfigBody — Node parseAlertConfigBody(req.body) with the exact
// `{"message": ...}` error strings (pinned).
func parseAlertConfigBody(body map[string]any) (map[string]any, string) {
	emails, errStr := normalizeEmails(body)
	if errStr != "" {
		return nil, errStr
	}
	badErr := "must be a number between 1 and 100"
	disk, okD := toFloat(body["diskWarningPercent"])
	if !okD || disk < 1 || disk > 100 {
		return nil, "diskWarningPercent " + badErr
	}
	ram, okR := toFloat(body["ramWarningPercent"])
	if !okR || ram < 1 || ram > 100 {
		return nil, "ramWarningPercent " + badErr
	}
	first := ""
	if len(emails) > 0 {
		first = emails[0]
	}
	// Node passes the JS numbers through to $set: mongoose stores them as
	// BSON doubles (not int64). Non-integers (55.5) are legal and must
	// round-trip exactly.
	return map[string]any{
		"alertEmails":        emails,
		"alertEmail":         first,
		"diskWarningPercent": disk,
		"ramWarningPercent":  ram,
	}, ""
}

func saveAlertConfig(a *core.App) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		if !a.RequireSiteAdmin(cxt, res) {
			return
		}
		body := parseJSONBody(cxt.Req)
		parsed, msg := parseAlertConfigBody(body)
		if msg != "" {
			b := core.JSON(map[string]string{"message": msg})
			res.JSON(400, b)
			return
		}
		ctx, cancel := context.WithTimeout(cxt.Req.Context(), 5*time.Second)
		defer cancel()
		db, err := a.Mongo.DB(ctx)
		if err != nil {
			res.SendStatus(500)
			return
		}
		if _, err := db.Collection("instanceStatAlertConfigs").UpdateOne(ctx,
			bson.D{{Key: "_id", Value: alertConfigID}},
			bson.D{{Key: "$set", Value: parsed}},
			options.Update().SetUpsert(true),
		); err != nil {
			res.SendStatus(500)
			return
		}
		res.JSON(200, []byte(`{"ok":true}`))
	}
}

// sendTestAlert — Node sendTestAlertEmail: body {emails?: string|string[],
// email?: string}; one mail per normalized recipient; 400 shapes pinned.

// sendTestAlert — Node sendTestAlertEmail: body {emails?: string|string[],
// email?: string}; one mail per normalized recipient; 400 shapes pinned.
func sendTestAlert(a *core.App, mail *core.Mail) func(*core.Cxt, *core.Res) {
	return func(cxt *core.Cxt, res *core.Res) {
		if !a.RequireSiteAdmin(cxt, res) {
			return
		}
		body := parseJSONBody(cxt.Req)
		// Node reshapes req.body before normalizeEmails:
		//   { alertEmails: (typeof emails === 'string' ? emails.split(/[\s,;]+/)
		//                    : emails), alertEmail: email }
		nbody := map[string]any{}
		if v, ok := body["emails"]; ok {
			switch t := v.(type) {
			case string:
				// Keep the STRING — normalizeEmails splits it (Node: the split
				// result array is passed; observationally identical). The
				// split must NOT happen here: a pre-split []string is not a
				// case normalizeEmails handles and would silently drop all
				// recipients (P3.2 live diff: mail_legacy 500 / mail_priority
				// 400 family).
				nbody["alertEmails"] = t
			default:
				nbody["alertEmails"] = v
			}
		}
		if v, ok := body["email"]; ok {
			if s, ok := v.(string); ok {
				nbody["alertEmail"] = s
			}
		}
		emails, errStr := normalizeEmails(nbody)
		if errStr != "" {
			b := core.JSON(map[string]string{"message": errStr})
			res.JSON(400, b)
			return
		}
		if len(emails) == 0 {
			res.JSON(400, []byte(`{"message":"Invalid email address"}`))
			return
		}
		subject := "[Overleaf] Instance stats alert test"
		html := "<p>This is a test email from the Instance Statistics alert configuration.</p>"
		text := "This is a test email from the Instance Statistics alert configuration."
		if mail != nil {
			for _, to := range emails {
				if err := mail.Send(to, subject, text, html); err != nil {
					log.Printf("instancestats: mail send to %s failed: %v", to, err)
					res.SendStatus(500)
					return
				}
			}
		}
		b := core.JSON(map[string]any{"ok": true, "sentTo": emails})
		res.JSON(200, b)
	}
}
