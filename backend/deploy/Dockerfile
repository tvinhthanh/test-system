# syntax=docker/dockerfile:1.7
# One image, every binary (§11.5: non-root, pinned, healthcheck). Build from
# backend/ so the module is the context:
#
#   docker build -f deploy/Dockerfile -t atp/backend:<git-sha> .
#
# Stage 1 compiles with the exact toolchain go.mod asks for (golang:1.25.13
# carries the stdlib fixes govulncheck flagged, see go.mod); GOTOOLCHAIN=local
# refuses to download anything else. Static binaries, no cgo, symbols stripped.
FROM golang:1.25.13-alpine AS build
WORKDIR /src
ENV CGO_ENABLED=0 GOFLAGS=-trimpath GOTOOLCHAIN=local
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    go build -ldflags='-s -w' -o /out/ ./cmd/api ./cmd/worker ./cmd/migrate ./cmd/admin ./cmd/healthcheck

# Stage 2: distroless static — no shell, no package manager, runs as the
# unprivileged `nonroot` user (uid 65532). The image is the same for api,
# worker, migrate and admin; compose picks the entrypoint per service.
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/ /app/
USER nonroot:nonroot
ENV HTTP_ADDR=:8080 METRICS_ADDR=:9090
EXPOSE 8080 9090
HEALTHCHECK --interval=15s --timeout=3s --start-period=10s --retries=3 \
    CMD ["/app/healthcheck", "http://127.0.0.1:8080/healthz"]
ENTRYPOINT ["/app/api"]
