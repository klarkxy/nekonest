# syntax=docker/dockerfile:1.12

FROM --platform=$BUILDPLATFORM node:24-alpine@sha256:d32cdf619f63fe0471182d08996dd516c6275bb5fd31ae06e55a570bd9e1ad43 AS pwa-build
WORKDIR /src/pwa
RUN --mount=type=cache,target=/root/.npm \
    npm install --global --ignore-scripts pnpm@10.29.2
COPY pwa/package.json pwa/pnpm-lock.yaml pwa/pnpm-workspace.yaml ./
RUN --mount=type=cache,target=/pnpm/store \
    pnpm config set store-dir /pnpm/store && \
    pnpm install --frozen-lockfile \
      --network-concurrency=1 \
      --fetch-retries=5 \
      --fetch-retry-mintimeout=10000 \
      --fetch-retry-maxtimeout=60000
COPY pwa/ ./
RUN pnpm build

FROM --platform=$BUILDPLATFORM golang:1.22-alpine@sha256:1699c10032ca2582ec89a24a1312d986a3f094aed3d5c1147b19880afe40e052 AS server-build
ARG TARGETOS=linux
ARG TARGETARCH=amd64
ARG VERSION=0.0.0-dev
WORKDIR /src/server
COPY server/go.mod server/go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY server/ ./
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build \
    -trimpath \
    -buildvcs=false \
    -ldflags="-s -w -X github.com/nekonest/server/internal/buildinfo.Version=${VERSION}" \
    -o /out/nekonest-server ./cmd/server

FROM alpine:3.22@sha256:14358309a308569c32bdc37e2e0e9694be33a9d99e68afb0f5ff33cc1f695dce AS runtime
RUN apk add --no-cache ca-certificates tzdata \
    && addgroup -S -g 10001 nekonest \
    && adduser -S -D -H -u 10001 -G nekonest nekonest \
    && mkdir -p /app/pwa-dist /data /tmp \
    && chown -R 10001:10001 /app /data /tmp \
    && chmod 0700 /data \
    && chmod 1770 /tmp
COPY --from=server-build --chown=10001:10001 /out/nekonest-server /usr/local/bin/nekonest-server
COPY --from=pwa-build --chown=10001:10001 /src/pwa/dist /app/pwa-dist

USER 10001:10001
WORKDIR /app
EXPOSE 8080
VOLUME ["/data"]
STOPSIGNAL SIGTERM
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
  CMD wget -qO- http://127.0.0.1:8080/health | grep -q '"status":"nyan~"' || exit 1
ENTRYPOINT ["/usr/local/bin/nekonest-server"]
CMD ["-port", "8080", "-data", "/data", "-pwa", "/app/pwa-dist"]
