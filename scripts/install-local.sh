#!/usr/bin/env bash
set -euo pipefail

# Install the current checkout for local development. Unlike the release
# installer, this always refreshes the managed Codex skill so its prompt and
# workflow stay in lockstep with the binary being tested.
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
INSTALL_DIR="${PLANMAXX_INSTALL_DIR:-$HOME/.local/bin}"
BIN_NAME="planmaxx"

mkdir -p "$INSTALL_DIR"
"$ROOT/scripts/build-web.sh"

TMP_BIN="$(mktemp "$INSTALL_DIR/.${BIN_NAME}.XXXXXX")"
cleanup() {
  rm -f "$TMP_BIN"
}
trap cleanup EXIT

(
  cd "$ROOT"
  go build -trimpath -o "$TMP_BIN" ./cmd/planmaxx
)
chmod 0755 "$TMP_BIN"
mv -f "$TMP_BIN" "$INSTALL_DIR/$BIN_NAME"
"$INSTALL_DIR/$BIN_NAME" skill install --target codex
"$INSTALL_DIR/$BIN_NAME" version
