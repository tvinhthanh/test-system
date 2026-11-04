package autotest

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/vinh/automation-testing-platform/backend/internal/platform/pg"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/queue"
)

// Load tests (QĐ-19 tier 3). A load test sends real traffic, so it runs only
// against a host its organization proved it owns — a token at
// /.well-known/atp-verification.txt, or in a TXT record at
// _atp-verification.<host> — checked when asked and again right before each
// run, and only within the operator's caps. The checker sends the requests
// through its guarded proxy, like a browser's.

const (
	WellKnownPath = "/.well-known/atp-verification.txt"
	TXTPrefix     = "_atp-verification."
	// MaxActiveLoads per organization: one load at a time; DailyLoads per project.
	MaxActiveLoads = 1
	DailyLoads     = 20
	maxLoadPaths   = 20
)

// Ownership states.
const (
	OwnershipUnverified = "unverified"
	OwnershipChecking   = "checking"
	OwnershipVerified   = "verified"
	OwnershipFailed     = "failed"
)

// Ownership is an organization's proof that it runs a host.
type Ownership struct {
	ID         string
	OrgID      string
	Host       string
	Token      string
	Method     string // "" until checked; file | dns
	Status     string
	Error      string
	VerifiedAt time.Time
	CheckedAt  time.Time
	CreatedBy  string
	CreatedAt  time.Time
}

// FileText is what the file must contain, DNSText what the TXT record holds.
func (o Ownership) FileText() string { return "atp-verification=" + o.Token }
func (o Ownership) DNSName() string  { return TXTPrefix + o.Host }

// LoadCaps are the operator's limits for one run (LOAD_MAX_VUS, LOAD_MAX_SECONDS).
type LoadCaps struct {
	MaxVUs     int
	MaxSeconds int
}

func (c LoadCaps) orDefault() LoadCaps {
	if c.MaxVUs <= 0 {
		c.MaxVUs = 20
	}
	if c.MaxSeconds <= 0 {
		c.MaxSeconds = 60
	}
	return c
}

type NewLoadRun struct {
	ProjectID     string
	OrgID         string
	EnvironmentID string
	Paths         []string // on the environment's host, each starting with /; none = "/"
	VUs           int
	Seconds       int
	RampSeconds   int
	MaxP95Ms      int     // 0 = no threshold
	MaxErrorRate  float64 // percent; < 0 = no threshold
	Actor         string
}

type LoadRun struct {
	ID            string
	ProjectID     string
	EnvironmentID string
	Host          string
	Base          string
	Paths         []string
	VUs           int
	Seconds       int
	RampSeconds   int
	MaxP95Ms      int     // 0 = none
	MaxErrorRate  float64 // -1 = none
	Status        string  // queued | running | done | error
	Verdict       string  // "" | passed | failed
	Reasons       []string
	Result        json.RawMessage // the checker's answer; null until done
	Error         string
	RequestedBy   string
	CreatedAt     time.Time
	StartedAt     time.Time
	FinishedAt    time.Time
}

// LoadSpec is what the checker runs.
type LoadSpec struct {
	Base        string   `json:"base"`
	Paths       []string `json:"paths"`
	VUs         int      `json:"vus"`
	Seconds     int      `json:"seconds"`
	RampSeconds int      `json:"rampSeconds"`
}

// LoadResult is the part of the checker's answer the verdict reads.
type LoadResult struct {
	Blocked   bool    `json:"blocked"`
	Error     string  `json:"error"`
	Requests  int     `json:"requests"`
	Errors    int     `json:"errors"`
	ErrorRate float64 `json:"errorRate"`
	LatencyMs struct {
		P95 int `json:"p95"`
	} `json:"latencyMs"`
}

// FileProof is the proof file as the checker read it.
type FileProof struct {
	Status int    `json:"status"`
	Text   string `json:"text"`
	Error  string `json:"error"`
}

// Loader is the checker's /verify and /load (NewHTTPLoader).
type Loader interface {
	VerifyFile(ctx context.Context, base string) (FileProof, error)
	Load(ctx context.Context, spec LoadSpec) (json.RawMessage, error)
}

// TXTLookup reads DNS TXT records (net.Resolver.LookupTXT).
type TXTLookup func(ctx context.Context, name string) ([]string, error)

func newToken() string {
	const alphabet = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	for i := range b {
		b[i] = alphabet[int(b[i])%len(alphabet)]
	}
	return string(b)
}

// hostOf is the host an environment's address names, lower case, with the
// port: what an organization proves and a load test hits.
func hostOf(base string) (string, error) {
	u, err := url.Parse(base)
	if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return "", fmt.Errorf("the environment has no http(s) address")
	}
	return strings.ToLower(u.Host), nil
}

