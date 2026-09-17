// Command admin is the operator's script for P1-min (§4.4: "identity
// operations as script/CSV", §11.2: no dashboard login yet): it creates
// tenants, users, roles and ingest tokens straight against the database.
// It is the only way to get an ingest token today, which keeps the
// "no demo to interviewees" rule (QĐ-11 #5) mechanical — there is no UI to
// show.
//
//	admin org create   -name "Acme" -slug acme
//	admin ws  create   -org <id> -name "Payments"
//	admin project create -ws <id> -name checkout [-branch main]
//	admin user create  -email alice@example.com [-password '…' -verified]
//	admin user set-password -id <user> -password '…'   (revokes the user's other sessions, §11.2)
//	admin user verify  -id <user>
//	admin role grant   -user <id> -level project -scope <id> -role admin
//	admin token issue  -project <id> -name "github-actions" [-expires 2027-01-01]
//	admin token revoke -id <id> -actor you@x            (recorded in authz_audit, §11.7)
//	admin role  revoke -user <id> -level project -scope <id> -actor you@x
//	admin audit list   [-scope <id>] [-limit 100]        (token/role revokes; identity ops are in identity_audit)
//	admin token list   -project <id>
//	admin identity show       -id <identity>
//	admin identity merge      -project <id> -dst <identity> -src <identity> -actor you@x
//	admin identity undo-merge -project <id> -src <identity> -actor you@x
//	admin identity split      -project <id> -fingerprint fp1:… -at 2026-09-14T00:00:00Z -actor you@x
//	admin review list         -project <id> [-limit 100] [-csv]
//	admin review decide       -id <item> -decision merged|kept -actor you@x
//	admin flaky list          -project <id> [-limit 50]
//	admin triage record       -project <id> -result <testResult> -type HYPOTHESIS_ACCEPTED -actor you@x [-category app_bug] [-note "..."]
//	admin triage list         -project <id> -result <testResult>
//	admin dash runs           -project <id> [-env <id>] [-limit 20]
//	admin dash run            -project <id> -run <id>
//	admin dash result         -project <id> -result <id>
//	admin dash identities     -project <id> [-view flaky]
//	admin dash history        -project <id> -identity <id>
//	admin dash triage         -project <id> [-days 30]
//	admin dash trend          -project <id> [-days 90]
//
// Every command prints the created object as JSON on stdout; the token
// plaintext is printed once and never again (§11.4). The identity and
// review commands are §9.4 in its P1-min form (§4.4: script + CSV): Merge,
// UndoMerge and SplitFrom are three different operations (N43) — when an
// alias has source auto_match and the match was wrong, the answer is
// undo-merge, not split.
package main

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/vinh/automation-testing-platform/backend/internal/authz"
	"github.com/vinh/automation-testing-platform/backend/internal/flaky"
	"github.com/vinh/automation-testing-platform/backend/internal/identity"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/config"
	"github.com/vinh/automation-testing-platform/backend/internal/platform/pg"
	"github.com/vinh/automation-testing-platform/backend/internal/query"
	"github.com/vinh/automation-testing-platform/backend/internal/results"
	"github.com/vinh/automation-testing-platform/backend/internal/tenancy"
	"github.com/vinh/automation-testing-platform/backend/internal/triage"
)

