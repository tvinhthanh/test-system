package testmgmt

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"
)

// ------------------------------------------------------------------ plans

const planColumns = `id, project_id, name, environment_id, coalesce(test_type, ''), status, created_at, updated_at`

func scanPlan(row interface{ Scan(dest ...any) error }) (Plan, error) {
	var p Plan
	var env *string
	if err := row.Scan(&p.ID, &p.ProjectID, &p.Name, &env, &p.TestType, &p.Status, &p.CreatedAt, &p.UpdatedAt); err != nil {
		return Plan{}, err
	}
	p.EnvironmentID = deref(env)
	return p, nil
}

func (s *service) CreatePlan(ctx context.Context, projectID string, in NewPlan) (Plan, error) {
	if !isUUID(projectID) {
		return Plan{}, ErrNotFound
	}
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" || len(in.Name) > 200 {
		return Plan{}, fmt.Errorf("%w: name", ErrInvalid)
	}
	if in.EnvironmentID != "" && !isUUID(in.EnvironmentID) {
		return Plan{}, fmt.Errorf("%w: environmentId", ErrInvalid)
	}
	if in.TestType != "" && !slices.Contains(PlanTestTypes, in.TestType) {
		return Plan{}, fmt.Errorf("%w: testType", ErrInvalid)
	}
	var p Plan
	err := s.db.WithTx(ctx, func(ctx context.Context) error {
		q := s.db.Q(ctx)
		var err error
		p, err = scanPlan(q.QueryRow(ctx, `INSERT INTO test_plans (project_id, name, environment_id, test_type) VALUES ($1, $2, $3, $4) RETURNING `+planColumns, projectID, in.Name, nullable(in.EnvironmentID), nullable(in.TestType)))
		if err != nil {
			return mapErr(err)
		}
		ids := in.CaseIDs
		if ids == nil {
			rows, err := q.Query(ctx, `SELECT id FROM test_cases WHERE project_id = $1 AND automation_status <> 'obsolete' ORDER BY id LIMIT 5001`, projectID)
			if err != nil {
				return err
			}
			selected := []string{}
			for rows.Next() {
				var id string
				if err := rows.Scan(&id); err != nil {
					rows.Close()
					return err
				}
				selected = append(selected, id)
			}
			err = rows.Err()
			rows.Close()
			if err != nil {
				return err
			}
			ids = &selected
		}
		if err := s.setPlanCases(ctx, projectID, p.ID, *ids); err != nil {
			return err
		}
		return s.auditPlan(ctx, projectID, p.ID, in.Actor, "plan.create", map[string]any{"caseIds": *ids})
	})
	if err != nil {
		return Plan{}, err
	}
	return s.GetPlan(ctx, projectID, p.ID)
}

