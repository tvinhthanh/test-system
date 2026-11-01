# ATP interface rules for Stitch work

This is a repository briefing for people and agents preparing Stitch prompts. `.stitch/rules.md` is a local convention; Stitch is not known to load it automatically. Include this file together with [../PROJECT.md](../PROJECT.md) and [../DESIGN.md](../DESIGN.md) in the design request. If source files are available, inspect the current screen and tokens before drawing.

## Read in this order

1. [../SPEC.md](../SPEC.md) for product behavior and decisions. It wins if documents disagree.
2. [../PROJECT.md](../PROJECT.md) for the current product brief, users, workflows, and screen map.
3. [../DESIGN.md](../DESIGN.md) for visual tokens and component behavior.
4. [../frontend/FRONTEND_RULES.md](../frontend/FRONTEND_RULES.md), [../frontend/src/shared/theme.css](../frontend/src/shared/theme.css), [../frontend/src/app/router/index.ts](../frontend/src/app/router/index.ts), and the affected Vue screen for implementation detail. When code is requested, also read [../frontend/AGENTS.md](../frontend/AGENTS.md) and [../frontend/PLAN.md](../frontend/PLAN.md).

## Draw the requested screen

- Keep ATP a compact operational dashboard for real QA/engineering work. Show the project and useful evidence above the fold. Use English UI labels that match the existing routes and terms.
- Follow the existing shell: 240px grouped sidebar (Workspace, Work, Results, Manage) on desktop; compact workspace/project/screen breadcrumb; mobile navigation drawer; centered content up to `max-w-6xl`. Reuse the current route's page header, tabs, filters, table/list, and detail patterns. Avoid adding a decorative footer.
- On desktop, related Overview/ROI metrics share one divided strip, and a short weekly chart shows category counts alongside it. Start testing presents its ten types as two columns of readable rows. Keep the existing mobile layouts and real data semantics.
- Keep navigation and actions permission-aware: CI tokens, Members, and Team appear only for users with the corresponding access. Show inherited roles as read-only.
- Build from the actual light and dark tokens in `DESIGN.md`. Show both modes for a new pattern. Use neutral surfaces, restrained green for primary action/focus, and semantic tones only for status. Make buttons look like product controls with clear hierarchy and labels, not promotional calls to action.
- Preserve truth in state presentation: passed plus flaky is amber and labeled; missing release evidence is not approval; AI hypotheses and ROI require their actual evidence. Render errors, stacks, IDs, and logs as selectable text with Copy.
- Cover loading, first-run empty, filtered empty, error/retry, success, and blocked/permission states. Show a narrow mobile layout and a wide desktop layout. Keep status understandable without color, 44px touch targets, visible focus, and readable contrast.
- For a screen proposal, name the route, user goal, primary action, data shown, visible states, and responsive behavior. Identify any proposed behavior or metric that is absent from the current product instead of drawing it as shipped.

## Avoid the generic AI dashboard look

No hero banner inside the app, neon or purple wash, giant rounded buttons, gradient cards, decorative illustration, fake analytics, repetitive cards, or icon-only navigation without labels. Do not move an existing decision into a hidden menu just to make the mockup look cleaner.

## Ready-to-use Stitch prompt prefix

> Design the requested ATP Report Center screen using the attached `PROJECT.md`, `DESIGN.md`, and `.stitch/rules.md`. Follow the existing Vue dashboard's information architecture and terminology. Show one wide desktop layout and one 360px mobile layout, with light and dark appearance. Prioritize real test evidence and the next user decision. Label data as illustrative when it is not from the running system. Keep buttons, statuses, tables, and empty/error states consistent with the attached design system.
