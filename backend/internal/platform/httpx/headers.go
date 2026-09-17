package httpx

import (
	"net/http"
	"strings"
)

// BrowserPolicy is §11.5 at the application layer: response headers every
// answer carries, and CORS for the one kind of cross-origin caller we have —
// the dashboard on an origin from the allowlist. Never "*": the API speaks
// with credentials (cookies), so the origin is echoed back only when it is
// listed, and a request from anywhere else gets no CORS headers at all
// (the browser then refuses to read the answer; the authz Origin/CSRF
// layers refuse to act on it).
type BrowserPolicy struct {
	// AllowedOrigins is the DASHBOARD_ORIGINS list, scheme://host[:port].
	AllowedOrigins []string
	// HSTS adds Strict-Transport-Security; on only when cookies are Secure,
	// i.e. TLS terminates in front of us — sending it over plain HTTP in dev
	// would poison the browser for localhost.
	HSTS bool
}

// Middleware applies the policy. Preflights (OPTIONS with
// Access-Control-Request-Method) are answered here and go no further.
func (p BrowserPolicy) Middleware(next http.Handler) http.Handler {
	allowed := map[string]bool{}
	for _, o := range p.AllowedOrigins {
		allowed[strings.TrimRight(strings.ToLower(o), "/")] = true
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		// Every response, cross-origin or not.
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Cache-Control", "no-store")
		if p.HSTS {
			h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}

		origin := r.Header.Get("Origin")
		if origin != "" && allowed[strings.ToLower(origin)] {
			h.Set("Access-Control-Allow-Origin", origin)
			h.Set("Access-Control-Allow-Credentials", "true")
			h.Add("Vary", "Origin")
			if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {
				h.Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
				h.Set("Access-Control-Allow-Headers", "Content-Type, X-CSRF-Token, X-Request-Id")
				h.Set("Access-Control-Expose-Headers", "X-Request-Id")
				h.Set("Access-Control-Max-Age", "600")
				w.WriteHeader(http.StatusNoContent)
				return
			}
		} else if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {
			// A preflight from an origin we do not list: answered, but with
			// nothing the browser can use.
			w.WriteHeader(http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}
