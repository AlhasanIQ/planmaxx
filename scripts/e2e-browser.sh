#!/bin/sh
set -eu

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$ROOT"
if ! command -v bun >/dev/null 2>&1; then
  echo "browser E2E requires Bun on PATH" >&2
  exit 1
fi
if [ ! -f web/node_modules/playwright/index.mjs ]; then
  echo "browser E2E requires web dependencies; run: cd web && bun install" >&2
  exit 1
fi
./scripts/build-web.sh
PLANMAXX_BROWSER_E2E=1 go test ./internal/e2e -run TestBrowserDiffRegression -count=1
