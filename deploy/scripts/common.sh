#!/bin/sh

die() {
  echo "ERROR: $*" >&2
  exit 1
}

require_command() {
  command -v "$1" >/dev/null 2>&1 || die "required command not found: $1"
}

require_root() {
  [ "$(id -u)" -eq 0 ] || die "this command must run as root"
}

validate_image_ref() {
  image_ref=$1
  case "$image_ref" in
    *[!A-Za-z0-9._/:+-]*|'') die "invalid image reference: $image_ref" ;;
  esac
  case "$image_ref" in
    */*) ;;
    *) die "image reference must include an explicit registry and repository: $image_ref" ;;
  esac
  registry=${image_ref%%/*}
  case "$registry" in
    *.*|*:*|localhost) ;;
    *) die "image reference must be fully qualified: $image_ref" ;;
  esac
  image_tail=${image_ref##*/}
  case "$image_tail" in
    *:*) image_tag=${image_tail##*:} ;;
    *) die "image reference must use an explicit SemVer tag: $image_ref" ;;
  esac
  [ "$image_tag" != "latest" ] || die "latest is forbidden in production image references"
  printf '%s\n' "$image_tag" | grep -Eq '^v?[0-9]+\.[0-9]+\.[0-9]+([+-][0-9A-Za-z.-]+)?$' ||
    die "RelayShelf image tag must be SemVer: $image_tag"
}

require_secure_file() {
  secure_file=$1
  [ -f "$secure_file" ] || die "required file not found: $secure_file"
  secure_mode=$(stat -c '%a' "$secure_file")
  [ "$secure_mode" = "600" ] || die "$secure_file must have mode 0600 (found $secure_mode)"
}

reject_placeholders() {
  placeholder_file=$1
  if grep -Eq '<[^>]+>' "$placeholder_file"; then
    die "$placeholder_file still contains placeholder values"
  fi
}
