package emailtemplates

import (
	"testing"
)

// Byte-parity pins: the rendered defaults MUST equal the exact strings the
// call sites built inline before slice B (the owner-rebranded differences
// are intentional and called out per pin).

func pin(t *testing.T, slot string, vars map[string]string, wantSubject, wantText, wantHTML string) {
	t.Helper()
	r, err := Render(Registry, map[string]Override{}, slot, vars)
	if err != nil {
		t.Fatal(err)
	}
	if r.Subject != wantSubject {
		t.Errorf("[%s] subject = %q\nwant    %q", slot, r.Subject, wantSubject)
	}
	if wantText != "" && r.Text != wantText {
		t.Errorf("[%s] text = %q\nwant   %q", slot, r.Text, wantText)
	}
	if wantHTML != "" && r.HTML != wantHTML {
		t.Errorf("[%s] html = %q\nwant   %q", slot, r.HTML, wantHTML)
	}
}

func TestPinActivateAccount(t *testing.T) {
	// registration/adminusers parity (appName = OlliTeX)
	pin(t, "activate-account", map[string]string{
		"app": "OlliTeX", "email": "u@x.test", "link": "http://s/a?token=t&user_id=h",
		"adminEmail": "admin@x.test", "site": "http://s",
	},
		"Activate your OlliTeX Account",
		"Hi,\n\nCongratulations, you've just had an account created for you on OlliTeX with the email address 'u@x.test'.\n\nClick here to set your password and log in:\n\nSet password: http://s/a?token=t&user_id=h\n\nIf you have any questions or problems, please contact admin@x.test\n\nRegards,\nThe OlliTeX Team - http://s\n",
		"<p>Hi,</p><p>Congratulations, you've just had an account created for you on OlliTeX with the email address 'u@x.test'.</p><p>Click here to set your password and log in:</p><p><a href=\"http://s/a?token=t&user_id=h\">Set password</a></p><p>If you have any questions or problems, please contact admin@x.test</p><p>Regards,<br/>The OlliTeX Team - http://s</p>",
	)
}

func TestPinSecurityNote(t *testing.T) {
	// adminusers parity (the subject's "Overleaf" is the intentional owner
	// rebrand to OlliTeX; everything else byte-identical)
	pin(t, "security-note", map[string]string{
		"app": "OlliTeX", "action": "Password changed",
		"description": "A password reset link was generated",
		"adminEmail":  "admin@x.test",
	},
		"OlliTeX security note: Password changed",
		"Hi,\n\nPassword changed.\n\nA password reset link was generated.\n\nThis is just a security notification — no action is required.\nIf you did not do this, please contact admin@x.test immediately.\n",
		"<p>Hi,</p><p>Password changed.</p><p>A password reset link was generated.</p><p>This is just a security notification — no action is required.</p><p>If you did not do this, please contact admin@x.test immediately.</p>",
	)
}

func TestPinSessionsCleared(t *testing.T) {
	// userpages parity (subject "Overleaf" → OlliTeX is the intentional
	// rebrand; the rest byte-identical)
	pin(t, "sessions-cleared", map[string]string{
		"app": "OlliTeX", "datetime": "Monday 21 September 2026 at 10:30",
		"email": "u@x.test", "guideUrl": "http://s/learn/how-to/Keeping_your_account_secure",
	},
		"OlliTeX security note: active sessions cleared",
		"Hi there,\n\nActive sessions cleared\n\nMonday 21 September 2026 at 10:30\n\nactive sessions were cleared on your account u@x.test\n\nQuick guide: http://s/learn/how-to/Keeping_your_account_secure\n\nThanks,\nOlliTeX Team\n",
		"<html><body><h1>Active sessions cleared</h1><p>Monday 21 September 2026 at 10:30</p><p>active sessions were cleared on your account u@x.test</p><p><a href=\"http://s/learn/how-to/Keeping_your_account_secure\">quick guide</a></p></body></html>",
	)
}

func TestPinCollabRequested(t *testing.T) {
	// collab parity: "<first> <last> (<email>) requested <role> access to <name> - OlliTeX"
	pin(t, "collab-access-requested", map[string]string{
		"app": "OlliTeX", "project": "My Paper", "first": "J", "last": "Doe",
		"email": "j@x.test", "roleWord": "editor",
	},
		"J Doe (j@x.test) requested editor access to My Paper - OlliTeX",
		"J Doe (j@x.test) requested editor access to My Paper - OlliTeX",
		"",
	)
}

