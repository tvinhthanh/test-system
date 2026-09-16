package identity_test

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/rand"
	"sync"
	"testing"
	"time"

	"github.com/vinh/automation-testing-platform/backend/internal/identity"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/pg"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/pg/pgtest"
	"github.com/vinh/automation-testing-platform/backend/migrations"
)

const (
	project = "11111111-1111-4111-8111-111111111111"
	run1    = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaa1"
)

func sig(file string, titles ...string) identity.Signals {
	return identity.Signals{Framework: "playwright", FilePath: file, TitlePath: titles}
}

func ref(key string, s identity.Signals) identity.Ref { return identity.Ref{Key: key, Signals: s} }

// TestTrigramParityWithPostgres is N49/N50 made executable: the Go
// TrigramSimilarity used for S_file must agree with pg_trgm's similarity()
// on the strings this system sees, or the candidate filter (SQL) and the
// score (Go) measure different things.
func TestTrigramParityWithPostgres(t *testing.T) {
	db := pgtest.New(t, migrations.FS)
	ctx := context.Background()
	rng := rand.New(rand.NewSource(1))
	words := []string{"declines", "expired", "card", "checkout", "payment", "spec", "ts", "login", "flow", "kiểm", "tra", "đơn", "hàng", "e2e", "handles-404", "order_8412", "Users/Admin"}
	pairs := [][2]string{{"a/b", "a b"}, {"two words", "two"}, {"Tests/Login.spec", "tests/login.e2e.spec"}, {"", ""}, {"///", "x"}, {"đơn hàng", "đơn hàng bị từ chối"}}
	for i := 0; i < 200; i++ {
		mk := func() string {
			n := 1 + rng.Intn(5)
			s := ""
			for j := 0; j < n; j++ {
				if j > 0 {
					s += []string{" ", "/", ".", "-", "_"}[rng.Intn(5)]
				}
				s += words[rng.Intn(len(words))]
			}
			return s
		}
		pairs = append(pairs, [2]string{mk(), mk()})
	}
	// pg_trgm classifies characters with the database's LC_CTYPE. Under C or
	// POSIX it works on bytes, every non-ASCII letter becomes a separator,
	// and no Go implementation can agree with it on Vietnamese titles. That
	// is a deployment requirement (a UTF-8 locale), so here it narrows the
	// check to ASCII and says so loudly rather than failing on a dev box.
	var ctype string
	if err := db.Q(ctx).QueryRow(ctx, `SELECT datctype FROM pg_database WHERE datname = current_database()`).Scan(&ctype); err != nil {
		t.Fatal(err)
	}
	byteLocale := ctype == "C" || ctype == "POSIX"
	if byteLocale {
		t.Logf("WARNING: database LC_CTYPE=%s — pg_trgm splits on every non-ASCII byte; comparing ASCII pairs only. Production must use a UTF-8 locale.", ctype)
	}
	isASCII := func(s string) bool {
		for i := 0; i < len(s); i++ {
			if s[i] >= 0x80 {
				return false
			}
		}
		return true
	}
	worst, compared := 0.0, 0
	for _, p := range pairs {
		if byteLocale && !(isASCII(p[0]) && isASCII(p[1])) {
			continue
		}
		compared++
		var want float64
		if err := db.Q(ctx).QueryRow(ctx, `SELECT public.similarity($1, $2)`, p[0], p[1]).Scan(&want); err != nil {
			t.Fatal(err)
		}
		got := identity.TrigramSimilarityForTest(p[0], p[1])
		if d := math.Abs(got - want); d > 1e-6 {
			t.Errorf("similarity(%q, %q): go %.4f pg %.4f", p[0], p[1], got, want)
			worst = math.Max(worst, d)
		}
	}
	if compared < 50 {
		t.Fatalf("only %d pairs compared", compared)
	}
	t.Logf("%d/%d pairs compared (LC_CTYPE=%s), worst drift %.6f", compared, len(pairs), ctype, worst)
}

