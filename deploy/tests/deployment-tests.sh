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
"$test_dir/storage-recovery-tests.sh"
"$test_dir/relayshelf-upgrade-tests.sh"

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
grep -Fxq 'HealthCmd=["/relayshelf","healthcheck"]' "$rendered" || fail "rendered healthcheck is not shell-free exec form"
! grep -Fq 'HealthCmd=/relayshelf healthcheck' "$rendered" || fail "rendered healthcheck regressed to shell form"
! grep -q '@@RELAYSHELF_IMAGE@@' "$rendered" || fail "render placeholder survived"
expect_failure "$bundle_root/scripts/render-quadlet.sh" ghcr.io/example/relayshelf:0.12.0-test.1 0.0.0.0 "$rendered"

# Rootful Podman cannot forward a published port through an internal network.
# The deployment authority must therefore use one ordinary dedicated bridge,
# publish only app TCP 8080, and publish no PostgreSQL port at all.
network_unit=$bundle_root/quadlet/relayshelf.network
postgres_unit=$bundle_root/quadlet/relayshelf-postgres.container
grep -Fxq 'Driver=bridge' "$network_unit" || fail "network authority is not a bridge"
! grep -Eq '^[[:space:]]*Internal[[:space:]]*=[[:space:]]*(true|yes|1)([[:space:]]|$)' "$network_unit" || fail "network authority still disables rootful port forwarding"
[ "$(grep -c '^PublishPort=' "$bundle_root/quadlet/relayshelf-app.container.in")" -eq 1 ] || fail "app authority must have exactly one PublishPort"
! grep -q '^PublishPort=' "$postgres_unit" || fail "PostgreSQL authority publishes a host port"
bad_network=$test_tmp_dir/internal.network
cp "$network_unit" "$bad_network"
printf '%s\n' 'Internal=true' >>"$bad_network"
expect_failure env RELAYSHELF_NETWORK_UNIT="$bad_network" "$bundle_root/scripts/verify.sh"

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
install_script=$bundle_root/scripts/install.sh
upgrade=$bundle_root/scripts/upgrade.sh
for deployment_script in "$install_script" "$upgrade"; do
  grep -Fq 'relayshelf-storage-recover' "$deployment_script" || fail "$deployment_script does not install the recovery helper"
  grep -Fq 'relayshelf-storage-recovery.service' "$deployment_script" || fail "$deployment_script does not install the recovery service"
  grep -Fq 'systemctl enable --now relayshelf-storage-recovery.timer' "$deployment_script" || fail "$deployment_script does not enable the recovery timer"
done
grep -Fq '/usr/local/bin/relayshelf-upgrade' "$install_script" || fail "fresh install does not install relayshelf-upgrade"
grep -Fq 'install -m 0755 "$bundle_root/scripts/relayshelf-upgrade"' "$install_script" || fail "fresh install does not set updater mode 0755"
grep -Fq 'mv -f "$updater_tmp" /usr/local/bin/relayshelf-upgrade' "$upgrade" || fail "upgrade does not atomically self-update the updater"
grep -Fq 'operation.lock' "$upgrade" || fail "upgrade does not take the shared host operation lock"
grep -Fq 'systemctl stop relayshelf-storage-recovery.timer' "$upgrade" || fail "upgrade does not quiesce a legacy recovery timer"
grep -Fq 'systemctl is-active --quiet relayshelf-storage-recovery.service' "$upgrade" || fail "upgrade does not reject an active recovery operation"
grep -Fq 'could not restart relayshelf-storage-recovery.timer' "$upgrade" || fail "upgrade failure cannot restore the recovery timer"
grep -Fq '"$script_dir/render-quadlet.sh" "$image_ref" "$listen_address" "$rendered"' "$upgrade" ||
  fail "upgrade does not render the candidate unit from the deployment authority"
grep -Fq 'install -m 0644 "$bundle_root/quadlet/relayshelf.network" /etc/containers/systemd/relayshelf.network' "$upgrade" ||
  fail "upgrade does not install the candidate network authority"
grep -Fq 'podman network inspect --format '\''{{.Internal}}'\'' relayshelf' "$upgrade" ||
  fail "upgrade does not inspect the existing network mode"
