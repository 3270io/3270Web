#!/usr/bin/env bash
set -euo pipefail

# Builds a native Linux 3270Web executable. Mirrors scripts/build-windows.ps1.
# Usage: ./scripts/build-linux.sh [output-name]
# Defaults to GOOS=linux, but still honors explicit GOOS/GOARCH overrides for
# cross-compiles, e.g.:
#   GOARCH=arm64 ./scripts/build-linux.sh 3270Web-arm64

OUTPUT="${1:-3270Web}"
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

export CGO_ENABLED=0
export GOOS="${GOOS:-linux}"

echo "Building Linux executable (GOOS=${GOOS:-linux} GOARCH=${GOARCH:-$(go env GOARCH)})..."
go build -trimpath -ldflags "-s -w" -o "$OUTPUT" ./cmd/3270Web
echo "Built: $OUTPUT"