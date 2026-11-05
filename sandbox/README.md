# Sandbox: a team's own test scripts (QĐ-19 tier 4)

A tester uploads a Playwright project or a k6 script as a `.tar.gz`
(`POST /v1/projects/{id}/script-runs`). The worker hands it to **sandboxd**
(`backend/cmd/sandboxd`), which runs it in a throwaway container. The report
comes back through the JUnit import, as a test run like any CI run's.

```
worker ──(sbxapi, SANDBOX_TOKEN)──▶ sandboxd ──(Docker socket)──▶ job container
                                      │  ▲                            │
                                      │  └── forwarder (job network) ◀┘  every connection
                                      ▼
                         the environment's host only (guarded address)
```

## What one job gets

| | |
|---|---|
| Network | An `internal` Docker network of its own: no gateway, no route out. The environment's host name points at sandboxd's forwarder, and no other name resolves. The forwarder resolves the host once, checks every address with the checker's rules (`platform/sandbox/guard.go`), and pipes TCP to that address, so TLS stays end to end. Scripts need no proxy settings. |
| Container | No capabilities, `no-new-privileges`, read-only root, uid 10001. `/work` and `/tmp` are tmpfs. No mounts, no socket, no environment variables beyond `BASE_URL` and the job's own. |
| Limits | `SANDBOX_MEMORY_MB` (2048, no swap on top), `SANDBOX_CPUS` (2), 512 processes, 4096 open files, `SCRIPT_MAX_SECONDS` (900; past it the container is killed). |
| Afterwards | The container and its network are removed, and again on sandboxd's start for anything a crash left. The archive is cleared from the database when the run ends. |

**Playwright** runs in `atp/sandbox-playwright` (this folder). Its
`@playwright/test` 1.63.0 replaces any version in the archive, so the runner
always matches the image's Chromium, Firefox and WebKit. The JUnit reporter is
always on. The config is `entry`, or `playwright.config.*` at the top of the
archive. `project` and `grep` reach the CLI as environment variables, never
inside the shell line.

The archive's own `node_modules` is used as is, nothing is installed, and the
sandbox has no registry. A project with other dependencies packs them in
(pure-JS packages work; native ones must be built for linux-x64, glibc).

**k6** runs in `grafana/k6:1.0.0`. The run's `vus` and `seconds` replace the
script's scenarios, within `LOAD_MAX_VUS` and `LOAD_MAX_SECONDS`. Because it
is real load, a k6 run needs the organization's ownership proof for the host
(tier 3), checked again right before it runs. Its thresholds and checks
become JUnit test cases. k6's exit code (99) decides whether a threshold
failed.

Two limits come from the network design. A script reaches only the
environment's host, so pages that load fonts or scripts from a CDN load
without them. And an environment addressed by IP rather than by name cannot
run scripts.

## Turning it on

```bash
# deploy/.env
SANDBOX=on
SANDBOX_TOKEN=<openssl rand -hex 32>     # atp.sh init makes one
DOCKER_GID=<stat -c %g /var/run/docker.sock>
# SANDBOX_RUNTIME=runsc                   # gVisor, if installed on the host (recommended)
./server/atp.sh up                       # adds the "scripts" profile, builds the Playwright image, pulls k6
```

**The risk you take on.** sandboxd holds the Docker socket, which means root
on the host. It is reduced to one job shape and answers only the worker with
the token, never a job's network. Still, a container escape (a kernel or runc
bug) on this host reaches everything on it, including Postgres. Put
`SANDBOX_RUNTIME=runsc` on, or run the stack on a host where that is
acceptable. Like everything with `URLCHECK_ALLOW_PRIVATE`,
`SANDBOX_ALLOW_PRIVATE=on` lets scripts reach private addresses that are an
environment's host.

## Tests

- `backend/internal/platform/sandbox`: archive checks, the guard, the
  forwarder, k6 → JUnit, the client.
- `backend/cmd/sandboxd`: the whole job against a fake Docker API, and the
  real container script run by `sh`.
- `sandbox/test/e2e.sh` (CI job `sandbox`) runs on real Docker. It sends a
  Playwright project through Chromium and Firefox to a fixture site, and
  `test/pw/tests/sandbox.spec.js` checks from inside:
  - nothing but the environment resolves;
  - there is no route to 1.1.1.1, the metadata address or the Docker host;
  - sandboxd's API refuses the job;
  - the job runs as uid 10001 on a read-only root.

  It also checks that a k6 script asking for 500 VUs runs with the run's 2,
  that a run past its limit is stopped, and that nothing is left behind.

```bash
bash sandbox/test/e2e.sh   # from the repository root; docker, jq, curl
```
