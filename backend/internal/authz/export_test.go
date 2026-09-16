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
