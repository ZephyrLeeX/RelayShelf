#!/usr/bin/env bash
# Persistent, production-isolated RelayShelf development stack.
set -euo pipefail

root_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
dev_dir="$root_dir/.local/dev"
storage_dir="$dev_dir/storage"
staging_dir="$dev_dir/staging"
logs_dir="$dev_dir/logs"
bin_dir="$dev_dir/bin"
secrets_file="$dev_dir/secrets.env"
backend_pid_file="$dev_dir/backend.pid"
vite_pid_file="$dev_dir/vite.pid"
vite_mode_file="$dev_dir/vite.mode"
backend_log="$logs_dir/backend.log"
vite_log="$logs_dir/vite.log"

container_runtime="${CONTAINER_RUNTIME:-podman}"
pg_container="relayshelf-dev-postgres"
pg_volume="relayshelf-dev-postgres-data"
pg_user="relayshelf_dev"
pg_password="relayshelf_dev_local_only"
pg_database="relayshelf_dev"
pg_port="55432"
backend_origin="http://127.0.0.1:8080"
database_url="postgres://${pg_user}:${pg_password}@127.0.0.1:${pg_port}/${pg_database}?sslmode=disable"

fail() {
  printf 'RelayShelf Dev Stack: %s\n' "$*" >&2
  exit 1
}

require_command() {
  command -v "$1" >/dev/null 2>&1 || fail "required command not found: $1"
}

prepare_dirs() {
  mkdir -p "$storage_dir" "$staging_dir" "$logs_dir" "$bin_dir"
}

create_secrets() {
  if [ -f "$secrets_file" ]; then
    chmod 600 "$secrets_file" 2>/dev/null || true
    return
  fi
  prepare_dirs
  umask 077
  local app_key csrf_key
  app_key="$(head -c 32 /dev/urandom | base64 | tr -d '\n')"
  csrf_key="$(head -c 32 /dev/urandom | base64 | tr -d '\n')"
  {
    printf 'APP_ENCRYPTION_KEY=%s\n' "$app_key"
    printf 'CSRF_SECRET=%s\n' "$csrf_key"
  } >"$secrets_file"
  chmod 600 "$secrets_file"
  printf 'Created persistent Dev secrets at .local/dev/secrets.env\n'
}

load_secrets() {
  create_secrets
  app_encryption_key=""
  csrf_secret=""
  while IFS= read -r line; do
    local key value
    key="${line%%=*}"
    value="${line#*=}"
    case "$key" in
      APP_ENCRYPTION_KEY) app_encryption_key="$value" ;;
      CSRF_SECRET) csrf_secret="$value" ;;
      ''|'#'*) ;;
      *) fail "unexpected key in .local/dev/secrets.env: $key" ;;
    esac
  done <"$secrets_file"
  [ -n "$app_encryption_key" ] || fail "APP_ENCRYPTION_KEY is missing from Dev secrets"
  [ -n "$csrf_secret" ] || fail "CSRF_SECRET is missing from Dev secrets"
}

container_label() {
  "$container_runtime" inspect --type container --format '{{ index .Config.Labels "com.relayshelf.dev-stack" }}' "$pg_container" 2>/dev/null || true
}

container_exists() {
  "$container_runtime" inspect --type container --format '{{.Name}}' "$pg_container" >/dev/null 2>&1
}

container_running() {
  [ "$("$container_runtime" inspect --type container --format '{{.State.Running}}' "$pg_container" 2>/dev/null || true)" = "true" ]
}

volume_label() {
  "$container_runtime" volume inspect --format '{{ index .Labels "com.relayshelf.dev-stack" }}' "$pg_volume" 2>/dev/null || true
}

assert_owned_container() {
  if container_exists && [ "$(container_label)" != "postgres" ]; then
    fail "container $pg_container exists without the RelayShelf Dev ownership label"
  fi
}

