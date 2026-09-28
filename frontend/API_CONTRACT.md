# Dashboard API Contract

Status: implemented by `backend/cmd/api` and live-verified (2026-09-16 → -18);
the machine-readable form is `backend/deploy/openapi.yaml` (tested against the
router) and this file is its prose. Ingest routes use bearer tokens; the
dashboard uses a separate cookie session. Do not send ingest tokens from the
dashboard or treat service structs as HTTP DTOs.

## Common Behavior

- Same origin `/v1/`, JSON; all dates RFC3339 UTC. Render dates in local time.
- Server enforces tenant ancestry and authorization on every resource lookup.
- API client includes credentials. Session cookies are HttpOnly, Secure and
  SameSite=Lax. Browser code never reads/stores the session secret.
- POST requires `X-CSRF-Token`; server validates Origin/Referer and CSRF.
- Errors use SPEC 7.1 `{error:{code,message,details}}`, not raw Go errors.
  Request ID comes from `X-Request-Id`. UI uses stable codes/status, not message
  parsing. Field-level errors must be agreed before adding editable forms.
- 401 invalidates client session and in-flight reads. 403 is permission denied,
  404 absent or concealed, 409 conflict, 429 throttled. No automatic POST retry.
- Cursor strings are opaque. Responses use `{items:[],nextCursor:null|string}`;
  maximum page size is enforced both server-side and at client DTO validation.
- Run sort: `startedAt DESC, id DESC`, stable tie-breaker. Changing filters resets
  the cursor. Cursor must be bound to project/filter/sort by the server.

## Session

`GET /v1/session` returns 200 even for anonymous bootstrap:

```json
{"user":null,"csrfToken":"server-issued-token"}
```

`POST /v1/session` takes `{email,password}`. It returns the same session shape,
with `user:{id,name,email}` and a rotated CSRF token. Invalid credentials use a
generic 401; rate limit is enforced by backend. Login must return a user.
`POST /v1/session/logout` takes `{}` and returns 200 `{}` or 204 after revoking
the server session. UI clears state only after server confirmation or a 401.

### Google / GitHub sign-in (§11.2, implemented 2026-09-27)

- The bootstrap carries `providers`: the OAuth sign-ins switched on in the
  api's configuration (`OAUTH_GOOGLE` / `OAUTH_GITHUB`, off by default).
  The sign-in page shows "Continue with …" only for those; unknown names are
  ignored.
- A button is a plain link (a full-page trip, not an XHR):
  `/v1/session/oauth/{google|github}/start?redirect=<path>`. The api sends
  the browser to the provider and back to its callback, which ends in one of:
  - the app at `redirect`, signed in;
  - `/sign-in?step=mfa&redirect=…` when the account has MFA on — the page
    opens the code step and posts `POST /v1/session/mfa {code}` with an empty
    challenge (the api holds it in an HttpOnly cookie);
  - `/sign-in?oauth=<reason>`, reason in `denied|state|provider|no_account|
    conflict|disabled|unavailable`, each shown in plain words.
- No account is created: the provider's verified email must match an existing
  verified account here (the first sign-in links them).
- Security page, "Connected accounts": `GET /v1/session/identities` →
  `{items:[{id,provider,email,createdAt,lastLoginAt}]}` (own links only);
  `DELETE /v1/session/identities/{id}` → 204, or `409 LAST_SIGN_IN` when it is
  the account's only way in (explained in words).

### Invitations by email (§11.2, implemented 2026-09-28)

- The bootstrap carries `invitations: true` when the api has `MAIL=smtp`. The
  members form then asks for any email and says unknown ones get an
  invitation.
- `POST …/members` for an email with no account answers **202**
  `{invitation:{id,email,role,level,invitedBy,createdAt,expiresAt}}` (a mail
  went out); `502 MAIL_FAILED` means nothing was created. With mail off it is
  still `404 USER_NOT_FOUND`.
