// Package outbound is the api's one HTTP client for fixed third-party
// endpoints — today the OAuth providers of §11.2. §11.6 API7 (SSRF) and
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
}

// New allows the hosts (host or host:port) of the given base URLs.
func New(timeout time.Duration, baseURLs ...string) (*Client, error) {
	c := &Client{hosts: map[string]bool{}, http: &http.Client{
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
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil || len(body) > maxBody {
		return fmt.Errorf("%w: %s: body unreadable or over %d bytes", ErrAnswer, req.URL.Host, maxBody)
	}
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("%w: %s answered %d", ErrAnswer, req.URL.Host, resp.StatusCode)
	}
	if err := json.Unmarshal(body, v); err != nil {
		return fmt.Errorf("%w: %s: %v", ErrAnswer, req.URL.Host, err)
	}
	return nil
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

// GetJSON gets rawURL with a bearer token and decodes the JSON answer into v.
func (c *Client) GetJSON(ctx context.Context, rawURL, bearer string, v any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+bearer)
	return c.do(req, v)
}
