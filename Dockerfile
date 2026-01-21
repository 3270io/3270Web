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
RUN go build -trimpath -ldflags "-s -w" -o /out/3270web ./cmd/3270Web

FROM public.ecr.aws/ubuntu/ubuntu:24.04 AS runtime
WORKDIR /app

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates \
    && rm -rf /var/lib/apt/lists/*

COPY --from=build /out/3270web /usr/local/bin/3270web
COPY web/ ./web/
#COPY webapp/ ./webapp/

EXPOSE 8080

ENTRYPOINT ["/usr/local/bin/3270web"]