ensure_postgres() {
  require_command "$container_runtime"
  assert_owned_container
  if "$container_runtime" volume inspect "$pg_volume" >/dev/null 2>&1; then
    [ "$(volume_label)" = "postgres-data" ] || fail "volume $pg_volume exists without the RelayShelf Dev ownership label"
  else
    "$container_runtime" volume create --label com.relayshelf.dev-stack=postgres-data "$pg_volume" >/dev/null
  fi
  if ! container_exists; then
    "$container_runtime" run -d \
      --name "$pg_container" \
      --label com.relayshelf.dev-stack=postgres \
      -e "POSTGRES_USER=$pg_user" \
      -e "POSTGRES_PASSWORD=$pg_password" \
      -e "POSTGRES_DB=$pg_database" \
      -p "127.0.0.1:${pg_port}:5432" \
      -v "${pg_volume}:/var/lib/postgresql/data" \
      postgres:17-alpine >/dev/null
  elif ! container_running; then
    "$container_runtime" start "$pg_container" >/dev/null
  fi

  local ready=0
  for _ in $(seq 1 60); do
    if "$container_runtime" exec "$pg_container" pg_isready -U "$pg_user" -d "$pg_database" >/dev/null 2>&1; then
      ready=1
      break
    fi
    sleep 1
  done
  [ "$ready" = "1" ] || fail "PostgreSQL did not become ready"
}

process_start_time() {
  local pid="$1"
  [ -r "/proc/$pid/stat" ] || return 1
  awk '{print $22}' "/proc/$pid/stat"
}

write_pid_file() {
  local file="$1" pid="$2" role="$3" start
  start="$(process_start_time "$pid")" || fail "cannot inspect newly started $role process"
  printf '%s %s %s\n' "$pid" "$start" "$role" >"$file"
}

read_owned_pid() {
  local file="$1" expected_role="$2"
  [ -f "$file" ] || return 1
  local pid recorded_start role current_start args cwd
  read -r pid recorded_start role <"$file" || return 1
  case "$pid" in ''|*[!0-9]*) return 1 ;; esac
  [ "$role" = "$expected_role" ] || return 1
  kill -0 "$pid" >/dev/null 2>&1 || return 1
  current_start="$(process_start_time "$pid" 2>/dev/null || true)"
  [ -n "$current_start" ] && [ "$current_start" = "$recorded_start" ] || return 1
  args="$(tr '\0' ' ' <"/proc/$pid/cmdline" 2>/dev/null || true)"
  cwd="$(readlink "/proc/$pid/cwd" 2>/dev/null || true)"
  [ "$cwd" = "$root_dir" ] || return 1
  case "$expected_role:$args" in
    backend:*"$dev_dir/bin/relayshelf serve"*) ;;
    vite:*"pnpm"*"$root_dir/web"*" dev "*) ;;
    *) return 1 ;;
  esac
  printf '%s\n' "$pid"
}

clean_stale_pid() {
  local file="$1" role="$2"
  if [ -f "$file" ] && ! read_owned_pid "$file" "$role" >/dev/null; then
    rm -f "$file"
  fi
}

stop_process() {
  local file="$1" role="$2" pid
  if pid="$(read_owned_pid "$file" "$role" 2>/dev/null)"; then
    kill -TERM -- "-$pid" >/dev/null 2>&1 || kill -TERM "$pid" >/dev/null 2>&1 || true
    for _ in $(seq 1 30); do
      kill -0 "$pid" >/dev/null 2>&1 || break
      sleep 0.2
    done
    if kill -0 "$pid" >/dev/null 2>&1; then
      kill -KILL -- "-$pid" >/dev/null 2>&1 || kill -KILL "$pid" >/dev/null 2>&1 || true
    fi
  fi
  rm -f "$file"
}

