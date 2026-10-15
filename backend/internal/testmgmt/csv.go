package testmgmt

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

// CSV import (§6.8.5: "CSV / Excel — làm trước"). One importer, upsert by
// external key so re-running a corrected sheet fixes rows instead of
// duplicating them. Excel users export CSV; a second importer is built only
// for a named customer (§6.8.5 rule).
//
// Columns, case-insensitive, any order; unknown columns are ignored:
//
//	key         external key (TC-42). Absent/empty → upsert by exact title
//	title       required
//	suite       suite name, created when new (one level)
//	steps       "action => expected" per line; "|" also separates steps
//	expected    overall expected result
//	priority    p0..p3 (default p2)
//	critical    true/yes/1
//	requirement external key of a requirement; created (active, p2) when new

const maxImportRows = 5000

func (s *service) ImportCSV(ctx context.Context, projectID string, text string) (ImportReport, error) {
	return s.ImportCSVAs(ctx, projectID, text, "")
}

func (s *service) ImportCSVAs(ctx context.Context, projectID string, text, actor string) (ImportReport, error) {
	if !isUUID(projectID) {
		return ImportReport{}, ErrNotFound
	}
	if len(text) > 8<<20 {
		return ImportReport{}, fmt.Errorf("%w: file over 8 MiB", ErrInvalid)
	}
	r := csv.NewReader(strings.NewReader(strings.TrimPrefix(text, "\ufeff")))
	r.FieldsPerRecord = -1
	r.TrimLeadingSpace = true
	header, err := r.Read()
	if err != nil {
		return ImportReport{}, fmt.Errorf("%w: empty file or bad header: %v", ErrInvalid, err)
	}
	col := map[string]int{}
	for i, h := range header {
		col[strings.ToLower(strings.TrimSpace(h))] = i
	}
	if _, ok := col["title"]; !ok {
		return ImportReport{}, fmt.Errorf("%w: header needs a \"title\" column (got %q)", ErrInvalid, header)
	}
	field := func(rec []string, name string) string {
		i, ok := col[name]
		if !ok || i >= len(rec) {
			return ""
		}
		value := strings.TrimSpace(rec[i])
		if version, ok := col["_atp_csv_version"]; ok && version < len(rec) && strings.TrimSpace(rec[version]) == "1" && strings.HasPrefix(value, "'") {
			return value[1:]
		}
		return value
	}

	var rep ImportReport
	suites := map[string]string{}       // name → id
	requirements := map[string]string{} // external key → id
	err = s.db.WithTx(ctx, func(ctx context.Context) error {
		if err := s.caseAuditActor(ctx, actor, "case.import"); err != nil {
			return err
		}
		q := s.db.Q(ctx)
		// Existing suites and requirements of the project, by name/key.
		rows, err := q.Query(ctx, `SELECT id, name FROM test_suites WHERE project_id = $1 AND parent_id IS NULL`, projectID)
		if err != nil {
			return err
		}
		for rows.Next() {
			var id, name string
			if err := rows.Scan(&id, &name); err != nil {
				rows.Close()
				return err
			}
			suites[strings.ToLower(name)] = id
		}
		rows.Close()
		rows, err = q.Query(ctx, `SELECT id, external_key FROM requirements WHERE project_id = $1 AND external_key IS NOT NULL`, projectID)
		if err != nil {
			return err
		}
		for rows.Next() {
			var id, key string
			if err := rows.Scan(&id, &key); err != nil {
				rows.Close()
				return err
			}
			requirements[strings.ToLower(key)] = id
		}
		rows.Close()

		for line := 2; ; line++ {
			rec, err := r.Read()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				rep.Errors = append(rep.Errors, fmt.Sprintf("row %d: %v", line, err))
				continue
			}
			if line-1 > maxImportRows {
				return fmt.Errorf("%w: more than %d rows", ErrInvalid, maxImportRows)
			}
			title := field(rec, "title")
			if title == "" {
				rep.Skipped++
				continue
			}
			if len(title) > 500 {
				rep.Errors = append(rep.Errors, fmt.Sprintf("row %d: title over 500 characters", line))
				continue
			}
			key := field(rec, "key")
			if len(key) > 100 {
				rep.Errors = append(rep.Errors, fmt.Sprintf("row %d: key over 100 characters", line))
				continue
			}
			prio := Priority(strings.ToLower(field(rec, "priority")))
			if err := checkPriority(&prio); err != nil {
				rep.Errors = append(rep.Errors, fmt.Sprintf("row %d: priority %q not in p0..p3", line, field(rec, "priority")))
				continue
			}
			critical := truthy(field(rec, "critical"))
			steps := parseSteps(field(rec, "steps"))
			if _, ok := col["steps_json"]; ok {
				text := field(rec, "steps_json")
				steps = []Step{}
				if text != "" {
					if err := json.Unmarshal([]byte(text), &steps); err != nil {
						rep.Errors = append(rep.Errors, fmt.Sprintf("row %d: invalid steps_json", line))
						continue
					}
				}
			}
			stepsJSON, err := normalizeSteps(steps)
			if err != nil {
				rep.Errors = append(rep.Errors, fmt.Sprintf("row %d: %v", line, err))
				continue
			}
			expected := field(rec, "expected")
			preconditions, testData := field(rec, "preconditions"), field(rec, "test_data")
			if len(preconditions) > 16000 || len(testData) > 16000 {
				rep.Errors = append(rep.Errors, fmt.Sprintf("row %d: case context exceeds 16000 bytes", line))
				continue
			}
			_, hasPreconditions := col["preconditions"]
			_, hasTestData := col["test_data"]
			_, hasTaxonomy := col["taxonomy_json"]
			classification := Taxonomy{}
			if text := field(rec, "taxonomy_json"); hasTaxonomy && text != "" {
				decoder := json.NewDecoder(strings.NewReader(text))
				decoder.DisallowUnknownFields()
				if !strings.HasPrefix(text, "{") || decoder.Decode(&classification) != nil || decoder.Decode(new(any)) != io.EOF {
					rep.Errors = append(rep.Errors, fmt.Sprintf("row %d: invalid taxonomy_json", line))
					continue
				}
			}
			taxonomy, err := taxonomyJSON(classification)
			if err != nil {
				rep.Errors = append(rep.Errors, fmt.Sprintf("row %d: %v", line, err))
				continue
			}

			var suiteID *string
			if name := field(rec, "suite"); name != "" {
				id, ok := suites[strings.ToLower(name)]
				if !ok {
					if len(name) > 200 {
						rep.Errors = append(rep.Errors, fmt.Sprintf("row %d: suite name over 200 characters", line))
						continue
					}
					if err := q.QueryRow(ctx, `INSERT INTO test_suites (project_id, name) VALUES ($1, $2) RETURNING id`, projectID, name).Scan(&id); err != nil {
						return mapErr(err)
					}
					suites[strings.ToLower(name)] = id
					rep.Suites++
				}
				suiteID = &id
			}

			// Upsert: by key when given, else by exact title among keyless cases.
			var caseID string
			var inserted bool
			if key != "" {
				err = q.QueryRow(ctx, `
					INSERT INTO test_cases (project_id, suite_id, external_key, title, steps, expected, priority, is_critical, preconditions, test_data, taxonomy)
					VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $13)
					ON CONFLICT (project_id, external_key) WHERE external_key IS NOT NULL DO UPDATE
					   SET suite_id = coalesce(EXCLUDED.suite_id, test_cases.suite_id), title = EXCLUDED.title, steps = EXCLUDED.steps,
					       expected = EXCLUDED.expected, priority = EXCLUDED.priority, is_critical = EXCLUDED.is_critical,
					       preconditions = CASE WHEN $11 THEN EXCLUDED.preconditions ELSE test_cases.preconditions END,
					       test_data = CASE WHEN $12 THEN EXCLUDED.test_data ELSE test_cases.test_data END,
                       taxonomy = CASE WHEN $14 THEN EXCLUDED.taxonomy ELSE test_cases.taxonomy END, updated_at = clock_timestamp()
					RETURNING id, (xmax = 0)`, projectID, suiteID, key, title, stepsJSON, expected, string(prio), critical, preconditions, testData, hasPreconditions, hasTestData, taxonomy, hasTaxonomy).Scan(&caseID, &inserted)
			} else {
				err = q.QueryRow(ctx, `SELECT id FROM test_cases WHERE project_id = $1 AND external_key IS NULL AND title = $2 ORDER BY created_at LIMIT 1`, projectID, title).Scan(&caseID)
				switch {
				case err == nil:
					_, err = q.Exec(ctx, `UPDATE test_cases SET suite_id = coalesce($2, suite_id), steps = $3, expected = $4, priority = $5, is_critical = $6,
						preconditions = CASE WHEN $9 THEN $7 ELSE preconditions END, test_data = CASE WHEN $10 THEN $8 ELSE test_data END,
                        taxonomy = CASE WHEN $12 THEN $11::jsonb ELSE taxonomy END, updated_at = clock_timestamp() WHERE id = $1`,
						caseID, suiteID, stepsJSON, expected, string(prio), critical, preconditions, testData, hasPreconditions, hasTestData, taxonomy, hasTaxonomy)
				case mapErr(err) == ErrNotFound:
					inserted = true
					err = q.QueryRow(ctx, `INSERT INTO test_cases (project_id, suite_id, title, steps, expected, priority, is_critical, preconditions, test_data, taxonomy) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10) RETURNING id`,
						projectID, suiteID, title, stepsJSON, expected, string(prio), critical, preconditions, testData, taxonomy).Scan(&caseID)
				}
			}
			if err != nil {
				return mapErr(err)
			}
			if inserted {
				rep.Created++
			} else {
				rep.Updated++
			}

			if rk := field(rec, "requirement"); rk != "" {
				if len(rk) > 100 {
					rep.Errors = append(rep.Errors, fmt.Sprintf("row %d: requirement key over 100 characters", line))
					continue
				}
				rid, ok := requirements[strings.ToLower(rk)]
				if !ok {
					if err := q.QueryRow(ctx, `INSERT INTO requirements (project_id, external_key, title) VALUES ($1, $2, $2) RETURNING id`, projectID, rk).Scan(&rid); err != nil {
						return mapErr(err)
					}
					requirements[strings.ToLower(rk)] = rid
					rep.Requirements++
				}
				if _, err := q.Exec(ctx, `INSERT INTO requirement_coverage (project_id, requirement_id, test_case_id) VALUES ($1, $2, $3) ON CONFLICT DO NOTHING`, projectID, rid, caseID); err != nil {
					return mapErr(err)
				}
			}
		}
		return nil
	})
	if err != nil {
		return ImportReport{}, err
	}
	if rep.Errors == nil {
		rep.Errors = []string{}
	}
	return rep, nil
}

func truthy(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "1", "true", "yes", "y", "x", "critical":
		return true
	}
	return false
}

// parseSteps turns "open cart => cart shown | pay => receipt" (or one step
// per line) into Steps. Text without "=>" is an action with no expectation.
func parseSteps(raw string) []Step {
	raw = strings.ReplaceAll(raw, "\r\n", "\n")
	var out []Step
	for _, part := range strings.FieldsFunc(raw, func(r rune) bool { return r == '\n' || r == '|' }) {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		action, expected, _ := strings.Cut(part, "=>")
		out = append(out, Step{Action: strings.TrimSpace(action), Expected: strings.TrimSpace(expected)})
	}
	return out
}
