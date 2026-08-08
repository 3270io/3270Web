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

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates curl s3270 \
    && rm -rf /var/lib/apt/lists/* \
    && groupadd --gid 10001 app \
    && useradd --uid 10001 --gid 10001 --no-create-home --home-dir /app app

COPY --from=build /out/3270Web /app/3270Web
COPY web/ ./web/

RUN chown -R app:app /app

USER app

# The server binds to 127.0.0.1 by default, which is correct for a desktop or
# `go run` install but unreachable inside a container: a published port forwards
# to the container's external interface, so a loopback-only listener refuses
# every connection from the host while the container still reports healthy.
# Bind all interfaces here and let the `ports:` mapping decide what is exposed
# (use "127.0.0.1:8080:8080" to keep it on the host's loopback).
ENV WEBUI_BIND=0.0.0.0

EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 \
    CMD curl -fsS http://127.0.0.1:8080/healthz || exit 1

ENTRYPOINT ["/app/3270Web"]
