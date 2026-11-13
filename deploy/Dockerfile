# syntax=docker/dockerfile:1

# ---- Web ---------------------------------------------------------------
FROM node:24-alpine AS web
WORKDIR /src/web
ENV CI=true
RUN corepack enable
COPY web/package.json web/pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile
COPY web/ ./
RUN pnpm build

# ---- Server ------------------------------------------------------------
FROM golang:1.27 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/web/dist ./web/dist

ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILD_DATE=unknown
RUN CGO_ENABLED=0 go build -trimpath \
    -ldflags "-s -w \
      -X github.com/dumhouse/clavis/internal/version.Version=${VERSION} \
      -X github.com/dumhouse/clavis/internal/version.Commit=${COMMIT} \
      -X github.com/dumhouse/clavis/internal/version.Date=${BUILD_DATE}" \
    -o /out/clavis ./cmd/clavis

# ---- Runtime -----------------------------------------------------------
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/clavis /usr/local/bin/clavis
USER nonroot:nonroot
# Create /data owned by nonroot (WORKDIR after USER does that under BuildKit;
# distroless has no shell for mkdir/chown). Without it Docker creates the
# volume mount point as root and the server cannot write clavis.db.
WORKDIR /data
EXPOSE 8080 8443
ENV CLAVIS_SERVER_ADDR=:8080 \
    CLAVIS_DATA_DIR=/data \
    CLAVIS_LOG_FORMAT=json
VOLUME ["/data"]
ENTRYPOINT ["/usr/local/bin/clavis"]
CMD ["server"]