stop_postgres_line=$(grep -n '^  systemctl stop relayshelf-postgres.service$' "$upgrade" | cut -d: -f1)
remove_network_line=$(grep -n '^  if ! podman network rm relayshelf; then$' "$upgrade" | cut -d: -f1)
start_network_line=$(grep -n '^  if ! systemctl start relayshelf-network.service; then$' "$upgrade" | cut -d: -f1)
start_postgres_line=$(grep -n '^  if ! systemctl start relayshelf-postgres.service; then$' "$upgrade" | cut -d: -f1)
migrate_line=$(grep -n '"$image_ref" migrate;' "$upgrade" | cut -d: -f1)
install_line=$(grep -n 'install -m 0644 "$rendered" /etc/containers/systemd/relayshelf-app.container' "$upgrade" | cut -d: -f1)
[ -n "$stop_postgres_line" ] && [ -n "$remove_network_line" ] && [ "$stop_postgres_line" -lt "$remove_network_line" ] || fail "upgrade can remove a network still used by PostgreSQL"
[ -n "$start_network_line" ] && [ -n "$start_postgres_line" ] && [ "$remove_network_line" -lt "$start_network_line" ] && [ "$start_network_line" -lt "$start_postgres_line" ] || fail "replacement network startup order is unsafe"
[ -n "$start_postgres_line" ] && [ -n "$migrate_line" ] && [ "$start_postgres_line" -lt "$migrate_line" ] || fail "migration can run before PostgreSQL is healthy on the replacement network"
[ -n "$migrate_line" ] && [ -n "$install_line" ] && [ "$migrate_line" -lt "$install_line" ] || fail "candidate unit can be installed before migration succeeds"
grep -Fq 'readiness failed' "$upgrade" || fail "readiness failure is not reported"
grep -Fq 'previous.' "$upgrade" || fail "previous unit is not retained"
grep -Fq 'backup_network_unit=' "$upgrade" || fail "previous network authority is not retained"
grep -Fq 'restore_old_network' "$upgrade" || fail "pre-migration network failure has no rollback path"
grep -Fq 'http://$listen_address:8080/health/live' "$upgrade" || fail "upgrade does not test the published endpoint"
grep -Fq 'http://$listen_address:8080/health/ready' "$upgrade" || fail "upgrade does not test published readiness"
grep -Fq 'podman port relayshelf-postgres' "$upgrade" || fail "upgrade does not check PostgreSQL host exposure"
grep -Fq 'candidate image build metadata does not match the deployment bundle' "$upgrade" || fail "upgrade does not bind image metadata to the release bundle"

# Release publication must be gated by the same CI. A published release must
# stop the workflow before any authoritative build, image push, or asset upload.
release_workflow=$bundle_root/../.github/workflows/release.yml
ci_workflow=$bundle_root/../.github/workflows/ci.yml
grep -Fq 'workflow_call:' "$ci_workflow" || fail "CI is not reusable as the release quality gate"
grep -Fq -- "- 'v*.*.*'" "$release_workflow" || fail "release tag pattern is missing"
grep -Fq 'contents: write' "$release_workflow" || fail "release workflow cannot publish GitHub Releases"
grep -Fq 'packages: write' "$release_workflow" || fail "release workflow cannot push GHCR"
grep -Fq 'uses: ./.github/workflows/ci.yml' "$release_workflow" || fail "release does not run the CI quality gate"
grep -Fq 'ghcr.io/zephyrleex/relayshelf:${VERSION}' "$release_workflow" || fail "release image is not the exact project GHCR SemVer tag"
grep -Fq 'sha256sum "${{ steps.version.outputs.bundle }}"' "$release_workflow" || fail "release checksum is not generated"
grep -Fq 'gh release create' "$release_workflow" || fail "GitHub Release publication is missing"
grep -Fq 'isDraft' "$release_workflow" || fail "release workflow does not distinguish draft and published releases"
grep -Fq 'already published and immutable' "$release_workflow" || fail "published releases do not fail closed as immutable"
! grep -Fq -- '--draft=true' "$release_workflow" || fail "a published release can be changed back to draft"
guard_line=$(grep -n -- '- name: Check release immutability' "$release_workflow" | cut -d: -f1)
build_line=$(grep -n 'build-release.sh' "$release_workflow" | cut -d: -f1)
push_line=$(grep -n 'podman push' "$release_workflow" | cut -d: -f1)
create_line=$(grep -n 'gh release create' "$release_workflow" | cut -d: -f1)
upload_line=$(grep -n 'gh release upload' "$release_workflow" | cut -d: -f1)
[ -n "$guard_line" ] && [ -n "$build_line" ] && [ -n "$push_line" ] && [ -n "$create_line" ] && [ -n "$upload_line" ] ||
  fail "release workflow ordering markers are incomplete"
[ "$guard_line" -lt "$build_line" ] && [ "$build_line" -lt "$push_line" ] &&
  [ "$push_line" -lt "$create_line" ] && [ "$push_line" -lt "$upload_line" ] ||
  fail "release guard/build/push/assets ordering is unsafe"

for bundled_path in \
  scripts/relayshelf-upgrade \
  scripts/upgrade.sh \
  libexec/relayshelf-storage-recover \
  systemd/relayshelf-storage-recovery.service \
  systemd/relayshelf-storage-recovery.timer \
  quadlet/relayshelf-app.container.in; do
  [ -e "$bundle_root/$bundled_path" ] || fail "release bundle source is missing $bundled_path"
done
grep -Fq 'RELEASE_SCHEMA=1' "$bundle_root/scripts/build-release.sh" || fail "release metadata schema is missing"

echo "RelayShelf deployment failure-policy tests: PASS"