func (s *service) ListPlans(ctx context.Context, projectID string) ([]Plan, error) {
	if !isUUID(projectID) {
		return nil, ErrNotFound
	}
	rows, err := s.db.Q(ctx).Query(ctx, `SELECT `+planColumns+` FROM test_plans WHERE project_id = $1 ORDER BY created_at DESC LIMIT 200`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Plan{}
	for rows.Next() {
		p, err := scanPlan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *service) GetPlan(ctx context.Context, projectID, id string) (Plan, error) {
	if !isUUID(projectID) || !isUUID(id) {
		return Plan{}, ErrNotFound
	}
	q := s.db.Q(ctx)
	p, err := scanPlan(q.QueryRow(ctx, `SELECT `+planColumns+` FROM test_plans WHERE id = $1 AND project_id = $2`, id, projectID))
	if err != nil {
		return Plan{}, mapErr(err)
	}
	members, err := q.Query(ctx, `SELECT `+caseColumns+`, pc.assignee_user_id, pc.assignee_email, pc.assigned_by, pc.assigned_at, pc.work_status
		FROM test_plan_cases pc JOIN test_cases c ON c.id = pc.test_case_id AND c.project_id = pc.project_id
		LEFT JOIN test_suites s ON s.id = c.suite_id WHERE pc.test_plan_id = $1 AND pc.project_id = $2 ORDER BY c.priority, c.title, c.id`, id, projectID)
	if err != nil {
		return Plan{}, err
	}
	p.Cases = []PlanCase{}
	for members.Next() {
		var item PlanCase
		var user *string
		item.TestCase, err = scanCaseExtra(members, &user, &item.AssigneeEmail, &item.AssignedBy, &item.AssignedAt, &item.WorkStatus)
		if err != nil {
			members.Close()
			return Plan{}, err
		}
		item.AssigneeUserID = deref(user)
		p.Cases = append(p.Cases, item)
	}
	err = members.Err()
	members.Close()
	if err != nil {
		return Plan{}, err
	}
	// Latest verdict per case (append-only rows, §6.8.2 ManualExecution).
	rows, err := q.Query(ctx, `
		SELECT DISTINCT ON (e.test_case_id) `+executionColumns+`
		  FROM manual_executions e JOIN test_cases c ON c.id = e.test_case_id
		 WHERE e.test_plan_id = $1 AND e.project_id = $2
		 ORDER BY e.test_case_id, e.executed_at DESC, e.id DESC`, id, projectID)
	if err != nil {
		return Plan{}, err
	}
	defer rows.Close()
	p.Executions = []Execution{}
	for rows.Next() {
		e, err := scanExecution(rows)
		if err != nil {
			return Plan{}, err
		}
		p.Executions = append(p.Executions, e)
	}
	if err := rows.Err(); err != nil {
		return Plan{}, err
	}
	rows.Close()
	history, err := q.Query(ctx, `SELECT `+executionColumns+`
		FROM manual_executions e JOIN test_cases c ON c.id = e.test_case_id
		WHERE e.test_plan_id = $1 AND e.project_id = $2
		ORDER BY e.executed_at DESC, e.id DESC LIMIT 201`, id, projectID)
	if err != nil {
		return Plan{}, err
	}
	defer history.Close()
	p.ExecutionHistory = []Execution{}
	for history.Next() {
		e, err := scanExecution(history)
		if err != nil {
			return Plan{}, err
		}
		if len(p.ExecutionHistory) == 200 {
			p.HistoryTruncated = true
			break
		}
		p.ExecutionHistory = append(p.ExecutionHistory, e)
	}
	return p, history.Err()
}

const executionColumns = `e.id, e.test_plan_id, e.test_case_id,
	COALESCE(e.case_snapshot->>'title', c.title), e.executed_by, e.status, e.note,
	e.executed_at, e.case_snapshot, e.step_results, e.bug_url, e.retest_of`

func scanExecution(row interface{ Scan(...any) error }) (Execution, error) {
	var e Execution
	var snapshot, steps []byte
	var retest *string
	if err := row.Scan(&e.ID, &e.PlanID, &e.TestCaseID, &e.CaseTitle, &e.ExecutedBy,
		&e.Status, &e.Note, &e.ExecutedAt, &snapshot, &steps, &e.BugURL, &retest); err != nil {
		return e, err
	}
	if snapshot != nil {
		if err := json.Unmarshal(snapshot, &e.CaseSnapshot); err != nil {
			return e, err
		}
	}
	if err := json.Unmarshal(steps, &e.StepResults); err != nil {
		return e, err
	}
	e.RetestOf = deref(retest)
	return e, nil
}

func (s *service) SetPlanStatus(ctx context.Context, projectID, id string, status PlanStatus) (Plan, error) {
	return s.UpdatePlan(ctx, projectID, id, PlanPatch{Status: &status}, "")
}

func (s *service) UpdatePlan(ctx context.Context, projectID, id string, patch PlanPatch, actor string) (Plan, error) {
	if !isUUID(projectID) || !isUUID(id) {
		return Plan{}, ErrNotFound
	}
	if patch.Status == nil && patch.CaseIDs == nil && patch.CaseUpdate == nil && patch.EnvironmentID == nil {
		return Plan{}, fmt.Errorf("%w: empty plan patch", ErrInvalid)
	}
	if patch.EnvironmentID != nil && *patch.EnvironmentID != "" && !isUUID(*patch.EnvironmentID) {
		return Plan{}, fmt.Errorf("%w: environmentId", ErrInvalid)
	}
	if patch.Status != nil {
		switch *patch.Status {
		case PlanDraft, PlanActive, PlanClosed:
		default:
			return Plan{}, fmt.Errorf("%w: status", ErrInvalid)
		}
	}
	err := s.db.WithTx(ctx, func(ctx context.Context) error {
		q := s.db.Q(ctx)
		p, err := scanPlan(q.QueryRow(ctx, `SELECT `+planColumns+` FROM test_plans WHERE id = $1 AND project_id = $2 FOR UPDATE`, id, projectID))
		if err != nil {
			return mapErr(err)
		}
		if !patch.ExpectedUpdatedAt.IsZero() && !patch.ExpectedUpdatedAt.Equal(p.UpdatedAt) {
			return fmt.Errorf("%w: plan changed; refresh before saving", ErrConflict)
		}
		if (patch.CaseIDs != nil || patch.CaseUpdate != nil || patch.EnvironmentID != nil) && p.Status == PlanClosed {
			return fmt.Errorf("%w: plan is closed", ErrConflict)
		}
		if patch.CaseIDs != nil {
			if err := s.setPlanCases(ctx, projectID, id, *patch.CaseIDs); err != nil {
				return err
			}
			if err := s.auditPlan(ctx, projectID, id, actor, "plan.scope", map[string]any{"caseIds": *patch.CaseIDs}); err != nil {
				return err
			}
		}
		if update := patch.CaseUpdate; update != nil {
			if !isUUID(update.TestCaseID) {
				return ErrNotFound
			}
			if update.AssigneeUserID == nil && update.WorkStatus == nil {
				return fmt.Errorf("%w: empty case update", ErrInvalid)
			}
			if update.AssigneeUserID != nil {
				if update.AssigneeEmail == nil || (*update.AssigneeUserID != "" && !isUUID(*update.AssigneeUserID)) {
					return fmt.Errorf("%w: assignee", ErrInvalid)
				}
				tag, err := q.Exec(ctx, `UPDATE test_plan_cases SET assignee_user_id = $4, assignee_email = $5, assigned_by = $6, assigned_at = now() WHERE test_plan_id = $1 AND project_id = $2 AND test_case_id = $3`, id, projectID, update.TestCaseID, nullable(*update.AssigneeUserID), *update.AssigneeEmail, actor)
				if err != nil {
					return mapErr(err)
				}
				if tag.RowsAffected() == 0 {
					return ErrNotFound
				}
				if err := s.auditPlan(ctx, projectID, id, actor, "plan.assignment", map[string]any{"testCaseId": update.TestCaseID, "assigneeUserId": *update.AssigneeUserID, "assigneeEmail": *update.AssigneeEmail}); err != nil {
					return err
				}
			}
			if update.WorkStatus != nil {
				if *update.WorkStatus != "in_progress" && *update.WorkStatus != "not_run" {
					return fmt.Errorf("%w: workStatus", ErrInvalid)
				}
				if *update.WorkStatus == "not_run" {
					var hasResult bool
					if err := q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM manual_executions WHERE test_plan_id = $1 AND test_case_id = $2)`, id, update.TestCaseID).Scan(&hasResult); err != nil {
						return err
					}
					if hasResult {
						return fmt.Errorf("%w: recorded case cannot become not_run", ErrConflict)
					}
				}
				tag, err := q.Exec(ctx, `UPDATE test_plan_cases pc SET work_status = $4 FROM test_cases c WHERE pc.test_plan_id = $1 AND pc.project_id = $2 AND pc.test_case_id = $3 AND c.id = pc.test_case_id AND c.automation_status <> 'obsolete'`, id, projectID, update.TestCaseID, *update.WorkStatus)
				if err != nil {
					return mapErr(err)
				}
				if tag.RowsAffected() == 0 {
					return ErrNotFound
				}
				if err := s.auditPlan(ctx, projectID, id, actor, "plan.workflow", map[string]any{"testCaseId": update.TestCaseID, "workStatus": *update.WorkStatus}); err != nil {
					return err
				}
			}
		}
		status := p.Status
		if patch.CaseUpdate != nil && patch.CaseUpdate.WorkStatus != nil && *patch.CaseUpdate.WorkStatus == "in_progress" && status == PlanDraft {
			status = PlanActive
		}
		if patch.Status != nil {
			status = *patch.Status
		}
		if _, err := q.Exec(ctx, `UPDATE test_plans SET status = $3, updated_at = clock_timestamp() WHERE id = $1 AND project_id = $2`, id, projectID, status); err != nil {
			return mapErr(err)
		}
		if env := patch.EnvironmentID; env != nil && *env != p.EnvironmentID {
			if _, err := q.Exec(ctx, `UPDATE test_plans SET environment_id = $3 WHERE id = $1 AND project_id = $2`, id, projectID, nullable(*env)); err != nil {
				return mapErr(err)
			}
			if err := s.auditPlan(ctx, projectID, id, actor, "plan.environment", map[string]any{"environmentId": []string{p.EnvironmentID, *env}}); err != nil {
				return err
			}
		}
		if patch.Status != nil {
			return s.auditPlan(ctx, projectID, id, actor, "plan.status", map[string]any{"status": status})
		}
		return nil
	})
	if err != nil {
		return Plan{}, err
	}
	return s.GetPlan(ctx, projectID, id)
}

func (s *service) setPlanCases(ctx context.Context, projectID, planID string, ids []string) error {
	if len(ids) > 5000 {
		return fmt.Errorf("%w: more than 5000 plan cases", ErrInvalid)
	}
	if ids == nil {
		ids = []string{}
	}
	seen := map[string]bool{}
	for _, id := range ids {
		if !isUUID(id) {
			return ErrNotFound
		}
		if seen[id] {
			return fmt.Errorf("%w: duplicate case", ErrInvalid)
		}
		seen[id] = true
	}
	q := s.db.Q(ctx)
	var invalid bool
	if err := q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM unnest($3::uuid[]) selected(id)
		WHERE NOT EXISTS(SELECT 1 FROM test_cases c WHERE c.id = selected.id AND c.project_id = $1
		AND (c.automation_status <> 'obsolete' OR EXISTS(SELECT 1 FROM test_plan_cases pc WHERE pc.test_plan_id = $2 AND pc.test_case_id = c.id))))`, projectID, planID, ids).Scan(&invalid); err != nil {
		return err
	}
	if invalid {
		return ErrNotFound
	}
	var removesEvidence bool
	if err := q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM manual_executions WHERE test_plan_id = $1 AND NOT (test_case_id = ANY($2::uuid[])))`, planID, ids).Scan(&removesEvidence); err != nil {
		return err
	}
	if removesEvidence {
		return fmt.Errorf("%w: cannot remove cases with recorded executions", ErrConflict)
	}
	if _, err := q.Exec(ctx, `DELETE FROM test_plan_cases WHERE test_plan_id = $1 AND project_id = $2 AND NOT (test_case_id = ANY($3::uuid[]))`, planID, projectID, ids); err != nil {
		return err
	}
	_, err := q.Exec(ctx, `INSERT INTO test_plan_cases(test_plan_id, test_case_id, project_id) SELECT $1, selected, $2 FROM unnest($3::uuid[]) selected ON CONFLICT DO NOTHING`, planID, projectID, ids)
	return mapErr(err)
}

func (s *service) auditPlan(ctx context.Context, projectID, planID, actor, action string, details map[string]any) error {
	if actor == "" {
		actor = "system"
	}
	data, err := json.Marshal(details)
	if err != nil {
		return err
	}
	_, err = s.db.Q(ctx).Exec(ctx, `INSERT INTO test_plan_audit(project_id, test_plan_id, actor, action, details) VALUES ($1, $2, $3, $4, $5)`, projectID, planID, actor, action, data)
	return err
}

func (s *service) RecordExecution(ctx context.Context, projectID, planID string, in NewExecution, actor string) (Execution, error) {
	if !isUUID(projectID) || !isUUID(planID) || !isUUID(in.TestCaseID) {
		return Execution{}, ErrNotFound
	}
	switch in.Status {
	case ExecPassed, ExecFailed, ExecBlocked, ExecSkipped:
	default:
		return Execution{}, fmt.Errorf("%w: status %q", ErrInvalid, in.Status)
	}
	if actor == "" {
		return Execution{}, fmt.Errorf("%w: actor required", ErrInvalid)
	}
	if len(in.Note) > 4000 {
		return Execution{}, fmt.Errorf("%w: note too long", ErrInvalid)
	}
	in.BugURL = strings.TrimSpace(in.BugURL)
	if in.BugURL != "" {
		u, err := url.Parse(in.BugURL)
		if err != nil || len(in.BugURL) > 2000 || (u.Scheme != "https" && u.Scheme != "http") || u.Hostname() == "" || u.User != nil {
			return Execution{}, fmt.Errorf("%w: bugUrl must be an http(s) URL without credentials", ErrInvalid)
		}
	}
	if in.RetestOf != "" && !isUUID(in.RetestOf) {
		return Execution{}, ErrNotFound
	}
	// Back-dating a paper record is fine; a future executedAt is not: the
	// newest executed_at is the case's verdict, so it would pin it (5 min of
	// clock skew allowed, as for specialized reports).
	at := in.ExecutedAt
	if at.IsZero() {
		at = s.now()
	} else if at.After(s.now().Add(5 * time.Minute)) {
		return Execution{}, fmt.Errorf("%w: executedAt is in the future", ErrInvalid)
	}
	var out Execution
	err := s.db.WithTx(ctx, func(ctx context.Context) error {
		q := s.db.Q(ctx)
		var status string
		if err := q.QueryRow(ctx, `SELECT status FROM test_plans WHERE id = $1 AND project_id = $2 FOR UPDATE`, planID, projectID).Scan(&status); err != nil {
			return mapErr(err)
		}
		if status == string(PlanClosed) {
			return fmt.Errorf("%w: plan is closed", ErrConflict)
		}
		var member bool
		if err := q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM test_plan_cases WHERE test_plan_id = $1 AND test_case_id = $2 AND project_id = $3)`, planID, in.TestCaseID, projectID).Scan(&member); err != nil {
			return err
		}
		if !member {
			return ErrNotFound
		}
		var snapshot CaseSnapshot
		var rawSteps, rawTaxonomy []byte
		var preconditions, testData string
		if err := q.QueryRow(ctx, `SELECT title, steps, expected, updated_at, preconditions, test_data, taxonomy FROM test_cases WHERE id = $1 AND project_id = $2 AND automation_status <> 'obsolete' FOR SHARE`, in.TestCaseID, projectID).Scan(&snapshot.Title, &rawSteps, &snapshot.Expected, &snapshot.UpdatedAt, &preconditions, &testData, &rawTaxonomy); err != nil {
			return mapErr(err)
		}
		snapshot.Preconditions = &preconditions
		snapshot.TestData = &testData
		if err := json.Unmarshal(rawTaxonomy, &snapshot.Taxonomy); err != nil {
			return err
		}
		if err := json.Unmarshal(rawSteps, &snapshot.Steps); err != nil {
			return err
		}
		if !in.CaseUpdatedAt.IsZero() && !in.CaseUpdatedAt.Equal(snapshot.UpdatedAt) {
			return fmt.Errorf("%w: test case changed; refresh before recording", ErrConflict)
		}
		seen := map[int]bool{}
		for _, step := range in.StepResults {
			if in.Status == ExecPassed && (step.Status == ExecFailed || step.Status == ExecBlocked) {
				return fmt.Errorf("%w: passed execution cannot contain failed or blocked steps", ErrInvalid)
			}
			if step.StepIndex < 0 || step.StepIndex >= len(snapshot.Steps) || seen[step.StepIndex] || len(step.Actual) > 4000 {
				return fmt.Errorf("%w: invalid step results", ErrInvalid)
			}
			switch step.Status {
			case ExecPassed, ExecFailed, ExecBlocked, ExecSkipped:
			default:
				return fmt.Errorf("%w: step status", ErrInvalid)
			}
			seen[step.StepIndex] = true
		}
		if in.RetestOf != "" {
			var previous string
			if err := q.QueryRow(ctx, `SELECT id FROM manual_executions WHERE id = $1 AND project_id = $2 AND test_plan_id = $3 AND test_case_id = $4`, in.RetestOf, projectID, planID, in.TestCaseID).Scan(&previous); err != nil {
				return mapErr(err)
			}
		}
		if in.StepResults == nil {
			in.StepResults = []StepResult{}
		}
		snapshotJSON, err := json.Marshal(snapshot)
		if err != nil {
			return err
		}
		stepsJSON, err := json.Marshal(in.StepResults)
		if err != nil {
			return err
		}
		err = q.QueryRow(ctx, `
			INSERT INTO manual_executions (project_id, test_plan_id, test_case_id, executed_by, status, note, executed_at, case_snapshot, step_results, bug_url, retest_of)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
			RETURNING id, test_plan_id, test_case_id, executed_by, status, note, executed_at`,
			projectID, planID, in.TestCaseID, actor, string(in.Status), strings.TrimSpace(in.Note), at.UTC(), snapshotJSON, stepsJSON, in.BugURL, nullable(in.RetestOf)).
			Scan(&out.ID, &out.PlanID, &out.TestCaseID, &out.ExecutedBy, &out.Status, &out.Note, &out.ExecutedAt)
		if err != nil {
			return mapErr(err)
		}
		out.CaseTitle = snapshot.Title
		out.CaseSnapshot = &snapshot
		out.StepResults = in.StepResults
		out.BugURL = in.BugURL
		out.RetestOf = in.RetestOf
		if _, err := q.Exec(ctx, `UPDATE test_plan_cases SET work_status = 'completed' WHERE test_plan_id = $1 AND test_case_id = $2`, planID, in.TestCaseID); err != nil {
			return err
		}
		if _, err := q.Exec(ctx, `UPDATE test_plans SET status = CASE WHEN status = 'draft' THEN 'active' ELSE status END, updated_at = clock_timestamp() WHERE id = $1`, planID); err != nil {
			return err
		}
		return s.auditPlan(ctx, projectID, planID, actor, "plan.execution", map[string]any{"executionId": out.ID, "testCaseId": in.TestCaseID, "status": in.Status})
	})
	if err != nil {
		return Execution{}, err
	}
	return out, nil
}

func (s *service) ExecutionForEvidence(ctx context.Context, projectID, planID, id string) (Execution, error) {
	if !isUUID(projectID) || !isUUID(planID) || !isUUID(id) {
		return Execution{}, ErrNotFound
	}
	var e Execution
	var planStatus string
	err := s.db.Q(ctx).QueryRow(ctx, `
		SELECT e.id, e.test_plan_id, e.test_case_id, COALESCE(e.case_snapshot->>'title', c.title), e.executed_by, e.status, e.note, e.executed_at, p.status
		  FROM manual_executions e
		  JOIN test_cases c ON c.id = e.test_case_id
		  JOIN test_plans p ON p.id = e.test_plan_id
		 WHERE e.id = $1 AND e.test_plan_id = $2 AND e.project_id = $3`, id, planID, projectID).
		Scan(&e.ID, &e.PlanID, &e.TestCaseID, &e.CaseTitle, &e.ExecutedBy, &e.Status, &e.Note, &e.ExecutedAt, &planStatus)
	if err != nil {
		return Execution{}, mapErr(err)
	}
	if planStatus == string(PlanClosed) {
		return Execution{}, fmt.Errorf("%w: plan is closed", ErrConflict)
	}
	return e, nil
}