func TestResolveTiersAgainstPostgres(t *testing.T) {
	db := pgtest.New(t, migrations.FS)
	svc := identity.New(db)
	ctx := context.Background()

	pay := sig("tests/checkout/payment.spec.ts", "Checkout", "Payment", "declines expired card")
	cart := sig("tests/checkout/cart.spec.ts", "Checkout", "Cart", "keeps state on reload")
	login := sig("tests/auth/login.spec.ts", "Auth", "Login", "rejects duplicate email")

	// Tier 1 on an empty project knows nothing and creates nothing.
	m, err := svc.ResolveInline(ctx, project, []identity.Ref{ref("ct_pay", pay), ref("ct_cart", cart)})
	if err != nil || len(m) != 0 {
		t.Fatalf("inline on empty: %v %v", m, err)
	}

	// Tier 2 on run 1: three new identities.
	m, outcomes, err := svc.ResolveDeferred(ctx, project, run1, []identity.Ref{ref("ct_pay", pay), ref("ct_cart", cart), ref("ct_login", login), ref("ct_pay_again", pay)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(m) != 4 || m["ct_pay"] == "" || m["ct_pay"] != m["ct_pay_again"] || m["ct_pay"] == m["ct_cart"] {
		t.Fatalf("deferred: %v", m)
	}
	news := 0
	for _, o := range outcomes {
		if o.Decision == identity.DecisionNew {
			news++
		}
	}
	if news != 3 {
		t.Errorf("%d new identities, want 3 (%+v)", news, outcomes)
	}
	payID, cartID := m["ct_pay"], m["ct_cart"]

	// Tier 1 now hits, and touches last_seen_at.
	m, err = svc.ResolveInline(ctx, project, []identity.Ref{ref("ct_pay", pay), ref("ct_new", sig("t.spec.ts", "x"))})
	if err != nil || m["ct_pay"] != payID || len(m) != 1 {
		t.Errorf("inline after create: %v %v", m, err)
	}
	got, err := svc.Get(ctx, payID)
	if err != nil || got.Status != identity.StatusActive || got.CurrentFingerprint == "" || got.CanonicalFilePath != "tests/checkout/payment.spec.ts" {
		t.Errorf("get: %+v %v", got, err)
	}

	// Run 2: payment test renamed (leaf reworded), cart still present.
	// Presence of cart is the N34 signal: cart takes the ×0.3 penalty, the
	// absent payment identity is the clear winner → auto-match.
	renamed := sig("tests/checkout/payment.spec.ts", "Checkout", "Payment", "rejects expired card")
	m, outcomes, err = svc.ResolveDeferred(ctx, project, run1, []identity.Ref{ref("ct_pay2", renamed)}, []string{cartID})
	if err != nil {
		t.Fatal(err)
	}
	if m["ct_pay2"] != payID || outcomes[0].Decision != identity.DecisionAutoMatch {
		t.Fatalf("rename: %v %+v (want auto-match to %s)", m, outcomes, payID)
	}
	if got, _ := svc.Get(ctx, payID); got.CanonicalTitlePath[2] != "rejects expired card" || got.CurrentFingerprint == "" {
		t.Errorf("identity did not follow the rename: %+v", got)
	}
	aliases, _ := svc.Aliases(ctx, payID)
	if len(aliases) != 2 || aliases[1].Source != identity.SourceAutoMatch || aliases[1].MatchScore < 0.85 {
		t.Errorf("aliases after auto-match: %+v", aliases)
	}
	// Both the old and the new fingerprint exact-match now.
	m, _ = svc.ResolveInline(ctx, project, []identity.Ref{ref("old", pay), ref("new", renamed)})
	if m["old"] != payID || m["new"] != payID {
		t.Errorf("old/new fingerprints: %v", m)
	}

	// A near-twin while the original is still present is a NEW test: cart
	// takes the ×0.3 penalty and drops below even the review line.
	twin := sig("tests/checkout/cart.spec.ts", "Checkout", "Cart", "keeps state on reload twice")
	m, outcomes, err = svc.ResolveDeferred(ctx, project, run1, []identity.Ref{ref("ct_twin", twin)}, []string{cartID, payID})
	if err != nil {
		t.Fatal(err)
	}
	if m["ct_twin"] == cartID || outcomes[0].Decision != identity.DecisionNew {
		t.Fatalf("twin while cart is present: %v %+v (want a new identity)", m, outcomes)
	}

	// Two absent look-alikes: both clear the threshold, neither leads by
	// MinGap (N40) → a new identity now and a pair for a human (§9.3 step 4).
	a := sig("tests/search/filters.spec.ts", "Search", "Filters", "sorts by relevance")
	b := sig("tests/search/filters.spec.ts", "Search", "Filters", "sorts by relevance!")
	m, _, err = svc.ResolveDeferred(ctx, project, run1, []identity.Ref{ref("a", a), ref("b", b)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	aID, bID := m["a"], m["b"]
	reworded := sig("tests/search/filters.spec.ts", "Search", "Filters", "orders by relevance")
	m, outcomes, err = svc.ResolveDeferred(ctx, project, run1, []identity.Ref{ref("ct_rw", reworded)}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if outcomes[0].Decision != identity.DecisionReview {
		t.Fatalf("look-alikes: %+v, want review (score %.3f gap %.3f)", outcomes[0], outcomes[0].Score, outcomes[0].Gap)
	}
	rwID := m["ct_rw"]
	if rwID == aID || rwID == bID {
		t.Fatal("review case must not merge on its own")
	}
	items, err := svc.ListReview(ctx, project, 10)
	if err != nil || len(items) != 1 || items[0].IdentityID != rwID || (items[0].CandidateID != aID && items[0].CandidateID != bID) || items[0].RunID != run1 {
		t.Fatalf("review queue: %+v %v", items, err)
	}
	if items[0].Breakdown["adjusted"] == 0 || items[0].Gap >= 0.10 {
		t.Errorf("review item should explain itself: %+v", items[0])
	}
	// The reviewer confirms the rename: the new identity folds into the
	// candidate, and the reworded fingerprint now exact-matches it.
	if err := svc.DecideReview(ctx, items[0].ID, identity.ReviewMerged, "reviewer@x"); err != nil {
		t.Fatal(err)
	}
	if got, _ := svc.Get(ctx, rwID); got.Status != identity.StatusMerged || got.MergedIntoID != items[0].CandidateID {
		t.Errorf("after merged verdict: %+v", got)
	}
	if m, _ := svc.ResolveInline(ctx, project, []identity.Ref{ref("rw", reworded)}); m["rw"] != items[0].CandidateID {
		t.Errorf("reworded fingerprint resolves to %s, want %s", m["rw"], items[0].CandidateID)
	}
	if items, _ := svc.ListReview(ctx, project, 10); len(items) != 0 {
		t.Error("decided item still pending")
	}
	if err := svc.DecideReview(ctx, items[0].ID, identity.ReviewKept, "reviewer@x"); !errors.Is(err, identity.ErrConflict) {
		t.Errorf("deciding twice: %v", err)
	}

	// Another project sees none of this.
	m, _ = svc.ResolveInline(ctx, "22222222-2222-4222-8222-222222222222", []identity.Ref{ref("ct_pay", pay)})
	if len(m) != 0 {
		t.Error("identity leaked across projects")
	}
}

// TestRaceOnUnknownFingerprint is §9.7's last row: two runs sending the same
// unknown fingerprint at once create exactly one identity.
func TestRaceOnUnknownFingerprint(t *testing.T) {
	db := pgtest.New(t, migrations.FS)
	svc := identity.New(db)
	ctx := context.Background()
	s := sig("tests/race.spec.ts", "Race", "same test")
	const n = 8
	ids := make(chan string, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			m, _, err := svc.ResolveDeferred(ctx, project, "", []identity.Ref{ref("k", s)}, nil)
			if err != nil {
				t.Errorf("worker %d: %v", i, err)
				return
			}
			ids <- m["k"]
		}(i)
	}
	wg.Wait()
	close(ids)
	seen := map[string]bool{}
	for id := range ids {
		seen[id] = true
	}
	if len(seen) != 1 {
		t.Errorf("%d distinct identities for one fingerprint: %v", len(seen), seen)
	}
	var count int
	_ = db.Q(ctx).QueryRow(ctx, `SELECT count(*) FROM test_identities WHERE project_id = $1`, project).Scan(&count)
	if count != 1 {
		t.Errorf("%d identity rows, want 1", count)
	}
}

// day returns a UTC timestamp for "August n, 2026", the prototype's clock.
func day(n int) time.Time { return time.Date(2026, 8, n, 12, 0, 0, 0, time.UTC) }

// TestSplitIsNotAnUndo is N43 on the real tables. A wrong merge on day 10,
// discovered on day 30: SplitFrom leaves days 10–29 wrong, UndoMerge leaves
// none. Both exist, and they are not interchangeable.
func TestSplitIsNotAnUndo(t *testing.T) {
	db := pgtest.New(t, migrations.FS)
	ctx := context.Background()
	fpLogin, fpLogout := sig("tests/auth.spec.ts", "Auth", "login").Fingerprint(), sig("tests/auth.spec.ts", "Auth", "logout").Fingerprint()

	build := func(t *testing.T, clock *time.Time) (identity.Service, string, string) {
		svc := identity.NewForTest(db, func() time.Time { return *clock })
		*clock = day(1)
		m, _, err := svc.ResolveDeferred(ctx, project, "", []identity.Ref{ref("login", sig("tests/auth.spec.ts", "Auth", "login")), ref("logout", sig("tests/auth.spec.ts", "Auth", "logout"))}, nil)
		if err != nil {
			t.Fatal(err)
		}
		// Day 10: the wrong merge — logout folded into login.
		*clock = day(10)
		if err := svc.Merge(ctx, project, m["login"], m["logout"], "auto"); err != nil {
			t.Fatal(err)
		}
		if got, _ := svc.ResolveAt(ctx, project, fpLogout, day(20)); got != m["login"] {
			t.Fatalf("premise: after the merge fpLogout on day 20 resolves to %s, want login %s", got, m["login"])
		}
		return svc, m["login"], m["logout"]
	}
	wrongDays := func(svc identity.Service, logoutID string) int {
		wrong := 0
		for d := 1; d < 30; d++ {
			if got, _ := svc.ResolveAt(ctx, project, fpLogout, day(d)); got != logoutID {
				wrong++
			}
		}
		return wrong
	}

	t.Run("SplitFrom", func(t *testing.T) {
		t.Cleanup(func() { wipe(t, db) })
		clock := day(1)
		svc, loginID, logoutID := build(t, &clock)
		clock = day(30)
		newID, err := svc.SplitFrom(ctx, project, fpLogout, day(30), "reviewer@x")
		if err != nil {
			t.Fatal(err)
		}
		if got, _ := svc.ResolveAt(ctx, project, fpLogout, day(31)); got != newID {
			t.Errorf("after split, day 31 resolves to %s, want the new identity %s", got, newID)
		}
		if got, _ := svc.ResolveAt(ctx, project, fpLogin, day(31)); got != loginID {
			t.Errorf("login disturbed: %s", got)
		}
		if got, _ := svc.ResolveAt(ctx, project, fpLogout, day(31)); got == loginID {
			t.Error("login still owns logout after the split")
		}
		if w := wrongDays(svc, logoutID); w != 29 {
			t.Errorf("SplitFrom left %d/29 days wrong; expected all 29 — that is the N43 finding", w)
		}
		// Invariants: one alias in force per fingerprint; the split identity
		// carries the alias's signals; the closed interval is preserved.
		var inForce int
		_ = db.Q(ctx).QueryRow(ctx, `SELECT count(*) FROM test_identity_aliases WHERE fingerprint = $1 AND valid_to IS NULL`, fpLogout).Scan(&inForce)
		if inForce != 1 {
			t.Errorf("%d aliases in force for the split fingerprint", inForce)
		}
		if got, _ := svc.Get(ctx, newID); got.CanonicalTitlePath[1] != "logout" {
			t.Errorf("split identity signals: %+v", got)
		}
		// Splitting before the alias started, or an unknown fingerprint, is refused.
		if _, err := svc.SplitFrom(ctx, project, fpLogin, day(0), "r"); !errors.Is(err, identity.ErrInvalid) {
			t.Errorf("split before start: %v", err)
		}
		if _, err := svc.SplitFrom(ctx, project, "fp1:nope", day(31), "r"); !errors.Is(err, identity.ErrNotFound) {
			t.Errorf("split unknown: %v", err)
		}
	})

	t.Run("UndoMerge", func(t *testing.T) {
		t.Cleanup(func() { wipe(t, db) })
		clock := day(1)
		svc, loginID, logoutID := build(t, &clock)
		clock = day(30)
		if err := svc.UndoMerge(ctx, project, logoutID, "reviewer@x"); err != nil {
			t.Fatal(err)
		}
		if w := wrongDays(svc, logoutID); w != 0 {
			t.Errorf("UndoMerge left %d/29 days wrong; must restore the prior state in full", w)
		}
		if got, _ := svc.Get(ctx, logoutID); got.Status != identity.StatusActive || got.MergedIntoID != "" {
			t.Errorf("logout not reactivated: %+v", got)
		}
		al, _ := svc.Aliases(ctx, logoutID)
		if len(al) != 1 || al[0].Source != identity.SourceExact {
			t.Errorf("restored alias should carry its prior source: %+v", al)
		}
		if got, _ := svc.ResolveAt(ctx, project, fpLogin, day(31)); got != loginID {
			t.Errorf("login disturbed by the undo: %s", got)
		}
		if err := svc.UndoMerge(ctx, project, logoutID, "reviewer@x"); !errors.Is(err, identity.ErrConflict) {
			t.Errorf("undo twice: %v", err)
		}
		// Merge guards.
		if err := svc.Merge(ctx, project, loginID, loginID, "r"); !errors.Is(err, identity.ErrInvalid) {
			t.Errorf("self merge: %v", err)
		}
		if err := svc.Merge(ctx, project, loginID, logoutID, "r"); err != nil {
			t.Fatal(err)
		}
		third, _, _ := svc.ResolveDeferred(ctx, project, "", []identity.Ref{ref("t", sig("tests/other.spec.ts", "Other", "thing"))}, nil)
		if err := svc.Merge(ctx, project, logoutID, third["t"], "r"); !errors.Is(err, identity.ErrConflict) {
			t.Errorf("merging into a retired identity: %v", err)
		}
		// Merge chains resolve to the survivor.
		if got, _ := svc.ResolveAt(ctx, project, fpLogout, day(40)); got != loginID {
			t.Errorf("after re-merge: %s", got)
		}
		var audits int
		_ = db.Q(ctx).QueryRow(ctx, `SELECT count(*) FROM identity_audit WHERE project_id = $1`, project).Scan(&audits)
		if audits < 3 {
			t.Errorf("%d audit rows; merge, undo and merge again must each leave one (§9.4)", audits)
		}
	})
}

// wipe clears the identity tables between sub-tests sharing one schema.
func wipe(t *testing.T, db *pg.DB) {
	t.Helper()
	ctx := context.Background()
	for _, tbl := range []string{"review_queue_items", "identity_audit", "test_identity_aliases", "test_identities"} {
		if _, err := db.Q(ctx).Exec(ctx, fmt.Sprintf(`DELETE FROM %s`, tbl)); err != nil {
			t.Fatal(err)
		}
	}
}
