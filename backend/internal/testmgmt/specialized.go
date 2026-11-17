package testmgmt

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"
)

// SpecializedInput is the provider-neutral ATP report v1 envelope. It does not
// claim a vendor report can be interpreted without a matching adapter.
type SpecializedInput struct {
	SchemaVersion      int             `json:"schemaVersion"`
	IdempotencyKey     string          `json:"idempotencyKey"`
	TestCaseID         string          `json:"testCaseId"`
	Kind               string          `json:"kind"`
	Title              string          `json:"title"`
	State              string          `json:"state"`
	Notes              string          `json:"notes"`
	Environment        string          `json:"environment"`
	ApplicationVersion string          `json:"applicationVersion"`
	Source             string          `json:"source"`
	ToolVersion        string          `json:"toolVersion"`
	AdapterVersion     string          `json:"adapterVersion"`
	StartedAt          time.Time       `json:"startedAt"`
	FinishedAt         time.Time       `json:"finishedAt"`
	RetestOf           string          `json:"retestOf"`
	Details            json.RawMessage `json:"details"`
	OriginalReport     string          `json:"originalReport"`
}

type SpecializedResult struct {
	ID        string           `json:"id"`
	Kind      string           `json:"kind"`
	Title     string           `json:"title"`
	Outcome   string           `json:"outcome"`
	Reasons   []string         `json:"reasons"`
	Input     SpecializedInput `json:"input"`
	CreatedBy string           `json:"createdBy"`
	CreatedAt time.Time        `json:"createdAt"`
}
type SpecializedFilter struct{ Kind, Outcome, TestCaseID, Before string }
type specializedCursor struct {
	Project string    `json:"project"`
	Kind    string    `json:"kind"`
	Outcome string    `json:"outcome"`
	Case    string    `json:"case"`
	At      time.Time `json:"at"`
	ID      string    `json:"id"`
}
type SpecializedPage struct {
	Items      []SpecializedResult `json:"items"`
	NextCursor string              `json:"nextCursor"`
}

var specializedKinds = []string{"performance", "security", "accessibility", "compatibility", "recovery", "usability"}
var specializedOutcomes = []string{"passed", "failed", "inconclusive", "not_run", "not_applicable", "import_error"}

func strictReportJSON(raw []byte, out any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		return fmt.Errorf("%w: malformed report details", ErrInvalid)
	}
	if decoder.Decode(new(any)) != io.EOF {
		return fmt.Errorf("%w: trailing report details", ErrInvalid)
	}
	return nil
}