backend_env() {
  env -i \
    PATH="$PATH" \
    HOME="${HOME:-/tmp}" \
    TMPDIR="${TMPDIR:-/tmp}" \
    DATABASE_URL="$database_url" \
    STORAGE_ROOT="$storage_dir" \
    STAGING_ROOT="$staging_dir" \
    APP_ENCRYPTION_KEY="$app_encryption_key" \
    CSRF_SECRET="$csrf_secret" \
    PUBLIC_ORIGIN="$backend_origin" \
    LISTEN_ADDR="127.0.0.1:8080" \
    STAGING_MIN_FREE_BYTES=0 \
    STAGING_MIN_FREE_PERCENT=0 \
    UPLOAD_STAGING_MAX_BYTES=1073741824 \
    "$@"
}

build_and_start_backend() {
  stop_process "$backend_pid_file" backend
  go build -o "$bin_dir/relayshelf" ./cmd/relayshelf
  backend_env "$bin_dir/relayshelf" migrate >/dev/null
  backend_env go run ./tools/e2eseed >/dev/null
  env -i \
    PATH="$PATH" \
    HOME="${HOME:-/tmp}" \
    TMPDIR="${TMPDIR:-/tmp}" \
    DATABASE_URL="$database_url" \
    STORAGE_ROOT="$storage_dir" \
    STAGING_ROOT="$staging_dir" \
    APP_ENCRYPTION_KEY="$app_encryption_key" \
    CSRF_SECRET="$csrf_secret" \
    PUBLIC_ORIGIN="$backend_origin" \
    LISTEN_ADDR="127.0.0.1:8080" \
    STAGING_MIN_FREE_BYTES=0 \
    STAGING_MIN_FREE_PERCENT=0 \
    UPLOAD_STAGING_MAX_BYTES=1073741824 \
    setsid "$bin_dir/relayshelf" serve >>"$backend_log" 2>&1 &
  local pid=$!
  write_pid_file "$backend_pid_file" "$pid" backend
  sleep 0.2
  local ready=0
  for _ in $(seq 1 60); do
    if ! kill -0 "$pid" >/dev/null 2>&1; then
      tail -n 30 "$backend_log" >&2 || true
      fail "backend stopped before becoming ready"
    fi
    if curl -fsS "$backend_origin/health/live" >/dev/null 2>&1; then
      ready=1
      break
    fi
    sleep 1
  done
  [ "$ready" = "1" ] || fail "backend did not become ready; see .local/dev/logs/backend.log"
}

start_vite() {
  local mode="$1" host pid
  case "$mode" in
    local) host="127.0.0.1" ;;
    preview) host="0.0.0.0" ;;
    *) fail "up mode must be local or preview" ;;
  esac

  if [ ! -d "$root_dir/web/node_modules" ]; then
    pnpm --dir "$root_dir/web" install --frozen-lockfile
  fi

  clean_stale_pid "$vite_pid_file" vite
  if [ -f "$vite_pid_file" ] && [ "$(cat "$vite_mode_file" 2>/dev/null || true)" != "$mode" ]; then
    stop_process "$vite_pid_file" vite
  fi
  if ! read_owned_pid "$vite_pid_file" vite >/dev/null 2>&1; then
    : >"$vite_log"
    setsid env -i \
      PATH="$PATH" \
      HOME="${HOME:-/tmp}" \
      TMPDIR="${TMPDIR:-/tmp}" \
      RELAY_SHELF_DEV_API_ORIGIN="$backend_origin" \
      RELAY_SHELF_DEV_ALLOWED_HOSTS="${RELAY_SHELF_DEV_ALLOWED_HOSTS:-}" \
      pnpm --dir "$root_dir/web" dev --host "$host" --port 5173 --strictPort >>"$vite_log" 2>&1 &
    pid=$!
    write_pid_file "$vite_pid_file" "$pid" vite
    printf '%s\n' "$mode" >"$vite_mode_file"
  fi
  pid="$(read_owned_pid "$vite_pid_file" vite)" || fail "cannot verify Vite process ownership"

  local ready=0
  for _ in $(seq 1 60); do
    if ! kill -0 "$pid" >/dev/null 2>&1; then
      tail -n 30 "$vite_log" >&2 || true
      fail "Vite stopped before becoming ready"
    fi
    if curl -fsS http://127.0.0.1:5173/ >/dev/null 2>&1; then
      ready=1
      break
    fi
    sleep 1
  done
  [ "$ready" = "1" ] || fail "Vite did not become ready; see .local/dev/logs/vite.log"
}

