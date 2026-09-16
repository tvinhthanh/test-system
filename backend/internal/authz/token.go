package authz

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// Token wire format (§11.4):
//
//	atp_ing_ <64 hex chars = 256 random bits>
//
// The first 8 hex chars of the random part are the lookup prefix stored in
// clear; the whole random part is the secret, stored only as
// HMAC-SHA-256(pepper, secret). HMAC, not Argon2: this runs on every ingest
// request and the secret already has 256 bits of entropy, so slowing the
// hash buys nothing (§11.4).

const (
	secretHexLen = 64
	prefixLen    = 8
)

// newSecret returns 256 random bits as lower-case hex.
func newSecret() string {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand failing means the platform is broken; there is no
		// safe fallback for a credential.
		panic("authz: crypto/rand: " + err.Error())
	}
	return hex.EncodeToString(b[:])
}

// splitBearer takes the presented token apart. ok is false for anything
// that is not exactly prefix + 64 lower-case hex chars, so a malformed
// string never reaches the database.
func splitBearer(bearer string) (prefix, secret string, ok bool) {
	if !strings.HasPrefix(bearer, TokenPrefix) {
		return "", "", false
	}
	secret = bearer[len(TokenPrefix):]
	if len(secret) != secretHexLen {
		return "", "", false
	}
	for i := 0; i < len(secret); i++ {
		c := secret[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return "", "", false
		}
	}
	return secret[:prefixLen], secret, true
}

// hashSecret is the stored form.
func hashSecret(pepper []byte, secret string) []byte {
	m := hmac.New(sha256.New, pepper)
	m.Write([]byte(secret))
	return m.Sum(nil)
}

// hashEqual compares in constant time.
func hashEqual(a, b []byte) bool { return hmac.Equal(a, b) }
