package core

import (
	"context"
	"os"
	"strconv"
	"strings"
	"time"

	goma "github.com/wneessen/go-mail"
)

// Mail — outbound SMTP parity for all web mails (P2 password-reset link,
// P3.2 instance-stats alert, P3.3 security alerts).
//
// Node CE (nodemailer) pins carried here:
//   - e2e/live transport: host `smtpsink`, port 1025, NO TLS (secure=false)
//   - from: the configured sender (settings.email.from)
//   - envelope MAIL FROM = the local address of the From header (nodemailer
//     parses the display name off the envelope — kept: envelopeFrom)
//   - multipart/alternative text+html (nodemailer shape)
//   - subject pinned per template, e.g. `Overleaf security note: <action>`
//     (the node template hardcodes "Overleaf" — keep it byte-exact)
//
// Transport: wneessen/go-mail (owner-approved mail stack for P3).
type Mail struct {
	Host    string
	Port    int
	Secure  bool
	From    string // e.g. `OlliTeX <no-reply@site.test>`
	Timeout int    // seconds; 0 = 15
}

// NewMail builds the transport from env, mirroring settings.js's email
// block (services/web/config + server-ce settings.js):
//
//	fromAddress: OVERLEAF_EMAIL_FROM_ADDRESS
//	  => { driver: smtp, host: OVERLEAF_EMAIL_SMTP_HOST, port: OVERLEAF_EMAIL_SMTP_PORT }
//
// MAIL_* fallbacks are kept for direct runs without the OVERLEAF_ env.
func NewMail() *Mail {
	m := &Mail{
		Host:    envOr("OVERLEAF_EMAIL_SMTP_HOST", envOr("MAIL_HOST", "smtpsink")),
		Secure:  envOr("OVERLEAF_EMAIL_SMTP_SECURE", envOr("MAIL_SECURE", "false")) == "true",
		From:    envOr("OVERLEAF_EMAIL_FROM_ADDRESS", envOr("MAIL_FROM", "OlliTeX <no-reply@localhost>")),
		Timeout: 15,
	}
	if p, err := strconv.Atoi(envOr("OVERLEAF_EMAIL_SMTP_PORT", envOr("MAIL_PORT", "1025"))); err == nil {
		m.Port = p
	}
	return m
}

func envOr(k, d string) string {
	if v := strings.TrimSpace(os.Getenv(k)); v != "" {
		return v
	}
	return d
}

// envelopeFrom — nodemailer sets the SMTP MAIL FROM to the parsed local
// address of the From header (the display name never rides the envelope).
func envelopeFrom(from string) string {
	if i := strings.LastIndexByte(from, '>'); i >= 0 {
		if j := strings.LastIndexByte(from[:i], '<'); j >= 0 {
			return from[j+1 : i]
		}
	}
	return from
}

// Send delivers a text+html message (multipart/alternative, the Node
// nodemailer shape) to `to` via SMTP.
func (m *Mail) Send(to, subject, text, html string) error {
	t := m.Timeout
	if t <= 0 {
		t = 15
	}
	// nodemailer `secure: false` semantics: opportunistic STARTTLS (upgrade
	// if the server advertises it, else plaintext). go-mail's DEFAULT is
	// TLSMandatory, which hard-fails against plaintext sinks
	// ("STARTTLS mode set to TLSMandatory, but target host does not
	// support STARTTLS") — the e2e smtpsink is plaintext, pinned P3.2.
	policy := goma.TLSOpportunistic
	if m.Secure {
		policy = goma.TLSMandatory
	}
	c, err := goma.NewClient(m.Host, goma.WithPort(m.Port), goma.WithTLSPolicy(policy))
	if err != nil {
		return err
	}
	defer c.Close()
	if env := envelopeFrom(m.From); env != "" {
		c.SetDomain(env)
	}
	msg := goma.NewMsg()
	if err := msg.EnvelopeFrom(envelopeFrom(m.From)); err != nil {
		return err
	}
	if err := msg.From(m.From); err != nil {
		return err
	}
	if err := msg.AddTo(to); err != nil {
		return err
	}
	msg.Subject(subject)
	msg.SetBodyString(goma.TypeTextPlain, text)
	msg.AddAlternativeString(goma.TypeTextHTML, html)
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(t)*time.Second)
	defer cancel()
	if err := c.DialAndSendWithContext(ctx, msg); err != nil {
		return err
	}
	if err := msg.SendError(); err != nil {
		return err
	}
	return nil
}

