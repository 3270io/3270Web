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
PORT="3270"
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

# Which options this run was actually given.
#
# "Settings you do not pass are carried over" is only implementable if the
# script can tell a value that was typed from a default that merely looks like
# one: --port 3270 and no --port at all produce the same PORT, and only the
# first should override what an existing install is already published on.
COMPOSE_DIR_EXPLICIT=0
PORT_EXPLICIT=0
BIND_EXPLICIT=0
VERSION_EXPLICIT=0
MCP_TOOLS_EXPLICIT=0

# Where /data comes from, and it is the one setting a re-run must never decide
# on its own: point a container that has accounts at a different (or new, or
# empty) source and it comes up in first-run setup with every account still
# sitting in the source it stopped using.
DATA_MOUNT="./data"       # left side of the ":/data" mapping, as written
DATA_DIR_PATH=""          # host directory to create and chown; empty for a volume
DATA_IS_VOLUME=0          # 1 when DATA_MOUNT names a Docker volume
DATA_IS_EXTERNAL=0        # 1 when that volume was not created by this stack
PUBLISH_SPEC=""           # host side of the port mapping, e.g. 127.0.0.1:3270

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
  if [ "$#" -gt 0 ]; then
    step "$@"
  fi
  # Always succeed.
  #
  # This was `[ "$#" -gt 0 ] && step "$@"` as the function's last command, so
  # stopping the spinner with no label -- which is what every failure path
  # does, right before explaining itself -- returned 1. Under `set -e` that
  # ended the script on the spot, so the explanation never printed and a failed
  # `compose up`, a failed pull or a failed download all came out as the same
  # bare "installation did not complete", with the actual reason discarded.
  return 0
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
  # Compose has to talk to the daemon the same way `docker` does.
  #
  # `docker compose version` prints a version without going near the socket, so
  # it succeeds for a user who is not in the docker group -- and the plain
  # "docker compose" that check blessed then failed at `up -d` with a
  # permission error on /var/run/docker.sock, on a host where every other step
  # had just worked through sudo. Carry the prefix that was actually resolved
  # above rather than deciding again from a check that cannot see the problem.
  local prefix=""
  case "$DOCKER" in
    "sudo "*) prefix="sudo " ;;
  esac
  if docker compose version >/dev/null 2>&1 </dev/null; then
    DOCKER_COMPOSE="${prefix}docker compose"
  elif have docker-compose; then
    DOCKER_COMPOSE="${prefix}docker-compose"
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
  # WEBUI_PORT, defaulting to 127.0.0.1:3270. Show the env vars that actually
  # apply them rather than printing a URL the binary would not be serving.
  if [ "$PORT" != "3270" ] || [ "$BIND" != "127.0.0.1" ]; then
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

  # Recreating the container is how this method upgrades, so everything the
  # running one was given has to be read off it first. The data mount above
  # all: `docker rm -f` and a `docker run` pointed somewhere else is exactly
  # the shape of "the installer reset every account".
  DATA_MOUNT="${HOME}/.3270web/data"
  adopt_existing "" ""
  resolve_data_mount
  choose_auth_mode
  resolve_api_token
  assert_data_preserved

  local tag="latest"
  [ "$VERSION" != "latest" ] && tag="$VERSION"
  [ -z "$PUBLISH_SPEC" ] && PUBLISH_SPEC="${BIND}:${PORT}"

  step "image" "${IMAGE}:${tag}"
  step "name" "$CONTAINER_NAME"
  step "listen" "${PUBLISH_SPEC} → 3270"
  step "data" "${DATA_MOUNT} → /data"
  step_auth
  step_api
  printf '\n'

  # On an update the listener on that port is the container about to be
  # replaced, so saying it is taken would be the installer warning about
  # itself.
  if [ "$EXISTING_STACK" -eq 0 ] && port_in_use "$PORT"; then
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
    note "Recreating it is how this method upgrades. Its settings above were"
    note "read off it, and ${DATA_MOUNT} — accounts, tokens, the audit trail —"
    note "is mounted onto the new container unchanged."
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

  if [ "$DATA_IS_VOLUME" -eq 1 ]; then
    step "data" "docker volume ${DATA_MOUNT} (kept)" "ok"
  else
    prepare_data_dir "$DATA_DIR_PATH" || true
  fi

  $DOCKER run -d \
    --name "$CONTAINER_NAME" \
    --restart unless-stopped \
    -p "${PUBLISH_SPEC}:3270" \
    "${env_args[@]}" \
    -v "${DATA_MOUNT}:/data" \
    "${IMAGE}:${tag}" >/dev/null </dev/null
  step "started" "container ${CONTAINER_NAME}" "up"
  step "data" "${DATA_MOUNT} → /data" "ok"
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