func main() {
	if len(os.Args) < 3 {
		usage()
	}
	cfg, err := config.Load()
	if err != nil {
		fail(2, err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	db, err := pg.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		fail(2, err)
	}
	defer db.Close()
	ten := tenancy.New(db)
	az := authz.New(db, []byte(cfg.TokenPepper))
	idn := identity.New(db)
	res := results.New(db)
	flk := flaky.New(db, res)
	tri := triage.New(db, res)
	dash := query.New(db)

	fs := flag.NewFlagSet(os.Args[1]+" "+os.Args[2], flag.ExitOnError)
	var out any
	switch os.Args[1] + " " + os.Args[2] {
	case "org create":
		name, slug := fs.String("name", "", "display name"), fs.String("slug", "", "url slug")
		parse(fs)
		out, err = ten.CreateOrganization(ctx, *name, *slug)
	case "ws create":
		org, name := fs.String("org", "", "organization id"), fs.String("name", "", "display name")
		parse(fs)
		out, err = ten.CreateWorkspace(ctx, tenancy.OrganizationID(*org), *name)
	case "project create":
		ws, name, branch := fs.String("ws", "", "workspace id"), fs.String("name", "", "display name"), fs.String("branch", "main", "default branch")
		parse(fs)
		out, err = ten.CreateProject(ctx, tenancy.WorkspaceID(*ws), *name, *branch)
	case "user create":
		email, password, verified := fs.String("email", "", "email"), fs.String("password", "", "initial password (≥ 12 chars)"), fs.Bool("verified", false, "mark the email verified now")
		parse(fs)
		var u authz.User
		if u, err = az.CreateUser(ctx, *email); err == nil && *password != "" {
			err = az.SetPassword(ctx, u.ID, *password, "")
		}
		if err == nil && *verified {
			err = az.VerifyEmail(ctx, u.ID)
		}
		if err == nil {
			u, err = az.GetUser(ctx, u.ID)
		}
		out = u
	case "user set-password":
		id, password := fs.String("id", "", "user id"), fs.String("password", "", "new password (≥ 12 chars)")
		parse(fs)
		err = az.SetPassword(ctx, *id, *password, "")
		out = map[string]string{"user": *id, "password": "updated; other sessions revoked"}
	case "user verify":
		id := fs.String("id", "", "user id")
		parse(fs)
		err = az.VerifyEmail(ctx, *id)
		out = map[string]string{"user": *id, "email": "verified"}
	case "role grant":
		user, level, scope, role := fs.String("user", "", "user id"), fs.String("level", "project", "organization|workspace|project"), fs.String("scope", "", "scope id"), fs.String("role", "", "admin|member|viewer")
		parse(fs)
		out, err = az.GrantRole(ctx, *user, authz.ScopeRef{Level: authz.Level(*level), ID: *scope}, authz.Role(*role))
	case "token issue":
		project, name, expires := fs.String("project", "", "project id"), fs.String("name", "", "label, e.g. github-actions"), fs.String("expires", "", "YYYY-MM-DD, default one year")
		parse(fs)
		spec := authz.TokenSpec{Scope: authz.ScopeRef{Level: authz.LevelProject, ID: *project}, Permissions: []authz.Permission{authz.PermProjectIngest}, Name: *name}
		if *expires != "" {
			spec.ExpiresAt, err = time.Parse("2006-01-02", *expires)
			if err != nil {
				fail(2, err)
			}
		}
		// The project must exist: authz does not know tenancy's tables (§6.9.5).
		if _, err = ten.GetProject(ctx, tenancy.ProjectID(*project)); err == nil {
			out, err = az.IssueToken(ctx, spec)
		}
	case "token revoke":
		id, actor := fs.String("id", "", "token id"), fs.String("actor", "", "who (email) — recorded in authz_audit, §11.7")
		parse(fs)
		err = az.RevokeToken(ctx, *id, *actor)
		out = map[string]string{"revoked": *id, "actor": *actor}
	case "role revoke":
		user, level, scope, actor := fs.String("user", "", "user id"), fs.String("level", "project", "organization|workspace|project"), fs.String("scope", "", "scope id"), fs.String("actor", "", "who (email) — recorded in authz_audit, §11.7")
		parse(fs)
		err = az.RevokeRole(ctx, *user, authz.ScopeRef{Level: authz.Level(*level), ID: *scope}, *actor)
		out = map[string]string{"revoked": *user, "level": *level, "scope": *scope, "actor": *actor}
	case "audit list":
		scope, limit := fs.String("scope", "", "organization|workspace|project id (default: everything)"), fs.Int("limit", 100, "max rows, newest first")
		parse(fs)
		out, err = az.ListAudit(ctx, *scope, *limit)
	case "token list":
		project := fs.String("project", "", "project id")
		parse(fs)
		out, err = az.ListTokens(ctx, authz.ScopeRef{Level: authz.LevelProject, ID: *project})
	case "identity show":
		id := fs.String("id", "", "identity id")
		parse(fs)
		var ident identity.Identity
		if ident, err = idn.Get(ctx, *id); err == nil {
			var aliases []identity.Alias
			aliases, err = idn.Aliases(ctx, *id)
			out = map[string]any{"identity": ident, "aliases": aliases}
		}
	case "identity merge":
		project, dst, src, actor := fs.String("project", "", "project id"), fs.String("dst", "", "surviving identity"), fs.String("src", "", "identity to fold in"), fs.String("actor", "", "who (email)")
		parse(fs)
		err = idn.Merge(ctx, *project, *dst, *src, *actor)
		out = map[string]string{"merged": *src, "into": *dst}
	case "identity undo-merge":
		project, src, actor := fs.String("project", "", "project id"), fs.String("src", "", "identity that was merged away"), fs.String("actor", "", "who (email)")
		parse(fs)
		err = idn.UndoMerge(ctx, *project, *src, *actor)
		out = map[string]string{"restored": *src}
	case "identity split":
		project, fp, at, actor := fs.String("project", "", "project id"), fs.String("fingerprint", "", "fp1:… (see identity show)"), fs.String("at", "", "RFC 3339 split point"), fs.String("actor", "", "who (email)")
		parse(fs)
		var t time.Time
		if t, err = time.Parse(time.RFC3339, *at); err == nil {
			var newID string
			newID, err = idn.SplitFrom(ctx, *project, *fp, t, *actor)
			out = map[string]string{"fingerprint": *fp, "newIdentity": newID, "from": t.UTC().Format(time.RFC3339)}
		}
	case "review list":
		project, limit, csvOut := fs.String("project", "", "project id"), fs.Int("limit", 100, "max items"), fs.Bool("csv", false, "CSV instead of JSON")
		parse(fs)
		var items []identity.ReviewItem
		if items, err = idn.ListReview(ctx, *project, *limit); err == nil && *csvOut {
			w := csv.NewWriter(os.Stdout)
			_ = w.Write([]string{"item_id", "created_at", "new_identity", "candidate", "score", "runner_up", "runner_up_score", "gap", "fingerprint", "run_id", "decision(merged|kept)"})
			for _, it := range items {
				_ = w.Write([]string{it.ID, it.CreatedAt.Format(time.RFC3339), it.IdentityID, it.CandidateID, fmt.Sprintf("%.4f", it.Score), it.RunnerUpID, fmt.Sprintf("%.4f", it.RunnerUpScore), fmt.Sprintf("%.4f", it.Gap), it.Fingerprint, it.RunID, ""})
			}
			w.Flush()
			return
		}
		out = items
	case "flaky list":
		project, limit := fs.String("project", "", "project id"), fs.Int("limit", 50, "max rows")
		parse(fs)
		out, err = flk.ListWorst(ctx, *project, *limit)
	case "triage record":
		project, result, typ, actor, category, note := fs.String("project", "", "project id"), fs.String("result", "", "test result id"), fs.String("type", "", "§8.3 event type"), fs.String("actor", "", "who (email)"), fs.String("category", "", "§8.7 category for HYPOTHESIS_ACCEPTED"), fs.String("note", "", "free text")
		parse(fs)
		rec := triage.Record{ProjectID: *project, TestResultID: *result, Type: triage.EventType(*typ), ActorType: triage.ActorUser, ActorID: *actor, Category: triage.Category(*category)}
		if *note != "" {
			rec.Payload = map[string]any{"note": *note}
		}
		out, _, err = tri.Record(ctx, rec)
	case "triage list":
		project, result := fs.String("project", "", "project id"), fs.String("result", "", "test result id")
		parse(fs)
		out, err = tri.ListForResult(ctx, *project, *result)
	case "dash runs":
		project, env, status, limit := fs.String("project", "", "project id"), fs.String("env", "", "environment key (optional)"), fs.String("status", "", "run status (optional)"), fs.Int("limit", 20, "rows")
		parse(fs)
		out, err = dash.Runs(ctx, *project, query.RunFilter{Environment: *env, Status: *status, Limit: *limit})
	case "dash run":
		project, run := fs.String("project", "", "project id"), fs.String("run", "", "run id")
		parse(fs)
		out, err = dash.Results(ctx, *project, *run, 100, "")
	case "dash result":
		project, id := fs.String("project", "", "project id"), fs.String("result", "", "test result id")
		parse(fs)
		out, err = dash.ResultDetail(ctx, *project, *id)
	case "dash identities":
		project, view := fs.String("project", "", "project id"), fs.String("view", "", "flaky | all")
		parse(fs)
		out, err = dash.Identities(ctx, *project, *view, 100, "")
	case "dash history":
		project, id := fs.String("project", "", "project id"), fs.String("identity", "", "stable test id")
		parse(fs)
		out, err = dash.IdentityRuns(ctx, *project, *id, 100, "")
	case "dash triage":
		project, days := fs.String("project", "", "project id"), fs.Int("days", 30, "window")
		parse(fs)
		out, err = dash.Triage(ctx, *project, time.Now().Add(-time.Duration(*days)*24*time.Hour))
	case "dash trend":
		project, days := fs.String("project", "", "project id"), fs.Int("days", 90, "window")
		parse(fs)
		out, err = dash.CategoryTrend(ctx, *project, time.Now().Add(-time.Duration(*days)*24*time.Hour))
	case "review decide":
		id, decision, actor := fs.String("id", "", "review item id"), fs.String("decision", "", "merged|kept"), fs.String("actor", "", "who (email)")
		parse(fs)
		err = idn.DecideReview(ctx, *id, identity.ReviewStatus(*decision), *actor)
		out = map[string]string{"item": *id, "decision": *decision}
	default:
		usage()
	}
	if err != nil {
		fail(1, err)
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(out)
}

func parse(fs *flag.FlagSet) { _ = fs.Parse(os.Args[3:]) }

func fail(code int, err error) {
	fmt.Fprintln(os.Stderr, "admin:", err)
	os.Exit(code)
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: admin <org create|ws create|project create|user create|user set-password|user verify|role grant|role revoke|token issue|token revoke|token list|audit list|identity show|identity merge|identity undo-merge|identity split|review list|review decide|flaky list|triage record|triage list|dash runs|dash run|dash result|dash identities|dash history|dash triage|dash trend> [flags]")
	os.Exit(2)
}
