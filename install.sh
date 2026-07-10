#!/usr/bin/env bash
set -euo pipefail

REPO="${PLANMAXX_REPO:-AlhasanIQ/planmaxx}"
VERSION="${PLANMAXX_VERSION:-latest}"
INSTALL_DIR="${PLANMAXX_INSTALL_DIR:-$HOME/.local/bin}"
BASE_URL_OVERRIDE="${PLANMAXX_BASE_URL:-}"
INSTALL_CODEX_SKILL="${PLANMAXX_INSTALL_CODEX_SKILL:-0}"

log() {
  printf '[planmaxx installer] %s\n' "$*"
}

die() {
  printf '[planmaxx installer] ERROR: %s\n' "$*" >&2
  exit 1
}

usage() {
  cat <<'EOF'
Install PlanMaxx from GitHub Releases.

Usage:
  install.sh [options]

Options:
  --version <tag|latest>  Version tag to install (default: latest)
  --install-dir <path>    Binary install directory (default: ~/.local/bin)
  --repo <owner/repo>     GitHub repo (default: AlhasanIQ/planmaxx)
  --install-codex-skill   Also install the optional user-level Codex skill under ~/.agents/skills
  --help                  Show this help

Environment overrides:
  PLANMAXX_REPO
  PLANMAXX_VERSION
  PLANMAXX_INSTALL_DIR
  PLANMAXX_INSTALL_CODEX_SKILL=1
  PLANMAXX_BASE_URL        Override release asset base URL for mirrors or tests
EOF
}

require_cmd() {
  local cmd="$1"
  command -v "$cmd" >/dev/null 2>&1 || die "missing required command: $cmd"
}

download_to_file() {
  local url="$1"
  local dst="$2"
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL "$url" -o "$dst"
    return
  fi
  if command -v wget >/dev/null 2>&1; then
    wget -qO "$dst" "$url"
    return
  fi
  die "curl or wget is required"
}

download_to_stdout() {
  local url="$1"
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL "$url"
    return
  fi
  if command -v wget >/dev/null 2>&1; then
    wget -qO- "$url"
    return
  fi
  die "curl or wget is required"
}

detect_os() {
  local raw
  raw="$(uname -s)"
  case "$raw" in
    Linux) echo "linux" ;;
    Darwin) echo "darwin" ;;
    MINGW*|MSYS*|CYGWIN*) echo "windows" ;;
    *) die "unsupported OS: $raw (supported: linux, darwin, windows via bash)" ;;
  esac
}

detect_arch() {
  local raw
  raw="$(uname -m)"
  case "$raw" in
    x86_64|amd64) echo "amd64" ;;
    arm64|aarch64) echo "arm64" ;;
    *) die "unsupported architecture: $raw (supported: amd64, arm64)" ;;
  esac
}

