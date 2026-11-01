# ATP / Report Center — project brief for Stitch

Design for the actual Automation Testing Platform (ATP) web app: a workspace for planning and running tests, and understanding results over time. A reporter sends runs from a team's existing CI pipeline; ATP can also run browser test cases from the Auto test flow. The app helps people inspect failures and artifacts, recognize flaky tests, record triage decisions, connect test cases to stable test identities, and review release evidence. It is an operational tool used repeatedly during testing, debugging, and release work, not a marketing site.

## Source of truth

Read [SPEC.md](SPEC.md) for product decisions, domain rules, and role permissions; it takes precedence over historical files in `docs/`. For visual and interaction rules, use [frontend/FRONTEND_RULES.md](frontend/FRONTEND_RULES.md). For the implemented information architecture and wording, check [frontend/src/app/router/index.ts](frontend/src/app/router/index.ts), [frontend/src/app/SidebarNav.vue](frontend/src/app/SidebarNav.vue), and the relevant page in `frontend/src/features/`. [frontend/src/shared/theme.css](frontend/src/shared/theme.css) defines the live design tokens; [frontend/src/app/AppShell.vue](frontend/src/app/AppShell.vue) shows the responsive shell. [frontend/API_CONTRACT.md](frontend/API_CONTRACT.md) describes dashboard data behavior; verify an interaction against the relevant page and backend route before representing it as live.

When these sources differ, follow `SPEC.md` for product policy and the current frontend/backend code for implemented behavior. Flag a discrepancy rather than silently designing a new feature. Keep interface labels in English, as in the app.

## People and their jobs

| Person | Primary question the UI must answer |
| --- | --- |
| QA Automation Lead | Which failure needs attention, what caused it, and is this test flaky? |
| Engineering Manager | Are triage health and release evidence improving, and what remains untriaged? |
| Developer | Which attempt failed, on which branch/environment, and where are its error, stack, screenshot, video, log, or trace? |
| Manual QA / test manager | Which requirements and cases are covered, what was executed by hand or by Auto test, and how does that connect to automated test history? |

These are product personas, not permission roles. Actual access comes from organization, workspace, and project memberships: `org_owner`, `org_admin`, `workspace_admin`, `maintainer`, `member`, and `viewer` ([SPEC.md §11.3](SPEC.md), [role vocabulary](frontend/src/features/members/roles.ts)). A design must show actions only where the signed-in user has the required permission, while the server remains authoritative.

## Current app map

The signed-in app starts with **Projects** (`/`). Within a selected project, the sidebar offers **Start testing** where permitted, **Auto test**, **Overview**, **ROI**, **Test runs**, **Test intelligence**, **Test management**, and **How to test**. **CI tokens** and **Members** appear only for users with the relevant project permissions. **Team** is available to users who can manage an organization or workspace; account **Security** is reached from the user area. The same navigation is a desktop sidebar and a mobile drawer.

| Area | Existing screens and route shape | Useful content to show |
| --- | --- | --- |
| Report Center | `/p/:project/overview`, `/roi`, `/runs`, `/runs/:run`, `/runs/:run/results/:result` | Triage health, time window, run status, branch/environment, attempts, error/stack, artifacts, triage actions |
| Test intelligence | `/p/:project/tests`, `/flaky`, `/review`, `/tests/:identity/history` | Stable identities, flaky signals, review decisions, merge/split history |
| Test management | `/p/:project/specialized-results`, `/cases`, `/cases/:case`, `/requirements`, `/plans`, `/plans/:plan`, `/releases`, `/releases/:release` | Cases and requirements, automation links, manual executions and evidence, quality-gate rules and verdict |
| Start testing | `/p/:project/start`, `/plans/:plan/run` | Choose one of ten test types, a site and cases from the library, starter set or AI drafts; create a plan and run cases step by step |
| Auto test | `/p/:project/auto`, `/auto/runs/:run`, `/auto/scans/:scan` | Set up executable cases and environment test accounts, scan a site, run browser cases, inspect results and suggested repairs |
| Administration | `/p/:project/tokens`, `/p/:project/members`, `/team`, `/team/organizations/:organization`, `/team/workspaces/:workspace`, `/team/organizations/:organization/usage` | CI setup, scoped members, workspace/project management, usage |
| Account access | `/sign-in`, `/invite`, `/forgot`, `/reset`, `/account/security` | Session/MFA, invitations and password flows when configured, connected accounts, personal security |

Use the actual [route table](frontend/src/app/router/index.ts) and [Test management tabs](frontend/src/features/testmgmt/TmTabs.vue) when drawing specific screens. Keep deep-link context: project, selected area, filters, and return path should remain clear.

## Core journeys

1. **Connect a project:** choose or create a project where permitted, issue a CI token, add the Playwright reporter to an existing test repo, then inspect the first ingested run. Show a real empty state before data arrives; do not invent a populated account.
2. **Investigate a failure:** scan runs and statuses, open a run, open a result, compare attempts, read and copy technical text, inspect available artifacts, visit stable test history, then record a permitted triage decision. Keep the decision controls visible near the failure evidence.
3. **Understand test health:** use Overview and Test intelligence to inspect time-to-root-cause, triage coverage, untriaged failures, failure categories, flaky signals, and identity review. Distinguish no data, incomplete data, and a measured value.
4. **Plan and assess a release:** connect Requirement → Test Case → stable automated Test Identity → run history; record manual execution evidence in a plan; evaluate a release quality gate and show each rule, whether it applies, its severity, and its evidence. A gate verdict is distinct from a person's release decision.
5. **Administer access:** manage members at the right organization/workspace/project scope, see inherited roles as read-only, and expose token or destructive controls only to authorized users. Security flows include MFA where required.
6. **Start or automate testing:** choose a test type, site and cases for a manual round, or configure executable cases for Auto test. Auto runs execute in a browser and feed both a plan and Test runs; show the case steps, outcome and evidence without implying the UI can run arbitrary code.

## Truthful states and scope

- Draw loading, first-run empty, filtered empty, error/retry, success, disabled/prerequisite, and permission-denied states for data and mutations. Preserve filters and user input through failures. Show request IDs when an API error provides one.
- A passed run with flaky tests is **“Passed with flaky tests”** and uses warning treatment; it is never a clean green pass. Every status needs text or an icon as well as color.
- Technical report text must remain selectable and have an explicit Copy action. Treat run names, errors, stacks, logs, and attachments as untrusted content.
- Show metric values only when backed by product data. Missing baselines or insufficient evidence need explicit explanations; do not fabricate trends, savings, pass rates, customers, or release confidence.
- OAuth buttons, invitation flows, evidence, and admin actions depend on configuration, available data, and permissions. Do not present optional paths as universally enabled.
- Keep concepts distinct: a **Test run** can come from reporter ingestion or an ATP Auto test; a **Test Identity** groups a test's history; a **Test Case** is managed test documentation and can have an executable spec; a **Release Gate** evaluates evidence. Auto test runs browser cases on demand or through a scoped CI token, with fixed data actions and site boundaries; do not depict arbitrary code execution.

For page styling, follow [DESIGN.md](DESIGN.md) and the existing tokens and components. Visual exploration may improve hierarchy and clarity, but it should preserve this product model and map back to real screens.
