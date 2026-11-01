---
version: alpha
name: ATP Report Center
description: A calm, compact operational interface for testing evidence and team triage.
colors:
  primary: "oklch(0.37 0.075 158)"
  light-background: "oklch(0.992 0.001 150)"
  light-foreground: "oklch(0.2 0.012 155)"
  light-card: "oklch(1 0 0)"
  light-sidebar: "oklch(0.975 0.003 150)"
  light-muted: "oklch(0.965 0.003 150)"
  light-muted-foreground: "oklch(0.5 0.012 155)"
  light-border: "oklch(0.905 0.005 150)"
  light-primary-foreground: "oklch(0.99 0.005 150)"
  light-secondary: "oklch(0.962 0.004 150)"
  light-secondary-foreground: "oklch(0.28 0.012 155)"
  light-accent: "oklch(0.955 0.006 152)"
  light-ring: "oklch(0.55 0.11 158)"
  light-success: "oklch(0.5 0.12 155)"
  light-success-soft: "oklch(0.955 0.03 155)"
  light-warning: "oklch(0.53 0.12 75)"
  light-warning-soft: "oklch(0.965 0.04 85)"
  light-danger: "oklch(0.53 0.18 24)"
  light-danger-soft: "oklch(0.96 0.03 24)"
  light-info: "oklch(0.52 0.13 252)"
  light-info-soft: "oklch(0.955 0.025 252)"
  light-neutral: "oklch(0.52 0.01 155)"
  light-neutral-soft: "oklch(0.955 0.004 150)"
  dark-background: "oklch(0.165 0.006 155)"
  dark-foreground: "oklch(0.95 0.005 150)"
  dark-card: "oklch(0.2 0.007 155)"
  dark-sidebar: "oklch(0.15 0.006 155)"
  dark-muted: "oklch(0.235 0.007 155)"
  dark-muted-foreground: "oklch(0.7 0.01 152)"
  dark-border: "oklch(1 0 0 / 10%)"
  dark-primary: "oklch(0.8 0.12 158)"
  dark-primary-foreground: "oklch(0.18 0.03 158)"
  dark-secondary: "oklch(0.26 0.008 155)"
  dark-secondary-foreground: "oklch(0.93 0.005 150)"
  dark-accent: "oklch(0.255 0.009 155)"
  dark-ring: "oklch(0.7 0.12 158)"
  dark-success: "oklch(0.76 0.13 155)"
  dark-success-soft: "oklch(0.5 0.12 155 / 18%)"
  dark-warning: "oklch(0.8 0.13 80)"
  dark-warning-soft: "oklch(0.56 0.12 75 / 20%)"
  dark-danger: "oklch(0.74 0.16 24)"
  dark-danger-soft: "oklch(0.53 0.18 24 / 20%)"
  dark-info: "oklch(0.76 0.11 252)"
  dark-info-soft: "oklch(0.52 0.13 252 / 20%)"
  dark-neutral: "oklch(0.72 0.01 155)"
  dark-neutral-soft: "oklch(1 0 0 / 7%)"
typography:
  body:
    fontFamily: "Geist Variable"
    fontSize: 14px
    fontWeight: 400
    lineHeight: 1.43
  page-title:
    fontFamily: "Geist Variable"
    fontSize: 24px
    fontWeight: 600
    lineHeight: 1.33
    letterSpacing: -0.02em
  eyebrow:
    fontFamily: "Geist Variable"
    fontSize: 11px
    fontWeight: 600
    lineHeight: 1.3
    letterSpacing: 0.12em
  section-title:
    fontFamily: "Geist Variable"
    fontSize: 16px
    fontWeight: 600
    lineHeight: 1.5
  metadata:
    fontFamily: "Geist Variable"
    fontSize: 12px
    fontWeight: 400
    lineHeight: 1.33
  technical:
    fontFamily: "Geist Mono Variable"
    fontSize: 13px
    fontWeight: 400
    lineHeight: 1.625
  button:
    fontFamily: "Geist Variable"
    fontSize: 14px
    fontWeight: 500
    lineHeight: 1.43
rounded:
  sm: 4px
  md: 6px
  lg: 8px
  xl: 12px
  full: 9999px
spacing:
  xs: 4px
  sm: 8px
  base: 12px
  md: 16px
  lg: 24px
  xl: 32px
