// Package s3 implements objstore.Store against any S3-compatible endpoint
// (AWS S3, Cloudflare R2) with AWS Signature Version 4 over net/http. No
// SDK: the surface §7.7 needs is six verbs, and the signing algorithm is
// two hundred lines that never change — which is less to audit than an SDK
// and its transitive tree. The signer is checked against the worked
// examples in the AWS documentation.
//
// Requests are path-style ({endpoint}/{bucket}/{key}); both S3 and R2
// accept that form.
package s3

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/vinh/automation-testing-platform/backend/internal/platform/config"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/objstore"
)

// Config selects the bucket and credentials. Region is "auto" for R2.
type Config struct {
	Endpoint  string // https://s3.us-east-1.amazonaws.com or https://<account>.r2.cloudflarestorage.com
	Region    string
	Bucket    string
	AccessKey string
	SecretKey string
}

// Store is the S3 client.
type Store struct {
	cfg    Config
	base   *url.URL
	client *http.Client
	now    func() time.Time
}

// New validates cfg and returns the store. It makes no request.
func New(cfg Config) (*Store, error) {
	if cfg.Endpoint == "" || cfg.Region == "" || cfg.Bucket == "" || cfg.AccessKey == "" || cfg.SecretKey == "" {
		return nil, errors.New("s3: endpoint, region, bucket, access key and secret key are all required")
	}
	u, err := url.Parse(cfg.Endpoint)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return nil, fmt.Errorf("s3: endpoint %q must be an http(s) URL", cfg.Endpoint)
	}
	u.Path = strings.TrimSuffix(u.Path, "/")
	return &Store{cfg: cfg, base: u, client: newClient(HeaderTimeout), now: time.Now}, nil
}

// HeaderTimeout bounds how long a store call waits for the connection and
// the response headers. A hung S3/R2 (§14.3 "S3/R2 timeout khi upload")
// must turn into 503 STORE_UNAVAILABLE before the reporter's own 30 s
// timeout (§7.5), and complete makes up to three store calls in a row.
// Reading a body may take longer — bounded by bodyTimeout; the largest
// cap (§10.4) is 50 MB.
const HeaderTimeout = 8 * time.Second

const bodyTimeout = 60 * time.Second

func newClient(header time.Duration) *http.Client {
	dialer := &net.Dialer{Timeout: header, KeepAlive: 30 * time.Second}
	return &http.Client{
		Timeout: bodyTimeout,
		Transport: &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			DialContext:           dialer.DialContext,
			TLSHandshakeTimeout:   header,
			ResponseHeaderTimeout: header,
			ExpectContinueTimeout: time.Second,
			MaxIdleConnsPerHost:   16,
			IdleConnTimeout:       90 * time.Second,
			ForceAttemptHTTP2:     true,
		},
	}
}

func (s *Store) objectPath(key string) string {
	return s.base.Path + "/" + s.cfg.Bucket + "/" + strings.TrimPrefix(key, "/")
}

// ---- objstore.Store ----------------------------------------------------

func (s *Store) PresignPut(_ context.Context, key, contentType string, ttl time.Duration) (string, error) {
	return s.presign("PUT", s.objectPath(key), nil, map[string]string{"content-type": contentType}, ttl), nil
}

func (s *Store) CreateMultipart(ctx context.Context, key, contentType string) (string, error) {
	res, err := s.do(ctx, "POST", s.objectPath(key), url.Values{"uploads": {""}}, map[string]string{"content-type": contentType}, nil)
	if err != nil {
		return "", err
	}
	var out struct {
		UploadID string `xml:"UploadId"`
	}
	if err := xml.Unmarshal(res, &out); err != nil || out.UploadID == "" {
		return "", fmt.Errorf("s3: create multipart: bad response: %v", err)
	}
	return out.UploadID, nil
}

func (s *Store) PresignPart(_ context.Context, key, uploadID string, partNumber int, ttl time.Duration) (string, error) {
	q := url.Values{"partNumber": {strconv.Itoa(partNumber)}, "uploadId": {uploadID}}
	return s.presign("PUT", s.objectPath(key), q, nil, ttl), nil
}

