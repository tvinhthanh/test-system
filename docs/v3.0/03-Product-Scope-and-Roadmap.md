# 03 — Product Scope and Roadmap

## Scope Philosophy

The long-term product vision is large, but development must be validation-driven.

Each phase has a validation gate.

A later phase should only start if the previous phase demonstrates real user value.

## Scope Definitions

### MVP

MVP = Phase 0 + Phase 1 only.

### V1

V1 may include Phase 2 and Phase 3 depending on validation.

### Long-term Platform

Long-term platform includes Phase 4+ capabilities such as AI expansion, visual regression, performance, security and plugin marketplace.

## Phase 0 — Discovery and Design Partners

### Goal

Validate the ICP, pain and onboarding path before building the core system.

### Scope

- Recruit 3–5 design partners
- Interview QA Automation Leads
- Collect sample Playwright/Cypress reports
- Understand current debugging workflow
- Define MVP success metric
- Validate pricing willingness
- Decide reporter-first vs agent-first onboarding

### Deliverables

- ICP validation notes
- Design partner list
- Current workflow map
- Failure triage pain map
- Phase 1 success criteria

### Exit Criteria

- At least 3 teams agree to test the product.
- They have real E2E test suites.
- They confirm failure debugging is painful enough to try a new tool.

## Phase 1 — E2E Report Center + Runner Baseline

### Goal

Deliver the first usable product.

### Scope

- Project creation
- Environment creation
- Reporter SDK or agent ingestion
- Basic Playwright integration
- Run history
- Unified report center v0
- Screenshot/video/trace upload
- Safety policy v0
- Artifact retention baseline
- Minimal RBAC

### Key Decision

Reporter SDK should be considered as the lowest-friction adoption path.

Agent is still important, but may become an upgrade path for scheduling and controlled execution.

### Deliverables

- First real report from an existing Playwright project
- Artifact viewer
- Basic flaky marker
- Environment metadata
- Toolchain version capture
- Production guard v0

### Exit Criteria

- Design partners use the report center weekly.
- Users can understand failures faster than their current workflow.
- Reports contain enough artifacts to support AI analysis later.

## Phase 2 — Test Management and Migration

### Goal

Bridge manual testing and automation workflows.

### Scope

- Test Plan
- Test Suite
- Test Case
- Manual execution
- Evidence capture
- Automation status
- Requirement mapping v0
- Excel/TestRail/Xray import
- JUnit history backfill

### Why Phase 2

QA teams need a system of record.

Without import, adoption is too expensive.

### Exit Criteria

- Users can import existing test cases.
- Manual testers can execute test cases.
- Automation results can be linked to test cases.

## Phase 3 — Scheduler, CI Ingestion and Notifications

### Goal

Integrate into real team workflow.

### Scope

- CI webhook receiver
- GitHub/GitLab/Jenkins result ingestion
- Scheduler
- Notification rules
- Slack/Telegram/Email notification
- Branch and commit mapping
- Run comparison

### Exit Criteria

- Runs are triggered automatically after deployment.
- QA receives failure notification with actionable report link.
- CI results and platform reports stay linked.

## Phase 4 — AI Failure Analysis

### Goal

Reduce time-to-root-cause with AI.

### Scope

- Failure summarization
- Root-cause hypothesis
- Selector repair suggestion
- API failure explanation
- Flaky test explanation
- Human approval workflow for suggested fixes

### Guardrails

- AI must not silently modify tests.
- AI must preserve the original failure.
- AI output must show confidence and evidence.

### Exit Criteria

- AI suggestions are useful in real failures.
- Users report faster triage.
- False-positive AI suggestions are tracked.

## Phase 5 — Advanced Quality Features

Possible modules:

- Visual regression
- Performance lite
- Security scan lite
- API test builder
- Advanced flaky detection
- Distributed runners

Only build if customer demand exists.

## Phase 6 — Enterprise and Ecosystem

Possible modules:

- SSO/SAML
- Advanced RBAC
- Audit log
- Plugin SDK
- Marketplace
- On-premise deployment
- Compliance controls

## Roadmap Summary

```text
Phase 0: Validate buyer and pain
Phase 1: E2E report center + runner baseline
Phase 2: Test management + migration
Phase 3: Scheduler + CI ingestion + notifications
Phase 4: AI failure analysis
Phase 5: Advanced quality modules
Phase 6: Enterprise and ecosystem
```

## Scope Control Rule

Do not build a Phase N+1 module until Phase N has real usage and clear demand.

## Product Strategy

Start with:

> Best Playwright/E2E failure report center.

Then expand toward:

> Quality Engineering Platform.