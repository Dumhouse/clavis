# syntax=docker/dockerfile:1

# ---- Web ---------------------------------------------------------------
FROM node:24-alpine@sha256:116b800c3754b440ee0aa3ed2f20f5bf3d99a554bfd9adeea09ea8017f6e85f5 AS web
WORKDIR /src/web
ENV CI=true
RUN corepack enable
COPY web/package.json web/pnpm-lock.yaml ./
RUN --mount=type=cache,target=/root/.local/share/pnpm/store \
    pnpm install --frozen-lockfile
COPY web/ ./
RUN pnpm build

# ---- Server ------------------------------------------------------------
FROM golang:1.27@sha256:ec47ea3725fea0cc88f6de94919aef19f40f12fc1ec0e2d1b3426dbf15ecca04 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
COPY --from=web /src/web/dist ./web/dist

ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILD_DATE=unknown
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath \
    -ldflags "-s -w \
      -X github.com/dumhouse/clavis/internal/version.Version=${VERSION} \
      -X github.com/dumhouse/clavis/internal/version.Commit=${COMMIT} \
      -X github.com/dumhouse/clavis/internal/version.Date=${BUILD_DATE}" \
    -o /out/clavis ./cmd/clavis

# ---- Runtime -----------------------------------------------------------
FROM gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab
COPY --from=build /out/clavis /usr/local/bin/clavis
USER nonroot:nonroot
# Create /data owned by nonroot (WORKDIR after USER does that under BuildKit;
# distroless has no shell for mkdir/chown). Without it Docker creates the
# volume mount point as root and the server cannot write clavis.db.
# /backups gets the same treatment, for a separate backup volume.
WORKDIR /backups
WORKDIR /data
EXPOSE 8080 8443
ENV CLAVIS_SERVER_ADDR=:8080 \
    CLAVIS_DATA_DIR=/data \
    CLAVIS_LOG_FORMAT=json
VOLUME ["/data"]
# The image has no shell or curl, so the binary probes /readyz itself. Start
# period covers database migrations on the first start after an upgrade.
HEALTHCHECK --interval=30s --timeout=5s --start-period=30s --retries=3 \
  CMD ["/usr/local/bin/clavis", "healthcheck"]
ENTRYPOINT ["/usr/local/bin/clavis"]
CMD ["server"]
