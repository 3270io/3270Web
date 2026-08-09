#!/usr/bin/env bash
#
# 3270Web installer — https://3270Web.3270.io
#
#   curl -fsSL https://3270Web.3270.io/install.sh | bash
#
# Installs 3270Web one of three ways: as a native binary, as a single Docker
# container, or as a Docker Compose stack. Run it with no arguments and it
# asks; pass --method to skip straight through.
#
# The script is deliberately self-contained (no dependencies beyond curl and
# coreutils) and never writes outside the directories it reports.
#
# Copyright (c) 3270.io — MIT licensed.

set -euo pipefail

# ==========================================================================
# Constants
# ==========================================================================

REPO="3270io/3270Web"
IMAGE="ghcr.io/3270io/3270web"
DOCS_URL="https://3270Web.3270.io"
API="https://api.github.com/repos/${REPO}"
CONTAINER_NAME="3270web"

# The release publishes one Linux binary and it is amd64. Everything else is
# steered at the multi-arch image rather than failing late.
BINARY_ASSET="3270Web"

# ==========================================================================
# Options (overridable by flags; see usage)
# ==========================================================================

METHOD=""                 # binary | docker | compose
VERSION="latest"
PORT="8080"
BIND="127.0.0.1"
THEME="grn"
ASSUME_YES=0
USE_COLOR="auto"
SYSTEM_INSTALL="auto"     # auto | yes | no
COMPOSE_DIR="$PWD/3270web"
APP_DIR=""                # resolved during the binary install
BIN_LINK=""               # resolved during the binary install
DRY_RUN=0
API_TOKEN_ARG=""          # --api-token: empty, "auto", or a literal token
API_TOKEN_VALUE=""        # resolved token; empty leaves /api/v1 and MCP off
MCP_TOOLS="interactive"   # --mcp-tools: readonly | interactive | full
AUTH_MODE_ARG=""          # --auth: "", none or local
AUTH_MODE_VALUE="none"    # resolved: none (one operator) or local (accounts)
EXISTING_STACK=0          # 1 when this run is updating an install already here

# ==========================================================================
# 1. Palette
#
# The four palettes are the GRN / AMB / ICE / DAY set from 3270.io, the
# MkDocs sites and the terminal UI itself, so an install reads as the same
# surface as the thing it installs. Truecolor when the terminal advertises
# it, a hand-picked 256-colour approximation otherwise, plain text when
# piped or when NO_COLOR is set.
# ==========================================================================

C_ACCENT=""; C_TEXT=""; C_DIM=""; C_FAINT=""
C_OK=""; C_INFO=""; C_WARN=""; C_DANGER=""
C_BOLD=""; C_RESET=""; C_LINE=""

setup_palette() {
  local mode="none"

  if [ "$USE_COLOR" = "always" ]; then
    mode="truecolor"
  elif [ "$USE_COLOR" = "never" ] || [ -n "${NO_COLOR:-}" ]; then
    mode="none"
  elif [ ! -t 1 ] || [ "${TERM:-dumb}" = "dumb" ]; then
    mode="none"
  elif [ "${COLORTERM:-}" = "truecolor" ] || [ "${COLORTERM:-}" = "24bit" ]; then
    mode="truecolor"
  else
    mode="256"
  fi

  [ "$mode" = "none" ] && return 0

  C_BOLD=$'\033[1m'
  C_RESET=$'\033[0m'

  local rgb_accent rgb_text rgb_dim rgb_faint rgb_ok rgb_info rgb_warn rgb_danger rgb_line
  local i_accent i_text i_dim i_faint i_ok i_info i_warn i_danger i_line

  case "$THEME" in
    amb|amber)
      rgb_accent="255;184;77";  i_accent=215
      rgb_text="255;243;220";   i_text=230
      rgb_dim="233;196;138";    i_dim=180
      rgb_faint="163;129;76";   i_faint=137
      rgb_ok="143;227;136";     i_ok=114
      rgb_info="124;200;255";   i_info=117
      rgb_warn="255;209;102";   i_warn=221
      rgb_danger="255;122;107"; i_danger=209
      rgb_line="122;83;24";     i_line=94
      ;;
    ice)
      rgb_accent="90;210;255";  i_accent=81
      rgb_text="234;245;255";   i_text=195
      rgb_dim="167;201;232";    i_dim=152
      rgb_faint="107;139;171";  i_faint=103
      rgb_ok="78;233;176";      i_ok=86
      rgb_info="90;210;255";    i_info=81
      rgb_warn="255;204;112";   i_warn=221
      rgb_danger="255;122;144"; i_danger=210
      rgb_line="42;80;118";     i_line=60
      ;;
    day|daylight)
      # The light palette. Only sensible on a light terminal background —
      # documented as such rather than silently guessing.
      rgb_accent="0;135;90";    i_accent=29
      rgb_text="8;32;25";       i_text=235
      rgb_dim="61;92;81";       i_dim=239
      rgb_faint="109;133;121";  i_faint=245
      rgb_ok="18;128;90";       i_ok=29
      rgb_info="11;111;164";    i_info=25
      rgb_warn="161;98;7";      i_warn=136
      rgb_danger="192;57;43";   i_danger=160
      rgb_line="164;186;176";   i_line=250
      ;;
    *)  # grn / phosphor — the default
      rgb_accent="78;255;179";  i_accent=86
      rgb_text="230;255;245";   i_text=195
      rgb_dim="159;230;200";    i_dim=151
      rgb_faint="95;158;134";   i_faint=72
      rgb_ok="78;255;179";      i_ok=86
      rgb_info="90;210;255";    i_info=81
      rgb_warn="247;195;107";   i_warn=215
      rgb_danger="255;111;130"; i_danger=204
      rgb_line="29;77;61";      i_line=22
      ;;
  esac

  if [ "$mode" = "truecolor" ]; then
    C_ACCENT=$'\033[38;2;'"${rgb_accent}"'m'
    C_TEXT=$'\033[38;2;'"${rgb_text}"'m'
    C_DIM=$'\033[38;2;'"${rgb_dim}"'m'
    C_FAINT=$'\033[38;2;'"${rgb_faint}"'m'
    C_OK=$'\033[38;2;'"${rgb_ok}"'m'
    C_INFO=$'\033[38;2;'"${rgb_info}"'m'
    C_WARN=$'\033[38;2;'"${rgb_warn}"'m'
    C_DANGER=$'\033[38;2;'"${rgb_danger}"'m'
    C_LINE=$'\033[38;2;'"${rgb_line}"'m'
  else
    C_ACCENT=$'\033[38;5;'"${i_accent}"'m'
    C_TEXT=$'\033[38;5;'"${i_text}"'m'
    C_DIM=$'\033[38;5;'"${i_dim}"'m'
    C_FAINT=$'\033[38;5;'"${i_faint}"'m'
    C_OK=$'\033[38;5;'"${i_ok}"'m'
    C_INFO=$'\033[38;5;'"${i_info}"'m'
    C_WARN=$'\033[38;5;'"${i_warn}"'m'
    C_DANGER=$'\033[38;5;'"${i_danger}"'m'
    C_LINE=$'\033[38;5;'"${i_line}"'m'
  fi
}

# ==========================================================================
# 2. Drawing primitives
#
# The vocabulary is the one on the docs home page: a terminal panel with a
# live dot in its head bar, `›` log lines with an aligned label, a value and
# a bracketed status tag, and mono uppercase eyebrows over each section.
# ==========================================================================

