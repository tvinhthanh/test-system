package billing

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/vinh/automation-testing-platform/backend/internal/platform/pg/pgtest"
	"github.com/vinh/automation-testing-platform/backend/migrations"
)

const (
	orgA  = "11111111-1111-4111-8111-111111111111"
	orgB  = "22222222-2222-4222-8222-222222222222"
	projA = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	projC = "cccccccc-cccc-4ccc-8ccc-cccccccccccc"
	projB = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
)

// §10.6: drift is the gap between how the meter and the raw fingerprints
// grew; a flagged day or a short record keeps the axis from billing.
func TestJudge(t *testing.T) {
	day := func(i, active, raw, corr int) Day {
		return Day{Day: time.Date(2026, 9, 1+i, 0, 0, 0, 0, time.UTC), ActiveTests: active, RawFingerprints: raw, Corrections: corr}
	}
	steady := []Day{}
	for i := 0; i < RequiredDays; i++ {
		steady = append(steady, day(i, 1000+i*10, 1020+i*10, 0)) // both grow about 1 % a day
	}
	r := judge(orgA, steady)
	if !r.Ready || len(r.Reasons) != 0 {
		t.Fatalf("28 steady days: ready=%v reasons=%v", r.Ready, r.Reasons)
	}
	for _, d := range r.Days {
		if d.Drift > 0.001 {
			t.Fatalf("steady growth drifted: %+v", d)
		}
	}

	// Day 10: a merge wave folds 40 identities; raw fingerprints do not move.
	// From then on the meter stays 40 lower — a merge is permanent.
	merged := append([]Day(nil), steady...)
	for i := 10; i < len(merged); i++ {
		merged[i].ActiveTests -= 40
	}
	merged[10].Corrections = 40
	r = judge(orgA, merged)
	if r.Ready || !r.Days[10].Flagged || r.Days[10].Drift < DriftLimit {
		t.Fatalf("a merge wave must flag its day: %+v", r.Days[10])
	}
	if !strings.Contains(strings.Join(r.Reasons, "; "), "1 day(s)") {
		t.Errorf("reasons: %v", r.Reasons)
	}
	// The next day grows with the raw count again: only the jump is flagged.
	if r.Days[11].Flagged {
		t.Errorf("the day after the merge is flagged: %+v", r.Days[11])
	}
	// A dip that comes straight back (a merge undone the next day) is two
	// moves the raw count never made: both days flagged.
	bounce := append([]Day(nil), steady...)
	bounce[10].ActiveTests -= 40
	if r := judge(orgA, bounce); !r.Days[10].Flagged || !r.Days[11].Flagged {
		t.Errorf("dip and rebound: %+v / %+v", r.Days[10], r.Days[11])
	}

	// Short records are never ready, however clean.
	if r := judge(orgA, steady[:5]); r.Ready || !strings.Contains(r.Reasons[0], "5 of 28") {
		t.Errorf("5 days: %+v", r.Reasons)
	}
	// Zero to zero is no drift; zero to something is full growth on both.
	if g := growth(0, 0); g != 0 {
		t.Errorf("growth(0,0) = %v", g)
	}
	if r := judge(orgA, []Day{day(0, 0, 0, 0), day(1, 5, 5, 0)}); r.Days[1].Flagged {
		t.Errorf("both appearing together flagged: %+v", r.Days[1])
	}
}

type fakeSource struct{ facts map[string][]Facts }

func (f *fakeSource) MeterFacts(_ context.Context, day time.Time) ([]Facts, error) {
	return f.facts[day.Format("2006-01-02")], nil
}

func TestRecordAndReportAgainstPostgres(t *testing.T) {
	db := pgtest.New(t, migrations.FS)
	ctx := context.Background()
	src := &fakeSource{facts: map[string][]Facts{
		"2026-09-01": {
			{ProjectID: projA, OrgID: orgA, ActiveTests: 100, RawFingerprints: 104, Runs: 3, Results: 300, ArtifactBytes: 1 << 20},
			{ProjectID: projC, OrgID: orgA, ActiveTests: 50, RawFingerprints: 50, Runs: 1, Results: 50},
			{ProjectID: projB, OrgID: orgB, ActiveTests: 7, RawFingerprints: 7},
		},
		"2026-09-02": {
			{ProjectID: projA, OrgID: orgA, ActiveTests: 60, RawFingerprints: 104, Merges: 40, ReviewDecisions: 2, Runs: 2, Results: 200},
			{ProjectID: projC, OrgID: orgA, ActiveTests: 50, RawFingerprints: 50},
		},
	}}
	svc := New(db, src)
	d1 := time.Date(2026, 9, 1, 15, 0, 0, 0, time.FixedZone("ICT", 7*3600)) // 08:00 UTC, still 1 Sept
	if ok, _ := svc.Recorded(ctx, d1); ok {
		t.Fatal("recorded before recording")
	}
	if n, err := svc.Record(ctx, d1); err != nil || n != 3 {
		t.Fatalf("record day 1: %d %v", n, err)
	}
	if ok, _ := svc.Recorded(ctx, d1); !ok {
		t.Error("day 1 not recorded")
	}
	// Recording again replaces, never doubles.
	if _, err := svc.Record(ctx, d1); err != nil {
		t.Fatal(err)
	}
	d2 := time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC)
	if _, err := svc.Record(ctx, d2); err != nil {
		t.Fatal(err)
	}

	r, err := svc.Report(ctx, orgA, time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), d2)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Days) != 2 {
		t.Fatalf("days: %+v", r.Days)
	}
	first, second := r.Days[0], r.Days[1]
	if first.ActiveTests != 150 || first.RawFingerprints != 154 || first.Runs != 4 || first.Results != 350 || first.ArtifactBytes != 1<<20 {
		t.Errorf("day 1 org sum: %+v", first)
	}
	if second.ActiveTests != 110 || second.Corrections != 42 || !second.Flagged || r.Ready {
		t.Errorf("day 2 (a merge wave): %+v ready=%v", second, r.Ready)
	}
	// Tenancy: org B sees only its own project; an unknown org sees nothing.
	if rb, _ := svc.Report(ctx, orgB, d1, d2); len(rb.Days) != 1 || rb.Days[0].ActiveTests != 7 {
		t.Errorf("org B: %+v", rb.Days)
	}
	if _, err := svc.Report(ctx, "not-a-uuid", d1, d2); !errors.Is(err, ErrNotFound) {
		t.Errorf("bad org id: %v", err)
	}
	if _, err := svc.Report(ctx, orgA, d2, d1); !errors.Is(err, ErrInvalid) {
		t.Errorf("backwards window: %v", err)
	}
}
