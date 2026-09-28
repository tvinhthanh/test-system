package config

import (
	"strings"
	"testing"
	"time"
)

func TestLoadDefaultsAndValidation(t *testing.T) {
	for _, k := range []string{"APP_ENV", "HTTP_ADDR", "METRICS_ADDR", "DATABASE_URL"} {
		t.Setenv(k, "")
	}
	c, err := Load()
	if err != nil {
		t.Fatalf("defaults must validate: %v", err)
	}
	if c.Env != "dev" || c.HTTPAddr != ":8080" || !strings.HasPrefix(c.MetricsAddr, "127.0.0.1:") || c.JobRetention != 168*time.Hour {
		t.Errorf("unexpected defaults: %+v", c)
	}

	cases := []struct {
		name string
		env  map[string]string
		want string
	}{
		{"bad env", map[string]string{"APP_ENV": "production"}, "APP_ENV"},
		{"bad addr", map[string]string{"HTTP_ADDR": "8080"}, "HTTP_ADDR"},
		{"bad metrics addr", map[string]string{"METRICS_ADDR": "9090"}, "METRICS_ADDR"},
		{"not a postgres url", map[string]string{"DATABASE_URL": "mysql://x"}, "DATABASE_URL"},
		{"prod without tls", map[string]string{"APP_ENV": "prod", "DATABASE_URL": "postgres://u:p@h/db?sslmode=disable"}, "sslmode"},
		{"prod with the default sslmode (prefer)", map[string]string{"APP_ENV": "prod", "DATABASE_URL": "postgres://u:p@h/db"}, "sslmode"},
		{"prod with require (no server check)", map[string]string{"APP_ENV": "prod", "DATABASE_URL": "postgres://u:p@h/db?sslmode=require"}, "sslmode"},
		{"short pepper", map[string]string{"TOKEN_PEPPER": "short"}, "TOKEN_PEPPER"},
		{"dev pepper outside dev", map[string]string{"APP_ENV": "staging", "DATABASE_URL": "postgres://u:p@h/db"}, "TOKEN_PEPPER"},
		{"memory store outside dev", map[string]string{"APP_ENV": "staging", "DATABASE_URL": "postgres://u:p@h/db", "TOKEN_PEPPER": "0123456789abcdef0123456789abcdef"}, "OBJSTORE=memory"},
		{"s3 without settings", map[string]string{"OBJSTORE": "s3"}, "OBJSTORE_ENDPOINT"},
		{"unknown store", map[string]string{"OBJSTORE": "gcs"}, "OBJSTORE="},
		{"job retention not a duration", map[string]string{"JOB_RETENTION": "7d"}, "JOB_RETENTION"},
		{"job retention too short", map[string]string{"JOB_RETENTION": "30s"}, "JOB_RETENTION"},
		{"trusted proxies not cidrs", map[string]string{"TRUSTED_PROXIES": "10.0.0.0/8, caddy"}, "TRUSTED_PROXIES"},
		{"mail switch unknown", map[string]string{"MAIL": "sendgrid"}, "MAIL="},
		{"smtp without host", map[string]string{"MAIL": "smtp", "SMTP_FROM": "a@b.c", "PUBLIC_URL": "http://localhost:5173"}, "SMTP_HOST"},
		{"smtp without public url", map[string]string{"MAIL": "smtp", "SMTP_HOST": "mail", "SMTP_FROM": "a@b.c"}, "PUBLIC_URL"},
		{"smtp bad port", map[string]string{"SMTP_PORT": "smtp"}, "SMTP_PORT"},
		{"smtp tls unknown", map[string]string{"MAIL": "smtp", "SMTP_HOST": "mail", "SMTP_FROM": "a@b.c", "PUBLIC_URL": "http://localhost:5173", "SMTP_TLS": "ssl3"}, "SMTP_TLS"},
		{"oauth switch not on/off", map[string]string{"OAUTH_GOOGLE": "yes"}, "OAUTH_GOOGLE"},
		{"google on without credentials", map[string]string{"OAUTH_GOOGLE": "on", "PUBLIC_URL": "http://localhost:5173"}, "OAUTH_GOOGLE_CLIENT_ID"},
		{"github on without a public url", map[string]string{"OAUTH_GITHUB": "on", "OAUTH_GITHUB_CLIENT_ID": "id", "OAUTH_GITHUB_CLIENT_SECRET": "s"}, "PUBLIC_URL"},
		{"plain http public url outside dev", map[string]string{"APP_ENV": "staging", "DATABASE_URL": "postgres://u:p@h/db", "TOKEN_PEPPER": "0123456789abcdef0123456789abcdef", "OBJSTORE": "s3", "OBJSTORE_ENDPOINT": "https://s3", "OBJSTORE_BUCKET": "b", "OBJSTORE_ACCESS_KEY": "a", "OBJSTORE_SECRET_KEY": "s", "OAUTH_GITHUB": "on", "OAUTH_GITHUB_CLIENT_ID": "id", "OAUTH_GITHUB_CLIENT_SECRET": "s", "PUBLIC_URL": "http://atp.example.com"}, "PUBLIC_URL"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			_, err := Load()
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %v, want mention of %q", err, tc.want)
			}
			if err != nil && strings.Contains(err.Error(), ":p@") {
				t.Error("config error leaked the database password")
			}
		})
	}
}

// OAuth is off unless switched on, and a switched-on provider with its
// credentials and a public URL loads.
func TestOAuthSwitches(t *testing.T) {
	for _, k := range []string{"APP_ENV", "OAUTH_GOOGLE", "OAUTH_GITHUB", "PUBLIC_URL"} {
		t.Setenv(k, "")
	}
	c, err := Load()
	if err != nil || c.OAuthGoogle.Enabled || c.OAuthGitHub.Enabled {
		t.Fatalf("default must be off: %+v %v", c, err)
	}
	t.Setenv("OAUTH_GITHUB", "on")
	t.Setenv("OAUTH_GITHUB_CLIENT_ID", "gh-id")
	t.Setenv("OAUTH_GITHUB_CLIENT_SECRET", "gh-secret")
	t.Setenv("PUBLIC_URL", "http://localhost:5173/")
	c, err = Load()
	if err != nil || !c.OAuthGitHub.Enabled || c.OAuthGoogle.Enabled || c.OAuthGitHub.ClientID != "gh-id" || c.PublicURL != "http://localhost:5173" {
		t.Errorf("github on: %+v %v", c, err)
	}
}

// A prod configuration that checks the database's certificate loads.
func TestProdAcceptsVerifiedTLS(t *testing.T) {
	for _, mode := range []string{"verify-full", "verify-ca"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("APP_ENV", "prod")
			t.Setenv("DATABASE_URL", "postgres://u:p@postgres:5432/atp?sslmode="+mode+"&sslrootcert=/tls/ca.crt")
			t.Setenv("TOKEN_PEPPER", "0123456789abcdef0123456789abcdef")
			t.Setenv("OBJSTORE", "s3")
			t.Setenv("OBJSTORE_ENDPOINT", "https://s3.example.com")
			t.Setenv("OBJSTORE_BUCKET", "atp")
			t.Setenv("OBJSTORE_ACCESS_KEY", "k")
			t.Setenv("OBJSTORE_SECRET_KEY", "s")
			t.Setenv("DASHBOARD_ORIGINS", "https://atp.example.com")
			if _, err := Load(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