WIDTH=72
GLYPH_DOT="●"; GLYPH_ARROW="›"; GLYPH_BAR="▌"; GLYPH_TICK="✓"; GLYPH_CROSS="✕"
GLYPH_SEP="·"
BOX_TL="╭"; BOX_TR="╮"; BOX_BL="╰"; BOX_BR="╯"; BOX_H="─"; BOX_V="│"

setup_geometry() {
  local cols
  cols="${COLUMNS:-0}"
  if [ "$cols" -le 0 ] 2>/dev/null; then
    cols="$( (tput cols) 2>/dev/null || echo 80)"
  fi
  [ "$cols" -le 0 ] 2>/dev/null && cols=80
  WIDTH=$((cols - 4))
  [ "$WIDTH" -gt 84 ] && WIDTH=84
  [ "$WIDTH" -lt 52 ] && WIDTH=52

  # Fall back to ASCII when the locale cannot promise UTF-8, so the layout
  # degrades to something square rather than to mojibake.
  case "${LC_ALL:-${LC_CTYPE:-${LANG:-}}}" in
    *UTF-8*|*utf8*|*UTF8*|*utf-8*) : ;;
    *)
      GLYPH_DOT="*"; GLYPH_ARROW=">"; GLYPH_BAR="|"; GLYPH_TICK="+"; GLYPH_CROSS="x"
      GLYPH_SEP="-"
      BOX_TL="+"; BOX_TR="+"; BOX_BL="+"; BOX_BR="+"; BOX_H="-"; BOX_V="|"
      ;;
  esac
}

repeat() { # repeat <string> <count>
  local s="$1" n="$2" out=""
  while [ "$n" -gt 0 ]; do out="${out}${s}"; n=$((n - 1)); done
  printf '%s' "$out"
}

