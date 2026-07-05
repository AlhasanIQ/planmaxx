#!/bin/sh
set -eu

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
TMPDIR_PLANMAXX=$(mktemp -d)
PID=""

cleanup() {
  if [ -n "$PID" ] && kill -0 "$PID" 2>/dev/null; then
    kill "$PID" 2>/dev/null || true
    wait "$PID" 2>/dev/null || true
  fi
  rm -rf "$TMPDIR_PLANMAXX"
}
trap cleanup EXIT INT TERM

PLAN="$TMPDIR_PLANMAXX/real-world-plan.md"
STDOUT_LOG="$TMPDIR_PLANMAXX/stdout.log"
STDERR_LOG="$TMPDIR_PLANMAXX/stderr.log"
HANDOFF="$TMPDIR_PLANMAXX/handoff.md"

cat > "$PLAN" <<'PLAN'
# Billing Export Rollout

## Context

The team needs a safe rollout for a CSV billing export used by finance and
support teams.

## Plan

1. Add the Cobra CLI command.
2. Implement export validation and audit logging.
3. Add review comments for risky migration steps.
4. Finalize the implementation handoff.
PLAN

cd "$ROOT"
go run ./cmd/planmaxx review --no-browser --handoff-out "$HANDOFF" "$PLAN" >"$STDOUT_LOG" 2>"$STDERR_LOG" &
PID=$!

URL=""
i=0
while [ "$i" -lt 200 ]; do
  URL=$(sed -n 's/^PlanMaxx review URL: //p' "$STDERR_LOG" | tail -n 1 || true)
  if [ -n "$URL" ]; then
    break
  fi
  if ! kill -0 "$PID" 2>/dev/null; then
    echo "PlanMaxx exited before printing a URL" >&2
    cat "$STDERR_LOG" >&2 || true
    exit 1
  fi
  i=$((i + 1))
  sleep 0.05
done

if [ -z "$URL" ]; then
  echo "Timed out waiting for PlanMaxx review URL" >&2
  cat "$STDERR_LOG" >&2 || true
  exit 1
fi

THREAD_JSON=$(curl -fsS -H 'Content-Type: application/json' \
  -d '{"anchor":{"startLine":9,"endLine":10},"body":"Validate audit logging before export rollout."}' \
  "$URL/api/threads")
THREAD_ID=$(printf '%s' "$THREAD_JSON" | node -pe 'JSON.parse(require("fs").readFileSync(0, "utf8")).id')

curl -fsS -H 'Content-Type: application/json' \
  -d '{"body":"Finance needs rollback steps in the implementation handoff."}' \
  "$URL/api/threads/$THREAD_ID/reply" >/dev/null

DRAFT_JSON=$(curl -fsS -H 'Content-Type: application/json' -d '{}' "$URL/api/digest/draft")
SUMMARY=$(printf '%s' "$DRAFT_JSON" | node -pe 'JSON.parse(require("fs").readFileSync(0, "utf8")).summary')
FINAL_JSON=$(printf '%s' "$DRAFT_JSON" | node -e '
const fs = require("fs");
const draft = JSON.parse(fs.readFileSync(0, "utf8"));
draft.summary = process.argv[1];
process.stdout.write(JSON.stringify(draft));
' "$SUMMARY")

curl -fsS -H 'Content-Type: application/json' -d "$FINAL_JSON" "$URL/api/finalize" >/dev/null
wait "$PID"
PID=""

grep -q 'Validate audit logging before export rollout.' "$STDOUT_LOG"
cmp -s "$STDOUT_LOG" "$HANDOFF"
printf 'PlanMaxx smoke passed: %s\n' "$URL"