# normalize_path <path> — an absolute path with any "." or ".." components
# resolved, so two spellings of one folder compare equal. Anything that is not
# an absolute path (a volume name, a path whose parent does not exist yet) is
# returned untouched.
normalize_path() {
  local p="$1" dir base
  case "$p" in
    /*) : ;;
    *)  printf '%s' "$p"; return 0 ;;
  esac
  dir="$(dirname "$p")"
  base="$(basename "$p")"
  if [ -d "$dir" ]; then
    printf '%s/%s' "$(cd "$dir" && pwd)" "$base"
  else
    printf '%s' "$p"
  fi
}

# resolve_data_mount — work out what DATA_MOUNT means on this host.
#
# A bind mount needs a directory created and handed to the runtime user; a
# named volume needs neither, and must not have a directory of that name
# invented for it beside the stack file.
resolve_data_mount() {
  DATA_IS_VOLUME=0
  case "$DATA_MOUNT" in
    /*)        DATA_DIR_PATH="$DATA_MOUNT" ;;
    "~"/*)     DATA_DIR_PATH="${HOME}/${DATA_MOUNT#"~"/}" ;;
    ./*|../*)  DATA_DIR_PATH="${COMPOSE_DIR}/${DATA_MOUNT#./}" ;;
    *)         DATA_IS_VOLUME=1; DATA_DIR_PATH="" ;;
  esac
}

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
  note "The new stack keeps all of it in ${DATA_MOUNT}."
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

# read_setting <file> <NAME> — the value of NAME in an existing compose file,
# ignoring commented-out lines. Empty when absent.
#
# Both spellings of an environment block are read. The installer writes the
# list form, but a file somebody has edited by hand very often uses the mapping
# form, and a re-run that could not see `AUTH_MODE: local` would write the
# stack back out with accounts commented out -- taking away the sign-in page of
# an instance that had one.
read_setting() {
  [ -r "$1" ] || return 0
  sed -n \
    -e "s/^[[:space:]]*-[[:space:]]*$2=[\"']\\{0,1\\}\\([^\"']*\\)[\"']\\{0,1\\}[[:space:]]*\$/\\1/p" \
    -e "s/^[[:space:]]*$2:[[:space:]]*[\"']\\{0,1\\}\\([^\"']*\\)[\"']\\{0,1\\}[[:space:]]*\$/\\1/p" \
    "$1" | tail -1
}

# read_data_mount <compose-file> — the left side of the ":/data" mapping.
#
# Scanned inside volumes: blocks only, and by string rather than by regex, so
# an absolute host path, a relative one and a named volume all come back as
# written. The legacy ":/app/chaos-runs" entry is not a match and is skipped.
read_data_mount() {
  [ -r "$1" ] || return 0
  awk '
    /^[[:space:]]*volumes:/ { invol = 1; next }
    invol && /^[[:space:]]*-/ {
      line = $0
      sub(/^[[:space:]]*-[[:space:]]*/, "", line)
      sub(/[[:space:]]*$/, "", line)
      gsub(/"/, "", line)
      gsub(/\x27/, "", line)
      n = index(line, ":/data")
      if (n > 0) {
        rest = substr(line, n)
        if (rest == ":/data" || substr(rest, 1, 7) == ":/data:") {
          print substr(line, 1, n - 1)
          exit
        }
      }
    }
  ' "$1"
}