# Absolute paths are what the script acts on, but they wrap the card layout
# on a normal terminal. Display them the way a shell prompt would.
short_path() { # short_path <path>
  local p="$1"
  case "$p" in
    "$PWD"/*) printf '.%s' "${p#"$PWD"}"; return 0 ;;
    "$PWD")   printf '.';                 return 0 ;;
  esac
  case "$p" in
    "$HOME"/*) printf '~%s' "${p#"$HOME"}"; return 0 ;;
  esac
  printf '%s' "$p"
}

# Panel head bar: `╭─ ● left ──────── right ─╮` on one line, mirroring the
# .term-head component (live dot, label, right-aligned meta).
panel() { # panel <left> <right>
  local left="$1" right="${2:-}"
  local inner=$((WIDTH - 2))
  local text="  ${GLYPH_DOT} ${left}"
  local pad=$((inner - ${#text} - ${#right} - 2))
  [ "$pad" -lt 1 ] && pad=1
  printf '%s%s%s%s\n' \
    "$C_LINE" "$BOX_TL" "$(repeat "$BOX_H" "$inner")" "${BOX_TR}${C_RESET}"
  printf '%s%s%s  %s%s %s%s%s%s%s%s  %s%s%s\n' \
    "$C_LINE" "$BOX_V" "$C_RESET" \
    "$C_OK" "$GLYPH_DOT" \
    "$C_BOLD$C_TEXT" "$left" "$C_RESET" \
    "$(repeat ' ' "$pad")" \
    "$C_FAINT" "$right" \
    "$C_RESET$C_LINE" "$BOX_V" "$C_RESET"
  printf '%s%s%s%s\n' \
    "$C_LINE" "$BOX_BL" "$(repeat "$BOX_H" "$inner")" "${BOX_BR}${C_RESET}"
}

eyebrow() { # eyebrow <TITLE>
  printf '\n  %s%s%s %s%s%s\n\n' \
    "$C_ACCENT" "$GLYPH_BAR" "$C_RESET" "$C_DIM$C_BOLD" "$1" "$C_RESET"
}

rule() {
  printf '  %s%s%s\n' "$C_LINE" "$(repeat "$BOX_H" $((WIDTH - 2)))" "$C_RESET"
}

# `› label      value      [tag]` — the docs-home log line.
step() { # step <label> <value> [tag] [tagcolor]
  local label="$1" value="${2:-}" tag="${3:-}" tagcolor="${4:-$C_OK}"
  local pad=$((11 - ${#label}))
  [ "$pad" -lt 1 ] && pad=1
  printf '  %s%s%s %s%s%s%s%s%s' \
    "$C_ACCENT" "$GLYPH_ARROW" "$C_RESET" \
    "$C_DIM" "$label" "$C_RESET" "$(repeat ' ' "$pad")" "$C_TEXT" "$value"
  if [ -n "$tag" ]; then
    printf '  %s[%s]%s' "$tagcolor" "$tag" "$C_RESET"
  fi
  printf '%s\n' "$C_RESET"
}

note()  { printf '  %s%s%s\n' "$C_FAINT" "$1" "$C_RESET"; }
info()  { printf '  %s%s%s %s\n' "$C_INFO" "$GLYPH_ARROW" "$C_RESET" "$1"; }
good()  { printf '  %s%s%s %s%s%s\n' "$C_OK" "$GLYPH_TICK" "$C_RESET" "$C_TEXT" "$1" "$C_RESET"; }
warn()  { printf '  %s!%s %s%s%s\n' "$C_WARN" "$C_RESET" "$C_DIM" "$1" "$C_RESET" >&2; }

die() {
  printf '\n  %s%s%s %s%s%s\n\n' \
    "$C_DANGER" "$GLYPH_CROSS" "$C_RESET" "$C_TEXT" "$1" "$C_RESET" >&2
  exit "${2:-1}"
}

banner() {
  printf '\n'
  panel "3270Web installer" "$(uname -s | tr '[:upper:]' '[:lower:]') ${GLYPH_SEP} $(uname -m)"
  printf '\n  %s%sTHE MAINFRAME, IN A BROWSER TAB%s\n' "$C_BOLD" "$C_ACCENT" "$C_RESET"
  printf '  %sNo emulator install. No thick client. One command.%s\n\n' "$C_FAINT" "$C_RESET"
}

# A spinner that stays out of the way: only on a TTY, always cleaned up.
SPIN_PID=""
spin_start() { # spin_start <message>
  [ -t 1 ] || { printf '  %s%s%s %s...\n' "$C_ACCENT" "$GLYPH_ARROW" "$C_RESET" "$1"; return 0; }
  local msg="$1"
  (
    local frames='⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏' i=0 n
    case "$GLYPH_DOT" in '*') frames='|/-\' ;; esac
    n=$(printf '%s' "$frames" | wc -m | tr -d ' ')
    while :; do
      i=$(((i % n) + 1))
      printf '\r  %s%s%s %s%s%s ' \
        "$C_ACCENT" "$(printf '%s' "$frames" | cut -c "$i" 2>/dev/null || printf '%s' "$frames" | head -c1)" \
        "$C_RESET" "$C_DIM" "$msg" "$C_RESET"
      sleep 0.1
    done
  ) 2>/dev/null &
  SPIN_PID=$!
}

spin_stop() { # spin_stop <label> <value> [tag]
  if [ -n "$SPIN_PID" ]; then
    kill "$SPIN_PID" 2>/dev/null || true
    wait "$SPIN_PID" 2>/dev/null || true
    SPIN_PID=""
    [ -t 1 ] && printf '\r%s\r' "$(repeat ' ' "$((WIDTH + 6))")"
  fi
  [ "$#" -gt 0 ] && step "$@"
}

cleanup() {
  local code=$?
  if [ -n "$SPIN_PID" ]; then
    kill "$SPIN_PID" 2>/dev/null || true
    SPIN_PID=""
    [ -t 1 ] && printf '\r%s\r' "$(repeat ' ' "$((WIDTH + 6))")"
  fi
  if [ "$code" -ne 0 ] && [ "$code" -ne 130 ]; then
    printf '\n  %s%s installation did not complete.%s\n' "$C_DANGER" "$GLYPH_CROSS" "$C_RESET" >&2
    printf '  %sTroubleshooting: %s/installation/%s\n\n' "$C_FAINT" "$DOCS_URL" "$C_RESET" >&2
  fi
}
trap cleanup EXIT
trap 'exit 130' INT

# ==========================================================================
# 3. Prompting
#
# Piped into bash, stdin is the script itself — every prompt must come from
# the controlling terminal or not at all.
# ==========================================================================

TTY_OK=0
setup_tty() {
  if [ -r /dev/tty ] && [ -t 1 ]; then TTY_OK=1; else TTY_OK=0; fi
}

ask() { # ask <prompt> <default>  → echoes the answer
  local prompt="$1" default="$2" reply=""
  if [ "$TTY_OK" -eq 0 ] || [ "$ASSUME_YES" -eq 1 ]; then
    printf '%s' "$default"
    return 0
  fi
  printf '  %s%s%s %s %s[%s]%s ' \
    "$C_ACCENT" "$GLYPH_ARROW" "$C_RESET" "$prompt" "$C_FAINT" "$default" "$C_RESET" >&2
  IFS= read -r reply < /dev/tty || reply=""
  [ -z "$reply" ] && reply="$default"
  printf '%s' "$reply"
}

confirm() { # confirm <question>  → 0 yes / 1 no
  local answer
  [ "$ASSUME_YES" -eq 1 ] && return 0
  [ "$TTY_OK" -eq 0 ] && return 0
  answer="$(ask "$1 (y/n)" "y")"
  case "$answer" in [yY]|[yY][eE][sS]) return 0 ;; *) return 1 ;; esac
}

# ==========================================================================
# 4. Environment probing
# ==========================================================================

have() { command -v "$1" >/dev/null 2>&1; }

ARCH=""
detect_arch() {
  case "$(uname -m)" in
    x86_64|amd64)  ARCH="amd64" ;;
    aarch64|arm64) ARCH="arm64" ;;
    *)             ARCH="$(uname -m)" ;;
  esac
}

SUDO=""
resolve_sudo() { # resolve_sudo <dir> → sets SUDO to "" or "sudo"
  local dir="$1"
  SUDO=""
  [ "$(id -u)" -eq 0 ] && return 0
  if [ -w "$dir" ]; then return 0; fi
  if have sudo; then SUDO="sudo"; return 0; fi
  return 1
}

DOCKER=""
DOCKER_COMPOSE=""
detect_docker() {
  DOCKER=""; DOCKER_COMPOSE=""
  have docker || return 0
  # `docker` on PATH is not the same as a reachable daemon.
  if docker info >/dev/null 2>&1 </dev/null; then
    DOCKER="docker"
  elif have sudo && sudo -n docker info >/dev/null 2>&1 </dev/null; then
    DOCKER="sudo docker"
  else
    DOCKER="docker"   # present but the daemon check failed; reported later
  fi
  if docker compose version >/dev/null 2>&1 </dev/null; then
    DOCKER_COMPOSE="docker compose"
  elif have docker-compose; then
    DOCKER_COMPOSE="docker-compose"
  fi
}

docker_ready() {
  [ -n "$DOCKER" ] || return 1
  $DOCKER info >/dev/null 2>&1 </dev/null
}

port_in_use() { # port_in_use <port>
  local port="$1"
  if have ss; then
    ss -ltn 2>/dev/null | grep -qE "[:.]${port}[[:space:]]" && return 0
  elif have netstat; then
    netstat -ltn 2>/dev/null | grep -qE "[:.]${port}[[:space:]]" && return 0
  fi
  return 1
}

# ==========================================================================
# 5. Release metadata
# ==========================================================================

RESOLVED_VERSION=""
DOWNLOAD_URL=""
EXPECTED_SHA=""

resolve_release() {
  local url json
  if [ "$VERSION" = "latest" ]; then
    url="${API}/releases/latest"
  else
    url="${API}/releases/tags/${VERSION}"
  fi

  json="$(curl -fsSL -H 'Accept: application/vnd.github+json' "$url" 2>/dev/null </dev/null || true)"

  if [ -n "$json" ]; then
    RESOLVED_VERSION="$(printf '%s' "$json" \
      | tr ',' '\n' | grep -m1 '"tag_name"' \
      | sed -e 's/.*"tag_name" *: *"//' -e 's/".*//' || true)"
    # The digest sits in the same object as the asset name, so walk the
    # asset list rather than grepping the whole document for a sha.
    EXPECTED_SHA="$(printf '%s' "$json" \
      | tr '{' '\n' \
      | grep '"name" *: *"'"${BINARY_ASSET}"'"' \
      | grep -m1 -o '"digest" *: *"sha256:[0-9a-f]*"' \
      | sed -e 's/.*sha256://' -e 's/"//' || true)"
  fi

  [ -z "$RESOLVED_VERSION" ] && RESOLVED_VERSION="$VERSION"

  if [ "$VERSION" = "latest" ]; then
    DOWNLOAD_URL="https://github.com/${REPO}/releases/latest/download/${BINARY_ASSET}"
  else
    DOWNLOAD_URL="https://github.com/${REPO}/releases/download/${VERSION}/${BINARY_ASSET}"
  fi
}

# ==========================================================================
# 6. Method: native binary
#
# 3270Web keeps its runtime state (.env, 3270Web.log, chaos-runs/) in the
# directory holding the executable, so the binary goes into an application
# directory of its own and a symlink on PATH points at it. Dropping the bare
# binary into /usr/local/bin would have it try to write there on first run.
# ==========================================================================

resolve_binary_paths() {
  local system="$SYSTEM_INSTALL"
  if [ "$system" = "auto" ]; then
    if [ "$(id -u)" -eq 0 ]; then system="yes"; else system="no"; fi
  fi

  if [ "$system" = "yes" ]; then
    APP_DIR="/opt/3270web"
    BIN_LINK="/usr/local/bin/3270web"
  else
    APP_DIR="${XDG_DATA_HOME:-$HOME/.local/share}/3270web"
    BIN_LINK="${HOME}/.local/bin/3270web"
  fi
}

