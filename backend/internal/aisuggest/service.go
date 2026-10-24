package aisuggest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/vinh/automation-testing-platform/backend/internal/platform/llm"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/pg"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/queue"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/secretbox"
)

type service struct {
	Deps
	q   *queue.Queue
	box *secretbox.Box
}

// New returns the service. The api needs DB, Pepper, LLM (to check keys)
// and the server key; the worker also Pages.
func New(d Deps) Service {
	return &service{Deps: d, q: queue.New(d.DB), box: secretbox.New(d.Pepper, "atp llm api key v1")}
}

var uuidRe = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func nullable(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// ------------------------------------------------------------- settings

func (s *service) Settings(ctx context.Context, orgID string) (Settings, bool, error) {
	if !uuidRe.MatchString(orgID) {
		return Settings{}, false, ErrNotFound
	}
	st := Settings{OrgID: orgID}
	err := s.DB.Q(ctx).QueryRow(ctx, `SELECT model, key_sealed IS NOT NULL, key_hint, updated_by, updated_at FROM ai_settings WHERE org_id = $1`, orgID).
		Scan(&st.Model, &st.OwnKey, &st.KeyHint, &st.UpdatedBy, &st.UpdatedAt)
	if errors.Is(err, pg.ErrNoRows) {
		return Settings{}, false, nil
	}
	st.UpdatedAt = st.UpdatedAt.UTC()
	return st, err == nil, err
}

func (s *service) SaveSettings(ctx context.Context, orgID string, in SaveInput) (Settings, error) {
	if !uuidRe.MatchString(orgID) {
		return Settings{}, ErrNotFound
	}
	apiKey, actor, model := strings.TrimSpace(in.APIKey), strings.TrimSpace(in.Actor), in.Model
	if !llm.ValidModel(model) {
		return Settings{}, fmt.Errorf("%w: model", ErrInvalid)
	}
	if len(apiKey) > 500 || strings.ContainsAny(apiKey, " \t\r\n") {
		return Settings{}, fmt.Errorf("%w: apiKey", ErrInvalid)
	}
	if actor == "" || len(actor) > 254 {
		return Settings{}, fmt.Errorf("%w: actor", ErrInvalid)
	}
	key := apiKey
	switch {
	case !in.OwnKey && s.ServerKey == "":
		return Settings{}, fmt.Errorf("%w: keySource: the server has no API key; use your own", ErrInvalid)
	case !in.OwnKey:
		key = s.ServerKey
	case key == "":
		var err error
		if key, err = s.orgKey(ctx, orgID); err != nil {
			if errors.Is(err, pg.ErrNoRows) {
				return Settings{}, fmt.Errorf("%w: apiKey is required the first time", ErrInvalid)
			}
			return Settings{}, err
		}
	}
	// The provider says whether the key that will pay may use this model —
	// before anything is stored, so a typo fails here and not under a tester.
	if err := s.LLM.CheckKey(ctx, key, model); err != nil {
		return Settings{}, err
	}
	// The server's key is never stored; choosing it forgets any own key.
	var sealed []byte
	hint := ""
	if in.OwnKey {
		sealed = s.box.Seal(orgID, []byte(key))
		hint = key
		if len(hint) > 4 {
			hint = hint[len(hint)-4:]
		}
	}
	_, err := s.DB.Q(ctx).Exec(ctx, `
		INSERT INTO ai_settings (org_id, model, key_sealed, key_hint, updated_by, updated_at)
		VALUES ($1, $2, $3, $4, $5, now())
		ON CONFLICT (org_id) DO UPDATE SET model = EXCLUDED.model, key_sealed = EXCLUDED.key_sealed,
			key_hint = EXCLUDED.key_hint, updated_by = EXCLUDED.updated_by, updated_at = now()`,
		orgID, model, sealed, hint, actor)
	if err != nil {
		return Settings{}, err
	}
	st, _, err := s.Settings(ctx, orgID)
	return st, err
}

func (s *service) orgKey(ctx context.Context, orgID string) (string, error) {
	var sealed []byte
	if err := s.DB.Q(ctx).QueryRow(ctx, `SELECT key_sealed FROM ai_settings WHERE org_id = $1 AND key_sealed IS NOT NULL`, orgID).Scan(&sealed); err != nil {
		return "", err
	}
	key, err := s.box.Open(orgID, sealed)
	if err != nil {
		return "", fmt.Errorf("aisuggest: the stored key no longer opens (TOKEN_PEPPER changed?) — save it again: %w", err)
	}
	return string(key), nil
}

func (s *service) DeleteSettings(ctx context.Context, orgID string) (bool, error) {
	if !uuidRe.MatchString(orgID) {
		return false, ErrNotFound
	}
	tag, err := s.DB.Q(ctx).Exec(ctx, `DELETE FROM ai_settings WHERE org_id = $1`, orgID)
	return err == nil && tag.RowsAffected() > 0, err
}

func (s *service) Access(ctx context.Context, orgID string) (Access, error) {
	st, chosen, err := s.Settings(ctx, orgID)
	if err != nil {
		return Access{}, err
	}
	model := llm.DefaultModel
	if chosen {
		model = st.Model
	}
	switch {
	case st.OwnKey:
		return Access{Available: true, Source: SourceOrganization, Model: model}, nil
	case s.ServerKey != "":
		return Access{Available: true, Source: SourceServer, Model: model}, nil
	}
	return Access{Model: model}, nil
}

// credentials picks the key and model a job uses now: the organization's,
// else the server's.
func (s *service) credentials(ctx context.Context, orgID string) (key, model, source string, err error) {
	access, err := s.Access(ctx, orgID)
	switch {
	case err != nil:
		return "", "", "", err
	case access.Source == SourceOrganization:
		key, err = s.orgKey(ctx, orgID)
		return key, access.Model, SourceOrganization, err
	case access.Source == SourceServer:
		return s.ServerKey, access.Model, SourceServer, nil
	}
	return "", "", "", ErrNotConfigured
}

func (s *service) Credentials(ctx context.Context, orgID string) (key, model, source string, ok bool, err error) {
	if !uuidRe.MatchString(orgID) {
		return "", "", "", false, ErrNotFound
	}
	key, model, source, err = s.credentials(ctx, orgID)
	if errors.Is(err, ErrNotConfigured) {
		return "", "", "", false, nil
	}
	return key, model, source, err == nil, err
}

// ---------------------------------------------------------- suggestions

func (s *service) Request(ctx context.Context, in NewSuggestion) (Suggestion, error) {
	if !uuidRe.MatchString(in.ProjectID) || !uuidRe.MatchString(in.OrgID) {
		return Suggestion{}, ErrNotFound
	}
	if !ValidTestType(in.TestType) {
		return Suggestion{}, fmt.Errorf("%w: testType", ErrInvalid)
	}
	if in.EnvironmentID != "" && !uuidRe.MatchString(in.EnvironmentID) {
		return Suggestion{}, fmt.Errorf("%w: environmentId", ErrInvalid)
	}
	if in.URL = strings.TrimSpace(in.URL); in.URL != "" {
		u, err := url.Parse(in.URL)
		if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || len(in.URL) > 2048 {
			return Suggestion{}, fmt.Errorf("%w: url must be an http(s) address with a host and no credentials", ErrInvalid)
		}
	}
	if in.Context = strings.TrimSpace(in.Context); len(in.Context) > 4000 {
		return Suggestion{}, fmt.Errorf("%w: context is over 4000 bytes", ErrInvalid)
	}
	if in.Count == 0 {
		in.Count = 8
	}
	if in.Count < 1 || in.Count > MaxCount {
		return Suggestion{}, fmt.Errorf("%w: count is 1 to %d", ErrInvalid, MaxCount)
	}
	if in.Actor = strings.TrimSpace(in.Actor); in.Actor == "" || len(in.Actor) > 254 {
		return Suggestion{}, fmt.Errorf("%w: actor", ErrInvalid)
	}
	if access, err := s.Access(ctx, in.OrgID); err != nil {
		return Suggestion{}, err
	} else if !access.Available {
		return Suggestion{}, ErrNotConfigured
	}
	var out Suggestion
	err := s.DB.WithTx(ctx, func(ctx context.Context) error {
		q := s.DB.Q(ctx)
		// One project's requests take turns, so the limits hold under races.
		if _, err := q.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('aisuggest:' || $1, 0))`, in.ProjectID); err != nil {
			return err
		}
		var waiting, today int
		if err := q.QueryRow(ctx, `SELECT count(*) FILTER (WHERE status = 'queued'), count(*) FILTER (WHERE created_at > now() - interval '24 hours')
			FROM case_suggestions WHERE project_id = $1`, in.ProjectID).Scan(&waiting, &today); err != nil {
			return err
		}
		if waiting >= MaxQueued || today >= DailyLimit {
			return ErrBusy
		}
		var err error
		out, err = scan(q.QueryRow(ctx, `INSERT INTO case_suggestions (project_id, org_id, environment_id, test_type, url, context, count, requested_by)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING `+columns,
			in.ProjectID, in.OrgID, nullable(in.EnvironmentID), in.TestType, nullable(in.URL), in.Context, in.Count, in.Actor))
		if err != nil {
			return err
		}
		_, _, err = s.q.Add(ctx, queue.Enqueue{Kind: JobDraft, Key: out.ID, MaxAttempts: 3,
			Payload: map[string]string{"suggestionId": out.ID, "projectId": in.ProjectID}})
		return err
	})
	return out, err
}

const columns = `id, project_id, coalesce(environment_id::text, ''), test_type, coalesce(url, ''), context, count, status,
	coalesce(drafts, '[]'::jsonb), page_read, model, key_source, input_tokens, output_tokens, coalesce(error, ''),
	requested_by, created_at, finished_at`

func scan(row interface{ Scan(dest ...any) error }) (Suggestion, error) {
	var s Suggestion
	var drafts json.RawMessage
	var finished *time.Time
	err := row.Scan(&s.ID, &s.ProjectID, &s.EnvironmentID, &s.TestType, &s.URL, &s.Context, &s.Count, &s.Status,
		&drafts, &s.PageRead, &s.Model, &s.KeySource, &s.InputTokens, &s.OutputTokens, &s.Error,
		&s.RequestedBy, &s.CreatedAt, &finished)
	if errors.Is(err, pg.ErrNoRows) {
		return Suggestion{}, ErrNotFound
	}
	if err != nil {
		return Suggestion{}, err
	}
	s.CreatedAt = s.CreatedAt.UTC()
	if finished != nil {
		s.FinishedAt = finished.UTC()
	}
	s.Drafts = []Draft{}
	_ = json.Unmarshal(drafts, &s.Drafts)
	return s, nil
}

func (s *service) Get(ctx context.Context, projectID, id string) (Suggestion, error) {
	if !uuidRe.MatchString(projectID) || !uuidRe.MatchString(id) {
		return Suggestion{}, ErrNotFound
	}
	return scan(s.DB.Q(ctx).QueryRow(ctx, `SELECT `+columns+` FROM case_suggestions WHERE id = $1 AND project_id = $2`, id, projectID))
}

func (s *service) Worker(concurrency int) *queue.Worker {
	// Reading the page (≤ 45 s) and the model call (≤ 3 min, SDK retries
	// included) fit well inside the lease.
	return &queue.Worker{Queue: s.q, DB: s.DB, Kind: JobDraft, Handle: s.run, Concurrency: concurrency, Lease: 8 * time.Minute, Poll: 2 * time.Second}
}

// run drafts one suggestion. Idempotent: a suggestion no longer queued is
// left alone. A key or model problem ends it as error at once (retrying
// cannot fix it); rate limits and outages retry, and the last attempt ends
// it as error so it never stays queued.
func (s *service) run(ctx context.Context, job queue.Job) error {
	var p struct {
		SuggestionID string `json:"suggestionId"`
		ProjectID    string `json:"projectId"`
	}
	if err := json.Unmarshal(job.Payload, &p); err != nil || !uuidRe.MatchString(p.SuggestionID) || !uuidRe.MatchString(p.ProjectID) {
		return nil
	}
	q := s.DB.Q(ctx)
	var orgID string
	sug, err := scan(q.QueryRow(ctx, `SELECT `+columns+` FROM case_suggestions WHERE id = $1 AND project_id = $2 FOR UPDATE`, p.SuggestionID, p.ProjectID))
	if errors.Is(err, ErrNotFound) || (err == nil && sug.Status != StatusQueued) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := q.QueryRow(ctx, `SELECT org_id FROM case_suggestions WHERE id = $1`, sug.ID).Scan(&orgID); err != nil {
		return err
	}
	fail := func(msg string) error {
		msg = cut(msg, 1000)
		_, err := q.Exec(ctx, `UPDATE case_suggestions SET status = 'error', error = $2, finished_at = now() WHERE id = $1`, sug.ID, msg)
		return err
	}
	key, model, source, err := s.credentials(ctx, orgID)
	if err != nil {
		if errors.Is(err, ErrNotConfigured) {
			return fail("No API key: an organization admin adds one under Team → AI settings, or the server operator sets LLM_API_KEY.")
		}
		return fail(err.Error())
	}
	var outline json.RawMessage
	if sug.URL != "" && s.Pages != nil {
		pctx, cancel := context.WithTimeout(ctx, 45*time.Second)
		outline, err = s.Pages.Outline(pctx, sug.URL)
		cancel()
		if err != nil {
			outline = nil // drafts from the description alone; page_read says so
		}
	}
	res, err := s.LLM.JSON(ctx, llm.Request{APIKey: key, Model: model, System: systemPrompt, User: userPrompt(sug, outline), Schema: draftSchema})
	if err != nil {
		switch {
		case errors.Is(err, llm.ErrKey):
			return fail("The API key was rejected by Anthropic. Check it under Team → AI settings (or LLM_API_KEY on the server).")
		case errors.Is(err, llm.ErrModel):
			return fail("This key cannot use " + model + ". Pick another model under Team → AI settings.")
		case errors.Is(err, llm.ErrRefused):
			return fail("The model declined this request. Rephrase the description and try again.")
		case errors.Is(err, llm.ErrTruncated):
			return fail("The answer was too long and got cut off. Ask for fewer cases.")
		}
		if job.Attempts < job.MaxAttempts {
			return err
		}
		return fail("The AI service could not be reached: " + err.Error())
	}
	drafts, err := parseDrafts(res.JSON, sug.Count)
	if err != nil {
		return fail(err.Error())
	}
	b, err := json.Marshal(drafts)
	if err != nil {
		return err
	}
	_, err = q.Exec(ctx, `UPDATE case_suggestions SET status = 'done', drafts = $2, page_read = $3, model = $4, key_source = $5,
		input_tokens = $6, output_tokens = $7, finished_at = now() WHERE id = $1`,
		sug.ID, b, len(outline) > 0, model, source, res.InputTokens, res.OutputTokens)
	return err
}

func (s *service) Prune(ctx context.Context, before time.Time, limit int) (int, error) {
	tag, err := s.DB.Q(ctx).Exec(ctx, `DELETE FROM case_suggestions WHERE id IN (
		SELECT id FROM case_suggestions WHERE created_at < $1 ORDER BY created_at LIMIT $2)`, before.UTC(), limit)
	if err != nil {
		return 0, err
	}
	return int(tag.RowsAffected()), nil
}

func (s *service) PurgeProject(ctx context.Context, projectID string) error {
	if !uuidRe.MatchString(projectID) {
		return nil
	}
	_, err := s.DB.Q(ctx).Exec(ctx, `DELETE FROM case_suggestions WHERE project_id = $1`, projectID)
	return err
}

func (s *service) PurgeOrganization(ctx context.Context, orgID string) error {
	if !uuidRe.MatchString(orgID) {
		return nil
	}
	_, err := s.DB.Q(ctx).Exec(ctx, `DELETE FROM ai_settings WHERE org_id = $1`, orgID)
	return err
}
