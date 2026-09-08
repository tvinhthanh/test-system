# 04 — System Architecture

## Architecture Goal

Design a platform that supports:

- Existing framework result ingestion
- Local/server agent execution
- Rich artifact collection
- Unified reporting
- Future test management
- Future AI analysis
- Future plugin ecosystem

## High-level Architecture

```text
User
↓
Web Dashboard
↓
API Server
↓
Report Center
↓
Database + Artifact Storage

Existing CI / Playwright
↓
Reporter SDK
↓
API Server

Local/Server Agent
↓
Execution Engine
↓
Reporter
↓
API Server
```

## Main Components

### Web Dashboard

Responsibilities:

- Project management
- Environment management
- Run history
- Report viewer
- Artifact viewer
- Test management UI
- AI analysis UI
- Settings and access control

### API Server

Responsibilities:

- Authentication
- Project APIs
- Environment APIs
- Run ingestion APIs
- Agent APIs
- Artifact APIs
- Report query APIs
- Notification APIs
- Webhook APIs
- Audit log

### Reporter SDK

Purpose:

Allow teams to send results from existing Playwright/Cypress/Jest/Pytest workflows into the platform without installing a full agent.

Benefits:

- Low-friction onboarding
- Works in existing CI
- Faster proof of value
- Helps validate report schema early

### Runner Agent

Purpose:

Execute or coordinate test runs under platform control.

Agent modes:

- Local workspace mode
- Server/managed container mode

The agent is needed for:

- Localhost testing
- Scheduled execution
- Controlled environment execution
- Artifact streaming
- Future distributed execution

### Report Center

Core system of record for:

- Runs
- Test results
- Retry attempts
- Flaky status
- Artifacts
- Logs
- Environment metadata
- Toolchain metadata
- AI analysis

### Artifact Storage

Stores:

- Screenshot
- Video
- Playwright trace
- HAR
- Logs
- Attachments
- Manual evidence

Storage must support retention policy from MVP.

## Agent Pull Model

The agent always initiates communication.

```text
Agent
↓
Connect / poll / long-poll
↓
Server
↓
Job response
↓
Agent executes
↓
Agent uploads results
```

Server must not initiate inbound network calls to the agent.

## Managed Agent vs Local Agent

### Local Agent

- Runs on user machine
- Can access localhost
- May not require Docker
- Uses workspace trust model
- Similar to GitHub self-hosted runner

### Managed Agent

- Runs in controlled container
- Must enforce CPU/RAM/time limits
- Must enforce egress policy
- Must isolate project dependencies
- Must support pinned runner images

## Security Architecture

Key principles:

- Do not build a full custom secret manager.
- Integrate with Vault/AWS KMS/cloud secret stores when possible.
- Provide encrypted fallback only for MVP/self-hosted use.
- Mask secrets in logs.
- Redact headers/tokens in HAR and logs.
- Treat screenshots/videos as sensitive by default.

## Multi-tenancy

Future SaaS model must define tenant boundaries.

Possible model:

```text
Organization
↓
Workspace
↓
Project
↓
Environment
↓
Run
```

Each tenant must have:

- Data isolation
- Access control
- Audit logs
- Artifact access policy
- Retention settings

## API Versioning

All public APIs must be versioned.

Examples:

```text
/api/v1/runs
/api/v1/agents
/api/v1/artifacts
```

Reporter SDK and agent protocol must include protocol version.

## Event Model

Possible events:

- run.created
- run.started
- run.completed
- run.failed
- test.started
- test.finished
- artifact.uploaded
- agent.heartbeat
- agent.offline
- ai.analysis.completed
- notification.sent

## Observability

The platform must track:

- API errors
- Agent heartbeat latency
- Job queue delay
- Artifact upload failure
- Storage usage
- Report ingestion errors
- AI analysis latency

## Important Architecture Decision

Report Center should not be downstream only from automated frameworks.

It must also accept:

- Manual execution results
- CI ingested results
- Agent executed results
- Imported historical results