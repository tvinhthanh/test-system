package config

import (
	"strings"
	"testing"
)

func TestLoadDefaultsAndValidation(t *testing.T) {
	for _, k := range []string{"APP_ENV", "HTTP_ADDR", "METRICS_ADDR", "DATABASE_URL"} {
		t.Setenv(k, "")
	}
	c, err := Load()
	if err != nil {
		t.Fatalf("defaults must validate: %v", err)
	}
	if c.Env != "dev" || c.HTTPAddr != ":8080" || !strings.HasPrefix(c.MetricsAddr, "127.0.0.1:") {
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
		{"short pepper", map[string]string{"TOKEN_PEPPER": "short"}, "TOKEN_PEPPER"},
		{"dev pepper outside dev", map[string]string{"APP_ENV": "staging", "DATABASE_URL": "postgres://u:p@h/db"}, "TOKEN_PEPPER"},
		{"memory store outside dev", map[string]string{"APP_ENV": "staging", "DATABASE_URL": "postgres://u:p@h/db", "TOKEN_PEPPER": "0123456789abcdef0123456789abcdef"}, "OBJSTORE=memory"},
		{"s3 without settings", map[string]string{"OBJSTORE": "s3"}, "OBJSTORE_ENDPOINT"},
		{"unknown store", map[string]string{"OBJSTORE": "gcs"}, "OBJSTORE="},
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
