# 00 — Vision and Product Principles

## Product Name

Automation Testing Platform

## One-line Pitch

A local-agent based E2E test orchestration and unified reporting platform that helps QA automation teams debug test failures faster using rich artifacts, flaky detection, and AI-assisted failure analysis.

## Long-term Vision

Build a Unified Quality Engineering Platform over multiple years.

The long-term product may include:

- E2E orchestration
- Unified Report Center
- Manual Test Management
- AI Failure Analysis
- Scheduler and CI/CD ingestion
- Test artifact governance
- Plugin ecosystem
- Enterprise RBAC, SSO, audit and compliance

However, the product must not be built as one giant platform from day one.

## Execution Principle

Vision is large. Execution must be narrow.

Every phase must:

1. Deliver a working product increment.
2. Be validated with real users.
3. Prove measurable value before expanding scope.
4. Avoid building modules only because they exist in the long-term vision.

## Core Product Loop

```text
Run
↓
Collect
↓
Normalize
↓
Analyze
↓
Recommend
```

## Core Value

The product should reduce the time it takes for QA and developers to understand why an E2E test failed.

The product is not primarily selling automation execution. Playwright already executes tests well.

The product is selling:

- Unified visibility
- Faster debugging
- Rich artifacts
- Flaky detection
- AI-assisted root-cause hypothesis
- Safer test execution across environments

## MVP Definition

MVP must be limited to Phase 0 + Phase 1.

MVP includes:

- Design partner validation
- Playwright E2E result ingestion or execution
- Local/server runner agent baseline
- Unified Report Center v0
- Screenshot/video/trace artifact collection
- Safety policy v0
- Artifact retention baseline
- Minimal user/project access control

MVP does not include:

- Full Test Management
- Full no-code test builder
- Plugin marketplace
- Performance testing
- Security scanning
- Visual regression
- Enterprise SSO
- Full AI automation generation

Those belong to later phases only if the previous phase is validated.

## Product Principles

### 1. Report Center First

Runner execution is important, but the Report Center is the core differentiator.

The platform must normalize execution results, artifacts and historical trends into one readable system.

### 2. Agent Pull Only

The runner agent must initiate outbound connections to the server.

The server must never require inbound access to the user’s machine.

This supports:

- Localhost testing
- NAT/firewall-friendly deployment
- Better security posture
- Self-hosted runner model

### 3. Frameworks Are Engines, Not the Product

Playwright, Jest, Pytest, Cypress, Appium and K6 are execution engines.

The platform orchestrates and reports on them.

It should not attempt to replace them.

### 4. AI Suggests, Humans Approve

AI may propose:

- Root cause
- Selector repair
- Suggested fix
- Test case improvements

AI must not silently auto-heal tests in a way that hides real regressions.

### 5. Safety by Default

Production environments must be protected by policy.

Potentially destructive tests must be blocked, restricted or require approval.

### 6. Adoption Must Be Low-friction

Teams should be able to see value quickly.

Possible onboarding paths:

- Use reporter SDK with existing Playwright project.
- Use local agent to execute tests.
- Use UI onboarding to generate starter smoke tests.

## Success Metric

The primary MVP metric is:

> Median time from failed run to root-cause hypothesis.

Example target:

- Before platform: 30 minutes
- After platform: under 10 minutes

## Non-goals for MVP

The MVP should not attempt to become:

- A complete TestRail replacement
- A complete Postman replacement
- A complete Jenkins replacement
- A complete K6 Cloud replacement
- A complete no-code automation suite
- A universal enterprise QA platform

Those may be long-term expansion paths only after validation.