// Package memory is an in-process objstore.Store for tests: objects live in
// a map, and presigned URLs point at an httptest server that accepts the
// same PUTs a real bucket would — with the content-type pin and the expiry
// enforced — so the artifact flow (declare → upload → complete → verify)
// runs end to end without credentials.
package memory

import (
	"context"
	"crypto/md5"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/vinh/automation-testing-platform/backend/internal/platform/config"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/objstore"
)

type object struct {
	data        []byte
	contentType string
	etag        string
}

type upload struct {
	key         string
	contentType string
	parts       map[int][]byte
}

type grant struct {
	key, uploadID, contentType string
	part                       int
	expires                    time.Time
}

// Store is the fake. Close the server with Close.
type Store struct {
	mu      sync.Mutex
	objects map[string]*object
	uploads map[string]*upload
	grants  map[string]grant
	srv     *httptest.Server
	// Now is the clock presign expiry is checked against; tests move it.
	Now func() time.Time
	// Fail makes the next Head/Get/Delete return this error (S3 timeout
	// injection, §14.3).
	Fail error
}

// New starts the server.
func New() *Store {
	s := &Store{objects: map[string]*object{}, uploads: map[string]*upload{}, grants: map[string]grant{}, Now: time.Now}
	s.srv = httptest.NewServer(http.HandlerFunc(s.serve))
	return s
}

// Close stops the server.
func (s *Store) Close() { s.srv.Close() }

// URL is the server's base URL.
func (s *Store) URL() string { return s.srv.URL }

func token() string {
	var b [12]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

func (s *Store) grantURL(g grant) string {
	tok := token()
	s.mu.Lock()
	s.grants[tok] = g
	s.mu.Unlock()
	return s.srv.URL + "/upload?token=" + tok
}

func (s *Store) PresignPut(_ context.Context, key, contentType string, ttl time.Duration) (string, error) {
	return s.grantURL(grant{key: key, contentType: contentType, expires: s.Now().Add(ttl)}), nil
}

func (s *Store) CreateMultipart(_ context.Context, key, contentType string) (string, error) {
	id := token()
	s.mu.Lock()
	s.uploads[id] = &upload{key: key, contentType: contentType, parts: map[int][]byte{}}
	s.mu.Unlock()
	return id, nil
}

func (s *Store) PresignPart(_ context.Context, key, uploadID string, partNumber int, ttl time.Duration) (string, error) {
	s.mu.Lock()
	_, ok := s.uploads[uploadID]
	s.mu.Unlock()
	if !ok {
		return "", objstore.ErrNotFound
	}
	return s.grantURL(grant{key: key, uploadID: uploadID, part: partNumber, expires: s.Now().Add(ttl)}), nil
}

func (s *Store) ListParts(_ context.Context, _ string, uploadID string) ([]objstore.Part, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.uploads[uploadID]
	if !ok {
		return nil, objstore.ErrNotFound
	}
	var out []objstore.Part
	for n, data := range u.parts {
		out = append(out, objstore.Part{Number: n, ETag: etag(data), Size: int64(len(data))})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Number < out[j].Number })
	return out, nil
}

func (s *Store) CompleteMultipart(_ context.Context, key, uploadID string, parts []objstore.Part) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.uploads[uploadID]
	if !ok || u.key != key {
		return objstore.ErrNotFound
	}
	sorted := append([]objstore.Part(nil), parts...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Number < sorted[j].Number })
	var data []byte
	for _, p := range sorted {
		d, ok := u.parts[p.Number]
		if !ok {
			return fmt.Errorf("memory: InvalidPart: part %d not uploaded", p.Number)
		}
		if p.ETag != etag(d) {
			return fmt.Errorf("memory: InvalidPart: etag mismatch on part %d", p.Number)
		}
		data = append(data, d...)
	}
	s.objects[key] = &object{data: data, contentType: u.contentType, etag: etag(data) + "-" + strconv.Itoa(len(sorted))}
	delete(s.uploads, uploadID)
	return nil
}

func (s *Store) AbortMultipart(_ context.Context, _ string, uploadID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.uploads[uploadID]; !ok {
		return objstore.ErrNotFound
	}
	delete(s.uploads, uploadID)
	return nil
}

func (s *Store) Head(_ context.Context, key string) (objstore.Info, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Fail != nil {
		return objstore.Info{}, s.Fail
	}
	o, ok := s.objects[key]
	if !ok {
		return objstore.Info{}, objstore.ErrNotFound
	}
	return objstore.Info{Size: int64(len(o.data)), ContentType: o.contentType, ETag: o.etag}, nil
}

func (s *Store) Get(_ context.Context, key string) (io.ReadCloser, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Fail != nil {
		return nil, s.Fail
	}
	o, ok := s.objects[key]
	if !ok {
		return nil, objstore.ErrNotFound
	}
	return io.NopCloser(strings.NewReader(string(o.data))), nil
}

func (s *Store) Delete(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Fail != nil {
		return s.Fail
	}
	delete(s.objects, key)
	return nil
}

// Exists reports whether key is stored — for assertions.
func (s *Store) Exists(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.objects[key]
	return ok
}

// PendingUploads counts multipart uploads never completed or aborted.
func (s *Store) PendingUploads() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.uploads)
}

// serve is the "bucket": PUT to a granted URL stores the object or part.
func (s *Store) serve(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut || r.URL.Path != "/upload" {
		http.Error(w, "MethodNotAllowed", http.StatusMethodNotAllowed)
		return
	}
	s.mu.Lock()
	g, ok := s.grants[r.URL.Query().Get("token")]
	s.mu.Unlock()
	if !ok {
		http.Error(w, "SignatureDoesNotMatch", http.StatusForbidden)
		return
	}
	if s.Now().After(g.expires) {
		http.Error(w, "AccessDenied: Request has expired", http.StatusForbidden)
		return
	}
	if g.uploadID == "" && r.Header.Get("Content-Type") != g.contentType {
		// The content type is part of the signature: a different one is a
		// different request and does not verify.
		http.Error(w, "SignatureDoesNotMatch: content-type", http.StatusForbidden)
		return
	}
	data, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "IncompleteBody", http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if g.uploadID != "" {
		u, ok := s.uploads[g.uploadID]
		if !ok {
			http.Error(w, "NoSuchUpload", http.StatusNotFound)
			return
		}
		u.parts[g.part] = data
		w.Header().Set("ETag", etag(data))
		w.WriteHeader(http.StatusOK)
		return
	}
	s.objects[g.key] = &object{data: data, contentType: g.contentType, etag: etag(data)}
	w.Header().Set("ETag", etag(data))
	w.WriteHeader(http.StatusOK)
}

func etag(data []byte) string {
	sum := md5.Sum(data)
	return `"` + hex.EncodeToString(sum[:]) + `"`
}

func init() {
	objstore.Register("memory", func(config.Config) (objstore.Store, error) { return New(), nil })
}
