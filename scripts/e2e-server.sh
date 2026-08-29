#!/usr/bin/env bash
# Bootstraps the full E2E stack for Playwright: PostgreSQL (docker unless
# E2E_DATABASE_URL points at an existing one), the real Go binary built from
# the working tree, real filesystem storage/staging, and deterministic test
# users. Playwright starts this via webServer.command and probes /health/live.
set -euo pipefail

root_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root_dir"

port="${E2E_PORT:-8099}"
base="http://127.0.0.1:${port}"
container_runtime="${CONTAINER_RUNTIME:-docker}"
container=""
own_postgres=0
storage_root="${E2E_STORAGE_ROOT:-${TMPDIR:-/tmp}/relayshelf-e2e-storage-$$}"
staging_root="${E2E_STAGING_ROOT:-${TMPDIR:-/tmp}/relayshelf-e2e-staging-$$}"
pid=""

cleanup() {
  if [ -n "$pid" ]; then kill "$pid" >/dev/null 2>&1 || true; wait "$pid" 2>/dev/null || true; fi
  if [ "$own_postgres" = "1" ] && [ -n "$container" ]; then "$container_runtime" rm -f "$container" >/dev/null 2>&1 || true; fi
}
trap cleanup EXIT INT TERM

if [ -n "${E2E_DATABASE_URL:-}" ]; then
  database_url="$E2E_DATABASE_URL"
else
  pg_port="${E2E_PG_PORT:-55442}"
  container="relayshelf-e2e-postgres"
  own_postgres=1
  # A previous run may not have finished tearing down yet; reclaim the fixed
  # port deterministically instead of racing it.
  "$container_runtime" rm -f "$container" >/dev/null 2>&1 || true
  "$container_runtime" run --rm -d --name "$container" -e POSTGRES_PASSWORD=test -e POSTGRES_USER=test -e POSTGRES_DB=relayshelf_e2e -p "127.0.0.1:${pg_port}:5432" postgres:17-alpine >/dev/null
  for _ in $(seq 1 60); do
    if "$container_runtime" exec "$container" pg_isready -U test -d relayshelf_e2e >/dev/null 2>&1; then break; fi
    sleep 1
  done
  database_url="postgres://test:test@127.0.0.1:${pg_port}/relayshelf_e2e?sslmode=disable"
fi

mkdir -p "$storage_root" "$staging_root"

binary="${E2E_BINARY:-$root_dir/bin/relayshelf-e2e}"
if [ "${E2E_SKIP_BUILD:-0}" != "1" ] || [ ! -x "$binary" ]; then
  go build -o "$binary" ./cmd/relayshelf
fi

export DATABASE_URL="$database_url"
export APP_ENCRYPTION_KEY="$(head -c 32 /dev/urandom | base64)"
export CSRF_SECRET="$(head -c 32 /dev/urandom | base64)"
export PUBLIC_ORIGIN="$base"
export STORAGE_ROOT="$storage_root"
export STAGING_ROOT="$staging_root"
export LISTEN_ADDR="127.0.0.1:${port}"
# Browser journeys use small files; the VM free-space guard is policy for the
# real deployment, not for a throwaway E2E staging directory.
export STAGING_MIN_FREE_BYTES=0
export STAGING_MIN_FREE_PERCENT=0
export UPLOAD_STAGING_MAX_BYTES=1073741824

"$binary" migrate >/dev/null
go run ./tools/e2eseed -database-url "$database_url" >/dev/null

"$binary" serve &
pid=$!

for _ in $(seq 1 60); do
  if curl -fsS "$base/health/live" >/dev/null 2>&1; then
    echo "e2e stack ready on $base"
    wait "$pid"
    exit $?
  fi
  sleep 1
done

echo "E2E stack failed to become ready" >&2
exit 1
