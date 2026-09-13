package core

import (
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/smtp"
	"os"
	"strconv"
	"strings"
	"time"
)

// Mail — outbound SMTP parity for the password-reset link (P2).
//
// Node CE (nodemailer) pins carried here:
//   - e2e/live transport: host `smtpsink`, port 1025, NO TLS (secure=false)
//   - from: the configured sender (settings.email.from — `OlliTeX
//     <no-reply@...>` shape in the stack env)
//   - subject pinned by the battery: `Password Reset - OlliTeX`
//
// The e2e gate pins recipient + subject + the CTA link + delivery; exact
// MIME body parity is explicitly out of scope for P2 (the sink records
// the subject line and the link, not the render).
type Mail struct {
	Host    string
	Port    int
	Secure  bool
	From    string // e.g. `OlliTeX <no-reply@site.test>`
	Timeout int    // seconds; 0 = 15
}

// NewMail builds the transport from env (MAIL_HOST/PORT/SECURE/FROM),
// mirroring settings.js's email block.
func NewMail() *Mail {
	m := &Mail{
		Host:    envOr("MAIL_HOST", "smtpsink"),
		Secure:  envOr("MAIL_SECURE", "false") == "true",
		From:    envOr("MAIL_FROM", "OlliTeX <no-reply@localhost>"),
		Timeout: 15,
	}
	if p, err := strconv.Atoi(envOr("MAIL_PORT", "1025")); err == nil {
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

// Send delivers a text+html message (multipart/alternative, the Node
// nodemailer shape).
func (m *Mail) Send(to, subject, text, html string) error {
	t := m.Timeout
	if t <= 0 {
		t = 15
	}
	addr := net.JoinHostPort(m.Host, strconv.Itoa(m.Port))
	conn, err := net.DialTimeout("tcp", addr, time.Duration(t)*time.Second)
	if err != nil {
		return err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(time.Duration(t) * time.Second))
	if m.Secure {
		tc := tls.Client(conn, &tls.Config{ServerName: m.Host})
		if hErr := tc.Handshake(); hErr != nil {
			return hErr
		}
		conn = tc
	}
	c, err := smtp.NewClient(conn, m.Host)
	if err != nil {
		return err
	}
	defer c.Close()
	if err := c.Mail(m.From); err != nil {
		return err
	}
	if err := c.Rcpt(to); err != nil {
		return err
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	_, err = io.WriteString(w, composeMIME(to, m.From, subject, text, html))
	if err == nil {
		err = c.Quit()
	}
	return err
}

// composeMIME renders the nodemailer-equivalent message (Date/From/To/
// Subject headers, multipart/alternative body).
func composeMIME(to, from, subject, text, html string) string {
	boundary := "Olli" + fmt.Sprintf("%012d", time.Now().UnixNano())
	var b strings.Builder
	b.WriteString("Date: " + time.Now().UTC().Format("Mon, 02 Jan 2006 15:04:05") + " +0000 (UTC)\r\n")
	b.WriteString("From: " + from + "\r\n")
	b.WriteString("To: " + to + "\r\n")
	b.WriteString("Subject: " + subject + "\r\n")
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: multipart/alternative;\r\n")
	b.WriteString("\tboundary=\"" + boundary + "\"\r\n\r\n")
	b.WriteString("--" + boundary + "\r\n")
	b.WriteString("Content-Type: text/plain; charset=UTF-8\r\n\r\n")
	b.WriteString(text + "\r\n\r\n")
	b.WriteString("--" + boundary + "\r\n")
	b.WriteString("Content-Type: text/html; charset=UTF-8\r\n\r\n")
	b.WriteString(html + "\r\n\r\n")
	b.WriteString("--" + boundary + "--\r\n")
	return b.String()
}