install_binary() {
  eyebrow "INSTALL ${GLYPH_SEP} NATIVE BINARY"

  have curl || die "curl is required to download the release."

  if [ "$ARCH" != "amd64" ]; then
    warn "The published Linux binary is amd64 only; this host is ${ARCH}."
    note "Use --method docker (the image is multi-arch), or build from source."
    die "No ${ARCH} binary is published."
  fi

  resolve_binary_paths

  step "version" "$RESOLVED_VERSION"
  step "app dir" "$(short_path "$APP_DIR")"
  step "command" "$(short_path "$BIN_LINK")"
  printf '\n'

  if ! confirm "Install 3270Web here?"; then
    die "Cancelled." 130
  fi
  printf '\n'

  if [ "$DRY_RUN" -eq 1 ]; then
    step "dry run" "nothing written" "skip" "$C_WARN"
    return 0
  fi

  local parent_app parent_bin
  parent_app="$(dirname "$APP_DIR")"
  parent_bin="$(dirname "$BIN_LINK")"

  resolve_sudo "$parent_app" \
    || die "Cannot write to ${parent_app} and sudo is unavailable. Re-run with --user."
  $SUDO mkdir -p "$APP_DIR"

  local sudo_app="$SUDO"
  resolve_sudo "$parent_bin" \
    || die "Cannot write to ${parent_bin} and sudo is unavailable."
  local sudo_bin="$SUDO"
  $sudo_bin mkdir -p "$parent_bin"

  local upgrade=0
  [ -e "${APP_DIR}/3270Web" ] && upgrade=1

  local tmp
  tmp="$(mktemp -d "${TMPDIR:-/tmp}/3270web-install.XXXXXX")"
  # shellcheck disable=SC2064  # $tmp is intentionally expanded now, not later
  trap "rm -rf '$tmp'; cleanup" EXIT

  spin_start "Downloading ${BINARY_ASSET} ${RESOLVED_VERSION}"
  if ! curl -fsSL --retry 3 --retry-delay 2 -o "${tmp}/${BINARY_ASSET}" "$DOWNLOAD_URL" </dev/null; then
    spin_stop
    die "Download failed: ${DOWNLOAD_URL}"
  fi
  spin_stop "download" "$(du -h "${tmp}/${BINARY_ASSET}" | cut -f1) from GitHub releases" "ok"

  if [ -n "$EXPECTED_SHA" ] && have sha256sum; then
    local actual
    actual="$(sha256sum "${tmp}/${BINARY_ASSET}" | cut -d' ' -f1)"
    if [ "$actual" != "$EXPECTED_SHA" ]; then
      die "Checksum mismatch — refusing to install.
     expected ${EXPECTED_SHA}
     actual   ${actual}"
    fi
    step "checksum" "sha256 verified" "ok"
  else
    step "checksum" "not published for this release" "skip" "$C_WARN"
  fi

  chmod +x "${tmp}/${BINARY_ASSET}"
  # Stage inside APP_DIR and rename into place. Copying straight over the
  # target fails with ETXTBSY when an upgrade runs while 3270Web is still
  # serving; rename only swaps the directory entry, so a running process
  # keeps its old inode and the next start picks up the new build.
  $sudo_app cp "${tmp}/${BINARY_ASSET}" "${APP_DIR}/.3270Web.new"
  $sudo_app chmod 0755 "${APP_DIR}/.3270Web.new"
  $sudo_app mv -f "${APP_DIR}/.3270Web.new" "${APP_DIR}/3270Web"
  step "installed" "$(short_path "${APP_DIR}")/3270Web" "ok"

  $sudo_bin ln -sfn "${APP_DIR}/3270Web" "$BIN_LINK"
  step "linked" "$(short_path "$BIN_LINK")" "ok"

  if [ "$upgrade" -eq 1 ]; then
    step "upgraded" "restart 3270web to run the new build" "note" "$C_INFO"
  fi

  rm -rf "$tmp"
  trap cleanup EXIT

  # Non-amd64 never reaches here, so the bundled s3270 is always present —
  # but say so, because it is the question everyone asks next.
  step "s3270" "bundled in the binary" "ok"

  case ":${PATH}:" in
    *":${parent_bin}:"*) : ;;
    *)
      printf '\n'
      warn "${parent_bin} is not on your PATH."
      note "Add it for this shell:  export PATH=\"${parent_bin}:\$PATH\""
      ;;
  esac

  success_binary
}

success_binary() {
  printf '\n'
  panel "3270Web installed" "${RESOLVED_VERSION} ${GLYPH_SEP} binary"
  printf '\n'
  # --bind/--port describe a published container port, which this method does
  # not have: the native binary takes its listen address from WEBUI_BIND and
  # WEBUI_PORT, defaulting to 127.0.0.1:8080. Show the env vars that actually
  # apply them rather than printing a URL the binary would not be serving.
  if [ "$PORT" != "8080" ] || [ "$BIND" != "127.0.0.1" ]; then
    step "start" "WEBUI_BIND=${BIND} WEBUI_PORT=${PORT} 3270web"
    note "Set them in $(short_path "$APP_DIR")/.env to drop the prefix."
  else
    step "start" "3270web"
  fi
  step "open" "http://localhost:${PORT}"
  step "state" "$(short_path "$APP_DIR")/  (.env, 3270Web.log, chaos-runs/)"
  step "docs" "${DOCS_URL}/installation/"
  printf '\n'
}

# ==========================================================================
# 6b. API token
#
# One variable decides whether a container serves anything but the browser
# UI: with API_TOKEN unset every /api/v1 request answers 503, and MCP over
# HTTP lives under that same prefix. So --api-token is the switch that lets
# an AI client drive the terminal, and leaving it off is a real default
# rather than an oversight -- a published port with no token is a browser
# UI, not an open API.
# ==========================================================================

gen_token() {
  if have openssl; then
    openssl rand -hex 24
  elif [ -r /dev/urandom ] && have od; then
    # No pipe *into* head, so pipefail cannot trip on a SIGPIPE'd reader.
    od -An -tx1 -N24 /dev/urandom | tr -d ' \n'
  else
    die "Cannot generate a token on this host: install openssl, or pass --api-token <value>."
  fi
}

resolve_api_token() {
  case "$API_TOKEN_ARG" in
    "")     API_TOKEN_VALUE="" ;;
    auto)   API_TOKEN_VALUE="$(gen_token)" ;;
    *)      API_TOKEN_VALUE="$API_TOKEN_ARG" ;;
  esac
}

# The line the install panels print for the API, whichever method ran.
step_api() {
  if [ -n "$API_TOKEN_VALUE" ]; then
    step "api" "/api/v1 + MCP ${GLYPH_SEP} tools: ${MCP_TOOLS}" "on"
  else
    step "api" "off ${GLYPH_SEP} --api-token to enable MCP" "n/a" "$C_FAINT"
  fi
}

success_mcp() {
  [ -n "$API_TOKEN_VALUE" ] || return 0
  step "mcp url" "http://localhost:${PORT}/api/v1/mcp"
  step "token" "$API_TOKEN_VALUE"
  step "connect" "claude mcp add --transport http 3270web http://localhost:${PORT}/api/v1/mcp --header \"Authorization: Bearer \$API_TOKEN\""
  step "mcp docs" "${DOCS_URL}/mcp/"
}

# ==========================================================================
# 7. Method: single Docker container
# ==========================================================================

require_docker() {
  detect_docker
  if [ -z "$DOCKER" ]; then
    warn "Docker is not installed on this host."
    note "Install it first:  curl -fsSL https://get.docker.com | sh"
    note "Then re-run this installer, or use --method binary instead."
    die "Docker is required for --method ${METHOD}."
  fi
  if ! docker_ready; then
    warn "Docker is installed but the daemon is not reachable as $(id -un)."
    note "Try:  sudo systemctl start docker"
    note "Or add yourself to the docker group:  sudo usermod -aG docker \$USER"
    die "Cannot talk to the Docker daemon."
  fi
}

