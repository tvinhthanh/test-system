package httpx

import "net/http"

// Mux is the registration surface modules mount their routes on.
// *http.ServeMux satisfies it, so module tests keep using a plain mux;
// cmd/* mounts on a Router so the served surface can be enumerated.
type Mux interface {
	Handle(pattern string, handler http.Handler)
	HandleFunc(pattern string, handler func(http.ResponseWriter, *http.Request))
}

// Router is an http.ServeMux that remembers every pattern registered on
// it. §14.3 asks that a foreign principal be refused on *every* endpoint;
// a hand-kept list of routes drifts the first time someone adds one, the
// recorded list cannot.
type Router struct {
	*http.ServeMux
	patterns []string
}

func NewRouter() *Router { return &Router{ServeMux: http.NewServeMux()} }

func (r *Router) Handle(pattern string, handler http.Handler) {
	r.patterns = append(r.patterns, pattern)
	r.ServeMux.Handle(pattern, handler)
}

func (r *Router) HandleFunc(pattern string, handler func(http.ResponseWriter, *http.Request)) {
	r.patterns = append(r.patterns, pattern)
	r.ServeMux.HandleFunc(pattern, handler)
}

// Patterns returns every registered "METHOD /path" pattern in mount order.
func (r *Router) Patterns() []string { return append([]string(nil), r.patterns...) }
