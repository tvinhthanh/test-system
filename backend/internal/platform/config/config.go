// Package config loads process configuration from the environment and
// validates it before anything else starts. A bad value fails the process at
// boot, not on the first request that happens to need it (§6.9.2: "fail fast").
//
// This is platform code: it knows nothing about the business (§6.9.3).
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config is everything the binaries read from the environment.
type Config struct {
	// Env selects logging format and, later, safety rails. One of dev|staging|prod.
	Env string
	// HTTPAddr is the listen address for cmd/api, e.g. ":8080".
	HTTPAddr string
	// MetricsAddr is where cmd/api serves /metrics (§6.4). It is a second
	// listener so the scrape endpoint never shares a port with tenant traffic:
	// per-project labels are internal data, not something a bearer token
	// should be able to read.
	MetricsAddr string
	// DatabaseURL is the postgres:// URL every binary connects to.
	DatabaseURL string
	// TokenPepper is the HMAC key for API token hashes (§11.4). It lives in
	// the environment, not the database, so a database dump alone cannot
	// verify a token. Rotating it invalidates every token.
	TokenPepper string
	// ObjStore selects the artifact store (§7.7): "s3" (S3 or R2, needs the
	// OBJSTORE_* settings below) or "memory" (in-process, dev only —
	// objects vanish with the process).
	ObjStore          string
	ObjStoreEndpoint  string
	ObjStoreRegion    string
	ObjStoreBucket    string
	ObjStoreAccessKey string
	ObjStoreSecretKey string
	// ObjStoreMemoryAddr makes the dev memory store listen on a fixed address
	// (e.g. ":9100") so a reverse proxy can forward presigned PUTs from
	// outside the container; OBJSTORE_ENDPOINT is then the public base URL of
	// those PUTs (deploy/compose.yml with OBJSTORE=memory). Dev only.
	ObjStoreMemoryAddr string
	// DashboardOrigins are the browser origins allowed to make
	// cookie-authenticated state-changing requests (§11.2 Origin allowlist,
	// §11.5 CORS: named origins, never *).
	DashboardOrigins []string
	// CookieSecure sets Secure on session cookies. Only dev may turn it off.
	CookieSecure bool
	// DashboardRatePerMinute caps dashboard requests per signed-in user
	// (burst 2×); a browser tab needs a few per second, a scraper of every
	// run in a project does not (§11.6 API4). 0 disables — tests only.
	DashboardRatePerMinute int
	// JobRetention is how long finished queue jobs stay before the worker
	// prunes them (§14.1 T5: the jobs table must not grow with history).
	// Default 168h; the soak harness shortens it to watch the prune loop.
	JobRetention time.Duration
	// OAuth sign-in (§11.2), one switch per provider: OAUTH_GOOGLE=on|off,
	// OAUTH_GITHUB=on|off (default off), each with its CLIENT_ID and
	// CLIENT_SECRET. PublicURL is the dashboard's public origin; the
	// redirect URI registered with the provider is
	// PublicURL + "/v1/session/oauth/<provider>/callback".
	OAuthGoogle OAuthClient
	OAuthGitHub OAuthClient
	PublicURL   string
}

// OAuthClient is one provider's switch and credentials.
type OAuthClient struct {
	Enabled      bool
	ClientID     string
	ClientSecret string
}

// devPepper is only ever accepted when APP_ENV=dev.
const devPepper = "dev-only-pepper-not-a-secret-0000"

var validEnvs = map[string]bool{"dev": true, "staging": true, "prod": true}

// Load reads the environment. Missing values take development defaults;
// present-but-invalid values are an error, never silently corrected.
func Load() (Config, error) {
	c := Config{
		Env:                getenv("APP_ENV", "dev"),
		HTTPAddr:           getenv("HTTP_ADDR", ":8080"),
		MetricsAddr:        getenv("METRICS_ADDR", "127.0.0.1:9090"),
		DatabaseURL:        getenv("DATABASE_URL", "postgres://postgres:postgres@localhost:5432/atp?sslmode=disable"),
		TokenPepper:        getenv("TOKEN_PEPPER", devPepper),
		ObjStore:           getenv("OBJSTORE", "memory"),
		ObjStoreEndpoint:   os.Getenv("OBJSTORE_ENDPOINT"),
		ObjStoreRegion:     getenv("OBJSTORE_REGION", "auto"),
		ObjStoreBucket:     os.Getenv("OBJSTORE_BUCKET"),
		ObjStoreAccessKey:  os.Getenv("OBJSTORE_ACCESS_KEY"),
		ObjStoreSecretKey:  os.Getenv("OBJSTORE_SECRET_KEY"),
		ObjStoreMemoryAddr: os.Getenv("OBJSTORE_MEMORY_ADDR"),
		DashboardOrigins:   splitList(getenv("DASHBOARD_ORIGINS", "http://localhost:5173,http://127.0.0.1:5173,http://localhost:3000,http://localhost:8080")),
		CookieSecure:       getenv("COOKIE_SECURE", "true") != "false",
	}
	if v, err := strconv.Atoi(getenv("DASHBOARD_RATE_PER_MINUTE", "600")); err != nil || v < 0 {
		return c, fmt.Errorf("DASHBOARD_RATE_PER_MINUTE: want a non-negative integer")
	} else {
		c.DashboardRatePerMinute = v
	}
	if d, err := time.ParseDuration(getenv("JOB_RETENTION", "168h")); err != nil || d < time.Minute {
		return c, fmt.Errorf("JOB_RETENTION: want a duration of at least 1m, e.g. 168h")
	} else {
		c.JobRetention = d
	}
	for _, p := range []struct {
		env string
		dst *OAuthClient
	}{{"GOOGLE", &c.OAuthGoogle}, {"GITHUB", &c.OAuthGitHub}} {
		switch v := strings.ToLower(getenv("OAUTH_"+p.env, "off")); v {
		case "on", "true", "1":
			p.dst.Enabled = true
		case "off", "false", "0":
		default:
			return c, fmt.Errorf("OAUTH_%s=%q: want on|off", p.env, v)
		}
		p.dst.ClientID = os.Getenv("OAUTH_" + p.env + "_CLIENT_ID")
		p.dst.ClientSecret = os.Getenv("OAUTH_" + p.env + "_CLIENT_SECRET")
	}
	c.PublicURL = strings.TrimSuffix(os.Getenv("PUBLIC_URL"), "/")
	return c, c.validate()
}

