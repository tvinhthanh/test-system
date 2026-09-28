package mail

import (
	"bufio"
	"context"
	"errors"
	"net"
	"strings"
	"testing"
	"time"
)

// fakeSMTP speaks just enough SMTP to take one message and hand it back.
func fakeSMTP(t *testing.T) (port int, got <-chan string) {
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
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			cmd := strings.ToUpper(strings.TrimSpace(line))
			switch {
			case strings.HasPrefix(cmd, "EHLO"), strings.HasPrefix(cmd, "HELO"):
				say("250 fake")
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
	port, got := fakeSMTP(t)
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
	port, _ := fakeSMTP(t) // offers no STARTTLS
	s := &SMTP{Host: "127.0.0.1", Port: port, From: "no-reply@atp.example", TLS: "starttls", Timeout: 5 * time.Second}
	err := s.Send(context.Background(), Message{To: "a@b.example", Subject: "x", Text: "y"})
	if err == nil || !strings.Contains(err.Error(), "STARTTLS") {
		t.Errorf("sent without STARTTLS: %v", err)
	}
}