install_docker() {
  eyebrow "INSTALL ${GLYPH_SEP} DOCKER"
  require_docker

  local tag="latest"
  [ "$VERSION" != "latest" ] && tag="$VERSION"
  choose_auth_mode
  resolve_api_token

  DATA_DIR_PATH="${DATA_DIR_PATH:-${HOME}/.3270web/data}"

  step "image" "${IMAGE}:${tag}"
  step "name" "$CONTAINER_NAME"
  step "listen" "${BIND}:${PORT} → 8080"
  step "data" "${DATA_DIR_PATH} → /data"
  step_auth
  step_api
  printf '\n'

  if port_in_use "$PORT"; then
    warn "Port ${PORT} already has a listener."
    confirm "Continue anyway?" || die "Cancelled." 130
    printf '\n'
  fi

  if ! confirm "Pull the image and start the container?"; then
    die "Cancelled." 130
  fi
  printf '\n'

  if [ "$DRY_RUN" -eq 1 ]; then
    step "dry run" "nothing pulled or started" "skip" "$C_WARN"
    return 0
  fi

  if $DOCKER ps -a --format '{{.Names}}' 2>/dev/null </dev/null | grep -qx "$CONTAINER_NAME"; then
    warn "A container named ${CONTAINER_NAME} already exists."
    if confirm "Remove and recreate it?"; then
      $DOCKER rm -f "$CONTAINER_NAME" >/dev/null </dev/null
      step "removed" "previous ${CONTAINER_NAME}" "ok"
    else
      die "Cancelled." 130
    fi
  fi

  spin_start "Pulling ${IMAGE}:${tag}"
  if ! $DOCKER pull "${IMAGE}:${tag}" >/dev/null 2>&1 </dev/null; then
    spin_stop
    die "Could not pull ${IMAGE}:${tag}."
  fi
  spin_stop "pulled" "${IMAGE}:${tag}" "ok"

  # Redundant with the image default, but stated explicitly so the container's
  # listen address is visible in `docker inspect` next to the port mapping.
  # Note this cannot rescue a --version pinned before WEBUI_BIND existed: those
  # binaries bind 127.0.0.1 unconditionally and ignore the variable.
  local -a env_args=(-e GIN_MODE=release -e WEBUI_BIND=0.0.0.0)
  if [ -n "$API_TOKEN_VALUE" ]; then
    env_args+=(-e "API_TOKEN=${API_TOKEN_VALUE}" -e "MCP_TOOLS=${MCP_TOOLS}")
  fi
  if [ "$AUTH_MODE_VALUE" = "local" ]; then
    env_args+=(-e "AUTH_MODE=local")
  fi

  prepare_data_dir "$DATA_DIR_PATH" || true

  $DOCKER run -d \
    --name "$CONTAINER_NAME" \
    --restart unless-stopped \
    -p "${BIND}:${PORT}:8080" \
    "${env_args[@]}" \
    -v "${DATA_DIR_PATH}:/data" \
    "${IMAGE}:${tag}" >/dev/null </dev/null
  step "started" "container ${CONTAINER_NAME}" "up"
  step "data" "${DATA_DIR_PATH} → /data" "ok"
  # `docker inspect` and the daemon's process list both show -e values, so say
  # where the token ended up rather than leaving it to be discovered.
  [ -n "$API_TOKEN_VALUE" ] && step "token" "in the container environment" "ok"

  wait_for_health
  success_docker "$tag"
}

success_docker() {
  printf '\n'
  panel "3270Web is running" "${1} ${GLYPH_SEP} docker"
  printf '\n'
  step "open" "http://localhost:${PORT}"
  step "logs" "docker logs -f ${CONTAINER_NAME}"
  step "stop" "docker stop ${CONTAINER_NAME}"
  step "remove" "docker rm -f ${CONTAINER_NAME}"
  step "docs" "${DOCS_URL}/installation/"
  success_mcp
  printf '\n'
}


# ==========================================================================
# 7b. The data folder
# ==========================================================================

# The uid the container runs as. A bind-mounted host folder keeps the host's
# ownership — Docker adjusts a named volume's, but not a bind mount's — so the
# folder has to be handed to this user or the server refuses to start.
readonly RUNTIME_UID=10001
readonly RUNTIME_GID=10001

# prepare_data_dir <dir> — create it, hand it to the runtime user, and say so.
prepare_data_dir() {
  local dir="$1"
  local existed=0
  [ -d "$dir" ] && existed=1

  mkdir -p "$dir" || die "Could not create ${dir}."

  # chown needs root unless the folder is already the right owner, which it is
  # on a re-run. Checked rather than assumed so a second install is quiet.
  local owner
  owner="$(stat -c '%u:%g' "$dir" 2>/dev/null || echo '')"
  if [ "$owner" != "${RUNTIME_UID}:${RUNTIME_GID}" ]; then
    local sudo_cmd=""
    [ "$(id -u)" -ne 0 ] && have sudo && sudo_cmd="sudo"
    if ! $sudo_cmd chown -R "${RUNTIME_UID}:${RUNTIME_GID}" "$dir" 2>/dev/null; then
      warn "Could not give ${dir} to uid ${RUNTIME_UID}."
      note "3270Web runs unprivileged and will refuse to start without it. Run:"
      note "  sudo chown -R ${RUNTIME_UID}:${RUNTIME_GID} ${dir}"
      return 1
    fi
  fi

  if [ "$existed" -eq 1 ]; then
    step "data" "${dir} (kept)" "ok"
  else
    step "data" "${dir} (uid ${RUNTIME_UID})" "new"
  fi
  return 0
}

# report_data_upgrade <file-or-empty> — explain the change to somebody whose
# existing stack predates the data folder.
#
# Older stacks persisted only chaos runs, at a named volume on /app/chaos-runs.
# Everything else — accounts, API tokens, the audit trail, saved tasks — lived
# in the container and went with it on every upgrade. Whoever is upgrading is
# entitled to know that, and to know their chaos runs are about to move.
# LEGACY_CHAOS_VOLUME is set when the stack being replaced used the old
# chaos-only volume, so the new one can keep it mounted for a single start.
LEGACY_CHAOS_VOLUME=0

report_data_upgrade() {
  local previous="$1"
  LEGACY_CHAOS_VOLUME=0
  [ -z "$previous" ] && return 0
  grep -q '3270web-chaos' "$previous" 2>/dev/null || return 0
  LEGACY_CHAOS_VOLUME=1

  printf '\n'
  warn "This stack predates the data folder."
  note "It kept chaos runs in the 3270web-chaos volume and nothing else:"
  note "accounts, API tokens, the audit trail and saved tasks lived inside"
  note "the container, so every upgrade deleted them."
  note ""
  note "The new stack keeps all of it in ${DATA_DIR_PATH}."
  note ""
  note "The old volume stays mounted where it was for one more start, so"
  note "3270Web can move those runs into the folder itself. It says what it"
  note "moved. Afterwards the volume is empty and both it and the two lines"
  note "marked in the file can go:"
  note "  docker volume rm 3270web-chaos"
  printf '\n'
}

# ==========================================================================
# 7c. Accounts, and re-running over an install that is already here
# ==========================================================================

# read_setting <file> <NAME> — the value of NAME= in an existing compose file,
# ignoring commented-out lines. Empty when absent.
read_setting() {
  [ -r "$1" ] || return 0
  sed -n "s/^[[:space:]]*-[[:space:]]*$2=\\(.*\\)$/\\1/p" "$1" | tail -1
}

