package autotest

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
)

// fakeLoader is the checker's /verify and /load: the proof file holds
// whatever `file` says; a load answers `result`.
type fakeLoader struct {
	mu     sync.Mutex
	file   string
	loads  []LoadSpec
	result string
}

func (l *fakeLoader) VerifyFile(_ context.Context, base string) (FileProof, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if !strings.HasPrefix(base, "https://chat.example.com") {
		return FileProof{Error: "unexpected base " + base}, nil
	}
	return FileProof{Status: 200, Text: l.file}, nil
}

func (l *fakeLoader) Load(_ context.Context, spec LoadSpec) (json.RawMessage, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.loads = append(l.loads, spec)
	return json.RawMessage(l.result), nil
}

// Load tests (QĐ-19 tier 3): nothing runs without a proof, the proof is
// checked again right before the traffic, the caps hold, the verdict reads
// the thresholds.
func TestLoadNeedsProofAndCaps(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	loader := &fakeLoader{result: `{"requests": 500, "errors": 5, "errorRate": 1.0, "latencyMs": {"p95": 340}}`}
	txt := map[string][]string{}
	f.svc.Loader = loader
	f.svc.TXT = func(_ context.Context, name string) ([]string, error) {
		if r, ok := txt[name]; ok {
			return r, nil
		}
		return nil, errors.New("no such host")
	}
	f.svc.LoadCaps = LoadCaps{MaxVUs: 10, MaxSeconds: 30}

	o, err := f.svc.Ownership(ctx, f.project, f.org, f.env, "tia")
	if err != nil || o.Host != "chat.example.com" || len(o.Token) != 32 || o.Status != OwnershipUnverified {
		t.Fatalf("ownership: %+v %v", o, err)
	}
	if again, _ := f.svc.Ownership(ctx, f.project, f.org, f.env, "tia"); again.Token != o.Token {
		t.Error("the token changed on a second read")
	}
	newLoad := func(vus, seconds int) (LoadRun, error) {
		return f.svc.CreateLoadRun(ctx, NewLoadRun{ProjectID: f.project, OrgID: f.org, EnvironmentID: f.env, Paths: []string{"/", "/inbox"},
			VUs: vus, Seconds: seconds, MaxP95Ms: 300, MaxErrorRate: 2, Actor: "tia"})
	}
	if _, err := newLoad(5, 20); !errors.Is(err, ErrNotProved) {
		t.Fatalf("a load before any proof: %v", err)
	}

	// A file without the token fails, with the reason; with it, verified.
	loader.file = "something else"
	if o, err = f.svc.CheckOwnership(ctx, f.project, f.org, f.env, "file", "tia"); err != nil || o.Status != OwnershipChecking {
		t.Fatalf("check: %+v %v", o, err)
	}
	if n, err := f.svc.OwnershipWorker(1).Drain(ctx); err != nil || n != 1 {
		t.Fatalf("drain: %d %v", n, err)
	}
	if o, _ = f.svc.Ownership(ctx, f.project, f.org, f.env, "tia"); o.Status != OwnershipFailed || !strings.Contains(o.Error, "does not contain atp-verification=") {
		t.Fatalf("wrong file: %+v", o)
	}
	loader.file = "# ATP\n" + o.FileText() + "\n"
	if _, err := f.svc.CheckOwnership(ctx, f.project, f.org, f.env, "file", "tia"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.OwnershipWorker(1).Drain(ctx); err != nil {
		t.Fatal(err)
	}
	if o, _ = f.svc.Ownership(ctx, f.project, f.org, f.env, "tia"); o.Status != OwnershipVerified || o.VerifiedAt.IsZero() {
		t.Fatalf("right file: %+v", o)
	}

	// Caps, paths, one at a time per organization.
	for _, bad := range []NewLoadRun{
		{VUs: 11, Seconds: 20}, {VUs: 5, Seconds: 31}, {VUs: 5, Seconds: 20, RampSeconds: 20},
		{VUs: 5, Seconds: 20, Paths: []string{"//evil.example/"}}, {VUs: 5, Seconds: 20, Paths: []string{"https://evil.example/"}},
	} {
		bad.ProjectID, bad.OrgID, bad.EnvironmentID, bad.Actor, bad.MaxErrorRate = f.project, f.org, f.env, "tia", -1
		if _, err := f.svc.CreateLoadRun(ctx, bad); !errors.Is(err, ErrInvalid) {
			t.Errorf("%+v: %v", bad, err)
		}
	}
	run, err := newLoad(5, 20)
	if err != nil || run.Status != RunQueued || run.Host != "chat.example.com" {
		t.Fatalf("create: %+v %v", run, err)
	}
	if _, err := newLoad(5, 20); !errors.Is(err, ErrBusy) {
		t.Errorf("a second load at once: %v", err)
	}
	if n, err := f.svc.LoadWorker(1).Drain(ctx); err != nil || n != 1 {
		t.Fatalf("drain load: %d %v", n, err)
	}
	run, err = f.svc.GetLoadRun(ctx, f.project, run.ID)
	if err != nil || run.Status != RunDone || run.Verdict != "failed" || len(run.Reasons) != 1 || !strings.Contains(run.Reasons[0], "p95 340 ms is over 300 ms") {
		t.Fatalf("load run: %+v %v", run, err)
	}
	if len(loader.loads) != 1 || loader.loads[0].VUs != 5 || loader.loads[0].Base != "https://chat.example.com" || len(loader.loads[0].Paths) != 2 {
		t.Fatalf("checker asked: %+v", loader.loads)
	}

	// The proof is checked again before the traffic: a host that no longer
	// shows the token gets none, and its proof is marked failed.
	loader.file = "gone"
	run, err = newLoad(2, 10)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.LoadWorker(1).Drain(ctx); err != nil {
		t.Fatal(err)
	}
	run, _ = f.svc.GetLoadRun(ctx, f.project, run.ID)
	if run.Status != RunError || !strings.Contains(run.Error, "no longer holds") || len(loader.loads) != 1 {
		t.Fatalf("a proof that went: %+v, %d loads", run, len(loader.loads))
	}
	if o, _ = f.svc.Ownership(ctx, f.project, f.org, f.env, "tia"); o.Status != OwnershipFailed {
		t.Errorf("the proof should be failed now: %+v", o)
	}

	// DNS works too.
	txt[TXTPrefix+"chat.example.com"] = []string{"v=spf1 -all", o.FileText()}
	if _, err := f.svc.CheckOwnership(ctx, f.project, f.org, f.env, "dns", "tia"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.OwnershipWorker(1).Drain(ctx); err != nil {
		t.Fatal(err)
	}
	if o, _ = f.svc.Ownership(ctx, f.project, f.org, f.env, "tia"); o.Status != OwnershipVerified || o.Method != "dns" {
		t.Fatalf("dns proof: %+v", o)
	}
	if runs, err := f.svc.LoadRuns(ctx, f.project, 10); err != nil || len(runs) != 2 {
		t.Errorf("list: %d %v", len(runs), err)
	}
	if err := f.svc.PurgeProject(ctx, f.project); err != nil {
		t.Fatal(err)
	}
	if runs, _ := f.svc.LoadRuns(ctx, f.project, 10); len(runs) != 0 {
		t.Errorf("purge left %d load runs", len(runs))
	}
	// The proof is the organization's: it outlives a project, not the org.
	var proofs int
	count := func() int {
		if err := f.svc.DB.Q(ctx).QueryRow(ctx, `SELECT count(*) FROM site_ownership WHERE org_id = $1`, f.org).Scan(&proofs); err != nil {
			t.Fatal(err)
		}
		return proofs
	}
	if count() != 1 {
		t.Fatalf("a project purge took the org's proof: %d", proofs)
	}
	if err := f.svc.PurgeOrganization(ctx, f.org); err != nil || count() != 0 {
		t.Fatalf("org purge: %v, %d proofs left", err, proofs)
	}
}
