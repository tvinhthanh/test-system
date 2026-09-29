package triage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/vinh/automation-testing-platform/backend/internal/platform/pg"
	"github.com/vinh/automation-testing-platform/backend/internal/results"
)

type service struct {
	db      *pg.DB
	results results.Service
	now     func() time.Time
}

// New returns the Service over migrations/0008_triage.sql.
func New(db *pg.DB, res results.Service) Service {
	return &service{db: db, results: res, now: time.Now}
}

var uuidRe = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func isUUID(s string) bool { return uuidRe.MatchString(s) }

var eventTypes = map[EventType]bool{
	FailureSeen: true, HypothesisProposed: true, HypothesisAccepted: true, HypothesisRejected: true,
	MarkedFlaky: true, MarkedKnownIssue: true, Assigned: true, Resolved: true,
}

// failureSeenDedupe is §8.3: "FAILURE_SEEN (dedupe user+testResult/24h)".
const failureSeenDedupe = 24 * time.Hour

func (s *service) Record(ctx context.Context, in Record) (Event, bool, error) {
	if !isUUID(in.ProjectID) || !isUUID(in.TestResultID) {
		return Event{}, false, ErrNotFound
	}
	if !eventTypes[in.Type] {
		return Event{}, false, fmt.Errorf("%w: type %q", ErrInvalid, in.Type)
	}
	switch in.ActorType {
	case ActorUser, ActorAI, ActorSystem:
	default:
		return Event{}, false, fmt.Errorf("%w: actorType %q", ErrInvalid, in.ActorType)
	}
	if in.ActorType != ActorSystem && in.ActorID == "" {
		return Event{}, false, fmt.Errorf("%w: actorId required for %s", ErrInvalid, in.ActorType)
	}
	if in.HypothesisID != "" && !isUUID(in.HypothesisID) {
		return Event{}, false, fmt.Errorf("%w: hypothesisId", ErrInvalid)
	}
	if in.Category != "" && !ValidCategory(in.Category) {
		return Event{}, false, fmt.Errorf("%w: category %q is not in §8.7", ErrInvalid, in.Category)
	}
	if in.Type == HypothesisAccepted && in.Category == "" && in.HypothesisID == "" {
		return Event{}, false, fmt.Errorf("%w: HYPOTHESIS_ACCEPTED needs a category or a hypothesisId", ErrInvalid)
	}
	at := in.OccurredAt
	if at.IsZero() {
		at = s.now()
	}
	at = at.UTC()

	// The result must be the caller's: results scopes the lookup by project.
	tr, err := s.results.GetResult(ctx, in.ProjectID, results.TestResultID(in.TestResultID))
	if err != nil {
		if errors.Is(err, results.ErrNotFound) {
			return Event{}, false, ErrNotFound
		}
		return Event{}, false, err
	}

	payload := map[string]any{}
	for k, v := range in.Payload {
		payload[k] = v
	}
	if in.Category != "" {
		payload["category"] = string(in.Category)
	}
	payloadJSON, _ := json.Marshal(payload)

	var ev Event
	created := false
	err = s.db.WithTx(ctx, func(ctx context.Context) error {
		q := s.db.Q(ctx)
		if in.Type == FailureSeen {
			row := q.QueryRow(ctx, `
				SELECT `+columns+` FROM triage_events
				 WHERE test_result_id = $1 AND type = 'FAILURE_SEEN' AND actor_type = $2 AND actor_id = $3 AND occurred_at > $4
				 ORDER BY occurred_at LIMIT 1`, in.TestResultID, string(in.ActorType), in.ActorID, at.Add(-failureSeenDedupe))
			existing, err := scan(row)
			if err == nil {
				ev = existing
				return nil
			}
			if !errors.Is(err, pg.ErrNoRows) {
				return err
			}
		}
		var err error
		ev, err = scan(q.QueryRow(ctx, `
			INSERT INTO triage_events (project_id, test_result_id, run_id, type, actor_type, actor_id, hypothesis_id, payload, occurred_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
			RETURNING `+columns,
			in.ProjectID, in.TestResultID, string(tr.RunID), string(in.Type), string(in.ActorType), nullable(in.ActorID), nullable(in.HypothesisID), payloadJSON, at))
		if err != nil {
			return mapErr(err)
		}
		created = true
		// §8.7: the accepted category becomes the result's failureCategory.
		if in.Type == HypothesisAccepted && in.Category != "" {
			if err := s.results.SetFailureCategory(ctx, tr.ID, string(in.Category)); err != nil {
				return fmt.Errorf("set category: %w", err)
			}
		}
		return nil
	})
	if err != nil {
		return Event{}, false, err
	}
	return ev, created, nil
}

const columns = `id, project_id, test_result_id, run_id, type, actor_type, actor_id, hypothesis_id, payload, occurred_at`

func scan(row interface{ Scan(dest ...any) error }) (Event, error) {
	var e Event
	var actor, hyp *string
	err := row.Scan(&e.ID, &e.ProjectID, &e.TestResultID, &e.RunID, &e.Type, &e.ActorType, &actor, &hyp, &e.Payload, &e.OccurredAt)
	if err != nil {
		return Event{}, err
	}
	if actor != nil {
		e.ActorID = *actor
	}
	if hyp != nil {
		e.HypothesisID = *hyp
	}
	e.OccurredAt = e.OccurredAt.UTC()
	return e, nil
}

func (s *service) ListForResult(ctx context.Context, projectID, testResultID string) ([]Event, error) {
	if !isUUID(projectID) || !isUUID(testResultID) {
		return nil, ErrNotFound
	}
	rows, err := s.db.Q(ctx).Query(ctx, `
		SELECT `+columns+` FROM triage_events WHERE project_id = $1 AND test_result_id = $2 ORDER BY occurred_at, created_at`, projectID, testResultID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Event
	for rows.Next() {
		e, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func nullable(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func mapErr(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, pg.ErrNoRows):
		return ErrNotFound
	case pg.IsCheckViolation(err):
		return fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	return err
}

func (s *service) PurgeProject(ctx context.Context, projectID string) error {
	if !purgeUUID.MatchString(projectID) {
		return ErrNotFound
	}
	return s.db.WithTx(ctx, func(ctx context.Context) error {
		for _, q := range []string{
			"DELETE FROM ai_hypotheses WHERE project_id = $1",
			"DELETE FROM triage_events WHERE project_id = $1",
		} {
			if _, err := s.db.Q(ctx).Exec(ctx, q, projectID); err != nil {
				return fmt.Errorf("triage: purge project: %w", err)
			}
		}
		return nil
	})
}

var purgeUUID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
