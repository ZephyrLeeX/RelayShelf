#!/bin/sh
set -eu

test_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
bundle_root=$(CDPATH= cd -- "$test_dir/.." && pwd)
. "$bundle_root/scripts/common.sh"

fail() { echo "FAIL: $*" >&2; exit 1; }
expect_failure() {
  if "$@" >/dev/null 2>&1; then
    fail "command unexpectedly succeeded: $*"
  fi
}

"$bundle_root/scripts/verify.sh"

expect_failure sh -c '. "$1"; validate_image_ref relayshelf:1.2.3' sh "$bundle_root/scripts/common.sh"
expect_failure sh -c '. "$1"; validate_image_ref docker.io/example/relayshelf:latest' sh "$bundle_root/scripts/common.sh"
expect_failure sh -c '. "$1"; validate_image_ref docker.io/example/relayshelf:release' sh "$bundle_root/scripts/common.sh"
expect_failure sh -c '. "$1"; require_secure_file /definitely/missing/relayshelf.env' sh "$bundle_root/scripts/common.sh"

rendered=$(mktemp)
test_tmp_dir=$(mktemp -d)
trap 'rm -f "$rendered"; rm -rf "$test_tmp_dir"' EXIT HUP INT TERM
"$bundle_root/scripts/render-quadlet.sh" ghcr.io/example/relayshelf:0.12.0-test.1 192.0.2.10 "$rendered"
grep -Fxq 'Image=ghcr.io/example/relayshelf:0.12.0-test.1' "$rendered" || fail "rendered image mismatch"
grep -Fxq 'PublishPort=192.0.2.10:8080:8080' "$rendered" || fail "rendered LAN binding mismatch"
! grep -q '@@RELAYSHELF_IMAGE@@' "$rendered" || fail "render placeholder survived"
expect_failure "$bundle_root/scripts/render-quadlet.sh" ghcr.io/example/relayshelf:0.12.0-test.1 0.0.0.0 "$rendered"

# Fail before parsing units when CI accidentally supplies the old Ubuntu 24.04
# generator that originally masked the Debian 13 production baseline.
old_generator=$test_tmp_dir/podman-system-generator
cat >"$old_generator" <<'EOF'
#!/bin/sh
if [ "${1:-}" = "--version" ]; then
  echo 4.9.3
  exit 0
fi
exit 99
EOF
chmod +x "$old_generator"
old_generator_log=$test_tmp_dir/old-generator.log
if QUADLET_GENERATOR="$old_generator" "$bundle_root/scripts/verify.sh" >"$old_generator_log" 2>&1; then
  fail "Podman/Quadlet 4.9.3 unexpectedly satisfied the deployment baseline"
fi
grep -Fq 'Found: 4.9.3. Required: >= 5.2.0.' "$old_generator_log" ||
  fail "old Quadlet generator failure did not report the version contract"

# A normal local directory must never satisfy the host NFS identity gate.
expect_failure "$bundle_root/libexec/relayshelf-host-storage-check" /tmp

# Upgrade ordering and failure boundaries are release invariants.
upgrade=$bundle_root/scripts/upgrade.sh
migrate_line=$(grep -n '"$image_ref" migrate;' "$upgrade" | cut -d: -f1)
install_line=$(grep -n 'install -m 0644 "$rendered" /etc/containers/systemd/relayshelf-app.container' "$upgrade" | cut -d: -f1)
[ -n "$migrate_line" ] && [ -n "$install_line" ] && [ "$migrate_line" -lt "$install_line" ] || fail "candidate unit can be installed before migration succeeds"
grep -Fq 'readiness failed' "$upgrade" || fail "readiness failure is not reported"
grep -Fq 'previous.' "$upgrade" || fail "previous unit is not retained"

echo "RelayShelf deployment failure-policy tests: PASS"
