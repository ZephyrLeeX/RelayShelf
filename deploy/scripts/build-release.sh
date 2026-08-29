#!/bin/sh
set -eu

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
bundle_root=$(CDPATH= cd -- "$script_dir/.." && pwd)
repo_root=$(CDPATH= cd -- "$bundle_root/.." && pwd)
. "$script_dir/common.sh"

[ "$#" -eq 3 ] || die "usage: $0 <semver> <fully-qualified-image> <output-tar.gz>"
version=$1
image_ref=$2
output=$3
validate_image_ref "$image_ref"
image_tag=${image_ref##*:}
[ "${image_tag#v}" = "${version#v}" ] || die "version $version and image tag $image_tag must match"

require_command podman
require_command git
require_command tar
git_commit=$(git -C "$repo_root" rev-parse HEAD)
[ -z "$(git -C "$repo_root" status --short)" ] || die "release builds require a clean working tree"
build_time=$(date -u +%Y-%m-%dT%H:%M:%SZ)

podman build \
  --build-arg "VERSION=${version#v}" \
  --build-arg "GIT_COMMIT=$git_commit" \
  --build-arg "BUILD_TIME=$build_time" \
  --tag "$image_ref" "$repo_root"

metadata=$(podman run --rm --entrypoint /relayshelf "$image_ref" version 2>&1)
echo "$metadata" | grep -Fq "RelayShelf ${version#v} (commit $git_commit, built $build_time)" || die "image metadata verification failed"

stage=$(mktemp -d)
trap 'rm -rf "$stage"' EXIT HUP INT TERM
cp -R "$bundle_root" "$stage/relayshelf-deploy-${version#v}"
cat >"$stage/relayshelf-deploy-${version#v}/RELEASE-METADATA" <<EOF
VERSION=${version#v}
GIT_COMMIT=$git_commit
BUILD_TIME=$build_time
IMAGE=$image_ref
EOF
tar -C "$stage" -czf "$output" "relayshelf-deploy-${version#v}"
echo "Release bundle created: $output"

