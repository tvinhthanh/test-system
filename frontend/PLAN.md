# Frontend Implementation Plan

This is the working plan for the Vue.js Web client. Update this file before
starting a new slice and after completing or changing one. It is the continuity
record for the implementation; do not use it as a substitute for `SPEC.md`.

Frontend conventions are in [`FRONTEND_RULES.md`](FRONTEND_RULES.md). Read
that file before changing frontend code.

## Source Of Truth

- Product and architecture requirements: `../SPEC.md`.
- Backend conventions and current implementation: `../backend/README.md` and
  `../backend/`.
- Stack decision: Vue 3 + TypeScript, explicitly requested by the user on
  2026-09-14. SPEC QD-9 was rewritten for Vue on 2026-09-18 (it had said
  Flutter Web); its four constraints are now stated for the DOM and point at
  the tests that enforce them.
- P1-min scope: SPEC section 4.4 and section 6.9.11.

Follow SPEC for product behavior. Do not revert to Flutter on resume; the
draft was removed on 2026-09-18.

## Current Status

### Active Slice (2026-10-15)

Implementation is pushed through `76f6df0`. The GĐ2 code slices below are
implemented and locally verified; the current work is closing CI and deployment
gates, not rebuilding those features. Team acceptance remains separate.

| Workstream | Status | Evidence / Remaining Work |
| --- | --- | --- |
| Reporter retry identity and response-body timeout | verified locally; CI reporter passed | Regression tests and reporter job on 96607e6 |
| Frontend dependencies / clean install | verified in CI | npm ci, unit tests, build/bundle gate and audit passed on 96607e6 |
| GĐ2 daily tester workflow | implemented; technically verified locally | Snapshot, step results, bug URL/retest/history, fixed plan scope, assignment/status, context/audit, bulk and CSV; real UI scripts passed |
| Backend integration | verified in CI | gofmt, go vet and full go test with real Postgres/S3 passed on 96607e6 |
| Backend security gate | verified in CI | govulncheck failed on 96607e6 (9 stdlib advisories and 1 x/text advisory); 7347b0e moved to go1.26.9 + x/text 0.41 and scanner v1.9.0; backend job green on run #42 |
| New complete-stack E2E | verified in CI | Run #42 / 38384680965 on 76f6df0 passed live-check, complete manual workflow and bulk/export through Caddy and the real API; memory-store CI is not durable deployment acceptance |
| GĐ1 Windows deployment | in_progress | Docker stack too heavy for this laptop (Docker Desktop stopped 14-10); deployment test moves to another machine via `server/atp.sh`. Vercel projects cannot host the backend (needs Postgres, worker, object storage) |
| GĐ2 team acceptance | planned | Tester team must complete requirement -> case -> plan -> execution -> bug -> retest -> release and record feedback |
| GĐ3 taxonomy / GĐ4 specialized results | planned | Not implemented by the current slices; follow SPEC 6.8.7 after GĐ1/GĐ2 acceptance |
| GĐ5 pilot / GĐ6 VPS | planned | Real pilot cycles, server load/24-72h soak, then staging/migration/rollback on VPS |

