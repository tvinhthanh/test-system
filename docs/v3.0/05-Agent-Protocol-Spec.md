# 05 — Agent Protocol Specification

## Purpose

The agent protocol defines communication between the platform server and runner agents.

The protocol must support:

- Local agents
- Managed agents
- Long-running test jobs
- Result streaming
- Artifact upload
- Heartbeat
- Failure recovery
- Future distributed execution

## Protocol Principles

### 1. Pull-only Communication

The agent initiates all communication.

The server never calls into the agent.

### 2. Versioned Protocol

Every message must include protocol version.

Example:

```json
{
  "protocolVersion": "1.0",
  "agentId": "agent_123",
  "messageType": "heartbeat"
}
```

### 3. Idempotency

All result and artifact events must be idempotent.

If the agent retries a message, the server must not double-count.

### 4. Sequence Numbers

Job events should include monotonically increasing sequence numbers.

Example:

```json
{
  "jobId": "job_123",
  "sequence": 12,
  "eventType": "test.finished"
}
```

### 5. Resumable Artifact Upload

Large artifacts such as video and trace files must support chunked upload.

Single POST upload is risky for unstable networks.

## Agent Lifecycle

```text
Start
↓
Register
↓
Authenticate
↓
Heartbeat
↓
Poll for job
↓
Execute job
↓
Stream events
↓
Upload artifacts
↓
Finalize job
```

## Agent Registration

Agent sends:

```json
{
  "protocolVersion": "1.0",
  "agentName": "local-agent-vinh",
  "agentType": "local",
  "capabilities": ["playwright", "chromium"],
  "os": "windows",
  "arch": "x64"
}
```

Server returns:

```json
{
  "agentId": "agent_123",
  "status": "registered"
}
```

## Authentication

Agent authenticates using token.

Token should be scoped to:

- Organization
- Project
- Environment
- Allowed capabilities

Tokens must be revocable.

## Heartbeat

Agent sends heartbeat every N seconds.

```json
{
  "protocolVersion": "1.0",
  "agentId": "agent_123",
  "messageType": "heartbeat",
  "timestamp": "2026-07-07T10:00:00Z",
  "status": "idle",
  "currentJobId": null
}
```

Server uses heartbeat to determine:

- Online
- Busy
- Offline
- Stale
- Crashed

## Job Polling

Agent requests next job:

```json
{
  "agentId": "agent_123",
  "capabilities": ["playwright"],
  "available": true
}
```

Server returns either:

```json
{
  "job": null
}
```

or:

```json
{
  "job": {
    "jobId": "job_123",
    "runId": "run_123",
    "projectId": "project_123",
    "environment": {
      "baseUrl": "https://staging.example.com"
    },
    "execution": {
      "framework": "playwright",
      "browser": "chromium",
      "timeoutSeconds": 1800
    }
  }
}
```

## Job States

```text
queued
claimed
running
cancel_requested
completed
failed
timed_out
abandoned
```

## Result Events

Examples:

- run.started
- test.started
- test.step.started
- test.step.finished
- test.finished
- artifact.created
- run.finished

Event example:

```json
{
  "idempotencyKey": "job_123_seq_21",
  "jobId": "job_123",
  "runId": "run_123",
  "sequence": 21,
  "eventType": "test.finished",
  "payload": {
    "testId": "test_abc",
    "status": "failed",
    "durationMs": 2450,
    "errorMessage": "Expected text not found"
  }
}
```

## Artifact Upload

Artifact metadata:

```json
{
  "artifactId": "artifact_123",
  "runId": "run_123",
  "testResultId": "result_123",
  "type": "video",
  "fileName": "login-failure.webm",
  "sizeBytes": 419430400,
  "contentType": "video/webm"
}
```

Chunk upload must include:

- artifactId
- chunkIndex
- totalChunks
- checksum
- idempotencyKey

## Job Timeout

Each job must have:

- hard timeout
- soft heartbeat timeout
- cancellation timeout

If an agent disappears:

```text
running job
↓
heartbeat missing
↓
mark job as possibly abandoned
↓
wait grace period
↓
mark failed/abandoned
↓
release resources
```

## Crash Recovery

If agent restarts, it should report previous job state if possible.

Server must be able to mark unfinished jobs as abandoned.

## Managed Agent Sandboxing

Managed agents must enforce:

- CPU limit
- Memory limit
- Disk limit
- Execution timeout
- Network egress policy
- Secret scope
- Workspace cleanup

## Local Agent Trust Model

Local agents run in user workspace mode.

The user accepts risk similar to running tests locally or using GitHub self-hosted runner.

Local agents should not require Docker for MVP.

## Protocol Compatibility

Server should support a compatibility window.

Example:

- Current protocol: 1.3
- Supported agents: 1.1, 1.2, 1.3
- Agents below 1.1 must upgrade

## Open Questions

1. Should the agent use long polling or WebSocket for job acquisition?
2. What is the default job timeout?
3. What artifact size requires chunked upload?
4. How many protocol versions should be supported?
5. Should local agent auto-update or require manual update?