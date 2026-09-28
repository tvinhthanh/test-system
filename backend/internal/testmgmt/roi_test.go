package testmgmt

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"
)

func near(a, b float64) bool { return math.Abs(a-b) < 1e-9 }

// §6.8.3's arithmetic, one rule at a time.
func TestComputeROI(t *testing.T) {
	from := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	to := from.AddDate(0, 1, 0)
	facts := ROIFacts{
		Failures: 40, Triaged: 30,
		TTRCSum:     30 * 45 * time.Minute, // 22.5 h: a median-ish 45 min each
		FlakyRetry:  90 * time.Minute,
		MarkedFlaky: 6,
	}
	base := &Baseline{Kind: Prospective, Median: 3 * time.Hour, FlakyInvestigation: 20 * time.Minute}

	// Rule 3: no baseline, no savings figure — never a made-up default.
	none := computeROI(from, to, facts, nil)
	if none.SavedHours != nil {
		t.Fatalf("saved hours without a baseline: %v", *none.SavedHours)
	}
	// Rule 1: coverage and untriaged rate travel with it either way.
	if !near(none.TriageCoverage, 0.75) || !near(none.UntriagedRate, 0.25) {
		t.Errorf("coverage %v untriaged %v", none.TriageCoverage, none.UntriagedRate)
	}
	// Without a measured investigation time only the CI retry time counts.
	if !near(none.FlakyHours, 1.5) || none.InvestigationCounted {
		t.Errorf("flaky hours without baseline: %v counted=%v", none.FlakyHours, none.InvestigationCounted)
	}

	got := computeROI(from, to, facts, base)
	// Σ(3 h − TTRC_i) over 30 triaged = 90 h − 22.5 h.
	if got.SavedHours == nil || !near(*got.SavedHours, 67.5) {
		t.Fatalf("saved hours = %v, want 67.5", got.SavedHours)
	}
	// 1.5 h of retries + 6 × 20 min of investigation.
	if !near(got.FlakyHours, 3.5) || !got.InvestigationCounted {
		t.Errorf("flaky hours = %v counted=%v, want 3.5", got.FlakyHours, got.InvestigationCounted)
	}
	if got.FewTriaged {
		t.Error("30 triaged flagged as too few")
	}

	// Slower than before is shown as it is: negative, not clamped.
	slow := computeROI(from, to, ROIFacts{Failures: 5, Triaged: 5, TTRCSum: 5 * 4 * time.Hour}, base)
	if slow.SavedHours == nil || !near(*slow.SavedHours, -5) || !slow.FewTriaged {
		t.Errorf("slower month: saved %v few=%v", slow.SavedHours, slow.FewTriaged)
	}
	// No failures: no division by zero, coverage 0.
	empty := computeROI(from, to, ROIFacts{}, base)
	if empty.TriageCoverage != 0 || empty.SavedHours == nil || *empty.SavedHours != 0 {
		t.Errorf("empty month: %+v", empty)
	}
}

// §5.4: the newest prospective baseline is the official one; a
// retrospective stands in until there is one.
func TestApplicableBaseline(t *testing.T) {
	retroNew := Baseline{ID: "r2", Kind: Retrospective}
	pro := Baseline{ID: "p1", Kind: Prospective}
	retroOld := Baseline{ID: "r1", Kind: Retrospective}
	if b := applicable([]Baseline{retroNew, pro, retroOld}); b == nil || b.ID != "p1" {
		t.Errorf("with a prospective: %v", b)
	}
	if b := applicable([]Baseline{retroNew, retroOld}); b == nil || b.ID != "r2" {
		t.Errorf("retrospective only: %v", b)
	}
	if applicable(nil) != nil {
		t.Error("no baseline invented")
	}
}

