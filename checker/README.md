# Checker (URL checks QĐ-14, auto test QĐ-16)

Opens pages in headless Chromium for the ATP worker, its only client.

A **URL check** reports what a tester looks at first on one page: page
load, HTTPS certificate, JavaScript errors, failed images and scripts,
broken links (first 40), accessibility (axe-core, WCAG A/AA), speed (first
byte, LCP, layout shift), page basics (title, lang, description, h1) and
phone fit — with a desktop and a phone screenshot. It can sign in first and
lists the page's same-site links, which is how a **site scan** walks a site.

A **session** runs the steps of an auto-test case (`src/steps.mjs`): each
step is data — an action (`goto`, `click`, `fill`, `select`, `check`,
`press`, `hover`, `wait`, `expect_*`) and an element named by role and
name, label, placeholder, text, test id or CSS — interpreted with
Playwright's own locators; an exact name wins over one that only contains
it. Nothing runs as code. Top-level navigation stays on the site under test
(the host and its siblings under the same registrable name): a step that
leaves it fails and the browser comes back. `{{name}}` in a step is a
variable the worker sends per call — including `{{password}}`, which is
used and forgotten when the session closes.

```
POST /check {"url": "https://staging.example.com", "login"?: {url, steps, vars}}
  → {"report": {...}, "links": [...], "screenshots": {"desktop": "<base64 JPEG>", "mobile": "..."}}
POST /outline {"url": "…"}
  → {"outline": {title, lang, headings, links, forms, buttons, text}}   (QĐ-15: what AI drafting reads)
POST /sessions {"base": "https://staging.example.com", "login"?: {url, steps, vars}}
  → {"id", "loggedIn"?, "error"?}
POST /sessions/:id/steps {"steps": [...], "vars": {...}, "shot"?: "failure"|"always"|"none"}
  → {"ok", "results": [{status, kind, error, ms}], "url", "console", tree/title/screenshot on a failure}
POST /sessions/:id/look {"shot"?: true}   → {"url", "title", "tree" (accessibility snapshot), "screenshot"?}
DELETE /sessions/:id
GET  /healthz
```

A failed step says why (`kind`): `locator` — the element was not found or
could not be used (the worker may ask a model to repair it); `assert` — an
expectation did not hold; `navigation` — a page did not open or the browser
tried to leave the site; `input` — the step itself is wrong. A session idle
for two minutes, or open for fifteen, is closed.

## Why the proxy

The checker opens addresses users type, from inside the server's network
(SSRF, §11.6 API7). Chromium is launched so that **every** connection —
pages, images, scripts, links, the link checker's requests — goes through a
proxy started for that one check (`src/proxy.mjs`): it resolves the name
once, judges every address with `src/guard.mjs`, and connects to the address
it judged, so DNS rebinding has nothing to race. Never opened: loopback,
"this host", link-local (cloud metadata at 169.254.169.254), multicast,
reserved. Private ranges only with `URLCHECK_ALLOW_PRIVATE=on`. WebRTC UDP
and QUIC are off, service workers blocked, downloads refused. Compose puts
the service on a network shared with the worker alone.

## Settings

| Env | Default | |
|---|---|---|
| `PORT` | `8090` | |
| `URLCHECK_ALLOW_PRIVATE` | `off` | `on`: also 10/8, 172.16/12, 192.168/16, 100.64/10, fc00::/7 … |
| `CHECKER_CONCURRENCY` | `2` | checks at once; more answer 503 and the worker retries |
| `CHECKER_SESSIONS` | `4` | auto-test sessions open at once; more answer 503 and the worker retries |

## Develop

```bash
npm ci
npx playwright install --only-shell chromium
npm test          # guard unit tests + real Chromium against local fixture sites (a chat that needs signing in)
npm start         # PORT=8099 npm start, then POST /check
```

The fixture site listens on 127.0.0.1, which the guard never allows; the
tests let exactly that address through and prove 127.0.0.2 and `localhost`
are still refused.
