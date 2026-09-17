// Package restore is the §11.5 / §14.3 drill: "backup chưa restore thử =
// chưa có backup". It seeds a world through the real binaries, takes a
// pg_dump the way deploy/backup/backup.sh does, restores it into a fresh
// database the way deploy/backup/restore.sh does, and then proves the
// restored copy is the same world: every table's row count and content
// checksum match, the migration ledger is complete with no drift, the api
// serves byte-identical dashboard answers from it, a worker started on it
// finishes the jobs that were pending at backup time, and new ingest lands.
// The wall-clock of dump + create + restore + boot is the measured RTO.
//
// Needs TEST_DATABASE_URL and pg_dump/pg_restore: PG_BIN, then PATH, then
// the Windows default install. Without the tools it skips — unless
// RESTORE_TEST=1, where a missing tool fails, because a machine that is
// supposed to prove the backup must have the tool that takes it.
package restore

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vinh/automation-testing-platform/backend/internal/authz"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/pg"
	"github.com/vinh/automation-testing-platform/backend/migrations"
	"github.com/vinh/automation-testing-platform/backend/test/harness"
)

func TestMain(m *testing.M) {
	code := m.Run()
	harness.Cleanup()
	os.Exit(code)
}

// pgTool finds pg_dump / pg_restore.
func pgTool(t *testing.T, name string) string {
	t.Helper()
	exe := name
	if runtime.GOOS == "windows" {
		exe += ".exe"
	}
	if bin := os.Getenv("PG_BIN"); bin != "" {
		return filepath.Join(bin, exe)
	}
	if p, err := exec.LookPath(name); err == nil {
		return p
	}
	if runtime.GOOS == "windows" {
		matches, _ := filepath.Glob(`C:\Program Files\PostgreSQL\*\bin\` + exe)
		if len(matches) > 0 {
			sort.Strings(matches)
			return matches[len(matches)-1] // highest version: a newer pg_dump reads an older server
		}
	}
	if os.Getenv("RESTORE_TEST") == "1" {
		t.Fatalf("%s not found: set PG_BIN or put it on PATH — the restore drill needs the tool the backup uses", name)
	}
	t.Skipf("%s not found (PG_BIN, PATH); skipping the restore drill", name)
	return ""
}

func run(t *testing.T, what string, bin string, args ...string) (time.Duration, string) {
	t.Helper()
	start := time.Now()
	cmd := exec.Command(bin, args...)
	out, err := cmd.CombinedOutput()
	took := time.Since(start)
	if err != nil {
		t.Fatalf("%s: %v\n%s %s\n%s", what, err, bin, strings.Join(args, " "), out)
	}
	return took, string(out)
}

// tableDigest is one table's row count and an order-independent content
// checksum: md5 over the text form of every row, sorted.
type tableDigest struct {
	rows int
	md5  string
}

func digests(t *testing.T, ctx context.Context, q pg.Querier, schema string) map[string]tableDigest {
	t.Helper()
	rows, err := q.Query(ctx, `SELECT tablename FROM pg_tables WHERE schemaname = $1 ORDER BY tablename`, schema)
	if err != nil {
		t.Fatal(err)
	}
	var tables []string
	for rows.Next() {
		var n string
		_ = rows.Scan(&n)
		tables = append(tables, n)
	}
	rows.Close()
	out := map[string]tableDigest{}
	for _, tbl := range tables {
		var d tableDigest
		var sum *string
		ident := pgx.Identifier{schema, tbl}.Sanitize()
		if err := q.QueryRow(ctx, `SELECT count(*), md5(string_agg(t::text, '|' ORDER BY t::text)) FROM `+ident+` t`).Scan(&d.rows, &sum); err != nil {
			t.Fatalf("digest %s: %v", tbl, err)
		}
		if sum != nil {
			d.md5 = *sum
		}
		out[tbl] = d
	}
	return out
}

func TestBackupRestoreDrill(t *testing.T) {
	pgDump, pgRestore := pgTool(t, "pg_dump"), pgTool(t, "pg_restore")
	h := harness.New(t)
	ctx := h.Ctx
	api := h.StartAPI()
	worker := h.StartWorker()

	// ---- a world worth restoring ----
	// Two pipelines, 24 runs × 100 tests with failures and a flaky retry,
	// four artifacts, a signed-in member who triaged a failure. The last
	// four runs finish with the worker dead so their run.finished jobs are
	// pending in the backup — a restore must pick those up, not lose them.
	tokens := []string{h.Token, h.IssueToken(h.Project, "second")}
	const runs, suite = 24, 100
	batch := func() map[string]any {
		var events []any
		for k := 1; k <= suite; k++ {
			status, outcome := "passed", "expected"
			if k%10 == 0 {
				status, outcome = "failed", "unexpected"
			}
			events = append(events, harness.EventWith(k, fmt.Sprintf("tests/area%02d.spec.ts", k%5), fmt.Sprintf("case %03d", k), status, outcome))
		}
		return map[string]any{"events": events}
	}
	body := batch()
	push := func(n int, finish bool) {
		tok := tokens[n%2]
		local := harness.LocalRun(n)
		if r := h.CreateRunAs(tok, local); r.Code != 201 {
			t.Fatalf("run %d create: %d %v", n, r.Code, r.Body)
		}
		if r := h.PostAs(tok, "/v1/runs/"+local+"/events", body); r.Code != 200 {
			t.Fatalf("run %d events: %d %v", n, r.Code, r.Body)
		}
		if finish {
			if r := h.PostAs(tok, "/v1/runs/"+local+"/finish", map[string]any{}); r.Code != 200 {
				t.Fatalf("run %d finish: %d %v", n, r.Code, r.Body)
			}
		}
	}
	for n := 1; n <= runs-4; n++ {
		push(n, true)
	}
	h.WaitFor("seed jobs to drain", 2*time.Minute, func() bool {
		s := h.JobsByStatus()
		return s["pending"] == 0 && s["running"] == 0
	})
	worker.Kill()
	for n := runs - 3; n <= runs; n++ {
		push(n, true)
	}
	if s := h.JobsByStatus(); s["pending"] != 4 {
		t.Fatalf("expected 4 pending jobs in the backup, have %v", s)
	}
	// Four artifacts on run 1: two verified, one rejected for a checksum
	// mismatch, one declared and never uploaded (pending). Their bytes live in
	// the object store, which has its own durability; the rows are the
	// database's part of the story and must survive the round trip.
	for i, corrupt := range []bool{false, false, true} {
		data := []byte(strings.Repeat(fmt.Sprintf("artifact %d line\n", i), 200))
		if d, p, c := h.Artifact(tokens[1], harness.LocalRun(1), fmt.Sprintf("log-%d.txt", i), data, corrupt); d.Code != 201 || p.Code != 200 || (!corrupt && c.Code != 200) || (corrupt && c.Code != 422) {
			t.Fatalf("artifact %d: declare %d put %d complete %d %v", i, d.Code, p.Code, c.Code, c.Body)
		}
	}
	if d := h.PostAs(tokens[1], "/v1/artifacts", map[string]any{"localRunId": harness.LocalRun(1), "clientTestId": "ct_tests_area01.spec.ts_case 0001", "occurrenceIndex": 0, "attempt": 0, "name": "never-uploaded.txt", "kind": "log", "contentType": "text/plain", "sizeBytes": 10, "sha256": strings.Repeat("ab", 32)}); d.Code != 201 {
		t.Fatalf("pending artifact: %d %v", d.Code, d.Body)
	}

	h.NewUser("ana@gate.example", "gate passphrase long enough", h.Project, authz.RoleMember)
	web := h.Login("ana@gate.example", "gate passphrase long enough")
	base := "/v1/projects/" + string(h.Project)
	runsPage := web.Get(base + "/runs?limit=50")
	runID, _ := runsPage.Items()[len(runsPage.Items())-1]["id"].(string) // an old, fully resolved run
	resultsPage := web.Get(base + "/runs/" + runID + "/results?limit=20")
	resultID, _ := resultsPage.Items()[0]["id"].(string)
	if r := web.Post(base+"/results/"+resultID+"/triage-events", map[string]any{"type": "FAILURE_SEEN", "payload": map[string]any{}}); r.Code != 201 {
		t.Fatalf("triage: %d %v", r.Code, r.Body)
	}
	// The dashboard answers that must come back identical from the copy.
	// Lists are fetched after every write, so nothing moves under them.
	pages := []string{
		"/v1/projects",
		base + "/runs?limit=50",
		base + "/runs/" + runID,
		base + "/runs/" + runID + "/results?limit=20",
		base + "/results/" + resultID,
		base + "/identities?limit=50",
		base + "/identities?limit=50&view=flaky",
		base + "/metrics/triage",
		base + "/metrics/trend",
	}
	before := map[string]string{}
	for _, p := range pages {
		r := web.Get(p)
		if r.Code != 200 {
			t.Fatalf("GET %s: %d %v", p, r.Code, r.Body)
		}
		before[p] = fmt.Sprint(r.Body)
	}

	// ---- the incident: the processes are gone; the database is the backup source ----
	api.Kill()
	schema := h.Schema()
	source := digests(t, ctx, h.DB.Q(ctx), schema)
	if source["runs"].rows != runs || source["test_results"].rows != runs*suite {
		t.Fatalf("seed: %d runs, %d results", source["runs"].rows, source["test_results"].rows)
	}

	// ---- backup, as deploy/backup/backup.sh does ----
	srcURL := os.Getenv("TEST_DATABASE_URL")
	dump := filepath.Join(t.TempDir(), "atp.dump")
	dumpTook, _ := run(t, "pg_dump", pgDump, "--format=custom", "--no-owner", "--no-privileges", "--schema="+schema, "--file="+dump, "--dbname="+srcURL)
	info, err := os.Stat(dump)
	if err != nil {
		t.Fatal(err)
	}
	_, listing := run(t, "pg_restore --list", pgRestore, "--list", dump)
	if !strings.Contains(listing, "TABLE DATA "+schema+" test_results") {
		t.Fatalf("dump does not list test_results data:\n%s", listing)
	}

	// ---- restore into a fresh database, as deploy/backup/restore.sh does ----
	// Same encoding and locale as the source: a collation change would
	// silently reorder every text index (§11.5 restore drill exists to find
	// exactly this kind of difference before the day it matters).
	var encoding, collate, ctype string
	if err := h.DB.Q(ctx).QueryRow(ctx, `SELECT pg_encoding_to_char(encoding), datcollate, datctype FROM pg_database WHERE datname = current_database()`).Scan(&encoding, &collate, &ctype); err != nil {
		t.Fatal(err)
	}
	target := "atp_restore_" + strings.TrimPrefix(schema, "t_")
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }
	createStart := time.Now()
	if _, err := h.DB.Q(ctx).Exec(ctx, fmt.Sprintf(`CREATE DATABASE %s TEMPLATE template0 ENCODING %s LC_COLLATE %s LC_CTYPE %s`,
		pgx.Identifier{target}.Sanitize(), quote(encoding), quote(collate), quote(ctype))); err != nil {
		t.Fatalf("CREATE DATABASE (the drill needs CREATEDB on TEST_DATABASE_URL's role): %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		_, _ = h.DB.Q(ctx).Exec(ctx, `DROP DATABASE IF EXISTS `+pgx.Identifier{target}.Sanitize()+` WITH (FORCE)`)
	})
	u, err := url.Parse(srcURL)
	if err != nil {
		t.Fatal(err)
	}
	u.Path = "/" + target
	targetURL := u.String()
	// Extensions live outside the schema (0004: pg_trgm WITH SCHEMA public)
	// and a schema-scoped dump does not carry them; restore.sh creates them
	// first, so does the drill.
	admin, err := pgx.Connect(ctx, targetURL)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx, `CREATE EXTENSION IF NOT EXISTS pg_trgm WITH SCHEMA public`); err != nil {
		t.Fatal(err)
	}
	admin.Close(ctx)
	createTook := time.Since(createStart)
	restoreTook, _ := run(t, "pg_restore", pgRestore, "--no-owner", "--no-privileges", "--exit-on-error", "--dbname="+targetURL, dump)

	// The restored copy, read through a pool pinned to the schema like the
	// binaries will be.
	cfg, err := pgxpool.ParseConfig(targetURL)
	if err != nil {
		t.Fatal(err)
	}
	cfg.AfterConnect = func(ctx context.Context, c *pgx.Conn) error {
		_, err := c.Exec(ctx, `SET search_path TO `+pgx.Identifier{schema}.Sanitize())
		return err
	}
	copyDB, err := pg.Open(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	restored := digests(t, ctx, copyDB.Q(ctx), schema)
	var names []string
	for n := range source {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		if restored[n] != source[n] {
			t.Errorf("table %s differs after restore: source %+v, restored %+v", n, source[n], restored[n])
		}
	}
	if len(restored) != len(source) {
		t.Errorf("%d tables restored, %d in the source", len(restored), len(source))
	}
	t.Logf("restore: %d tables, %d runs, %d results, %d artifacts, %d jobs (4 pending) — every row count and content checksum equal", len(source), source["runs"].rows, source["test_results"].rows, source["artifacts"].rows, source["jobs"].rows)

	// Migration ledger: the binary and the restored database must agree on
	// history — nothing to apply, no drift. A dump that missed
	// schema_migrations would make the next deploy try to re-run 0001.
	applied, err := pg.Migrate(ctx, copyDB, migrations.FS)
	if err != nil {
		t.Fatalf("migrate against the restored copy: %v", err)
	}
	if len(applied) != 0 {
		t.Errorf("restored copy was missing migrations: %v", applied)
	}

	// ---- the binaries on the copy ----
	h.DBURL = targetURL + "&search_path=" + schema + "&application_name=atp-restore-" + schema
	if !strings.Contains(targetURL, "?") {
		h.DBURL = targetURL + "?search_path=" + schema + "&application_name=atp-restore-" + schema
	}
	bootStart := time.Now()
	api2 := h.StartAPI()
	bootTook := time.Since(bootStart)
	rto := dumpTook + createTook + restoreTook + bootTook
	t.Logf("RTO on this machine: dump %s (%d KB) + create db %s + restore %s + api boot %s = %s (§11.5 budget 4 h)",
		dumpTook.Round(time.Millisecond), info.Size()/1024, createTook.Round(time.Millisecond), restoreTook.Round(time.Millisecond), bootTook.Round(time.Millisecond), rto.Round(time.Millisecond))

	// Sessions were in the dump too: the same cookie is still valid, and
	// every dashboard page answers exactly what it did before the incident.
	for _, p := range pages {
		r := web.Get(p)
		if r.Code != 200 {
			t.Errorf("after restore GET %s: %d %v", p, r.Code, r.Body)
			continue
		}
		if got := fmt.Sprint(r.Body); got != before[p] {
			t.Errorf("after restore GET %s differs:\n before: %s\n after:  %s", p, before[p], got)
		}
	}

	// The worker picks up the four jobs that were pending at backup time.
	worker2 := h.StartWorker()
	copyH := &copyWorld{t: t, db: copyDB}
	copyH.waitFor("pending jobs from the backup to finish", 2*time.Minute, func() bool {
		return copyH.count(`SELECT count(*) FROM jobs WHERE status IN ('pending', 'running')`) == 0
	})
	if n := copyH.count(`SELECT count(*) FROM jobs WHERE status = 'dead' OR last_error IS NOT NULL`); n != 0 {
		t.Errorf("%d jobs failed after restore:\n%s", n, worker2.Tail())
	}
	if n := copyH.count(`SELECT count(*) FROM test_results WHERE stable_test_id IS NULL`); n != 0 {
		t.Errorf("%d results unresolved after the restored worker drained", n)
	}
	// And the write path: a new run through the same token, ids and
	// sequences intact (a restore that lost a sequence would collide here).
	local := harness.LocalRun(runs + 1)
	created := h.CreateRunAs(tokens[0], local)
	if created.Code != 201 {
		t.Fatalf("post-restore create: %d %v", created.Code, created.Body)
	}
	if r := h.PostAs(tokens[0], "/v1/runs/"+local+"/events", body); r.Code != 200 || r.Body["accepted"] != float64(suite) {
		t.Fatalf("post-restore events: %d %v", r.Code, r.Body)
	}
	if r := h.PostAs(tokens[0], "/v1/runs/"+local+"/finish", map[string]any{}); r.Code != 200 {
		t.Fatalf("post-restore finish: %d %v", r.Code, r.Body)
	}
	copyH.waitFor("post-restore run to resolve", time.Minute, func() bool {
		return copyH.count(`SELECT count(*) FROM jobs WHERE status IN ('pending', 'running')`) == 0
	})
	if n := copyH.count(`SELECT count(*) FROM runs`); n != runs+1 {
		t.Errorf("runs after restore + one new: %d", n)
	}
	if r := web.Get(base + "/runs?limit=1"); r.Code != 200 || len(r.Items()) != 1 || r.Items()[0]["id"] != created.Str("runId") {
		t.Errorf("newest run after restore: %d %v", r.Code, r.Body)
	}
	if rto > 4*time.Hour {
		t.Errorf("RTO %s exceeds the §11.5 budget", rto)
	}
	api2.Kill()
	worker2.Kill()
	copyDB.Close()
}

// copyWorld is the harness's Count/WaitFor against the restored database.
type copyWorld struct {
	t  *testing.T
	db *pg.DB
}

func (c *copyWorld) count(q string) int {
	var n int
	if err := c.db.Q(context.Background()).QueryRow(context.Background(), q).Scan(&n); err != nil {
		c.t.Fatalf("%s: %v", q, err)
	}
	return n
}

func (c *copyWorld) waitFor(what string, d time.Duration, cond func() bool) {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	c.t.Fatalf("timed out after %s waiting for %s", d, what)
}
