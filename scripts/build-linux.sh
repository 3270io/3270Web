#!/usr/bin/env bash
set -euo pipefail

# Builds a native Linux/Unix 3270Web executable. Mirrors scripts/build-windows.ps1.
# Usage: ./scripts/build-linux.sh [output-name]
# Honors GOOS/GOARCH from the environment for cross-compiles, e.g.:
#   GOARCH=arm64 ./scripts/build-linux.sh 3270Web-arm64

OUTPUT="${1:-3270Web}"
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

export CGO_ENABLED=0

echo "Building Linux executable (GOOS=${GOOS:-linux} GOARCH=${GOARCH:-$(go env GOARCH)})..."
go build -trimpath -ldflags "-s -w" -o "$OUTPUT" ./cmd/3270Web
echo "Built: $OUTPUT"