const ownershipColumns = `id, org_id, host, token, method, status, error, verified_at, checked_at, created_by, created_at`

func scanOwnership(row interface{ Scan(dest ...any) error }) (Ownership, error) {
	var o Ownership
	var verified, checked *time.Time
	err := row.Scan(&o.ID, &o.OrgID, &o.Host, &o.Token, &o.Method, &o.Status, &o.Error, &verified, &checked, &o.CreatedBy, &o.CreatedAt)
	if errors.Is(err, pg.ErrNoRows) {
		return Ownership{}, ErrNotFound
	}
	if err != nil {
		return Ownership{}, err
	}
	if verified != nil {
		o.VerifiedAt = verified.UTC()
	}
	if checked != nil {
		o.CheckedAt = checked.UTC()
	}
	o.CreatedAt = o.CreatedAt.UTC()
	return o, nil
}

// Ownership returns the organization's proof for the environment's host,
// making its token the first time.
func (s *service) Ownership(ctx context.Context, projectID, orgID, environmentID, actor string) (Ownership, error) {
	if !uuidRe.MatchString(orgID) {
		return Ownership{}, ErrNotFound
	}
	env, err := s.siteEnvironment(ctx, projectID, environmentID)
	if err != nil {
		return Ownership{}, err
	}
	host, err := hostOf(env.BaseURL)
	if err != nil {
		return Ownership{}, invalid("%v", err)
	}
	actor, err = validActor(actor)
	if err != nil {
		return Ownership{}, err
	}
	q := s.DB.Q(ctx)
	if _, err := q.Exec(ctx, `INSERT INTO site_ownership (org_id, host, token, created_by) VALUES ($1, $2, $3, $4)
		ON CONFLICT (org_id, host) DO NOTHING`, orgID, host, newToken(), actor); err != nil {
		return Ownership{}, err
	}
	return scanOwnership(q.QueryRow(ctx, `SELECT `+ownershipColumns+` FROM site_ownership WHERE org_id = $1 AND host = $2`, orgID, host))
}

// CheckOwnership queues checking the proof by method (file | dns).
func (s *service) CheckOwnership(ctx context.Context, projectID, orgID, environmentID, method, actor string) (Ownership, error) {
	if method != "file" && method != "dns" {
		return Ownership{}, invalid(`method is "file" or "dns"`)
	}
	o, err := s.Ownership(ctx, projectID, orgID, environmentID, actor)
	if err != nil {
		return Ownership{}, err
	}
	err = s.DB.WithTx(ctx, func(ctx context.Context) error {
		q := s.DB.Q(ctx)
		o, err = scanOwnership(q.QueryRow(ctx, `UPDATE site_ownership SET status = 'checking', method = $2, error = ''
			WHERE id = $1 RETURNING `+ownershipColumns, o.ID, method))
		if err != nil {
			return err
		}
		_, _, err = s.q.Add(ctx, queue.Enqueue{Kind: JobOwnership, Key: "ownership/" + o.ID + "/" + newToken()[:8], MaxAttempts: 2,
			Payload: map[string]string{"ownershipId": o.ID, "projectId": projectID, "environmentId": environmentID}})
		return err
	})
	return o, err
}

// prove checks a proof now; nil when the host shows the token.
func (s *service) prove(ctx context.Context, o Ownership, base string) error {
	switch o.Method {
	case "dns":
		lookup := s.TXT
		if lookup == nil {
			lookup = net.DefaultResolver.LookupTXT
		}
		host := o.Host
		if h, _, err := net.SplitHostPort(host); err == nil {
			host = h
		}
		records, err := lookup(ctx, TXTPrefix+host)
		if err != nil {
			return tell("no TXT record at %s%s (%v)", TXTPrefix, host, err)
		}
		if !slices.ContainsFunc(records, func(r string) bool { return strings.TrimSpace(r) == o.FileText() }) {
			return tell("the TXT record at %s%s does not hold %s", TXTPrefix, host, o.FileText())
		}
		return nil
	case "file":
		if s.Loader == nil {
			return errors.New("autotest: no checker configured in this process")
		}
		p, err := s.Loader.VerifyFile(ctx, base)
		if err != nil {
			return err
		}
		switch {
		case p.Error != "":
			return tell("%s%s could not be read: %s", o.Host, WellKnownPath, p.Error)
		case p.Status != 200:
			return tell("%s%s answered %d (want 200, no redirect)", o.Host, WellKnownPath, p.Status)
		case !strings.Contains(p.Text, o.FileText()):
			return tell("%s%s does not contain %s", o.Host, WellKnownPath, o.FileText())
		}
		return nil
	}
	return tell("no proof method chosen yet")
}

