package s3

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"testing"
	"time"

	"github.com/vinh/automation-testing-platform/backend/internal/platform/objstore"
)

// liveStore connects to a real S3-compatible server named by S3_TEST_*
// (SeaweedFS or MinIO locally, R2/S3 on staging). The signer tests above
// prove the maths against AWS's examples; this proves a real server
// accepts what we send, verb by verb. Skipped when S3_TEST_ENDPOINT is
// unset.
func liveStore(t *testing.T) *Store {
	t.Helper()
	endpoint := os.Getenv("S3_TEST_ENDPOINT")
	if endpoint == "" {
		t.Skip("S3_TEST_ENDPOINT not set (see backend/README.md, \"S3 conformance\")")
	}
	getenv := func(k, def string) string {
		if v := os.Getenv(k); v != "" {
			return v
		}
		return def
	}
	s, err := New(Config{
		Endpoint:  endpoint,
		Region:    getenv("S3_TEST_REGION", "us-east-1"),
		Bucket:    getenv("S3_TEST_BUCKET", "atp-conformance"),
		AccessKey: os.Getenv("S3_TEST_ACCESS_KEY"),
		SecretKey: os.Getenv("S3_TEST_SECRET_KEY"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := EnsureBucketReady(s, 60*time.Second); err != nil {
		t.Fatalf("create bucket: %v", err)
	}
	return s
}

func randomBytes(t *testing.T, n int) []byte {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return b
}

func httpPut(t *testing.T, url string, headers map[string]string, body []byte) (int, http.Header) {
	t.Helper()
	req, err := http.NewRequest("PUT", url, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, res.Body)
	return res.StatusCode, res.Header
}

func TestConformancePresignedPut(t *testing.T) {
	s := liveStore(t)
	ctx := context.Background()
	key := fmt.Sprintf("conformance/%d/shot one.png", time.Now().UnixNano())
	data := randomBytes(t, 4096)

	url, err := s.PresignPut(ctx, key, "image/png", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	// The content type is signed: a client that sends another one is refused.
	if code, _ := httpPut(t, url, map[string]string{"Content-Type": "text/html"}, data); code != http.StatusForbidden {
		t.Errorf("presigned PUT with a different content type: HTTP %d, want 403", code)
	}
	if code, _ := httpPut(t, url, map[string]string{"Content-Type": "image/png"}, data); code != http.StatusOK {
		t.Fatalf("presigned PUT: HTTP %d", code)
	}

	info, err := s.Head(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size != int64(len(data)) || info.ContentType != "image/png" {
		t.Errorf("head = %+v", info)
	}
	body, err := s.Get(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(body)
	body.Close()
	if !bytes.Equal(got, data) {
		t.Error("get returned other bytes")
	}

	if err := s.Delete(ctx, key); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Head(ctx, key); !errors.Is(err, objstore.ErrNotFound) {
		t.Errorf("head after delete: %v", err)
	}
	if _, err := s.Get(ctx, key); !errors.Is(err, objstore.ErrNotFound) {
		t.Errorf("get after delete: %v", err)
	}
}

// An expired presigned URL is refused — the ttl is honoured by the server,
// not just written into the query.
func TestConformancePresignExpires(t *testing.T) {
	s := liveStore(t)
	key := fmt.Sprintf("conformance/%d/expired.txt", time.Now().UnixNano())
	past := *s
	past.now = func() time.Time { return time.Now().Add(-2 * time.Minute) }
	url, _ := past.PresignPut(context.Background(), key, "text/plain", time.Minute)
	if code, _ := httpPut(t, url, map[string]string{"Content-Type": "text/plain"}, []byte("late")); code != http.StatusForbidden {
		t.Errorf("expired URL: HTTP %d, want 403", code)
	}
}

func TestConformanceMultipart(t *testing.T) {
	s := liveStore(t)
	ctx := context.Background()
	key := fmt.Sprintf("conformance/%d/trace.zip", time.Now().UnixNano())
	// S3's minimum part is 5 MiB except the last one.
	part1 := randomBytes(t, 5<<20)
	part2 := randomBytes(t, 1234)

	id, err := s.CreateMultipart(ctx, key, "application/zip")
	if err != nil {
		t.Fatal(err)
	}
	// Parts out of order, as a resumed upload may send them.
	for _, p := range []struct {
		n    int
		data []byte
	}{{2, part2}, {1, part1}} {
		url, _ := s.PresignPart(ctx, key, id, p.n, time.Minute)
		if code, _ := httpPut(t, url, nil, p.data); code != http.StatusOK {
			t.Fatalf("part %d: HTTP %d", p.n, code)
		}
	}
	parts, err := s.ListParts(ctx, key, id)
	if err != nil {
		t.Fatal(err)
	}
	if len(parts) != 2 || parts[0].Number != 1 || parts[0].Size != int64(len(part1)) || parts[1].Size != int64(len(part2)) || parts[0].ETag == "" {
		t.Fatalf("list parts = %+v", parts)
	}
	// Before completion the object does not exist — verify relies on this.
	if _, err := s.Head(ctx, key); !errors.Is(err, objstore.ErrNotFound) {
		t.Errorf("head before complete: %v", err)
	}
	// Complete sorts, so the order given does not matter.
	if err := s.CompleteMultipart(ctx, key, id, []objstore.Part{parts[1], parts[0]}); err != nil {
		t.Fatal(err)
	}
	info, err := s.Head(ctx, key)
	if err != nil || info.Size != int64(len(part1)+len(part2)) {
		t.Fatalf("head after complete: %+v %v", info, err)
	}
	body, err := s.Get(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(body)
	body.Close()
	if !bytes.Equal(got, append(append([]byte(nil), part1...), part2...)) {
		t.Error("assembled object has other bytes")
	}
	// The upload is gone once completed: ListParts on it is NotFound, which
	// verify reads as "retried complete" when the object exists.
	if _, err := s.ListParts(ctx, key, id); !errors.Is(err, objstore.ErrNotFound) {
		t.Errorf("list parts after complete: %v", err)
	}
	_ = s.Delete(ctx, key)
}

func TestConformanceAbortAndPut(t *testing.T) {
	s := liveStore(t)
	ctx := context.Background()
	key := fmt.Sprintf("conformance/%d/abandoned.webm", time.Now().UnixNano())

	id, err := s.CreateMultipart(ctx, key, "video/webm")
	if err != nil {
		t.Fatal(err)
	}
	url, _ := s.PresignPart(ctx, key, id, 1, time.Minute)
	if code, _ := httpPut(t, url, nil, randomBytes(t, 1024)); code != http.StatusOK {
		t.Fatalf("part: HTTP %d", code)
	}
	if err := s.AbortMultipart(ctx, key, id); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ListParts(ctx, key, id); !errors.Is(err, objstore.ErrNotFound) {
		t.Errorf("list parts after abort: %v", err)
	}

	// Put: the api's own write path (manual evidence).
	evidence := fmt.Sprintf("conformance/%d/evidence.txt", time.Now().UnixNano())
	if err := s.Put(ctx, evidence, "text/plain", []byte("seen on staging")); err != nil {
		t.Fatal(err)
	}
	info, err := s.Head(ctx, evidence)
	if err != nil || info.Size != 15 || info.ContentType != "text/plain" {
		t.Errorf("head after put: %+v %v", info, err)
	}
	_ = s.Delete(ctx, evidence)
	// Deleting what is not there is not an error to callers that ignore
	// ErrNotFound; S3 answers 204 either way.
	if err := s.Delete(ctx, evidence); err != nil && !errors.Is(err, objstore.ErrNotFound) {
		t.Errorf("delete twice: %v", err)
	}
}

// A store that accepts the connection and never answers (§14.3 "S3/R2
// timeout") must fail the call within the header timeout rather than hold
// it for the whole body timeout. No server needed.
func TestHungStoreFailsWithinHeaderTimeout(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	var held []net.Conn
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			held = append(held, c) // read nothing, write nothing
		}
	}()
	defer func() {
		for _, c := range held {
			c.Close()
		}
	}()

	s, err := New(Config{Endpoint: "http://" + l.Addr().String(), Region: "us-east-1", Bucket: "b", AccessKey: "k", SecretKey: "s"})
	if err != nil {
		t.Fatal(err)
	}
	s.client = newClient(300 * time.Millisecond)
	for name, call := range map[string]func() error{
		"head":   func() error { _, err := s.Head(context.Background(), "k"); return err },
		"get":    func() error { _, err := s.Get(context.Background(), "k"); return err },
		"delete": func() error { return s.Delete(context.Background(), "k") },
		"list":   func() error { _, err := s.ListParts(context.Background(), "k", "u"); return err },
	} {
		start := time.Now()
		err := call()
		if took := time.Since(start); err == nil || took > 2*time.Second {
			t.Errorf("%s on a hung store: err=%v after %s, want an error within ~300 ms", name, err, took)
		}
	}
	// Three store calls in a row (HEAD, HEAD, GET on complete) must still
	// answer before the reporter's 30 s timeout (§7.5).
	if 3*HeaderTimeout >= 30*time.Second {
		t.Errorf("HeaderTimeout %s: three calls exceed the reporter's 30 s", HeaderTimeout)
	}
}