Latest verified CI: [run #42 / 38384680965](https://github.com/tvinhthanh/test-system/actions/runs/38384680965)
on `76f6df0`: frontend, backend, reporter and E2E all passed. Backend includes
govulncheck, real Postgres/S3 tests, Docker build, compose, promtool and OpenAPI.
E2E logs confirm both manual workflow and selected/full-filter export.

Current implementation: `server/atp.sh doctor [--pilot]`, read-only deployment
preflight with fake-Docker regression tests and `server/README.md` acceptance
checklist. On this laptop, --pilot correctly fails: Docker engine unavailable,
memory artifacts and APP_ENV=dev. No deployment acceptance is claimed.

#### Next Actions

1. Validate the new read-only deployment doctor in CI, without changing the
   now-green application gates. Security and complete-stack E2E are verified
   on 76f6df0, not waiting on the old failing run anymore.
2. Finish demo deployment on the chosen host with durable artifacts, restart verification,
   backup outside the laptop and stack restore. Do not expose the native
   trust-auth/memory-store demo as the pilot deployment. Use server/README.md
   to record host-specific evidence; doctor is not deployment certification.
3. Run team acceptance for GĐ2, record issues and close critical gaps. Then
   start GĐ3 taxonomy, followed by the Performance result contract in GĐ4.

No percentage or completion date is assigned to external acceptance gates.

#### Completed And Verified

- Migration 0030 captures a case snapshot, step results, bug URL and same-case /
  same-plan retest link; legacy rows retain a null snapshot. Case version checked
  before saving; passed verdicts reject failed/blocked steps.
- Plan reads include the latest 200 executions and their original evidence with
  an explicit truncation flag. UI has execution recorder and history components;
  search does not change progress totals; executed obsolete cases stay visible.
- Real Postgres service/API/architecture tests passed. All frontend unit tests:
  77 passed; reporter: 20 passed, 1 opt-in outage test skipped; dependency audit: 0.
- `scripts/live-manual-history.mjs`: real login/API, step results, bug link,
  edited case preserves original snapshot, linked retest and original evidence;
  Chrome screenshots at 1440x960 and 390x844, no horizontal overflow/page errors.
- Full backend suite had a Windows harness port collision in restore; standalone
  restore drill with RESTORE_TEST=1 and PostgreSQL tools passed on rerun.
- Migration 0031 and the existing plan POST/GET/PATCH now fix plan case scope,
  assign verified project-authorized testers by email, and track not_run /
  in_progress / completed independently of execution verdicts. Changes write
  test_plan_audit in the same transaction. Recorded cases cannot be removed;
  closed plans reject changes and stale plan versions return 409.
- Scope editor, assignment and start controls passed the real Chrome/API flow
  in live-manual-history.mjs on desktop/mobile. New project cases do not enter
  an existing plan automatically; PATCH retains old execution evidence.
- Membership slice verification: 81 frontend unit tests passed; typecheck,
  build/bundle gate (initial JS 52.2 KiB gzip), real Postgres testmgmt/API/arch
  tests and OpenAPI lint passed (7 pre-existing warnings).
- Migration test starts from schema 0030 with legacy data, then applies 0031:
  existing evidence and obsolete recorded cases remain accessible. Execution
  forms retain the case version being filled and require explicit reload after
  a newer case arrives; unsaved step results cannot silently move to new steps.
- Migration 0032 adds bounded UTF-8 case preconditions/test data and captures
  transactional case audit for create/update/CSV/automation-status changes. Audit
  records the authenticated actor and server-side before/after data; API/UI expose
  the latest 50 summaries with an explicit truncation flag, no legacy backfill.
- Create/edit forms and CSV support context; absent CSV columns preserve existing
  context. Execution forms/history use immutable context snapshots and label
  unavailable legacy fields. Case edits send expectedUpdatedAt to prevent stale
  saves. Real Postgres tests verify preservation, limits, audit scope and actors.
- Context/audit verification: 84 frontend tests passed; typecheck/build and
  bundle gate passed (52.4 KiB gzip), backend testmgmt/API/architecture and
  OpenAPI lint passed (7 existing warnings). Live case edit/audit and original
  snapshot/evidence passed on Chrome at 1440, 768 and 360px widths, no horizontal
  overflow/page errors; screenshots test-results/case-context-*.png.
- Backup/restore drill rerun with migration 0032 and RESTORE_TEST=1 passed on
  the local PostgreSQL 17 instance; git diff --check passed.
- Bulk/export verification: 90 frontend tests passed; typecheck/build and bundle
  gate passed (52.7 KiB gzip). Postgres testmgmt/API/architecture tests passed:
  foreign project and viewer write denied, audit actor, stale partial success,
  full-filter export beyond 200 rows, 5000-row/8-MiB limits and CSV round-trip.
  Real Chrome script live-case-bulk.mjs verifies retained conflict selection/
  report, one confirmed success, real selected/full-filter downloads and no
  overflow at 1440/768/360px. Page header actions now wrap on narrow screens.
  npm audit --audit-level=low: 0 vulnerabilities. Fixes were subsequently pushed
  and the frontend audit passed in CI on 96607e6.
- Extended live-manual-history.mjs passed with requirement/case/plan created
  through UI, coverage linking, assignment, failure/bug/evidence, context edits,
  retest and persisted named release decision. A unique branch without an
  automation run returns no_evidence even when manual retest passes.
  Screenshots include manual-release-{desktop,tablet,mobile}.png.
- A linked requirement exposed an implicit grid's mobile minimum width; case
  detail now uses a minmax(0,1fr) base grid track. Real 360/768/1440px checks
  pass. CI E2E now runs both manual-history and bulk scripts through Caddy;
  bundled Chromium supported, internal TLS bypass limited to localhost.
- Follow-up CI fixes: gofmt applied; missing bundled emnapi core/runtime lock
  metadata restored from a fresh npm-generated lock without unrelated version
  updates. Clean npm ci in an isolated directory passed, audit 0. Committed
  frontend tests (excluding the unrelated local fixture test): 89 passed;
  typecheck/build/bundle gate passed. New GitHub E2E was skipped by the backend
  govulncheck failure; local script success is not full-stack CI acceptance.
- Historical GitHub run 38369351402 (96607e6): frontend and reporter passed;
  backend formatting, vet and real Postgres/S3 tests passed, govulncheck failed.
  Remaining backend checks and E2E were skipped. Superseded by green run #42.
- 15-10 verification: authenticated GitHub job/log reads confirm run #42 is
  fully green, including both new UI scripts. Fresh local scan with Go 1.26.9
  also reports 0 called vulnerabilities (18 uncalled module advisories remain).

- Phase: **P1-min UI complete** — sign-in, projects, runs, run detail,
  failure detail with attempts / artifacts (screenshots and video inline,
  trace viewer on our origin) / triage, identity history, flaky view —
  verified against the real Go backend (no mocks) on 2026-09-16, -17 and -18.
  Beyond P1-min, on the owner's call (2026-09-18, "all of this"): the §2.5
  engineering overview (P1.5) and the Review Queue / Merge-Split UI (§9.4)
  are in; OAuth, MFA and 6-role RBAC stay deferred.
- Target: Vue.js web client only. Visual system since 2026-09-21: Tailwind
  CSS 4 + shadcn-vue (reka-ui) on `src/shared/theme.css` tokens, Geist /
  Geist Mono self-hosted, light and dark palettes with a toggle in the shell.
  The owner judged the hand-rolled CSS "nhót" (cheap-looking) on 2026-09-20
  and chose this over a Next.js port: the framework was never the problem,
  the styling was.
- Backend status: every route in `API_CONTRACT.md` is mounted and inventoried
  in `../backend/deploy/routes.txt` (a test fails if the mux and the file
  differ). `scripts/live-check.mjs` drives the real UI through the real api:
  30 `/v1` calls, all 2xx except the deliberate wrong-password 401, console
  clean. Production shape: `../backend/deploy/` (Caddy serves `dist/` and
  proxies `/v1`).
- Preview data: allowed only through an explicit fixture transport. Live API
  failures must never silently fall back to fixtures.
- P2 (SPEC §6.8, QĐ-12) since 2026-09-21: Test Management screens — test
  cases (list, filters, create, CSV import, detail with steps / automation
  links / requirement coverage), requirements, test plans with manual
  executions, releases with the server-side quality gate and the human
  decision. Routes under `p/:project/{cases,requirements,plans,releases}`,
  repository `features/testmgmt/testmgmtRepository.ts`, vocabularies in
  `features/testmgmt/enums.ts`. Since 2026-09-22 a manual execution can carry
  evidence (§6.8.2): files uploaded from the plan page as multipart, stored as
  `manual_evidence` artifacts, shown as thumbnails/links per row.
- Last reviewed: 2026-09-22.
- Latest verification: typecheck, 51 unit tests, production build + bundle gate
  (+ vendored trace viewer copied to `dist/trace/`), `scripts/live-check.mjs`
  against the real api (also run by the CI `e2e` job on the compose stack);
  backend `go test ./...` green on Postgres.
- Earlier visual/regression fixes (2026-09-14: sidebar treatment, 641–900px
  identity list, single active nav item, hover state) are in the change log.

## P1-Min Product Scope

Build these screens in this order:

1. Sign-in and session bootstrap.
2. Project/workspace selector.
3. Run list with status, environment, branch, commit, duration, filters, and
   cursor pagination.
4. Run detail with summary metrics and test results.
5. Failure detail with attempts, error, stack, triage events, copy action, and
   `FAILURE_SEEN` emission.
6. Test identity history.
7. Flaky view with server-provided verdict, score, run count, and recent
   status history.

Explicitly out of the current slice:

- Desktop and mobile application targets; the current client is Web only.
- Embedding the trace viewer *inside* the failure page (iframe policy). It
  opens in a new tab on our origin instead (2026-09-18).

## Architecture Rules

Use feature-first MVVM-style modules with a strict data boundary:

```text
View
    -> ViewModel / composable
    -> Repository
    -> ApiClient
    -> Go API
```

Expected folders:

```text
src/
  app/
    router/
    providers/
  core/
    api/
    auth/
    state/
  features/
    auth/
    reports/
    triage/
  shared/
```

Rules:

- Vue components do not call HTTP directly.
- Repositories own endpoint paths and JSON mapping.
- ViewModels/composables own loading, empty, error, retry, pagination, and
  stale-request protection.
- Client never re-implements authorization, tenant ancestry, flaky scoring,
  identity resolution, or run aggregation.
- Report content is untrusted text. Do not render it through raw HTML.
- Technical text uses native selectable text/pre elements and an explicit Copy
  button; `v-html` is prohibited for report content.
- State-changing requests carry the CSRF token. Session authentication uses
  the backend cookie contract.
- A run with `status=passed` and `flakyCount > 0` is displayed as a warning,
  never as clean green.
- Use stable backend IDs in routes and keys. Do not use display names as IDs.

## API Contract To Confirm

`API_CONTRACT.md` is the single contract; the Go side implements exactly it
(`../backend/cmd/api/dashboard.go`, driven end-to-end by
`../backend/cmd/api/api_test.go`). State as of 2026-09-17:

| Feature | Method and route | Backend state |
| --- | --- | --- |
| Session | `GET /v1/session` | Mounted; live-verified |
| Login | `POST /v1/session` | Mounted; live-verified (401 generic on wrong password, CSRF rotates) |
| Logout | `POST /v1/session/logout` | Mounted; live-verified (204) |
| Projects | `GET /v1/projects` | Mounted; live-verified |
| Runs | `GET /v1/projects/{projectId}/runs` | Mounted; live-verified (`q`, `status`, `environment`, cursor) |
| Run detail | `GET /v1/projects/{projectId}/runs/{runId}` | Mounted; live-verified |
| Results | `GET /v1/projects/{projectId}/runs/{runId}/results` | Mounted; live-verified (failures first) |
| Result detail | `GET /v1/projects/{projectId}/results/{resultId}` | Mounted; live-verified (attempts + events) |
| Triage event | `POST /v1/projects/{projectId}/results/{resultId}/triage-events` | Mounted; live-verified (201, 200 on dedupe) |
| Identities | `GET /v1/projects/{projectId}/identities` | Mounted; live-verified (`view=flaky`) |
| Identity detail | `GET /v1/projects/{projectId}/identities/{identityId}` | Mounted; live-verified |
| Identity history | `GET /v1/projects/{projectId}/identities/{identityId}/runs` | Mounted; live-verified |
| Triage / trend metrics | `GET /v1/projects/{projectId}/metrics/{triage,trend}` | Mounted (EM dashboard is P1.5; not consumed by the UI yet) |

Required response conventions:

- Success responses use JSON objects.
- Lists use `{ "items": [...], "nextCursor": "..." }`.
- Errors use `{ "error": { "code": "...", "message": "...", "details": {} } }`.
- The server remains the authority for permission filtering and tenant scope.
- The UI must preserve `X-Request-Id` in displayed technical diagnostics when
  an error has one.

## Work Slices

Completed local flow: slices 0-2 and slices 3-5 on fixture contracts. The full
P1-min read/triage UI is now navigable locally. Next work is backend contract
confirmation and live integration; HTTP schemas remain a proposal until Go
handlers are mounted and tested.
Vite builds only `src/` (the Flutter draft is gone since 2026-09-18).

Foundation choice: Vue/Router/Pinia plus native Fetch and a small cancellable
resource composable. A query-cache library is deferred until multiple screens
need cache invalidation; current data is route-owned and session scoped.

### Slice 0: Foundation

- [x] Create Vue 3 + TypeScript package and base metadata.
- [x] Add API client boundary and typed API error.
- [x] Add resource composable with stale-request protection.
- [x] Add app bootstrap, dependency wiring, and router.
- [x] Add TypeScript and component test baseline.

### Slice 1: Auth And Shell

- [x] Session bootstrap and auth guard (fixture/mocked HTTP verified).
- [x] Sign-in form with non-enumerating server error display.
- [x] App shell with paginated project navigation.
- [x] Explicit live/fixture configuration.

### Slice 2: Runs

- [x] Run models, repository contract, and route composable.
- [x] Run list layout with URL filters and cursor controls.
- [x] Wire router and shell.
- [x] Confirm backend response fields (live against the Go api, 2026-09-16/17).
- [x] Add repository/composable tests, including stale requests and navigation.

### Slice 3: Run Detail

- [x] Run summary and bounded result list.
- [x] Add result pagination behavior tests through bounded DTO/repository checks.
- [x] Confirm aggregate fields are server-provided (run counts come from the api, live 2026-09-16/17).

### Slice 4: Failure And Triage

- [x] Technical block with selectable text and Copy action.
- [x] Result detail page with attempts, error and stack blocks.
- [x] Emit `FAILURE_SEEN` through the repository path; server dedupe remains authoritative.
- [x] Add triage actions and event timeline.
- [x] List each attempt's verified artifacts with links to the server's
      content route (2026-09-17). The server decides inline vs download
      (§11.5 allowlist); the UI never embeds artifact bytes.
- [x] §4.4 P1-min client-side handling of video + trace (2026-09-18):
      screenshots render as `<img>`, videos as `<video controls>` from the
      content route; `trace.zip` opens in Playwright's trace viewer served
      from our own origin at `/trace/` (vendored `playwright-core` 1.63.0,
      `scripts/copy-trace-viewer.mjs` → `dist/trace/`, Vite dev middleware),
      whose service worker fetches the zip with the session cookie — no
      customer trace ever goes to trace.playwright.dev.

### Slice 5: History And Flaky

- [x] Identity model/repository contract.
- [x] Identity history page.
- [x] Flaky view and server verdict display.
- [x] Display insufficient-data state when `totalRuns < 2`.

### Slice 6: Verification And Integration

- [x] `npm run typecheck`.
- [x] `npm run test:unit`.
- [x] Live API integration against the mounted Go handlers (`scripts/live-check.mjs`, repeatable).
- [x] Responsive web check at 360px, 768px and 1440px.
- [x] Verify no raw HTML report rendering and no silent fixture fallback.
- [x] Verify route chunks, lazy loading, request count, and no fetch loop.
- [x] Profile render/update cost with a representative large dataset
      (2026-09-17, against the T5 soak's api while its watched project held
      ~75 000 results / ~375 runs, Vite dev build through the proxy, laptop):
      runs page 331 ms, results page 327 ms, identities + flaky + one history
      1.2 s for three navigations, sign-in → projects 636 ms — indistinguishable
      from the same steps against a 3-run project, because every list is
      server-paged at 20/50 rows and the api holds p95 < 500 ms at 1M results
      (`../backend/test/load`). Numbers are `live-check.mjs` step wall times
      and include its fixed waits; rerun it against a bigger project to refresh.

## Retired Draft

- `lib/`, `web/`, `pubspec.yaml`, `analysis_options.yaml` were a Flutter draft
  from before the Vue decision. Removed 2026-09-18 (git history keeps it);
  nothing in the Vue app was ported from it.

These files are a foundation draft. Do not add another API client, model set,
pagination helper, status badge, or fixture layer without first checking these
files and updating this plan.

## Change Log

### 2026-09-29 (delete / restore a run)

- Run page: "Delete run" for `data.delete` (second click confirms), back to
  the Runs list. Runs page: "Recently deleted runs" with who / when / purge
  date and Restore (the list refreshes). Repository methods,
  `run-delete.test.ts`. live-check: the JUnit run is deleted, disappears,
  shows under Recently deleted, is restored and listed again.

### 2026-09-29 (delete / restore a project)

- Project Members page: "Delete this project" for `project.manage`, enabled
  only once the exact name is typed; goes to Projects. Workspace page:
  "Recently deleted projects" with who, when, purge date, Restore (opens the
  project's runs). `ProjectAccess.name()`, repository methods,
  `project-delete.test.ts`. Checked on the compose stack as a workspace
  admin: create → delete (wrong name keeps the button off) → not listed →
  restore → runs; no non-2xx, no console errors; full live-check green.

### 2026-09-29 (create a project)

- Workspace Team page: "New project" for `project.manage`; on success the
  project list cache is cleared and the new project opens on CI tokens.
  `MembersRepository.createProject`, fixture (and the fixture scopes now
  carry `project.manage`, as the server does), `project-create.test.ts`;
  checked in fixture mode: create → lands on `/p/…/tokens`.

### 2026-09-29 (CI tokens)

- `/p/:project/tokens` "CI tokens" (nav only with `token.manage`): issue
  (name, 30/90/365 days, optional release-gate read), the secret shown once
  with Copy and the ATP_TOKEN hint, list with prefix / last used / expiry /
  status, revoke with a confirm click. `TokensRepository` (strict decode),
  fixture, `tokens.test.ts`.
- `format.day()`: a date with its year — the first screenshot showed a
  one-year token "expiring 29 Sept, 01:16", i.e. apparently today.
- live-check: issue → secret shown → only the prefix listed → revoke →
  "revoked".

### 2026-09-29 (change password)

- Security page "Password": current + new twice; this browser stays signed
  in, others are signed out; wrong current, too short, OAuth-only and rate
  limit each explained. `SessionRepository.changePassword`, store method,
  fixture, `password.test.ts` (a WRONG_PASSWORD 400 never triggers the
  sign-out handler).
- live-check: wrong current → 400 shown, change, change back, still signed
  in; the one extra allowed non-2xx is that deliberate 400.

### 2026-09-28 (shadow meter, QĐ-13)

- `/team/organizations/:organization/usage` "Usage (shadow meter)", linked
  from Team for org owners: the §10.6 verdict first (ready, or the reasons),
  the latest day (active tests, raw fingerprints, runs, storage), then every
  day with its identity changes and drift, flagged days marked.
- `MeterRepository` (strict decode: negative drift or a non-boolean flag is
  refused — it caught a non-integer byte count in the fixture), fixture with
  a merge-wave day; `meter.test.ts`.

### 2026-09-28 (ROI, QĐ-13)

- `/p/:project/roi` "Return on triage" (nav "ROI"): month picker; coverage,
  untriaged, TTRC vs baseline first; "Not enough data" empty state without a
  baseline; hours saved and hours on flaky with their arithmetic in the note;
  an optional hourly rate that turns hours into money in the note only and is
  kept nowhere; baseline history; "Record a baseline" sheet for
  `project.configure`.
- Number inputs: v-model hands back numbers — the forms accept both (a
  live-check failure found it). Architecture test gets a 30 s timeout (it
  parses every file; 5.4 s under load broke the 5 s default).
- Tests: `roi.test.ts` (null stays null, negative savings, bad kind refused,
  POST body, labels, rate). live-check: fresh stack → "Not enough data" →
  maintainer records → hours → money appears only after a rate is typed.

### 2026-09-28 (JUnit import, QĐ-13)

- Runs page: "Import JUnit" (only with `project.configure`) opens a sheet —
  file, environment, framework, branch, a run date for reports without one;
  shows "Imported" / "Already imported" with a link to the run and refreshes
  the list.
- `ApiClient` sends a `Blob` body as its own bytes (`contentType` option)
  and keeps the server's message as `ApiError.detail` (the generic
  `message` is unchanged elsewhere). `ReportsRepository.importJUnit`.
- Fixture transport answers the upload offline. Tests: `junit-import.test.ts`
  (bytes not JSON, query, detail kept, bad answer refused).
- live-check step: follows `actions[]`; a maintainer uploads a real report
  and opens the new run. CI seeds the account as maintainer so the upload runs.

### 2026-09-28 (forgot password)

- Session decodes `passwordReset`; the store gains `forgotPassword` /
  `resetPassword` (token only in the body).
- `PasswordPage` serves `/forgot` (email → the same "check your email" for
  anyone) and `/reset` (token from the fragment, stripped from the URL; new
  password + confirm; used / replaced / expired link explained). Sign-in page
  shows "Forgot password?" when available.
- Verified: typecheck, 51 unit tests; a browser run on the real api with
  `MAIL=smtp` to a local SMTP catcher: forgot → mail over SMTP → link in a
  fresh browser → new password → signed in with it → link refused again;
  the token is in neither the api nor the Vite log.

### 2026-09-28 (invitations by email)

- Session decodes `invitations`; the store gains `lookupInvitation` /
  `acceptInvitation` (repository posts the token in the body only).
- `InvitePage` at `/invite` (public): reads the token from the fragment,
  strips it from the address bar and history, shows who invited whom to
  what, sets a password (12+, confirmed) and signs in; states for used /
  withdrawn / expired, account-exists and rate-limited.
- Members page: "Email" instead of "Email of an existing account" when mail
  is on, a sent notice, and "Pending invitations" with Withdraw (owner
  invitations only for owners). `MembersRepository.set` returns
  member-or-invitation; `invitations()` / `revokeInvitation()`.
- Verified: typecheck, 50 unit tests; a browser run on the real api + Postgres
  with `MAIL=smtp` to a local SMTP catcher: invite from the project Members
  page, the mail received over SMTP with a fragment link, the link opened in
  a fresh browser (token gone from the URL), password mismatch shown, account
  created and signed in with the project visible, the link refused a second
  time, the member listed and nothing pending; the token appears in neither
  the api nor the Vite log.

### 2026-09-28 (connected accounts)

- Security page: "Connected accounts" (`LinkedAccounts.vue`,
  `IdentitiesRepository`) lists the Google / GitHub sign-ins linked to the
  account with a two-step Disconnect; `LAST_SIGN_IN` explained; empty state
  says how to link (sign in with a provider using the same verified email).
  Fixture: one GitHub link, bootstrap providers.
- Verified: typecheck, 49 unit tests, screenshots light 1280 / dark 390,
  disconnect → empty state in fixture; backend `oauth_test.go`.

### 2026-09-27 (Google / GitHub sign-in)

- Bootstrap decodes `providers` (unknown names skipped); the sign-in page
  shows "Continue with Google / GitHub" as plain links for the switched-on
  ones, reads `?oauth=<reason>` into a sentence, and opens the code step on
  `?step=mfa` (`session.resumeMfa()`, empty challenge — the api reads its
  HttpOnly cookie).
- Verified: typecheck, 48 unit tests, screenshots light/dark/390 px; a
  browser run on the real api with both providers on (fake credentials):
  buttons shown, the Google link's 302 carries client id, the callback
  redirect URI, PKCE S256 and a state, sets the signed `atp_oauth` cookie; a
  forged-state callback lands on `/sign-in?oauth=state` with its sentence;
  restarting the api with `OAUTH_GITHUB=off` leaves only the Google button.
  The full round trip (code → token → identity → session, PKCE checked) is
  `backend/cmd/api/oauth_test.go` against a fake provider.

### 2026-09-27 (Team: workspace and organization members)

- `MembersRepository` takes a `MemberNode` (`project|workspace|organization`
  + id) instead of a project id, adds `scopes()` (`GET /v1/member-scopes`), and
  explains `OWNER_ONLY` / `LAST_OWNER`. `roles.ts` gains `grantable` per level.
- `TeamAccess`: the scopes the user manages; cached once answered, not on a
  403, cleared when the user changes. Sidebar "Team" link when non-empty,
  re-asked on navigation and when MFA turns on (live run found the link
  missing right after an owner enrolled).
- `TeamPage` (`/team`) lists them; `MembersPage` serves
  `/team/organizations/:organization` and `/team/workspaces/:workspace` as
  well as the project route: inherited rows read-only, `org_owner` offered and
  owner rows editable only with `org.manage`.
- Fixture: an org with two workspaces and a small roster.
- Verified: typecheck, 46 unit tests, bundle gate 49.0 KiB, screenshots
  (light/dark/390 px), and a browser run on the real api + Postgres: an owner
  reset by the operator enrols, sees org + workspace, cannot step down as last
  owner (select put back), makes an org admin; the org admin sees owner rows
  read-only, is offered only org admin, gets 403 OWNER_ONLY forcing it through
  the api, adds and removes a workspace admin; a project member has no Team
  link.

### 2026-09-27 (MFA: QR code on setup)

- `features/account/QrCode.vue`: the `otpauth://` link as a QR code, drawn as
  one SVG path from `uqr`'s module matrix (dependency `uqr@0.1.3`, MIT, no
  dependencies; lazy with the Security page, initial bundle unchanged at
  48.5 KiB). No `v-html`. Dark on a white tile in both themes.