# read_published <compose-file> — the host side of the port mapping, e.g.
# "127.0.0.1:3270" or "3270". The container side is always rewritten to 3270,
# so only the part somebody chose is carried forward.
read_published() {
  [ -r "$1" ] || return 0
  awk '
    /^[[:space:]]*ports:/ { inports = 1; next }
    inports && /^[[:space:]]*-/ {
      line = $0
      sub(/^[[:space:]]*-[[:space:]]*/, "", line)
      sub(/[[:space:]]*$/, "", line)
      gsub(/"/, "", line)
      gsub(/\x27/, "", line)
      if (line ~ /:[0-9]+$/) {
        sub(/:[0-9]+$/, "", line)
        print line
        exit
      }
    }
    inports && /^[[:space:]]*[A-Za-z_]+:/ { inports = 0 }
  ' "$1"
}

# read_image_tag <compose-file> — the tag an existing stack is pinned to.
read_image_tag() {
  [ -r "$1" ] || return 0
  sed -n "s|^[[:space:]]*image:[[:space:]]*${IMAGE}:\\([^[:space:]]*\\)[[:space:]]*\$|\\1|p" "$1" | tail -1
}

# apply_published <spec> — split a host-side mapping into BIND and PORT.
apply_published() {
  local spec="$1"
  [ -n "$spec" ] || return 0
  PUBLISH_SPEC="$spec"
  case "$spec" in
    *:*) BIND="${spec%:*}"; PORT="${spec##*:}" ;;
    *)   BIND=""; PORT="$spec" ;;
  esac
}

# ==========================================================================
# 7c-i. Finding an install that is already here
#
# The installer defaults its project directory to ./3270web *relative to the
# current directory*, and Compose derives its project name from that
# directory's basename. Re-running from somewhere else -- which is the normal
# thing to do when the command came from a docs page -- therefore lands on a
# second, empty ./3270web that Compose still considers the same project: it
# recreates the running container against a brand new data folder, and the
# instance comes back up in first-run setup with every account still sitting in
# the folder it stopped using.
#
# So an existing install is found through Docker, not through the current
# directory.
# ==========================================================================

# container_label <label> — a label on the existing container, if there is one.
container_label() {
  [ -n "$DOCKER" ] || return 0
  local value
  value="$($DOCKER inspect --format "{{index .Config.Labels \"$1\"}}" "$CONTAINER_NAME" 2>/dev/null </dev/null || true)"
  case "$value" in
    "<no value>") value="" ;;
  esac
  printf '%s' "$value"
}

# container_env <NAME> — an environment variable on the existing container.
#
# The inspect is captured before the pipeline rather than piped into sed
# directly: with `set -o pipefail` a failing inspect (no such container, which
# is the ordinary case on a first install) would fail the whole pipeline, and
# under `set -e` that ends the script instead of answering "not set".
container_env() {
  [ -n "$DOCKER" ] || return 0
  local out
  out="$($DOCKER inspect --format '{{range .Config.Env}}{{println .}}{{end}}' "$CONTAINER_NAME" 2>/dev/null </dev/null || true)"
  printf '%s\n' "$out" | sed -n "s/^$1=\\(.*\\)\$/\\1/p" | tail -1
  return 0
}

# container_data_mount — where the existing container gets /data from: a volume
# name, or the host path of a bind mount.
container_data_mount() {
  [ -n "$DOCKER" ] || return 0
  $DOCKER inspect --format \
    '{{range .Mounts}}{{if eq .Destination "/data"}}{{if eq .Type "volume"}}{{.Name}}{{else}}{{.Source}}{{end}}{{end}}{{end}}' \
    "$CONTAINER_NAME" 2>/dev/null </dev/null || true
}

# container_published — the host side of the existing container's mapping.
container_published() {
  [ -n "$DOCKER" ] || return 0
  local spec
  spec="$($DOCKER inspect --format \
    '{{range $p, $c := .HostConfig.PortBindings}}{{range $c}}{{.HostIp}}:{{.HostPort}}{{end}}{{end}}' \
    "$CONTAINER_NAME" 2>/dev/null </dev/null || true)"
  # An unset HostIp means every interface; keep it that way rather than
  # inventing a loopback bind an existing install did not ask for.
  case "$spec" in
    :*) spec="${spec#:}" ;;
  esac
  printf '%s' "$spec"
}

container_exists() {
  [ -n "$DOCKER" ] || return 1
  $DOCKER inspect "$CONTAINER_NAME" >/dev/null 2>&1 </dev/null
}

# locate_existing_stack — the directory of the Compose project already running
# 3270Web, whether or not this run was started anywhere near it.
locate_existing_stack() {
  local dir
  dir="$(container_label com.docker.compose.project.working_dir)"
  [ -n "$dir" ] || return 0
  [ -e "${dir}/docker-compose.yml" ] || return 0
  printf '%s' "$dir"
}

