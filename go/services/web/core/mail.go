package core

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	goma "github.com/wneessen/go-mail"
)

func readMailLine(c net.Conn) (string, error) {
	r := bufio.NewReader(c)
	line, err := r.ReadString('\n')
	return strings.TrimRight(line, "\r\n"), err
}

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

// SendExact delivers a caller-built RFC 822 message byte-exact (raw SMTP
// dialog, no TLS — the e2e smtpsink is plaintext; this path exists so the
// subject header can carry Node nodemailer's exact RFC 2047 Q-encoding +
// folding, which go-mail's encoder would re-encode differently).
func (m *Mail) SendExact(to, replyTo, msg string) error {
	t := m.Timeout
	if t <= 0 {
		t = 15
	}
	_ = replyTo // carried inside the message headers by the caller
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(m.Host, strconv.Itoa(m.Port)), time.Duration(t)*time.Second)
	if err != nil {
		return err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(time.Duration(t) * time.Second))
	rx := bufio.NewReader(conn)
	readLine := func() (string, error) {
		ln, err := rx.ReadString('\n')
		return strings.TrimRight(ln, "\r\n"), err
	}
	cmd := func(line string) (string, error) {
		if _, err := fmt.Fprintf(conn, "%s\r\n", line); err != nil {
			return "", err
		}
		return readLine()
	}
	if ln, err := readLine(); err != nil || !okCode(ln, 220) {
		return fmt.Errorf("greeting: %q (err=%v)", ln, err)
	}
	// the sink answers EHLO with a multi-line 250- / 250  sequence; drain it all.
	if _, err := cmd("EHLO " + localDomain(m.From)); err != nil {
		return err
	}
	for {
		ln, err := readLine()
		if err != nil || !okCode(ln, 250) {
			return fmt.Errorf("ehlo: %q (err=%v)", ln, err)
		}
		if len(ln) >= 4 && ln[3] == ' ' {
			break
		}
	}
	from := envelopeFrom(m.From)
	if ln, err := cmd("MAIL FROM:<" + from + ">"); err != nil || !okCode(ln, 250) {
		return fmt.Errorf("mail from: %q", ln)
	}
	if ln, err := cmd("RCPT TO:<" + to + ">"); err != nil || !okCode(ln, 250) {
		return fmt.Errorf("rcpt to: %q", ln)
	}
	if ln, err := cmd("DATA"); err != nil || !okCode(ln, 354) {
		return fmt.Errorf("data: %q", ln)
	}
	if _, err := fmt.Fprint(conn, dotStuff(msg)); err != nil {
		return err
	}
	if ln, err := readLine(); err != nil || !okCode(ln, 250) {
		return fmt.Errorf("data end: %q (err=%v)", ln, err)
	}
	_, _ = cmd("QUIT")
	return nil
}

// dotStuff — RFC 5321: leading dots are doubled and the message is
// terminated with the CRLF-dot-CRLF line the sink matches on.
func dotStuff(msg string) string {
	lines := strings.Split(msg, "\n")
	for i, ln := range lines {
		if strings.HasPrefix(ln, ".") {
			lines[i] = "." + ln
		}
	}
	out := strings.Join(lines, "\n")
	out = strings.TrimSuffix(out, "\n")
	return out + "\r\n.\r\n"
}

func localDomain(from string) string {
	if i := strings.LastIndexByte(from, '@'); i >= 0 {
		if j := strings.IndexByte(from[i+1:], '>'); j >= 0 {
			return from[i+1 : i+1+j]
		}
		return from[i+1:]
	}
	return "localhost"
}

// okCode — first 3 digits equal code.
func okCode(line string, want int) bool {
	if len(line) < 4 || line[3] != ' ' {
		return false
	}
	n, err := strconv.Atoi(line[:3])
	return err == nil && n == want
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