print_ready() {
  local mode="$1"
  if [ "$mode" = "preview" ]; then
    cat <<'EOF'
RelayShelf Dev Preview READY

Preview port:
  5173

Frontend listening:
  0.0.0.0:5173

Internal API:
  127.0.0.1:8080

Use the workspace/Codex port preview for port 5173.
EOF
  else
    cat <<'EOF'
RelayShelf Dev Stack READY

Frontend:
  http://127.0.0.1:5173

Backend:
  http://127.0.0.1:8080

Database:
  PostgreSQL 127.0.0.1:55432
  database: relayshelf_dev

Users:
  e2e-alice
  e2e-bob
  e2e-admin

Storage:
  .local/dev/storage

Logs:
  .local/dev/logs/

Stop:
  make dev-down

Reset:
  make dev-reset
EOF
  fi
}

up() {
  local mode="${1:-local}"
  require_command go
  require_command pnpm
  require_command curl
  require_command setsid
  cd "$root_dir"
  prepare_dirs
  load_secrets
  ensure_postgres
  build_and_start_backend
  start_vite "$mode"
  print_ready "$mode"
}

down() {
  prepare_dirs
  stop_process "$vite_pid_file" vite
  rm -f "$vite_mode_file"
  stop_process "$backend_pid_file" backend
  require_command "$container_runtime"
  assert_owned_container
  if container_exists; then
    "$container_runtime" rm -f "$pg_container" >/dev/null
  fi
  printf 'RelayShelf Dev Stack stopped; database volume and .local/dev data were preserved.\n'
}

status() {
  local postgres_status="STOPPED" backend_status="STOPPED" vite_status="STOPPED"
  local backend_health="FAIL" frontend_http="FAIL"
  if command -v "$container_runtime" >/dev/null 2>&1 && container_running; then postgres_status="RUNNING"; fi
  if read_owned_pid "$backend_pid_file" backend >/dev/null 2>&1; then backend_status="RUNNING"; fi
  if read_owned_pid "$vite_pid_file" vite >/dev/null 2>&1; then vite_status="RUNNING"; fi
  if curl -fsS "$backend_origin/health/live" >/dev/null 2>&1; then backend_health="PASS"; fi
  if curl -fsS http://127.0.0.1:5173/ >/dev/null 2>&1; then frontend_http="PASS"; fi
  printf 'PostgreSQL    %s\nBackend       %s\nVite          %s\nBackend health %s\nFrontend HTTP  %s\n' \
    "$postgres_status" "$backend_status" "$vite_status" "$backend_health" "$frontend_http"
}

reset() {
  down
  [ "$dev_dir" = "$root_dir/.local/dev" ] || fail "refusing reset: unexpected Dev path"
  [ -n "$root_dir" ] && [ "$root_dir" != "/" ] || fail "refusing reset: invalid repository root"
  if "$container_runtime" volume inspect "$pg_volume" >/dev/null 2>&1; then
    [ "$(volume_label)" = "postgres-data" ] || fail "volume $pg_volume lacks the RelayShelf Dev ownership label"
    "$container_runtime" volume rm "$pg_volume" >/dev/null
  fi
  rm -rf "$storage_dir" "$staging_dir"
  mkdir -p "$storage_dir" "$staging_dir"
  printf 'RelayShelf Dev data reset; .local/dev/secrets.env was preserved. Run make dev to start clean.\n'
}

case "${1:-}" in
  up) up "${2:-local}" ;;
  down) down ;;
  status) status ;;
  reset) reset ;;
  *) fail "usage: scripts/dev-stack.sh {up [local|preview]|down|status|reset}" ;;
esac