# resolve_compose_dir — decide which directory this run updates.
resolve_compose_dir() {
  local existing
  existing="$(locate_existing_stack)"
  [ -n "$existing" ] || return 0
  [ "$existing" = "$COMPOSE_DIR" ] && return 0

  if [ "$COMPOSE_DIR_EXPLICIT" -eq 0 ]; then
    COMPOSE_DIR="$existing"
    step "found" "an install already running from ${existing}" "ok"
    note "Updating that one. Pass --dir to work on a different stack."
    return 0
  fi

  # --dir was given and points elsewhere. Writing a second stack here would not
  # be a second install: Compose names its project after the directory, so
  # bringing this one up recreates the container that is running -- against
  # whatever ./data this directory has, which is nothing.
  warn "3270Web is already installed at ${existing}."
  note "Bringing up a second stack in ${COMPOSE_DIR} would not run alongside"
  note "it. Compose recreates the container that is already running, against"
  note "this directory's data folder, and the accounts, tokens and saved work"
  note "in ${existing}/data would be left behind on an instance that no longer"
  note "reads them."
  note ""
  note "To update the install that is here:"
  note "  --dir ${existing}"
  note "To move it, stop it and take its data folder with you:"
  note "  cd ${existing} && ${DOCKER_COMPOSE:-docker compose} down"
  note "  cp -a ${existing}/data ${COMPOSE_DIR}/data"
  die "Refusing to recreate the running instance against an empty data folder."
}

