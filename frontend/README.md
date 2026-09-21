# ATP Frontend

Vue 3 + TypeScript web client, styled with Tailwind CSS 4 and shadcn-vue
(reka-ui) on self-hosted Geist fonts; light and dark palettes. Screens: sign-in,
projects, runs, failure detail, test intelligence (identities, flaky, review
queue), engineering overview, and — since 2026-09-21 — the Test Management
Plane (cases, requirements, plans, releases with the quality gate). Read [PLAN.md](PLAN.md),
[FRONTEND_RULES.md](FRONTEND_RULES.md), and [API_CONTRACT.md](API_CONTRACT.md)
before changing code. SPEC QD-9 records the stack: Vue 3 was chosen on 2026-09-14 (replacing the
earlier Flutter choice); the inert Flutter draft was removed on 2026-09-18 and
lives only in git history.

## Run

Node 22.12+ is required. Use the committed package-lock.json with `npm ci`.

```sh
npm ci
npm run dev:fixture
```

Fixture preview is development-only, shows an explicit label, and starts with
a preview session. Sign out then use any email/non-empty password to exercise
the login form. It is not real authentication or persisted business data.

```sh
npm run dev
```

Live mode forwards `/v1` through the Vite dev proxy to `127.0.0.1:8080`.
Session/dashboard endpoints must first be implemented by Go; no mock fallback
is performed. Deploy production static files with same-origin `/v1` forwarding
and a history fallback to index.html for UI routes only. API requests must not
receive the SPA HTML fallback. Production cookie/security headers belong to
the backend/reverse proxy configuration.

## Checks

```sh
npm run typecheck
npm run test:unit
npm run build
```

Against the real backend (no mocks) — api + worker running, a member user and
some ingested runs, the UI in live mode (`npm run dev`, or the Caddy of
`../backend/deploy/`):

```sh
ATP_UI=http://127.0.0.1:5173 ATP_EMAIL=alice@example.com ATP_PASSWORD=… \n  node scripts/live-check.mjs ../reporter/playwright/node_modules/playwright
```

It lists every `/v1` call with its real status and fails on any non-2xx (other
than the deliberate wrong-password 401) or console error. This is the
repeatable form of GATES.md T0 layer 3.

## Artifacts on the failure page

Screenshots and videos render inline from the api's content route; `trace.zip`
opens in Playwright's trace viewer served from **this origin** at `/trace/`
(`vite dev` serves it from `node_modules/playwright-core`; `npm run build`
copies it to `dist/trace/`, which Caddy serves with the rest of `dist/`). The
viewer's service worker fetches the zip with the session cookie, so a
customer's trace never leaves the deployment. `playwright-core` is a pinned
devDependency for that directory alone — it is not part of the SPA bundle and
the bundle gate skips `dist/trace/`.

Build includes a bundle gate: initial JS and each lazy JS chunk at most 100 KiB
gzip, at least four lazy route modules, no known fixture payload markers.
These are initial project budgets, not performance measurements of a real
customer deployment. Change budgets only with a recorded dependency rationale.

## Ownership

- `app/`: composition root, dependency injection, router, shell.
- `core/api`: Fetch transport, errors, DTO decode primitives.
- `core/state`: cancellable async resource lifecycle.
- `features/auth`: session repository and Pinia store.
- `features/reports`: DTOs, repository, route composables and views.
- `shared/`: semantic UI components, formatters and CSS tokens.
- `dev/`: fixture transport, excluded by compile-time production branching.

Route changes own their requests; run summary is not refetched on result page
changes. No polling, N+1 row fetching or shared report cache exists in this
slice. API client cancels all pending reads on session invalidation.