func TestPinMailConfigTest(t *testing.T) {
	// sitesettings parity (the "Overleaf admin console" brand position is the
	// intentional rebrand to OlliTeX; everything else byte-identical)
	pin(t, "mail-config-test", map[string]string{
		"app": "OlliTeX", "sentAt": "2026-09-21T10:30:00Z",
		"via": "driver=smtp host=mx.example port=465",
	},
		"[OlliTeX] E-mail configuration test",
		"This is a test e-mail sent from the OlliTeX admin console (Manage Site → E-mail).\n\nSent at: 2026-09-21T10:30:00Z\nVia: driver=smtp host=mx.example port=465.",
		"<p>This is a test e-mail sent from the OlliTeX admin console (Manage Site → E-mail).</p><p>Sent at: 2026-09-21T10:30:00Z via <code>driver=smtp host=mx.example port=465</code></p>",
	)
}

func TestPinInstanceStatsTest(t *testing.T) {
	// instancestats parity (the "[Overleaf]" prefix is the intentional
	// rebrand; body byte-identical)
	pin(t, "instance-stats-test", map[string]string{"app": "OlliTeX"},
		"[OlliTeX] Instance stats alert test",
		"This is a test email from the Instance Statistics alert configuration.",
		"<p>This is a test email from the Instance Statistics alert configuration.</p>",
	)
}

func TestPinGitToken(t *testing.T) {
	// gitbridge parity: wire = SendExact(email, "", subject+"\n\n"+text)
	// (the "Overleaf security note" subject is the intentional rebrand)
	pin(t, "git-token", map[string]string{"app": "OlliTeX", "email": "u@x.test"},
		"OlliTeX security note: new Git authentication token generated",
		"A new Git authentication token has been generated for your account u@x.test. If you did not do this, disable the token in your account settings and change your password as soon as possible.",
		"",
	)
}

func TestPinTestMail(t *testing.T) {
	// notifications + launchpad parity (identical shapes pre-move)
	pin(t, "test-mail", map[string]string{"app": "OlliTeX", "site": "http://s"},
		"A Test Email from OlliTeX",
		"Hi,\n\nThis is a test Email from OlliTeX\n\nOpen OlliTeX: http://s\n\nRegards,\nThe OlliTeX Team - http://s\n",
		"<p>Hi,</p><p>This is a test Email from OlliTeX</p><p><a href=\"http://s\">Open OlliTeX</a></p><p>Regards,<br/>The OlliTeX Team - http://s</p>",
	)
}

func TestPinPasswordReset(t *testing.T) {
	pin(t, "password-reset", map[string]string{
		"app": "OlliTeX", "link": "http://s/user/password/set?passwordResetToken=t&email=u%40x.test",
	},
		"Password Reset - OlliTeX",
		"We got a request to reset your OlliTeX password.\n\nReset password: http://s/user/password/set?passwordResetToken=t&email=u%40x.test\n\nIf you ignore this message, your password won't be changed.\nIf you didn't request a password reset, let us know.",
		"<p>We got a request to reset your OlliTeX password.</p><p><a href=\"http://s/user/password/set?passwordResetToken=t&email=u%40x.test\">Reset password</a></p><p>If you ignore this message, your password won't be changed.<br>If you didn't request a password reset, let us know.</p>",
	)
}

func TestPinDeclinedGrantedOwnership(t *testing.T) {
	pin(t, "collab-access-declined", map[string]string{"app": "OlliTeX", "project": "P"},
		"Your access request to P was declined - OlliTeX",
		"Your access request to P was declined - OlliTeX", "")
	pin(t, "collab-access-granted", map[string]string{"app": "OlliTeX", "project": "P"},
		"Your access request to P was granted - OlliTeX",
		"Your access request to P was granted - OlliTeX", "")
	pin(t, "ownership-transfer", map[string]string{"app": "OlliTeX"},
		"Project ownership transfer - OlliTeX",
		"Project ownership transfer - OlliTeX", "")
}