func (s *service) OwnershipWorker(concurrency int) *queue.Worker {
	return &queue.Worker{Queue: s.q, DB: s.DB, Kind: JobOwnership, Handle: s.runOwnership, Concurrency: concurrency, Lease: 2 * time.Minute, Poll: 2 * time.Second}
}

func (s *service) runOwnership(ctx context.Context, job queue.Job) error {
	var p struct {
		OwnershipID   string `json:"ownershipId"`
		ProjectID     string `json:"projectId"`
		EnvironmentID string `json:"environmentId"`
	}
	if err := json.Unmarshal(job.Payload, &p); err != nil || !uuidRe.MatchString(p.OwnershipID) {
		return nil
	}
	q := s.DB.Q(ctx)
	o, err := scanOwnership(q.QueryRow(ctx, `SELECT `+ownershipColumns+` FROM site_ownership WHERE id = $1 FOR UPDATE`, p.OwnershipID))
	if errors.Is(err, ErrNotFound) || (err == nil && o.Status != OwnershipChecking) {
		return nil
	}
	if err != nil {
		return err
	}
	env, err := s.siteEnvironment(ctx, p.ProjectID, p.EnvironmentID)
	if err == nil {
		err = s.prove(ctx, o, env.BaseURL)
	}
	if err != nil {
		msg, final := lastWord(err, job)
		if !final {
			return err
		}
		_, err = q.Exec(ctx, `UPDATE site_ownership SET status = 'failed', error = $2, checked_at = clock_timestamp() WHERE id = $1`, o.ID, cut(msg, 1000))
		return err
	}
	_, err = q.Exec(ctx, `UPDATE site_ownership SET status = 'verified', error = '', verified_at = clock_timestamp(), checked_at = clock_timestamp() WHERE id = $1`, o.ID)
	return err
}

// ------------------------------------------------------------------ runs

const loadColumns = `id, project_id, environment_id, host, base, paths, vus, seconds, ramp_seconds, coalesce(max_p95_ms, 0),
	coalesce(max_error_rate, -1)::float8, status, verdict, reasons, result, coalesce(error, ''), requested_by, created_at, started_at, finished_at`

func scanLoad(row interface{ Scan(dest ...any) error }) (LoadRun, error) {
	var l LoadRun
	var started, finished *time.Time
	var result []byte
	err := row.Scan(&l.ID, &l.ProjectID, &l.EnvironmentID, &l.Host, &l.Base, &l.Paths, &l.VUs, &l.Seconds, &l.RampSeconds, &l.MaxP95Ms,
		&l.MaxErrorRate, &l.Status, &l.Verdict, &l.Reasons, &result, &l.Error, &l.RequestedBy, &l.CreatedAt, &started, &finished)
	if errors.Is(err, pg.ErrNoRows) {
		return LoadRun{}, ErrNotFound
	}
	if err != nil {
		return LoadRun{}, err
	}
	if result != nil {
		l.Result = json.RawMessage(result)
	}
	l.CreatedAt = l.CreatedAt.UTC()
	if started != nil {
		l.StartedAt = started.UTC()
	}
	if finished != nil {
		l.FinishedAt = finished.UTC()
	}
	if l.Reasons == nil {
		l.Reasons = []string{}
	}
	return l, nil
}

// cleanPaths keeps paths on the host: each starts with one "/".
func cleanPaths(paths []string) ([]string, error) {
	if len(paths) == 0 {
		return []string{"/"}, nil
	}
	if len(paths) > maxLoadPaths {
		return nil, invalid("at most %d paths", maxLoadPaths)
	}
	out := []string{}
	for _, p := range paths {
		p = strings.TrimSpace(p)
		if !strings.HasPrefix(p, "/") || strings.HasPrefix(p, "//") || len(p) > 2000 || strings.ContainsAny(p, " \t\r\n\\") {
			return nil, invalid("path %q: each path starts with / and stays on the environment's host", p)
		}
		out = append(out, p)
	}
	return out, nil
}

