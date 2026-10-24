// Package outbound is the one HTTP client for fixed endpoints — the OAuth
// providers of §11.2 and the URL checker service (QĐ-14, which is handed
// the user's address as data and opens it itself). §11.6 API7 (SSRF) and
// API10 (unsafe consumption) are its shape:
//
//   - the hosts it may reach are fixed when it is built, from configuration,
//     never from a request (a URL a client hands the api is rendered, not
//     followed — §7.10);
//   - redirects are not followed, so an allowed host cannot bounce a call
//     elsewhere;
//   - every call has a deadline and every answer a size cap, and only JSON
//     is decoded.
//
// This is platform code: it knows nothing about the business (§6.9.3).
package outbound

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// maxBody bounds any answer read (an identity is a few hundred bytes).
const maxBody = 1 << 20

var (
	// ErrHost: the URL's host is not one this client was built for.
	ErrHost = errors.New("outbound: host not allowed")
	// ErrAnswer: the remote answered non-2xx, too much, or not JSON.
	ErrAnswer = errors.New("outbound: unexpected answer")
)

// Client talks only to the hosts it was built with.
type Client struct {
	hosts map[string]bool
	http  *http.Client
	max   int64
}

// New allows the hosts (host or host:port) of the given base URLs.
func New(timeout time.Duration, baseURLs ...string) (*Client, error) {
	c := &Client{hosts: map[string]bool{}, max: maxBody, http: &http.Client{
		Timeout:       timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}
	for _, b := range baseURLs {
		u, err := url.Parse(b)
		if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
			return nil, fmt.Errorf("outbound: base URL %q: want http(s)://host", b)
		}
		c.hosts[strings.ToLower(u.Host)] = true
	}
	return c, nil
}

func (c *Client) do(req *http.Request, v any) error {
	if !c.hosts[strings.ToLower(req.URL.Host)] {
		return fmt.Errorf("%w: %s", ErrHost, req.URL.Host)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %s: %v", ErrAnswer, req.URL.Host, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, c.max+1))
	if err != nil || int64(len(body)) > c.max {
		return fmt.Errorf("%w: %s: body unreadable or over %d bytes", ErrAnswer, req.URL.Host, c.max)
	}
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("%w: %s answered %d", ErrAnswer, req.URL.Host, resp.StatusCode)
	}
	if v == nil {
		return nil
	}
	if err := json.Unmarshal(body, v); err != nil {
		return fmt.Errorf("%w: %s: %v", ErrAnswer, req.URL.Host, err)
	}
	return nil
}

// WithMaxBody raises (or lowers) the answer cap from 1 MiB — for an
// internal service whose answers carry images (the URL checker, QĐ-14).
func (c *Client) WithMaxBody(n int64) *Client {
	c.max = n
	return c
}

// PostJSON posts v as JSON and decodes the JSON answer into out.
func (c *Client) PostJSON(ctx context.Context, rawURL string, v, out any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, rawURL, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	return c.do(req, out)
}

// PostForm posts a form and decodes the JSON answer into v.
func (c *Client) PostForm(ctx context.Context, rawURL string, form url.Values, v any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, rawURL, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return c.do(req, v)
}

// Delete sends DELETE to rawURL; the answer is decoded into v unless v is
// nil.
func (c *Client) Delete(ctx context.Context, rawURL string, v any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, rawURL, nil)
	if err != nil {
		return err
	}
	return c.do(req, v)
}

// GetJSON gets rawURL with a bearer token and decodes the JSON answer into v.
func (c *Client) GetJSON(ctx context.Context, rawURL, bearer string, v any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+bearer)
	return c.do(req, v)
}
