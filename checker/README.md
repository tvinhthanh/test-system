# URL checker (QĐ-14)

Opens one page in headless Chromium and reports what a tester looks at
first: page load, HTTPS certificate, JavaScript errors, failed images and
scripts, broken links (first 40), accessibility (axe-core, WCAG A/AA), speed
(first byte, LCP, layout shift), page basics (title, lang, description, h1)
and phone fit — with a desktop and a phone screenshot. Not a test suite: one
page, a first look. The ATP worker is its only client.

```
POST /check {"url": "https://staging.example.com"}
  → {"report": {...}, "screenshots": {"desktop": "<base64 JPEG>", "mobile": "..."}}
POST /outline {"url": "…"}
  → {"outline": {title, lang, headings, links, forms, buttons, text}}   (QĐ-15: what AI drafting reads)
GET  /healthz
```

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

## Develop

```bash
npm ci
npx playwright install --only-shell chromium
npm test          # guard unit tests + real Chromium against a local fixture site
npm start         # PORT=8099 npm start, then POST /check
```

The fixture site listens on 127.0.0.1, which the guard never allows; the
tests let exactly that address through and prove 127.0.0.2 and `localhost`
are still refused.
