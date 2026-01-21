# syntax=docker/dockerfile:1

FROM public.ecr.aws/docker/library/node:20-alpine AS frontend-builder
WORKDIR /frontend

FROM golang:1.22-bookworm AS build
WORKDIR /src

ENV CGO_ENABLED=0
ENV GOTOOLCHAIN=auto

COPY go.mod go.sum ./
RUN go mod download

COPY . ./
RUN go build -trimpath -ldflags "-s -w" -o /out/h3270-web ./cmd/h3270-web

FROM public.ecr.aws/ubuntu/ubuntu:24.04 AS runtime
WORKDIR /app

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates \
    && rm -rf /var/lib/apt/lists/*

COPY --from=build /out/h3270-web /usr/local/bin/h3270-web
COPY web/ ./web/
#COPY webapp/ ./webapp/

EXPOSE 8080

ENTRYPOINT ["/usr/local/bin/h3270-web"]
