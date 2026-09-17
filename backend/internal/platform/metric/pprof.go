package metric

import (
	"net/http"
	"net/http/pprof"
)

// Pprof mounts the runtime profiles under /debug/pprof/ on the metrics mux.
// That listener is loopback by default (config.MetricsAddr) and never shares
// a port with tenant traffic, which is the only reason this is acceptable
// without authentication: a heap profile is internal data. It is what turns
// "RSS grew 12 MB over the soak" into a stack trace:
//
//	go tool pprof -base heap-early.pb.gz http://127.0.0.1:9090/debug/pprof/heap
func Pprof(mux *http.ServeMux) {
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
}
