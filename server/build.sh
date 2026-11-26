#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")"
if [[ "${1:-}" != --web-ready ]]; then
  bun install --cwd ../client/web --frozen-lockfile
  bun run --bun --cwd ../client/web build
fi
mkdir -p build
go build -o build/atrace-server .