- Verified by decoding screenshots of the rendered code with jsQR (light,
  dark, 390 px): each decodes to exactly the setup link.
- Backend counterpart: `admin user mfa-reset` for people who lost both phone
  and recovery codes.

### 2026-09-27 (two-factor authentication, §11.2)

- Session user decodes `mfa:{enabled,required}`; `SessionRepository.login`
  returns either a session or a challenge (202); the store keeps the
  challenge, `completeMfa(code)` opens the session, `refresh()` re-reads the
  flags after a change. Error codes `INVALID_CODE` and
  `MFA_CHALLENGE_EXPIRED` get their own words; an expired challenge returns
  the form to the password step.
- Sign-in page: code step (one-time-code autocomplete, recovery codes
  accepted, "Use another account").
- `features/account/`: `MfaRepository` (setup link must be `otpauth://totp/`,
  at most ten recovery codes) and `SecurityPage` at `/account/security` —
  set up with the key in groups of four + an `otpauth://` link, confirm with
  a code, recovery codes shown once (copy / download .txt), turn off with a
  code or recovery code (not offered to owners). Shield link in the sidebar
  footer, amber while an owner has not enrolled.
- Router guard: an owner without MFA reaches only `/account/security`.
- `decode.boolean` shared (testmgmt's local copy now uses it).
- Verified: typecheck, 44 unit tests, fixture screenshots light/dark/390 px,
  and a browser run against the real api + Postgres with an independent
  RFC 6238 authenticator: owner held on Security (api 403 agrees), enable
  with a wrong then right code, ten recovery codes, projects open, sign out,
  password → code step, wrong code kept on the step, recovery code signs in,
  9 left, no "turn off" for an owner.

### 2026-09-27 (project members, §11.3.2)

- `features/members/`: `MembersRepository` (roster pages, grant/change,
  revoke; `USER_NOT_FOUND` mapped to actionable text), role vocabulary in
  `roles.ts` so the screen imports no repository value, `MembersPage` at
  `/p/:project/members` — add by email + role, per-row role select and a
  two-step Remove on project-level rows, inherited rows read-only with
  "via workspace/organization".
- Sidebar "Members" item and breadcrumb only when `ProjectAccess` grants
  `member.manage`; fixture project `commerce` has it and a four-person roster.
- `live-check.mjs`: the Members link must match the api's `member.manage`
  for the signed-in account (absent for the CI member; page opens when
  present).
- Verified: typecheck, 40 unit tests, fixture screenshots light/dark/390 px,
  and a browser run against the real api + Postgres (workspace admin adds,
  changes and removes a member; the member sees no link, gets 403 on the
  api, and 404 on the project once removed).

### 2026-09-22 (project permissions and API hardening)

- Projects decode the server-computed `actions` vocabulary and a shared
  `ProjectAccess` cache resolves permissions for project deep links without
  inferring roles in components. The cache is cleared when the signed-in user
  changes, and only a full scan (not a later page reached by cursor) marks it
  complete.
- Mutation controls now follow the backend matrix: triage, identity merge,
  Test Management writes, and release decisions are hidden when their action
  is absent. The server remains authoritative for every request.
- `ApiClient` default timeout now matches SPEC §7.9 at 30 s; evidence upload
  keeps its explicit 120 s timeout.
- Browser check uses DOM text rather than CSS-transformed text for the failure
  detail marker and restores the documented E2E hook classes.

### 2026-09-22 (P2 — manual evidence on test plans, §6.8.2)

- `ApiClient.request`: a `FormData` body is sent as-is (multipart; the browser
  sets the boundary and the client sets no `Content-Type`), CSRF header as
  for any mutation; `timeoutMs` per request (an upload gets 120 s, not the
  15 s default); 413 has a user message.
- Repository: `Execution.evidence: Evidence[]` (tolerates its absence),
  `attachEvidence(project, plan, execution, File)` → one `FormData` with a
  `file` part, `evidenceContentUrl` (the shared artifact content route).
- `PlanDetailPage`: per row with a verdict, an evidence strip (images as
  48 px thumbnails from the content route, video/other as icon + name links,
  all `target=_blank`), a paperclip that opens a hidden `<input type=file>`
  bound to the server's accept list; disabled on a closed plan or a row
  without a verdict. Client-side 20 MB guard; a server 400 (sniff mismatch /
  type outside the policy) is reworded inline for the person; after a
  successful upload the plan is refetched, not spliced.
- Fixture: exec-2 carries one text evidence; `POST …/evidence` answers a PNG
  ref, so `dev:fixture` renders both chip kinds.
- `scripts/live-check.mjs`: new step 14 — new plan, verdict `failed`, a real
  1×1 PNG through the hidden picker, asserts the request was multipart and
  the thumbnail decoded (`naturalWidth`), screenshot `live-plan-evidence.png`.
- Tests: `tests/testmgmt.test.ts` + 1 (FormData body, no JSON content type,
  CSRF, decode on the plan, absence of `evidence` decodes to `[]`).

### 2026-09-21 (P2 — Test Management screens, §6.8)

- `ApiClient.request` gained `method: PATCH | DELETE`; every non-GET carries
  the CSRF header, a body-less DELETE sends no content type. GET/POST
  behaviour unchanged (client tests still pass).
- New feature folder `src/features/testmgmt/`: repository (paths + decoders
  for suites, requirements, cases, links, plans, executions, releases, gate,
  import report), `enums.ts` (closed vocabularies shared with screens),
  `useMutation` (one in-flight write per screen, error kept for the row
  that asked), `ErrorNote`, `TmTabs`, `GateBadge`.
- Screens: TestCasesPage (URL-state filters, create panel, CSV import with
  file picker and per-row error report), TestCaseDetailPage (editable title /
  priority / automation / critical, steps editor, automation links picked
  from the project's active identities with the server's 404/409 shown,
  requirement coverage), RequirementsPage (create, inline priority/status,
  p0 coverage line, `uncovered` badge), PlansPage + PlanDetailPage (progress
  tiles, one-click pass/fail/blocked/skipped with a note, close/reopen),
  ReleasesPage + ReleaseDetailPage (gate rules with level / applies / details,
  Evaluate, Approve / Block / Ship with note, override notice naming the
  decider, curl snippet for the CI gate URL).
- Sidebar gained "Test management"; breadcrumbs know the four sections;
  `tone()` learned the new statuses (approved/shipped/automated/done →
  success, blocked/dropped/uncovered → danger, warn/automation_candidate →
  warning, active/open → info).
- Fixture transport answers every Test Management route with a coherent set
  (two suites, three requirements, five cases, one plan, two releases — one
  blocked, one shipped by override), so `dev:fixture` shows the whole plane.
- `scripts/live-check.mjs`: 13th step for the plane against the real api.
- Verified: typecheck, 32 unit tests, prettier, bundle gate 46.6 KiB, live-check
  13/13 (58 calls), screenshots of the six screens reviewed.

### 2026-09-21 (visual system: Tailwind CSS 4 + shadcn-vue)

- Replaced the 1 468-line hand-written `tokens.css` with `src/shared/theme.css`:
  Tailwind 4 (`@tailwindcss/vite`), OKLCH semantic tokens (background, card,
  muted, accent, primary = brand green used only for primary action / ring /
  mark), reserved status tones with soft fills, the validated §8.7 chart
  palette as `--chart-<category>`, and a selected dark palette under `.dark`.
  Fonts: Geist + Geist Mono via `@fontsource-variable` (self-hosted).
- shadcn-vue `new-york` primitives generated into `src/components/ui/`
  (button, badge, input, label, table, skeleton, separator, alert, sheet,
  card) with `components.json` and the `@/` alias; `src/lib/utils.ts` (`cn`).
  Icons stay on `@lucide/vue`, which the generated components also use.
- New layout blocks in `src/shared/`: PageHeader, SectionHeading, ViewTabs /
  ViewTab, StatTile, EmptyState (heading level configurable), Pagination,
  BackLink, NativeSelect; StatusBadge / AsyncState / TechnicalBlock restyled.
  Shell: sidebar rail (`SidebarNav`) reused inside a focus-trapped Sheet on
  phones, breadcrumb top bar, `ThemeToggle` (light → dark → system, stored in
  `localStorage`, applied inline in `index.html` before first paint).
- Every page rebuilt on those blocks; hook classes the live-check and tests
  rely on were kept (listed in FRONTEND_RULES.md). Sign-in gained a product
  statement panel on wide screens. The overview chart widens its columns when
  the window has few weeks and thins tick labels so they never collide.
- Fixture transport now answers `metrics/triage`, `metrics/trend`, `review`
  and `identities/{id}/aliases`, so `npm run dev:fixture` renders every
  screen offline (it used to error on overview, review and fingerprints).
- Fixed: IdentitiesPage used `<GitMerge>` without importing it (the review
  queue tab rendered without its icon); status badge label casing.
- Verified: typecheck, 28 unit tests, bundle gate 44.4 KiB, prettier, and the
  12-step live-check on a real api (44 calls, console clean). Screenshots of
  every screen in light, dark and 390 px were reviewed by eye.

### 2026-09-18 (stack recorded in SPEC; Flutter draft removed)

- SPEC QD-9 now says Vue 3 + TypeScript (Vite, Pinia, vue-router) and states
  the four client constraints for the DOM, naming the tests that enforce them
  (`tests/architecture.test.ts`: no `v-html`, components never import a
  repository or the HTTP client). SPEC §0.5 carries the build progress.
- Deleted the inert Flutter draft (`lib/`, `web/`, `pubspec.yaml`,
  `analysis_options.yaml`, Dart entries in `.gitignore`). Typecheck and the 28
  unit tests pass without it; nothing in `src/` referenced it.

### 2026-09-18 (review queue and identity tools — §9.4 over HTTP)

- New route `p/:project/review` (third tab under Test intelligence): the
  pending pairs the resolver would not decide alone, both sides linked, score,
  gap, breakdown, run; "Same test — merge" / "Different tests — keep" with the
  server's answer shown per row.
- Identity history page: fingerprints table (in force / historical), a
  "merged into …" notice with Undo, a "retired" notice after a split, and the
  admin tools — merge into another test (radio list of active identities with
  a filter) and split from a point in time (fingerprint in force +
  `datetime-local`, navigates to the new identity).
- Backend: six dashboard routes (`identity_ops.go`), project-scoped
  `ReviewItem` lookup, `Identity.status`/`mergedIntoId`; tenant walk covers the
  new routes with a seeded review item and valid bodies; contract test drives
  member 403 → admin merge → undo → split → decide with the audit count.
- Verified in Chrome against the real api (keep → empty queue; merge → notice
  → undo; split → new identity page); `live-check.mjs` walks the queue and the
  fingerprints (44 `/v1` calls, clean). `tests/identity-ops.test.ts` pins the
  DTO decoders (review item, alias, identity status), the CSRF header on
  decisions, 403/409 surfacing as `ApiError`, and the merge/undo/split paths.

### 2026-09-18 (engineering overview — the §2.5 EM screen, first P1.5 slice)

- New route `p/:project/overview` and nav item "Overview": a KPI row (median
  time to root cause, time to first view, untriaged rate, triage coverage,
  each with the counts behind it) and "Failure categories by week" — a
  stacked weekly column chart with hover tooltip, legend and a table view,
  over a 30/90-day window kept in the URL (`?days=`). Data comes from the
  existing `metrics/triage` and `metrics/trend` routes; the client formats,
  never re-derives.
- Chart palette: the §8.7 categories in fixed order → the validated
  reference categorical slots 1–6 (dataviz skill validator: CVD and
  normal-vision floors pass; the light-mode contrast warning is relieved by
  the legend labels and the table view); `unknown` in de-emphasis gray.
  `categories.ts` owns the enum, its order and labels.
- Backend: `TrendBucket` gained camelCase JSON names (`week`, `category`,
  `count`) and weeks are truncated in UTC regardless of the session timezone;
  `openapi.yaml` updated.
- `live-check.mjs` walks the overview (tiles, hover, table, 90-day switch);
  35 `/v1` calls, all 2xx.

### 2026-09-18 (video, screenshots and traces on the failure page)

- Screenshot artifacts render inline (`img`, lazy), videos as `video controls
  preload=metadata`, both straight from the content route (the two kinds the
  server serves inline). Trace artifacts get "Open in trace viewer": the
  vendored Playwright trace viewer at `/trace/index.html?trace=<absolute
  content URL>` on our origin; its service worker fetches the zip with the
  session cookie. Verified in Chrome against the real api: 1280×720
  screenshots decoded, 2.3 s 800×450 videos with metadata parsed, the viewer
  shows the actions and the failing `toHaveText`.
- `playwright-core` 1.63.0 is a pinned devDependency only for its
  `lib/vite/traceViewer`; `npm run build` copies it to `dist/trace/`
  (skipped by the bundle gate as third-party); `vite dev` serves it from
  node_modules. Caddy serves `dist/trace/` like the rest of `dist/`.
- `TechnicalBlock` strips terminal colour codes (Playwright leaves `[2m`,
  `[31m` in assertion text); the reporter now strips them at the source too.
- Reporter example gained a real Chromium test that fails on purpose with
  trace/video/screenshot retained, so a demo run carries every kind.

### 2026-09-17 (artifacts reach the user)

- Result detail lists each attempt's verified artifacts (`artifacts[]` on
  the attempt DTO, ≤ 20) with `Paperclip` links to
  `GET …/artifacts/{id}/content`. Repository owns the path
  (`artifactContentUrl`); the server owns rendering policy — inline only for
  image/video/plain text, download otherwise, `nosniff`, `no-store`. Kinds the
  server will not render inline get a `download` attribute so the intent is
  visible before the click. Contract recorded in `API_CONTRACT.md`.
