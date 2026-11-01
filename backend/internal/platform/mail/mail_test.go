package mail

import (
	"bufio"
	"context"
	"encoding/base64"
	"errors"
	"net"
	"net/smtp"
	"strings"
	"testing"
	"time"
)

// fakeSMTP speaks just enough SMTP to take one message and hand it back.
// auth is the AUTH list its EHLO offers ("" = none); a login shows up in the
// envelope as "AUTH <mechanism> <username>:<password>".
func fakeSMTP(t *testing.T, auth string) (port int, got <-chan string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	out := make(chan string, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		r, w := bufio.NewReader(conn), conn
		say := func(s string) { _, _ = w.Write([]byte(s + "\r\n")) }
		say("220 fake ESMTP")
		var data strings.Builder
		var env []string
		ask := func(challenge string) string {
			say("334 " + base64.StdEncoding.EncodeToString([]byte(challenge)))
			l, _ := r.ReadString('\n')
			b, _ := base64.StdEncoding.DecodeString(strings.TrimSpace(l))
			return string(b)
		}
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			cmd := strings.ToUpper(strings.TrimSpace(line))
			switch {
			case strings.HasPrefix(cmd, "EHLO"), strings.HasPrefix(cmd, "HELO"):
				if auth == "" {
					say("250 fake")
				} else {
					say("250-fake")
					say("250 AUTH " + auth)
				}
			case strings.HasPrefix(cmd, "AUTH PLAIN "):
				b, _ := base64.StdEncoding.DecodeString(strings.Fields(line)[2])
				parts := strings.Split(string(b), "\x00") // identity, username, password
				env = append(env, "AUTH PLAIN "+parts[1]+":"+parts[2])
				say("235 ok")
			case cmd == "AUTH LOGIN":
				user := ask("Username:")
				env = append(env, "AUTH LOGIN "+user+":"+ask("Password:"))
				say("235 ok")
			case strings.HasPrefix(cmd, "MAIL FROM"), strings.HasPrefix(cmd, "RCPT TO"):
				env = append(env, strings.TrimSpace(line))
				say("250 ok")
			case cmd == "DATA":
				say("354 go on")
				for {
					l, err := r.ReadString('\n')
					if err != nil || l == ".\r\n" {
						break
					}
					data.WriteString(l)
				}
				say("250 queued")
				out <- strings.Join(env, "\n") + "\n\n" + data.String()
			case cmd == "QUIT":
				say("221 bye")
				return
			default:
				say("502 no")
			}
		}
	}()
	return ln.Addr().(*net.TCPAddr).Port, out
}

func TestSendsOnePlainTextMessage(t *testing.T) {
	port, got := fakeSMTP(t, "")
	s := &SMTP{Host: "127.0.0.1", Port: port, From: "ATP <no-reply@atp.example>", TLS: "none", Timeout: 5 * time.Second}
	err := s.Send(context.Background(), Message{To: "alice@example.com", Subject: "Bạn được mời vào ATP", Text: "Hello,\nopen https://atp.example/invite/abc\n"})
	if err != nil {
		t.Fatal(err)
	}
	msg := <-got
	for _, want := range []string{
		"MAIL FROM:<no-reply@atp.example>", "RCPT TO:<alice@example.com>",
		"From: \"ATP\" <no-reply@atp.example>", "To: alice@example.com\r\n",
		"Subject: =?utf-8?q?", "Content-Type: text/plain; charset=utf-8",
		"open https://atp.example/invite/abc",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("message lacks %q:\n%s", want, msg)
		}
	}
}

// Nothing a caller passes can add a header or a recipient.
func TestRefusesHeaderInjection(t *testing.T) {
	s := &SMTP{Host: "127.0.0.1", Port: 1, From: "no-reply@atp.example", TLS: "none"}
	for _, m := range []Message{
		{To: "alice@example.com\r\nBcc: eve@evil.example", Subject: "x"},
		{To: "alice@example.com", Subject: "hi\r\nBcc: eve@evil.example"},
		{To: "not an address", Subject: "x"},
	} {
		if err := s.Send(context.Background(), m); !errors.Is(err, ErrInvalid) {
			t.Errorf("%+v: %v", m, err)
		}
	}
}

func TestStartTLSIsRequiredWhenConfigured(t *testing.T) {
	port, _ := fakeSMTP(t, "") // offers no STARTTLS
	s := &SMTP{Host: "127.0.0.1", Port: port, From: "no-reply@atp.example", TLS: "starttls", Timeout: 5 * time.Second}
	err := s.Send(context.Background(), Message{To: "a@b.example", Subject: "x", Text: "y"})
	if err == nil || !strings.Contains(err.Error(), "STARTTLS") {
		t.Errorf("sent without STARTTLS: %v", err)
	}
}

// Microsoft 365 and Exchange offer LOGIN and OAuth, not PLAIN (probed
// 30-10-2026); a server that lists PLAIN, or nothing, keeps getting PLAIN.
func TestAuthUsesAMechanismTheServerOffers(t *testing.T) {
	for _, tc := range []struct{ offered, want string }{
		{"LOGIN XOAUTH2", "AUTH LOGIN mailer@atp.example:app password"},
		{"PLAIN LOGIN", "AUTH PLAIN mailer@atp.example:app password"},
		{"", "AUTH PLAIN mailer@atp.example:app password"},
	} {
		port, got := fakeSMTP(t, tc.offered)
		s := &SMTP{Host: "127.0.0.1", Port: port, Username: "mailer@atp.example", Password: "app password", From: "no-reply@atp.example", TLS: "none", Timeout: 5 * time.Second}
		if err := s.Send(context.Background(), Message{To: "a@b.example", Subject: "x", Text: "y"}); err != nil {
			t.Fatalf("AUTH %q: %v", tc.offered, err)
		}
		if msg := <-got; !strings.Contains(msg, tc.want) {
			t.Errorf("AUTH %q: want %q in\n%s", tc.offered, tc.want, msg)
		}
	}
}

func TestAuthRefusesAMechanismItCannotSpeak(t *testing.T) {
	port, _ := fakeSMTP(t, "XOAUTH2")
	s := &SMTP{Host: "127.0.0.1", Port: port, Username: "mailer@atp.example", Password: "secret", From: "no-reply@atp.example", TLS: "none", Timeout: 5 * time.Second}
	err := s.Send(context.Background(), Message{To: "a@b.example", Subject: "x", Text: "y"})
	if err == nil || !strings.Contains(err.Error(), "XOAUTH2") {
		t.Errorf("want an error naming what the server offers: %v", err)
	}
}

// LOGIN keeps PlainAuth's rule: no password over a plain connection to
// another machine, none to a host other than the configured one.
func TestLoginAuthNeedsTLSAwayFromLocalhost(t *testing.T) {
	a := &loginAuth{username: "u", password: "p", host: "smtp.example.com"}
	if _, _, err := a.Start(&smtp.ServerInfo{Name: "smtp.example.com", Auth: []string{"LOGIN"}}); err == nil {
		t.Error("LOGIN over an unencrypted connection")
	}
	if _, _, err := a.Start(&smtp.ServerInfo{Name: "evil.example", TLS: true, Auth: []string{"LOGIN"}}); err == nil {
		t.Error("LOGIN to another host")
	}
	if mech, _, err := a.Start(&smtp.ServerInfo{Name: "smtp.example.com", TLS: true, Auth: []string{"LOGIN"}}); err != nil || mech != "LOGIN" {
		t.Errorf("LOGIN over TLS: %q %v", mech, err)
	}
}
