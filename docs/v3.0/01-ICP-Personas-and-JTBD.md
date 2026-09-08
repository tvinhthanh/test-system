# 01 — ICP, Personas and Jobs To Be Done

## Primary ICP

### QA Automation Lead

The first ideal customer is a QA Automation Lead or QA team responsible for maintaining E2E test suites, usually built with Playwright, Cypress or similar frameworks.

## Why This ICP

This user feels the pain most directly:

- E2E tests fail often.
- Reports are fragmented.
- Screenshots, videos, traces and logs are scattered.
- Debugging failures takes too long.
- Flaky tests reduce trust in automation.
- Management wants release confidence, but existing reports are hard to interpret.

## Primary Buyer Pain

> “Our E2E automation exists, but when it fails, it takes too long to understand whether it is a real bug, a flaky test, an environment issue or a test code issue.”

## Secondary Personas

### QA Manual Tester

Needs:

- Test plans
- Manual execution UI
- Evidence capture
- Step-by-step test execution
- Ability to convert manual cases into automation candidates

### Developer

Needs:

- Fast debugging
- Artifacts linked to commit/branch
- Clear network/console errors
- Selector and API failure hints

Developers are not expected to use the dashboard for unit test execution in MVP. They are more likely to benefit from result ingestion and debugging reports.

### QA Manager / Team Lead

Needs:

- Pass/fail trends
- Flaky test visibility
- Release confidence
- Coverage visibility
- Team progress

### Product Owner / Business Analyst

Needs:

- High-level release status
- Requirement to test mapping
- Sign-off visibility
- Defect status

They are not primary daily users in the MVP.

## Non-primary Users for MVP

The MVP is not optimized for:

- Solo developers
- Students learning Playwright
- Teams without any QA/testing process
- Enterprise compliance-heavy organizations requiring full SSO/RBAC from day one

These may be addressed later.

## Jobs To Be Done

### JTBD 1 — Failed Run Triage

When a test run fails after deployment,  
I want to quickly understand what failed and why,  
so that I can decide whether to rollback, fix the app, or fix the test.

### JTBD 2 — Artifact Consolidation

When an E2E test fails,  
I want screenshots, videos, traces, console logs and network errors in one report,  
so that I do not waste time jumping between CI logs, local files and browser devtools.

### JTBD 3 — Flaky Test Identification

When a test sometimes fails and sometimes passes,  
I want the platform to detect and label it as flaky,  
so that my team does not treat it as a clean pass.

### JTBD 4 — Environment Comparison

When the same test passes locally but fails on staging,  
I want to compare environment, browser, commit and toolchain versions,  
so that I can identify environment-specific issues faster.

### JTBD 5 — Safe Production Smoke Testing

When I run smoke tests on production,  
I want destructive tests and payment tests to be blocked by policy,  
so that I do not damage real data or trigger real transactions.

### JTBD 6 — Release Confidence

When preparing a release,  
I want to see which key flows passed, failed or were skipped,  
so that I can make a release decision with evidence.

### JTBD 7 — Manual to Automation Bridge

When manual testers execute repeated test cases,  
I want to identify which cases should become automation candidates,  
so that the automation team can prioritize high-value flows.

### JTBD 8 — AI-assisted Debugging

When a failure contains lots of logs and artifacts,  
I want AI to summarize the likely cause and suggested next action,  
so that I can start debugging from a useful hypothesis.

## MVP User Journey

```text
User signs up
↓
Creates project
↓
Connects existing Playwright project via reporter SDK or agent
↓
Runs or ingests first test run
↓
Sees report with artifacts
↓
AI summarizes failure cause
↓
User decides whether the tool saves debugging time
```

## Design Partner Validation

Before Phase 1 build, recruit 3–5 design partner teams.

Criteria:

- They already use Playwright/Cypress or have E2E automation.
- They experience flaky tests or difficult debugging.
- They are willing to share feedback weekly.
- They can provide sample reports/artifacts.

## Phase 1 Exit Criteria

Phase 1 succeeds only if:

- At least 3 design partners use the report center weekly.
- They can import/ingest or execute real E2E tests.
- They confirm measurable debugging time reduction.
- They request continued use after the initial evaluation.