package triage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/vinh/automation-testing-platform/backend/internal/platform/llm"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/pg"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/queue"
	"github.com/vinh/automation-testing-platform/backend/internal/results"
)

const investigationDailyLimit = 200

const investigationColumns = `id, project_id, test_result_id, status, requested_by, provider, model, key_source,
	turn_count, tool_count, input_tokens, output_tokens, coalesce(error,''), hypothesis_id, created_at, started_at, finished_at`

func scanInvestigation(row interface{ Scan(...any) error }) (Investigation, error) {
	var in Investigation
	var hypothesisID *string
	if err := row.Scan(&in.ID, &in.ProjectID, &in.ResultID, &in.Status, &in.RequestedBy, &in.Provider, &in.Model, &in.KeySource,
		&in.TurnCount, &in.ToolCount, &in.InputTokens, &in.OutputTokens, &in.Error, &hypothesisID, &in.CreatedAt, &in.StartedAt, &in.FinishedAt); err != nil {
		if errors.Is(err, pg.ErrNoRows) {
			return Investigation{}, ErrNotFound
		}
		return Investigation{}, err
	}
	in.CreatedAt = in.CreatedAt.UTC()
	if in.StartedAt != nil {
		x := in.StartedAt.UTC()
		in.StartedAt = &x
	}
	if in.FinishedAt != nil {
		x := in.FinishedAt.UTC()
		in.FinishedAt = &x
	}
	if hypothesisID != nil {
		in.Hypothesis = &Hypothesis{ID: *hypothesisID}
	}
	return in, nil
}

