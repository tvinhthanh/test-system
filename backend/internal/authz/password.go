package authz

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Passwords are argon2id (§11.2), stored as a PHC string so the parameters
// travel with the hash and can be raised later without a migration:
//
//	$argon2id$v=19$m=19456,t=2,p=1$<salt>$<hash>
//
// m=19 MiB, t=2, p=1 is the OWASP minimum recommendation; ~50 ms on a
// laptop core, which is the point — this runs on login only, never on the
// ingest hot path (§11.4: API tokens use HMAC for that reason).
const (
	argonMemory  = 19 * 1024
	argonTime    = 2
	argonThreads = 1
	argonKeyLen  = 32
	argonSaltLen = 16

	// MinPasswordLen is [ASSUMPTION]: long enough to defeat online guessing
	// with the §11.2 rate limit in front; no composition rules, which only
	// produce "Password1!".
	MinPasswordLen = 12
	maxPasswordLen = 256
)

func hashPassword(password string) (string, error) {
	if l := len(password); l < MinPasswordLen || l > maxPasswordLen {
		return "", fmt.Errorf("%w: password must be %d–%d characters", ErrInvalid, MinPasswordLen, maxPasswordLen)
	}
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	enc := base64.RawStdEncoding
	return fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s", argonMemory, argonTime, argonThreads, enc.EncodeToString(salt), enc.EncodeToString(key)), nil
}

// verifyPassword is constant-time on the hash comparison. A malformed
// stored hash is a false, not an error: the caller answers "wrong
// credentials" either way (§11.2: no enumeration).
func verifyPassword(encoded, password string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" || parts[2] != "v=19" {
		return false
	}
	var m, t uint32
	var p uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &m, &t, &p); err != nil {
		return false
	}
	enc := base64.RawStdEncoding
	salt, err := enc.DecodeString(parts[4])
	if err != nil {
		return false
	}
	want, err := enc.DecodeString(parts[5])
	if err != nil || len(want) == 0 || m == 0 || t == 0 || p == 0 {
		return false
	}
	got := argon2.IDKey([]byte(password), salt, t, m, p, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1
}

// dummyHash is verified against when the email is unknown, so a request
// for a missing account costs the same time as one for a real account with
// a wrong password (§11.2: no user enumeration, by timing either).
var dummyHash = func() string {
	h, err := hashPassword("correct-horse-battery-staple")
	if err != nil {
		panic(err)
	}
	return h
}()
