# 07 — Validation Gates and Next Steps

## Why Validation Gates Matter

The product vision is intentionally large, but execution must be controlled.

Validation gates prevent the project from becoming a large platform with no users.

Each gate answers:

> Did the previous phase create enough real value to justify building the next phase?

## Primary Metric

Median time from failed run to root-cause hypothesis.

This metric should be measured with real users and real failures.

## Phase 0 Validation Gate

### Question

Do QA automation teams actually feel enough pain to try this product?

### Required Evidence

- 3–5 design partners recruited
- Current debugging workflow documented
- Existing tools identified
- Sample failure artifacts collected
- Willingness to test prototype confirmed

### Pass Criteria

At least 3 design partners agree to use the Phase 1 product with real projects.

## Phase 1 Validation Gate

### Question

Does the report center reduce debugging time?

### Required Evidence

- Real Playwright/Cypress results ingested or executed
- Users open report after failures
- Artifacts are useful
- AI-ready data exists
- Users prefer report center over existing CI logs

### Pass Criteria

- Design partners use it weekly.
- Median time-to-root-cause improves.
- At least 2 teams ask to continue using it.

## Phase 2 Validation Gate

### Question

Do QA teams want this to become their test management system of record?

### Required Evidence

- Test cases imported
- Manual executions performed
- Automation status linked
- Requirement mapping used

### Pass Criteria

- At least one design partner imports real test cases.
- Manual testers execute cases inside the platform.
- QA lead uses the dashboard for release discussion.

## Phase 3 Validation Gate

### Question

Does automation fit into existing delivery workflow?

### Required Evidence

- CI webhook works
- Scheduled run works
- Notification useful
- Branch/commit mapping useful

### Pass Criteria

- Failures are detected automatically after deploy.
- Notifications lead users to actionable reports.
- Users stop manually searching CI artifacts.

## Phase 4 Validation Gate

### Question

Does AI failure analysis produce useful triage?

### Required Evidence

- AI analysis generated from real failures
- Suggested causes reviewed by QA/devs
- False positives tracked
- Human approval workflow tested

### Pass Criteria

- AI suggestions are considered useful in real debugging.
- Median time-to-root-cause improves further.
- Users trust AI as assistant, not as silent auto-healer.

## Adoption Ladder

### Level 1 — Reporter-only

User installs reporter SDK.

```text
Existing Playwright/Cypress
↓
Reporter SDK
↓
Report Center
```

Best for low-friction onboarding.

### Level 2 — Agent Execution

User installs local/server agent.

```text
Platform
↓
Agent
↓
Run tests
↓
Report Center
```

Best for controlled execution, localhost and scheduling.

### Level 3 — Test Management

User manages:

- Test Plan
- Test Suite
- Test Case
- Manual execution
- Automation mapping

### Level 4 — AI Triage

User gets:

- Failure summary
- Likely root cause
- Suggested fix
- Flaky explanation

### Level 5 — Enterprise

User adds:

- RBAC
- SSO
- Audit
- Data retention
- Compliance
- On-premise deployment

## Immediate Next Technical Artifacts

After this document set, build two deeper specs:

### 1. Agent Protocol Specification

Should include:

- Heartbeat
- Job lifecycle
- Protocol versioning
- Idempotency
- Sequence numbers
- Chunked artifact upload
- Timeout
- Crash recovery
- Managed/local agent security

### 2. Canonical Report Schema

Should include:

- Stable test identity
- Run schema
- Result schema
- Retry attempt schema
- Artifact schema
- Flaky schema
- AI analysis schema
- Retention/redaction policy

## First Build Recommendation

Do not start with the full platform.

Start with:

1. Report ingestion API
2. Playwright reporter SDK
3. Minimal report dashboard
4. Artifact upload
5. Failure details page

Then add agent execution.

## Development Mindset

Build the smallest product that proves:

> Users can understand E2E failures faster with this platform than with their current CI/reporting workflow.

Everything else is secondary until this is true.