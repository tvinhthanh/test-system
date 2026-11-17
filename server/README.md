# Internal Demo Deployment

Use Linux/macOS or Windows with Docker Desktop in Linux-container mode and
Git Bash. Run commands from the repository root. The deployment host needs
enough capacity for the Compose stack; the development laptop does not have
to be that host. Do not start Docker on a constrained laptop just to run the
checks below.

## Read-Only Preflight

```bash
bash server/atp.sh doctor
bash server/atp.sh doctor --pilot
```

`doctor` checks Docker Compose, a reachable Linux engine, curl, required config
and secrets, object-store fields, and Compose validation when the engine is
available. It does not start containers, edit config, install certificates,
open the firewall, contact the bucket or print credential values. Exit 0 means
the checked prerequisites passed; exit 1 means at least one failed.

The default permits a memory-store/dev configuration with a warning for trying
the app. `--pilot` additionally requires S3-compatible storage, staging/prod mode
and secure cookies. Neither mode certifies security, bucket access, available
capacity, backup quality or application health.

## Prepare The Deployment Host

1. Check out the intended commit on the deployment host.
2. Run `bash server/atp.sh init <hostname-or-LAN-IP>` there. Select an existing
   S3/R2/MinIO bucket for artifacts that must survive restart. Use a dedicated
   bucket/account scope and do not commit `backend/deploy/.env`.
3. Run `bash server/atp.sh doctor --pilot`. Resolve every failure before starting
   the pilot stack. A syntactically valid config does not prove S3 access.
4. Run `bash server/atp.sh up`, then `bash server/atp.sh seed` once for the first
   tenant. Seed prints the initial account and ingest token; treat that output
   as secret. Do not seed a second tenant accidentally during upgrades.
5. Configure the intended LAN/firewall access. For an internal Caddy CA, export
   it with `bash server/atp.sh ca` and trust it on the client machines/reporters.
   A test runner's localhost TLS bypass is not the tester deployment setup.

See [deployment configuration](../backend/deploy/README.md) for TLS, object
storage, monitoring and the production/staging differences.

## Acceptance Evidence

These checks must be performed on the intended deployment, not inferred from
the CI memory-store stack. Record commit, host, date, result and evidence in
the table when actually performed. Do not store passwords/tokens in evidence.

| Check | Required Result | Status |
| --- | --- | --- |
| Preflight | `doctor --pilot` passes on the deployment host | Pending |
| Tester access | A second device signs in over trusted HTTPS at the configured address | Pending |
| Daily workflow | Requirement -> case -> plan -> assignment -> failure/bug/evidence -> retest -> named release decision | Pending |
| Restart persistence | Record IDs and evidence bytes, restart API/worker, then reopen the same history and download identical evidence | Pending |
| Database backup | `backup`, list dumps with `backups`, and `backup-copy <off-host-destination>`; verify the destination is actually outside this host | Pending |
| Artifact backup | Separate durable backup/versioning/retention for the object store, including config/key recovery | Pending |
| Restore drill | Restore DB and artifacts in an isolated environment; verify history, access, evidence bytes and worker recovery | Pending |
| Team acceptance | Testers complete their own project cycle and record gaps/critical issues | Pending |

`backup-copy` copies database dumps only. It does not copy the object-store
bucket, guarantee an off-host destination or perform a restore. Keep deployment
secrets recoverable through an approved secure backup; TOKEN_PEPPER is needed
for existing tokens and MFA enrollment. Never run `reset` on retained demo or
pilot data: it deletes the Compose data and backup volumes.

## For Testers Without The Source (QĐ-14)

- `bash server/atp.sh up` also packs the Playwright reporter into
  `backend/deploy/downloads/`; Caddy serves it at `https://<host>/downloads/`
  and the dashboard's CI tokens page prints the `npm install` line.
  `bash server/atp.sh reporter` re-packs it alone. Tarball names carry the
  version and a content hash; old ones are kept because lockfiles pin them.
- URL checks are off by default. Set `URLCHECK=on` in `backend/deploy/.env`
  and run `bash server/atp.sh up`: the script adds the compose profile
  `urlcheck`, which builds and starts the `checker` service (headless
  Chromium, about 1 GB of image). It runs on its own Docker network with the
  worker only and never opens loopback or link-local (cloud metadata)
  addresses. Private ranges stay closed unless `URLCHECK_ALLOW_PRIVATE=on`,
  which lets anyone who may run a check make this host open pages on those
  networks — turn it on only for a staging site on the office LAN.

## Self-Serve Sign-Up (QĐ-17)

Off by default. With `SIGNUP=on` (which needs `MAIL=smtp`; the api refuses
to start otherwise), anyone can ask to join an organization, workspace or
project by its id with a role. The form answers the same for every address
and mails a link (`https://<host>/signup#…`, one use, 24 hours); the account
exists only once that link is opened, and it sees nothing until an admin of
that node approves the request. Run `bash server/atp.sh restart` after
changing it.

## AI-Drafted Test Cases (QĐ-15)

Optional. `LLM_API_KEY` in `backend/deploy/.env` is the operator's Anthropic
API key, offered to every organization; leave it empty and only
organizations that save their own key can draft. The model, and whether an
organization uses its own key or the server's, are chosen in the dashboard
(Team → organization → AI settings) — there is no model setting in `.env`.
Run `bash server/atp.sh restart` after changing the key. The worker sends
the test type, the tester's description and, with URL checks on, the page's
structure to Anthropic; nothing is sent until a tester asks for drafts.

## Auto Test, Site Scans And Runs From CI (QĐ-16)

Needs `URLCHECK=on`: cases run in browser sessions of the `checker`
service (`CHECKER_SESSIONS`, default 4, about 150 MB each). Testers keep
test accounts per environment in the dashboard; their passwords are sealed
with a key derived from `TOKEN_PEPPER` (changing the pepper means saving
them again) and never reach a model. AI drafting, step repairs and finding
sign-in steps use the organization's AI settings (see above); without a
key, cases written by hand still run. Steps really happen on the site — a
message is really sent — so point environments at staging. A deploy
pipeline starts runs with a token issued with **Start auto runs**
(`project:autorun`); the dashboard's Auto test page has the step.

## Local Dev Stack Without Owner MFA

On a throwaway local stack (`APP_ENV=dev`), `bash server/dev-owner-mfa.sh off`
turns org owners who have no MFA into `org_admin`, so the dashboard opens
without the TOTP enrolment screen; `on <email>` restores the owner role and
`status` lists organization roles. It edits the database directly (no audit
row) and refuses any other `APP_ENV`. Never use it on a pilot or shared host.

## Regression Tests

```bash
bash -n server/atp.sh
bash server/tests/doctor.sh
```

The regression suite uses a fake Docker CLI and a temporary repository with
spaces in its path. It checks stopped/non-Linux engines, missing Compose,
invalid config, memory-store restrictions, cookie settings, placeholders and
missing secrets/config. It verifies no credential is printed and config is
unchanged. Passing these tests is not evidence of a successful deployment.