func (s *service) AddSpecializedResult(ctx context.Context, projectID string, in SpecializedInput, actor string) (SpecializedResult, error) {
	if !isUUID(projectID) || (in.TestCaseID != "" && !isUUID(in.TestCaseID)) || (in.RetestOf != "" && !isUUID(in.RetestOf)) {
		return SpecializedResult{}, ErrNotFound
	}
	if in.SchemaVersion != 1 || !slices.Contains(specializedKinds, in.Kind) {
		return SpecializedResult{}, fmt.Errorf("%w: report version or kind", ErrInvalid)
	}
	for _, field := range []*string{&in.IdempotencyKey, &in.Title, &in.Environment, &in.ApplicationVersion, &in.Source, &in.ToolVersion, &in.AdapterVersion} {
		*field = strings.TrimSpace(*field)
	}
	for _, value := range []string{in.IdempotencyKey, in.Environment, in.ApplicationVersion, in.Source, in.ToolVersion, in.AdapterVersion} {
		if len(value) == 0 || len(value) > 128 {
			return SpecializedResult{}, fmt.Errorf("%w: required report metadata (1..128 bytes)", ErrInvalid)
		}
	}
	if len(in.Title) == 0 || len(in.Title) > 500 || len(in.Notes) > 4000 || len(in.OriginalReport) > 524288 || len(in.Details) > 65536 {
		return SpecializedResult{}, fmt.Errorf("%w: report text limits", ErrInvalid)
	}
	if in.State == "" {
		in.State = "completed"
	}
	if !slices.Contains([]string{"completed", "not_run", "not_applicable", "import_error"}, in.State) {
		return SpecializedResult{}, fmt.Errorf("%w: report state", ErrInvalid)
	}
	if in.State == "completed" && (in.StartedAt.IsZero() || in.FinishedAt.IsZero() || in.FinishedAt.Before(in.StartedAt) || in.FinishedAt.After(s.now().Add(5*time.Minute)) || strings.TrimSpace(in.OriginalReport) == "") {
		return SpecializedResult{}, fmt.Errorf("%w: completed report needs original text and valid execution times", ErrInvalid)
	}
	if in.State != "completed" && strings.TrimSpace(in.Notes) == "" {
		return SpecializedResult{}, fmt.Errorf("%w: incomplete/import state requires notes", ErrInvalid)
	}
	if in.State == "import_error" && strings.TrimSpace(in.OriginalReport) == "" {
		return SpecializedResult{}, fmt.Errorf("%w: import error requires the original text", ErrInvalid)
	}
	in.StartedAt, in.FinishedAt = in.StartedAt.UTC(), in.FinishedAt.UTC()
	outcome, reasons, details, err := assessSpecialized(in)
	if err != nil {
		return SpecializedResult{}, err
	}
	in.Details = details
	if actor == "" {
		actor = "system"
	}
	canonical, _ := json.Marshal(in)
	sum := sha256.Sum256(canonical)
	hash := hex.EncodeToString(sum[:])
	// Keep the original text in its bounded dedicated column, not twice.
	withoutOriginal := in
	withoutOriginal.OriginalReport = ""
	rawEnvelope, _ := json.Marshal(struct {
		Input   SpecializedInput `json:"input"`
		Reasons []string         `json:"reasons"`
	}{withoutOriginal, reasons})
	var id string
	err = s.db.WithTx(ctx, func(ctx context.Context) error {
		q := s.db.Q(ctx)
		if in.TestCaseID != "" {
			var found string
			if err := q.QueryRow(ctx, `SELECT id FROM test_cases WHERE project_id=$1 AND id=$2 FOR SHARE`, projectID, in.TestCaseID).Scan(&found); err != nil {
				return mapErr(err)
			}
		}
		if in.RetestOf != "" {
			var previousKind, previousCase string
			if err := q.QueryRow(ctx, `SELECT kind, coalesce(test_case_id::text,'') FROM specialized_results WHERE project_id=$1 AND id=$2 FOR SHARE`, projectID, in.RetestOf).Scan(&previousKind, &previousCase); err != nil {
				return mapErr(err)
			}
			if previousKind != in.Kind || previousCase != in.TestCaseID {
				return ErrNotFound
			}
		}
		err := q.QueryRow(ctx, `INSERT INTO specialized_results(project_id,test_case_id,kind,title,outcome,envelope,original_report,idempotency_key,payload_hash,retest_of,created_by)
            VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11) ON CONFLICT(project_id,idempotency_key) DO NOTHING RETURNING id`,
			projectID, nullable(in.TestCaseID), in.Kind, in.Title, outcome, rawEnvelope, in.OriginalReport, in.IdempotencyKey, hash, nullable(in.RetestOf), actor).Scan(&id)
		if mapErr(err) == ErrNotFound {
			var previousHash string
			if err := q.QueryRow(ctx, `SELECT id,payload_hash FROM specialized_results WHERE project_id=$1 AND idempotency_key=$2`, projectID, in.IdempotencyKey).Scan(&id, &previousHash); err != nil {
				return err
			}
			if previousHash != hash {
				return fmt.Errorf("%w: idempotency key already used for different report", ErrConflict)
			}
			return nil
		}
		return mapErr(err)
	})
	if err != nil {
		return SpecializedResult{}, err
	}
	return s.GetSpecializedResult(ctx, projectID, id)
}

