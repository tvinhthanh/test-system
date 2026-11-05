// Command sandboxd runs a team's own test scripts — a Playwright project or
// a k6 script — each in a container of its own (QĐ-19 tier 4). It is the
// only part of ATP with the Docker socket, so it does one thing: take a
// job from the worker (with the shared SANDBOX_TOKEN), run it, answer.
//
// Each job gets an internal network (no gateway), a container with no
// capabilities, a read-only root, memory, CPU, process and time limits and
// an unprivileged user, and a forwarder (platform/sandbox) that is its only
// way out — to the environment's host, past the same address guard as the
// checker. Both are removed when the job ends, so nothing is kept between
// two runs. SANDBOX_RUNTIME=runsc puts the containers under gVisor where
// the host has it (recommended).
//
//	POST /run      sandbox.Job → sandbox.Result   (one job; 503 when all slots are busy)
//	GET  /healthz  Docker answers and both images are present
package main

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/vinh/automation-testing-platform/backend/internal/platform/sandbox"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := serve(logger); err != nil {
		logger.Error("sandboxd", "err", err)
		os.Exit(1)
	}
}

type server struct {
	r     *runner
	token string
	slots chan struct{}
}

func getenv(k, def string) string {
	if v := strings.TrimSpace(os.Getenv(k)); v != "" {
		return v
	}
	return def
}

func intEnv(k string, def, lo, hi int) (int, error) {
	v, err := strconv.Atoi(getenv(k, strconv.Itoa(def)))
	if err != nil || v < lo || v > hi {
		return 0, fmt.Errorf("%s: want %d to %d", k, lo, hi)
	}
	return v, nil
}

func load() (settings, string, string, int, error) {
	var s settings
	token := os.Getenv("SANDBOX_TOKEN")
	if len(token) < 32 {
		return s, "", "", 0, errors.New("SANDBOX_TOKEN: at least 32 characters, the same in the worker")
	}
	host, _ := os.Hostname()
	s.Self = getenv("SANDBOXD_SELF", host)
	s.PlaywrightImg = getenv("SANDBOX_PLAYWRIGHT_IMAGE", "atp/sandbox-playwright:local")
	s.K6Img = getenv("SANDBOX_K6_IMAGE", "grafana/k6:1.0.0")
	s.Runtime = getenv("SANDBOX_RUNTIME", "")
	var err error
	if s.MemoryMB, err = intEnv("SANDBOX_MEMORY_MB", 2048, 256, 16384); err != nil {
		return s, "", "", 0, err
	}
	if s.Pids, err = intEnv("SANDBOX_PIDS", 512, 64, 4096); err != nil {
		return s, "", "", 0, err
	}
	if s.MaxSeconds, err = intEnv("SCRIPT_MAX_SECONDS", 900, 60, 3600); err != nil {
		return s, "", "", 0, err
	}
	concurrency, err := intEnv("SANDBOX_CONCURRENCY", 1, 1, 8)
	if err != nil {
		return s, "", "", 0, err
	}
	cpus, err := strconv.ParseFloat(getenv("SANDBOX_CPUS", "2"), 64)
	if err != nil || cpus < 0.5 || cpus > 16 {
		return s, "", "", 0, errors.New("SANDBOX_CPUS: want 0.5 to 16")
	}
	s.CPUs = cpus
	switch v := strings.ToLower(getenv("SANDBOX_ALLOW_PRIVATE", "off")); v {
	case "on", "true", "1":
		s.AllowPrivate = true
	case "off", "false", "0":
	default:
		return s, "", "", 0, fmt.Errorf("SANDBOX_ALLOW_PRIVATE=%q: want on|off", v)
	}
	return s, token, getenv("DOCKER_HOST", "unix:///var/run/docker.sock"), concurrency, nil
}

func serve(logger *slog.Logger) error {
	s, token, dockerHost, concurrency, err := load()
	if err != nil {
		return err
	}
	d, err := newDocker(dockerHost)
	if err != nil {
		return err
	}
	r := &runner{d: d, s: s, log: logger, subnets: map[string]netip.Prefix{}}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	r.sweep(ctx)
	srv := &http.Server{
		Addr:              getenv("SANDBOXD_ADDR", ":8091"),
		Handler:           (&server{r: r, token: token, slots: make(chan struct{}, concurrency)}).routes(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       2 * time.Minute,
		// A run answers when it ends.
		WriteTimeout: time.Duration(s.MaxSeconds)*time.Second + 5*time.Minute,
	}
	errc := make(chan error, 1)
	go func() { errc <- srv.ListenAndServe() }()
	logger.Info("sandboxd listening", "addr", srv.Addr, "playwright", s.PlaywrightImg, "k6", s.K6Img, "runtime", s.Runtime, "concurrency", concurrency)
	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	sctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_ = srv.Shutdown(sctx)
	r.sweep(sctx)
	return nil
}

func (s *server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /run", s.run)
	mux.HandleFunc("GET /healthz", s.health)
	return s.guard(mux)
}

// guard: nobody inside a job's network, and the token on everything but
// the health check.
func (s *server) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
			if a, err := netip.ParseAddr(host); err == nil && s.r.fromJob(a) {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
		}
		if r.URL.Path != "/healthz" {
			got := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
			if subtle.ConstantTimeCompare([]byte(got), []byte(s.token)) != 1 {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (s *server) run(w http.ResponseWriter, r *http.Request) {
	select {
	case s.slots <- struct{}{}:
		defer func() { <-s.slots }()
	default:
		http.Error(w, "busy", http.StatusServiceUnavailable)
		return
	}
	var job sandbox.Job
	// The archive is base64 in JSON: a third more than its limit, and room.
	if err := json.NewDecoder(io.LimitReader(r.Body, sandbox.MaxArchiveBytes*4/3+64<<10)).Decode(&job); err != nil {
		http.Error(w, "a job: "+err.Error(), http.StatusBadRequest)
		return
	}
	start := time.Now()
	res, err := s.r.run(r.Context(), job)
	if err != nil {
		s.r.log.Error("run", "job", job.ID, "err", err)
		http.Error(w, "the sandbox could not run the job: "+err.Error(), http.StatusBadGateway)
		return
	}
	s.r.log.Info("run", "job", job.ID, "kind", job.Kind, "exit", res.ExitCode, "timedOut", res.TimedOut, "error", res.Error, "ms", time.Since(start).Milliseconds())
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(res)
}

func (s *server) health(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	missing := []string{}
	for _, img := range []string{s.r.s.PlaywrightImg, s.r.s.K6Img} {
		if err := s.r.d.imageExists(ctx, img); err != nil {
			missing = append(missing, img)
		}
	}
	if len(missing) > 0 {
		http.Error(w, "images missing: "+strings.Join(missing, ", "), http.StatusServiceUnavailable)
		return
	}
	_, _ = io.WriteString(w, "ok")
}
