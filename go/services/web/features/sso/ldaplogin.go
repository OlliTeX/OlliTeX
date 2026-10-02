package sso

// ldaplogin.go — LDAP authentication contract logic (TODO fedgap-6 / 6b).
//
// Oracle-pinned to the Node overleaf-fed LDAP module:
//
//   - modules/authentication/ldap/app/src/LDAPAuthenticationManager.mjs
//     `findOrCreateUser` — the attribute-mapping algorithm (exactly).
//   - modules/authentication/ssoConfigLoader.mjs `getLDAPConfig` — the
//     sso-ldap field map (emailAtt, attName, attAdmin/valAdmin, …).
//   - modules/authentication/utils.mjs `splitFullName` (exactly).
//
// The BIND/SEARCH transport (go-ldap dial/bind/search + bind-as-user), the
// find-or-create JIT (mirror samlJIT/oidcJIT + P1c roles + audit), and the
// rate-limited login route are the focused continuation on top of this
// verified contract (see the 6b TODO body). This file holds the
// algorithmically-dense, edge-case-rich part of LDAP auth — the attribute
// mapping — implemented for Go parity and covered by ldaplogin_test.go,
// with zero I/O and zero new dependencies.

import "strings"

// ldapMapProfile — exact Node `findOrCreateUser` attribute mapping.
//
// Node (Settings.ldap = the sso-ldap section):
//
//	email     attEmail (default "mail"): array→first || string, lowercased
//	names     (!attFirstName || !attLastName) && attName → splitFullName(cn)
//	firstName attFirstName ? profile[attFirstName] || "" : names[0]
//	lastName  attLastName  ? profile[attLastName]  || "" : names[1]
//	if (!firstName && !lastName) lastName = email
//	isAdmin   attAdmin && valAdmin ? (array.includes(val) || string===val)
//
// LDAP attribute values arrive as []string (multi-valued); [0] semantics are
// shared with the existing firstString helper.
func ldapMapProfile(p *LDAPProvider, profile ssoProfile) (email, firstName, lastName string, isAdmin bool) {
	attEmail := p.EmailAtt
	if attEmail == "" {
		attEmail = "mail"
	}
	email = strings.ToLower(firstString(profile[attEmail]))

	var nFirst, nLast string
	if (p.FirstNameAtt == "" || p.LastNameAtt == "") && p.NameAtt != "" {
		nFirst, nLast = ldapSplitFullName(firstString(profile[p.NameAtt]))
	}
	if p.FirstNameAtt != "" {
		firstName = firstString(profile[p.FirstNameAtt])
	} else {
		firstName = nFirst
	}
	if p.LastNameAtt != "" {
		lastName = firstString(profile[p.LastNameAtt])
	} else {
		lastName = nLast
	}
	if firstName == "" && lastName == "" {
		lastName = email
	}
	isAdmin = attrEq(profile, p.IsAdminAtt, p.ValAdmin)
	return email, firstName, lastName, isAdmin
}

// ldapSplitFullName — exact Node utils.splitFullName:
//
//	trim; split at the LAST space; return [before, after] (both trimmed).
//	"Alice" → ["","Alice"] · "John von Neumann" → ["John von","Neumann"].
func ldapSplitFullName(fullName string) (firstNames, lastName string) {
	fullName = strings.TrimSpace(fullName)
	i := strings.LastIndex(fullName, " ")
	if i < 0 {
		return "", fullName
	}
	return strings.TrimSpace(fullName[:i]), strings.TrimSpace(fullName[i+1:])
}
