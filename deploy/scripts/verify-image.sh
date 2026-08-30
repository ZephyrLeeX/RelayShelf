#!/bin/sh
set -eu

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
. "$script_dir/common.sh"

[ "$#" -eq 1 ] || die "usage: $0 <fully-qualified-semver-image>"
image_ref=$1
validate_image_ref "$image_ref"
runtime=${CONTAINER_RUNTIME:-podman}
require_command "$runtime"

image_user=$($runtime image inspect --format '{{.Config.User}}' "$image_ref")
case "$image_user" in
  nonroot|65532|65532:65532) ;;
  *) die "production image user is not non-root: $image_user" ;;
esac

image_tag=${image_ref##*:}
label_version=$($runtime image inspect --format '{{index .Config.Labels "org.opencontainers.image.version"}}' "$image_ref")
label_revision=$($runtime image inspect --format '{{index .Config.Labels "org.opencontainers.image.revision"}}' "$image_ref")
label_created=$($runtime image inspect --format '{{index .Config.Labels "org.opencontainers.image.created"}}' "$image_ref")
[ "$label_version" = "${image_tag#v}" ] || die "OCI version label does not match image tag"
[ -n "$label_revision" ] && [ "$label_revision" != "unknown" ] || die "OCI revision label is missing"
[ -n "$label_created" ] && [ "$label_created" != "unknown" ] || die "OCI build-time label is missing"

version_output=$($runtime run --rm --entrypoint /relayshelf "$image_ref" version 2>&1)
printf '%s\n' "$version_output"
echo "$version_output" | grep -Fq "RelayShelf ${image_tag#v} (" || die "binary version does not match image tag $image_tag"
echo "$version_output" | grep -q 'commit unknown' && die "image Git commit metadata is unknown"
echo "$version_output" | grep -q 'built unknown' && die "image build time metadata is unknown"

container_id=$($runtime create "$image_ref")
filesystem=$(mktemp)
trap '$runtime rm -f "$container_id" >/dev/null 2>&1 || true; rm -f "$filesystem"' EXIT HUP INT TERM
$runtime export "$container_id" >"$filesystem"
$runtime rm "$container_id" >/dev/null
container_id=

contents=$(tar -tf "$filesystem")
echo "$contents" | grep -Eq '(^|/)relayshelf$' || die "RelayShelf binary missing from runtime image"
if echo "$contents" | grep -Eq '^(\./)?bin/(ba)?sh$'; then
  die "runtime image unexpectedly contains /bin/sh or /bin/bash"
fi
if echo "$contents" | grep -Eq '^(src|usr/local/go|go/bin|usr/bin/node|usr/bin/nodejs|usr/bin/pnpm|node_modules)(/|$)'; then
  die "runtime image contains a compiler, Node/pnpm, or source tree"
fi

echo "RelayShelf production image verification: PASS"