- `live-check.mjs` follows the first artifact link with the session cookie
  and asserts 200 + disposition + nosniff (skips when the run carried none).
- CI job `e2e` (`.github/workflows/ci.yml`) stands the compose stack up, seeds
  a tenant, pushes the reporter example over https and runs `live-check.mjs`
  with Playwright's bundled Chromium (`ATP_BROWSER_CHANNEL=bundled`).
- Vite dev proxy target is configurable (`ATP_API=http://host:port npm run dev`)
  so the live check can point at a soak's or a staging api.

### 2026-09-17 (live integration made repeatable; two fixes it found)

- Added `scripts/live-check.mjs`: the real Vue app against the real Go api,
  no route mocking. Sign-in (wrong password → generic 401), projects, runs,
  empty search state, run → results → result detail, triage from the UI,
  identities / flaky / history, sign-out. Every `/v1` call is listed with its
  real status; any non-2xx other than the deliberate 401, or any console
  error, fails the run. Screenshots in ignored `test-results/live-*.png`.
- Fixed: signing out remounted the current protected page for one frame
  (`RouterView` keyed on `session.user?.id`), which fired two unauthenticated
  fetches → 401s in the console. `RouterView` now renders only while a user
  is present.
- Fixed: a run with no branch (a reporter outside CI) rendered an empty,
  unclickable primary link and a `/ /` meta line. The link falls back to
  `(no branch)`; the commit segment is hidden when absent.