components:
  button-primary-light:
    backgroundColor: "{colors.primary}"
    textColor: "{colors.light-primary-foreground}"
    typography: "{typography.button}"
    rounded: "{rounded.md}"
    height: 36px
  button-primary-dark:
    backgroundColor: "{colors.dark-primary}"
    textColor: "{colors.dark-primary-foreground}"
    typography: "{typography.button}"
    rounded: "{rounded.md}"
    height: 36px
  surface-light:
    backgroundColor: "{colors.light-card}"
    textColor: "{colors.light-foreground}"
    rounded: "{rounded.lg}"
  surface-dark:
    backgroundColor: "{colors.dark-card}"
    textColor: "{colors.dark-foreground}"
    rounded: "{rounded.lg}"
  app-canvas-light:
    backgroundColor: "{colors.light-background}"
    textColor: "{colors.light-foreground}"
  app-canvas-dark:
    backgroundColor: "{colors.dark-background}"
    textColor: "{colors.dark-foreground}"
  sidebar-light:
    backgroundColor: "{colors.light-sidebar}"
    textColor: "{colors.light-foreground}"
  sidebar-dark:
    backgroundColor: "{colors.dark-sidebar}"
    textColor: "{colors.dark-foreground}"
  muted-panel-light:
    backgroundColor: "{colors.light-muted}"
    textColor: "{colors.light-muted-foreground}"
    rounded: "{rounded.md}"
  muted-panel-dark:
    backgroundColor: "{colors.dark-muted}"
    textColor: "{colors.dark-muted-foreground}"
    rounded: "{rounded.md}"
  divider-light:
    backgroundColor: "{colors.light-border}"
    height: 1px
  divider-dark:
    backgroundColor: "{colors.dark-border}"
    height: 1px
  button-secondary-light:
    backgroundColor: "{colors.light-secondary}"
    textColor: "{colors.light-secondary-foreground}"
    typography: "{typography.button}"
    rounded: "{rounded.md}"
    height: 36px
  button-secondary-dark:
    backgroundColor: "{colors.dark-secondary}"
    textColor: "{colors.dark-secondary-foreground}"
    typography: "{typography.button}"
    rounded: "{rounded.md}"
    height: 36px
  control-hover-light:
    backgroundColor: "{colors.light-accent}"
    textColor: "{colors.light-foreground}"
    rounded: "{rounded.md}"
  control-hover-dark:
    backgroundColor: "{colors.dark-accent}"
    textColor: "{colors.dark-foreground}"
    rounded: "{rounded.md}"
  badge-success-light:
    backgroundColor: "{colors.light-success-soft}"
    textColor: "{colors.light-success}"
    rounded: "{rounded.full}"
  badge-success-dark:
    backgroundColor: "{colors.dark-success-soft}"
    textColor: "{colors.dark-success}"
    rounded: "{rounded.full}"
  badge-warning-light:
    backgroundColor: "{colors.light-warning-soft}"
    textColor: "{colors.light-warning}"
    rounded: "{rounded.full}"
  badge-warning-dark:
    backgroundColor: "{colors.dark-warning-soft}"
    textColor: "{colors.dark-warning}"
    rounded: "{rounded.full}"
  badge-danger-light:
    backgroundColor: "{colors.light-danger-soft}"
    textColor: "{colors.light-danger}"
    rounded: "{rounded.full}"
  badge-danger-dark:
    backgroundColor: "{colors.dark-danger-soft}"
    textColor: "{colors.dark-danger}"
    rounded: "{rounded.full}"
  badge-info-light:
    backgroundColor: "{colors.light-info-soft}"
    textColor: "{colors.light-info}"
    rounded: "{rounded.full}"
  badge-info-dark:
    backgroundColor: "{colors.dark-info-soft}"
    textColor: "{colors.dark-info}"
    rounded: "{rounded.full}"
  badge-neutral-light:
    backgroundColor: "{colors.light-neutral-soft}"
    textColor: "{colors.light-neutral}"
    rounded: "{rounded.full}"
  badge-neutral-dark:
    backgroundColor: "{colors.dark-neutral-soft}"
    textColor: "{colors.dark-neutral}"
    rounded: "{rounded.full}"
---

# ATP Report Center design system

## Overview

ATP is a working tool for QA leads, testers, developers, and engineering managers. It should feel trustworthy after hours of reading runs, comparing failures, and recording decisions: calm surfaces, clear hierarchy, compact information, and no decorative filler. The first viewport should answer a real testing question. Product UI copy is in English. [PROJECT.md](PROJECT.md) defines the product and screen map; [SPEC.md](SPEC.md) is authoritative for behavior.

The token values above mirror [frontend/src/shared/theme.css](frontend/src/shared/theme.css). That CSS file remains the implementation source of truth. When a token changes there, update this document with it. Use the existing semantic token names in code, never a raw color value in a component.

## Colors

Use near-neutral backgrounds and warm-neutral text as the main palette. The restrained brand green is for the logo, the main action in a region, and keyboard focus. It is not general decoration; charts use their fixed category tokens, even when a category happens to be green. Light mode uses a soft off-white canvas, white data surfaces, and a slightly tinted sidebar; dark mode has its own charcoal surfaces and lighter green accent. Design both modes independently. Avoid pure-black slabs, glowing outlines, neon status colors, and tinted overlays across whole pages.

| Purpose | Light | Dark | Use |
| --- | --- | --- | --- |
| Canvas | `light-background` | `dark-background` | Page background |
| Reading text | `light-foreground` | `dark-foreground` | Titles, body and data |
| Data surface | `light-card` | `dark-card` | Tables, charts, forms |
| Supporting text | `light-muted-foreground` | `dark-muted-foreground` | Metadata and help text; keep AA contrast |
| Primary action | `primary` | `dark-primary` | One strong action per region |
| Quiet controls | `light-secondary` / `light-accent` | `dark-secondary` / `dark-accent` | Filters, tabs, hover and secondary actions |

