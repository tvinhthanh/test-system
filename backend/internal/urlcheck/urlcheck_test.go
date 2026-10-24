package urlcheck

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/vinh/automation-testing-platform/backend/internal/platform/pg/pgtest"
	"github.com/vinh/automation-testing-platform/backend/migrations"
)

const (
	projA = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	projB = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
)

var jpeg = []byte{0xFF, 0xD8, 0xFF, 0xE0, 1, 2, 3}

type fakeChecker struct {
	fail   error
	calls  []string
	logins []*Login
	links  map[string][]string // a scan's site: page → its links
}

func (f *fakeChecker) Check(_ context.Context, url string, login *Login) (Result, error) {
	f.calls = append(f.calls, url)
	f.logins = append(f.logins, login)
	if f.fail != nil {
		return Result{}, f.fail
	}
	report := `{"url":"` + url + `","finalUrl":"` + url + `/","title":"Shop","verdict":"warning","checks":[{"key":"load","status":"passed"},{"key":"basics","status":"warning"}]}`
	return Result{Report: json.RawMessage(report), Verdict: "warning", Desktop: jpeg, Mobile: []byte("<svg onload=alert(1)>"), Links: f.links[url]}, nil
}

// QĐ-14: a check is queued with its job, runs once through the checker,
// keeps the report and only real JPEGs, and a project has at most
// MaxQueued waiting.
func TestQueueRunAndRead(t *testing.T) {
	db := pgtest.New(t, migrations.FS)
	ctx := context.Background()
	fake := &fakeChecker{}
	svc := New(db, fake)

	for _, bad := range []string{"", "ftp://x.example", "x.example", "https://u:p@x.example", "https://"} {
		if _, err := svc.Create(ctx, projA, "", bad, "tia@acme.example"); !errors.Is(err, ErrInvalid) {
			t.Errorf("Create(%q) = %v, want ErrInvalid", bad, err)
		}
	}
	c, err := svc.Create(ctx, projA, "", "  HTTPS://Shop.Example/cart#top ", "tia@acme.example")
	if err != nil {
		t.Fatal(err)
	}
	if c.URL != "https://shop.example/cart" || c.Status != StatusQueued || c.Verdict != "" || c.FinishedAt != (time.Time{}) {
		t.Fatalf("queued check: %+v", c)
	}

	w := svc.Worker(1)
	if did, err := w.RunOnce(ctx); !did || err != nil {
		t.Fatalf("run: %v %v", did, err)
	}
	if did, _ := w.RunOnce(ctx); did {
		t.Fatal("a second job for one check")
	}
	if len(fake.calls) != 1 || fake.calls[0] != "https://shop.example/cart" {
		t.Fatalf("checker calls: %v", fake.calls)
	}
	got, err := svc.Get(ctx, projA, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != StatusDone || got.Verdict != "warning" || got.Title != "Shop" || got.FinalURL != "https://shop.example/cart/" ||
		!got.HasDesktop || got.HasMobile || !strings.Contains(string(got.Report), `"basics"`) {
		t.Fatalf("done check: %+v report=%s", got, got.Report)
	}
	if shot, err := svc.Screenshot(ctx, projA, c.ID, "desktop"); err != nil || string(shot) != string(jpeg) {
		t.Errorf("desktop shot: %v %v", shot, err)
	}
	if _, err := svc.Screenshot(ctx, projA, c.ID, "mobile"); !errors.Is(err, ErrNotFound) {
		t.Errorf("a non-JPEG is never stored: %v", err)
	}
	if _, err := svc.Screenshot(ctx, projA, c.ID, "../desktop"); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown screenshot: %v", err)
	}
	// Another project sees nothing of it.
	if _, err := svc.Get(ctx, projB, c.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("other project reads it: %v", err)
	}
	if _, err := svc.Screenshot(ctx, projB, c.ID, "desktop"); !errors.Is(err, ErrNotFound) {
		t.Errorf("other project reads its screenshot: %v", err)
	}
	list, err := svc.List(ctx, projA, 10)
	if err != nil || len(list) != 1 || list[0].Report != nil || strings.Join(list[0].Statuses, ",") != "passed,warning" {
		t.Fatalf("list: %v %+v", err, list)
	}

	for i := 0; i < MaxQueued; i++ {
		if _, err := svc.Create(ctx, projA, "", "https://shop.example/p"+string(rune('a'+i)), "tia@acme.example"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := svc.Create(ctx, projA, "", "https://shop.example/one-more", "tia@acme.example"); !errors.Is(err, ErrBusy) {
		t.Fatalf("over MaxQueued: %v", err)
	}
	if _, err := svc.Create(ctx, projB, "", "https://other.example", "bo@b.example"); err != nil {
		t.Fatalf("another project is not held back: %v", err)
	}
}

// The checker down: the queue retries, and the last attempt closes the
// check as error so it never stays queued.
func TestCheckerFailureEndsAsError(t *testing.T) {
	db := pgtest.New(t, migrations.FS)
	ctx := context.Background()
	fake := &fakeChecker{fail: errors.New("connection refused")}
	svc := New(db, fake)
	c, err := svc.Create(ctx, projA, "", "https://shop.example", "tia@acme.example")
	if err != nil {
		t.Fatal(err)
	}
	w := svc.Worker(1)
	w.Queue.Backoff = func(int) time.Duration { return 0 }
	for i := 0; i < 3; i++ {
		if did, err := w.RunOnce(ctx); !did || err != nil {
			t.Fatalf("attempt %d: %v %v", i+1, did, err)
		}
		got, _ := svc.Get(ctx, projA, c.ID)
		want := StatusQueued
		if i == 2 {
			want = StatusError
		}
		if got.Status != want {
			t.Fatalf("after attempt %d: %s, want %s", i+1, got.Status, want)
		}
	}
	got, _ := svc.Get(ctx, projA, c.ID)
	if !strings.Contains(got.Error, "connection refused") || got.FinishedAt.IsZero() {
		t.Fatalf("error check: %+v", got)
	}
}

func TestPruneAndPurge(t *testing.T) {
	db := pgtest.New(t, migrations.FS)
	ctx := context.Background()
	svc := New(db, &fakeChecker{})
	old, _ := svc.Create(ctx, projA, "", "https://old.example", "tia@acme.example")
	keep, _ := svc.Create(ctx, projA, "", "https://new.example", "tia@acme.example")
	other, _ := svc.Create(ctx, projB, "", "https://b.example", "bo@b.example")
	if _, err := db.Q(ctx).Exec(ctx, `UPDATE url_checks SET created_at = now() - interval '91 days' WHERE id = $1`, old.ID); err != nil {
		t.Fatal(err)
	}
	if n, err := svc.Prune(ctx, time.Now().Add(-Retention), 100); err != nil || n != 1 {
		t.Fatalf("prune: %d %v", n, err)
	}
	if _, err := svc.Get(ctx, projA, old.ID); !errors.Is(err, ErrNotFound) {
		t.Error("old check survived the prune")
	}
	// The pruned check's job finds nothing and is done quietly.
	w := svc.Worker(1)
	for {
		did, err := w.RunOnce(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if !did {
			break
		}
	}
	if err := svc.PurgeProject(ctx, projA); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Get(ctx, projA, keep.ID); !errors.Is(err, ErrNotFound) {
		t.Error("purged project's check survived")
	}
	if _, err := svc.Get(ctx, projB, other.ID); err != nil {
		t.Errorf("another project's check went with the purge: %v", err)
	}
}

// The real client against a stand-in checker: one POST to the configured
// host, report and screenshots decoded, a non-2xx answer is an error.
func TestHTTPChecker(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in struct{ URL string }
		_ = json.NewDecoder(r.Body).Decode(&in)
		if r.Method != http.MethodPost || r.URL.Path != "/check" || in.URL != "https://shop.example" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		_, _ = w.Write([]byte(`{"report":{"verdict":"failed","checks":[]},"screenshots":{"desktop":"/9j/4A==","mobile":""}}`))
	}))
	t.Cleanup(srv.Close)
	c, err := NewHTTPChecker(srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	res, err := c.Check(context.Background(), "https://shop.example", nil)
	if err != nil || res.Verdict != "failed" || len(res.Desktop) != 4 || len(res.Mobile) != 0 {
		t.Fatalf("check: %+v %v", res, err)
	}
	if _, err := c.Check(context.Background(), "https://elsewhere.example", nil); err == nil {
		t.Error("a 400 from the checker is not an error")
	}
}

type fakeLogins struct{}

func (fakeLogins) Login(_ context.Context, projectID, environmentID, account string) (*Login, error) {
	return &Login{URL: "https://shop.example/login", Steps: json.RawMessage(`[]`), Vars: map[string]string{"username": account}}, nil
}

// QĐ-16: a scan checks the start page, then the same-host pages it links
// to, breadth first, never a sign-out or delete link, never a file, never
// past maxPages or three links deep; it closes when nothing is queued; its
// pages stay out of the URL check list; signed in on every page.
func TestSiteScan(t *testing.T) {
	db := pgtest.New(t, migrations.FS)
	ctx := context.Background()
	fake := &fakeChecker{links: map[string][]string{
		"https://shop.example/": {"https://shop.example/a", "https://shop.example/b", "https://shop.example/logout",
			"https://other.example/x", "https://shop.example/report.pdf", "https://shop.example/a#top"},
		"https://shop.example/a":     {"https://shop.example/a/1", "https://shop.example/"},
		"https://shop.example/b":     {"https://shop.example/b/1", "https://shop.example/b/2"},
		"https://shop.example/a/1":   {"https://shop.example/a/1/x"},
		"https://shop.example/a/1/x": {"https://shop.example/third"},
		"https://shop.example/third": {"https://shop.example/fourth"},
	}}
	svc := New(db, fake, fakeLogins{})
	env := "eeeeeeee-eeee-4eee-8eee-eeeeeeeeeeee"
	sc, err := svc.CreateScan(ctx, NewScan{ProjectID: projA, EnvironmentID: env, URL: "https://shop.example", Account: "default", MaxPages: 6, Actor: "tia"})
	if err != nil || sc.Status != ScanRunning || sc.Pages != 1 {
		t.Fatalf("create: %+v %v", sc, err)
	}
	if _, err := svc.CreateScan(ctx, NewScan{ProjectID: projA, URL: "https://shop.example", Actor: "tia"}); !errors.Is(err, ErrBusy) {
		t.Errorf("a second running scan: %v", err)
	}
	if _, err := svc.Worker(1).Drain(ctx); err != nil {
		t.Fatal(err)
	}
	sc, err = svc.GetScan(ctx, projA, sc.ID)
	if err != nil || sc.Status != ScanDone || sc.Pages != 6 || sc.Checked != 6 || sc.Warnings != 6 || sc.FinishedAt.IsZero() {
		t.Fatalf("scan: %+v %v", sc, err)
	}
	got := []string{}
	for _, c := range sc.Checks {
		got = append(got, strings.TrimPrefix(c.URL, "https://shop.example"))
	}
	if strings.Join(got, " ") != "/ /a /b /a/1 /b/1 /b/2" {
		t.Errorf("pages: %v", got)
	}
	for i, l := range fake.logins {
		if l == nil || l.Vars["username"] != "default" {
			t.Errorf("check %d not signed in: %+v", i, l)
		}
	}
	if list, _ := svc.List(ctx, projA, 50); len(list) != 0 {
		t.Errorf("scan pages in the check list: %d", len(list))
	}
	if scans, _ := svc.Scans(ctx, projA, 10); len(scans) != 1 || scans[0].Pages != 6 {
		t.Errorf("scans: %+v", scans)
	}
	if _, err := svc.GetScan(ctx, projB, sc.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("another project reads the scan: %v", err)
	}
	// Depth: from /a, /third is three links away and still checked; the
	// page it links to would be four.
	deep, _ := svc.CreateScan(ctx, NewScan{ProjectID: projA, URL: "https://shop.example/a", MaxPages: 50, Actor: "tia"})
	if _, err := svc.Worker(1).Drain(ctx); err != nil {
		t.Fatal(err)
	}
	deep, _ = svc.GetScan(ctx, projA, deep.ID)
	urls := []string{}
	for _, c := range deep.Checks {
		urls = append(urls, strings.TrimPrefix(c.URL, "https://shop.example"))
	}
	if j := " " + strings.Join(urls, " ") + " "; !strings.Contains(j, " /third ") || strings.Contains(j, " /fourth ") || len(urls) != 8 {
		t.Errorf("deep scan: %v", urls)
	}
	for _, c := range deep.Checks {
		if c.Depth > MaxScanDepth {
			t.Errorf("%s at depth %d", c.URL, c.Depth)
		}
	}
	if err := svc.PurgeProject(ctx, projA); err != nil {
		t.Fatal(err)
	}
	if scans, _ := svc.Scans(ctx, projA, 10); len(scans) != 0 {
		t.Errorf("purge left %d scans", len(scans))
	}
}
