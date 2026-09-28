// Package mail sends the api's few emails (invitations, §11.2) over SMTP —
// any provider speaks it (SES, Resend, Mailgun, Postmark, a company relay).
// Plain text only; every header value is checked for CR/LF so no input can
// add a header or a recipient; the connection has a deadline; STARTTLS is
// required unless the configuration says otherwise.
//
// This is platform code: it knows nothing about the business (§6.9.3).
package mail

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"mime"
	"mime/quotedprintable"
	"net"
	netmail "net/mail"
	"net/smtp"
	"strconv"
	"strings"
	"time"
)

// Message is one plain-text email to one recipient.
type Message struct {
	To      string
	Subject string
	Text    string
}

// Sender delivers a Message.
type Sender interface {
	Send(ctx context.Context, m Message) error
}

// ErrInvalid: a header value that could not be sent safely.
var ErrInvalid = errors.New("mail: invalid message")

// SMTP is a Sender for one SMTP server.
type SMTP struct {
	Host     string
	Port     int
	Username string
	Password string
	From     string // "Name <address>" or an address
	TLS      string // starttls | tls | none
	Timeout  time.Duration
}

func clean(s string) bool { return !strings.ContainsAny(s, "\r\n") }

// build renders the message; exported shape tested in mail_test.go.
func (s *SMTP) build(m Message, now time.Time) (envelopeFrom, to string, data []byte, err error) {
	from, err := netmail.ParseAddress(s.From)
	if err != nil {
		return "", "", nil, fmt.Errorf("%w: from: %v", ErrInvalid, err)
	}
	rcpt, err := netmail.ParseAddress(m.To)
	if err != nil || !clean(m.To) || !clean(m.Subject) {
		return "", "", nil, fmt.Errorf("%w: recipient or subject", ErrInvalid)
	}
	var id [12]byte
	_, _ = rand.Read(id[:])
	domain := from.Address[strings.LastIndexByte(from.Address, '@')+1:]
	var b bytes.Buffer
	fmt.Fprintf(&b, "From: %s\r\n", from.String())
	fmt.Fprintf(&b, "To: %s\r\n", rcpt.Address)
	fmt.Fprintf(&b, "Subject: %s\r\n", mime.QEncoding.Encode("utf-8", m.Subject))
	fmt.Fprintf(&b, "Date: %s\r\n", now.UTC().Format(time.RFC1123Z))
	fmt.Fprintf(&b, "Message-ID: <%s@%s>\r\n", hex.EncodeToString(id[:]), domain)
	b.WriteString("MIME-Version: 1.0\r\nContent-Type: text/plain; charset=utf-8\r\nContent-Transfer-Encoding: quoted-printable\r\n\r\n")
	qp := quotedprintable.NewWriter(&b)
	_, _ = qp.Write([]byte(strings.ReplaceAll(m.Text, "\n", "\r\n")))
	_ = qp.Close()
	return from.Address, rcpt.Address, b.Bytes(), nil
}

// Send delivers m, or returns why not.
func (s *SMTP) Send(ctx context.Context, m Message) error {
	envFrom, to, data, err := s.build(m, time.Now())
	if err != nil {
		return err
	}
	timeout := s.Timeout
	if timeout == 0 {
		timeout = 20 * time.Second
	}
	deadline := time.Now().Add(timeout)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	addr := net.JoinHostPort(s.Host, strconv.Itoa(s.Port))
	dialer := &net.Dialer{Deadline: deadline}
	var conn net.Conn
	if s.TLS == "tls" {
		conn, err = tls.DialWithDialer(dialer, "tcp", addr, &tls.Config{ServerName: s.Host, MinVersion: tls.VersionTLS12})
	} else {
		conn, err = dialer.DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return fmt.Errorf("mail: connect %s: %w", addr, err)
	}
	_ = conn.SetDeadline(deadline)
	c, err := smtp.NewClient(conn, s.Host)
	if err != nil {
		conn.Close()
		return fmt.Errorf("mail: greeting: %w", err)
	}
	defer c.Close()
	if s.TLS == "starttls" {
		if ok, _ := c.Extension("STARTTLS"); !ok {
			return fmt.Errorf("mail: %s does not offer STARTTLS (SMTP_TLS=starttls)", addr)
		}
		if err := c.StartTLS(&tls.Config{ServerName: s.Host, MinVersion: tls.VersionTLS12}); err != nil {
			return fmt.Errorf("mail: starttls: %w", err)
		}
	}
	if s.Username != "" {
		if err := c.Auth(smtp.PlainAuth("", s.Username, s.Password, s.Host)); err != nil {
			return fmt.Errorf("mail: auth: %w", err)
		}
	}
	if err := c.Mail(envFrom); err != nil {
		return fmt.Errorf("mail: MAIL FROM: %w", err)
	}
	if err := c.Rcpt(to); err != nil {
		return fmt.Errorf("mail: RCPT TO: %w", err)
	}
	w, err := c.Data()
	if err != nil {
		return fmt.Errorf("mail: DATA: %w", err)
	}
	if _, err := w.Write(data); err != nil {
		return fmt.Errorf("mail: write: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("mail: end of data: %w", err)
	}
	return c.Quit()
}