# adopt_existing <compose-file> <env-file> — carry a previous run's choices
# forward, so re-running the installer updates an install rather than quietly
# reconfiguring it.
#
# Someone re-running this to take a new image should not lose their accounts
# setting, their port, their pinned version or their API token because they
# typed a shorter command the second time. Anything given explicitly on the
# command line still wins.
adopt_existing() {
  local file="$1" envfile="$2"
  local from_file=0
  [ -e "$file" ] && from_file=1
  if [ "$from_file" -eq 0 ] && ! container_exists; then
    return 0
  fi
  EXISTING_STACK=1

  local previous_auth previous_token previous_tools previous_data previous_publish previous_tag

  previous_auth=""
  previous_token=""
  previous_tools=""
  previous_data=""
  previous_publish=""
  previous_tag=""
  if [ "$from_file" -eq 1 ]; then
    previous_auth="$(read_setting "$file" AUTH_MODE)"
    previous_tools="$(read_setting "$file" MCP_TOOLS)"
    previous_data="$(read_data_mount "$file")"
    previous_publish="$(read_published "$file")"
    previous_tag="$(read_image_tag "$file")"
  fi

  # The running container is the fallback, and for an install made with
  # --method docker it is the only record there is. It is also the truth when
  # the stack file has drifted from what is actually running.
  [ -z "$previous_auth" ] && previous_auth="$(container_env AUTH_MODE)"
  [ -z "$previous_tools" ] && previous_tools="$(container_env MCP_TOOLS)"
  [ -z "$previous_publish" ] && previous_publish="$(container_published)"
  if [ -z "$previous_data" ]; then
    previous_data="$(container_data_mount)"
    # A volume named by the container was not created by this stack file, so
    # Compose must be told to use that exact volume rather than create a
    # project-prefixed one of its own -- which would come up empty.
    case "$previous_data" in
      ""|/*) : ;;
      *)     DATA_IS_EXTERNAL=1 ;;
    esac
  fi

  if [ -z "$AUTH_MODE_ARG" ] && [ -n "$previous_auth" ]; then
    AUTH_MODE_VALUE="$previous_auth"
    AUTH_MODE_ARG="$previous_auth"
  fi
  [ "$MCP_TOOLS_EXPLICIT" -eq 0 ] && [ -n "$previous_tools" ] && MCP_TOOLS="$previous_tools"

  # Where /data comes from. Carried forward verbatim, because this is the
  # setting that turns a re-run into "it reset the admin and every user": the
  # accounts are not gone, the container is simply reading somewhere else.
  if [ -n "$previous_data" ] && [ "$previous_data" != "$DATA_MOUNT" ]; then
    DATA_MOUNT="$previous_data"
    step "kept" "the data folder this install already uses: ${DATA_MOUNT}" "ok"
    # An unnamed volume is one Docker made up when a stack mounted nothing on
    # /data. Naming it in the stack is what keeps the accounts in it reachable,
    # but it is worth saying out loud, because nobody chose that name and a
    # `down -v` would take it away.
    case "$DATA_MOUNT" in
      *[!0-9a-f]*) : ;;
      ????????????????????????????????????????????????????????????????)
        note "That is an unnamed volume Docker created for this instance. The"
        note "accounts are in it, so the stack names it rather than coming up"
        note "empty beside it. To move them somewhere you can back up:"
        note "  docker run --rm -v ${DATA_MOUNT}:/from -v \"\$PWD/data\":/to \\"
        note "    alpine sh -c 'cp -a /from/. /to/'"
        note "then point the volumes: entry at ./data and restart."
        ;;
    esac
  fi

  if [ "$PORT_EXPLICIT" -eq 0 ] && [ "$BIND_EXPLICIT" -eq 0 ] && [ -n "$previous_publish" ]; then
    apply_published "$previous_publish"
  fi

  if [ "$VERSION_EXPLICIT" -eq 0 ] && [ -n "$previous_tag" ] && [ "$previous_tag" != "latest" ]; then
    VERSION="$previous_tag"
    step "kept" "the version this install is pinned to: ${previous_tag}" "ok"
  fi

  # The token lives in .env, never in the stack file. Reusing it matters more
  # than the rest: regenerating one silently breaks every client that has it,
  # and dropping it takes /api/v1 and MCP off an instance that was serving both.
  if [ -z "$API_TOKEN_ARG" ]; then
    if [ -r "$envfile" ]; then
      previous_token="$(sed -n 's/^API_TOKEN=\(.*\)$/\1/p' "$envfile" | tail -1)"
    fi
    [ -z "$previous_token" ] && previous_token="$(container_env API_TOKEN)"
    if [ -n "$previous_token" ]; then
      API_TOKEN_ARG="$previous_token"
      step "kept" "the API token this install already serves" "ok"
    fi
  fi
}

# assert_data_preserved — stop before a start that would read the wrong folder.
#
# The carry-forward above is what keeps an existing install's accounts; this is
# the check that it worked. An instance whose data folder holds accounts must
# never be restarted against a different source, whatever combination of flags
# and files led there.
assert_data_preserved() {
  local running
  running="$(container_data_mount)"
  [ -n "$running" ] || return 0

  # A relative mount in a stack file and an absolute bind source on a container
  # are routinely the same folder, so compare what they resolve to rather than
  # how they are spelled. A volume name resolves to itself.
  local wanted="$DATA_MOUNT"
  [ "$DATA_IS_VOLUME" -eq 0 ] && [ -n "$DATA_DIR_PATH" ] && wanted="$DATA_DIR_PATH"
  [ "$(normalize_path "$running")" = "$(normalize_path "$wanted")" ] && return 0

  warn "This would change where 3270Web reads its accounts from."
  note "running now:  ${running}"
  note "would become: ${DATA_MOUNT}"
  note ""
  note "Accounts, API tokens, the audit trail and everyone's saved work live"
  note "in the first one. Starting against the second brings the instance up"
  note "in first-run setup with all of it still in the folder it left."
  note ""
  note "Point this run at the folder that is in use, or copy it across first."
  die "Refusing to restart 3270Web against a different data folder."
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

  # Before anything is resolved against the current directory: an install that
  # is already running decides which directory this run updates.
  resolve_compose_dir

  local file="${COMPOSE_DIR}/docker-compose.yml"
  local envfile="${COMPOSE_DIR}/.env"
  adopt_existing "$file" "$envfile"
  resolve_data_mount
  choose_auth_mode
  resolve_api_token
  assert_data_preserved

  local tag="latest"
  [ "$VERSION" != "latest" ] && tag="$VERSION"
  [ -z "$PUBLISH_SPEC" ] && PUBLISH_SPEC="${BIND}:${PORT}"

  step "file" "$file"
  step "image" "${IMAGE}:${tag}"
  step "listen" "${PUBLISH_SPEC} → 3270"
  step "compose" "$DOCKER_COMPOSE"
  step "data" "${DATA_MOUNT} → /data"
  step_auth
  step_api
  printf '\n'

  report_data_upgrade "$file"

  # Re-running is the ordinary way to take a new image or change a setting, so
  # it explains what it is about to do rather than asking whether to overwrite
  # a file the person may not remember writing. Nothing in the data folder is
  # touched, and the previous file is kept beside the new one.
  if [ "$EXISTING_STACK" -eq 1 ]; then
    note "Updating the stack already in ${COMPOSE_DIR}."
    note "Settings not given on the command line are carried over, and"
    note "${DATA_MOUNT} — accounts, tokens, the audit trail — is left alone."
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
  if [ "$DATA_IS_VOLUME" -eq 1 ]; then
    step "data" "docker volume ${DATA_MOUNT} (kept)" "ok"
  else
    prepare_data_dir "$DATA_DIR_PATH" || true
  fi

  # An upgrade keeps the old chaos volume mounted where it was for one start.
  # 3270Web migrates anything it finds beside the program into the data
  # directory, so mounting it is what lets those runs move themselves; drop
  # the volume and they would be stranded in it instead.
  local legacy_volume="" volume_decls=""
  if [ "$LEGACY_CHAOS_VOLUME" -eq 1 ]; then
    legacy_volume="
      # UPGRADE: the old chaos-runs volume, mounted for one start so 3270Web
      # can move its contents into the data folder. Delete this line and the
      # entry in the volumes: block at the end of the file once it has, then:
      #   docker volume rm 3270web-chaos
      - 3270web-chaos:/app/chaos-runs"
    volume_decls="${volume_decls}
  # UPGRADE: see the note above; removable once the runs have moved.
  3270web-chaos:"
  fi

  # A data folder that is a Docker volume has to be declared, and declared as
  # external when this stack did not create it: Compose otherwise prefixes the
  # name with the project's and mounts a new, empty volume of its own.
  if [ "$DATA_IS_VOLUME" -eq 1 ]; then
    if [ "$DATA_IS_EXTERNAL" -eq 1 ]; then
      volume_decls="${volume_decls}
  # The volume this instance's accounts, API tokens, audit trail and saved
  # work are already in. External, so Compose uses that exact volume rather
  # than creating a project-prefixed one -- which would come up empty and put
  # the instance back into first-run setup.
  ${DATA_MOUNT}:
    external: true"
    else
      volume_decls="${volume_decls}
  ${DATA_MOUNT}:"
    fi
  fi

  local legacy_volume_decl=""
  if [ -n "$volume_decls" ]; then
    legacy_volume_decl="

volumes:${volume_decls}"
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

  # The file this run replaces is kept beside it. Rewriting somebody's stack
  # is a destructive edit to a file they may have added their own settings to,
  # and a copy costs nothing next to reconstructing one from memory.
  if [ -e "$file" ]; then
    cp -p "$file" "${file}.bak" 2>/dev/null || cp "$file" "${file}.bak" || true
    step "kept" "the previous stack at $(short_path "${file}.bak")" "ok"
  fi

  local ports_note
  if [ "$PUBLISH_SPEC" = "$PORT" ]; then
    ports_note="      # Published on every interface. Prefix a host address --
      # \"127.0.0.1:${PORT}:3270\" -- to keep the terminal on this machine."
  else
    ports_note="      # Bound to ${PUBLISH_SPEC%:*} so the terminal is not exposed to the network
      # by default. Change to \"${PORT}:3270\" to publish on every interface."
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
${ports_note}
      - "${PUBLISH_SPEC}:3270"
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
      # Accounts, API tokens, the audit trail and everyone's saved work. It has
      # to outlive the container: recreating one without this -- which is what
      # pulling a new image does -- takes every account with it, and an
      # instance with AUTH_MODE=local comes back up in first-run setup for
      # whoever reaches it first.
      #
      # Do not repoint this at somewhere else without moving its contents. The
      # accounts are in the folder named here; a container started against a
      # different one does not find them and asks to be set up again.
      #
      # The server runs unprivileged as uid ${RUNTIME_UID} and a bind mount keeps the
      # host's ownership, so the folder belongs to that user:
      #   sudo chown -R ${RUNTIME_UID}:${RUNTIME_GID} ${DATA_MOUNT}
      - ${DATA_MOUNT}:/data
    restart: unless-stopped${legacy_volume_decl}
YAML
  step "wrote" "$file" "ok"

  # The output is kept, not discarded.
  #
  # This used to send both streams to /dev/null and then say "run it yourself
  # to see why" -- which is the installer telling somebody to do the
  # installer's job, at the one moment it holds the answer and they do not.
  # Compose is specific about its failures (a port already bound, a permission
  # error on the socket, an image it cannot pull), and every one of those is
  # actionable if it is on screen.
  # Pulled explicitly rather than left to `up -d`: compose only pulls an image
  # it does not already have, so a re-run against an existing stack — the
  # normal way to take a new build of a floating tag like :latest — would
  # otherwise restart the same cached image under the appearance of an update.
  local pull_log status=0
  pull_log="$(mktemp "${TMPDIR:-/tmp}/3270web-pull.XXXXXX")"
  spin_start "Pulling ${IMAGE}:${tag}"
  (cd "$COMPOSE_DIR" && $DOCKER_COMPOSE pull >"$pull_log" 2>&1 </dev/null) || status=$?
  if [ "$status" -ne 0 ]; then
    spin_stop
    printf '\n'
    warn "${DOCKER_COMPOSE} pull failed (exit ${status}). It said:"
    printf '\n'
    sed -e 's/^/      /' "$pull_log" | tail -n 12 >&2
    printf '\n'
    rm -f "$pull_log"
    die "Could not pull ${IMAGE}:${tag}."
  fi
  rm -f "$pull_log"
  spin_stop "pulled" "${IMAGE}:${tag}" "ok"

  local compose_log status=0
  compose_log="$(mktemp "${TMPDIR:-/tmp}/3270web-compose.XXXXXX")"
  spin_start "Starting the stack"
  (cd "$COMPOSE_DIR" && $DOCKER_COMPOSE up -d >"$compose_log" 2>&1 </dev/null) || status=$?
  if [ "$status" -ne 0 ]; then
    spin_stop
    printf '\n'
    warn "${DOCKER_COMPOSE} up -d failed (exit ${status}). It said:"
    printf '\n'
    # The tail, because compose repeats its progress lines and the reason is
    # the last thing it prints. Indented so it reads as quoted output.
    sed -e 's/^/      /' "$compose_log" | tail -n 12 >&2
    printf '\n'
    note "The stack file is written and unchanged; fix the cause and re-run:"
    note "  cd ${COMPOSE_DIR} && ${DOCKER_COMPOSE} up -d"
    rm -f "$compose_log"
    die "Could not start the stack."
  fi
  rm -f "$compose_log"
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
  --port <port>                     Host port to serve on (default: 3270)
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
      --version) VERSION="${2:-}"; VERSION_EXPLICIT=1; shift 2 ;;
      --version=*) VERSION="${1#*=}"; VERSION_EXPLICIT=1; shift ;;
      --port)    PORT="${2:-}"; PORT_EXPLICIT=1; shift 2 ;;
      --port=*)  PORT="${1#*=}"; PORT_EXPLICIT=1; shift ;;
      --bind)    BIND="${2:-}"; BIND_EXPLICIT=1; shift 2 ;;
      --bind=*)  BIND="${1#*=}"; BIND_EXPLICIT=1; shift ;;
      --dir)     COMPOSE_DIR="${2:-}"; COMPOSE_DIR_EXPLICIT=1; shift 2 ;;
      --dir=*)   COMPOSE_DIR="${1#*=}"; COMPOSE_DIR_EXPLICIT=1; shift ;;
      --api-token)   API_TOKEN_ARG="${2:-}"; shift 2 ;;
      --api-token=*) API_TOKEN_ARG="${1#*=}"; shift ;;
      --mcp-tools)   MCP_TOOLS="${2:-}"; MCP_TOOLS_EXPLICIT=1; shift 2 ;;
      --mcp-tools=*) MCP_TOOLS="${1#*=}"; MCP_TOOLS_EXPLICIT=1; shift ;;
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

  # Absolute from here on. The project directory is compared against the one
  # Docker reports for an install that is already running, and "3270web" and
  # "/home/you/3270web" have to be able to match.
  case "$COMPOSE_DIR" in
    /*) : ;;
    *)  COMPOSE_DIR="${PWD}/${COMPOSE_DIR#./}" ;;
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
