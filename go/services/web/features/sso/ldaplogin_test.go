package sso

import "testing"

// Oracle: utils.mjs splitFullName.
func TestLdapSplitFullName(t *testing.T) {
	cases := []struct{ in, wantFirst, wantLast string }{
		{"Alice", "", "Alice"},
		{"John von Neumann", "John von", "Neumann"},
		{"  Jane Doe  ", "Jane", "Doe"},
		{"a b c", "a b", "c"},
		{"", "", ""},
		{"  ", "", ""},
	}
	for _, c := range cases {
		f, l := ldapSplitFullName(c.in)
		if f != c.wantFirst || l != c.wantLast {
			t.Errorf("ldapSplitFullName(%q) = (%q,%q); want (%q,%q)", c.in, f, l, c.wantFirst, c.wantLast)
		}
	}
}

// Oracle: LDAPAuthenticationManager.findOrCreateUser attribute mapping.
func TestLdapMapProfile(t *testing.T) {
	t.Run("email array first lowercased + cn name fallback", func(t *testing.T) {
		p := &LDAPProvider{EmailAtt: "mail", NameAtt: "cn", IsAdminAtt: "groups", ValAdmin: "admin"}
		prof := ssoProfile{"mail": []string{"User@Example.COM"}, "cn": "John von Neumann"}
		email, first, last, admin := ldapMapProfile(p, prof)
		if email != "user@example.com" {
			t.Errorf("email = %q; want user@example.com", email)
		}
		if first != "John von" {
			t.Errorf("firstName = %q; want 'John von'", first)
		}
		if last != "Neumann" {
			t.Errorf("lastName = %q; want Neumann", last)
		}
		if admin {
			t.Errorf("isAdmin = true; want false (groups absent)")
		}
	})

	t.Run("explicit attFirstName/attLastName override the name fallback", func(t *testing.T) {
		p := &LDAPProvider{EmailAtt: "mail", FirstNameAtt: "givenName", LastNameAtt: "sn"}
		prof := ssoProfile{"mail": "a@b.c", "givenName": "Ada", "sn": "Lovelace"}
		_, first, last, _ := ldapMapProfile(p, prof)
		if first != "Ada" || last != "Lovelace" {
			t.Errorf("first/last = (%q,%q); want (Ada,Lovelace)", first, last)
		}
	})

	t.Run("lastName = email when both names empty", func(t *testing.T) {
		p := &LDAPProvider{EmailAtt: "mail"}
		prof := ssoProfile{"mail": "only@x.org"}
		_, first, last, _ := ldapMapProfile(p, prof)
		if first != "" || last != "only@x.org" {
			t.Errorf("first/last = (%q,%q); want ('',only@x.org)", first, last)
		}
	})

	t.Run("isAdmin array includes(val)", func(t *testing.T) {
		p := &LDAPProvider{EmailAtt: "mail", IsAdminAtt: "member", ValAdmin: "admin"}
		if _, _, _, admin := ldapMapProfile(p, ssoProfile{"mail": "x@y", "member": []string{"users", "admin"}}); !admin {
			t.Errorf("isAdmin = false; want true (array includes)")
		}
	})

	t.Run("isAdmin string equals(val)", func(t *testing.T) {
		p := &LDAPProvider{EmailAtt: "mail", IsAdminAtt: "role", ValAdmin: "admin"}
		if _, _, _, admin := ldapMapProfile(p, ssoProfile{"mail": "x@y", "role": "admin"}); !admin {
			t.Errorf("isAdmin = false; want true (string eq)")
		}
	})

	t.Run("isAdmin false when value does not match", func(t *testing.T) {
		p := &LDAPProvider{EmailAtt: "mail", IsAdminAtt: "member", ValAdmin: "admin"}
		if _, _, _, admin := ldapMapProfile(p, ssoProfile{"mail": "x@y", "member": []string{"users"}}); admin {
			t.Errorf("isAdmin = true; want false (no match)")
		}
	})

	t.Run("default emailAtt=mail when unset", func(t *testing.T) {
		p := &LDAPProvider{}
		if email, _, _, _ := ldapMapProfile(p, ssoProfile{"mail": "Default@Mail"}); email != "default@mail" {
			t.Errorf("email = %q; want default@mail (default attEmail)", email)
		}
	})

	t.Run("string email (not array) lowercased", func(t *testing.T) {
		p := &LDAPProvider{EmailAtt: "uid"}
		if email, _, _, _ := ldapMapProfile(p, ssoProfile{"uid": "UID@Ex.COM"}); email != "uid@ex.com" {
			t.Errorf("email = %q; want uid@ex.com", email)
		}
	})
}