const specializedColumns = `id,kind,title,outcome,envelope,created_by,created_at`

func scanSpecialized(row interface{ Scan(...any) error }) (SpecializedResult, error) {
	var out SpecializedResult
	var envelope []byte
	if err := row.Scan(&out.ID, &out.Kind, &out.Title, &out.Outcome, &envelope, &out.CreatedBy, &out.CreatedAt); err != nil {
		return out, mapErr(err)
	}
	var data struct {
		Input   SpecializedInput `json:"input"`
		Reasons []string         `json:"reasons"`
	}
	if err := json.Unmarshal(envelope, &data); err != nil {
		return out, err
	}
	out.Input, out.Reasons = data.Input, data.Reasons
	return out, nil
}
func (s *service) GetSpecializedResult(ctx context.Context, projectID, id string) (SpecializedResult, error) {
	if !isUUID(projectID) || !isUUID(id) {
		return SpecializedResult{}, ErrNotFound
	}
	return scanSpecialized(s.db.Q(ctx).QueryRow(ctx, `SELECT `+specializedColumns+` FROM specialized_results WHERE project_id=$1 AND id=$2`, projectID, id))
}
func (s *service) SpecializedOriginal(ctx context.Context, projectID, id string) (string, error) {
	if !isUUID(projectID) || !isUUID(id) {
		return "", ErrNotFound
	}
	var text string
	err := s.db.Q(ctx).QueryRow(ctx, `SELECT original_report FROM specialized_results WHERE project_id=$1 AND id=$2`, projectID, id).Scan(&text)
	return text, mapErr(err)
}
func (s *service) ListSpecializedResults(ctx context.Context, projectID string, f SpecializedFilter) (SpecializedPage, error) {
	out := SpecializedPage{Items: []SpecializedResult{}}
	if !isUUID(projectID) || (f.TestCaseID != "" && !isUUID(f.TestCaseID)) {
		return out, ErrNotFound
	}
	if (f.Kind != "" && !slices.Contains(specializedKinds, f.Kind)) || (f.Outcome != "" && !slices.Contains(specializedOutcomes, f.Outcome)) {
		return out, ErrInvalid
	}
	var before *time.Time
	beforeID := ""
	if f.Before != "" {
		if len(f.Before) > 2048 {
			return out, ErrInvalid
		}
		raw, err := base64.RawURLEncoding.DecodeString(f.Before)
		if err != nil {
			return out, ErrInvalid
		}
		var cursor specializedCursor
		if strictReportJSON(raw, &cursor) != nil || cursor.Project != projectID || cursor.Kind != f.Kind || cursor.Outcome != f.Outcome || cursor.Case != f.TestCaseID || cursor.At.IsZero() || !isUUID(cursor.ID) {
			return out, ErrInvalid
		}
		before, beforeID = &cursor.At, cursor.ID
	}
	rows, err := s.db.Q(ctx).Query(ctx, `SELECT `+specializedColumns+` FROM specialized_results WHERE project_id=$1
        AND ($2='' OR kind=$2) AND ($3='' OR outcome=$3) AND ($4='' OR test_case_id=$4::uuid)
        AND ($5::timestamptz IS NULL OR (created_at,id)<($5,$6::uuid)) ORDER BY created_at DESC,id DESC LIMIT 51`, projectID, f.Kind, f.Outcome, f.TestCaseID, before, nullable(beforeID))
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		item, err := scanSpecialized(rows)
		if err != nil {
			return out, err
		}
		if len(out.Items) == 50 {
			last := out.Items[49]
			raw, _ := json.Marshal(specializedCursor{projectID, f.Kind, f.Outcome, f.TestCaseID, last.CreatedAt.UTC(), last.ID})
			out.NextCursor = base64.RawURLEncoding.EncodeToString(raw)
			break
		}
		// List is a summary: details are fetched on demand, never 50 large reports.
		item.Input.Details = nil
		out.Items = append(out.Items, item)
	}
	return out, rows.Err()
}
