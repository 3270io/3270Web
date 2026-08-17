# syntax=docker/dockerfile:1

# Run the build stage natively on the builder's architecture and cross-compile
# to the target arch. With CGO disabled this is a pure Go cross-compile, which
# avoids slow QEMU-emulated compilation for non-native targets (e.g. arm64).
FROM --platform=$BUILDPLATFORM golang:1.25-bookworm AS build
WORKDIR /src

ARG TARGETOS
ARG TARGETARCH

ENV CGO_ENABLED=0
ENV GOTOOLCHAIN=auto

COPY go.mod go.sum ./
RUN go mod download

COPY . ./
RUN GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath -ldflags "-s -w" -o /out/3270Web ./cmd/3270Web

FROM public.ecr.aws/ubuntu/ubuntu:24.04 AS runtime
WORKDIR /app

# Standard OCI metadata. The licence label is what an image scanner and a
# corporate policy gate read; s3270, installed below from the Debian archive,
# is an independent program aggregated here and keeps its own BSD 3-Clause
# terms (see THIRD-PARTY-LICENSES.md).
LABEL org.opencontainers.image.title="3270Web" \
      org.opencontainers.image.description="Browser-based IBM 3270 terminal with AI discovery, session recording and chaos exploration" \
      org.opencontainers.image.source="https://github.com/3270io/3270Web" \
      org.opencontainers.image.documentation="https://3270web.3270.io" \
      org.opencontainers.image.url="https://3270.io" \
      org.opencontainers.image.licenses="AGPL-3.0-or-later"

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates curl s3270 pr3287 \
    && rm -rf /var/lib/apt/lists/* \
    && groupadd --gid 10001 app \
    && useradd --uid 10001 --gid 10001 --no-create-home --home-dir /app app

COPY --from=build /out/3270Web /app/3270Web
COPY web/ ./web/

# State goes somewhere the image does not, because the image is replaced on
# every deploy. Accounts, API tokens, the audit trail and everyone's saved work
# live here; /app holds only the program and its web assets. Without this,
# `docker compose pull && up -d` destroys every account and brings the instance
# back up in first-run setup for whoever reaches it first.
ENV DATA_DIR=/data
RUN mkdir -p /data && chown -R app:app /app /data

# Declared so that an instance started without an explicit volume still keeps
# its accounts across a container replacement. Name it in compose (see
# docker-compose.yml) to keep track of which volume is which.
VOLUME ["/data"]

USER app

# The server binds to 127.0.0.1 by default, which is correct for a desktop or
# `go run` install but unreachable inside a container: a published port forwards
# to the container's external interface, so a loopback-only listener refuses
# every connection from the host while the container still reports healthy.
# Bind all interfaces here and let the `ports:` mapping decide what is exposed
# (use "127.0.0.1:3270:3270" to keep it on the host's loopback).
ENV WEBUI_BIND=0.0.0.0

EXPOSE 3270

HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 \
    CMD curl -fsS http://127.0.0.1:3270/healthz || exit 1

ENTRYPOINT ["/app/3270Web"]
