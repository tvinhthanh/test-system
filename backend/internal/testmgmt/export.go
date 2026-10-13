package testmgmt

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// Prefix spreadsheet formulas and literal apostrophes. The format marker lets
// our importer remove exactly one prefix without changing the original text.
func csvSafeText(value string) string {
	trimmed := strings.TrimLeftFunc(value, unicode.IsSpace)
	if strings.HasPrefix(value, "'") || (len(trimmed) > 0 && strings.IndexByte("=+-@", trimmed[0]) >= 0) {
		return "'" + value
	}
	return value
}

func (s *service) ExportCases(ctx context.Context, projectID string, f CaseFilter, ids []string) (CaseExport, error) {
	if !isUUID(projectID) || (f.SuiteID != "" && !isUUID(f.SuiteID)) {
		return CaseExport{}, ErrNotFound
	}
	if len(ids) > 200 {
		return CaseExport{}, fmt.Errorf("%w: more than 200 selected cases", ErrInvalid)
	}
	seen := map[string]bool{}
	for _, id := range ids {
		if !isUUID(id) {
			return CaseExport{}, ErrNotFound
		}
		if seen[id] {
			return CaseExport{}, fmt.Errorf("%w: duplicate export case", ErrInvalid)
		}
		seen[id] = true
	}
	rows, err := s.db.Q(ctx).Query(ctx, `SELECT `+caseColumns+`
        FROM test_cases c LEFT JOIN test_suites s ON s.id = c.suite_id
        WHERE `+caseFilterWhere+` AND ($6::uuid[] IS NULL OR c.id = ANY($6))
        ORDER BY c.priority, c.created_at, c.id LIMIT 5001`,
		projectID, f.SuiteID, string(f.AutomationStatus), f.Critical, strings.TrimSpace(f.Query), ids)
	if err != nil {
		return CaseExport{}, err
	}
	defer rows.Close()
	var buffer bytes.Buffer
	writer := csv.NewWriter(&buffer)
	if err := writer.Write([]string{"_atp_csv_version", "id", "key", "title", "suite", "steps_json", "expected", "priority", "critical", "automation_status", "preconditions", "test_data", "owner_id", "created_at", "updated_at"}); err != nil {
		return CaseExport{}, err
	}
	count := 0
	for rows.Next() {
		if count == 5000 {
			return CaseExport{}, fmt.Errorf("%w: export exceeds 5000 cases; narrow the filter", ErrInvalid)
		}
		c, err := scanCase(rows)
		if err != nil {
			return CaseExport{}, err
		}
		steps, err := json.Marshal(c.Steps)
		if err != nil {
			return CaseExport{}, err
		}
		record := []string{"1", c.ID, c.ExternalKey, c.Title, c.SuiteName, string(steps), c.Expected, string(c.Priority), strconv.FormatBool(c.IsCritical), string(c.AutomationStatus), c.Preconditions, c.TestData, c.OwnerID, c.CreatedAt.UTC().Format(time.RFC3339Nano), c.UpdatedAt.UTC().Format(time.RFC3339Nano)}
		for index := range record {
			record[index] = csvSafeText(record[index])
		}
		if err := writer.Write(record); err != nil {
			return CaseExport{}, err
		}
		writer.Flush()
		if err := writer.Error(); err != nil {
			return CaseExport{}, err
		}
		if buffer.Len() > 8<<20 {
			return CaseExport{}, fmt.Errorf("%w: export exceeds 8 MiB; narrow the filter", ErrInvalid)
		}
		count++
	}
	if err := rows.Err(); err != nil {
		return CaseExport{}, err
	}
	if ids != nil && count != len(ids) {
		return CaseExport{}, ErrNotFound
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return CaseExport{}, err
	}
	return CaseExport{CSV: buffer.String(), Count: count, ExportedAt: s.now().UTC()}, nil
}
