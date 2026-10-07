#!/usr/bin/env bash
# Builds the single Windows executable: the UI is compiled and embedded, then Go
# cross-compiles without cgo. Usage: scripts/build.sh [version]
set -euo pipefail
cd "$(dirname "$0")/.."
version="${1:-$(git describe --tags --always --dirty 2>/dev/null || echo dev)}"
(cd web && npm ci && npm run build)
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath \
  -ldflags "-H windowsgui -s -w -X main.version=${version}" \
  -o bin/ullage.exe ./cmd/ullage
echo "bin/ullage.exe (${version})"
