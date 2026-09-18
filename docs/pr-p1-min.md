## P1-min (QĐ-11): backend, reporter, dashboard, deploy shape, gate evidence

> Landed on `main` 2026-09-18 as a fast-forward of 32 commits onto `10984f0`; branches `p1-min` and `p15-em-review` were deleted. No PR was opened — this file stays as the merge record.

24 commits, 11-09 → 18-09. Everything the §4.4 P1-min column asks for, plus the technical-gate evidence §14 requires before anyone real logs in. Nothing here needs a product decision; what is still open needs infrastructure (listed at the end).

### What is in the branch

**Backend (`backend/`, Go 1.25)** — every §6.9.11 P1-min module except `billing`: tenancy, authz (argon2id login, sessions, CSRF, 3 roles, ingest tokens with HMAC + audit), ingest (500-event batches, idempotent on `(localRunId, sequence)`, tier-1 inline resolve), identity resolver (§9, pg_trgm parity with the prototype), results, flaky (§8.6, incremental), artifacts (declare/PUT/complete, caps, retention, linking), triage, query (read model), Postgres job queue with lease recovery and pruning. Architecture rules §6.9.3 are enforced by `test/arch`. 12 migrations.

**Reporter (`reporter/playwright/`)** — §7.5 npm reporter: 5 s / 100-event batching, 1/2/4/8 s retry, spool on outage (the customer's run never fails), artifacts with client-side sha256 and caps, HAR stripping, fingerprint §9.2 pinned to the Go vectors, ANSI-clean error text.

**Dashboard (`frontend/`, Vue 3)** — sign-in, projects, runs, run detail, failure detail (attempts, error/stack, artifacts: screenshots/video inline, `trace.zip` in a self-hosted Playwright trace viewer, triage), identity history, flaky view. `scripts/live-check.mjs` drives the real UI through the real api; the CI `e2e` job runs it on the compose stack.

**Deploy (`backend/deploy/`)** — distroless non-root image, `compose.yml` staging (Postgres 17.5, one-shot migrate, api, worker, Prometheus with the §6.4 rules, Caddy as the only TLS listener, daily backup), `openapi.yaml` as the §11.6 API9 source of truth (structurally tested against the router), `.env.example`, backup/restore scripts.

**CI (`.github/workflows/ci.yml`)** — gofmt/vet, `go test ./...` on Postgres 17 (T1/T3/T4 suites + the restore drill), govulncheck, image build, compose config, promtool, Redocly lint, reporter and frontend tests + `npm audit`, and the `e2e` job.

### Gate evidence (`backend/GATES.md` — dated, with commands)

| Gate | State |
|---|---|
| T0 core flow | ✅ contract test on real Postgres + real reporter run + real browser (`live-check.mjs`) |
| T1 no data loss | ✅ `test/failure`: kill api mid-batch → replay dedupes; kill worker mid-job → lease recovery; DB connections dropped under traffic. Found and fixed a 40P01 deadlock. |
| T2 load §14.2 | dashboard p95 < 500 ms at 100k **and 1M** results ✅; resolver p99 ✅; ingest 100-concurrent p95 ≈ 2 s on the laptop (target < 1 s) — **needs a server measurement** |
| T3 observability | ✅ alert rules on real metric names, api-side health gauges, 4 failure scenarios visible on `/metrics`, no secrets in logs |
| T4 security | ✅ cross-tenant walk over every mounted route (3 foreign principals), §11.6 checklist with evidence, restore drill (dump → new DB → every table checksum-equal, api byte-identical, worker resumes), audit of revokes |
| T5 soak | harness proven: **2 h on the laptop, 5 400 runs, 545k results, 87k requests, 0 errors, every trend flat**; found and fixed: jobs never pruned, backlog gauge scanning the whole table. **The 24–72 h run needs a host with S3.** |

### Review pointers

- `backend/GATES.md` is the record of what actually ran; `backend/README.md` the module map and conventions; `frontend/PLAN.md` the UI continuity record; `docs/openapi-contract-reimplementation.md` a review brief that was implemented (cookie name, `sequence ≥ 1`).
- Stack: QĐ-9 originally chose Flutter; the owner chose Vue on 14-09 (`frontend/PLAN.md`). SPEC QĐ-9 was rewritten for Vue on 18-09 and the inert Flutter draft was removed from `frontend/` the same day.
- `frontend/dist/trace/` is Playwright's vendored trace viewer (pinned `playwright-core` devDependency), not our bundle.

### Included beyond P1-min (was branch `p15-em-review`)

The owner asked on 18-09 to go beyond the §4.4 cut: the §2.5 engineering overview (P1.5), the §9.4 review queue / merge-split UI, and unit tests for the identity-operations repository layer. Three commits; they merged into `main` together with P1-min instead of as a separate PR.

### Still open before Gate-1 (infrastructure, not code)

- 24–72 h soak on a non-laptop host with `OBJSTORE=s3` (`deploy/README.md` has the command).
- Ingest 100-concurrent p95 measured on a server with a separate load generator.
- TLS Postgres for `APP_ENV=prod` (config refuses `sslmode=disable` there).
- External pentest before the first paying customer.

🤖 Generated with [Claude Code](https://claude.com/claude-code)