func (c Config) validate() error {
	var errs []string
	if !validEnvs[c.Env] {
		errs = append(errs, fmt.Sprintf("APP_ENV=%q: want dev|staging|prod", c.Env))
	}
	if !strings.Contains(c.HTTPAddr, ":") {
		errs = append(errs, fmt.Sprintf("HTTP_ADDR=%q: want host:port or :port", c.HTTPAddr))
	}
	if !strings.Contains(c.MetricsAddr, ":") {
		errs = append(errs, fmt.Sprintf("METRICS_ADDR=%q: want host:port or :port", c.MetricsAddr))
	}
	if !strings.HasPrefix(c.DatabaseURL, "postgres://") && !strings.HasPrefix(c.DatabaseURL, "postgresql://") {
		errs = append(errs, "DATABASE_URL: want a postgres:// URL")
	}
	if c.Env == "prod" && strings.Contains(c.DatabaseURL, "sslmode=disable") {
		errs = append(errs, "DATABASE_URL: sslmode=disable is not allowed when APP_ENV=prod (§11.5)")
	}
	if len(c.TokenPepper) < 32 {
		errs = append(errs, "TOKEN_PEPPER: want at least 32 bytes")
	}
	if c.Env != "dev" && c.TokenPepper == devPepper {
		errs = append(errs, "TOKEN_PEPPER: the dev default is not allowed when APP_ENV="+c.Env)
	}
	if !c.CookieSecure && c.Env != "dev" {
		errs = append(errs, "COOKIE_SECURE=false is dev-only (§11.5 TLS everywhere)")
	}
	for _, o := range c.DashboardOrigins {
		if o == "*" || (!strings.HasPrefix(o, "http://") && !strings.HasPrefix(o, "https://")) {
			errs = append(errs, fmt.Sprintf("DASHBOARD_ORIGINS: %q must be a scheme://host origin, never *", o))
		}
	}
	for name, p := range map[string]OAuthClient{"GOOGLE": c.OAuthGoogle, "GITHUB": c.OAuthGitHub} {
		if p.Enabled && (p.ClientID == "" || p.ClientSecret == "") {
			errs = append(errs, fmt.Sprintf("OAUTH_%s=on needs OAUTH_%s_CLIENT_ID and OAUTH_%s_CLIENT_SECRET", name, name, name))
		}
	}
	if c.OAuthGoogle.Enabled || c.OAuthGitHub.Enabled {
		switch {
		case c.PublicURL == "":
			errs = append(errs, "PUBLIC_URL is required when an OAuth provider is on (the redirect URI is built from it)")
		case !strings.HasPrefix(c.PublicURL, "https://") && !(c.Env == "dev" && strings.HasPrefix(c.PublicURL, "http://")):
			errs = append(errs, fmt.Sprintf("PUBLIC_URL=%q: want https:// (http:// only when APP_ENV=dev)", c.PublicURL))
		}
	}
	switch c.ObjStore {
	case "memory":
		if c.Env != "dev" {
			errs = append(errs, "OBJSTORE=memory is dev-only; set OBJSTORE=s3 when APP_ENV="+c.Env)
		}
	case "s3":
		if c.ObjStoreEndpoint == "" || c.ObjStoreBucket == "" || c.ObjStoreAccessKey == "" || c.ObjStoreSecretKey == "" {
			errs = append(errs, "OBJSTORE=s3 needs OBJSTORE_ENDPOINT, OBJSTORE_BUCKET, OBJSTORE_ACCESS_KEY and OBJSTORE_SECRET_KEY")
		}
	default:
		errs = append(errs, fmt.Sprintf("OBJSTORE=%q: want s3|memory", c.ObjStore))
	}
	if len(errs) > 0 {
		return fmt.Errorf("config: %s", strings.Join(errs, "; "))
	}
	return nil
}

// splitList parses a comma-separated list, trimming blanks.
func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, strings.TrimSuffix(p, "/"))
		}
	}
	return out
}

func getenv(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}
