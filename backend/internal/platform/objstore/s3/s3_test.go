package s3

import (
	"net/url"
	"strings"
	"testing"
	"time"
)

// The worked examples from the AWS documentation ("Signature Calculations
// for the Authorization Header" and "Authenticating Requests: Using Query
// Parameters"), bucket examplebucket, region us-east-1, 24 May 2013.
func exampleStore(t *testing.T) *Store {
	t.Helper()
	s, err := New(Config{Endpoint: "https://examplebucket.s3.amazonaws.com", Region: "us-east-1", Bucket: "examplebucket",
		AccessKey: "AKIAIOSFODNN7EXAMPLE", SecretKey: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"})
	if err != nil {
		t.Fatal(err)
	}
	s.now = func() time.Time { return time.Date(2013, 5, 24, 0, 0, 0, 0, time.UTC) }
	return s
}

func TestPresignMatchesAWSExample(t *testing.T) {
	s := exampleStore(t)
	got := s.presign("GET", "/test.txt", nil, nil, 86400*time.Second)
	u, err := url.Parse(got)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	if sig := q.Get("X-Amz-Signature"); sig != "aeeed9bbccd4d02ee5c0109b86d86835f995330da4c265957d157751f604d404" {
		t.Errorf("signature = %s\nurl = %s", sig, got)
	}
	if q.Get("X-Amz-Credential") != "AKIAIOSFODNN7EXAMPLE/20130524/us-east-1/s3/aws4_request" || q.Get("X-Amz-SignedHeaders") != "host" {
		t.Errorf("query = %s", u.RawQuery)
	}
}

func TestAuthorizationHeaderMatchesAWSExamples(t *testing.T) {
	s := exampleStore(t)
	empty := "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

	// GET object with a Range header.
	auth := s.authorization("GET", "/test.txt", nil, map[string]string{
		"host": "examplebucket.s3.amazonaws.com", "range": "bytes=0-9", "x-amz-content-sha256": empty, "x-amz-date": "20130524T000000Z",
	}, empty, s.now())
	if !strings.HasSuffix(auth, "Signature=f0e8bdb87c964420e857bd35b5d6ed310bd44f0170aba48dd91039c6036bdb41") {
		t.Errorf("GET: %s", auth)
	}
	if !strings.Contains(auth, "SignedHeaders=host;range;x-amz-content-sha256;x-amz-date") {
		t.Errorf("GET signed headers: %s", auth)
	}

	// PUT object "Welcome to Amazon S3." to /test$file.text with a storage class.
	payload := sha256hex([]byte("Welcome to Amazon S3."))
	auth = s.authorization("PUT", "/test$file.text", nil, map[string]string{
		"host": "examplebucket.s3.amazonaws.com", "date": "Fri, 24 May 2013 00:00:00 GMT", "x-amz-content-sha256": payload,
		"x-amz-date": "20130524T000000Z", "x-amz-storage-class": "REDUCED_REDUNDANCY",
	}, payload, s.now())
	if !strings.HasSuffix(auth, "Signature=98ad721746da40c64f1a55b78f14c238d841ea1380cd77a1b5971af0ece108bd") {
		t.Errorf("PUT: %s", auth)
	}

	// GET bucket lifecycle: a query with an empty value.
	auth = s.authorization("GET", "/", url.Values{"lifecycle": {""}}, map[string]string{
		"host": "examplebucket.s3.amazonaws.com", "x-amz-content-sha256": empty, "x-amz-date": "20130524T000000Z",
	}, empty, s.now())
	if !strings.HasSuffix(auth, "Signature=fea454ca298b7da1c68078a5d1bdbfbbe0d65c699e0f91ac7a200a0136783543") {
		t.Errorf("lifecycle: %s", auth)
	}
}

func TestCanonicalEncoding(t *testing.T) {
	if got := canonicalURI("/b/photos/2026 09/tr@ce.zip"); got != "/b/photos/2026%2009/tr%40ce.zip" {
		t.Errorf("uri = %s", got)
	}
	if got := canonicalQuery(url.Values{"uploadId": {"a/b+c"}, "partNumber": {"2"}, "uploads": {""}}); got != "partNumber=2&uploadId=a%2Fb%2Bc&uploads=" {
		t.Errorf("query = %s", got)
	}
}
