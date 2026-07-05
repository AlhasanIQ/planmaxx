#!/usr/bin/env bash
set -euo pipefail

# Build the React review UI and emit it into internal/review/static/ so the
# Go binary can serve it via go:embed.

here="$(cd "$(dirname "$0")" && pwd)"
root="$(cd "$here/.." && pwd)"
cd "$root/web"

if [ ! -d node_modules ]; then
  echo "Installing web dependencies (bun install)…"
  bun install
fi

echo "Building review UI…"
bun run build