- `GET /v1/{projects|workspaces|organizations}/{id}/invitations` lists the
  open ones; `DELETE …/invitations/{invitationId}` withdraws one (owner
  invitations only by owners).
- The mail links to `/invite#<token>`. The page reads the fragment, removes
  it from the address bar and history, and posts it in a body:
  `POST /v1/invitations/lookup {token}` → `{email,role,level,scopeName,
  invitedBy,expiresAt}`; `POST /v1/invitations/accept {token,password}` →
  session (signed in). `404 INVITATION_NOT_FOUND` (used, withdrawn,
  expired), `409 ACCOUNT_EXISTS` (the role was added; sign in),
  `400 INVALID_PASSWORD` (12+ characters), `429` after repeated bad tokens.

### Forgot password (§11.2, implemented 2026-09-28)

- The bootstrap carries `passwordReset: true` with `MAIL=smtp`; the sign-in
  page then shows "Forgot password?" → `/forgot`.
- `POST /v1/session/password/forgot {email}` → **202** with the same body for
  any email (never tells whether an account exists); `429` after 5 requests
  per email or IP in 15 minutes; `404 PASSWORD_RESET_OFF` without mail.
- The mail links to `/reset#<token>` (30 minutes, single use, replaced by a
  newer request). The page strips the fragment and posts
  `POST /v1/session/password/reset {token,password}` → 204;
  `404 RESET_NOT_FOUND`, `400 INVALID_PASSWORD` (token not spent). Every
  session is signed out and nobody is signed in: the page sends the person to
  sign in (MFA still asked if on).

### Two-factor authentication (§11.2, implemented 2026-09-27)

- The session user carries `mfa:{enabled,required}`. `required` means an org
  owner: until `enabled`, every route except `/v1/session*` answers
  `403 MFA_ENROLLMENT_REQUIRED`, so the router sends such a user to
  `/account/security` and nowhere else.
- With MFA on, `POST /v1/session` answers **202**
  `{mfa:"required",challenge,expiresAt}` and sets no cookie. The sign-in form
  turns into a code step: `POST /v1/session/mfa {challenge,code}` returns the
  normal session shape. `code` is a 6-digit TOTP or a recovery code
  (`xxxxx-xxxxx`). `400 INVALID_CODE` keeps the step; `401 MFA_CHALLENGE_EXPIRED`
  (five minutes, single use) goes back to the password.
- Security page: `GET /v1/session/mfa` → `{enabled,required,recoveryCodesLeft}`;
  `POST …/setup` → `{secret,otpauthUri}` (the link must start with
  `otpauth://totp/`); `POST …/enable {code}` → `{recoveryCodes:[10]}`, shown
  once, other sessions revoked; `POST …/disable {code}` → 204, or
  `409 MFA_MANDATORY` for owners (the UI offers no "turn off" to them).

## Projects

`GET /v1/projects?limit=50&cursor=...` returns a page of
`{id,name,workspaceName,actions}`. `actions` is the server-computed subset of
`view|triage|identity.merge|token.manage|project.configure|project.manage|member.manage|org.manage|data.delete|testmgmt.write|release.decide` for that project. Only projects the session can read appear. Selector
is a paginated project page, avoiding an unbounded app-wide dropdown cache.

## Runs

`GET /v1/projects/{projectId}/runs` accepts `limit=20`, optional `cursor`,
`q` (max 200 characters; branch/commit/run search), `status`, `environment`
(name filter), and `sort=-startedAt`. Returns a page of explicit Run DTOs.
`GET /v1/projects/{projectId}/runs/{runId}` returns one Run DTO. `runId` is the
internal Run ID, not reporter `localRunId`.

```json
{
  "id":"uuid", "branch":"main", "commitSha":"abc123",
  "status":"passed", "environmentName":"Staging",
  "startedAt":"2026-09-14T09:00:00Z", "durationMs":92400,
  "totalCount":128, "passedCount":126, "failedCount":0, "flakyCount":2
}
```

