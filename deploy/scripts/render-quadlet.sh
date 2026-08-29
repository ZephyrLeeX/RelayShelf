#!/bin/sh
set -eu

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
. "$script_dir/common.sh"

[ "$#" -eq 3 ] || die "usage: $0 <fully-qualified-semver-image> <debian-lan-ipv4> <output-file>"
image_ref=$1
listen_address=$2
output_file=$3
validate_image_ref "$image_ref"
printf '%s\n' "$listen_address" | grep -Eq '^([0-9]{1,3}\.){3}[0-9]{1,3}$' || die "listen address must be the Debian VM LAN IPv4 address"
case "$listen_address" in 0.0.0.0|127.*) die "listen address must not be wildcard or loopback" ;; esac

bundle_root=$(CDPATH= cd -- "$script_dir/.." && pwd)
sed \
  -e "s|@@RELAYSHELF_IMAGE@@|$image_ref|g" \
  -e "s|@@RELAYSHELF_LISTEN_ADDRESS@@|$listen_address|g" \
  "$bundle_root/quadlet/relayshelf-app.container.in" >"$output_file"
