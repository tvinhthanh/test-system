package authz

import (
	"time"

	"github.com/vinh/automation-testing-platform/backend/internal/platform/pg"
)

// NewForTest is New with an injectable clock.
func NewForTest(db *pg.DB, pepper []byte, now func() time.Time) Service {
	s := New(db, pepper).(*service)
	s.now = now
	return s
}

func HashPasswordForTest(p string) (string, error) { return hashPassword(p) }
func VerifyPasswordForTest(h, p string) bool       { return verifyPassword(h, p) }

// TOTPCodeForTest is the code for a base32 secret at t (RFC 6238 vectors,
// and the tests' authenticator app).
func TOTPCodeForTest(secretB32 string, t time.Time) string {
	secret, err := b32.DecodeString(secretB32)
	if err != nil {
		panic(err)
	}
	return totpCode(secret, totpStep(t))
}

func B32ForTest(raw string) string { return b32.EncodeToString([]byte(raw)) }