Reserve semantic tones for meaning: success, warning, danger, info, neutral. Use each with its matching soft fill, an icon, and a written label. A passed run containing flaky tests must read **“Passed with flaky tests”** in warning/amber, never clean green. A release gate distinguishes **approved**, **warnings**, **blocked**, **no finished run**, and **not evaluated**; do not imply that missing evidence is approval. Failure-category charts use the fixed category colors in `theme.css`; legends and accessible table views carry the labels.

Check text contrast on the rendered surface before accepting a new visual. Dark soft fills are translucent over the parent surface, so measure their contrast after compositing them on the dark card. If a pair fails, adjust the shared token in `theme.css` and mirror that change here rather than adding a one-off page color.

## Typography

Use the self-hosted Geist Variable for interface text and Geist Mono Variable for IDs, commit hashes, durations, error messages, stack traces, and code. Titles are compact and useful, not promotional: the existing page pattern is an 11px uppercase eyebrow, a 24px semibold title, then a short 14px description when needed. Use 16px section titles, 14px body/control text, and 12px metadata. Align numeric columns with tabular figures. Technical text must remain selectable and wrap or scroll deliberately, with a visible Copy action and feedback.

## Layout

At wide desktop widths, keep the 240px project sidebar, 56px top bar with breadcrumb/theme control, and content limited to roughly 1152px (`max-w-6xl`). Group sidebar links by Workspace, Work, Results, and Manage; omit groups without permitted links. The breadcrumb gives workspace, project, and current screen context without a redundant root label. Put the key status/action near the page title. Lists use a single filter/search row followed by a table or compact list, then pagination. Detail views use a back link, a concise title/status row, the next decision/action, and then evidence/history. On a failure detail, triage actions should be visible before the attempt logs.

Use a 4/8px spacing rhythm. Group related controls; leave enough breathing room between sections without turning every datum into a separate card. A chart must answer a named question and offer readable labels or a table alternative. At 360px, 768px, and wide desktop, preserve the same information hierarchy. The mobile sidebar becomes a drawer; filters wrap or stack; long tables may scroll within their own container without forcing page-wide horizontal scrolling.

On Overview and ROI, related metrics form one divided desktop strip rather than four repeated cards; mobile keeps individually legible cards. A short weekly chart uses the adjacent space for counts by the same categories instead of leaving a large empty plotting box. The ten Start testing types read as two desktop columns of text rows; the small-screen choice grid stays easy to tap. These are information layouts, not decorative variants.

## Elevation & Depth

Use borders, subtle surface differences, and spacing to establish hierarchy. Keep shadows faint and functional, such as a small hover or popover shadow. Do not nest cards to manufacture depth. Modals and drawers should stand out through their overlay and boundary, not through heavy glow or blur.

## Shapes

Use restrained radii: 6px for controls, 8px for data surfaces, and 12px only where a larger container benefits. Status badges may be pill shaped because their label and icon are small. Keep control shapes consistent within a screen. Do not introduce giant pill buttons or unrelated corner styles.

## Components

Follow the actual [button variants](frontend/src/components/ui/button/index.ts): **default** is the primary visual treatment for a region's main action; **outline** is for ordinary secondary actions; **secondary** supports an active control or lower-priority action; **ghost** is for low-emphasis icon or inline controls; **link** is for text navigation; and **destructive** is only for a confirmed harmful action. Selected page tabs use the existing primary underline, not a filled button. The default button is 36px high; small is 32px. Increase touch targets to at least 44px where used on touch screens. Keep labels as verbs with specific objects: “New test case”, “Import CSV”, “Resolve”, “Copy stack trace”. An icon-only control needs an accessible name and tooltip.

Reuse the generated shadcn-vue primitives in `frontend/src/components/ui/` and the composed blocks in `frontend/src/shared/`: `PageHeader`, `SectionHeading`, `ViewTabs`, `StatTile`, `StatusBadge`, `AsyncState`, `EmptyState`, `Pagination`, and `TechnicalBlock`. Data tables and status badges are core patterns; preserve their compact row density, clear column labels, and readable metadata. For Test Management, use the existing five-tab switcher rather than a second sidebar.

For every asynchronous screen draw loading skeletons, first-run empty, filtered empty, error with retry/request ID, success confirmation, and permission/blocked states. Keep user input when a save fails. Disabled controls explain the missing prerequisite. Focus visibility, semantic controls, keyboard order, reduced motion, and normal-text WCAG AA contrast are part of the visual design.

## Do's and Don'ts

- Do show actual testing signals in the first viewport: project, branch/environment, run status, failed/flaky counts, triage decision, case progress, or release evidence.
- Do use the same light/dark semantic roles and component hierarchy on every route.
- Do write realistic, clearly illustrative sample data in mockups; never fabricate measured ROI, baseline, gate approval, or an AI conclusion as if it came from the product.
- Don't use marketing heroes, glass panels, decorative blobs, gradients, generic purple, oversized headings, emoji status indicators, or a wall of metric cards.
- Don't use color as the only status signal or present a flaky pass as a clean pass.
- Don't add a new visual pattern when an existing block can express the information.
