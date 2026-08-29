#!/usr/bin/env bash
set -euo pipefail

container_runtime="${CONTAINER_RUNTIME:-podman}"
read -r -a go_test_flags <<< "${GO_TEST_FLAGS:--race}"
go_build_tags="${GO_BUILD_TAGS:-integration}"
postgres_test_image="${POSTGRES_TEST_IMAGE:-docker.io/library/postgres:17-alpine}"
container_name="relayshelf-postgres-test-$$"
port="${POSTGRES_TEST_PORT:-55432}"

race_requested=false
for flag in "${go_test_flags[@]}"; do
  case "$flag" in
    -race|-race=true)
      race_requested=true
      ;;
  esac
done

if "$race_requested"; then
  if [ "${CGO_ENABLED:-1}" != 1 ]; then
    echo 'The integration test race detector requires CGO. Unset CGO_ENABLED or set CGO_ENABLED=1, and install a C toolchain (for example, build-essential on Debian).' >&2
    exit 1
  fi

  c_compiler="${CC:-$(CGO_ENABLED=1 go env CC)}"
  read -r c_compiler_command _ <<< "$c_compiler"
  if [ -z "$c_compiler_command" ] || ! command -v "$c_compiler_command" >/dev/null 2>&1; then
    echo "The integration test race detector requires a C compiler, but '${c_compiler_command:-<unset>}' was not found. Install a C toolchain (for example, build-essential on Debian) or set CC to a working compiler." >&2
    exit 1
  fi
fi

cleanup() {
  "$container_runtime" rm -f "$container_name" >/dev/null 2>&1 || true
}
trap cleanup EXIT

"$container_runtime" run --rm -d --name "$container_name" -e POSTGRES_PASSWORD=test -e POSTGRES_USER=test -e POSTGRES_DB=relayshelf_test -p "127.0.0.1:${port}:5432" "$postgres_test_image" >/dev/null

for _ in $(seq 1 30); do
  if "$container_runtime" exec "$container_name" pg_isready -U test -d relayshelf_test >/dev/null 2>&1; then
    if "$race_requested"; then
      CGO_ENABLED=1 DATABASE_URL="postgres://test:test@127.0.0.1:${port}/relayshelf_test?sslmode=disable" go test "${go_test_flags[@]}" -tags="${go_build_tags}" ./internal/...
    else
      DATABASE_URL="postgres://test:test@127.0.0.1:${port}/relayshelf_test?sslmode=disable" go test "${go_test_flags[@]}" -tags="${go_build_tags}" ./internal/...
    fi
    exit 0
  fi
  sleep 1
done

echo 'PostgreSQL test container did not become ready.' >&2
exit 1