func (s *Store) ListParts(ctx context.Context, key, uploadID string) ([]objstore.Part, error) {
	res, err := s.do(ctx, "GET", s.objectPath(key), url.Values{"uploadId": {uploadID}, "max-parts": {"1000"}}, nil, nil)
	if err != nil {
		return nil, err
	}
	var out struct {
		Parts []struct {
			PartNumber int    `xml:"PartNumber"`
			ETag       string `xml:"ETag"`
			Size       int64  `xml:"Size"`
		} `xml:"Part"`
	}
	if err := xml.Unmarshal(res, &out); err != nil {
		return nil, fmt.Errorf("s3: list parts: %w", err)
	}
	parts := make([]objstore.Part, 0, len(out.Parts))
	for _, p := range out.Parts {
		parts = append(parts, objstore.Part{Number: p.PartNumber, ETag: p.ETag, Size: p.Size})
	}
	return parts, nil
}

func (s *Store) CompleteMultipart(ctx context.Context, key, uploadID string, parts []objstore.Part) error {
	sorted := append([]objstore.Part(nil), parts...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Number < sorted[j].Number })
	var b bytes.Buffer
	b.WriteString(`<CompleteMultipartUpload>`)
	for _, p := range sorted {
		fmt.Fprintf(&b, `<Part><PartNumber>%d</PartNumber><ETag>%s</ETag></Part>`, p.Number, xmlEscape(p.ETag))
	}
	b.WriteString(`</CompleteMultipartUpload>`)
	res, err := s.do(ctx, "POST", s.objectPath(key), url.Values{"uploadId": {uploadID}}, map[string]string{"content-type": "application/xml"}, b.Bytes())
	if err != nil {
		return err
	}
	// S3 can answer 200 with an <Error> body when assembly fails late.
	if bytes.Contains(res, []byte("<Error>")) {
		return fmt.Errorf("s3: complete multipart: %s", summarize(res))
	}
	return nil
}

func (s *Store) AbortMultipart(ctx context.Context, key, uploadID string) error {
	_, err := s.do(ctx, "DELETE", s.objectPath(key), url.Values{"uploadId": {uploadID}}, nil, nil)
	return err
}

func (s *Store) Head(ctx context.Context, key string) (objstore.Info, error) {
	req, err := s.request(ctx, "HEAD", s.objectPath(key), nil, nil, nil)
	if err != nil {
		return objstore.Info{}, err
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return objstore.Info{}, fmt.Errorf("s3: head: %w", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		return objstore.Info{}, objstore.ErrNotFound
	default:
		return objstore.Info{}, fmt.Errorf("s3: head: HTTP %d", resp.StatusCode)
	}
	size, _ := strconv.ParseInt(resp.Header.Get("Content-Length"), 10, 64)
	return objstore.Info{Size: size, ContentType: resp.Header.Get("Content-Type"), ETag: resp.Header.Get("ETag")}, nil
}

func (s *Store) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	req, err := s.request(ctx, "GET", s.objectPath(key), nil, nil, nil)
	if err != nil {
		return nil, err
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("s3: get: %w", err)
	}
	if resp.StatusCode == http.StatusNotFound {
		resp.Body.Close()
		return nil, objstore.ErrNotFound
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("s3: get: HTTP %d", resp.StatusCode)
	}
	return resp.Body, nil
}

func (s *Store) Delete(ctx context.Context, key string) error {
	_, err := s.do(ctx, "DELETE", s.objectPath(key), nil, nil, nil)
	return err
}

// EnsureBucket creates the bucket; one that already exists and is ours is
// fine. Not part of objstore.Store — production buckets are made by the
// operator (deploy/README.md). Tests and local S3 servers use it.
func (s *Store) EnsureBucket(ctx context.Context) error {
	_, err := s.do(ctx, "PUT", s.base.Path+"/"+s.cfg.Bucket, nil, nil, nil)
	if err != nil && strings.Contains(err.Error(), "HTTP 409") && strings.Contains(err.Error(), "BucketAlreadyOwnedByYou") {
		return nil
	}
	return err
}

