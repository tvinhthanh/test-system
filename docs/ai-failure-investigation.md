# AI Failure Investigation — implementation design

Status: **Proposed**  
Scope: QĐ-4 / P1.5  
Date: 11-11-2026

## 1. Decision

Implement failure investigation inside `backend/internal/triage`. That module
already owns `TriageEvent` and `AiHypothesis`, and SPEC §6.9.5 assigns AI
triage to it. A second `investigation` domain would split ownership of one
workflow across two modules.

The first release is a bounded Go state machine. The model may request one of
a small set of read-only evidence tools or finish with one structured
hypothesis. It cannot execute test code, fetch an arbitrary URL, change a test,
rerun a job, create an issue, edit source, or open a pull request.

This extends the QĐ-4 v0 contract. It does not change the existing self-healing
rule: self-healing may repair a missing locator, but never weakens an
assertion to make a test pass.

## 2. User flow

1. A member opens a failed result and selects **Investigate with AI**.
2. The API authorizes the project and queues one investigation.
3. The worker collects the failed attempts and stable-test history, then lets
   the model request more evidence within the fixed tool and turn budgets.
4. The result page polls until the investigation is `done` or `error`.
5. A completed investigation shows one category, confidence, evidence list,
   and suggested action.
6. A member accepts or rejects it. Acceptance records
   `HYPOTHESIS_ACCEPTED`; rejection records `HYPOTHESIS_REJECTED`. The existing
   triage event remains the only source of truth for TTRC and acceptance
   metrics.

Starting another investigation for the same failure fingerprint on the same
UTC day returns the existing row. A user can explicitly request a new one only
after the prior row ended in `error`; it still counts against the daily limit.

## 3. State machine

```text
queued -> collecting -> investigating -> done
   |          |               |
   +----------+---------------+-> error
```

The queue job key is the investigation id. A handler locks the row and exits
when it is already terminal, so delivery is idempotent.

Each model response must match one of two strict JSON shapes:

```json
{"action":"call_tool","tool":"stable_history","arguments":{}}
```

```json
{
  "action":"finish",
  "hypothesis": {
    "category":"flaky_timing",
    "confidence":0.78,
    "evidence":[
      {"source":"attempt:0","fact":"Timed out waiting for the checkout button"},
      {"source":"history","fact":"The same identity passed 18 of its last 20 runs"}
    ],
    "suggestedAction":"Inspect the button readiness condition and replace the fixed wait."
  }
}
```

The server validates category, confidence, evidence count and source ids. A
model cannot invent a source: every evidence item must refer to an observation
already returned by a tool. The UI shows facts and sources, never hidden model
reasoning or raw prompts.

### Hard budgets

| Budget | MVP limit |
|---|---:|
| Model turns | 4 |
| Tool calls | 6 |
| Wall time | 3 minutes |
| Combined observation JSON | 64 KiB |
| Evidence items in final answer | 10 |
| Suggested action | 1,000 bytes |
| Investigations per organization/day | Team 200, Free 20 |

Provider retries remain inside `platform/llm`. A queue retry resumes from the
last committed step and does not repeat a completed tool call or create a
second hypothesis.

## 4. Evidence tools

All tools are server functions bound to the authorized `projectId` and
`testResultId`. The model supplies no project id, SQL, path, host, or URL.

| Tool | Input | Output | MVP |
|---|---|---|---|
| `failure_context` | none | result, run metadata, attempts, normalized error and stack excerpts, artifact metadata | Always run first |
| `stable_history` | optional `limit` 1–20 | recent results for the same stable identity, status, duration, branch, commit, environment | Yes |
| `compare_last_pass` | none | field-level comparison with the nearest prior pass: error signature, duration, branch/commit and attempt count | Yes |
| `triage_history` | optional `limit` 1–20 | prior accepted/rejected categories for the same stable identity | Yes |
| `text_artifact` | an artifact id already returned by `failure_context` | redacted UTF-8 log/attachment excerpt, at most 16 KiB | Yes |
| `github_change_context` | repository connection and bounded commit range | changed files and selected diff excerpts | P3, disabled in MVP |

MVP does not send screenshot, video, trace, HAR bodies, secrets, cookies,
authorization headers, environment variables, or arbitrary attachments to a
provider. Vision support needs a separate privacy and retention decision.

`text_artifact` accepts only verified `log` or text `attachment` artifacts
owned by the same project and result attempt. It strips common token, cookie,
authorization and password patterns before the content enters a prompt.

## 5. Module contracts

`triage` owns orchestration and writes only its own tables. It declares the
interfaces it consumes:

