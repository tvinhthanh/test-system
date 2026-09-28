package main

import (
	"crypto/tls"
	"crypto/x509"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// What a client with sslmode=verify-full checks: the chain to ca.crt
// (trusted alone) and the name; LoadX509KeyPair also proves the key
// Postgres will serve with belongs to the certificate.
func handshake(t *testing.T, dir, serverName string, at ...time.Time) error {
	t.Helper()
	pair, err := tls.LoadX509KeyPair(filepath.Join(dir, "server.crt"), filepath.Join(dir, "server.key"))
	if err != nil {
		t.Fatal(err)
	}
	caPEM, err := os.ReadFile(filepath.Join(dir, "ca.crt"))
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(caPEM) {
		t.Fatal("ca.crt does not parse")
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	_, err = leaf.Verify(x509.VerifyOptions{Roots: pool, DNSName: serverName, CurrentTime: append(at, time.Now())[0], KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}})
	return err
}

func TestIssuesAPairThatVerifiesByName(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	if err := run(dir, []string{"postgres", "localhost", "127.0.0.1"}, -1, now); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"postgres", "localhost", "127.0.0.1"} {
		if err := handshake(t, dir, name); err != nil {
			t.Errorf("verify-full as %q: %v", name, err)
		}
	}
	if err := handshake(t, dir, "evil.example.com"); err == nil {
		t.Error("a name not asked for verified")
	}
}

func TestSecondRunKeepsEverything(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	if err := run(dir, []string{"postgres"}, -1, now); err != nil {
		t.Fatal(err)
	}
	before := map[string]string{}
	for _, f := range []string{"ca.crt", "ca.key", "server.crt", "server.key"} {
		b, _ := os.ReadFile(filepath.Join(dir, f))
		before[f] = string(b)
	}
	if err := run(dir, []string{"postgres"}, -1, now.Add(24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	for f, was := range before {
		if b, _ := os.ReadFile(filepath.Join(dir, f)); string(b) != was {
			t.Errorf("%s changed on a second run", f)
		}
	}
}

// Near expiry, or a new host: a new server certificate from the same CA —
// clients that trust ca.crt keep working.
func TestRenewsTheServerCertificateFromTheSameCA(t *testing.T) {
	for name, second := range map[string]struct {
		hosts []string
		at    time.Duration
	}{
		"close to expiry": {[]string{"postgres"}, serverValidity - 10*24*time.Hour},
		"a new host":      {[]string{"postgres", "db.internal"}, time.Hour},
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			now := time.Now()
			if err := run(dir, []string{"postgres"}, -1, now); err != nil {
				t.Fatal(err)
			}
			ca, _ := os.ReadFile(filepath.Join(dir, "ca.crt"))
			crt, _ := os.ReadFile(filepath.Join(dir, "server.crt"))
			if err := run(dir, second.hosts, -1, now.Add(second.at)); err != nil {
				t.Fatal(err)
			}
			if b, _ := os.ReadFile(filepath.Join(dir, "ca.crt")); string(b) != string(ca) {
				t.Error("the CA changed — every client would stop trusting the server")
			}
			if b, _ := os.ReadFile(filepath.Join(dir, "server.crt")); string(b) == string(crt) {
				t.Error("server certificate not renewed")
			}
			for _, h := range second.hosts {
				if err := handshake(t, dir, h, now.Add(second.at)); err != nil {
					t.Errorf("after renewal, %q: %v", h, err)
				}
			}
		})
	}
}

// A mismatched pair (a crash between the two writes) is redone.
func TestMismatchedPairIsReissued(t *testing.T) {
	dir := t.TempDir()
	if err := run(dir, []string{"postgres"}, -1, time.Now()); err != nil {
		t.Fatal(err)
	}
	other := t.TempDir()
	if err := run(other, []string{"postgres"}, -1, time.Now()); err != nil {
		t.Fatal(err)
	}
	key, _ := os.ReadFile(filepath.Join(other, "server.key"))
	if err := os.WriteFile(filepath.Join(dir, "server.key"), key, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := run(dir, []string{"postgres"}, -1, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := handshake(t, dir, "postgres"); err != nil {
		t.Errorf("after reissue: %v", err)
	}
}

// Half a CA is an operator's problem, never silently replaced.
func TestRefusesHalfACA(t *testing.T) {
	dir := t.TempDir()
	if err := run(dir, []string{"postgres"}, -1, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(dir, "ca.key")); err != nil {
		t.Fatal(err)
	}
	err := run(dir, []string{"postgres"}, -1, time.Now())
	if err == nil || !strings.Contains(err.Error(), "remove both") {
		t.Fatalf("err = %v, want a refusal", err)
	}
}
