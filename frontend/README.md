# ATP Frontend

Vue 3 + TypeScript web client. Read [PLAN.md](PLAN.md),
[FRONTEND_RULES.md](FRONTEND_RULES.md), and [API_CONTRACT.md](API_CONTRACT.md)
before changing code. The user-selected Vue stack supersedes SPEC QD-9's
Flutter choice. `lib/`, `pubspec.yaml`, `analysis_options.yaml`, and `web/` are
inactive Flutter draft files; npm/Vite never compile them.

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