// EnsureBucketReady retries EnsureBucket until it succeeds or wait passes —
// a freshly started local server answers before its storage is up.
func EnsureBucketReady(s *Store, wait time.Duration) error {
	deadline := time.Now().Add(wait)
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err := s.EnsureBucket(ctx)
		cancel()
		if err == nil || time.Now().After(deadline) {
			return err
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// Put is a header-signed PUT of the whole body (SigV4 signs its SHA-256, so
// the payload is in memory anyway; the caller capped it per §10.4).
func (s *Store) Put(ctx context.Context, key, contentType string, body []byte) error {
	_, err := s.do(ctx, "PUT", s.objectPath(key), nil, map[string]string{"content-type": contentType}, body)
	return err
}

// ---- HTTP with header auth ----------------------------------------------

// do sends a signed request and returns the body; 2xx only. 404 maps to
// ErrNotFound.
func (s *Store) do(ctx context.Context, method, path string, query url.Values, headers map[string]string, body []byte) ([]byte, error) {
	req, err := s.request(ctx, method, path, query, headers, body)
	if err != nil {
		return nil, err
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("s3: %s: %w", method, err)
	}
	defer resp.Body.Close()
	res, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode == http.StatusNotFound {
		return nil, objstore.ErrNotFound
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("s3: %s %s: HTTP %d: %s", method, path, resp.StatusCode, summarize(res))
	}
	return res, nil
}

func (s *Store) request(ctx context.Context, method, path string, query url.Values, headers map[string]string, body []byte) (*http.Request, error) {
	u := *s.base
	u.Path = path
	u.RawPath = ""
	u.RawQuery = canonicalQuery(query)
	req, err := http.NewRequestWithContext(ctx, method, u.String(), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	now := s.now().UTC()
	payloadHash := sha256hex(body)
	signed := map[string]string{
		"host":                 u.Host,
		"x-amz-content-sha256": payloadHash,
		"x-amz-date":           now.Format(amzDateFormat),
	}
	for k, v := range headers {
		signed[strings.ToLower(k)] = v
	}
	auth := s.authorization(method, path, query, signed, payloadHash, now)
	for k, v := range signed {
		if k != "host" {
			req.Header.Set(k, v)
		}
	}
	req.Header.Set("Authorization", auth)
	return req, nil
}

// ---- Signature Version 4 ------------------------------------------------

const (
	amzDateFormat = "20060102T150405Z"
	algorithm     = "AWS4-HMAC-SHA256"
)

func (s *Store) scope(t time.Time) string {
	return t.Format("20060102") + "/" + s.cfg.Region + "/s3/aws4_request"
}

func (s *Store) signingKey(t time.Time) []byte {
	k := hmacSHA256([]byte("AWS4"+s.cfg.SecretKey), []byte(t.Format("20060102")))
	k = hmacSHA256(k, []byte(s.cfg.Region))
	k = hmacSHA256(k, []byte("s3"))
	return hmacSHA256(k, []byte("aws4_request"))
}

// canonicalRequest builds the SigV4 canonical request. headers are
// lower-case name → value and are all signed.
func canonicalRequest(method, path string, query url.Values, headers map[string]string, payloadHash string) (string, string) {
	names := make([]string, 0, len(headers))
	for k := range headers {
		names = append(names, k)
	}
	sort.Strings(names)
	var ch strings.Builder
	for _, k := range names {
		ch.WriteString(k)
		ch.WriteByte(':')
		ch.WriteString(strings.Join(strings.Fields(headers[k]), " "))
		ch.WriteByte('\n')
	}
	signedHeaders := strings.Join(names, ";")
	cr := method + "\n" + canonicalURI(path) + "\n" + canonicalQuery(query) + "\n" + ch.String() + "\n" + signedHeaders + "\n" + payloadHash
	return cr, signedHeaders
}

func (s *Store) stringToSign(cr string, t time.Time) string {
	return algorithm + "\n" + t.Format(amzDateFormat) + "\n" + s.scope(t) + "\n" + sha256hex([]byte(cr))
}

func (s *Store) authorization(method, path string, query url.Values, headers map[string]string, payloadHash string, t time.Time) string {
	cr, signedHeaders := canonicalRequest(method, path, query, headers, payloadHash)
	sig := hex.EncodeToString(hmacSHA256(s.signingKey(t), []byte(s.stringToSign(cr, t))))
	return fmt.Sprintf("%s Credential=%s/%s, SignedHeaders=%s, Signature=%s", algorithm, s.cfg.AccessKey, s.scope(t), signedHeaders, sig)
}

// presign returns a query-string-authenticated URL (UNSIGNED-PAYLOAD).
// extraHeaders are signed too, so the client must send them verbatim —
// that is how content-type is pinned on a presigned PUT.
func (s *Store) presign(method, path string, query url.Values, extraHeaders map[string]string, ttl time.Duration) string {
	t := s.now().UTC()
	headers := map[string]string{"host": s.base.Host}
	for k, v := range extraHeaders {
		headers[strings.ToLower(k)] = v
	}
	names := make([]string, 0, len(headers))
	for k := range headers {
		names = append(names, k)
	}
	sort.Strings(names)

	q := url.Values{}
	for k, vs := range query {
		q[k] = vs
	}
	q.Set("X-Amz-Algorithm", algorithm)
	q.Set("X-Amz-Credential", s.cfg.AccessKey+"/"+s.scope(t))
	q.Set("X-Amz-Date", t.Format(amzDateFormat))
	q.Set("X-Amz-Expires", strconv.Itoa(int(ttl.Seconds())))
	q.Set("X-Amz-SignedHeaders", strings.Join(names, ";"))

	cr, _ := canonicalRequest(method, path, q, headers, "UNSIGNED-PAYLOAD")
	sig := hex.EncodeToString(hmacSHA256(s.signingKey(t), []byte(s.stringToSign(cr, t))))
	q.Set("X-Amz-Signature", sig)

	u := *s.base
	u.Path = path
	u.RawPath = ""
	u.RawQuery = canonicalQuery(q)
	return u.String()
}

// canonicalURI percent-encodes each path segment per RFC 3986, keeping "/".
func canonicalURI(path string) string {
	if path == "" {
		return "/"
	}
	segs := strings.Split(path, "/")
	for i, s := range segs {
		segs[i] = uriEncode(s, false)
	}
	return strings.Join(segs, "/")
}

// canonicalQuery sorts by key then value and encodes both.
func canonicalQuery(q url.Values) string {
	if len(q) == 0 {
		return ""
	}
	keys := make([]string, 0, len(q))
	for k := range q {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		vs := append([]string(nil), q[k]...)
		sort.Strings(vs)
		for _, v := range vs {
			if b.Len() > 0 {
				b.WriteByte('&')
			}
			b.WriteString(uriEncode(k, true))
			b.WriteByte('=')
			b.WriteString(uriEncode(v, true))
		}
	}
	return b.String()
}

// uriEncode is the AWS flavour of percent-encoding: unreserved characters
// pass, everything else (including "/" when encodeSlash) is %XX upper-case.
func uriEncode(s string, encodeSlash bool) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' || c == '_' || c == '.' || c == '~':
			b.WriteByte(c)
		case c == '/' && !encodeSlash:
			b.WriteByte(c)
		default:
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}

func hmacSHA256(key, data []byte) []byte {
	m := hmac.New(sha256.New, key)
	m.Write(data)
	return m.Sum(nil)
}

func sha256hex(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func xmlEscape(s string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

// summarize keeps error bodies short and free of anything but the S3
// error code and message.
func summarize(body []byte) string {
	var e struct {
		Code    string `xml:"Code"`
		Message string `xml:"Message"`
	}
	if xml.Unmarshal(body, &e) == nil && e.Code != "" {
		return e.Code + ": " + e.Message
	}
	s := strings.TrimSpace(string(body))
	if len(s) > 200 {
		s = s[:200]
	}
	return s
}

func init() {
	objstore.Register("s3", func(cfg config.Config) (objstore.Store, error) {
		return New(Config{Endpoint: cfg.ObjStoreEndpoint, Region: cfg.ObjStoreRegion, Bucket: cfg.ObjStoreBucket, AccessKey: cfg.ObjStoreAccessKey, SecretKey: cfg.ObjStoreSecretKey})
	})
}