# adopt_existing <compose-file> <env-file> — carry a previous run's choices
# forward, so re-running the installer updates an install rather than quietly
# reconfiguring it.
#
# Someone re-running this to take a new image should not lose their accounts
# setting, their port, or their API token because they typed a shorter command
# the second time. Anything given explicitly on the command line still wins.
adopt_existing() {
  local file="$1" envfile="$2"
  [ -e "$file" ] || return 0
  EXISTING_STACK=1

  local previous_auth previous_token previous_tools
  previous_auth="$(read_setting "$file" AUTH_MODE)"
  previous_tools="$(read_setting "$file" MCP_TOOLS)"
  if [ -z "$AUTH_MODE_ARG" ] && [ -n "$previous_auth" ]; then
    AUTH_MODE_VALUE="$previous_auth"
    AUTH_MODE_ARG="$previous_auth"
  fi
  [ -n "$previous_tools" ] && [ "$MCP_TOOLS" = "interactive" ] && MCP_TOOLS="$previous_tools"

  # The token lives in .env, never in the stack file. Reusing it matters more
  # than the rest: regenerating one silently breaks every client that has it.
  if [ -z "$API_TOKEN_ARG" ] && [ -r "$envfile" ]; then
    previous_token="$(sed -n 's/^API_TOKEN=\\(.*\\)$/\\1/p' "$envfile" | tail -1)"
    if [ -n "$previous_token" ]; then
      API_TOKEN_ARG="$previous_token"
      step "kept" "the API token already in .env" "ok"
    fi
  fi
}

# choose_auth_mode — decide whether this instance has accounts.
#
# Asked rather than defaulted on, because turning it on is a decision about
# who the instance is for: with it off there is one operator and no sign-in,
# which is right for a laptop and wrong for a shared port.
choose_auth_mode() {
  if [ -n "$AUTH_MODE_ARG" ]; then
    AUTH_MODE_VALUE="$AUTH_MODE_ARG"
  elif [ "$TTY_OK" -eq 0 ] || [ "$ASSUME_YES" -eq 1 ]; then
    AUTH_MODE_VALUE="none"
  else
    printf '\n'
    eyebrow "ACCOUNTS"
    note "Off:  one operator, no sign-in. Right for a laptop."
    note "On:   a sign-in page, an account each, and one person's terminal"
    note "      sessions and saved work kept from another's. Turn it on"
    note "      whenever more than one person can reach the port."
    note "      ${DOCS_URL}/multi-user/"
    printf '\n'
    if confirm "Require a sign-in? (accounts)"; then
      AUTH_MODE_VALUE="local"
    else
      AUTH_MODE_VALUE="none"
    fi
    printf '\n'
  fi

  # A single shared API_TOKEN reaches every account's sessions, so an instance
  # with accounts refuses to start with one set. Catching it here beats
  # writing a stack that will not come up.
  if [ "$AUTH_MODE_VALUE" = "local" ] && [ -n "$API_TOKEN_ARG" ]; then
    warn "--api-token cannot be combined with accounts."
    note "One shared token would reach every account's sessions, so 3270Web"
    note "refuses to start with both. With accounts, each client gets its own:"
    note "  3270Web token add <username> <name>"
    note "Dropping the shared token from this stack."
    printf '\n'
    API_TOKEN_ARG=""
  fi
}

# The line the install panels print for accounts.
step_auth() {
  if [ "$AUTH_MODE_VALUE" = "local" ]; then
    step "accounts" "sign-in required ${GLYPH_SEP} first start prints a setup code" "on"
  else
    step "accounts" "off ${GLYPH_SEP} --auth local to require a sign-in" "n/a" "$C_FAINT"
  fi
}

# What to do next when the instance has accounts and none exist yet.
success_auth() {
  [ "$AUTH_MODE_VALUE" = "local" ] || return 0
  printf '\n'
  step "first run" "open the URL above and create the first administrator"
  step "setup code" "$1"
  step "accounts" "${DOCS_URL}/authentication/"
}

# ==========================================================================
# 8. Method: Docker Compose
# ==========================================================================

