package sso

import (
	"context"
	"os"
	"testing"
)

// TestLdapAuthenticateLive is the REAL-SERVER E2E for the fedgap-6b LDAP
// transport: it dials an actual LDAP server (no fake dialer) and runs the
// exact service-bind → search → bind-as-user flow that POST /sso/ldap/login
// performs, then checks the attribute mapping (email/first/last + admin).
//
// It is skipped unless LIVE_LDAP_URL is set, so it is safe in CI without a
// server. Point it at a self-contained osixia/openldap with two seeded users:
//
//	LIVE_LDAP_URL="ldap://localhost:389"
//	LIVE_LDAP_BASE="ou=people,dc=example,dc=com"
//	LIVE_LDAP_BIND_DN="cn=ldap_reader,dc=example,dc=com"
//	LIVE_LDAP_BIND_PW="GoodNewsEveryone"
//	(plus the LIVE_LDAP_REG* / LIVE_LDAP_ADM* seeded-user creds, default to
//	 the ssoe2e / ssoadm fixtures in tests/tools/sso-test)
func TestLdapAuthenticateLive(t *testing.T) {
	url := os.Getenv("LIVE_LDAP_URL")
	if url == "" {
		t.Skip("LIVE_LDAP_URL not set (no real LDAP server for the live E2E)")
	}
	eDef := func(k, def string) string {
		if v := os.Getenv(k); v != "" {
			return v
		}
		return def
	}
	regUser := eDef("LIVE_LDAP_REG", "ssoe2e")
	regPw := eDef("LIVE_LDAP_REG_PW", "SsoE2ePass123")
	regEmail := eDef("LIVE_LDAP_REG_EMAIL", "ssoe2e@example.com")
	admUser := eDef("LIVE_LDAP_ADM", "ssoadm")
	admPw := eDef("LIVE_LDAP_ADM_PW", "SsoAdmPass123")
	admEmail := eDef("LIVE_LDAP_ADM_EMAIL", "ssoadm@example.com")

	cfg := func(attr string) *LDAPProvider {
		return &LDAPProvider{
			URL:             url,
			SearchBase:      eDef("LIVE_LDAP_BASE", "ou=people,dc=example,dc=com"),
			BindDN:          os.Getenv("LIVE_LDAP_BIND_DN"),
			BindCredentials: os.Getenv("LIVE_LDAP_BIND_PW"),
			BindProperty:    attr, // "uid" or "mail" (Overleaf usernameField=email)
			EmailAtt:        "mail",
			FirstNameAtt:    "givenName",
			LastNameAtt:     "sn",
			IsAdminAtt:      "employeeType",
			ValAdmin:        "admin",
			SearchScope:     eDef("LIVE_LDAP_SCOPE", ""),
			Timeout:         5000,
		}
	}
	ctx := context.Background()

	t.Run("wrong password is denied", func(t *testing.T) {
		_, err := ldapAuthenticate(ctx, cfg("uid"), regUser, "definitely-wrong-"+regUser)
		if err == nil {
			t.Fatalf("expected bind-as-user to REJECT the wrong password, got success")
		}
	})

	t.Run("regular user logs in (by uid) and maps to non-admin", func(t *testing.T) {
		prof, err := ldapAuthenticate(ctx, cfg("uid"), regUser, regPw)
		if err != nil {
			t.Fatalf("bind-as-user failed for correct password: %v", err)
		}
		email, first, last, admin := ldapMapProfile(cfg("uid"), prof)
		if email != regEmail {
			t.Fatalf("email = %q, want %q", email, regEmail)
		}
		t.Logf("mapped: email=%q first=%q last=%q admin=%v", email, first, last, admin)
		if admin {
			t.Fatalf("regular user must NOT be admin (got admin=true)")
		}
	})

	t.Run("login by email (Overleaf usernameField=email)", func(t *testing.T) {
		prof, err := ldapAuthenticate(ctx, cfg("mail"), regEmail, regPw)
		if err != nil {
			t.Fatalf("email-login bind failed: %v", err)
		}
		if email, _, _, _ := ldapMapProfile(cfg("mail"), prof); email != regEmail {
			t.Fatalf("email-login email = %q, want %q", email, regEmail)
		}
	})

	t.Run("admin user maps to admin=true", func(t *testing.T) {
		prof, err := ldapAuthenticate(ctx, cfg("uid"), admUser, admPw)
		if err != nil {
			t.Fatalf("admin bind failed: %v", err)
		}
		email, _, _, admin := ldapMapProfile(cfg("uid"), prof)
		if email != admEmail || !admin {
			t.Fatalf("admin case = (email=%q admin=%v), want (email=%q admin=true)", email, admin, admEmail)
		}
	})
}
