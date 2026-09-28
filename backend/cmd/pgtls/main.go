// Command pgtls makes the TLS material for the compose Postgres (§11.5 "TLS
// mọi nơi", deploy/compose.yml): a private CA and a server certificate
// signed by it, in one directory (a volume) that Postgres reads its key
// from and the api, worker, migrate and backup read the CA from — they
// connect with sslmode=verify-full, so a certificate for another name, or
// from anyone else, is refused.
//
// The distroless image has no openssl; this is the stdlib's crypto/x509. It
// runs once per `up` and is idempotent: material that exists, parses, names
// every host asked for and has more than 30 days left is kept (the CA the
// clients trust must not change under them). Otherwise the server
// certificate is re-issued from the same CA — Postgres reads it at start,
// so restart postgres after a renewal (deploy/README.md).
//
//	pgtls -dir /tls -hosts postgres,localhost,127.0.0.1 -group 70
//
// -group is the gid Postgres runs as (70 in the alpine image). server.key
// is then root's, group-readable, 0640 — the one other layout Postgres
// accepts besides "its own, 0600" — so that this command, run again as
// root without CAP_DAC_OVERRIDE, can still read the key to see the pair is
// current (with the key Postgres' 0600 it could not, and re-issued on every
// run). Setting the group needs CAP_CHOWN, the only privilege the compose
// service keeps.
package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	caValidity     = 20 * 365 * 24 * time.Hour
	serverValidity = 5 * 365 * 24 * time.Hour
	renewBefore    = 30 * 24 * time.Hour
)

func main() {
	dir := flag.String("dir", "/tls", "directory for ca.crt, ca.key, server.crt, server.key")
	hosts := flag.String("hosts", "postgres,localhost,127.0.0.1", "names and addresses the server certificate is for")
	group := flag.Int("group", -1, "gid that may read server.key (Postgres' group); -1 leaves the key 0600 to the current user")
	flag.Parse()
	if err := run(*dir, splitHosts(*hosts), *group, time.Now()); err != nil {
		fmt.Fprintln(os.Stderr, "pgtls:", err)
		os.Exit(1)
	}
}

func splitHosts(s string) []string {
	var out []string
	for _, h := range strings.Split(s, ",") {
		if h = strings.TrimSpace(h); h != "" {
			out = append(out, h)
		}
	}
	return out
}