Counts and status are computed by the server. `passedCount` excludes flaky
passes; `failedCount` includes failed/timed_out/interrupted results; unreported
or skipped results occupy the remainder. Duration is nullable while running.
`passed` plus positive `flakyCount` must display an amber warning. Unknown
statuses get neutral text, never an inferred success.

### JUnit import (§6.8.5, QĐ-13, implemented 2026-09-28)

`POST /v1/projects/{projectId}/imports/junit?environment=&framework=&branch=&commit=&startedAt=`
with the report as the raw body (`Content-Type: application/xml`, ≤ 20 MiB),
CSRF like any mutation. Needs `project.configure` in the project's
`actions[]` — the Runs page shows "Import JUnit" only then. `201` a new
run, `200` the same report again (`created: false`, nothing added):

```json
{ "runId":"uuid", "localRunId":"uuid", "created":true, "tests":3, "attempts":5,
  "duplicates":0, "status":"failed", "flakyCount":1,
  "startedAt":"2026-03-02T09:15:00Z", "finishedAt":"2026-03-02T09:15:04Z" }
```

Errors the form explains in its own words: `INVALID_JUNIT` (the server's
message says what — e.g. no date: give `startedAt`; `ApiError.detail`
carries it), `INVALID_PAYLOAD`, `RUN_CONFLICT` (409), `REPORT_TOO_LARGE`
(413). The run is dated from the report, so it sorts into history, not to
the top of the list. CI imports the same way with a token:
`POST /v1/imports/junit` (`project:ingest`).

### ROI (§6.8.3, QĐ-13, implemented 2026-09-28)

`GET /v1/projects/{projectId}/roi?month=YYYY-MM` (UTC; default current;
not future → 400 `INVALID_QUERY`), any viewer:

```json
{ "month":"2026-09", "failures":42, "triaged":31, "triageCoverage":0.74,
  "untriagedRate":0.26, "ttrcMedianMinutes":52, "baseline":{…}|null,
  "savedHours":61.4|null, "flakyHours":6.2, "flakyRetryHours":3.7,
  "markedFlaky":6, "investigationCounted":true, "fewTriaged":false,
  "minTriaged":10 }
```

`baseline: null` / `savedHours: null` means "not enough data" — never show
0 for it. `savedHours` may be negative. Money is never in the API: the page
multiplies hours by a rate the reader types and keeps nowhere.
`GET …/roi/baselines` lists; `POST …/roi/baselines` (`project.configure`)
takes `{kind, failures, medianMinutes, p90Minutes?, rerunPassRate?,
flakyInvestigationMinutes?, periodFrom, periodTo, note?}` → 201 baseline with
`enteredBy`.

## Results

`GET /v1/projects/{projectId}/runs/{runId}/results?limit=50&cursor=...` returns:

```json
{"items":[{"id":"uuid","displayName":"Checkout succeeds","filePath":"tests/checkout.spec.ts","finalStatus":"passed","attemptCount":1,"durationMs":1234}],"nextCursor":null}
```

`GET /v1/projects/{projectId}/results/{resultId}` returns a bounded result
detail. The result must include `stableTestId` when resolved. Attempts are
ordered by `attempt ASC` and include `attempt`, `status`, `durationMs`, and
optional `error:{message,stack}`. Triage events include `id`, `type`,
`actorName`, and `occurredAt`.

Each attempt also carries `artifacts[]` — the **verified** uploads linked to
it (§7.7), oldest first, at most 20: `{id,name,kind,contentType,sizeBytes,
expiresAt}`. Pending, rejected or expired uploads are never listed. Linking is
eventual (the worker sweep, within a minute of upload), so a run that finished
seconds ago may show none yet.