install_compose() {
  eyebrow "INSTALL ${GLYPH_SEP} DOCKER COMPOSE"
  require_docker

  if [ -z "$DOCKER_COMPOSE" ]; then
    warn "Docker Compose is not available."
    note "It ships with modern Docker as 'docker compose'."
    note "On older installs: https://docs.docker.com/compose/install/"
    die "Docker Compose is required for --method compose."
  fi

  local tag="latest"
  [ "$VERSION" != "latest" ] && tag="$VERSION"
  local file="${COMPOSE_DIR}/docker-compose.yml"
  local envfile="${COMPOSE_DIR}/.env"
  DATA_DIR_PATH="${COMPOSE_DIR}/data"
  adopt_existing "$file" "$envfile"
  choose_auth_mode
  resolve_api_token

  step "file" "$file"
  step "image" "${IMAGE}:${tag}"
  step "listen" "${BIND}:${PORT} → 8080"
  step "compose" "$DOCKER_COMPOSE"
  step "data" "${DATA_DIR_PATH} → /data"
  step_auth
  step_api
  printf '\n'

  report_data_upgrade "$file"

  # Re-running is the ordinary way to take a new image or change a setting, so
  # it explains what it is about to do rather than asking whether to overwrite
  # a file the person may not remember writing. Nothing in ./data is touched.
  if [ "$EXISTING_STACK" -eq 1 ]; then
    note "Updating the stack already in ${COMPOSE_DIR}."
    note "Settings not given on the command line are carried over, and"
    note "${DATA_DIR_PATH} — accounts, tokens, the audit trail — is left alone."
    printf '\n'
    confirm "Rewrite the stack file and restart?" || die "Cancelled." 130
    printf '\n'
  else
    if ! confirm "Write the stack and bring it up?"; then
      die "Cancelled." 130
    fi
  fi
  printf '\n'

  if [ "$DRY_RUN" -eq 1 ]; then
    step "dry run" "nothing written or started" "skip" "$C_WARN"
    return 0
  fi

  mkdir -p "$COMPOSE_DIR"
  prepare_data_dir "$DATA_DIR_PATH" || true

  # An upgrade keeps the old chaos volume mounted where it was for one start.
  # 3270Web migrates anything it finds beside the program into the data
  # directory, so mounting it is what lets those runs move themselves; drop
  # the volume and they would be stranded in it instead.
  local legacy_volume="" legacy_volume_decl=""
  if [ "$LEGACY_CHAOS_VOLUME" -eq 1 ]; then
    legacy_volume="
      # UPGRADE: the old chaos-runs volume, mounted for one start so 3270Web
      # can move its contents into ./data. Delete this line and the volumes:
      # block at the end of the file once it has, then:
      #   docker volume rm 3270web-chaos
      - 3270web-chaos:/app/chaos-runs"
    legacy_volume_decl="

volumes:
  # UPGRADE: see the note above; removable once the runs have moved.
  3270web-chaos:"
  fi

  # The API block is built here rather than inline so the stack reads the same
  # either way: the variables are always named and always explained, and the
  # only difference is whether they are commented out.
  local auth_env
  if [ "$AUTH_MODE_VALUE" = "local" ]; then
    auth_env="      # Accounts. A sign-in page, an account each, and one person's
      # terminal sessions and saved work kept from another's. The first
      # start prints a setup code to the log for creating the first
      # administrator:  ${DOCKER_COMPOSE} logs ${CONTAINER_NAME}
      # See ${DOCS_URL}/multi-user/
      - AUTH_MODE=local"
  else
    auth_env="      # One operator, no sign-in. Turn on accounts whenever more than one
      # person can reach the port -- re-run the installer with --auth local,
      # or uncomment this and restart.
      # See ${DOCS_URL}/multi-user/
      # - AUTH_MODE=local"
  fi

  local mcp_env
  if [ -n "$API_TOKEN_VALUE" ]; then
    mcp_env="      # Turns on /api/v1 and, under the same prefix, MCP over HTTP at
      # POST /api/v1/mcp -- how an AI client drives this terminal. The
      # value is interpolated from the .env file beside this one, so the
      # stack can be shared and the secret cannot.
      - API_TOKEN=\${API_TOKEN}
      # Tool tier: readonly, interactive or full. See ${DOCS_URL}/mcp/
      - MCP_TOOLS=${MCP_TOOLS}
      # Fence the AI client off from hosts it has no business on:
      # - MCP_ALLOWED_HOSTS=*.test.example.com"
  else
    mcp_env="      # /api/v1 -- and MCP over HTTP at POST /api/v1/mcp, which is how an
      # AI client drives this terminal -- stay off until a token is set:
      # without one every /api/v1 request answers 503. Re-run the
      # installer with --api-token auto, or put a long random value in a
      # .env file beside this one and uncomment these two lines.
      # See ${DOCS_URL}/mcp/
      # - API_TOKEN=\${API_TOKEN}
      # - MCP_TOOLS=interactive"
  fi

  if [ -n "$API_TOKEN_VALUE" ]; then
    # Written before the stack starts, and readable only by this user: the
    # compose file is the thing you commit, the .env is the thing you do not.
    # Created empty and locked down first, so the token is never briefly
    # world-readable on a permissive umask.
    : > "$envfile"
    chmod 600 "$envfile" 2>/dev/null || true
    printf '# 3270Web API token. Read by docker compose for ${API_TOKEN}.\nAPI_TOKEN=%s\n' \
      "$API_TOKEN_VALUE" > "$envfile"
    step "wrote" "${envfile} (0600)" "ok"
  fi

  cat > "$file" <<YAML
# 3270Web — https://3270Web.3270.io
#
# Generated by the 3270Web installer. Edit freely; re-run
# "${DOCKER_COMPOSE} up -d" to apply changes.

services:
  3270web:
    image: ${IMAGE}:${tag}
    container_name: ${CONTAINER_NAME}
    ports:
      # Bound to ${BIND} so the terminal is not exposed to the network by
      # default. Change to "${PORT}:8080" to publish on every interface.
      - "${BIND}:${PORT}:8080"
    environment:
      - GIN_MODE=release
      # Listen on all interfaces *inside* the container. A published port
      # forwards to the container's external interface, so a loopback bind
      # here would be unreachable however the ports are mapped. What the
      # terminal is exposed to is decided by "ports:" above, not by this
      # line -- do not change it to 127.0.0.1 to keep the terminal private.
      - WEBUI_BIND=0.0.0.0
${auth_env}
${mcp_env}
      # Any S3270_* option can be set here, for example:
      # - S3270_MODEL=3279-2-E
      # - S3270_CODE_PAGE=bracket
    volumes:${legacy_volume}
      # Accounts, API tokens, the audit trail and everyone's saved work, in a
      # folder beside this file so it can be backed up and inspected with
      # ordinary tools. It has to outlive the container: recreating one
      # without this -- which is what pulling a new image does -- takes every
      # account with it, and an instance with AUTH_MODE=local comes back up in
      # first-run setup for whoever reaches it first.
      #
      # The server runs unprivileged as uid ${RUNTIME_UID} and a bind mount keeps the
      # host's ownership, so the folder belongs to that user:
      #   sudo chown -R ${RUNTIME_UID}:${RUNTIME_GID} ./data
      - ./data:/data
    restart: unless-stopped${legacy_volume_decl}
YAML
  step "wrote" "$file" "ok"

  spin_start "Starting the stack"
  if ! (cd "$COMPOSE_DIR" && $DOCKER_COMPOSE up -d >/dev/null 2>&1 </dev/null); then
    spin_stop
    die "${DOCKER_COMPOSE} up failed. Run it in ${COMPOSE_DIR} to see why."
  fi
  spin_stop "started" "${DOCKER_COMPOSE} up -d" "up"

  wait_for_health
  success_compose "$tag"
}

success_compose() {
  printf '\n'
  panel "3270Web is running" "${1} ${GLYPH_SEP} compose"
  printf '\n'
  step "open" "http://localhost:${PORT}"
  step "dir" "$COMPOSE_DIR"
  step "status" "${DOCKER_COMPOSE} ps"
  step "logs" "${DOCKER_COMPOSE} logs -f"
  step "stop" "${DOCKER_COMPOSE} down"
  step "docs" "${DOCS_URL}/installation/"
  success_auth "${DOCKER_COMPOSE} logs ${CONTAINER_NAME} | grep 'setup code'"
  success_mcp
  printf '\n'
}

# ==========================================================================
# 9. Health
# ==========================================================================

wait_for_health() {
  local url="http://127.0.0.1:${PORT}/healthz" i=0
  have curl || { step "health" "curl unavailable, skipped" "skip" "$C_WARN"; return 0; }
  spin_start "Waiting for ${url}"
  while [ "$i" -lt 45 ]; do
    if curl -fsS -m 2 "$url" >/dev/null 2>&1 </dev/null; then
      spin_stop "health" "GET /healthz" "ok"
      return 0
    fi
    i=$((i + 1))
    sleep 1
  done
  spin_stop "health" "no response after 45s" "slow" "$C_WARN"
  note "The container may still be starting. Check its logs if the page does not load."
}

# ==========================================================================
# 10. Method chooser — the "pick a door" card list from 3270.io
# ==========================================================================

card() { # card <key> <title> <blurb> <meta>
  printf '   %s%s[%s]%s  %s%s%-15s%s%s\n' \
    "$C_BOLD" "$C_ACCENT" "$1" "$C_RESET" \
    "$C_BOLD" "$C_TEXT" "$2" "$C_RESET" "$3"
  printf '         %s%s %s%s\n' "$C_FAINT" "$BOX_H" "$4" "$C_RESET"
}

choose_method() {
  eyebrow "PICK A DOOR"

  local docker_meta compose_meta
  if [ -n "$DOCKER" ]; then
    docker_meta="docker detected ${GLYPH_SEP} ${IMAGE}"
  else
    docker_meta="docker not found on this host"
  fi
  if [ -n "$DOCKER_COMPOSE" ]; then
    compose_meta="${DOCKER_COMPOSE} ${GLYPH_SEP} writes $(short_path "$COMPOSE_DIR")/docker-compose.yml"
  else
    compose_meta="docker compose not found on this host"
  fi

  local binary_meta
  resolve_binary_paths
  if [ "$ARCH" = "amd64" ]; then
    binary_meta="s3270 bundled ${GLYPH_SEP} installs to $(short_path "$APP_DIR")"
  else
    binary_meta="no ${ARCH} binary is published ${GLYPH_SEP} use Docker instead"
  fi

  card 1 "Binary" \
    "$(printf '%sself-contained, no runtime deps%s' "$C_DIM" "$C_RESET")" \
    "$binary_meta"
  printf '\n'
  card 2 "Docker" \
    "$(printf '%sone container, multi-arch image%s' "$C_DIM" "$C_RESET")" \
    "$docker_meta"
  printf '\n'
  card 3 "Compose" \
    "$(printf '%sa stack you can edit and re-up%s' "$C_DIM" "$C_RESET")" \
    "$compose_meta"
  printf '\n'

  if [ "$TTY_OK" -eq 0 ]; then
    # Non-interactive and no --method: pick the one most likely to work.
    if [ "$ARCH" = "amd64" ]; then METHOD="binary"; else METHOD="docker"; fi
    step "selected" "${METHOD} (non-interactive default)" "auto" "$C_INFO"
    return 0
  fi

  local default="1"
  [ "$ARCH" != "amd64" ] && default="2"

  while :; do
    local reply
    reply="$(ask "Select 1-3" "$default")"
    case "$reply" in
      1|binary)  METHOD="binary";  break ;;
      2|docker)  METHOD="docker";  break ;;
      3|compose) METHOD="compose"; break ;;
      *) warn "Enter 1, 2 or 3." ;;
    esac
  done
}

