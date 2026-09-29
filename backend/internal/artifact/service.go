package artifact

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/vinh/automation-testing-platform/backend/internal/platform/objstore"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/pg"
	"github.com/vinh/automation-testing-platform/backend/internal/results"
)

type service struct {
	db      *pg.DB
	store   objstore.Store
	results results.Service
	now     func() time.Time
}

// New returns the Service. results is used only to link artifacts to
// attempts (§7.6 #3).
func New(db *pg.DB, store objstore.Store, res results.Service) Service {
	return &service{db: db, store: store, results: res, now: time.Now}
}

var (
	uuidRe = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	shaRe  = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

func isUUID(s string) bool { return uuidRe.MatchString(s) }

const columns = `id, project_id, run_id, manual_execution_id, test_result_attempt_id, client_test_id, occurrence_index, attempt, kind, name, content_type,
	declared_bytes, actual_bytes, sha256, storage_key, upload_id, status, failure_reason, expires_at, created_at, verified_at, linked_at`

func scan(row interface{ Scan(dest ...any) error }) (Artifact, error) {
	var a Artifact
	// run_id, client_test_id and attempt are NULL on manual_evidence (0014).
	var runID, executionID, attemptID, clientTestID, uploadID, reason *string
	var attempt *int
	var actual *int64
	var verified, linked *time.Time
	err := row.Scan(&a.ID, &a.ProjectID, &runID, &executionID, &attemptID, &clientTestID, &a.OccurrenceIndex, &attempt, &a.Kind, &a.Name, &a.ContentType,
		&a.DeclaredBytes, &actual, &a.SHA256, &a.StorageKey, &uploadID, &a.Status, &reason, &a.ExpiresAt, &a.CreatedAt, &verified, &linked)
	if err != nil {
		return Artifact{}, err
	}
	a.RunID, a.ExecutionID, a.AttemptID, a.ClientTestID = deref(runID), deref(executionID), deref(attemptID), deref(clientTestID)
	a.UploadID, a.FailureReason = deref(uploadID), deref(reason)
	if attempt != nil {
		a.Attempt = *attempt
	}
	if actual != nil {
		a.ActualBytes = *actual
	}
	a.ExpiresAt, a.CreatedAt = a.ExpiresAt.UTC(), a.CreatedAt.UTC()
	if verified != nil {
		a.VerifiedAt = verified.UTC()
	}
	if linked != nil {
		a.LinkedAt = linked.UTC()
	}
	return a, nil
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// ---- Declare ------------------------------------------------------------

func (s *service) Declare(ctx context.Context, in Declare) (Artifact, Upload, bool, error) {
	if err := in.validate(); err != nil {
		return Artifact{}, Upload{}, false, err
	}
	if cap := Caps[in.Kind]; in.SizeBytes > cap {
		return Artifact{}, Upload{}, false, &TooLargeError{Kind: in.Kind, Limit: cap, Got: in.SizeBytes}
	}
	now := s.now().UTC()
	plan := RetentionDays[in.Plan]
	if plan == nil {
		plan = RetentionDays["free"]
	}
	expires := now.Add(time.Duration(plan[in.Kind]) * 24 * time.Hour)
	// The key is ours: tenant, run, then a random id — never the client's name.
	key := fmt.Sprintf("%s/%s/%s", in.ProjectID, in.RunID, newID())

	var a Artifact
	created := false
	err := s.db.WithTx(ctx, func(ctx context.Context) error {
		q := s.db.Q(ctx)
		row := q.QueryRow(ctx, `
			INSERT INTO artifacts (project_id, run_id, client_test_id, occurrence_index, attempt, kind, name, content_type, declared_bytes, sha256, storage_key, expires_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
			ON CONFLICT (run_id, client_test_id, occurrence_index, attempt, name) DO NOTHING
			RETURNING `+columns,
			in.ProjectID, in.RunID, in.ClientTestID, in.OccurrenceIndex, in.Attempt, string(in.Kind), in.Name, in.ContentType, in.SizeBytes, in.SHA256, key, expires)
		var err error
		a, err = scan(row)
		if err == nil {
			created = true
			return nil
		}
		if !errors.Is(err, pg.ErrNoRows) {
			return mapErr(err)
		}
		// Already declared: same natural key, so this is a retry or a resume.
		a, err = scan(q.QueryRow(ctx, `
			SELECT `+columns+` FROM artifacts
			 WHERE run_id = $1 AND client_test_id = $2 AND occurrence_index = $3 AND attempt = $4 AND name = $5 AND project_id = $6 FOR UPDATE`,
			in.RunID, in.ClientTestID, in.OccurrenceIndex, in.Attempt, in.Name, in.ProjectID))
		if err != nil {
			return mapErr(err)
		}
		switch a.Status {
		case StatusExpired, StatusDeleted:
			return ErrGone
		case StatusRejected:
			// A rejected upload may be retried with corrected metadata.
			a, err = scan(q.QueryRow(ctx, `
				UPDATE artifacts SET status = 'pending', failure_reason = NULL, actual_bytes = NULL, upload_id = NULL,
				       kind = $2, content_type = $3, declared_bytes = $4, sha256 = $5, storage_key = $6, updated_at = $7
				 WHERE id = $1 RETURNING `+columns, a.ID, string(in.Kind), in.ContentType, in.SizeBytes, in.SHA256, key, now))
			return mapErr(err)
		}
		return nil
	})
	if err != nil {
		return Artifact{}, Upload{}, false, err
	}
	if a.Status == StatusVerified {
		return a, Upload{}, false, nil
	}
	up, err := s.upload(ctx, &a)
	if err != nil {
		return Artifact{}, Upload{}, false, err
	}
	return a, up, created, nil
}

func (in Declare) validate() error {
	switch {
	case !isUUID(in.ProjectID) || !isUUID(in.RunID):
		return ErrNotFound
	case in.ClientTestID == "" || len(in.ClientTestID) > 256:
		return fmt.Errorf("%w: clientTestId", ErrInvalid)
	case in.OccurrenceIndex < 0 || in.Attempt < 0:
		return fmt.Errorf("%w: occurrenceIndex/attempt", ErrInvalid)
	case !ValidKind(in.Kind):
		return fmt.Errorf("%w: kind %q", ErrInvalid, in.Kind)
	case in.Name == "" || len(in.Name) > 200 || strings.ContainsAny(in.Name, "/\\\x00"):
		return fmt.Errorf("%w: name", ErrInvalid)
	case !AllowedContentType(in.Kind, in.ContentType):
		return fmt.Errorf("%w: contentType %q is not allowed for %s", ErrInvalid, in.ContentType, in.Kind)
	case in.SizeBytes <= 0:
		return fmt.Errorf("%w: sizeBytes", ErrInvalid)
	case !shaRe.MatchString(in.SHA256):
		return fmt.Errorf("%w: sha256 must be 64 lower-case hex chars", ErrInvalid)
	}
	return nil
}

// upload builds the presigned instructions for a pending/uploaded artifact.
// Store failures come back as ErrStore: the row stays, the reporter retries.
func (s *service) upload(ctx context.Context, a *Artifact) (Upload, error) {
	expires := s.now().UTC().Add(PresignTTL)
	if a.DeclaredBytes < MultipartThreshold {
		u, err := s.store.PresignPut(ctx, a.StorageKey, a.ContentType, PresignTTL)
		if err != nil {
			return Upload{}, fmt.Errorf("%w: %v", ErrStore, err)
		}
		return Upload{Put: &PutUpload{URL: u, Headers: map[string]string{"Content-Type": a.ContentType}, ExpiresAt: expires}}, nil
	}
	if a.UploadID == "" {
		id, err := s.store.CreateMultipart(ctx, a.StorageKey, a.ContentType)
		if err != nil {
			return Upload{}, fmt.Errorf("%w: %v", ErrStore, err)
		}
		if _, err := s.db.Q(ctx).Exec(ctx, `UPDATE artifacts SET upload_id = $2, updated_at = $3 WHERE id = $1`, a.ID, id, s.now().UTC()); err != nil {
			return Upload{}, mapErr(err)
		}
		a.UploadID = id
	}
	received, err := s.store.ListParts(ctx, a.StorageKey, a.UploadID)
	if err != nil {
		if errors.Is(err, objstore.ErrNotFound) {
			// The store lost the upload (lifecycle cleanup): start over.
			id, err := s.store.CreateMultipart(ctx, a.StorageKey, a.ContentType)
			if err != nil {
				return Upload{}, fmt.Errorf("%w: %v", ErrStore, err)
			}
			if _, err := s.db.Q(ctx).Exec(ctx, `UPDATE artifacts SET upload_id = $2, updated_at = $3 WHERE id = $1`, a.ID, id, s.now().UTC()); err != nil {
				return Upload{}, mapErr(err)
			}
			a.UploadID = id
			received = nil
		} else {
			return Upload{}, fmt.Errorf("%w: %v", ErrStore, err)
		}
	}
	have := map[int]bool{}
	mp := &MultipartUpload{UploadID: a.UploadID, PartSize: PartSize, Parallel: MaxParallelParts, ExpiresAt: expires}
	for _, p := range received {
		have[p.Number] = true
		mp.Received = append(mp.Received, p.Number)
	}
	total := int((a.DeclaredBytes + PartSize - 1) / PartSize)
	for n := 1; n <= total; n++ {
		if have[n] {
			continue
		}
		u, err := s.store.PresignPart(ctx, a.StorageKey, a.UploadID, n, PresignTTL)
		if err != nil {
			return Upload{}, fmt.Errorf("%w: %v", ErrStore, err)
		}
		mp.Parts = append(mp.Parts, PartURL{Number: n, URL: u})
	}
	return Upload{Multipart: mp}, nil
}

// ---- Finish -------------------------------------------------------------

func (s *service) Finish(ctx context.Context, projectID, artifactID string, in Complete) (Artifact, bool, error) {
	if !isUUID(projectID) || !isUUID(artifactID) {
		return Artifact{}, false, ErrNotFound
	}
	a, err := s.Get(ctx, projectID, artifactID)
	if err != nil {
		return Artifact{}, false, err
	}
	switch a.Status {
	case StatusVerified:
		return a, false, nil
	case StatusExpired, StatusDeleted:
		return Artifact{}, false, ErrGone
	case StatusRejected:
		return Artifact{}, false, fmt.Errorf("%w: rejected (%s); declare again", ErrInvalid, a.FailureReason)
	}
	if in.SHA256 != "" && in.SHA256 != a.SHA256 {
		return Artifact{}, false, fmt.Errorf("%w: sha256 differs from the declared one", ErrInvalid)
	}
	a, err = s.verify(ctx, a, in)
	if err != nil {
		return Artifact{}, false, err
	}
	return a, true, nil
}

// verify is the store-facing half of Finish for a pending artifact.
func (s *service) verify(ctx context.Context, a Artifact, in Complete) (Artifact, error) {
	var err error

	// Multipart: assemble first. NoSuchUpload with the object present means
	// a retried complete; without it, the upload was never finished.
	if a.UploadID != "" {
		if _, herr := s.store.Head(ctx, a.StorageKey); errors.Is(herr, objstore.ErrNotFound) {
			// The store's own part list is the source of truth for ETags — a
			// resumed upload cannot know the ETags of parts sent earlier. The
			// client's list, when given, must agree with it.
			parts, err := s.store.ListParts(ctx, a.StorageKey, a.UploadID)
			if err != nil {
				if errors.Is(err, objstore.ErrNotFound) {
					return Artifact{}, ErrUploadIncomplete
				}
				return Artifact{}, fmt.Errorf("%w: %v", ErrStore, err)
			}
			expected := int((a.DeclaredBytes + PartSize - 1) / PartSize)
			if len(parts) < expected {
				return Artifact{}, fmt.Errorf("%w: %d of %d parts received", ErrUploadIncomplete, len(parts), expected)
			}
			known := map[int]string{}
			for _, p := range parts {
				known[p.Number] = p.ETag
			}
			for _, p := range in.Parts {
				if etag, ok := known[p.Number]; !ok || etag != p.ETag {
					return Artifact{}, fmt.Errorf("%w: part %d does not match what the store received", ErrUploadIncomplete, p.Number)
				}
			}
			if err := s.store.CompleteMultipart(ctx, a.StorageKey, a.UploadID, parts); err != nil {
				if errors.Is(err, objstore.ErrNotFound) {
					return Artifact{}, ErrUploadIncomplete
				}
				return Artifact{}, fmt.Errorf("%w: complete multipart: %v", ErrUploadIncomplete, err)
			}
		} else if herr != nil {
			return Artifact{}, fmt.Errorf("%w: %v", ErrStore, herr)
		}
	}

	// HEAD: what actually landed, before reading a byte of it.
	info, err := s.store.Head(ctx, a.StorageKey)
	if errors.Is(err, objstore.ErrNotFound) {
		return Artifact{}, ErrUploadIncomplete
	}
	if err != nil {
		return Artifact{}, fmt.Errorf("%w: %v", ErrStore, err)
	}
	if info.Size != a.DeclaredBytes || info.Size > Caps[a.Kind] {
		return s.reject(ctx, a, &MismatchError{Code: "SIZE_MISMATCH", Expected: fmt.Sprint(a.DeclaredBytes), Actual: fmt.Sprint(info.Size)})
	}
	if info.ContentType != "" && info.ContentType != a.ContentType {
		return s.reject(ctx, a, &MismatchError{Code: "CONTENT_TYPE_MISMATCH", Expected: a.ContentType, Actual: info.ContentType})
	}

	// GET streamed through SHA-256, bounded by the cap so a lying HEAD
	// cannot make us read more than §10.4 allows.
	body, err := s.store.Get(ctx, a.StorageKey)
	if err != nil {
		if errors.Is(err, objstore.ErrNotFound) {
			return Artifact{}, ErrUploadIncomplete
		}
		return Artifact{}, fmt.Errorf("%w: %v", ErrStore, err)
	}
	h := sha256.New()
	n, err := io.Copy(h, io.LimitReader(body, Caps[a.Kind]+1))
	body.Close()
	if err != nil {
		return Artifact{}, fmt.Errorf("%w: read: %v", ErrStore, err)
	}
	if n != a.DeclaredBytes {
		return s.reject(ctx, a, &MismatchError{Code: "SIZE_MISMATCH", Expected: fmt.Sprint(a.DeclaredBytes), Actual: fmt.Sprint(n)})
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != a.SHA256 {
		return s.reject(ctx, a, &MismatchError{Code: "CHECKSUM_MISMATCH", Expected: a.SHA256, Actual: got})
	}

	now := s.now().UTC()
	a, err = scan(s.db.Q(ctx).QueryRow(ctx, `
		UPDATE artifacts SET status = 'verified', actual_bytes = $2, verified_at = $3, updated_at = $3, upload_id = NULL
		 WHERE id = $1 RETURNING `+columns, a.ID, n, now))
	if err != nil {
		return Artifact{}, mapErr(err)
	}
	// Link now if the attempt exists; otherwise the sweep does it (§7.6 #3).
	if linked, err := s.link(ctx, &a); err != nil {
		return Artifact{}, err
	} else if linked {
		return a, nil
	}
	return a, nil
}

// reject deletes the object and records why (§7.7: "object bị xóa").
func (s *service) reject(ctx context.Context, a Artifact, cause *MismatchError) (Artifact, error) {
	if err := s.store.Delete(ctx, a.StorageKey); err != nil && !errors.Is(err, objstore.ErrNotFound) {
		return Artifact{}, fmt.Errorf("%w: delete after %s: %v", ErrStore, cause.Code, err)
	}
	_, err := s.db.Q(ctx).Exec(ctx, `
		UPDATE artifacts SET status = 'rejected', failure_reason = $2, updated_at = $3, upload_id = NULL WHERE id = $1`, a.ID, cause.Code, s.now().UTC())
	if err != nil {
		return Artifact{}, mapErr(err)
	}
	return Artifact{}, cause
}

// link attaches a to its attempt when results has it.
func (s *service) link(ctx context.Context, a *Artifact) (bool, error) {
	id, err := s.results.FindAttempt(ctx, results.RunID(a.RunID), a.ClientTestID, a.OccurrenceIndex, a.Attempt)
	if errors.Is(err, results.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	now := s.now().UTC()
	if _, err := s.db.Q(ctx).Exec(ctx, `
		UPDATE artifacts SET test_result_attempt_id = $2, linked_at = $3, updated_at = $3 WHERE id = $1 AND test_result_attempt_id IS NULL`, a.ID, id, now); err != nil {
		return false, mapErr(err)
	}
	a.AttemptID, a.LinkedAt = id, now
	return true, nil
}

// ---- Reads --------------------------------------------------------------

func (s *service) Get(ctx context.Context, projectID, artifactID string) (Artifact, error) {
	if !isUUID(projectID) || !isUUID(artifactID) {
		return Artifact{}, ErrNotFound
	}
	a, err := scan(s.db.Q(ctx).QueryRow(ctx, `SELECT `+columns+` FROM artifacts WHERE id = $1 AND project_id = $2`, artifactID, projectID))
	return a, mapErr(err)
}

func (s *service) ListByRun(ctx context.Context, projectID, runID string) ([]Artifact, error) {
	if !isUUID(projectID) || !isUUID(runID) {
		return nil, ErrNotFound
	}
	rows, err := s.db.Q(ctx).Query(ctx, `SELECT `+columns+` FROM artifacts WHERE run_id = $1 AND project_id = $2 ORDER BY created_at`, runID, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Artifact
	for rows.Next() {
		a, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// ---- Manual evidence (§6.8.2) --------------------------------------------

func (s *service) PutEvidence(ctx context.Context, in Evidence, body []byte) (Artifact, error) {
	switch {
	case !isUUID(in.ProjectID) || !isUUID(in.ExecutionID):
		return Artifact{}, ErrNotFound
	case in.Name == "" || len(in.Name) > 200 || strings.ContainsAny(in.Name, "/\\\x00"):
		return Artifact{}, fmt.Errorf("%w: name", ErrInvalid)
	case !AllowedContentType(KindManualEvidence, in.ContentType):
		return Artifact{}, fmt.Errorf("%w: contentType %q is not allowed for %s", ErrInvalid, in.ContentType, KindManualEvidence)
	case len(body) == 0:
		return Artifact{}, fmt.Errorf("%w: empty file", ErrInvalid)
	}
	if cap := Caps[KindManualEvidence]; int64(len(body)) > cap {
		return Artifact{}, &TooLargeError{Kind: KindManualEvidence, Limit: cap, Got: int64(len(body))}
	}
	// The declared type must be what the bytes are (§11.5): a browser sets
	// File.type from the extension, and an HTML file renamed .png must not
	// become an inline image. Text is the one type sniffing cannot pin down
	// beyond "text", so it passes as long as it does not sniff as markup.
	sniffed, _, _ := strings.Cut(http.DetectContentType(body), ";")
	if sniffed != in.ContentType && !(in.ContentType == "text/plain" && strings.HasPrefix(sniffed, "text/") && sniffed != "text/html" && sniffed != "text/xml") {
		return Artifact{}, fmt.Errorf("%w: file content is %s, not %s", ErrInvalid, sniffed, in.ContentType)
	}
	sum := sha256.Sum256(body)
	now := s.now().UTC()
	plan := RetentionDays[in.Plan]
	if plan == nil {
		plan = RetentionDays["free"]
	}
	expires := now.Add(time.Duration(plan[KindManualEvidence]) * 24 * time.Hour)
	// Tenant, execution, random id — the key is ours, as for every kind.
	key := fmt.Sprintf("%s/manual/%s/%s", in.ProjectID, in.ExecutionID, newID())
	// Object first, row second: a crash between the two leaves an orphan
	// object with no row (invisible; bucket lifecycle reclaims it), never a
	// verified row with no bytes.
	if err := s.store.Put(ctx, key, in.ContentType, body); err != nil {
		return Artifact{}, fmt.Errorf("%w: %v", ErrStore, err)
	}
	a, err := scan(s.db.Q(ctx).QueryRow(ctx, `
		INSERT INTO artifacts (project_id, manual_execution_id, kind, name, content_type, declared_bytes, actual_bytes, sha256, storage_key, status, expires_at, verified_at)
		VALUES ($1, $2, 'manual_evidence', $3, $4, $5, $5, $6, $7, 'verified', $8, $9)
		RETURNING `+columns,
		in.ProjectID, in.ExecutionID, in.Name, in.ContentType, len(body), hex.EncodeToString(sum[:]), key, expires, now))
	if err != nil {
		_ = s.store.Delete(ctx, key)
		return Artifact{}, mapErr(err)
	}
	return a, nil
}

func (s *service) ListEvidence(ctx context.Context, projectID string, executionIDs []string) (map[string][]Artifact, error) {
	if !isUUID(projectID) {
		return nil, ErrNotFound
	}
	out := map[string][]Artifact{}
	if len(executionIDs) == 0 {
		return out, nil
	}
	for _, id := range executionIDs {
		if !isUUID(id) {
			return nil, ErrNotFound
		}
	}
	rows, err := s.db.Q(ctx).Query(ctx, `
		SELECT `+columns+` FROM artifacts
		 WHERE project_id = $1 AND manual_execution_id = ANY($2::uuid[]) AND status = 'verified'
		 ORDER BY created_at, id`, projectID, executionIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		a, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out[a.ExecutionID] = append(out[a.ExecutionID], a)
	}
	return out, rows.Err()
}

// ---- Sweeps -------------------------------------------------------------

func (s *service) LinkPending(ctx context.Context) (int, int, error) {
	rows, err := s.db.Q(ctx).Query(ctx, `
		SELECT `+columns+` FROM artifacts
		 WHERE test_result_attempt_id IS NULL AND run_id IS NOT NULL AND status IN ('pending', 'uploaded', 'verified')
		 ORDER BY created_at LIMIT 1000`)
	if err != nil {
		return 0, 0, err
	}
	var pending []Artifact
	for rows.Next() {
		a, err := scan(rows)
		if err != nil {
			rows.Close()
			return 0, 0, err
		}
		pending = append(pending, a)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, 0, err
	}
	linked, overdue := 0, 0
	cutoff := s.now().Add(-UnlinkedAlertAfter)
	for i := range pending {
		ok, err := s.link(ctx, &pending[i])
		if err != nil {
			return linked, overdue, err
		}
		if ok {
			linked++
		} else if pending[i].Status != StatusPending && pending[i].CreatedAt.Before(cutoff) {
			// Only artifacts that actually landed count as unlinked (§6.4);
			// a stale pending upload is AbortAbandoned's business.
			overdue++
		}
	}
	return linked, overdue, nil
}

func (s *service) ExpireRetention(ctx context.Context) (int, error) {
	now := s.now().UTC()
	rows, err := s.db.Q(ctx).Query(ctx, `SELECT id, storage_key FROM artifacts WHERE status = 'verified' AND expires_at < $1 ORDER BY expires_at LIMIT 1000`, now)
	if err != nil {
		return 0, err
	}
	type row struct{ id, key string }
	var due []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.key); err != nil {
			rows.Close()
			return 0, err
		}
		due = append(due, r)
	}
	rows.Close()
	n := 0
	for _, r := range due {
		if err := s.store.Delete(ctx, r.key); err != nil && !errors.Is(err, objstore.ErrNotFound) {
			return n, fmt.Errorf("%w: %v", ErrStore, err)
		}
		if _, err := s.db.Q(ctx).Exec(ctx, `UPDATE artifacts SET status = 'expired', updated_at = $2 WHERE id = $1 AND status = 'verified'`, r.id, now); err != nil {
			return n, mapErr(err)
		}
		n++
	}
	return n, nil
}

func (s *service) AbortAbandoned(ctx context.Context) (int, error) {
	now := s.now().UTC()
	rows, err := s.db.Q(ctx).Query(ctx, `
		SELECT id, storage_key, coalesce(upload_id, '') FROM artifacts WHERE status = 'pending' AND created_at < $1 ORDER BY created_at LIMIT 1000`, now.Add(-AbandonAfter))
	if err != nil {
		return 0, err
	}
	type row struct{ id, key, upload string }
	var stale []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.key, &r.upload); err != nil {
			rows.Close()
			return 0, err
		}
		stale = append(stale, r)
	}
	rows.Close()
	n := 0
	for _, r := range stale {
		if r.upload != "" {
			if err := s.store.AbortMultipart(ctx, r.key, r.upload); err != nil && !errors.Is(err, objstore.ErrNotFound) {
				return n, fmt.Errorf("%w: %v", ErrStore, err)
			}
		}
		if err := s.store.Delete(ctx, r.key); err != nil && !errors.Is(err, objstore.ErrNotFound) {
			return n, fmt.Errorf("%w: %v", ErrStore, err)
		}
		if _, err := s.db.Q(ctx).Exec(ctx, `
			UPDATE artifacts SET status = 'expired', failure_reason = 'ABANDONED', upload_id = NULL, updated_at = $2 WHERE id = $1 AND status = 'pending'`, r.id, now); err != nil {
			return n, mapErr(err)
		}
		n++
	}
	return n, nil
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

func newID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

func (s *service) PurgeProject(ctx context.Context, projectID string) (int, error) {
	return s.purgeWhere(ctx, `project_id = $1`, projectID)
}

func (s *service) PurgeRun(ctx context.Context, runID string) (int, error) {
	return s.purgeWhere(ctx, `run_id = $1`, runID)
}

// purgeWhere removes matching artifacts, the object before the row, in
// batches; resumable.
func (s *service) purgeWhere(ctx context.Context, where string, arg string) (int, error) {
	n := 0
	for {
		rows, err := s.db.Q(ctx).Query(ctx, `SELECT id, storage_key, coalesce(upload_id, '') FROM artifacts WHERE `+where+` LIMIT 500`, arg)
		if err != nil {
			return n, err
		}
		type row struct{ id, key, upload string }
		var batch []row
		for rows.Next() {
			var r row
			if err := rows.Scan(&r.id, &r.key, &r.upload); err != nil {
				rows.Close()
				return n, err
			}
			batch = append(batch, r)
		}
		rows.Close()
		if len(batch) == 0 {
			return n, nil
		}
		for _, r := range batch {
			if r.upload != "" {
				if err := s.store.AbortMultipart(ctx, r.key, r.upload); err != nil && !errors.Is(err, objstore.ErrNotFound) {
					return n, fmt.Errorf("%w: %v", ErrStore, err)
				}
			}
			if err := s.store.Delete(ctx, r.key); err != nil && !errors.Is(err, objstore.ErrNotFound) {
				return n, fmt.Errorf("%w: %v", ErrStore, err)
			}
			if _, err := s.db.Q(ctx).Exec(ctx, `DELETE FROM artifacts WHERE id = $1`, r.id); err != nil {
				return n, err
			}
			n++
		}
	}
}
