#!/usr/bin/env bash
set -euo pipefail

container_runtime="${CONTAINER_RUNTIME:-podman}"
container_name="relayshelf-postgres-test-$$"
port="${POSTGRES_TEST_PORT:-55432}"

cleanup() {
  "$container_runtime" rm -f "$container_name" >/dev/null 2>&1 || true
}
trap cleanup EXIT

"$container_runtime" run --rm -d --name "$container_name" -e POSTGRES_PASSWORD=test -e POSTGRES_USER=test -e POSTGRES_DB=relayshelf_test -p "127.0.0.1:${port}:5432" postgres:17-alpine >/dev/null

for _ in $(seq 1 30); do
  if "$container_runtime" exec "$container_name" pg_isready -U test -d relayshelf_test >/dev/null 2>&1; then
    DATABASE_URL="postgres://test:test@127.0.0.1:${port}/relayshelf_test?sslmode=disable" go test -tags=integration ./internal/platform/database
    exit 0
  fi
  sleep 1
done

echo 'PostgreSQL test container did not become ready.' >&2
exit 1
