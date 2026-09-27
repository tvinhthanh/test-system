package authz

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// TOTP per RFC 6238 with the parameters every authenticator app defaults
// to: HMAC-SHA-1, 6 digits, 30-second steps. A code is accepted one step
// either side of now (clock drift) and only for a step later than the last
// one accepted, so an observed code cannot be replayed.
const (
	totpDigits     = 6
	totpPeriod     = 30
	totpSkew       = 1
	totpSecretSize = 20 // 160 bits, RFC 4226 §4 recommendation
	totpIssuer     = "ATP"
	recoveryCodes  = 10
)

var b32 = base32.StdEncoding.WithPadding(base32.NoPadding)

func totpStep(t time.Time) int64 { return t.Unix() / totpPeriod }

// totpCode is HOTP(secret, step) truncated to six digits (RFC 4226 §5.3).
func totpCode(secret []byte, step int64) string {
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], uint64(step))
	m := hmac.New(sha1.New, secret)
	m.Write(msg[:])
	sum := m.Sum(nil)
	off := sum[len(sum)-1] & 0x0f
	bin := binary.BigEndian.Uint32(sum[off:off+4]) & 0x7fffffff
	return fmt.Sprintf("%0*d", totpDigits, bin%1_000_000)
}

// totpMatch returns the step code matches within the skew window and after
// lastStep, or 0. Every candidate is compared in constant time.
func totpMatch(secret []byte, code string, now time.Time, lastStep int64) int64 {
	code = strings.ReplaceAll(strings.TrimSpace(code), " ", "")
	if len(code) != totpDigits {
		return 0
	}
	var hit int64
	cur := totpStep(now)
	for s := cur - totpSkew; s <= cur+totpSkew; s++ {
		if subtle.ConstantTimeCompare([]byte(totpCode(secret, s)), []byte(code)) == 1 && s > lastStep {
			hit = s
		}
	}
	return hit
}

func newTOTPSecret() []byte {
	b := make([]byte, totpSecretSize)
	if _, err := rand.Read(b); err != nil {
		panic("authz: crypto/rand: " + err.Error())
	}
	return b
}

// otpauthURI is the Key Uri Format authenticator apps import.
func otpauthURI(email string, secret []byte) string {
	q := url.Values{}
	q.Set("secret", b32.EncodeToString(secret))
	q.Set("issuer", totpIssuer)
	q.Set("algorithm", "SHA1")
	q.Set("digits", fmt.Sprint(totpDigits))
	q.Set("period", fmt.Sprint(totpPeriod))
	return "otpauth://totp/" + url.PathEscape(totpIssuer+":"+email) + "?" + q.Encode()
}

// sealKey derives the AES-256 key for TOTP secrets from the pepper, with a
// label so it can never equal a key used elsewhere.
func sealKey(pepper []byte) []byte {
	m := hmac.New(sha256.New, pepper)
	m.Write([]byte("atp totp secret v1"))
	return m.Sum(nil)
}

// seal encrypts a TOTP secret; the user id is the associated data, so a
// sealed secret copied onto another user's row does not open.
func seal(pepper []byte, userID string, secret []byte) []byte {
	gcm := mustGCM(pepper)
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		panic("authz: crypto/rand: " + err.Error())
	}
	return gcm.Seal(nonce, nonce, secret, []byte(userID))
}

var errSealed = errors.New("authz: sealed secret does not open")

func unseal(pepper []byte, userID string, sealed []byte) ([]byte, error) {
	gcm := mustGCM(pepper)
	if len(sealed) < gcm.NonceSize() {
		return nil, errSealed
	}
	out, err := gcm.Open(nil, sealed[:gcm.NonceSize()], sealed[gcm.NonceSize():], []byte(userID))
	if err != nil {
		return nil, errSealed
	}
	return out, nil
}

func mustGCM(pepper []byte) cipher.AEAD {
	block, err := aes.NewCipher(sealKey(pepper))
	if err != nil {
		panic("authz: aes: " + err.Error())
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		panic("authz: gcm: " + err.Error())
	}
	return gcm
}

// newRecoveryCode is 10 characters of base32 (50 bits) shown as xxxxx-xxxxx;
// the dash and case are ignored when it is typed back.
func newRecoveryCode() string {
	b := make([]byte, 7)
	if _, err := rand.Read(b); err != nil {
		panic("authz: crypto/rand: " + err.Error())
	}
	s := strings.ToLower(b32.EncodeToString(b))[:10]
	return s[:5] + "-" + s[5:]
}

func normalizeRecoveryCode(code string) string {
	return strings.ToLower(strings.NewReplacer("-", "", " ", "").Replace(strings.TrimSpace(code)))
}
