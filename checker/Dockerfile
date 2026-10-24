# syntax=docker/dockerfile:1.7
# The URL checker (QĐ-14): Node 22 and Playwright's headless Chromium shell,
# nothing else. Built by compose (profile "urlcheck") from checker/:
#
#   docker build -t atp/checker:local .
#
# Runs as the image's unprivileged `node` user. Chromium runs without its
# own sandbox (Playwright's default); the container — no capabilities, no
# new privileges, a network of its own — is the boundary, and every request
# the page makes goes through the guarded proxy (src/proxy.mjs).
FROM node:22-bookworm-slim
WORKDIR /app
ENV NODE_ENV=production PLAYWRIGHT_BROWSERS_PATH=/ms-playwright
COPY package.json package-lock.json ./
RUN npm ci --omit=dev --no-audit --no-fund \
 && npx playwright install --with-deps --only-shell chromium \
 && rm -rf /root/.npm /var/lib/apt/lists/*
COPY src ./src
USER node
EXPOSE 8090
HEALTHCHECK --interval=30s --timeout=5s --start-period=20s --retries=3 \
  CMD ["node", "-e", "fetch('http://127.0.0.1:8090/healthz').then(r => process.exit(r.ok ? 0 : 1), () => process.exit(1))"]
CMD ["node", "src/server.mjs"]