# ==========================================================================
# 11. Usage & arguments
# ==========================================================================

usage() {
  cat <<EOF
3270Web installer

  curl -fsSL ${DOCS_URL}/install.sh | bash
  curl -fsSL ${DOCS_URL}/install.sh | bash -s -- --method docker --yes

Options
  --method <binary|docker|compose>  Installation method (default: ask)
  --version <tag>                   Release tag, e.g. v0.3.2 (default: latest)
  --port <port>                     Host port to serve on (default: 8080)
  --bind <address>                  Host interface to bind (default: 127.0.0.1)
  --dir <path>                      Compose project directory
                                    (default: ./3270web)
  --api-token <value|auto>          Enable /api/v1 and MCP over HTTP with this
                                    token; "auto" generates one (docker and
                                    compose methods; default: off). Not valid
                                    with --auth local, which uses a token per
                                    account instead
  --auth <none|local>               none: one operator, no sign-in (default).
                                    local: accounts, sign-in page, per-user
                                    separation. Ask when not given
  --mcp-tools <readonly|interactive|full>
                                    MCP tool tier (default: interactive)
  --system                          Binary install to /opt + /usr/local/bin
  --user                            Binary install under \$HOME (default)
  --theme <grn|amb|ice|day>         Installer colour palette (default: grn)
  --no-color                        Disable colour output
  --color                           Force colour output
  --yes, -y                         Accept every prompt (non-interactive)
  --dry-run                         Show what would happen, change nothing
  --help, -h                        This text

Documentation
  ${DOCS_URL}/installation/
EOF
}

parse_args() {
  while [ "$#" -gt 0 ]; do
    case "$1" in
      --method)  METHOD="${2:-}"; shift 2 ;;
      --method=*) METHOD="${1#*=}"; shift ;;
      --version) VERSION="${2:-}"; shift 2 ;;
      --version=*) VERSION="${1#*=}"; shift ;;
      --port)    PORT="${2:-}"; shift 2 ;;
      --port=*)  PORT="${1#*=}"; shift ;;
      --bind)    BIND="${2:-}"; shift 2 ;;
      --bind=*)  BIND="${1#*=}"; shift ;;
      --dir)     COMPOSE_DIR="${2:-}"; shift 2 ;;
      --dir=*)   COMPOSE_DIR="${1#*=}"; shift ;;
      --api-token)   API_TOKEN_ARG="${2:-}"; shift 2 ;;
      --api-token=*) API_TOKEN_ARG="${1#*=}"; shift ;;
      --mcp-tools)   MCP_TOOLS="${2:-}"; shift 2 ;;
      --mcp-tools=*) MCP_TOOLS="${1#*=}"; shift ;;
      --auth)    AUTH_MODE_ARG="${2:-}"; shift 2 ;;
      --auth=*)  AUTH_MODE_ARG="${1#*=}"; shift ;;
      --theme)   THEME="${2:-}"; shift 2 ;;
      --theme=*) THEME="${1#*=}"; shift ;;
      --system)  SYSTEM_INSTALL="yes"; shift ;;
      --user)    SYSTEM_INSTALL="no"; shift ;;
      --no-color|--no-colour) USE_COLOR="never"; shift ;;
      --color|--colour)       USE_COLOR="always"; shift ;;
      -y|--yes)  ASSUME_YES=1; shift ;;
      --dry-run) DRY_RUN=1; shift ;;
      -h|--help) usage; exit 0 ;;
      *) printf 'Unknown option: %s\n\n' "$1" >&2; usage >&2; exit 2 ;;
    esac
  done

  case "$METHOD" in
    ""|binary|docker|compose) : ;;
    *) printf 'Unknown --method: %s (want binary, docker or compose)\n' "$METHOD" >&2; exit 2 ;;
  esac

  case "$AUTH_MODE_ARG" in
    ""|none|local) : ;;
    *) printf 'Unknown --auth: %s (want none or local)\n' "$AUTH_MODE_ARG" >&2; exit 2 ;;
  esac

  case "$PORT" in
    ''|*[!0-9]*) printf 'Invalid --port: %s\n' "$PORT" >&2; exit 2 ;;
  esac

  case "$MCP_TOOLS" in
    readonly|interactive|full) : ;;
    *) printf 'Unknown --mcp-tools: %s (want readonly, interactive or full)\n' "$MCP_TOOLS" >&2; exit 2 ;;
  esac

  # The binary install writes its own .env and 3270Web mcp drives it over
  # stdio, so a token there would be answering a question nobody asked.
  if [ -n "$API_TOKEN_ARG" ] && [ "$METHOD" = "binary" ]; then
    printf -- '--api-token applies to the docker and compose methods; the binary install configures API_TOKEN in its own .env\n' >&2
    exit 2
  fi
}

# ==========================================================================
# 12. Main
# ==========================================================================

main() {
  parse_args "$@"
  setup_geometry
  setup_palette
  setup_tty
  detect_arch

  banner

  [ "$(uname -s)" = "Linux" ] || warn "This installer targets Linux; $(uname -s) is untested."

  eyebrow "PREFLIGHT"
  step "host" "$(uname -s) $(uname -m)" "ok"
  have curl && step "curl" "$(command -v curl)" "ok" || step "curl" "not found" "missing" "$C_WARN"

  detect_docker
  if [ -n "$DOCKER" ] && docker_ready; then
    step "docker" "$($DOCKER --version 2>/dev/null </dev/null | head -n1)" "ok"
  elif [ -n "$DOCKER" ]; then
    step "docker" "installed, daemon unreachable" "warn" "$C_WARN"
  else
    step "docker" "not installed" "n/a" "$C_FAINT"
  fi
  [ -n "$DOCKER_COMPOSE" ] \
    && step "compose" "$DOCKER_COMPOSE" "ok" \
    || step "compose" "not installed" "n/a" "$C_FAINT"

  if [ "$METHOD" = "binary" ] || [ -z "$METHOD" ]; then
    resolve_release
    step "release" "$RESOLVED_VERSION" "ok"
  fi

  [ -z "$METHOD" ] && choose_method

  # --method binary given directly still needs the release metadata.
  [ "$METHOD" = "binary" ] && [ -z "$RESOLVED_VERSION" ] && resolve_release

  case "$METHOD" in
    binary)  install_binary ;;
    docker)  install_docker ;;
    compose) install_compose ;;
  esac
}

main "$@"
