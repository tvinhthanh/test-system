# Frontend Continuity

Before editing this directory, read PLAN.md, FRONTEND_RULES.md, and the relevant
SPEC.md sections. Use API_CONTRACT.md for proposed frontend HTTP schemas; verify
actual Go routes before claiming integration.

The user explicitly selected Vue 3 + TypeScript (SPEC QD-9, revised
2026-09-14). The earlier Flutter draft was deleted on 2026-09-18; do not
reintroduce it. Work in src/ through the slice recorded in PLAN.md. Update the checklist, verification evidence, and next step
before finishing. Preserve concurrent backend changes.

Follow View -> composable/store -> repository -> ApiClient. Live API failures
must never fall back to fixtures. Tests and bundle checks must not be marked
passed without execution evidence; mocked HTTP is not real backend integration.
