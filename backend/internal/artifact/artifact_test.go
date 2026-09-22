package artifact_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/vinh/automation-testing-platform/backend/internal/artifact"
	"github.com/vinh/automation-testing-platform/backend/internal/authz"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/httpx"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/metric"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/objstore/memory"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/pg"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/pg/pgtest"
	"github.com/vinh/automation-testing-platform/backend/internal/results"
	"github.com/vinh/automation-testing-platform/backend/internal/tenancy"
	"github.com/vinh/automation-testing-platform/backend/migrations"
)

var pepper = []byte("test-pepper-test-pepper-test-pepper!")

type env struct {
	t        *testing.T
	db       *pg.DB
	srv      *httptest.Server
	store    *memory.Store
	svc      artifact.Service
	jobs     *artifact.Jobs
	results  results.Service
	reg      *metric.Registry
	tokenA   string
	tokenB   string
	projectA string
	runA     results.Run // running, in project A
}

func setup(t *testing.T) *env {
	t.Helper()
	db := pgtest.New(t, migrations.FS)
	ctx := context.Background()
	ten := tenancy.New(db)
	az := authz.New(db, pepper)
	res := results.New(db)
	store := memory.New()
	t.Cleanup(store.Close)
	reg := metric.New()

	org, _ := ten.CreateOrganization(ctx, "Acme", "acme")
	ws, _ := ten.CreateWorkspace(ctx, org.ID, "Payments")
	pa, _ := ten.CreateProject(ctx, ws.ID, "checkout", "")
	pb, _ := ten.CreateProject(ctx, ws.ID, "search", "")
	issue := func(project tenancy.ProjectID) string {
		is, err := az.IssueToken(ctx, authz.TokenSpec{Scope: authz.ScopeRef{Level: authz.LevelProject, ID: string(project)}, Permissions: []authz.Permission{authz.PermProjectIngest}, Name: "t"})
		if err != nil {
			t.Fatal(err)
		}
		return is.Plaintext
	}
	envA, _ := ten.EnsureEnvironment(ctx, pa.ID, "staging")
	run, _, err := res.CreateRun(ctx, results.NewRun{ProjectID: string(pa.ID), EnvironmentID: string(envA.ID), LocalRunID: "0198c3e1-7b1a-7c3d-9e4f-0123456789ab", StartedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	svc := artifact.New(db, store, res)
	deps := artifact.Deps{DB: db, Tenancy: ten, Authz: az, Results: res, Artifact: svc, Metrics: reg, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	mux := http.NewServeMux()
	artifact.Routes(mux, deps)
	e := &env{t: t, db: db, store: store, svc: svc, jobs: artifact.NewJobs(deps), results: res, reg: reg, tokenA: issue(pa.ID), tokenB: issue(pb.ID), projectA: string(pa.ID), runA: run}
	e.srv = httptest.NewServer(httpx.RequestID(mux))
	t.Cleanup(e.srv.Close)
	return e
}

type resp struct {
	code int
	body map[string]any
	raw  string
}

func (e *env) post(token, path string, body any) resp {
	e.t.Helper()
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req, _ := http.NewRequest("POST", e.srv.URL+path, &buf)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	out := resp{code: res.StatusCode, raw: string(raw)}
	_ = json.Unmarshal(raw, &out.body)
	return out
}

func (r resp) errCode() string {
	if e, ok := r.body["error"].(map[string]any); ok {
		c, _ := e["code"].(string)
		return c
	}
	return ""
}

// put uploads data to a presigned URL the way a reporter would.
func (e *env) put(url string, headers map[string]any, data []byte) (int, string) {
	e.t.Helper()
	req, _ := http.NewRequest("PUT", url, bytes.NewReader(data))
	for k, v := range headers {
		req.Header.Set(k, v.(string))
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, res.Body)
	return res.StatusCode, res.Header.Get("ETag")
}

func sha(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func declareBody(local, client string, attempt int, name, kind, ct string, data []byte) map[string]any {
	return map[string]any{
		"localRunId": local, "clientTestId": client, "occurrenceIndex": 0, "attempt": attempt,
		"name": name, "kind": kind, "contentType": ct, "sizeBytes": len(data), "sha256": sha(data),
	}
}

func TestSinglePutFlow(t *testing.T) {
	e := setup(t)
	local := e.runA.LocalRunID
	png := bytes.Repeat([]byte("\x89PNG..."), 1000)

	// Declare → 201 with a presigned PUT; the same declare again → 200,
	// same artifact, fresh URL (§7.2 #4 idempotency).
	r := e.post(e.tokenA, "/v1/artifacts", declareBody(local, "ct_pay", 0, "failure.png", "screenshot", "image/png", png))
	if r.code != 201 || r.body["status"] != "pending" || r.body["upload"] == nil || r.body["linked"] != false {
		t.Fatalf("declare: %d %s", r.code, r.raw)
	}
	id, _ := r.body["artifactId"].(string)
	up := r.body["upload"].(map[string]any)
	if up["method"] != "PUT" || !strings.Contains(up["url"].(string), e.store.URL()) || up["headers"].(map[string]any)["Content-Type"] != "image/png" {
		t.Errorf("upload = %v", up)
	}
	if again := e.post(e.tokenA, "/v1/artifacts", declareBody(local, "ct_pay", 0, "failure.png", "screenshot", "image/png", png)); again.code != 200 || again.body["artifactId"] != id {
		t.Errorf("re-declare: %d %s", again.code, again.raw)
	}

	// Complete before upload: 409, nothing rejected.
	if c := e.post(e.tokenA, "/v1/artifacts/"+id+"/complete", nil); c.code != 409 || c.errCode() != "UPLOAD_INCOMPLETE" {
		t.Errorf("early complete: %d %s", c.code, c.raw)
	}
	// Wrong content type against the signed URL: the bucket refuses.
	if code, _ := e.put(up["url"].(string), map[string]any{"Content-Type": "text/html"}, png); code != 403 {
		t.Errorf("content-type not pinned: PUT with text/html got %d", code)
	}
	if code, _ := e.put(up["url"].(string), up["headers"].(map[string]any), png); code != 200 {
		t.Fatalf("upload: %d", code)
	}
	c := e.post(e.tokenA, "/v1/artifacts/"+id+"/complete", map[string]any{"sha256": sha(png)})
	if c.code != 200 || c.body["status"] != "verified" || c.body["sizeBytes"] != float64(len(png)) || c.body["linked"] != false {
		t.Fatalf("complete: %d %s", c.code, c.raw)
	}
	if c := e.post(e.tokenA, "/v1/artifacts/"+id+"/complete", nil); c.code != 200 || c.body["status"] != "verified" {
		t.Errorf("complete twice: %d %s", c.code, c.raw)
	}
	if d := e.post(e.tokenA, "/v1/artifacts", declareBody(local, "ct_pay", 0, "failure.png", "screenshot", "image/png", png)); d.code != 200 || d.body["status"] != "verified" || d.body["upload"] != nil {
		t.Errorf("declare after verified should not hand out an upload: %d %s", d.code, d.raw)
	}

	// Arrived before its event (§7.6 #3): unlinked now, linked once the
	// attempt exists and the sweep runs.
	ctx := context.Background()
	if _, overdue, err := e.svc.LinkPending(ctx); err != nil || overdue != 0 {
		t.Errorf("link before event: overdue=%d err=%v", overdue, err)
	}
	_, err := e.results.AppendAttempts(ctx, e.runA.ID, []results.AttemptEvent{{
		Sequence: 1, ClientTestID: "ct_pay", Attempt: 0, DisplayName: "pay", IdentityRaw: json.RawMessage(`{"framework":"playwright","filePath":"a.spec.ts","titlePath":["pay"]}`),
		Status: results.AttemptFailed, Outcome: results.OutcomeUnexpected,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.jobs.Sweep(ctx); err != nil {
		t.Fatal(err)
	}
	got, _ := e.svc.Get(ctx, e.projectA, id)
	if got.AttemptID == "" || got.LinkedAt.IsZero() || got.Status != artifact.StatusVerified {
		t.Errorf("not linked after the event arrived: %+v", got)
	}
	attemptID, _ := e.results.FindAttempt(ctx, e.runA.ID, "ct_pay", 0, 0)
	if got.AttemptID != attemptID {
		t.Errorf("linked to %s, want attempt %s", got.AttemptID, attemptID)
	}
	if d := time.Until(got.ExpiresAt); d < 13*24*time.Hour || d > 15*24*time.Hour {
		t.Errorf("free-plan screenshot retention should be 14 days, got %v", d)
	}

	// Cross-tenant: B cannot see A's run or artifact.
	if d := e.post(e.tokenB, "/v1/artifacts", declareBody(local, "ct_pay", 0, "x.png", "screenshot", "image/png", png)); d.code != 404 || d.errCode() != "RUN_NOT_FOUND" {
		t.Errorf("B declaring on A's run: %d %s", d.code, d.raw)
	}
	if c := e.post(e.tokenB, "/v1/artifacts/"+id+"/complete", nil); c.code != 404 {
		t.Errorf("B completing A's artifact: %d %s", c.code, c.raw)
	}

	m := e.reg.Expose()
	for _, want := range []string{`artifact_uploads_total{project="` + e.projectA + `",result="verified"} 1`, `artifact_bytes_total{project="` + e.projectA + `",kind="screenshot"} ` + strconv.Itoa(len(png))} {
		if !strings.Contains(m, want) {
			t.Errorf("metrics missing %q", want)
		}
	}
}

func TestMismatchesRejectAndDelete(t *testing.T) {
	e := setup(t)
	local := e.runA.LocalRunID
	data := bytes.Repeat([]byte("log line\n"), 100)

	// Declared size smaller than what is uploaded: SIZE_MISMATCH, object gone.
	body := declareBody(local, "ct_a", 1, "run.log", "log", "text/plain", data)
	body["sizeBytes"] = len(data) - 10
	r := e.post(e.tokenA, "/v1/artifacts", body)
	if r.code != 201 {
		t.Fatalf("declare: %d %s", r.code, r.raw)
	}
	id := r.body["artifactId"].(string)
	up := r.body["upload"].(map[string]any)
	e.put(up["url"].(string), up["headers"].(map[string]any), data)
	c := e.post(e.tokenA, "/v1/artifacts/"+id+"/complete", nil)
	if c.code != 422 || c.errCode() != "SIZE_MISMATCH" {
		t.Fatalf("size mismatch: %d %s", c.code, c.raw)
	}
	got, _ := e.svc.Get(context.Background(), e.projectA, id)
	if got.Status != artifact.StatusRejected || got.FailureReason != "SIZE_MISMATCH" || e.store.Exists(got.StorageKey) {
		t.Errorf("after size mismatch: %+v exists=%v", got, e.store.Exists(got.StorageKey))
	}
	// A rejected artifact can be declared again with corrected metadata.
	r = e.post(e.tokenA, "/v1/artifacts", declareBody(local, "ct_a", 1, "run.log", "log", "text/plain", data))
	if r.code != 200 || r.body["artifactId"] != id || r.body["status"] != "pending" || r.body["upload"] == nil {
		t.Fatalf("re-declare after reject: %d %s", r.code, r.raw)
	}

	// Right size, wrong hash: CHECKSUM_MISMATCH.
	body = declareBody(local, "ct_b", 0, "run.log", "log", "text/plain", data)
	body["sha256"] = strings.Repeat("0", 64)
	r = e.post(e.tokenA, "/v1/artifacts", body)
	id = r.body["artifactId"].(string)
	up = r.body["upload"].(map[string]any)
	e.put(up["url"].(string), up["headers"].(map[string]any), data)
	if c := e.post(e.tokenA, "/v1/artifacts/"+id+"/complete", nil); c.code != 422 || c.errCode() != "CHECKSUM_MISMATCH" {
		t.Errorf("checksum mismatch: %d %s", c.code, c.raw)
	}
	// Complete with a sha256 that differs from the declared one is a 400.
	r = e.post(e.tokenA, "/v1/artifacts", declareBody(local, "ct_c", 0, "run.log", "log", "text/plain", data))
	if c := e.post(e.tokenA, "/v1/artifacts/"+r.body["artifactId"].(string)+"/complete", map[string]any{"sha256": strings.Repeat("1", 64)}); c.code != 400 {
		t.Errorf("contradicting sha256: %d %s", c.code, c.raw)
	}

	if !strings.Contains(e.reg.Expose(), `result="rejected"} 2`) {
		t.Error("rejected count")
	}
}

func TestCapsAndValidation(t *testing.T) {
	e := setup(t)
	local := e.runA.LocalRunID
	// Video over 20 MB: 413 with the limit; the run is untouched.
	body := declareBody(local, "ct_v", 0, "run.webm", "video", "video/webm", []byte("x"))
	body["sizeBytes"] = 21 << 20
	r := e.post(e.tokenA, "/v1/artifacts", body)
	det, _ := r.body["error"].(map[string]any)["details"].(map[string]any)
	if r.code != 413 || r.errCode() != "ARTIFACT_TOO_LARGE" || det["limit"] != float64(20<<20) {
		t.Errorf("over cap: %d %s", r.code, r.raw)
	}
	if run, _ := e.results.GetRun(context.Background(), e.projectA, local); run.Status != results.RunRunning {
		t.Errorf("run affected by a rejected artifact: %+v", run)
	}
	for name, mutate := range map[string]func(map[string]any){
		"kind":        func(b map[string]any) { b["kind"] = "gif" },
		"contentType": func(b map[string]any) { b["contentType"] = "text/html" },
		"sha256":      func(b map[string]any) { b["sha256"] = "abc" },
		"name":        func(b map[string]any) { b["name"] = "../etc/passwd" },
		"attempt":     func(b map[string]any) { delete(b, "attempt") },
		"sizeBytes":   func(b map[string]any) { b["sizeBytes"] = 0 },
	} {
		b := declareBody(local, "ct_x", 0, "x.png", "screenshot", "image/png", []byte("x"))
		mutate(b)
		if r := e.post(e.tokenA, "/v1/artifacts", b); r.code != 400 {
			t.Errorf("bad %s: %d %s", name, r.code, r.raw)
		}
	}
	b := declareBody("00000000-0000-4000-8000-000000000000", "ct_x", 0, "x.png", "screenshot", "image/png", []byte("x"))
	if r := e.post(e.tokenA, "/v1/artifacts", b); r.code != 404 {
		t.Errorf("unknown run: %d", r.code)
	}
}

func TestMultipartWithResume(t *testing.T) {
	e := setup(t)
	local := e.runA.LocalRunID
	zip := bytes.Repeat([]byte("PK\x03\x04trace-data-"), (9<<20)/15+1) // ~9 MB → 2 parts
	r := e.post(e.tokenA, "/v1/artifacts", declareBody(local, "ct_t", 0, "trace.zip", "trace", "application/zip", zip))
	if r.code != 201 || r.body["multipart"] == nil || r.body["upload"] != nil {
		t.Fatalf("declare multipart: %d %s", r.code, r.raw)
	}
	id := r.body["artifactId"].(string)
	mp := r.body["multipart"].(map[string]any)
	parts := mp["parts"].([]any)
	if len(parts) != 2 || mp["partSize"] != float64(artifact.PartSize) || len(mp["received"].([]any)) != 0 {
		t.Fatalf("multipart = %v", mp)
	}
	// Upload part 1 only, then "crash".
	p1 := parts[0].(map[string]any)
	code, etag1 := e.put(p1["url"].(string), nil, zip[:artifact.PartSize])
	if code != 200 || etag1 == "" {
		t.Fatalf("part 1: %d %q", code, etag1)
	}
	// Resume: re-declare reports part 1 received and hands out only part 2.
	r = e.post(e.tokenA, "/v1/artifacts", declareBody(local, "ct_t", 0, "trace.zip", "trace", "application/zip", zip))
	mp = r.body["multipart"].(map[string]any)
	if r.code != 200 || mp["uploadId"] == "" || len(mp["received"].([]any)) != 1 || len(mp["parts"].([]any)) != 1 {
		t.Fatalf("resume: %d %s", r.code, r.raw)
	}
	p2 := mp["parts"].([]any)[0].(map[string]any)
	if p2["number"] != float64(2) {
		t.Fatalf("resume should hand out part 2, got %v", p2)
	}
	_, etag2 := e.put(p2["url"].(string), nil, zip[artifact.PartSize:])
	// Complete with a wrong ETag list is refused as incomplete (nothing assembled).
	if c := e.post(e.tokenA, "/v1/artifacts/"+id+"/complete", map[string]any{"parts": []map[string]any{{"partNumber": 1, "etag": etag1}, {"partNumber": 2, "etag": `"bogus"`}}}); c.code != 409 {
		t.Errorf("bad etags: %d %s", c.code, c.raw)
	}
	c := e.post(e.tokenA, "/v1/artifacts/"+id+"/complete", map[string]any{"sha256": sha(zip), "parts": []map[string]any{{"partNumber": 2, "etag": etag2}, {"partNumber": 1, "etag": etag1}}})
	if c.code != 200 || c.body["status"] != "verified" || c.body["sizeBytes"] != float64(len(zip)) {
		t.Fatalf("complete multipart: %d %s", c.code, c.raw)
	}
	if e.store.PendingUploads() != 0 {
		t.Error("multipart upload left open after completion")
	}
	// A second complete after success is a no-op 200 even without parts.
	if c := e.post(e.tokenA, "/v1/artifacts/"+id+"/complete", nil); c.code != 200 {
		t.Errorf("complete again: %d %s", c.code, c.raw)
	}
}

func TestStoreOutageIsRetryable(t *testing.T) {
	e := setup(t)
	local := e.runA.LocalRunID
	data := []byte("hello")
	r := e.post(e.tokenA, "/v1/artifacts", declareBody(local, "ct_s", 0, "a.txt", "log", "text/plain", data))
	id := r.body["artifactId"].(string)
	up := r.body["upload"].(map[string]any)
	e.put(up["url"].(string), up["headers"].(map[string]any), data)

	e.store.Fail = errors.New("timeout")
	c := e.post(e.tokenA, "/v1/artifacts/"+id+"/complete", nil)
	if c.code != 503 || c.errCode() != "STORE_UNAVAILABLE" {
		t.Fatalf("store down: %d %s", c.code, c.raw)
	}
	got, _ := e.svc.Get(context.Background(), e.projectA, id)
	if got.Status != artifact.StatusPending {
		t.Errorf("artifact after outage: %s, want pending (retryable, §14.3)", got.Status)
	}
	e.store.Fail = nil
	if c := e.post(e.tokenA, "/v1/artifacts/"+id+"/complete", nil); c.code != 200 || c.body["status"] != "verified" {
		t.Errorf("retry after outage: %d %s", c.code, c.raw)
	}
}

func TestSweepsExpireAndAbort(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	local := e.runA.LocalRunID
	data := []byte("screenshot")

	// Verified artifact whose retention passed → object deleted, expired.
	r := e.post(e.tokenA, "/v1/artifacts", declareBody(local, "ct_1", 0, "a.png", "screenshot", "image/png", data))
	id := r.body["artifactId"].(string)
	up := r.body["upload"].(map[string]any)
	e.put(up["url"].(string), up["headers"].(map[string]any), data)
	if c := e.post(e.tokenA, "/v1/artifacts/"+id+"/complete", nil); c.code != 200 {
		t.Fatal(c.raw)
	}
	got, _ := e.svc.Get(ctx, e.projectA, id)
	e.backdate(id, "expires_at", -15*24*time.Hour)
	// Pending multipart older than 24 h → aborted, expired ABANDONED.
	big := declareBody(local, "ct_2", 0, "t.zip", "trace", "application/zip", []byte("x"))
	big["sizeBytes"] = 9 << 20
	r2 := e.post(e.tokenA, "/v1/artifacts", big)
	if r2.code != 201 {
		t.Fatal(r2.raw)
	}
	id2 := r2.body["artifactId"].(string)
	e.backdate(id2, "created_at", -25*time.Hour)
	if e.store.PendingUploads() != 1 {
		t.Fatalf("pending uploads = %d", e.store.PendingUploads())
	}

	if err := e.jobs.Sweep(ctx); err != nil {
		t.Fatal(err)
	}
	if a, _ := e.svc.Get(ctx, e.projectA, id); a.Status != artifact.StatusExpired || e.store.Exists(got.StorageKey) {
		t.Errorf("retention: %+v exists=%v", a, e.store.Exists(got.StorageKey))
	}
	if a, _ := e.svc.Get(ctx, e.projectA, id2); a.Status != artifact.StatusExpired || a.FailureReason != "ABANDONED" || e.store.PendingUploads() != 0 {
		t.Errorf("abandoned: %+v pending=%d", a, e.store.PendingUploads())
	}
	// Declaring under a gone artifact's name is refused; complete too.
	if d := e.post(e.tokenA, "/v1/artifacts", declareBody(local, "ct_1", 0, "a.png", "screenshot", "image/png", data)); d.code != 410 {
		t.Errorf("declare gone: %d %s", d.code, d.raw)
	}
	m := e.reg.Expose()
	for _, want := range []string{`artifact_sweeps_total{action="expired"} 1`, `artifact_sweeps_total{action="abandoned"} 1`, "artifacts_unlinked_overdue 0"} {
		if !strings.Contains(m, want) {
			t.Errorf("metrics missing %q", want)
		}
	}
}

// backdate moves a timestamp column so a sweep sees the row as old.
func (e *env) backdate(id, column string, by time.Duration) {
	e.t.Helper()
	if _, err := e.db.Q(context.Background()).Exec(context.Background(), `UPDATE artifacts SET `+column+` = `+column+` + $2::interval WHERE id = $1`, id, pgInterval(by)); err != nil {
		e.t.Fatal(err)
	}
}

func pgInterval(d time.Duration) string { return strconv.Itoa(int(d.Seconds())) + " seconds" }

// pngBytes is a minimal PNG header — enough for http.DetectContentType to
// call it image/png, which is the sniff PutEvidence relies on (§11.5).
var pngBytes = append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{0}, 64)...)

// §6.8.2 "Artifact dùng lại": manual evidence is an Artifact of a new kind,
// written by the api itself — no run, no attempt, never declared, never
// linked, invisible to the unlinked sweep, expired by retention like any other.
func TestManualEvidence(t *testing.T) {
	e := setup(t)
	ctx := context.Background()
	const execID = "0198c3e1-7b1a-7c3d-9e4f-00000000e0e0"
	in := artifact.Evidence{ProjectID: e.projectA, ExecutionID: execID, Plan: "team", Name: "checkout.png", ContentType: "image/png"}

	a, err := e.svc.PutEvidence(ctx, in, pngBytes)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(pngBytes)
	if a.Kind != artifact.KindManualEvidence || a.Status != artifact.StatusVerified || a.ExecutionID != execID || a.RunID != "" || a.AttemptID != "" ||
		a.ActualBytes != int64(len(pngBytes)) || a.SHA256 != hex.EncodeToString(sum[:]) || !e.store.Exists(a.StorageKey) {
		t.Errorf("evidence: %+v exists=%v", a, e.store.Exists(a.StorageKey))
	}
	if days := int(a.ExpiresAt.Sub(a.CreatedAt).Round(time.Hour).Hours() / 24); days != 365 {
		t.Errorf("team retention = %d days, want 365", days)
	}
	if got, err := e.svc.Get(ctx, e.projectA, a.ID); err != nil || got.ExecutionID != execID {
		t.Errorf("Get: %+v %v", got, err)
	}
	// Wrong content: the declared type must be what the bytes are.
	if _, err := e.svc.PutEvidence(ctx, in, []byte("<html><script>alert(1)</script></html>")); !errors.Is(err, artifact.ErrInvalid) {
		t.Errorf("html as png: %v", err)
	}
	txt := in
	txt.Name, txt.ContentType = "notes.txt", "text/plain"
	if _, err := e.svc.PutEvidence(ctx, txt, []byte("<html><body>x</body></html>")); !errors.Is(err, artifact.ErrInvalid) {
		t.Errorf("html as text: %v", err)
	}
	if _, err := e.svc.PutEvidence(ctx, txt, []byte("step 3: the total was wrong\n")); err != nil {
		t.Errorf("plain text: %v", err)
	}
	// Policy: a reporter kind's type is not evidence; a reporter cannot declare the kind.
	bad := in
	bad.ContentType = "application/zip"
	if _, err := e.svc.PutEvidence(ctx, bad, pngBytes); !errors.Is(err, artifact.ErrInvalid) {
		t.Errorf("zip evidence: %v", err)
	}
	if artifact.ValidKind(artifact.KindManualEvidence) {
		t.Error("manual_evidence must not be declarable by a reporter")
	}
	if r := e.post(e.tokenA, "/v1/artifacts", declareBody(e.runA.LocalRunID, "ct_m", 0, "m.png", "manual_evidence", "image/png", pngBytes)); r.code != 400 {
		t.Errorf("declare manual_evidence: %d %s", r.code, r.raw)
	}
	// Cap: one byte over is refused before the store is touched.
	var tooLarge *artifact.TooLargeError
	if _, err := e.svc.PutEvidence(ctx, in, append(append([]byte(nil), pngBytes...), bytes.Repeat([]byte{0}, 20<<20)...)); !errors.As(err, &tooLarge) || tooLarge.Limit != 20<<20 {
		t.Errorf("over cap: %v", err)
	}
	// Store outage: nothing is left behind, the error is retryable.
	e.store.Fail = errors.New("timeout")
	if _, err := e.svc.PutEvidence(ctx, in, pngBytes); !errors.Is(err, artifact.ErrStore) {
		t.Errorf("store down: %v", err)
	}
	e.store.Fail = nil
	var n int
	_ = e.db.Q(ctx).QueryRow(ctx, `SELECT count(*) FROM artifacts WHERE manual_execution_id = $1`, execID).Scan(&n)
	if n != 2 {
		t.Errorf("rows for the execution = %d, want 2 (png + txt)", n)
	}

	// Listing: keyed by execution, verified only, oldest first; a foreign
	// project sees nothing.
	byExec, err := e.svc.ListEvidence(ctx, e.projectA, []string{execID, "0198c3e1-7b1a-7c3d-9e4f-00000000e0e1"})
	if err != nil || len(byExec) != 1 || len(byExec[execID]) != 2 || byExec[execID][0].Name != "checkout.png" || byExec[execID][1].Name != "notes.txt" {
		t.Errorf("ListEvidence: %+v %v", byExec, err)
	}
	if other, _ := e.svc.ListEvidence(ctx, "00000000-0000-4000-8000-000000000000", []string{execID}); len(other) != 0 {
		t.Errorf("foreign project lists evidence: %+v", other)
	}
	// Sweeps: never "unlinked" (§6.4 would page for it otherwise); expired by retention.
	e.backdate(a.ID, "created_at", -2*time.Hour)
	if linked, overdue, err := e.svc.LinkPending(ctx); err != nil || linked != 0 || overdue != 0 {
		t.Errorf("LinkPending saw evidence: linked=%d overdue=%d err=%v", linked, overdue, err)
	}
	e.backdate(a.ID, "expires_at", -400*24*time.Hour)
	if n, err := e.svc.ExpireRetention(ctx); err != nil || n != 1 {
		t.Errorf("ExpireRetention: %d %v", n, err)
	}
	if got, _ := e.svc.Get(ctx, e.projectA, a.ID); got.Status != artifact.StatusExpired || e.store.Exists(a.StorageKey) {
		t.Errorf("after retention: %+v exists=%v", got, e.store.Exists(a.StorageKey))
	}
	if byExec, _ := e.svc.ListEvidence(ctx, e.projectA, []string{execID}); len(byExec[execID]) != 1 {
		t.Errorf("expired evidence still listed: %+v", byExec)
	}
}
