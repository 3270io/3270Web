#!/usr/bin/env bash
#
# A Docker stand-in for testing docs/install.sh without a daemon.
#
# It keeps one container's worth of state in $FAKE_DOCKER_STATE and answers the
# subcommands and --format templates the installer actually uses: enough for a
# test to install, then re-run the installer over the result and check that the
# instance still reads its accounts from the same place.
#
# Used by install_rerun_test.go. Not shipped.

set -u

STATE="${FAKE_DOCKER_STATE:?FAKE_DOCKER_STATE is not set}"
[ -e "$STATE" ] || : > "$STATE"

get() { sed -n "s/^$1=\(.*\)$/\1/p" "$STATE" | tail -1; }

put() {
  local key="$1" value="$2" tmp
  tmp="$(mktemp)"
  grep -v "^${key}=" "$STATE" > "$tmp" 2>/dev/null || true
  printf '%s=%s\n' "$key" "$value" >> "$tmp"
  mv "$tmp" "$STATE"
}

exists() { [ "$(get EXISTS)" = "1" ]; }

# record_from_compose <dir> — read the generated stack the way the daemon would
# when `compose up` creates the container from it.
record_from_compose() {
  local dir="$1" file="${1}/docker-compose.yml"
  [ -r "$file" ] || return 0

  local data ports image env_lines token
  data="$(awk '
    /^[[:space:]]*volumes:/ { invol = 1; next }
    invol && /^[[:space:]]*-/ {
      line = $0
      sub(/^[[:space:]]*-[[:space:]]*/, "", line)
      sub(/[[:space:]]*$/, "", line)
      gsub(/"/, "", line)
      n = index(line, ":/data")
      if (n > 0 && substr(line, n) == ":/data") { print substr(line, 1, n - 1); exit }
    }' "$file")"
  ports="$(awk '
    /^[[:space:]]*ports:/ { inports = 1; next }
    inports && /^[[:space:]]*-/ {
      line = $0
      sub(/^[[:space:]]*-[[:space:]]*/, "", line)
      sub(/[[:space:]]*$/, "", line)
      gsub(/"/, "", line)
      if (line ~ /:[0-9]+$/) { sub(/:[0-9]+$/, "", line); print line; exit }
    }' "$file")"
  image="$(sed -n 's|^[[:space:]]*image:[[:space:]]*\(.*\)$|\1|p' "$file" | tail -1)"

  # The daemon stores a bind mount's source resolved, not as the stack spells it.
  case "$data" in
    ./*|../*) data="${dir}/${data#./}" ;;
  esac

  env_lines="$(sed -n 's/^[[:space:]]*-[[:space:]]*\([A-Z_][A-Z_0-9]*=.*\)$/\1/p' "$file" | tr '\n' '\036')"
  token=""
  [ -r "${dir}/.env" ] && token="$(sed -n 's/^API_TOKEN=\(.*\)$/\1/p' "${dir}/.env" | tail -1)"
  env_lines="$(printf '%s' "$env_lines" | sed "s|\${API_TOKEN}|${token}|")"

  put EXISTS 1
  put WORKDIR "$dir"
  put DATA "$data"
  put PORTS "$ports"
  put IMAGE "$image"
  put ENV "$env_lines"
}

format_answer() { # format_answer <go-template>
  case "$1" in
    *com.docker.compose.project.working_dir*) printf '%s\n' "$(get WORKDIR)" ;;
    *.Config.Env*) printf '%s' "$(get ENV)" | tr '\036' '\n' ;;
    *.Mounts*)     printf '%s\n' "$(get DATA)" ;;
    *PortBindings*) printf '%s\n' "$(get PORTS)" ;;
    *) printf '\n' ;;
  esac
}

cmd="${1:-}"; shift || true

case "$cmd" in
  info|version|--version)
    echo "Docker version 0.0.0-fake, build fake"
    ;;
  ps)
    exists && echo "3270web"
    ;;
  pull)
    ;;
  rm)
    put EXISTS 0
    ;;
  run)
    data=""; ports=""; image=""; env_lines=""
    while [ "$#" -gt 0 ]; do
      case "$1" in
        -v) data="${2%%:/data}"; shift 2 ;;
        -p) ports="${2%:3270}"; shift 2 ;;
        -e) env_lines="${env_lines}${2}"$'\036'; shift 2 ;;
        -d) shift ;;
        --name|--restart) shift 2 ;;
        *) image="$1"; shift ;;
      esac
    done
    put EXISTS 1
    put WORKDIR ""
    put DATA "$data"
    put PORTS "$ports"
    put IMAGE "$image"
    put ENV "$env_lines"
    ;;
  inspect)
    fmt=""
    while [ "$#" -gt 0 ]; do
      case "$1" in
        --format)   fmt="$2"; shift 2 ;;
        --format=*) fmt="${1#*=}"; shift ;;
        *) shift ;;
      esac
    done
    exists || exit 1
    [ -n "$fmt" ] && format_answer "$fmt"
    ;;
  compose)
    sub="${1:-}"; shift || true
    case "$sub" in
      version) echo "Docker Compose version v0.0.0-fake" ;;
      up)      record_from_compose "$PWD" ;;
      down)    put EXISTS 0 ;;
      *)       ;;
    esac
    ;;
esac

exit 0
