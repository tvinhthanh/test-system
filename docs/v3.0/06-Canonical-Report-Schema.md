# 06 — Canonical Report Schema

## Purpose

The Canonical Report Schema normalizes results from multiple sources:

- Playwright
- Cypress
- Jest
- Vitest
- Pytest
- Appium
- Manual execution
- CI ingestion
- Imported JUnit XML
- Historical migration

The schema must support reporting, trends, flaky detection, AI analysis and future test management.

## Core Entities

```text
Organization
Workspace
Project
Environment
Run
TestResult
TestAttempt
TestStepResult
Artifact
LogEvent
TestIdentity
FlakySignal
AIAnalysis
```

## Run

Represents a logical execution.

Fields:

- id
- projectId
- environmentId
- triggerType
- source
- branch
- commitSha
- status
- startedAt
- finishedAt
- durationMs
- toolchain
- summary

Example status:

```text
queued
running
passed
failed
flaky_pass
cancelled
timed_out
```

Important:

A run with flaky passes should not appear as clean green.

## Test Identity

Stable test identity is critical.

Frameworks often identify tests by file path and title, but both can change.

The platform needs a strategy.

Possible fields:

- stableId
- framework
- filePath
- titlePath
- normalizedFingerprint
- historicalAliases
- manualMergeGroupId

## Identity Strategy

Use multiple signals:

1. Explicit platform ID if present.
2. File path + title path.
3. Normalized title fingerprint.
4. Step similarity.
5. Historical rename detection.
6. Manual merge tool.

## TestResult

Represents the final result of one test in one run.

Fields:

- id
- runId
- stableTestId
- displayName
- suiteName
- status
- durationMs
- retryCount
- isFlaky
- errorMessage
- failureCategory
- owner
- tags

## TestAttempt

Each retry is a separate attempt.

Fields:

- id
- testResultId
- attemptIndex
- status
- startedAt
- finishedAt
- durationMs
- workerId
- agentId
- errorMessage

Rule:

If attempt 1 fails and retry passes, the final result is flaky_pass, not clean pass.

## TestStepResult

Fields:

- id
- attemptId
- stepIndex
- title
- action
- status
- startedAt
- finishedAt
- durationMs
- errorMessage

## Artifact

Types:

- screenshot
- video
- trace
- har
- console_log
- network_log
- attachment
- manual_evidence

Fields:

- id
- runId
- testResultId
- attemptId
- type
- fileName
- storageKey
- sizeBytes
- contentType
- retentionPolicy
- accessLevel
- createdAt

## LogEvent

Fields:

- id
- runId
- testResultId
- level
- source
- message
- timestamp
- redacted
- metadata

Sources:

- browser_console
- network
- runner
- framework
- agent
- system

## Toolchain Metadata

Every run should record actual resolved versions.

Examples:

- nodeVersion
- npmVersion
- pnpmVersion
- playwrightVersion
- browserVersion
- pythonVersion
- pytestVersion
- os
- containerImage
- agentVersion

This helps diagnose “works locally, fails on platform” issues.

## Flaky Detection

Signals:

- retry pass after failure
- alternating pass/fail history
- failure only in specific browser
- failure only in specific environment
- timeout variability
- network-dependent failures

Flaky fields:

- flakyScore
- flakyReason
- firstDetectedAt
- lastDetectedAt
- quarantineStatus

## AI Analysis

AI analysis should attach to:

- run
- test result
- attempt
- artifact group

Fields:

- id
- targetType
- targetId
- summary
- likelyCause
- evidence
- suggestedAction
- confidence
- createdAt
- modelVersion

## Redaction and Privacy

MVP should support:

- Header redaction
- Cookie redaction
- Token-like string masking
- HAR redaction
- Log redaction

MVP should not promise reliable screenshot/video PII redaction.

Visual artifacts should be treated as sensitive by default.

## Retention

Default MVP retention suggestion:

- Logs: 30 days
- Screenshots: 14–30 days
- Videos: 7–14 days
- Traces: 14–30 days
- HAR: 7–14 days due to sensitivity
- Summary metadata: long-term

## Query Requirements

The schema must support:

- Run history by project/environment
- Failed tests by branch/commit
- Flaky tests over time
- Tests newly failing since last run
- Artifact retrieval by result
- Failure category trends
- Time-to-root-cause measurement

## Future Extensions

- Requirement mapping
- Release sign-off
- Defect linking
- Manual execution
- Coverage mapping
- Distributed run shard merge
- Parameterized tests

## Important Design Note

The report center is not just a UI.

It is a normalized data model and query layer.

This is the foundation for:

- Trend analysis
- Flaky detection
- AI analysis
- Compliance
- Test management