```go
type EvidenceReader interface {
    FailureContext(ctx context.Context, projectID, resultID string) (FailureContext, error)
    StableHistory(ctx context.Context, projectID, stableTestID string, limit int) ([]HistoryItem, error)
    CompareLastPass(ctx context.Context, projectID, resultID string) (Comparison, error)
    TriageHistory(ctx context.Context, projectID, stableTestID string, limit int) ([]Decision, error)
    TextArtifact(ctx context.Context, projectID, resultID, artifactID string, maxBytes int64) (TextEvidence, error)
}

type CredentialSource interface {
    Credentials(ctx context.Context, orgID string) (llm.Account, string, bool, error)
}

type InvestigatorLLM interface {
    JSON(ctx context.Context, req llm.Request) (llm.Result, error)
}
```

The read implementation belongs to `query` because it needs cross-domain
joins. Artifact bytes are read through the `artifact` service and object store;
the adapter that combines the two lives in `cmd/api` / `cmd/worker`, not in
`query`. `aisuggest.Service.Credentials` supplies the organization or server
key without exposing sealed key storage to `triage`.

The public `triage.Service` gains:

```go
RequestInvestigation(ctx context.Context, in NewInvestigation) (Investigation, bool, error)
GetInvestigation(ctx context.Context, projectID, resultID, id string) (Investigation, error)
ListInvestigations(ctx context.Context, projectID, resultID string, limit int, cursor string) (Page[Investigation], error)
DecideHypothesis(ctx context.Context, in Decision) (Event, error)
InvestigationWorker(concurrency int) *queue.Worker
```

## 6. Database schema

Migration `0051_ai_investigations.sql` remains owned by `triage`.

```sql
CREATE TABLE ai_investigations (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id      uuid NOT NULL,
    org_id          uuid NOT NULL,
    test_result_id  uuid NOT NULL,
    stable_test_id  uuid,
    dedupe_key      text NOT NULL CHECK (length(dedupe_key) = 64),
    requested_day   date NOT NULL,
    status          text NOT NULL DEFAULT 'queued'
                    CHECK (status IN ('queued','collecting','investigating','done','error')),
    requested_by    text NOT NULL CHECK (length(requested_by) BETWEEN 1 AND 254),
    provider        text NOT NULL DEFAULT '',
    model           text NOT NULL DEFAULT '',
    key_source      text NOT NULL DEFAULT '',
    prompt_version  text NOT NULL DEFAULT 'investigation-v1',
    turn_count      integer NOT NULL DEFAULT 0 CHECK (turn_count BETWEEN 0 AND 4),
    tool_count      integer NOT NULL DEFAULT 0 CHECK (tool_count BETWEEN 0 AND 6),
    input_tokens    integer NOT NULL DEFAULT 0 CHECK (input_tokens >= 0),
    output_tokens   integer NOT NULL DEFAULT 0 CHECK (output_tokens >= 0),
    hypothesis_id   uuid,
    error           text NOT NULL DEFAULT '' CHECK (octet_length(error) <= 1000),
    created_at      timestamptz NOT NULL DEFAULT now(),
    started_at      timestamptz,
    finished_at     timestamptz,
    UNIQUE (project_id, dedupe_key, requested_day)
);

CREATE INDEX ai_investigations_result
    ON ai_investigations (project_id, test_result_id, created_at DESC);
CREATE INDEX ai_investigations_org_day
    ON ai_investigations (org_id, requested_day);

CREATE TABLE ai_investigation_steps (
    investigation_id uuid NOT NULL,
    sequence         integer NOT NULL CHECK (sequence BETWEEN 1 AND 12),
    kind             text NOT NULL CHECK (kind IN ('tool','model')),
    tool             text NOT NULL DEFAULT '',
    request          jsonb NOT NULL DEFAULT '{}'::jsonb,
    observation      jsonb NOT NULL DEFAULT '{}'::jsonb,
    input_tokens     integer NOT NULL DEFAULT 0 CHECK (input_tokens >= 0),
    output_tokens    integer NOT NULL DEFAULT 0 CHECK (output_tokens >= 0),
    created_at       timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (investigation_id, sequence)
);
```

`ai_hypotheses` gains nullable `investigation_id` with a unique index. The
final hypothesis is inserted in the same transaction that marks the
investigation `done` and appends `HYPOTHESIS_PROPOSED`. There are no
cross-module foreign keys, consistent with §6.9.5.

`request` stores validated tool arguments or model action metadata.
`observation` stores bounded, redacted facts. It never stores API keys, raw
prompts, hidden reasoning, binary artifacts, or an unbounded stack/log.

Purge order is steps → investigations/hypotheses → triage events. Existing
project and run retention paths must delete these rows by project/result.

## 7. HTTP API

All routes use the existing project scope and return `404` for a result outside
the project.

### Start or reuse

`POST /v1/projects/{projectId}/results/{resultId}/investigations`