`GET /v1/projects/{projectId}/artifacts/{artifactId}/content` streams the
bytes of a verified artifact to a project member (same session cookie, plain
`<a href>`). The server owns rendering policy (§11.5): `Content-Disposition:
inline` only for `image/png|webp|jpeg`, `video/webm|mp4` and `text/plain`,
`attachment` for everything else (traces, HAR, logs, markdown), always
`X-Content-Type-Options: nosniff`, `Cache-Control: private, no-store` and a
sandboxing CSP. The UI links to it and never embeds artifact bytes in its own
document. Unknown, foreign-project or unverified ids are `404`.

`POST /v1/projects/{projectId}/results/{resultId}/triage-events` takes
`{type,payload}`. Allowed P1 types are `FAILURE_SEEN`, `MARKED_FLAKY`,
`MARKED_KNOWN_ISSUE`, and `RESOLVED`; the server owns authorization, audit,
dedupe (`FAILURE_SEEN` user/result/24h), and TTRC effects. UI refreshes the
detail after a successful mutation and never computes a replacement score.

## Identity And Flaky

`GET /v1/projects/{projectId}/identities?view=flaky&limit=20&cursor=...`
returns `{id,canonicalTitle,canonicalFilePath,verdict,flakyScore,totalRuns,
recentStatuses[]}`. `view=flaky` is server filtering, not a client threshold.
`flakyScore=null` and `totalRuns < 2` display `Insufficient data`.

`GET /v1/projects/{projectId}/identities/{identityId}` returns one identity.
`GET /v1/projects/{projectId}/identities/{identityId}/runs?limit=20&cursor=...`
returns the bounded run DTO page for that identity. The server defines the
history ordering and verifies that the identity belongs to the project.

## Engineering Overview (§2.5 EM screen, P1.5)