func run(dir string, hosts []string, group int, now time.Time) error {
	if len(hosts) == 0 {
		return errors.New("-hosts: want at least one name")
	}
	keyMode := os.FileMode(0o600)
	if group >= 0 {
		keyMode = 0o640
	}
	caCert, caKey, err := loadOrMakeCA(dir, now)
	if err != nil {
		return err
	}
	if serverCurrent(dir, caCert, hosts, now) {
		fmt.Println("pgtls: server certificate current, nothing to do")
		return nil
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return err
	}
	tmpl := &x509.Certificate{
		SerialNumber: serial(),
		Subject:      pkix.Name{CommonName: hosts[0]},
		NotBefore:    now.Add(-time.Hour),
		NotAfter:     now.Add(serverValidity),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	for _, h := range hosts {
		if ip := net.ParseIP(h); ip != nil {
			tmpl.IPAddresses = append(tmpl.IPAddresses, ip)
		} else {
			tmpl.DNSNames = append(tmpl.DNSNames, h)
		}
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, caCert, &key.PublicKey, caKey)
	if err != nil {
		return err
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return err
	}
	// Key first, certificate last: a crash in between leaves a pair that
	// does not match, which the next run sees as "not current" and redoes.
	if err := write(filepath.Join(dir, "server.key"), "PRIVATE KEY", keyDER, keyMode, group); err != nil {
		return err
	}
	if err := write(filepath.Join(dir, "server.crt"), "CERTIFICATE", der, 0o644, group); err != nil {
		return err
	}
	fmt.Printf("pgtls: issued server certificate for %s, valid until %s\n", strings.Join(hosts, ", "), tmpl.NotAfter.Format("2006-01-02"))
	return nil
}

func loadOrMakeCA(dir string, now time.Time) (*x509.Certificate, *ecdsa.PrivateKey, error) {
	certPath, keyPath := filepath.Join(dir, "ca.crt"), filepath.Join(dir, "ca.key")
	cert, cerr := readCert(certPath)
	key, kerr := readKey(keyPath)
	if cerr == nil && kerr == nil {
		return cert, key, nil
	}
	if !errors.Is(cerr, os.ErrNotExist) || !errors.Is(kerr, os.ErrNotExist) {
		// Half a CA, or one that does not parse: replacing it would silently
		// break every client that trusts the old one. An operator decides.
		return nil, nil, fmt.Errorf("CA in %s is incomplete or unreadable (ca.crt: %v; ca.key: %v) — remove both to start over", dir, cerr, kerr)
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	tmpl := &x509.Certificate{
		SerialNumber:          serial(),
		Subject:               pkix.Name{CommonName: "ATP compose Postgres CA"},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(caValidity),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLenZero:        true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, nil, err
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, nil, err
	}
	// The CA key stays with whoever runs this (root in compose), unreadable
	// by Postgres and by the apps.
	if err := write(keyPath, "PRIVATE KEY", keyDER, 0o600, -1); err != nil {
		return nil, nil, err
	}
	if err := write(certPath, "CERTIFICATE", der, 0o644, -1); err != nil {
		return nil, nil, err
	}
	cert, err = x509.ParseCertificate(der)
	fmt.Println("pgtls: created a new CA")
	return cert, key, err
}

// serverCurrent: the pair exists, matches, chains to the CA, names every
// host and has more than renewBefore left.
func serverCurrent(dir string, ca *x509.Certificate, hosts []string, now time.Time) bool {
	cert, err := readCert(filepath.Join(dir, "server.crt"))
	if err != nil {
		return false
	}
	key, err := readKey(filepath.Join(dir, "server.key"))
	if err != nil || !key.PublicKey.Equal(cert.PublicKey) {
		return false
	}
	if cert.CheckSignatureFrom(ca) != nil || now.Add(renewBefore).After(cert.NotAfter) {
		return false
	}
	for _, h := range hosts {
		if cert.VerifyHostname(h) != nil {
			return false
		}
	}
	return true
}

func readCert(path string) (*x509.Certificate, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	blk, _ := pem.Decode(b)
	if blk == nil || blk.Type != "CERTIFICATE" {
		return nil, fmt.Errorf("%s: not a PEM certificate", path)
	}
	return x509.ParseCertificate(blk.Bytes)
}

func readKey(path string) (*ecdsa.PrivateKey, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	blk, _ := pem.Decode(b)
	if blk == nil || blk.Type != "PRIVATE KEY" {
		return nil, fmt.Errorf("%s: not a PEM private key", path)
	}
	k, err := x509.ParsePKCS8PrivateKey(blk.Bytes)
	if err != nil {
		return nil, err
	}
	ek, ok := k.(*ecdsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("%s: not an ECDSA key", path)
	}
	return ek, nil
}

// write replaces path atomically (temp file + rename in the same directory)
// with the mode and group set before the rename, so the key is never
// readable by anyone else, not even for a moment.
func write(path, typ string, der []byte, mode os.FileMode, gid int) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".pgtls-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp) // no-op after the rename
	if err := f.Chmod(mode); err != nil {
		f.Close()
		return err
	}
	if gid >= 0 {
		if err := f.Chown(-1, gid); err != nil {
			f.Close()
			return fmt.Errorf("chgrp %s to %d: %w", path, gid, err)
		}
	}
	if err := pem.Encode(f, &pem.Block{Type: typ, Bytes: der}); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func serial() *big.Int {
	n, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 127))
	if err != nil {
		panic(err)
	}
	return n
}