func (s *service) RequestInvestigation(ctx context.Context, in NewInvestigation) (Investigation, bool, error) {
	if !isUUID(in.ProjectID) || !isUUID(in.OrgID) || !isUUID(in.ResultID) || strings.TrimSpace(in.Actor) == "" {
		return Investigation{}, false, ErrInvalid
	}
	// The tenant boundary answers before anything else: another project's
	// result is not found, whether or not AI is configured here (§11.3.3).
	if err := s.ownResult(ctx, in.ProjectID, in.ResultID); err != nil {
		return Investigation{}, false, err
	}
	if s.evidence == nil || s.keys == nil || s.llm == nil {
		return Investigation{}, false, ErrNotConfigured
	}
	evidence, fingerprint, status, err := s.evidence.InvestigationEvidence(ctx, in.ProjectID, in.ResultID)
	if err != nil {
		// Not masked as "not found": the result exists (checked above), so
		// this is a failure the operator has to see.
		return Investigation{}, false, fmt.Errorf("triage: investigation evidence: %w", err)
	}
	if status != "failed" && status != "timed_out" && status != "interrupted" {
		return Investigation{}, false, fmt.Errorf("%w: result is not failed", ErrInvalid)
	}
	if len(evidence) == 0 || len(fingerprint) != 64 {
		return Investigation{}, false, fmt.Errorf("%w: evidence", ErrInvalid)
	}
	if _, _, ok, err := s.keys.Credentials(ctx, in.OrgID); err != nil {
		return Investigation{}, false, err
	} else if !ok {
		return Investigation{}, false, ErrNotConfigured
	}
	day := s.now().UTC().Format("2006-01-02")
	var out Investigation
	created := false
	err = s.db.WithTx(ctx, func(ctx context.Context) error {
		q := s.db.Q(ctx)
		if _, err := q.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('investigation:' || $1,0))`, in.OrgID); err != nil {
			return err
		}
		var count int
		if err := q.QueryRow(ctx, `SELECT count(*) FROM ai_investigations WHERE org_id=$1 AND requested_day=$2`, in.OrgID, day).Scan(&count); err != nil {
			return err
		}
		if count >= investigationDailyLimit {
			return ErrDailyLimit
		}
		existing, err := scanInvestigation(q.QueryRow(ctx, `SELECT `+investigationColumns+` FROM ai_investigations WHERE project_id=$1 AND dedupe_key=$2 AND requested_day=$3`, in.ProjectID, fingerprint, day))
		if err == nil {
			out = existing
			return nil
		}
		if !errors.Is(err, ErrNotFound) {
			return err
		}
		out, err = scanInvestigation(q.QueryRow(ctx, `INSERT INTO ai_investigations(project_id,org_id,test_result_id,stable_test_id,dedupe_key,requested_day,requested_by)
			SELECT $1,$2,$3,stable_test_id,$4,$5,$6 FROM test_results t JOIN runs r ON r.id=t.run_id WHERE t.id=$3 AND r.project_id=$1 RETURNING `+investigationColumns,
			in.ProjectID, in.OrgID, in.ResultID, fingerprint, day, strings.TrimSpace(in.Actor)))
		if err != nil {
			return err
		}
		_, _, err = s.q.Add(ctx, queue.Enqueue{Kind: JobInvestigation, Key: out.ID, MaxAttempts: 3, Payload: map[string]string{"id": out.ID, "projectId": in.ProjectID}})
		created = err == nil
		return err
	})
	return out, created, err
}

func (s *service) GetInvestigation(ctx context.Context, projectID, resultID, id string) (Investigation, error) {
	if !isUUID(projectID) || !isUUID(resultID) || !isUUID(id) {
		return Investigation{}, ErrNotFound
	}
	in, err := scanInvestigation(s.db.Q(ctx).QueryRow(ctx, `SELECT `+investigationColumns+` FROM ai_investigations WHERE id=$1 AND project_id=$2 AND test_result_id=$3`, id, projectID, resultID))
	if err != nil {
		return in, err
	}
	if in.Hypothesis != nil {
		h, err := s.loadHypothesis(ctx, projectID, resultID, in.Hypothesis.ID)
		if err != nil {
			return Investigation{}, err
		}
		in.Hypothesis = &h
	}
	return in, nil
}
func (s *service) ListInvestigations(ctx context.Context, projectID, resultID string, limit int) ([]Investigation, error) {
	if !isUUID(projectID) || !isUUID(resultID) {
		return nil, ErrNotFound
	}
	if limit < 1 || limit > 20 {
		limit = 20
	}
	// A result of another project is not found, not an empty list (§11.3.3).
	if err := s.ownResult(ctx, projectID, resultID); err != nil {
		return nil, err
	}
	rows, err := s.db.Q(ctx).Query(ctx, `SELECT `+investigationColumns+` FROM ai_investigations WHERE project_id=$1 AND test_result_id=$2 ORDER BY created_at DESC,id DESC LIMIT $3`, projectID, resultID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Investigation{}
	for rows.Next() {
		in, err := scanInvestigation(rows)
		if err != nil {
			return nil, err
		}
		if in.Hypothesis != nil {
			h, err := s.loadHypothesis(ctx, projectID, resultID, in.Hypothesis.ID)
			if err != nil {
				return nil, err
			}
			in.Hypothesis = &h
		}
		out = append(out, in)
	}
	return out, rows.Err()
}

// ownResult is nil when the result belongs to the project and its run is
// not deleted; results scopes the lookup by project.
func (s *service) ownResult(ctx context.Context, projectID, resultID string) error {
	if _, err := s.results.GetResult(ctx, projectID, results.TestResultID(resultID)); err != nil {
		if errors.Is(err, results.ErrNotFound) {
			return ErrNotFound
		}
		return err
	}
	return nil
}

func (s *service) loadHypothesis(ctx context.Context, projectID, resultID, id string) (Hypothesis, error) {
	var h Hypothesis
	var evidence json.RawMessage
	err := s.db.Q(ctx).QueryRow(ctx, `SELECT id,category,confidence,evidence,coalesce(suggested_action,''),coalesce((SELECT CASE type WHEN 'HYPOTHESIS_ACCEPTED' THEN 'accepted' ELSE 'rejected' END FROM triage_events WHERE hypothesis_id=ai_hypotheses.id AND type IN ('HYPOTHESIS_ACCEPTED','HYPOTHESIS_REJECTED') LIMIT 1),'') FROM ai_hypotheses WHERE id=$1 AND project_id=$2 AND test_result_id=$3`, id, projectID, resultID).Scan(&h.ID, &h.Category, &h.Confidence, &evidence, &h.SuggestedAction, &h.Decision)
	if errors.Is(err, pg.ErrNoRows) {
		return h, ErrNotFound
	}
	if err == nil {
		_ = json.Unmarshal(evidence, &h.Evidence)
	}
	return h, err
}

func (s *service) DecideHypothesis(ctx context.Context, in Decision) (Event, error) {
	if !isUUID(in.ProjectID) || !isUUID(in.ResultID) || !isUUID(in.HypothesisID) || strings.TrimSpace(in.Actor) == "" || (in.Decision != "accept" && in.Decision != "reject") {
		return Event{}, ErrInvalid
	}
	h, err := s.loadHypothesis(ctx, in.ProjectID, in.ResultID, in.HypothesisID)
	if err != nil {
		return Event{}, err
	}
	if h.Decision != "" {
		return Event{}, ErrDecided
	}
	typ := HypothesisRejected
	category := Category("")
	if in.Decision == "accept" {
		typ = HypothesisAccepted
		category = h.Category
		if in.Category != "" {
			category = in.Category
		}
		if !ValidCategory(category) {
			return Event{}, ErrInvalid
		}
	}
	ev, _, err := s.Record(ctx, Record{ProjectID: in.ProjectID, TestResultID: in.ResultID, Type: typ, ActorType: ActorUser, ActorID: in.Actor, HypothesisID: in.HypothesisID, Category: category, Payload: map[string]any{"note": cut(in.Note, 1000)}})
	return ev, err
}

func (s *service) InvestigationWorker(concurrency int) *queue.Worker {
	return &queue.Worker{Queue: s.q, DB: s.db, Kind: JobInvestigation, Handle: s.runInvestigation, Concurrency: concurrency, Lease: 5 * time.Minute, Poll: 2 * time.Second}
}

type modelAction struct {
	Action          string         `json:"action"`
	Tool            string         `json:"tool"`
	Category        Category       `json:"category"`
	Confidence      float64        `json:"confidence"`
	Evidence        []EvidenceFact `json:"evidence"`
	SuggestedAction string         `json:"suggestedAction"`
}

func (s *service) runInvestigation(ctx context.Context, job queue.Job) error {
	var p struct {
		ID        string `json:"id"`
		ProjectID string `json:"projectId"`
	}
	if json.Unmarshal(job.Payload, &p) != nil || !isUUID(p.ID) || !isUUID(p.ProjectID) {
		return nil
	}
	q := s.db.Q(ctx)
	in, err := scanInvestigation(q.QueryRow(ctx, `SELECT `+investigationColumns+` FROM ai_investigations WHERE id=$1 AND project_id=$2 FOR UPDATE`, p.ID, p.ProjectID))
	if errors.Is(err, ErrNotFound) || err == nil && (in.Status == InvestigationDone || in.Status == InvestigationError) {
		return nil
	}
	if err != nil {
		return err
	}
	fail := func(msg string) error {
		_, e := q.Exec(ctx, `UPDATE ai_investigations SET status='error',error=$2,finished_at=now() WHERE id=$1`, in.ID, cut(msg, 1000))
		return e
	}
	var orgID string
	if err := q.QueryRow(ctx, `SELECT org_id FROM ai_investigations WHERE id=$1`, in.ID).Scan(&orgID); err != nil {
		return err
	}
	acct, source, ok, err := s.keys.Credentials(ctx, orgID)
	if err != nil {
		return err
	}
	if !ok {
		return fail("No AI key is configured for this organization.")
	}
	raw, _, _, err := s.evidence.InvestigationEvidence(ctx, in.ProjectID, in.ResultID)
	if err != nil {
		return fail("The failure evidence is no longer available.")
	}
	var all map[string]json.RawMessage
	if json.Unmarshal(raw, &all) != nil {
		return fail("The failure evidence is invalid.")
	}
	observed := map[string]json.RawMessage{"failure_context": all["failureContext"]}
	toolCount := 1
	turns := 0
	input, output := 0, 0
	_, _ = q.Exec(ctx, `UPDATE ai_investigations SET status='investigating',provider=$2,model=$3,key_source=$4,started_at=coalesce(started_at,now()),tool_count=1 WHERE id=$1`, in.ID, acct.Provider, acct.Model, source)
	for turns < 4 {
		turns++
		prompt, _ := json.Marshal(map[string]any{"observations": observed, "instruction": "Request one allowed tool or finish with one source-backed hypothesis."})
		res, err := s.llm.JSON(ctx, llm.Request{Account: acct, System: investigationSystem, User: string(prompt), Schema: investigationSchema, MaxTokens: 2000})
		if err != nil {
			if job.Attempts < job.MaxAttempts {
				return err
			}
			return fail("The AI service could not complete this investigation: " + err.Error())
		}
		input += int(res.InputTokens)
		output += int(res.OutputTokens)
		var a modelAction
		if json.Unmarshal(res.JSON, &a) != nil {
			return fail("The AI answer was invalid.")
		}
		meta, _ := json.Marshal(map[string]any{"action": a.Action, "tool": a.Tool})
		_, _ = q.Exec(ctx, `INSERT INTO ai_investigation_steps(investigation_id,sequence,kind,request,input_tokens,output_tokens) VALUES($1,$2,'model',$3,$4,$5) ON CONFLICT DO NOTHING`, in.ID, (turns-1)*2+1, meta, res.InputTokens, res.OutputTokens)
		if a.Action == "finish" {
			if !ValidCategory(a.Category) || a.Confidence < 0 || a.Confidence > 1 || len(a.Evidence) < 1 || len(a.Evidence) > 10 {
				return fail("The AI hypothesis did not satisfy the investigation contract.")
			}
			for _, f := range a.Evidence {
				if _, ok := observed[f.Source]; !ok || strings.TrimSpace(f.Fact) == "" {
					return fail("The AI hypothesis cited evidence that was not observed.")
				}
			}
			evidence, _ := json.Marshal(a.Evidence)
			var hypID string
			err = q.QueryRow(ctx, `INSERT INTO ai_hypotheses(project_id,test_result_id,category,confidence,evidence,suggested_action,model,prompt_version,investigation_id) VALUES($1,$2,$3,$4,$5,$6,$7,'investigation-v1',$8) RETURNING id`, in.ProjectID, in.ResultID, a.Category, a.Confidence, evidence, cut(a.SuggestedAction, 1000), res.ServedBy(acct.Model), in.ID).Scan(&hypID)
			if err != nil {
				return err
			}
			tr, err := s.results.GetResult(ctx, in.ProjectID, results.TestResultID(in.ResultID))
			if err != nil {
				return err
			}
			payload, _ := json.Marshal(map[string]any{"category": a.Category, "confidence": a.Confidence})
			if _, err = q.Exec(ctx, `INSERT INTO triage_events(project_id,test_result_id,run_id,type,actor_type,actor_id,hypothesis_id,payload,occurred_at) VALUES($1,$2,$3,'HYPOTHESIS_PROPOSED','AI',$4,$5,$6,now())`, in.ProjectID, in.ResultID, string(tr.RunID), res.ServedBy(acct.Model), hypID, payload); err != nil {
				return err
			}
			_, err = q.Exec(ctx, `UPDATE ai_investigations SET status='done',hypothesis_id=$2,model=$3,turn_count=$4,tool_count=$5,input_tokens=$6,output_tokens=$7,finished_at=now() WHERE id=$1`, in.ID, hypID, res.ServedBy(acct.Model), turns, toolCount, input, output)
			return err
		}
		key := map[string]string{"stable_history": "stableHistory", "compare_last_pass": "stableHistory", "triage_history": "triageHistory"}[a.Tool]
		if key == "" || toolCount >= 6 {
			return fail("The AI requested an unavailable evidence tool.")
		}
		obs := all[key]
		if len(obs) == 0 {
			obs = json.RawMessage(`[]`)
		}
		observed[a.Tool] = obs
		toolCount++
		_, _ = q.Exec(ctx, `INSERT INTO ai_investigation_steps(investigation_id,sequence,kind,tool,observation) VALUES($1,$2,'tool',$3,$4) ON CONFLICT DO NOTHING`, in.ID, (turns-1)*2+2, a.Tool, obs)
	}
	return fail("The investigation reached its turn limit without a conclusion.")
}

func cut(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

const investigationSystem = `You investigate test failures. Use only supplied observations. Choose one allowed tool (stable_history, compare_last_pass, triage_history) or finish. Never invent evidence. Return exactly the requested JSON.`

var investigationSchema = map[string]any{"type": "object", "additionalProperties": false, "required": []string{"action", "tool", "category", "confidence", "evidence", "suggestedAction"}, "properties": map[string]any{
	"action": map[string]any{"type": "string", "enum": []string{"call_tool", "finish"}}, "tool": map[string]any{"type": "string", "enum": []string{"", "stable_history", "compare_last_pass", "triage_history"}}, "category": map[string]any{"type": "string", "enum": []string{"app_bug", "test_bug", "flaky_timing", "env_infra", "env_data", "dependency_external", "unknown"}}, "confidence": map[string]any{"type": "number", "minimum": 0, "maximum": 1}, "evidence": map[string]any{"type": "array", "maxItems": 10, "items": map[string]any{"type": "object", "additionalProperties": false, "required": []string{"source", "fact"}, "properties": map[string]any{"source": map[string]any{"type": "string"}, "fact": map[string]any{"type": "string", "maxLength": 2000}}}}, "suggestedAction": map[string]any{"type": "string", "maxLength": 1000}}}
