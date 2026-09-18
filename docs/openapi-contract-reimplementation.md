# OpenAPI Contract Reimplementation Brief

Status: Implemented 2026-09-18 on `p1-min` (Option B) — see `backend/GATES.md` §T4 API9 for the evidence; `gopkg.in/yaml.v3` (already in the module graph) became the test-only parser

Owner: Claude implementation agent

Scope: `backend/deploy/openapi.yaml`, API contract enforcement, and focused regression tests

## 1. Objective

Reimplement the public OpenAPI contract so it is a reliable source of truth for
the HTTP API described by `SPEC.md` section 11.6 API9.

The result must satisfy all three layers at the same time:

1. The OpenAPI document describes the routes and wire contract actually served.
2. Runtime validation enforces the documented constraints.
3. Automated tests fail when the router, security constants, validation rules,
   or OpenAPI document drift apart.

Treat the current uncommitted `backend/deploy/openapi.yaml` and the current
`TestOpenAPIMatchesMountedRoutes` implementation as a draft. Reuse verified
content, but do not preserve behavior merely because it exists in that draft.

## 2. Sources of truth

Use this precedence when evidence conflicts:

1. Explicit decisions in this brief.
2. `SPEC.md`, especially sections 6.5, 7.1-7.4, and 11.6 API9.
3. Existing externally observable behavior that is covered by tests.
4. The official Playwright reporter behavior.
5. Comments and prose documentation.

Do not infer a contract solely from one current implementation branch.

## 3. Confirmed problems in the current draft

### 3.1 Session cookie name is wrong

The OpenAPI security scheme currently declares the cookie name as `atp_sess`.
The server reads and writes the cookie named `atp_session` through
`authz.SessionCookie`.

`atp_sess_` is the prefix of the opaque cookie value. It is not the cookie
name. Conflating these two concepts causes generated clients and API consumers
to send a credential the server never reads, resulting in `401` responses.

### 3.2 Event sequence lower bound is inconsistent

The OpenAPI draft declares `Event.sequence` with `minimum: 1`, while the ingest
validator currently accepts zero because it only rejects values below zero.

The official Playwright reporter starts at 1. Since `SPEC.md` declares OpenAPI
the source of truth, this reimplementation standardizes the public protocol on
a positive, 1-based sequence.

### 3.3 Route parity alone is insufficient

The current route test only compares path and method pairs. It cannot detect
drift in security scheme names, required headers, field constraints, or schema
semantics. The line-oriented parser is also coupled to YAML indentation and can
silently miss valid structural changes.

Route parity remains useful, but it must be backed by structural parsing and
focused semantic assertions for critical shared constants.

## 4. Contract decisions

These decisions are part of the task and must not be reopened during
implementation unless contradictory evidence is found in a newer approved
specification.

### 4.1 Browser authentication

- Session cookie name: `atp_session`.
- Session cookie value prefix: `atp_sess_`.
- CSRF cookie name: `atp_csrf`.
- CSRF request header: `X-CSRF-Token`.
- OpenAPI `sessionCookie` must use `type: apiKey`, `in: cookie`, and
  `name: atp_session`.
- Descriptive text may mention that the opaque value starts with `atp_sess_`,
  but must not present that prefix as a cookie name.

### 4.2 Event sequence

- `Event.sequence` is a required positive integer.
- The first emitted sequence is `1`.
- Zero and negative values are invalid.
- Uniqueness remains scoped to one run.
- `(localRunId, sequence)` remains the idempotency key.
- A retry with the same pair must be counted as a duplicate and must not create
  a second stored event.

The OpenAPI schema must retain `minimum: 1`. Change runtime validation to match
it. The validation error should describe the field as a required positive
integer.

This is a protocol constraint, not a database auto-increment requirement. Do
not add ordering assumptions to storage or reject valid gaps in a sequence.

### 4.3 OpenAPI ownership

- Keep the public specification at `backend/deploy/openapi.yaml` unless the
  repository architecture is deliberately migrated in the same approved
  change.
- Every mounted public route must appear exactly once as a path and HTTP method.
- Every declared public operation must be mounted by `cmd/api`.
- `/healthz` is the only allowed public route outside `/v1/`.
- `backend/deploy/routes.txt` remains the flat, review-friendly route inventory.
  It does not replace OpenAPI.
- `frontend/API_CONTRACT.md` remains explanatory prose. It does not override the
  machine-readable contract.

## 5. Required implementation

### 5.1 Correct and audit the OpenAPI document

Review the whole document against the mounted handlers and their request and
response DTOs. At minimum:

- Correct the session cookie name and its description.
- Keep `Event.sequence` at `minimum: 1`.
- Verify required fields, nullable fields, enums, limits, status codes, security
  requirements, path parameters, query parameters, and response bodies.
- Verify all `operationId` values are unique and stable.
- Verify all local `$ref` values resolve.
- Preserve the separation between reporter bearer-token operations and browser
  session/CSRF operations.
- Do not document endpoints or behavior that are planned but not mounted.
- Do not omit mounted behavior merely because the frontend does not use it yet.

Avoid broad wording such as "all errors return X" unless every relevant handler
actually satisfies it. Narrow the prose or fix the implementation when an
assertion is meant to be normative.

### 5.2 Align ingest validation

Update the event conversion or validation boundary so `sequence <= 0` is
rejected. Keep the check close to the request DTO conversion where the other
field-level validation lives.

Do not duplicate this rule in handlers, storage, and services. The request
validation boundary should own malformed input; database uniqueness should
continue to own duplicate protection.

### 5.3 Replace indentation-dependent OpenAPI inspection

