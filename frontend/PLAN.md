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
- Historical stack decision: SPEC QD-9 selected Flutter Web in P1.
- Current implementation decision: Vue.js, explicitly requested by the user on
  2026-09-14. This is a deliberate deviation from QD-9 and must be reviewed
  against the product constraints before production acceptance.
- P1-min scope: SPEC section 4.4 and section 6.9.11.

Follow SPEC for product behavior. The user's explicit Vue decision supersedes
QD-9's framework choice; do not revert to Flutter on resume.

## Current Status

- Phase: Vue foundation/auth/shell/runs implemented and locally verified.
- Target: Vue.js web client only.
- Backend status: health and ingest routes are mounted; identity, queue and
  object-store packages now exist. Dashboard session/query handlers are still
  missing (checked against `cmd/api/main.go` on 2026-09-14).
- Preview data: allowed only through an explicit fixture transport. Live API
  failures must never silently fall back to fixtures.
- Last reviewed: 2026-09-14.
- Latest verification: frontend typecheck, 17 unit tests, production build and
  bundle gate pass; backend `go test ./...` also passes. These are compile and
  contract checks only; they do not prove a live dashboard API exists.
- Latest visual pass: reduced template-like sidebar treatment, tightened
  workspace/navigation hierarchy, added restrained active rail, and removed a
  stray nav markup character found during screenshot review.
- Fixed a medium-width identity list regression: fixed grid columns could
  consume the whole row and collapse the title column to `0px` at the
  `platform/tests` viewport. The new 641-900px layout keeps title, verdict,
  score, and chevron visible while hiding only the secondary history bars.
- Fixed navigation state leakage where any project route marked both `Test
  runs` and `Test intelligence` active. Browser regression coverage now
  asserts exactly one active project navigation item on each route.
- Refined inactive navigation hover so it cannot visually mimic the active
  state; only the current route receives a filled background.

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

- EM dashboard; this is P1.5.
- Review Queue UI; P1-min uses CSV/script operations.
- Merge/Split UI; P1-min uses CSV/script operations.
- Desktop and mobile application targets; the current client is Web only.
- Trace viewer integration until the artifact contract and safe iframe policy
  are implemented.

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

These routes are the frontend contract proposal. Before connecting live data,
confirm each route and response against backend handlers and `SPEC.md`.

| Feature | Method and route | Backend state |
| --- | --- | --- |
| Session | `GET /v1/session` | Not mounted |
| Login | `POST /v1/session` | Not mounted |
| Logout | `POST /v1/session/logout` | Not mounted |
| Projects | `GET /v1/projects` | Not mounted |
| Runs | `GET /v1/projects/{projectId}/runs` | Query handler missing |
| Run detail | `GET /v1/projects/{projectId}/runs/{runId}` | Query handler missing |
| Results | `GET /v1/projects/{projectId}/runs/{runId}/results` | Query handler missing |
| Result detail | `GET /v1/projects/{projectId}/results/{resultId}` | Query handler missing |
| Triage event | `POST /v1/projects/{projectId}/results/{resultId}/triage-events` | Not mounted |
| Identities | `GET /v1/projects/{projectId}/identities` | Query HTTP missing |
| Identity history | `GET /v1/projects/{projectId}/identities/{identityId}/runs` | Query HTTP missing |

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
The Flutter files remain inactive reference material; Vite builds only `src/`.

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
- [ ] Confirm backend response fields (live integration pending).
- [x] Add repository/composable tests, including stale requests and navigation.

### Slice 3: Run Detail

- [x] Run summary and bounded result list.
- [x] Add result pagination behavior tests through bounded DTO/repository checks.
- [ ] Confirm aggregate fields are server-provided (live integration pending).

### Slice 4: Failure And Triage

- [x] Technical block with selectable text and Copy action.
- [x] Result detail page with attempts, error and stack blocks.
- [x] Emit `FAILURE_SEEN` through the repository path; server dedupe remains authoritative.
- [x] Add triage actions and event timeline.

### Slice 5: History And Flaky

- [x] Identity model/repository contract.
- [x] Identity history page.
- [x] Flaky view and server verdict display.
- [x] Display insufficient-data state when `totalRuns < 2`.

### Slice 6: Verification And Integration

- [x] `npm run typecheck`.
- [x] `npm run test:unit`.
- [ ] Live API integration against the mounted Go handlers.
- [x] Responsive web check at 360px, 768px and 1440px.
- [x] Verify no raw HTML report rendering and no silent fixture fallback.
- [x] Verify route chunks, lazy loading, request count, and no fetch loop.
- [ ] Profile render/update cost with representative large customer datasets.

## Existing Draft To Retire Or Port

- `lib/` contains a Flutter draft created before the Vue.js decision. It is
  reference material only until explicitly ported; do not extend it.

These files are a foundation draft. Do not add another API client, model set,
pagination helper, status badge, or fixture layer without first checking these
files and updating this plan.

## Change Log

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

All commands below ran from `frontend/` on 2026-09-14.

| Check | Result |
| --- | --- |
| `npm run build` (includes typecheck + bundle gate) | PASS; initial JS 41.2 KiB gzip, nine lazy modules, no fixture markers |
| `npm run test:unit` | PASS; 17 tests in 7 files |
| `npm install` dependency audit after version fixes | 0 known vulnerabilities reported |
| `node scripts/browser-check.mjs <installed-playwright-path>` | PASS; projects, runs, result detail, attempts/copy/triage, identity/flaky/history, 20-row paging/reload, empty states, sign-out/in, 360/768/1440, console clean |
| `node scripts/browser-api-check.mjs <installed-playwright-path>` | PASS with mocked HTTP on live mode; request counts, 401/403, CSRF and generic login errors |
| `go test ./...` from `backend/` | PASS; domain, authz, ingest, artifact, queue, architecture tests |
| Real Go session/query integration | BLOCKED BY BACKEND SCOPE: required handlers and dashboard session schema absent |

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
