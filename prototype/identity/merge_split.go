package identity

import (
	"fmt"
	"sort"
	"time"
)

// Manual merge and split — SPEC §9.4, with the alias validity intervals added
// by finding N14 (§8.5).
//
// Why this matters as much as the matching algorithm: §9.6 accepts precision
// >= 0.98, so roughly one auto-match in fifty is wrong on real data. A wrong
// merge silently fuses the history of two tests. Without a working split, that
// corruption is permanent — and it is invisible, because the merged history
// still looks like a plausible test.
//
// Split is therefore not a convenience feature. It is what makes an imperfect
// matcher survivable.

// AliasSource records how a fingerprint came to point at an identity.
type AliasSource string

const (
	SourceExact       AliasSource = "exact"
	SourceAutoMatch   AliasSource = "auto_match"
	SourceManualMerge AliasSource = "manual_merge"
	SourceManualSplit AliasSource = "manual_split"
)

// Alias mirrors the TestIdentityAlias table in §8.5.
//
// ValidTo == nil means "currently in force". Realtime resolution only ever
// looks at those rows; historical backfill selects by event time.
type Alias struct {
	ProjectID   string
	Fingerprint string
	IdentityID  string
	Source      AliasSource
	MatchScore  float64
	CreatedBy   string
	ValidFrom   time.Time
	ValidTo     *time.Time
}

func (a Alias) activeAt(t time.Time) bool {
	if t.Before(a.ValidFrom) {
		return false
	}
	if a.ValidTo == nil {
		return true
	}
	return t.Before(*a.ValidTo)
}

// IdentityStore is an in-memory stand-in for the TestIdentity and
// TestIdentityAlias tables, enough to exercise merge and split semantics.
type IdentityStore struct {
	ProjectID string
	Aliases   []Alias
	// MergedInto records identities retired by a merge: source -> target.
	MergedInto map[string]string
	// Audit keeps every mutation so the Review Queue can show what happened.
	Audit []string
}

func NewIdentityStore(projectID string) *IdentityStore {
	return &IdentityStore{ProjectID: projectID, MergedInto: map[string]string{}}
}

// Bind records a fingerprint pointing at an identity from now on.
func (s *IdentityStore) Bind(fp, identityID string, src AliasSource, from time.Time) {
	s.Aliases = append(s.Aliases, Alias{
		ProjectID:   s.ProjectID,
		Fingerprint: fp,
		IdentityID:  identityID,
		Source:      src,
		ValidFrom:   from,
	})
}

// ResolveNow is the hot path: only aliases currently in force.
// This is the query behind §8.5's partial unique index
// UNIQUE(projectId, fingerprint) WHERE validTo IS NULL.
func (s *IdentityStore) ResolveNow(fp string) (string, bool) {
	for i := range s.Aliases {
		a := &s.Aliases[i]
		if a.Fingerprint == fp && a.ValidTo == nil {
			return s.follow(a.IdentityID), true
		}
	}
	return "", false
}

// ResolveAt is the backfill path: which identity owned this fingerprint at the
// time the event actually happened.
func (s *IdentityStore) ResolveAt(fp string, at time.Time) (string, bool) {
	var best *Alias
	for i := range s.Aliases {
		a := &s.Aliases[i]
		if a.Fingerprint != fp || !a.activeAt(at) {
			continue
		}
		if best == nil || a.ValidFrom.After(best.ValidFrom) {
			best = a
		}
	}
	if best == nil {
		return "", false
	}
	return s.follow(best.IdentityID), true
}

// follow walks merge chains so a retired identity resolves to its survivor.
func (s *IdentityStore) follow(id string) string {
	seen := map[string]bool{}
	for {
		next, ok := s.MergedInto[id]
		if !ok || seen[id] {
			return id
		}
		seen[id] = true
		id = next
	}
}

// Merge folds src into dst: every alias of src now points at dst, and src is
// retired. §9.4 requires an audit record.
func (s *IdentityStore) Merge(dst, src, actor string, at time.Time) error {
	if dst == src {
		return fmt.Errorf("cannot merge %s into itself", src)
	}
	if _, retired := s.MergedInto[dst]; retired {
		return fmt.Errorf("merge target %s is itself retired", dst)
	}
	moved := 0
	for i := range s.Aliases {
		if s.Aliases[i].IdentityID == src {
			s.Aliases[i].IdentityID = dst
			s.Aliases[i].Source = SourceManualMerge
			s.Aliases[i].CreatedBy = actor
			moved++
		}
	}
	s.MergedInto[src] = dst
	s.Audit = append(s.Audit, fmt.Sprintf("%s merged %s into %s (%d aliases) at %s",
		actor, src, dst, moved, at.Format(time.RFC3339)))
	return nil
}

