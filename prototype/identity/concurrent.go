package identity

import (
	"sync"
	"sync/atomic"
)

// Concurrent identity creation — SPEC §9.7, the row marked "race test mandatory".
//
// Two shards of the same run, or two runs in flight at once, can both meet the
// same unknown fingerprint within milliseconds. Naively each creates an
// identity, and the test ends up with two — which then diverge, each holding
// half the history. §9.7 says the fix is
//
//	UNIQUE(projectId, fingerprint) + upsert ON CONFLICT
//
// so exactly one identity is created and the loser reads back the winner's.
// That is a database-level guarantee; this models it so the semantics can be
// exercised without a database.

// SafeIdentityStore is a concurrency-safe get-or-create keyed by fingerprint.
// It stands in for the unique index plus ON CONFLICT that §9.7 relies on.
type SafeIdentityStore struct {
	mu       sync.Mutex
	byFP     map[string]string
	nextID   int64
	created  int64 // how many identities were actually minted
	conflict int64 // how many callers lost the race and read back the winner
}

func NewSafeIdentityStore() *SafeIdentityStore {
	return &SafeIdentityStore{byFP: map[string]string{}}
}

// GetOrCreate returns the identity for a fingerprint, creating it only if no
// other caller got there first. Second return value reports whether this call
// was the one that created it.
//
// The lock here plays the role the unique index plays in Postgres: it makes
// check-then-insert atomic. Doing the check and the insert as two separate
// statements — the obvious implementation — is exactly the bug §9.7 warns about.
func (s *SafeIdentityStore) GetOrCreate(fingerprint string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if id, ok := s.byFP[fingerprint]; ok {
		atomic.AddInt64(&s.conflict, 1)
		return id, false
	}
	s.nextID++
	id := idFor(s.nextID)
	s.byFP[fingerprint] = id
	atomic.AddInt64(&s.created, 1)
	return id, true
}

// Stats reports identities minted, races lost, and distinct fingerprints seen.
func (s *SafeIdentityStore) Stats() (created, conflicts, distinct int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.created, s.conflict, int64(len(s.byFP))
}

func idFor(n int64) string {
	const hexd = "0123456789abcdef"
	buf := make([]byte, 8)
	for i := 7; i >= 0; i-- {
		buf[i] = hexd[n&0xf]
		n >>= 4
	}
	return "st_" + string(buf)
}