Parse the OpenAPI YAML structurally in tests. Do not use a handwritten
line/indentation scanner as the long-term contract parser.

Preferred approach:

- Use an existing maintained YAML or OpenAPI parser if one is already available
  in the repository.
- If no suitable dependency exists, propose the smallest maintained test-only
  dependency and verify its version and OpenAPI 3.1 compatibility before adding
  it.
- Follow repository approval policy before running dependency commands.

The structural route test must normalize the parsed document into the same
`METHOD /path` form returned by the router and compare both directions. Failure
output must list missing and unexpected operations deterministically.

### 5.4 Add focused semantic contract tests

Route parity does not protect shared literals. Add narrow tests for critical
contracts that already have canonical constants in code:

- OpenAPI session cookie name equals `authz.SessionCookie`.
- OpenAPI CSRF header name equals `authz.CSRFHeader`.
- OpenAPI CSRF cookie description or schema does not rename `authz.CSRFCookie`.
- OpenAPI `Event.sequence` lower bound is `1`.

Prefer assertions against parsed nodes over searching raw YAML text. Keep these
tests close to the existing API security/contract tests unless a dedicated
contract-test package creates a clearer ownership boundary.

### 5.5 Add ingest boundary regression coverage

Add table-driven coverage for sequence validation:

| Input | Expected result |
|---|---|
| missing | `400 INVALID_PAYLOAD` for `sequence` |
| `-1` | `400 INVALID_PAYLOAD` for `sequence` |
| `0` | `400 INVALID_PAYLOAD` for `sequence` |
| `1` | accepted when the rest of the event is valid |
| positive gap, for example `42` | accepted |
| repeated positive value in the same run | duplicate, not a second event |

Assert the stable machine-readable error code and field detail. Avoid coupling
the test to incidental full error prose unless that prose is explicitly part of
the public contract.

## 6. Design options and recommendation

### Option A - Minimal correction

Change the two mismatched literals and keep the current line scanner.

Pros:

- Small diff.
- Fast to implement.

Cons:

- Does not prevent semantic contract drift.
- YAML formatting changes can break or bypass route discovery.
- Future reviewers can receive a green test while auth or validation schemas
  are wrong.

### Option B - Production-grade contract gate

Correct the contract, align runtime validation, parse OpenAPI structurally, and
add focused semantic and boundary tests.

Pros:

- Makes OpenAPI meaningfully authoritative.
- Produces actionable failures when code and contract diverge.
- Scales better when clients are generated from the specification.
- Keeps field validation ownership clear.

Cons:

- Slightly larger test change.
- May require one small test dependency.

Recommendation: implement Option B.

## 7. Non-goals

- Do not redesign authentication or session storage.
- Do not rename the real `atp_session` cookie.
- Do not change the `atp_sess_` secret prefix.
- Do not renumber reporter events or change its current first value of `1`.
- Do not require contiguous event sequences.
- Do not add new API endpoints.
- Do not generate or commit SDKs as part of this task.
- Do not remove `routes.txt`.
- Do not perform unrelated DTO or handler refactors.

## 8. Expected file scope

The final change should normally be limited to:

- `backend/deploy/openapi.yaml`
- `backend/cmd/api/security_test.go`, or a narrowly scoped contract test file
- `backend/internal/ingest/payload.go`
- focused ingest validation tests under `backend/internal/ingest/`
- `backend/go.mod` and `backend/go.sum` only if a parser dependency is approved
- `backend/GATES.md` only after the documented gate has actually passed

If implementation needs broader changes, explain the architectural reason
before making them.

## 9. Verification plan

Repository policy requires explicit approval for test, build, dependency, and
file-modifying commands. Ask for approval using the exact command before each
command class.

After approval, run the smallest checks first:

```bash
cd backend && go test ./internal/ingest ./cmd/api
```

Then run the complete backend suite:

```bash
cd backend && go test ./...
```

Run the repository's configured OpenAPI lint command from CI. Do not invent a
different lint configuration. If CI uses Redocly, run the same pinned command
and configuration locally after approval.

Report commands and outcomes exactly. Do not mark `backend/GATES.md` as passing
based only on static inspection.

## 10. Acceptance criteria

The change is ready for review only when all of the following are true:

- OpenAPI declares the session cookie as `atp_session`.
- `atp_sess_` appears only where it clearly describes a secret value prefix.
- OpenAPI declares `Event.sequence` as an integer with `minimum: 1`.
- Runtime ingest rejects missing, negative, and zero sequence values.
- Runtime ingest accepts valid positive sequence values, including gaps.
- Duplicate positive sequences preserve idempotency behavior.
- Router-to-OpenAPI comparison works from parsed YAML structure and checks both
  directions.
- Contract tests bind auth names to the existing Go constants.
- OpenAPI validation/lint passes using the repository's configured tool.
- Focused API and ingest tests pass.
- The complete backend test suite passes, or any unrelated failure is reported
  with evidence and left unclaimed.
- No unrelated source or generated-file churn is included.

## 11. Claude handoff prompt

Use the following prompt when assigning this work:

> Implement `docs/openapi-contract-reimplementation.md` end to end. Begin by
> reading `SPEC.md`, the current router mounts, auth constants, ingest payload
> validation, existing contract tests, and the Playwright event queue. Treat the
> current OpenAPI file and route test as a draft. Follow every contract decision,
> keep the change within the expected file scope, and use a structural parser
> rather than an indentation scanner. Respect the repository terminal approval
> policy before dependency, formatting, test, or build commands. Do not update
> gate claims until the corresponding commands have actually passed. Finish
> with a concise list of changed files, verification evidence, and any remaining
> contract uncertainty.
