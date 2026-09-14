package registrationpage

import (
	"context"
	"ollitex/go/services/web/core"
	"os"
	"strings"

	"go.mongodb.org/mongo-driver/bson"
)

// ---- sign-up site-settings (stored section over env seeds; fail Open) ----

type signupCfg struct {
	Enabled             bool
	AllowedEmailDomains []string
	DisabledRedirectURL string
}

func readSignup(a *core.App, ctx context.Context) signupCfg {
	def := defaultSignup()
	if a.Mongo == nil {
		return def
	}
	d, err := a.Mongo.DB(ctx)
	if err != nil {
		return def // fail open
	}
	var doc struct {
		Signup struct {
			Enabled             *bool     `bson:"enabled"`
			AllowedEmailDomains *[]string `bson:"allowedEmailDomains"`
			DisabledRedirectURL *string   `bson:"disabledRedirectUrl"`
		} `bson:"signup"`
	}
	if err := d.Collection("site_settings").FindOne(ctx, bson.M{"_id": "global"}).Decode(&doc); err != nil {
		return def
	}
	s := def
	if doc.Signup.Enabled != nil {
		s.Enabled = *doc.Signup.Enabled
	}
	if doc.Signup.AllowedEmailDomains != nil {
		s.AllowedEmailDomains = *doc.Signup.AllowedEmailDomains
	}
	if doc.Signup.DisabledRedirectURL != nil {
		s.DisabledRedirectURL = *doc.Signup.DisabledRedirectURL
	}
	return s
}

func defaultSignup() signupCfg {
	s := signupCfg{Enabled: true, AllowedEmailDomains: []string{}}
	if v := os.Getenv("OVERLEAF_ENABLE_REGISTRATION_PAGE"); v != "" {
		s.Enabled = strings.EqualFold(v, "true") || v == "1"
	}
	if v := os.Getenv("OVERLEAF_ALLOWED_REGISTRATION_EMAIL_DOMAINS"); v != "" {
		s.AllowedEmailDomains = splitDomains(v)
	}
	if v := os.Getenv("OVERLEAF_REGISTRATION_DISABLED_REDIRECT"); v != "" {
		s.DisabledRedirectURL = v
	}
	return s
}

func splitDomains(v string) []string {
	var out []string
	for _, p := range strings.FieldsFunc(v, func(r rune) bool { return r == ',' || r == ' ' || r == '\t' }) {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func domainAllowed(domains []string, email string) bool {
	if len(domains) == 0 {
		return true
	}
	i := strings.LastIndex(email, "@")
	if i < 0 || i+1 >= len(email) {
		return false
	}
	domain := email[i+1:]
	for _, pattern := range domains {
		if strings.HasPrefix(pattern, "*.") {
			if strings.HasSuffix(domain, "."+pattern[2:]) {
				return true
			}
			continue
		}
		if domain == pattern {
			return true
		}
	}
	return false
}

func reversedHostname(email string) string {
	i := strings.LastIndex(email, "@")
	hostname := email
	if i >= 0 && i+1 < len(email) {
		hostname = email[i+1:]
	}
	r := []rune(hostname)
	for a, b := 0, len(r)-1; a < b; a, b = a+1, b-1 {
		r[a], r[b] = r[b], r[a]
	}
	return string(r)
}