resolve_version() {
  if [[ "$VERSION" != "latest" ]]; then
    echo "$VERSION"
    return
  fi

  local api_url json tag
  api_url="https://api.github.com/repos/${REPO}/releases/latest"
  json="$(download_to_stdout "$api_url")"
  tag="$(printf '%s\n' "$json" | grep -m1 '"tag_name"' | sed -E 's/.*"tag_name"[[:space:]]*:[[:space:]]*"([^"]+)".*/\1/')"
  [[ -n "$tag" ]] || die "could not resolve latest release tag from ${api_url}"
  echo "$tag"
}

sha256_file() {
  local file="$1"
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$file" | awk '{print $1}'
    return
  fi
  if command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$file" | awk '{print $1}'
    return
  fi
  die "sha256sum or shasum is required for checksum verification"
}

verify_checksum() {
  local archive="$1"
  local checksums="$2"
  local archive_name expected actual
  archive_name="$(basename "$archive")"
  expected="$(awk -v name="$archive_name" '$2 == name || $2 == "*" name { print $1 }' "$checksums" | tail -n1)"
  [[ -n "$expected" ]] || die "checksum for ${archive_name} not found in checksums.txt"
  actual="$(sha256_file "$archive")"
  [[ "$actual" == "$expected" ]] || die "checksum mismatch for ${archive_name}"
}

install_binary() {
  local archive="$1"
  local install_dir="$2"
  local os="$3"
  local tmp_extract bin_name bin_src bin_dst

  tmp_extract="$(mktemp -d "${TMPDIR_PLANMAXX:-/tmp}/extract.XXXXXX")"
  tar -xzf "$archive" -C "$tmp_extract"

  bin_name="planmaxx"
  if [[ "$os" == "windows" ]]; then
    bin_name="planmaxx.exe"
  fi
  bin_src="${tmp_extract}/${bin_name}"
  [[ -f "$bin_src" ]] || die "archive does not contain ${bin_name}"

  mkdir -p "$install_dir"
  bin_dst="${install_dir}/${bin_name}"
  if command -v install >/dev/null 2>&1; then
    install -m 0755 "$bin_src" "$bin_dst"
  else
    cp "$bin_src" "$bin_dst"
    chmod 0755 "$bin_dst"
  fi
  printf '%s\n' "$bin_dst"
}

install_codex_skill() {
  local skill_dir skill_file skill_url

  if "$BIN_PATH" skill install --target codex; then
    return
  fi

  log "Installed binary does not support skill installation; installing the Codex skill directly."
  skill_dir="${HOME}/.agents/skills/planmaxx"
  skill_file="${skill_dir}/SKILL.md"
  skill_url="${BASE_URL}/SKILL.md"

  mkdir -p "$skill_dir"
  download_to_file "$skill_url" "${TMPDIR_PLANMAXX}/SKILL.md"
  verify_checksum "${TMPDIR_PLANMAXX}/SKILL.md" "$CHECKSUMS"
  if command -v install >/dev/null 2>&1; then
    install -m 0644 "${TMPDIR_PLANMAXX}/SKILL.md" "$skill_file"
  else
    cp "${TMPDIR_PLANMAXX}/SKILL.md" "$skill_file"
    chmod 0644 "$skill_file"
  fi
  log "Installed ${skill_file}"
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --version)
      [[ $# -ge 2 ]] || die "missing value for --version"
      VERSION="$2"
      shift 2
      ;;
    --install-dir)
      [[ $# -ge 2 ]] || die "missing value for --install-dir"
      INSTALL_DIR="$2"
      shift 2
      ;;
    --repo)
      [[ $# -ge 2 ]] || die "missing value for --repo"
      REPO="$2"
      shift 2
      ;;
    --install-codex-skill)
      INSTALL_CODEX_SKILL="1"
      shift
      ;;
    --help|-h)
      usage
      exit 0
      ;;
    *)
      die "unknown argument: $1 (run with --help)"
      ;;
  esac
done

require_cmd tar
require_cmd uname

OS="$(detect_os)"
ARCH="$(detect_arch)"
TAG="$(resolve_version)"
VERSION_NO_V="${TAG#v}"
ASSET="planmaxx_${VERSION_NO_V}_${OS}_${ARCH}.tar.gz"
if [[ -n "$BASE_URL_OVERRIDE" ]]; then
  BASE_URL="${BASE_URL_OVERRIDE%/}"
else
  BASE_URL="https://github.com/${REPO}/releases/download/${TAG}"
fi

TMPDIR_PLANMAXX="$(mktemp -d)"
trap 'rm -rf "$TMPDIR_PLANMAXX"' EXIT

ARCHIVE="${TMPDIR_PLANMAXX}/${ASSET}"
CHECKSUMS="${TMPDIR_PLANMAXX}/checksums.txt"

log "Installing ${REPO} ${TAG} for ${OS}/${ARCH}"
download_to_file "${BASE_URL}/${ASSET}" "$ARCHIVE"
download_to_file "${BASE_URL}/checksums.txt" "$CHECKSUMS"
verify_checksum "$ARCHIVE" "$CHECKSUMS"

BIN_PATH="$(install_binary "$ARCHIVE" "$INSTALL_DIR" "$OS")"
log "Installed ${BIN_PATH}"

case ":${PATH}:" in
  *":${INSTALL_DIR}:"*) ;;
  *) log "Add ${INSTALL_DIR} to PATH if your shell cannot find planmaxx." ;;
esac

"$BIN_PATH" version

case "$INSTALL_CODEX_SKILL" in
  1|true|TRUE|yes|YES)
    log "Installing optional user-level Codex skill..."
    install_codex_skill
    ;;
  0|false|FALSE|no|NO|"")
    log "Optional Codex skill not installed. To enable automatic plan review later, run: ${BIN_PATH} skill install --target codex"
    ;;
  *)
    die "invalid PLANMAXX_INSTALL_CODEX_SKILL value: ${INSTALL_CODEX_SKILL} (expected 1/0, true/false, yes/no)"
    ;;
esac
