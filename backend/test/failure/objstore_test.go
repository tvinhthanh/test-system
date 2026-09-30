package failure

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net"
	"net/http"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/vinh/automation-testing-platform/backend/internal/platform/objstore/s3"
	"github.com/vinh/automation-testing-platform/backend/test/harness"
)

// storeProxy sits between the binaries and a real S3-compatible server.
// Hang() is the §14.3 "S3/R2 timeout": connections are accepted and then
// nothing comes back — the worst kind of outage, since nothing fails fast.
type storeProxy struct {
	l      net.Listener
	target string
	mu     sync.Mutex
	hang   bool
	conns  []net.Conn
}

func newStoreProxy(t *testing.T, target string) *storeProxy {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	p := &storeProxy{l: l, target: target}
	t.Cleanup(func() { l.Close(); p.closeAll() })
	go p.serve()
	return p
}

func (p *storeProxy) URL() string { return "http://" + p.l.Addr().String() }

func (p *storeProxy) serve() {
	for {
		c, err := p.l.Accept()
		if err != nil {
			return
		}
		p.mu.Lock()
		p.conns = append(p.conns, c)
		hang := p.hang
		p.mu.Unlock()
		if hang {
			continue // hold it open, say nothing
		}
		go func() {
			up, err := net.Dial("tcp", p.target)
			if err != nil {
				c.Close()
				return
			}
			p.mu.Lock()
			p.conns = append(p.conns, up)
			p.mu.Unlock()
			go func() { _, _ = io.Copy(up, c); up.Close() }()
			_, _ = io.Copy(c, up)
			c.Close()
		}()
	}
}

// Hang drops every open connection (so keep-alive ones must redial) and
// makes new ones hang.
func (p *storeProxy) Hang() {
	p.mu.Lock()
	p.hang = true
	p.mu.Unlock()
	p.closeAll()
}

func (p *storeProxy) Recover() {
	p.closeAll()
	p.mu.Lock()
	p.hang = false
	p.mu.Unlock()
}

func (p *storeProxy) closeAll() {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, c := range p.conns {
		c.Close()
	}
	p.conns = nil
}

