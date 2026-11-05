# syntax=docker/dockerfile:1.7
# The image a team's Playwright project runs in (QĐ-19 tier 4). sandboxd
# starts one container per run from it, with no capabilities, a read-only
# root, an unprivileged user (10001) and a network whose only way out is the
# environment's host — so this image needs nothing but Node, the pinned
# @playwright/test and its three browsers:
#
#   docker build -t atp/sandbox-playwright:local sandbox/playwright
#
# The run links /opt/atp/node_modules/@playwright/test into the project, in
# place of any it brings, so the test runner always matches these browsers.
# Bump @playwright/test here and in checker/ together.
FROM node:22-bookworm-slim
ENV PLAYWRIGHT_BROWSERS_PATH=/ms-playwright NODE_ENV=production
WORKDIR /opt/atp
COPY package.json package-lock.json ./
RUN npm ci --omit=dev --no-audit --no-fund \
 && npx playwright install --with-deps chromium firefox webkit \
 && chmod -R a+rX /ms-playwright /opt/atp \
 && rm -rf /root/.npm /var/lib/apt/lists/*
USER 10001:10001
WORKDIR /tmp
CMD ["node", "--version"]