// SplitFrom is the undo for a wrong auto-match, and the reason N14 added
// validity intervals.
//
// Everything before T keeps belonging to the original identity; everything from
// T onwards belongs to newID. Implemented by closing the current alias at T and
// opening a fresh one — not by rewriting the old row, which would erase the
// history the split is meant to preserve.
func (s *IdentityStore) SplitFrom(fp, newID, actor string, at time.Time) error {
	var cur *Alias
	for i := range s.Aliases {
		a := &s.Aliases[i]
		if a.Fingerprint == fp && a.ValidTo == nil {
			cur = a
			break
		}
	}
	if cur == nil {
		return fmt.Errorf("no active alias for fingerprint %s", fp)
	}
	if !at.After(cur.ValidFrom) {
		return fmt.Errorf("split point %s is not after the alias start %s",
			at.Format(time.RFC3339), cur.ValidFrom.Format(time.RFC3339))
	}
	if cur.IdentityID == newID {
		return fmt.Errorf("split target %s is already the current owner", newID)
	}

	cutoff := at
	cur.ValidTo = &cutoff
	old := cur.IdentityID

	s.Aliases = append(s.Aliases, Alias{
		ProjectID:   s.ProjectID,
		Fingerprint: fp,
		IdentityID:  newID,
		Source:      SourceManualSplit,
		CreatedBy:   actor,
		ValidFrom:   at,
	})
	s.Audit = append(s.Audit, fmt.Sprintf("%s split %s off %s into %s from %s",
		actor, fp, old, newID, at.Format(time.RFC3339)))
	return nil
}

// ActiveAliasCount checks the invariant behind §8.5's partial unique index:
// a fingerprint may have at most one alias in force at a time.
func (s *IdentityStore) ActiveAliasCount(fp string) int {
	n := 0
	for _, a := range s.Aliases {
		if a.Fingerprint == fp && a.ValidTo == nil {
			n++
		}
	}
	return n
}

// HistoryFor returns, in order, which identity owned a fingerprint over time.
// This is what a reviewer needs to see before undoing a merge.
func (s *IdentityStore) HistoryFor(fp string) []Alias {
	var out []Alias
	for _, a := range s.Aliases {
		if a.Fingerprint == fp {
			out = append(out, a)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ValidFrom.Before(out[j].ValidFrom) })
	return out
}

// UndoMerge is NOT the same operation as SplitFrom, and finding N43 is that
// §9.4 conflates them under the single label "undo for a wrong auto-match".
//
//	SplitFrom(T)  — "this test genuinely diverged into two from time T onward".
//	                History before T stays unified, correctly.
//	UndoMerge     — "that merge was a mistake and never should have happened".
//	                The prior attribution must be restored in full.
//
// Using SplitFrom to undo a wrong merge leaves every run between the merge and
// the discovery attributed to the wrong test. That window is invisible in the
// UI — the merged history still looks like a plausible test — so nobody notices
// the residue.
//
// Merge is deliberately retroactive: unifying the whole history is the point
// when a genuine rename was missed. That is what makes a real undo necessary
// rather than optional.
func (s *IdentityStore) UndoMerge(src, actor string, at time.Time) error {
	dst, ok := s.MergedInto[src]
	if !ok {
		return fmt.Errorf("%s was not merged into anything", src)
	}
	restored := 0
	for i := range s.Aliases {
		a := &s.Aliases[i]
		// Only aliases that this merge moved: recorded as manual_merge and now
		// pointing at the target.
		if a.IdentityID == dst && a.Source == SourceManualMerge {
			a.IdentityID = src
			a.Source = SourceExact
			a.CreatedBy = actor
			restored++
		}
	}
	delete(s.MergedInto, src)
	s.Audit = append(s.Audit, fmt.Sprintf("%s undid merge of %s into %s (%d aliases restored) at %s",
		actor, src, dst, restored, at.Format(time.RFC3339)))
	return nil
}