- Updated the API contract table: every route is mounted and live-verified;
  the backend inventories routes in `../backend/deploy/routes.txt`.

### 2026-09-14 (Vue implementation)

- Added `src/`, npm lockfile, Vite, Vue Router, Pinia session store, Fetch API
  client and feature repositories. Components consume composables/store.
- Added project selection, login/logout, run list and run detail. Search/status/
  environment/cursor are URL state; runs cap at 20 and results cap at 50.
- Added live same-origin `/v1` configuration and development-only fixture mode.
- Recorded proposed HTTP DTOs in `API_CONTRACT.md`; did not change Go handlers.
- Added architecture test for direct UI HTTP/data imports and raw HTML;
  build gate checks lazy routes, gzip budgets and production fixture exclusion.
- Fixed responsive overflow caused by visually-hidden table content escaping
  the scroll container. Narrow rows now stack without a second DOM list.
- Fixed delayed search navigation after Clear filters; regression test locks it.
- Existing Flutter draft retained without modifications; not part of npm build.

### 2026-09-14 (P1-min read/triage UI)

- Added result detail with bounded attempts, selectable/copyable error and
  stack text, triage actions, and event timeline.
- Added test identity list, flaky view, identity history, recent run links,
  server-provided verdict/score display, and insufficient-data handling.