func TestBaselinesAgainstPostgres(t *testing.T) {
	svc, _, ev := newService(t)
	ctx := context.Background()
	day := func(s string) time.Time { v, _ := time.Parse("2006-01-02", s); return v }
	rate := 0.3
	valid := NewBaseline{
		Kind: Retrospective, Failures: 10, Median: 3 * time.Hour, P90: 26 * time.Hour, RerunPassRate: &rate,
		PeriodFrom: day("2026-08-01"), PeriodTo: day("2026-08-14"), Note: "10 failures rebuilt from Slack",
	}

	for name, mutate := range map[string]func(*NewBaseline){
		"unknown kind":       func(b *NewBaseline) { b.Kind = "guess" },
		"no failures":        func(b *NewBaseline) { b.Failures = 0 },
		"median under 1 min": func(b *NewBaseline) { b.Median = 30 * time.Second },
		"p90 under median":   func(b *NewBaseline) { b.P90 = time.Hour },
		"rate over 1":        func(b *NewBaseline) { r := 1.5; b.RerunPassRate = &r },
		"period backwards":   func(b *NewBaseline) { b.PeriodTo = day("2026-07-01") },
		"measured in future": func(b *NewBaseline) { b.PeriodTo = time.Now().AddDate(0, 1, 0) },
		"no period":          func(b *NewBaseline) { b.PeriodFrom = time.Time{} },
	} {
		in := valid
		mutate(&in)
		if _, err := svc.AddBaseline(ctx, projectA, in, "em@acme.example"); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: err = %v, want ErrInvalid", name, err)
		}
	}
	if _, err := svc.AddBaseline(ctx, projectA, valid, " "); !errors.Is(err, ErrInvalid) {
		t.Errorf("no actor: %v", err)
	}

	retro, err := svc.AddBaseline(ctx, projectA, valid, "em@acme.example")
	if err != nil {
		t.Fatal(err)
	}
	if retro.Median != 3*time.Hour || retro.P90 != 26*time.Hour || retro.RerunPassRate == nil || !near(*retro.RerunPassRate, 0.3) ||
		retro.FlakyInvestigation != 0 || retro.EnteredBy != "em@acme.example" || !retro.PeriodTo.Equal(day("2026-08-14")) {
		t.Errorf("stored baseline: %+v", retro)
	}

	ev.roi = ROIFacts{Failures: 20, Triaged: 12, TTRCSum: 12 * time.Hour, FlakyRetry: 30 * time.Minute, MarkedFlaky: 2}
	roi, err := svc.ROI(ctx, projectA, time.Date(2026, 9, 17, 13, 0, 0, 0, time.FixedZone("ICT", 7*3600)))
	if err != nil {
		t.Fatal(err)
	}
	if !roi.From.Equal(time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)) || !roi.To.Equal(time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("window %v – %v, want the calendar month in UTC", roi.From, roi.To)
	}
	if roi.Baseline == nil || roi.Baseline.ID != retro.ID || roi.SavedHours == nil || !near(*roi.SavedHours, 12*3-12) {
		t.Errorf("roi with the retrospective: %+v", roi)
	}

	// A prospective measurement arrives: it becomes the baseline, and its
	// investigation time brings the second flaky term in.
	pro := valid
	pro.Kind, pro.Median, pro.P90, pro.FlakyInvestigation = Prospective, 2*time.Hour, 0, 15*time.Minute
	if _, err := svc.AddBaseline(ctx, projectA, pro, "em@acme.example"); err != nil {
		t.Fatal(err)
	}
	roi, _ = svc.ROI(ctx, projectA, time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC))
	if roi.Baseline == nil || roi.Baseline.Kind != Prospective || !near(*roi.SavedHours, 12*2-12) || !near(roi.FlakyHours, 0.5+0.5) {
		t.Errorf("roi with the prospective: saved %v flaky %v", roi.SavedHours, roi.FlakyHours)
	}
	list, _ := svc.Baselines(ctx, projectA)
	if len(list) != 2 || list[0].Kind != Prospective {
		t.Errorf("history newest first: %+v", list)
	}
	// Another project sees none of it.
	if other, _ := svc.Baselines(ctx, projectB); len(other) != 0 {
		t.Errorf("project B sees %d baselines", len(other))
	}
	if roiB, _ := svc.ROI(ctx, projectB, time.Now()); roiB.Baseline != nil || roiB.SavedHours != nil {
		t.Error("project B got A's baseline")
	}
}