- Permission: `triage`.
- Body: `{}` for MVP.
- `202`: new queued investigation.
- `200`: the same-day investigation for this dedupe key already exists.
- Errors: `AI_NOT_CONFIGURED`, `AI_DAILY_LIMIT`, `RESULT_NOT_FAILED`,
  `INVESTIGATION_BUSY`.

### Read

`GET /v1/projects/{projectId}/results/{resultId}/investigations?limit=20&cursor=…`

- Permission: `view`.
- Cursor pagination, newest first.

`GET /v1/projects/{projectId}/results/{resultId}/investigations/{investigationId}`

- Permission: `view`.
- Returns status, public tool activity labels, token counts, error and the final
  hypothesis. It excludes prompts and internal step observations.

### Decide

`POST /v1/projects/{projectId}/results/{resultId}/hypotheses/{hypothesisId}/decision`

- Permission: `triage`.
- Body: `{"decision":"accept|reject","category":"app_bug","note":"..."}`.
- `category` is optional on reject. On accept it defaults to the hypothesis
  category; an override must still be a §8.7 category and is recorded in the
  event payload.
- A hypothesis can receive one terminal decision. Repeating the same request
  is idempotent; the opposite decision returns `409 HYPOTHESIS_DECIDED`.

Public DTO:

```json
{
  "id":"…",
  "status":"done",
  "requestedBy":"member@example.com",
  "createdAt":"…",
  "finishedAt":"…",
  "usage":{"turns":2,"tools":3,"inputTokens":5200,"outputTokens":310},
  "hypothesis":{
    "id":"…",
    "category":"flaky_timing",
    "confidence":0.78,
    "lowConfidence":false,
    "evidence":[{"source":"history","fact":"…"}],
    "suggestedAction":"…",
    "decision":null
  }
}
```

## 8. Frontend

Add an **AI investigation** card to `ResultDetailPage.vue` below failure
attempts and above the triage timeline.

- Show the button only for failed/timed-out/interrupted results when AI access
  is available and the user has `triage` permission.
- Keep prior investigations readable when the provider is disabled.
- Poll queued/running states with the same bounded polling pattern used by
  Script Runs.
- Show category, confidence, source-backed evidence and suggested action.
- Accept and reject require a deliberate click. Accept permits a category
  override; reject permits a short reason.
- Errors name the actionable cause: missing key, quota, model access, provider
  outage, daily limit or insufficient evidence.
- Never render model HTML. Evidence and actions are plain text.

Fixture coverage must include queued → done, low confidence, provider error,
daily limit, no stable identity, accept, reject, and a viewer without triage
permission.

## 9. Security, privacy and operations

- Project authorization occurs before lookup; every SQL read includes
  `project_id`.
- The worker revalidates project/result/org ownership instead of trusting job
  payload ids.
- Model and tools receive only allowlisted fields. Redaction runs before size
  truncation and before persistence.
- Tool names and arguments are schema-validated; unknown tools end the job as
  an error and are not executed.
- The model cannot control a network destination. Provider hosts remain fixed
  by `platform/llm` and the operator configuration.
- Daily limits use a project/org advisory transaction lock, matching the
  existing AI suggestion pattern.
- Metrics: request count, terminal status, latency, tool calls, token usage,
  provider errors, acceptance rate, decision rate and coverage. Labels never
  contain project, result, model output or error text.
- Logs contain investigation id, project id, provider and state transition;
  they exclude evidence bodies, prompts and credentials.

## 10. Evaluation gate

Before enabling by default, create 20–30 redacted cases with a human-labelled
category and evidence set. Measure:

- category accuracy and macro F1;
- evidence support rate: every claimed fact maps to a supplied source;
- unsupported-claim rate;
- tool success and unnecessary-call rate;
- p50/p95 latency, input/output tokens and estimated cost;
- human acceptance and decision rates after release.

Do not claim quality with fewer than 30 human decisions in a rolling 14-day
window. Apply the QĐ-4 kill switch when acceptance is below 40% and decision
rate is at least 60% for two consecutive windows.

## 11. Delivery slices

1. **Contract and persistence:** migration, `triage` types/service, purge,
   OpenAPI and tenant/architecture tests.
2. **Deterministic collector:** `failure_context`, history/comparison tools,
   redaction and same-day dedupe; no LLM yet.
3. **Bounded agent worker:** strict action schemas, budgets, retries, terminal
   hypothesis/event transaction and provider error mapping.
4. **API and UI:** request/read/decision routes and Result Detail card with
   polling and permission states.
5. **Evaluation:** labelled fixture set, offline evaluator, cost/latency report
   and release decision.
6. **GitHub context (later):** repository connection, commit-range guard,
   read-only diff tool and separate permission/privacy review.

Each slice is independently testable and leaves the existing manual triage
flow working.
