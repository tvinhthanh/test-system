// Package config loads process configuration from the environment and
// validates it before anything else starts. A bad value fails the process at
// boot, not on the first request that happens to need it (§6.9.2: "fail fast").
//
// This is platform code: it knows nothing about the business (§6.9.3).
package config

import (
	"fmt"
	"os"
	"strings"
)

// Config is everything the binaries read from the environment.
type Config struct {
	// Env selects logging format and, later, safety rails. One of dev|staging|prod.
	Env string
	// HTTPAddr is the listen address for cmd/api, e.g. ":8080".
	HTTPAddr string
}

var validEnvs = map[string]bool{"dev": true, "staging": true, "prod": true}

// Load reads the environment. Missing values take development defaults;
// present-but-invalid values are an error, never silently corrected.
func Load() (Config, error) {
	c := Config{
		Env:      getenv("APP_ENV", "dev"),
		HTTPAddr: getenv("HTTP_ADDR", ":8080"),
	}
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
	if len(errs) > 0 {
		return fmt.Errorf("config: %s", strings.Join(errs, "; "))
	}
	return nil
}

func getenv(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}
