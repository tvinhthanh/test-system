package urlcheck

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/vinh/automation-testing-platform/backend/internal/platform/outbound"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/pg"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/queue"
)

type service struct {
	db      *pg.DB
	q       *queue.Queue
	checker Checker
}

// New returns the service. checker may be nil where nothing runs checks
// (the api only queues them).
func New(db *pg.DB, checker Checker) Service {
	return &service{db: db, q: queue.New(db), checker: checker}
}

var uuidRe = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// normalize accepts an http(s) address with a host and no credentials —
// they would be stored and shown to everyone in the project.
func normalize(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	u, err := url.Parse(s)
	switch {
	case s == "", err != nil, len(s) > 2048, u.Hostname() == "",
		u.Scheme != "http" && u.Scheme != "https", strings.ContainsAny(s, " \t\r\n"):
		return "", fmt.Errorf("%w: url must be an http(s) address with a host", ErrInvalid)
	case u.User != nil:
		return "", fmt.Errorf("%w: url must not carry a user or password", ErrInvalid)
	}
	u.Scheme, u.Host, u.Fragment = strings.ToLower(u.Scheme), strings.ToLower(u.Host), ""
	return u.String(), nil
}

func nullable(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func (s *service) Create(ctx context.Context, projectID, environmentID, raw, actor string) (Check, error) {
	if !uuidRe.MatchString(projectID) {
		return Check{}, ErrNotFound
	}
	if environmentID != "" && !uuidRe.MatchString(environmentID) {
		return Check{}, fmt.Errorf("%w: environmentId", ErrInvalid)
	}
	u, err := normalize(raw)
	if err != nil {
		return Check{}, err
	}
	actor = strings.TrimSpace(actor)
	if actor == "" || len(actor) > 254 {
		return Check{}, fmt.Errorf("%w: actor", ErrInvalid)
	}
	var c Check
	err = s.db.WithTx(ctx, func(ctx context.Context) error {
		q := s.db.Q(ctx)
		// One project's creates take turns, so MaxQueued holds under races.
		if _, err := q.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('urlcheck:' || $1, 0))`, projectID); err != nil {
			return err
		}
		var waiting int
		if err := q.QueryRow(ctx, `SELECT count(*) FROM url_checks WHERE project_id = $1 AND status = 'queued'`, projectID).Scan(&waiting); err != nil {
			return err
		}
		if waiting >= MaxQueued {
			return ErrBusy
		}
		c, err = scanCheck(q.QueryRow(ctx, `INSERT INTO url_checks (project_id, environment_id, url, requested_by)
			VALUES ($1, $2, $3, $4) RETURNING `+listColumns, projectID, nullable(environmentID), u, actor), false)
		if err != nil {
			return err
		}
		_, _, err = s.q.Add(ctx, queue.Enqueue{Kind: JobRun, Key: c.ID, MaxAttempts: 3,
			Payload: map[string]string{"checkId": c.ID, "projectId": projectID}})
		return err
	})
	return c, err
}

// listColumns: everything but the report and the screenshots themselves.
const listColumns = `id, project_id, coalesce(environment_id::text, ''), url, status, coalesce(verdict, ''),
	coalesce(report->>'title', ''), coalesce(report->>'finalUrl', ''), requested_by, coalesce(error, ''),
	desktop_shot IS NOT NULL, mobile_shot IS NOT NULL, created_at, finished_at,
	coalesce(jsonb_path_query_array(report, '$.checks[*].status'), '[]'::jsonb)`

func scanCheck(row interface{ Scan(dest ...any) error }, withReport bool) (Check, error) {
	var c Check
	var finished *time.Time
	var statuses json.RawMessage
	dest := []any{&c.ID, &c.ProjectID, &c.EnvironmentID, &c.URL, &c.Status, &c.Verdict, &c.Title, &c.FinalURL,
		&c.RequestedBy, &c.Error, &c.HasDesktop, &c.HasMobile, &c.CreatedAt, &finished, &statuses}
	var report []byte
	if withReport {
		dest = append(dest, &report)
	}
	if err := row.Scan(dest...); err != nil {
		if errors.Is(err, pg.ErrNoRows) {
			return Check{}, ErrNotFound
		}
		return Check{}, err
	}
	c.CreatedAt = c.CreatedAt.UTC()
	if finished != nil {
		c.FinishedAt = finished.UTC()
	}
	c.Statuses = []string{}
	_ = json.Unmarshal(statuses, &c.Statuses)
	if len(report) > 0 {
		c.Report = json.RawMessage(report)
	}
	return c, nil
}

func (s *service) List(ctx context.Context, projectID string, limit int) ([]Check, error) {
	if !uuidRe.MatchString(projectID) {
		return nil, ErrNotFound
	}
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	rows, err := s.db.Q(ctx).Query(ctx, `SELECT `+listColumns+` FROM url_checks WHERE project_id = $1
		ORDER BY created_at DESC, id DESC LIMIT $2`, projectID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Check{}
	for rows.Next() {
		c, err := scanCheck(rows, false)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (s *service) Get(ctx context.Context, projectID, id string) (Check, error) {
	if !uuidRe.MatchString(projectID) || !uuidRe.MatchString(id) {
		return Check{}, ErrNotFound
	}
	return scanCheck(s.db.Q(ctx).QueryRow(ctx, `SELECT `+listColumns+`, report FROM url_checks WHERE id = $1 AND project_id = $2`, id, projectID), true)
}

func (s *service) Screenshot(ctx context.Context, projectID, id, which string) ([]byte, error) {
	col := map[string]string{"desktop": "desktop_shot", "mobile": "mobile_shot"}[which]
	if col == "" || !uuidRe.MatchString(projectID) || !uuidRe.MatchString(id) {
		return nil, ErrNotFound
	}
	var b []byte
	err := s.db.Q(ctx).QueryRow(ctx, `SELECT `+col+` FROM url_checks WHERE id = $1 AND project_id = $2`, id, projectID).Scan(&b)
	if errors.Is(err, pg.ErrNoRows) || (err == nil && b == nil) {
		return nil, ErrNotFound
	}
	return b, err
}

func (s *service) Worker(concurrency int) *queue.Worker {
	// A check takes seconds, up to the checker client's 150 s deadline; the
	// lease outlasts that so a slow check is not claimed twice.
	return &queue.Worker{Queue: s.q, DB: s.db, Kind: JobRun, Handle: s.run, Concurrency: concurrency, Lease: 4 * time.Minute, Poll: 2 * time.Second}
}

// run is the job handler: idempotent (a check that is no longer queued is
// left alone), and a checker failure is retried by the queue — on the last
// attempt the check is closed as error instead, so it never stays queued.
func (s *service) run(ctx context.Context, job queue.Job) error {
	var p struct {
		CheckID   string `json:"checkId"`
		ProjectID string `json:"projectId"`
	}
	if err := json.Unmarshal(job.Payload, &p); err != nil || !uuidRe.MatchString(p.CheckID) || !uuidRe.MatchString(p.ProjectID) {
		return nil // nothing this job could ever do
	}
	q := s.db.Q(ctx)
	var target, status string
	err := q.QueryRow(ctx, `SELECT url, status FROM url_checks WHERE id = $1 AND project_id = $2 FOR UPDATE`, p.CheckID, p.ProjectID).Scan(&target, &status)
	if errors.Is(err, pg.ErrNoRows) || (err == nil && status != StatusQueued) {
		return nil // pruned, purged, or already finished by an earlier delivery
	}
	if err != nil {
		return err
	}
	if s.checker == nil {
		return errors.New("urlcheck: no checker configured in this process")
	}
	res, err := s.checker.Check(ctx, target)
	if err == nil && res.Verdict != "passed" && res.Verdict != "warning" && res.Verdict != "failed" {
		err = fmt.Errorf("checker answered verdict %q", res.Verdict)
	}
	if err != nil {
		if job.Attempts < job.MaxAttempts {
			return err
		}
		msg := "The checker could not be reached: " + err.Error()
		if len(msg) > 1000 {
			msg = msg[:1000]
		}
		_, err = q.Exec(ctx, `UPDATE url_checks SET status = 'error', error = $3, finished_at = now() WHERE id = $1 AND project_id = $2`, p.CheckID, p.ProjectID, msg)
		return err
	}
	_, err = q.Exec(ctx, `UPDATE url_checks SET status = 'done', verdict = $3, report = $4, desktop_shot = $5, mobile_shot = $6, finished_at = now()
		WHERE id = $1 AND project_id = $2`, p.CheckID, p.ProjectID, res.Verdict, res.Report, jpegOrNil(res.Desktop), jpegOrNil(res.Mobile))
	return err
}

// jpegOrNil keeps only what is a JPEG: the bytes are served back as
// image/jpeg, so nothing else may be stored under that name.
func jpegOrNil(b []byte) []byte {
	if len(b) < 3 || !bytes.Equal(b[:3], []byte{0xFF, 0xD8, 0xFF}) || len(b) > 3<<20 {
		return nil
	}
	return b
}

func (s *service) Prune(ctx context.Context, before time.Time, limit int) (int, error) {
	tag, err := s.db.Q(ctx).Exec(ctx, `DELETE FROM url_checks WHERE id IN (
		SELECT id FROM url_checks WHERE created_at < $1 ORDER BY created_at LIMIT $2)`, before.UTC(), limit)
	if err != nil {
		return 0, err
	}
	return int(tag.RowsAffected()), nil
}

func (s *service) PurgeProject(ctx context.Context, projectID string) error {
	if !uuidRe.MatchString(projectID) {
		return nil
	}
	_, err := s.db.Q(ctx).Exec(ctx, `DELETE FROM url_checks WHERE project_id = $1`, projectID)
	return err
}

// httpChecker is the checker service at URLCHECK_URL.
type httpChecker struct {
	base   string
	client *outbound.Client
}

// NewHTTPChecker talks to the checker service at base (URLCHECK_URL): one
// fixed host, no redirects, answers up to 8 MiB (two screenshots).
func NewHTTPChecker(base string) (Checker, error) {
	c, err := outbound.New(150*time.Second, base)
	if err != nil {
		return nil, err
	}
	return &httpChecker{base: strings.TrimSuffix(base, "/"), client: c.WithMaxBody(8 << 20)}, nil
}

func (h *httpChecker) Check(ctx context.Context, target string) (Result, error) {
	var out struct {
		Report      json.RawMessage `json:"report"`
		Screenshots struct {
			Desktop string `json:"desktop"`
			Mobile  string `json:"mobile"`
		} `json:"screenshots"`
	}
	if err := h.client.PostJSON(ctx, h.base+"/check", map[string]string{"url": target}, &out); err != nil {
		return Result{}, err
	}
	var head struct {
		Verdict string `json:"verdict"`
	}
	if err := json.Unmarshal(out.Report, &head); err != nil {
		return Result{}, fmt.Errorf("checker report: %w", err)
	}
	desktop, _ := base64.StdEncoding.DecodeString(out.Screenshots.Desktop)
	mobile, _ := base64.StdEncoding.DecodeString(out.Screenshots.Mobile)
	return Result{Report: out.Report, Verdict: head.Verdict, Desktop: desktop, Mobile: mobile}, nil
}

// Outliner reads a page's structure through the checker (QĐ-15: aisuggest
// drafts cases from it). Same fixed host and guard as a check; a page the
// checker cannot or may not open is an error.
type Outliner interface {
	Outline(ctx context.Context, url string) (json.RawMessage, error)
}

// NewHTTPOutliner talks to the checker service at base (URLCHECK_URL).
func NewHTTPOutliner(base string) (Outliner, error) {
	c, err := outbound.New(60*time.Second, base)
	if err != nil {
		return nil, err
	}
	return &httpChecker{base: strings.TrimSuffix(base, "/"), client: c.WithMaxBody(1 << 20)}, nil
}

func (h *httpChecker) Outline(ctx context.Context, target string) (json.RawMessage, error) {
	var out struct {
		Outline json.RawMessage `json:"outline"`
	}
	if err := h.client.PostJSON(ctx, h.base+"/outline", map[string]string{"url": target}, &out); err != nil {
		return nil, err
	}
	if len(out.Outline) == 0 || !json.Valid(out.Outline) {
		return nil, fmt.Errorf("checker outline: empty or not JSON")
	}
	return out.Outline, nil
}