func (s *service) CreateLoadRun(ctx context.Context, in NewLoadRun) (LoadRun, error) {
	if !uuidRe.MatchString(in.OrgID) {
		return LoadRun{}, ErrNotFound
	}
	caps := s.LoadCaps.orDefault()
	env, err := s.siteEnvironment(ctx, in.ProjectID, in.EnvironmentID)
	if err != nil {
		return LoadRun{}, err
	}
	host, err := hostOf(env.BaseURL)
	if err != nil {
		return LoadRun{}, invalid("%v", err)
	}
	paths, err := cleanPaths(in.Paths)
	if err != nil {
		return LoadRun{}, err
	}
	switch {
	case in.VUs < 1 || in.VUs > caps.MaxVUs:
		return LoadRun{}, invalid("vus: 1 to %d on this server (LOAD_MAX_VUS)", caps.MaxVUs)
	case in.Seconds < 5 || in.Seconds > caps.MaxSeconds:
		return LoadRun{}, invalid("seconds: 5 to %d on this server (LOAD_MAX_SECONDS)", caps.MaxSeconds)
	case in.RampSeconds < 0 || in.RampSeconds >= in.Seconds:
		return LoadRun{}, invalid("rampSeconds: 0 to less than seconds")
	case in.MaxP95Ms < 0 || in.MaxP95Ms > 600000:
		return LoadRun{}, invalid("maxP95Ms: 0 (none) to 600000")
	case in.MaxErrorRate > 100:
		return LoadRun{}, invalid("maxErrorRate: a percent, 0 to 100")
	}
	actor, err := validActor(in.Actor)
	if err != nil {
		return LoadRun{}, err
	}
	var out LoadRun
	err = s.DB.WithTx(ctx, func(ctx context.Context) error {
		q := s.DB.Q(ctx)
		if _, err := q.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('autotest.load:' || $1, 0))`, in.OrgID); err != nil {
			return err
		}
		var status string
		err := q.QueryRow(ctx, `SELECT status FROM site_ownership WHERE org_id = $1 AND host = $2`, in.OrgID, host).Scan(&status)
		if errors.Is(err, pg.ErrNoRows) || (err == nil && status != OwnershipVerified) {
			return fmt.Errorf("%w: %s", ErrNotProved, host)
		}
		if err != nil {
			return err
		}
		var active, today int
		if err := q.QueryRow(ctx, `SELECT (SELECT count(*) FROM load_runs WHERE org_id = $1 AND status IN ('queued', 'running')),
			(SELECT count(*) FROM load_runs WHERE project_id = $2 AND created_at > now() - interval '24 hours')`, in.OrgID, in.ProjectID).Scan(&active, &today); err != nil {
			return err
		}
		if active >= MaxActiveLoads || today >= DailyLoads {
			return ErrBusy
		}
		var p95 *int
		if in.MaxP95Ms > 0 {
			p95 = &in.MaxP95Ms
		}
		var rate *float64
		if in.MaxErrorRate >= 0 {
			rate = &in.MaxErrorRate
		}
		out, err = scanLoad(q.QueryRow(ctx, `INSERT INTO load_runs (project_id, org_id, environment_id, host, base, paths, vus, seconds, ramp_seconds,
			max_p95_ms, max_error_rate, requested_by) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12) RETURNING `+loadColumns,
			in.ProjectID, in.OrgID, in.EnvironmentID, host, env.BaseURL, paths, in.VUs, in.Seconds, in.RampSeconds, p95, rate, actor))
		if err != nil {
			return err
		}
		_, _, err = s.q.Add(ctx, queue.Enqueue{Kind: JobLoad, Key: "load/" + out.ID, MaxAttempts: 1,
			Payload: map[string]string{"loadRunId": out.ID, "projectId": in.ProjectID, "orgId": in.OrgID}})
		return err
	})
	return out, err
}

func (s *service) LoadRuns(ctx context.Context, projectID string, limit int) ([]LoadRun, error) {
	if !uuidRe.MatchString(projectID) {
		return nil, ErrNotFound
	}
	if limit <= 0 || limit > 100 {
		limit = 30
	}
	rows, err := s.DB.Q(ctx).Query(ctx, `SELECT `+loadColumns+` FROM load_runs WHERE project_id = $1 ORDER BY created_at DESC, id DESC LIMIT $2`, projectID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []LoadRun{}
	for rows.Next() {
		l, err := scanLoad(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

func (s *service) GetLoadRun(ctx context.Context, projectID, id string) (LoadRun, error) {
	if !uuidRe.MatchString(projectID) || !uuidRe.MatchString(id) {
		return LoadRun{}, ErrNotFound
	}
	return scanLoad(s.DB.Q(ctx).QueryRow(ctx, `SELECT `+loadColumns+` FROM load_runs WHERE id = $1 AND project_id = $2`, id, projectID))
}

func (s *service) LoadWorker(concurrency int) *queue.Worker {
	// The longest run the caps allow and the checker's tail.
	lease := time.Duration(s.LoadCaps.orDefault().MaxSeconds)*time.Second + 3*time.Minute
	return &queue.Worker{Queue: s.q, DB: s.DB, Kind: JobLoad, Handle: s.runLoadJob, Concurrency: concurrency, Lease: lease, Poll: 2 * time.Second}
}

// verdict compares a result with the run's thresholds.
func verdict(l LoadRun, r LoadResult) (string, []string) {
	reasons := []string{}
	if l.MaxP95Ms > 0 && r.LatencyMs.P95 > l.MaxP95Ms {
		reasons = append(reasons, fmt.Sprintf("p95 %d ms is over %d ms", r.LatencyMs.P95, l.MaxP95Ms))
	}
	if l.MaxErrorRate >= 0 && r.ErrorRate > l.MaxErrorRate {
		reasons = append(reasons, fmt.Sprintf("error rate %.2f%% is over %.2f%%", r.ErrorRate, l.MaxErrorRate))
	}
	if r.Requests == 0 {
		reasons = append(reasons, "no request was answered")
	}
	if len(reasons) > 0 {
		return "failed", reasons
	}
	return "passed", reasons
}

func (s *service) runLoadJob(ctx context.Context, job queue.Job) error {
	var p struct {
		LoadRunID string `json:"loadRunId"`
		ProjectID string `json:"projectId"`
		OrgID     string `json:"orgId"`
	}
	if err := json.Unmarshal(job.Payload, &p); err != nil || !uuidRe.MatchString(p.LoadRunID) || !uuidRe.MatchString(p.OrgID) {
		return nil
	}
	q := s.DB.Q(ctx)
	// No row lock: the run takes minutes, and its "running" shows at once
	// from another connection. One attempt only (MaxAttempts 1).
	l, err := scanLoad(q.QueryRow(ctx, `SELECT `+loadColumns+` FROM load_runs WHERE id = $1`, p.LoadRunID))
	if errors.Is(err, ErrNotFound) || (err == nil && l.Status != RunQueued) {
		return nil
	}
	if err != nil {
		return err
	}
	fail := func(msg string) error {
		_, err := q.Exec(ctx, `UPDATE load_runs SET status = 'error', error = $2, started_at = coalesce(started_at, clock_timestamp()),
			finished_at = clock_timestamp() WHERE id = $1`, l.ID, cut(msg, 1000))
		return err
	}
	if s.Loader == nil {
		return fail("load tests run in the checker service, which this worker does not have (URLCHECK=on)")
	}
	// The proof again, right before the traffic: a host that changed hands
	// since it was proved gets none.
	o, err := scanOwnership(q.QueryRow(ctx, `SELECT `+ownershipColumns+` FROM site_ownership WHERE org_id = $1 AND host = $2`, p.OrgID, l.Host))
	if errors.Is(err, ErrNotFound) || (err == nil && o.Status != OwnershipVerified) {
		return fail("the organization has not proved it owns " + l.Host)
	}
	if err != nil {
		return err
	}
	if err := s.prove(ctx, o, l.Base); err != nil {
		msg, ok := told(err)
		if !ok {
			msg = "the ownership proof could not be checked: " + err.Error()
		}
		if _, uerr := q.Exec(ctx, `UPDATE site_ownership SET status = 'failed', error = $2, checked_at = clock_timestamp() WHERE id = $1`, o.ID, cut(msg, 1000)); uerr != nil {
			return uerr
		}
		return fail("the ownership proof no longer holds: " + msg)
	}
	octx, cancel := outside()
	_, _ = s.DB.Q(octx).Exec(octx, `UPDATE load_runs SET status = 'running', started_at = clock_timestamp() WHERE id = $1`, l.ID)
	cancel()
	raw, err := s.Loader.Load(ctx, LoadSpec{Base: l.Base, Paths: l.Paths, VUs: l.VUs, Seconds: l.Seconds, RampSeconds: l.RampSeconds})
	if err != nil {
		if msg, ok := told(err); ok {
			return fail(msg)
		}
		return fail("the checker could not run the load test: " + err.Error())
	}
	var r LoadResult
	if err := json.Unmarshal(raw, &r); err != nil {
		return fail("the checker's answer was not a load result")
	}
	if r.Blocked {
		return fail(r.Error)
	}
	v, reasons := verdict(l, r)
	_, err = q.Exec(ctx, `UPDATE load_runs SET status = 'done', verdict = $2, reasons = $3, result = $4, finished_at = clock_timestamp() WHERE id = $1`,
		l.ID, v, reasons, []byte(raw))
	return err
}
