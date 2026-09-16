package authz

import (
	"errors"
	"net/http"
	"strings"

	"github.com/vinh/automation-testing-platform/backend/internal/platform/httpx"
)

type principalKey struct{}

func bearerMiddleware(svc Service) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			raw := r.Header.Get("Authorization")
			scheme, token, _ := strings.Cut(raw, " ")
			if !strings.EqualFold(scheme, "Bearer") || token == "" {
				unauthenticated(w)
				return
			}
			p, err := svc.Authenticate(r.Context(), token)
			if err != nil {
				if !errors.Is(err, ErrUnauthenticated) {
					// Database trouble is not the client's fault; say so
					// without saying what (§11.5).
					httpx.WriteError(w, http.StatusServiceUnavailable, "AUTH_UNAVAILABLE", "authentication temporarily unavailable", nil)
					return
				}
				unauthenticated(w)
				return
			}
			next.ServeHTTP(w, r.WithContext(WithPrincipal(r.Context(), p)))
		})
	}
}

// unauthenticated is one answer for every failure mode (§7.2: wrong or
// missing token → 401, nothing more specific).
func unauthenticated(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", `Bearer realm="atp"`)
	httpx.WriteError(w, http.StatusUnauthorized, "UNAUTHENTICATED", "missing or invalid token", nil)
}