- Added lazy routes and fixture flows for all local P1-min read/triage pages.
- Browser flow now verifies run -> failure -> triage -> identity/flaky/history;
  fixture evidence only until Go query/session/triage handlers are live.

## Latest Verification

All commands below ran from `frontend/`; dates per row.

| Check | Result |
| --- | --- |
| `npm run build` (includes typecheck + bundle gate) | PASS 2026-09-22 with manual evidence on the plan page; initial JS 46.8 KiB gzip, 18 lazy modules, no fixture markers |
| `npm run test:unit` | PASS; 33 tests in 10 files (2026-09-22: + evidence upload as FormData and its decoding; 2026-09-21: `tests/testmgmt.test.ts` — decoders, PATCH/DELETE with CSRF, 403/409/404 mapping, CI URL, CSV import) |
| `npx prettier --check "src/**/*.{vue,ts,css}"` | PASS (2026-09-22) |
| `node scripts/live-check.mjs` against a fresh throwaway Postgres + `cmd/api` + `cmd/worker` (memory store), Vite on :3000, `npm run example` runs | PASS 2026-09-22; **14 steps** (step 14: plan → verdict → PNG evidence uploaded as multipart → thumbnail decoded from `…/artifacts/{id}/content`; 68 `/v1` calls all 2xx except the deliberate 401). Earlier: PASS 2026-09-21; 13 steps (the new one creates a case, links it to a real identity, opens a release and evaluates the gate — verdict `blocked` on the seed data, five rules rendered), 58 `/v1` calls all 2xx except the deliberate 401, console clean |
| `npm install` dependency audit after version fixes | 0 known vulnerabilities reported |
| `node scripts/browser-check.mjs <installed-playwright-path>` | PASS; projects, runs, result detail, attempts/copy/triage, identity/flaky/history, 20-row paging/reload, empty states, sign-out/in, 360/768/1440, console clean |
| `node scripts/browser-api-check.mjs <installed-playwright-path>` | PASS with mocked HTTP on live mode; request counts, 401/403, CSRF and generic login errors |
| `ATP_UI=… ATP_EMAIL=… ATP_PASSWORD=… node scripts/live-check.mjs <installed-playwright-path>` (2026-09-17, real Go api on :8080 seeded by `reporter/playwright` `npm run example` ×2, Vite dev proxy) | PASS; 10 steps, 30 `/v1` calls all 2xx except the deliberate 401, console clean |
| same script, `ATP_API` pointed at the T5 soak's api (~75k results in the project) | PASS; step wall times: runs 331 ms, results 327 ms, identities+flaky+history 1.2 s — flat versus the 3-run project |
| `go test ./...` from `backend/` | PASS; domain, authz, ingest, artifact, queue, architecture tests |
| Real Go session/query integration | PASS (2026-09-16 first pass; 2026-09-17 via `scripts/live-check.mjs`) |

