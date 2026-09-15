// P4.10b — the invitation surface (views/project/invite/show.pug +
// not-valid.pug + the generic 403 error page used by the split-test-disabled
// sharing routes).
package views

import (
	"net/http"
	"strings"
)

// Slot markers (P4.10b; see pages_invite_data.go):
const (
	slotInvToken = "\x01INVTOK\x02"
	slotInvProj  = "\x01INVPROJ\x02"
	slotInvName  = "\x01INVNAME\x02"
	slotInvMail  = "\x01INVMAIL\x02"
	slotInvUID   = "\x01INVUID\x02"
	slotInvUser  = "\x01INVUSERMETA\x02"
)

// applyInviteSlots fills the P4.10b markers (run after PageData.finalize,
// which already handled CSRF/NONCE/PATH). mail/uid/meta are the session
// user values ("" for anonymous — the gate never renders these pages
// anonymous, so the anonymous shape is best-effort).
func applyInviteSlots(html, mail, uid, meta string, inv struct {
	Token string
	Proj  string
	Name  string
	User  string // pre-escaped ol-user meta content
}) string {
	html = strings.ReplaceAll(html, slotInvToken, inv.Token)
	html = strings.ReplaceAll(html, slotInvProj, inv.Proj)
	html = strings.ReplaceAll(html, slotInvName, inv.Name)
	html = strings.ReplaceAll(html, slotInvMail, htmlAttrEsc(mail))
	html = strings.ReplaceAll(html, slotInvUID, htmlAttrEsc(uid))
	html = strings.ReplaceAll(html, slotInvUser, inv.User)
	return html
}

// InviteMetaJSON builds the ol-user meta CONTENT in Node's quoted-attribute
// form (inner JSON quotes escaped as &quot;). The value goes straight into
// content="..." without further escaping.
func InviteMetaJSON(email, firstName, lastName string) string {
	a := func(x string) string { return strings.ReplaceAll(x, "&", "&amp;") }
	return `{&quot;email&quot;:&quot;` + a(email) + `&quot;,&quot;first_name&quot;:&quot;` + a(firstName) + `&quot;,&quot;last_name&quot;:&quot;` + a(lastName) + `&quot;}`
}

// InvitePage — GET /project/:id/invite/token/:token (valid invite, the
// 'Project Invite' React shell). meta is the pre-escaped ol-user content
// ("" when there is no session user).
func InvitePage(w http.ResponseWriter, d PageData, inv struct {
	Token string
	Proj  string
	Name  string
	User  string
}) {
	d.CSP = cspReact(d.Nonce)
	Page(w, d, applyInviteSlots(inviteShowHTML, d.UserEmail, d.UserID, inv.User, inv))
}

// InviteNotValidPage — the 404 'Invalid Invite' view (bad token / missing
// owner on a real project).
func InviteNotValidPage(w http.ResponseWriter, d PageData, inv struct {
	Token string
	Proj  string
	Name  string
	User  string
}) {
	d.CSP = cspReact(d.Nonce)
	StatusPage(w, d, 404, applyInviteSlots(inviteNotValidHTML, d.UserEmail, d.UserID, inv.User, inv))
}

// Forbidden403Page — the generic 403 error page for the split-test-disabled
// sharing-link / share / share-validate routes (PATH slot = request path,
// set via d.Path like the other StatusPage views).
func Forbidden403Page(w http.ResponseWriter, d PageData) {
	d.CSP = cspReact(d.Nonce)
	// The shared chrome (ol-usersEmail / account menu) carries the SESSION
	// user — fill with the session slots; no invite values on this page.
	StatusPage(w, d, 403, applyInviteSlots(forbidden403HTML, d.UserEmail, d.UserID, "", struct {
		Token string
		Proj  string
		Name  string
		User  string
	}{}))
}
