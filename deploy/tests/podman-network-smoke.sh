#!/bin/sh
set -eu

test_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
bundle_root=$(CDPATH= cd -- "$test_dir/.." && pwd)
. "$bundle_root/scripts/common.sh"

require_root
for command_name in podman curl; do require_command "$command_name"; done

image=${RELAYSHELF_NETWORK_SMOKE_IMAGE:-docker.io/library/busybox:1.37.0}
port=${RELAYSHELF_NETWORK_SMOKE_PORT:-18080}
network=relayshelf-network-smoke-$$
container=relayshelf-network-smoke-$$

case "$port" in *[!0-9]*|'') die "smoke-test port must be numeric" ;; esac
[ "$port" -ge 1024 ] && [ "$port" -le 65535 ] || die "smoke-test port must be between 1024 and 65535"

cleanup() {
  podman rm -f "$container" >/dev/null 2>&1 || true
  podman network rm "$network" >/dev/null 2>&1 || true
}
trap cleanup EXIT HUP INT TERM

podman pull "$image"
podman network create --driver bridge "$network" >/dev/null
[ "$(podman network inspect --format '{{.Internal}}' "$network")" = false ] ||
  die "smoke network unexpectedly has Internal=true"
podman run --detach --name "$container" --network "$network" \
  --publish "127.0.0.1:$port:8080" "$image" httpd -f -p 8080 >/dev/null

attempt=0
while [ "$attempt" -lt 20 ]; do
  if curl --fail --silent --show-error --max-time 2 "http://127.0.0.1:$port/" >/dev/null; then
    echo "Rootful Podman bridge published-port smoke test: PASS"
    exit 0
  fi
  attempt=$((attempt + 1))
  sleep 1
done

die "host could not reach the container through 127.0.0.1:$port"
