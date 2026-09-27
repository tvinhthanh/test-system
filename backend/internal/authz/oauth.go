package authz

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Sign-in with Google or GitHub (§11.2 "OAuth Google/GitHub"): the
// authorization-code flow with PKCE, one provider per switch in config
// (OAUTH_GOOGLE / OAUTH_GITHUB). The provider tells us who signed in; the
// account is found by that provider's stable subject, or — the first time —
// by the provider's *verified* email matching an existing verified account
// here. There is no self-signup (§11.2), so anyone else is refused.

// OAuthHTTP is how the flow talks to a provider: platform/outbound, whose
// host allowlist is built from these providers' endpoints (§11.6 API7) —
// authz itself makes no outbound calls.
type OAuthHTTP interface {
	PostForm(ctx context.Context, rawURL string, form url.Values, v any) error
	GetJSON(ctx context.Context, rawURL, bearer string, v any) error
}

// Endpoints are the base URLs a provider is reached at, for the allowlist.
func (p *OAuthProvider) Endpoints() []string { return []string{p.TokenURL, p.APIBase} }

// OAuthProvider is one provider's endpoints and credentials. The endpoint
// fields are exported so tests can point them at a fake provider.
type OAuthProvider struct {
	Name         string // google | github — also the URL segment
	ClientID     string
	ClientSecret string
	RedirectURL  string // PUBLIC_URL + /v1/session/oauth/<name>/callback
	AuthURL      string
	TokenURL     string
	Scopes       []string
	// APIBase is where identity is read: Google's OIDC userinfo host,
	// GitHub's REST API.
	APIBase string
	// Extra authorize parameters (Google: prompt=select_account).
	Extra url.Values
	// identity reads who signed in, given an access token.
	identity func(ctx context.Context, c OAuthHTTP, p *OAuthProvider, token string) (ExternalIdentity, error)
}

// Google is Sign in with Google (OpenID Connect); the userinfo endpoint
// reports email_verified, which a link requires.
func Google(clientID, secret, publicURL string) *OAuthProvider {
	return &OAuthProvider{
		Name: "google", ClientID: clientID, ClientSecret: secret,
		RedirectURL: publicURL + "/v1/session/oauth/google/callback",
		AuthURL:     "https://accounts.google.com/o/oauth2/v2/auth",
		TokenURL:    "https://oauth2.googleapis.com/token",
		APIBase:     "https://openidconnect.googleapis.com",
		Scopes:      []string{"openid", "email"},
		Extra:       url.Values{"prompt": {"select_account"}},
		identity:    googleIdentity,
	}
}

// GitHub is Sign in with GitHub (an OAuth App); the email is the primary
// address GitHub has verified.
func GitHub(clientID, secret, publicURL string) *OAuthProvider {
	return &OAuthProvider{
		Name: "github", ClientID: clientID, ClientSecret: secret,
		RedirectURL: publicURL + "/v1/session/oauth/github/callback",
		AuthURL:     "https://github.com/login/oauth/authorize",
		TokenURL:    "https://github.com/login/oauth/access_token",
		APIBase:     "https://api.github.com",
		Scopes:      []string{"read:user", "user:email"},
		identity:    githubIdentity,
	}
}

// authorizeURL is where the browser is sent to sign in at the provider.
func (p *OAuthProvider) authorizeURL(state, challenge string) string {
	q := url.Values{
		"response_type":         {"code"},
		"client_id":             {p.ClientID},
		"redirect_uri":          {p.RedirectURL},
		"scope":                 {strings.Join(p.Scopes, " ")},
		"state":                 {state},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
	}
	for k, v := range p.Extra {
		q[k] = v
	}
	return p.AuthURL + "?" + q.Encode()
}

var errProvider = errors.New("authz: provider refused or answered unexpectedly")

// exchange trades the code for an access token (RFC 6749 §4.1.3 + PKCE).
func (p *OAuthProvider) exchange(ctx context.Context, c OAuthHTTP, code, verifier string) (string, error) {
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {code},
		"redirect_uri":  {p.RedirectURL},
		"client_id":     {p.ClientID},
		"client_secret": {p.ClientSecret},
		"code_verifier": {verifier},
	}
	var out struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
	}
	if err := c.PostForm(ctx, p.TokenURL, form, &out); err != nil {
		return "", fmt.Errorf("%w: %v", errProvider, err)
	}
	if out.AccessToken == "" {
		return "", fmt.Errorf("%w: token endpoint: %s", errProvider, out.Error)
	}
	return out.AccessToken, nil
}

func googleIdentity(ctx context.Context, c OAuthHTTP, p *OAuthProvider, token string) (ExternalIdentity, error) {
	var u struct {
		Sub           string `json:"sub"`
		Email         string `json:"email"`
		EmailVerified bool   `json:"email_verified"`
	}
	if err := c.GetJSON(ctx, p.APIBase+"/v1/userinfo", token, &u); err != nil {
		return ExternalIdentity{}, fmt.Errorf("%w: %v", errProvider, err)
	}
	if u.Sub == "" {
		return ExternalIdentity{}, fmt.Errorf("%w: userinfo without sub", errProvider)
	}
	return ExternalIdentity{Provider: "google", Subject: u.Sub, Email: u.Email, EmailVerified: u.EmailVerified}, nil
}

func githubIdentity(ctx context.Context, c OAuthHTTP, p *OAuthProvider, token string) (ExternalIdentity, error) {
	var u struct {
		ID int64 `json:"id"`
	}
	if err := c.GetJSON(ctx, p.APIBase+"/user", token, &u); err != nil {
		return ExternalIdentity{}, fmt.Errorf("%w: %v", errProvider, err)
	}
	if u.ID <= 0 {
		return ExternalIdentity{}, fmt.Errorf("%w: user without id", errProvider)
	}
	var emails []struct {
		Email    string `json:"email"`
		Primary  bool   `json:"primary"`
		Verified bool   `json:"verified"`
	}
	if err := c.GetJSON(ctx, p.APIBase+"/user/emails", token, &emails); err != nil {
		return ExternalIdentity{}, fmt.Errorf("%w: %v", errProvider, err)
	}
	id := ExternalIdentity{Provider: "github", Subject: strconv.FormatInt(u.ID, 10)}
	for _, e := range emails {
		if e.Primary {
			id.Email, id.EmailVerified = e.Email, e.Verified
		}
	}
	return id, nil
}

// OAuthStateKey derives the key that signs the short-lived state cookie of
// the flow from the pepper, with a label so it equals no other key.
func OAuthStateKey(pepper []byte) []byte {
	m := hmac.New(sha256.New, pepper)
	m.Write([]byte("atp oauth state v1"))
	return m.Sum(nil)
}

// oauthState rides in the signed cookie between start and callback: which
// provider, the state echoed back, the PKCE verifier, where to land.
type oauthState struct {
	Provider string `json:"p"`
	State    string `json:"s"`
	Verifier string `json:"v"`
	Redirect string `json:"r"`
	Expires  int64  `json:"e"`
}

const oauthStateTTL = 10 * time.Minute