Browser evidence is in ignored `test-results/`: runs-desktop.png, runs-768.png,
runs-360.png and run-detail-desktop.png. Browser scripts accept the host's
Playwright package path as argument; it is not a runtime dependency of the app.
The fixture script expects port 5173; the mocked-HTTP script expects live dev
mode on 5174. Start the appropriate dev server before running each script.

Request observations: session bootstrap 1, project page 1, entering runs 1,
debounced search 1, detail 2 (summary + result page); no idle loop or row-level
fetches. These validate frontend behavior, not server tenant authorization.

Preview: `http://127.0.0.1:5173` in explicit fixture mode. Production deployment
and backend authentication are not claimed complete.

### 2026-09-14

- Created the living frontend plan.
- Confirmed P1-min product scope from SPEC section 4.4.
- Recorded the proposed dashboard API contract and the currently missing
  backend query/auth handlers.
- Recorded the existing Flutter foundation draft so it is not extended after
  the stack decision changed.

### 2026-09-14 (stack change)

- User selected Vue.js for the frontend.
- Marked the Flutter choice in SPEC QD-9 as a deliberate, user-approved
  deviation.
- Reset Slice 0 to Vue 3 + TypeScript and marked the existing Flutter code as
  draft material to retire or port deliberately.

## Review Gate Before Each New Slice

Before coding:

1. Read this file and the relevant SPEC section.
2. Check current git status and inspect existing feature files.
3. State the slice and its API dependencies.
4. Update the checklist before implementation if scope changes.

After coding:

1. Update the checklist and change log.
2. Record commands and verification results.
3. Record API gaps or assumptions.
4. Check for duplicate clients, models, routes, or widgets.