`GET /v1/projects/{projectId}/metrics/triage?days=30` returns the window's
`{failures, triaged, seen, untriagedRate, triageCoverage, ttrcMedianSeconds,
timeToFirstViewSeconds}`; the two medians are `null` when nothing in the window
qualifies. Rates are ratios in `[0,1]` computed by the server — the client
formats, never re-derives them from the counts.
`GET /v1/projects/{projectId}/metrics/trend?days=90` returns
`{items:[{week, category, count}]}`: `week` is the start of the ISO week in
**UTC** (the bucket is part of the contract, not the connection's timezone),
`category` is the closed §8.7 enum (`unknown` = not triaged yet). The client
refuses categories outside the enum. `days` defaults to 30 / 90.

## Identity Operations (§9.4 manual tools, P2 cut lifted 2026-09-18)

Reading needs triage rights; every change needs `identity.merge` (admin in the
three-role matrix) and is written to `identity_audit` with the session user as
actor. The UI shows the server's `403`/`409` per action, never pre-judges the
role.

- `GET /v1/projects/{projectId}/review?limit=100` → `{items:[ReviewItem]}` —
  pending pairs, oldest first: `{id, createdAt, identity:{id, canonicalTitle,
  canonicalFilePath, status}, candidate:{…}, fingerprint, score, runnerUpId,
  runnerUpScore, gap, breakdown, runId}`.
- `POST /v1/projects/{projectId}/review/{itemId}` `{decision: "merged"|"kept"}`
  → `{id, status, decidedBy, decidedAt}`; `409 CONFLICT` once decided.
- `GET /v1/projects/{projectId}/identities/{identityId}/aliases` →
  `{items:[{id, fingerprint, source, matchScore, createdBy, validFrom,
  validTo}]}` (`validTo: null` = in force).
- `POST …/identities/{identityId}/merge` `{into}` → `{mergedInto}`;
  `POST …/identities/{identityId}/undo-merge` `{}` → `{restored}`;
  `POST …/identities/{identityId}/split` `{fingerprint, at}` → `201
  {newIdentityId}` (the fingerprint must be an alias of that identity).
- `Identity` DTO gained `status` (`active|merged|retired`) and `mergedIntoId`
  (nullable): merged and retired identities stay readable by id but leave the
  active list; the history page shows a notice and offers Undo for merged ones.

## Test Management (§6.8, P2 — backend and UI implemented 2026-09-21)

All under `/v1/projects/{projectId}`; reads need `view`, writes need
`testmgmt.write` (member), the release decision needs `release.decide`
(admin). Lists are `{items:[…]}` without cursors (bounded, ≤ 200–500).
Source of truth: `backend/deploy/openapi.yaml` (tag `testmgmt`).

- Suites: `GET|POST /suites` → `{id, parentId, name, cases, createdAt}`.
- Requirements: `GET|POST /requirements`, `PATCH /requirements/{id}`
  `{title?, priority?, status?}` → `{id, externalKey, title, priority
  (p0..p3), status (draft|active|done|dropped), coveredBy, createdAt,
  updatedAt}`.
- Test cases: `GET /test-cases?suite=&automation=&critical=true|false&q=`,
  `POST /test-cases`, `GET|PATCH /test-cases/{id}` → `{id, suiteId,
  suiteName, externalKey, title, steps:[{action, expected}], expected,
  priority, isCritical, automationStatus
  (manual_only|automation_candidate|automated|obsolete), ownerId, links?,
  requirements?, createdAt, updatedAt}` — `links`/`requirements` only on a
  single case. Steps are text; render with interpolation only.
- Automation link (§6.8.1): `POST /test-cases/{id}/links` `{stableTestId,
  linkType?}` → `201 {id, testCaseId, stableTestId, linkType, createdBy,
  createdAt}`; `409` for a merged/retired identity, `404` for an identity
  of another project. `DELETE /test-cases/{id}/links/{linkId}` → 204.
- Coverage: `POST|DELETE /test-cases/{id}/requirements/{requirementId}` → 204.
- Import: `POST /test-cases/import` with `text/csv` or JSON `{csv}` →
  `{created, updated, skipped, suites, requirements, errors:[string]}`.
- Plans: `GET|POST /test-plans`, `GET /test-plans/{id}` (with
  `executions`: latest per case), `PATCH /test-plans/{id}` `{status}`,
  `POST /test-plans/{id}/executions` `{testCaseId, status
  (passed|failed|blocked|skipped), note?, executedAt?}` → 201; `409` on a
  closed plan. Every execution carries `evidence: [{id, name, kind
  ("manual_evidence"), contentType, sizeBytes, expiresAt}]`.
- Evidence (§6.8.2): `POST /test-plans/{id}/executions/{executionId}/evidence`
  as `multipart/form-data` with one `file` part (the only non-JSON body in
  the contract; the client sends `FormData` and no `Content-Type` of its
  own) → `201` with the evidence ref. Accepted: `image/png|webp|jpeg`,
  `video/webm|mp4`, `application/pdf`, `text/plain`; the server sniffs the
  bytes and answers `400 INVALID_PAYLOAD` when they do not match the
  declared type, `413 ARTIFACT_TOO_LARGE` over 20 MiB, `409` on a closed
  plan. Bytes come back through `GET /artifacts/{id}/content` like any
  artifact (`inline` for images/video/text).
- Releases: `GET|POST /releases`, `GET /releases/{id}`,
  `POST /releases/{id}/evaluate` → release with `gate: {verdict
  (approved|warn|blocked|no_evidence), evaluatedAt, runId, branch,
  rules:[{id, level (blocked|warn), passed, applies, summary, details}],
  p0Coverage}`; `POST /releases/{id}/decision` `{status
  (blocked|approved|shipped), note?}` → release with `decidedBy` (the
  session user). `status` is the human decision and is independent of
  `gate.verdict`; an `approved` next to a `blocked` gate is an override to
  show as such. `shipped` is terminal (`409` afterwards).
- CI (not a browser route): `GET /v1/releases/{id}/gate` with a bearer
  token holding `project:read`.

## Members (§11.3.2, implemented 2026-09-27)

Every route needs `member.manage` (workspace admin, org admin, owner; not a
maintainer); the sidebar shows "Members" only when the project's `actions`
contain it.

- `GET /v1/projects/{projectId}/members?limit=100&cursor=...` → page of
  `{userId,email,role,level,createdAt}`, ordered by email then level. `level`
  other than `project` is inherited from the workspace/org and read-only here.
  The UI walks pages (≤ 20) since a roster is small.
- `POST /v1/projects/{projectId}/members` `{email, role}` with `role` in
  `maintainer|member|viewer` → the project-level member. Re-posting changes the
  role. `404 USER_NOT_FOUND` when no verified account uses the email (there is
  no invite mail yet); the repository turns it into advice, other 404s stay
  generic.
- `DELETE /v1/projects/{projectId}/members/{userId}` → 204; 404 for a role
  held only on the workspace/org.

Every grant and revoke is recorded server-side with the session user as actor.

### Workspaces and organizations (Team screens)

- `GET /v1/member-scopes` → `{items:[{level,id,name,organizationName,actions}]}`:
  the organizations and workspaces the caller manages (org admin/owner: the
  org and all its workspaces; workspace admin: that workspace). Non-empty →
  the sidebar shows "Team" (`/team`). A 403 (owner not yet enrolled in MFA) is
  not cached; the sidebar asks again on navigation and when MFA turns on.
- `/v1/workspaces/{workspaceId}/members` and
  `/v1/organizations/{organizationId}/members` take the same GET / POST /
  DELETE as the project route. Grantable: `workspace_admin` on a workspace;
  `org_admin` and `org_owner` on an organization. `org_owner` is offered only
  when the org's `actions` contain `org.manage`; an owner's row is read-only
  otherwise.
- `403 OWNER_ONLY`: an org admin tried to grant, change or remove the owner
  role. `409 LAST_OWNER`: the change would leave the org without an owner.
  Both get their own words in the repository; a refused role change reloads
  the list so the select shows the stored role again.

## Handoff And Verification

Backend owner must confirm session bootstrap semantics, list limits, query
field names, result ordering, and DTO count definitions before live acceptance.
Fixture preview implements only these contracts and is not backend evidence.
Current client contract tests validate fields, cursor encoding, error handling,
and request lifecycle; cross-tenant server tests remain a backend gate.

## Backend status (16-09-2026)

Implemented by `backend/` as the single dashboard contract — the earlier
`/v1/auth/*`, `/v1/me/projects` and `{runs: [...]}` shapes were removed, not
kept alongside. Every endpoint above is mounted in `backend/cmd/api/dashboard.go`
(reads on `internal/query`, cursors keyset-paged and bound to project + filter +
sort, run sort `startedAt DESC, id DESC`) and `internal/authz/login.go`
(`/v1/session` bootstrap issues the CSRF cookie; login requires the token and
rotates it).

Confirmed semantics: `user.name` is the email local part until profiles exist;
`environment` filters by environment key; `q` matches branch, commit and
localRunId (≤ 200 chars, `%`/`_` literal); result ordering is failures first
then `displayName, id`; identity `flakyScore`/`verdict` are the worst signal
across environments, `recentStatuses` newest first (≤ 30); `…/runs/{runId}/results`
for a run outside the project is 404, never an empty page; `view=flaky` keeps
`suspect|flaky|quarantine_suggested`; `HYPOTHESIS_ACCEPTED` is not a UI type yet
(400) — the operator CLI records it with a category.

Verification: `backend/cmd/api/api_test.go` drives the exact handler the binary
serves through this contract on a real Postgres (session bootstrap → login →
projects → runs/cursor/filters → results → detail → triage-events → identities
→ identity runs → cross-tenant 404s → viewer 403 → CSRF 403 → logout), and a
live run of this Vue app (Vite dev server, no route mocks) against the Go
backend passed sign-in, projects, runs, run detail, result detail + triage
button, identities and the flaky view — 18 real `/v1` calls, all 200/201
except the intentional wrong-password 401.