// §14.3 "S3/R2 timeout khi upload": the artifact stays retryable, the run
// stays valid, and the api answers before the reporter's 30 s timeout
// (§7.5) instead of holding the request. Real api + worker, a real
// S3-compatible server behind a proxy that hangs. Needs S3_TEST_* (see
// backend/README.md "S3 conformance"); skipped otherwise.
func TestObjectStoreHangKeepsRunValid(t *testing.T) {
	endpoint := os.Getenv("S3_TEST_ENDPOINT")
	if endpoint == "" {
		t.Skip("S3_TEST_ENDPOINT not set")
	}
	target := endpoint[len("http://"):]
	proxy := newStoreProxy(t, target)
	bucket := os.Getenv("S3_TEST_BUCKET")
	if bucket == "" {
		bucket = "atp-conformance"
	}
	region := os.Getenv("S3_TEST_REGION")
	if region == "" {
		region = "us-east-1"
	}

	store, err := s3.New(s3.Config{Endpoint: endpoint, Region: region, Bucket: bucket,
		AccessKey: os.Getenv("S3_TEST_ACCESS_KEY"), SecretKey: os.Getenv("S3_TEST_SECRET_KEY")})
	if err != nil {
		t.Fatal(err)
	}
	if err := s3.EnsureBucketReady(store, 60*time.Second); err != nil {
		t.Fatalf("create bucket: %v", err)
	}

	h := harness.New(t)
	h.ExtraEnv = append(h.ExtraEnv,
		"OBJSTORE=s3",
		"OBJSTORE_ENDPOINT="+proxy.URL(),
		"OBJSTORE_REGION="+region,
		"OBJSTORE_BUCKET="+bucket,
		"OBJSTORE_ACCESS_KEY="+os.Getenv("S3_TEST_ACCESS_KEY"),
		"OBJSTORE_SECRET_KEY="+os.Getenv("S3_TEST_SECRET_KEY"),
	)
	h.StartAPI()
	h.StartWorker()

	local := harness.LocalRun(1)
	if r := h.CreateRun(local); r.Code != 201 {
		t.Fatalf("create run: %d %v %v", r.Code, r.Body, r.Err)
	}
	ev := harness.EventWith(1, "tests/pay.spec.ts", "declines", "failed", "unexpected")
	if r := h.Post("/v1/runs/"+local+"/events", map[string]any{"events": []any{ev}}); r.Code != 200 {
		t.Fatalf("events: %d %v", r.Code, r.Body)
	}

	shot := bytes.Repeat([]byte("\x89PNG-failure-"), 2000)
	sum := sha256.Sum256(shot)
	declare := map[string]any{
		"localRunId": local, "clientTestId": ev["clientTestId"], "occurrenceIndex": 0, "attempt": 0,
		"name": "failure.png", "kind": "screenshot", "contentType": "image/png",
		"sizeBytes": len(shot), "sha256": hex.EncodeToString(sum[:]),
	}
	r := h.Post("/v1/artifacts", declare)
	if r.Code != 201 {
		t.Fatalf("declare: %d %v", r.Code, r.Body)
	}
	id := r.Body["artifactId"].(string)
	up := r.Body["upload"].(map[string]any)
	req, _ := http.NewRequest("PUT", up["url"].(string), bytes.NewReader(shot))
	for k, v := range up["headers"].(map[string]any) {
		req.Header.Set(k, v.(string))
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil || res.StatusCode != 200 {
		t.Fatalf("PUT to the store: %v %v", err, res)
	}
	res.Body.Close()

	// The store hangs. Complete must fail fast and retryably.
	proxy.Hang()
	c := h.Post("/v1/artifacts/"+id+"/complete", map[string]any{})
	if c.Code != 503 || c.Body["error"].(map[string]any)["code"] != "STORE_UNAVAILABLE" {
		t.Fatalf("complete while the store hangs: %d %v %v", c.Code, c.Body, c.Err)
	}
	if c.Elapsed >= 30*time.Second {
		t.Errorf("complete answered after %s — past the reporter's 30 s timeout", c.Elapsed)
	}
	t.Logf("complete during the hang answered 503 after %s", c.Elapsed.Round(time.Millisecond))
	if st := h.Count(`SELECT count(*) FROM artifacts WHERE id = $1 AND status = 'pending'`, id); st != 1 {
		t.Errorf("artifact after the outage is not pending (retryable)")
	}

	// The run does not depend on the store (§10.4): it finishes and is
	// processed while S3 is still down.
	if r := h.Post("/v1/runs/"+local+"/finish", map[string]any{}); r.Code != 200 {
		t.Fatalf("finish while the store hangs: %d %v", r.Code, r.Body)
	}
	h.WaitFor("run processed while the store hangs", 30*time.Second, func() bool {
		return h.Count(`SELECT count(*) FROM runs WHERE local_run_id = $1 AND status = 'failed'`, local) == 1 &&
			h.Count(`SELECT count(*) FROM test_results tr JOIN runs r ON r.id = tr.run_id WHERE r.local_run_id = $1`, local) == 1
	})

	// The store comes back; the reporter's retry of complete succeeds and
	// the artifact is linked to its failure.
	proxy.Recover()
	c = h.Post("/v1/artifacts/"+id+"/complete", map[string]any{})
	if c.Code != 200 || c.Body["status"] != "verified" {
		t.Fatalf("complete after recovery: %d %v", c.Code, c.Body)
	}
	h.WaitFor("artifact linked after recovery", 30*time.Second, func() bool {
		return h.Count(`SELECT count(*) FROM artifacts WHERE id = $1 AND status = 'verified' AND test_result_attempt_id IS NOT NULL`, id) == 1
	})
}
