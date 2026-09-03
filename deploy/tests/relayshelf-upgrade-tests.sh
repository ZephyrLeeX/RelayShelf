#!/bin/sh
set -eu
export LC_ALL=C

test_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
bundle_root=$(CDPATH= cd -- "$test_dir/.." && pwd)
updater=$bundle_root/scripts/relayshelf-upgrade
test_root=$(mktemp -d)
trap 'rm -rf "$test_root"' EXIT HUP INT TERM

fail() { echo "FAIL: $*" >&2; exit 1; }
assert_has() { grep -Fq "$2" "$1" || fail "$1 missing: $2"; }
assert_lacks() { ! grep -Fq "$2" "$1" || fail "$1 unexpectedly contains: $2"; }

make_case() {
  name=$1
  version=${2:-1.2.3}
  case_dir=$test_root/$name
  top=$case_dir/source/relayshelf-deploy-$version
  mkdir -p "$top/scripts" "$case_dir/serve" "$case_dir/tmp" "$case_dir/state" "$case_dir/bin"
  : >"$case_dir/calls"
  cat >"$top/RELEASE-METADATA" <<EOF
RELEASE_SCHEMA=1
VERSION=$version
GIT_COMMIT=0123456789abcdef0123456789abcdef01234567
BUILD_TIME=2026-09-03T00:00:00Z
IMAGE=ghcr.io/zephyrleex/relayshelf:$version
EOF
  cat >"$top/scripts/upgrade.sh" <<EOF
#!/bin/sh
echo "upgrade \$*" >>'$case_dir/calls'
echo "expected \${RELAYSHELF_EXPECTED_GIT_COMMIT:-missing} \${RELAYSHELF_EXPECTED_BUILD_TIME:-missing}" >>'$case_dir/calls'
status=0
[ ! -f '$case_dir/upgrade-status' ] || status=\$(sed -n '1p' '$case_dir/upgrade-status')
exit "\$status"
EOF
  chmod +x "$top/scripts/upgrade.sh"
  bundle_name=relayshelf-deploy-$version.tar.gz
  (cd "$case_dir/source" && tar -czf "$case_dir/serve/$bundle_name" "relayshelf-deploy-$version")
  (cd "$case_dir/serve" && sha256sum "$bundle_name" >"$bundle_name.sha256")

  cat >"$case_dir/bin/curl" <<EOF
#!/bin/sh
echo "curl \$*" >>'$case_dir/calls'
output=
url=
while [ "\$#" -gt 0 ]; do
  case "\$1" in
    --output) output=\$2; shift 2 ;;
    http*) url=\$1; shift ;;
    *) shift ;;
  esac
done
[ -n "\$output" ] && [ -n "\$url" ] || exit 2
asset=\${url##*/}
cp '$case_dir/serve/'"\$asset" "\$output"
EOF
  cat >"$case_dir/bin/id" <<EOF
#!/bin/sh
[ ! -f '$case_dir/mock-uid' ] || { sed -n '1p' '$case_dir/mock-uid'; exit; }
echo 0
EOF
  cat >"$case_dir/bin/podman" <<EOF
#!/bin/sh
echo "podman \$*" >>'$case_dir/calls'
count=0
[ ! -f '$case_dir/podman-count' ] || count=\$(sed -n '1p' '$case_dir/podman-count')
count=\$((count + 1)); echo "\$count" >'$case_dir/podman-count'
if [ "\$count" -eq 1 ]; then
  current=1.2.2
  [ ! -f '$case_dir/current-version' ] || current=\$(sed -n '1p' '$case_dir/current-version')
  [ -z "\$current" ] || echo "RelayShelf \$current (commit current, built now)"
else
  final=$version
  [ ! -f '$case_dir/final-version' ] || final=\$(sed -n '1p' '$case_dir/final-version')
  echo "RelayShelf \$final (commit 0123456789abcdef0123456789abcdef01234567, built 2026-09-03T00:00:00Z)"
fi
EOF
  cat >"$case_dir/bin/systemctl" <<EOF
#!/bin/sh
echo "systemctl \$*" >>'$case_dir/calls'
[ ! -f '$case_dir/systemctl-failure' ]
EOF
  chmod +x "$case_dir/bin/"*
  cat >"$case_dir/run" <<EOF
#!/bin/sh
export RELAYSHELF_ID_BIN='$case_dir/bin/id'
export RELAYSHELF_CURL_BIN='$case_dir/bin/curl'
export RELAYSHELF_PODMAN_BIN='$case_dir/bin/podman'
export RELAYSHELF_SYSTEMCTL_BIN='$case_dir/bin/systemctl'
export RELAYSHELF_STATE_DIR='$case_dir/state'
export RELAYSHELF_UPGRADE_TMPDIR='$case_dir/tmp'
exec '$updater' "\$@"
EOF
  chmod +x "$case_dir/run"
}

repack() {
  case_dir=$1
  version=$2
  bundle_name=relayshelf-deploy-$version.tar.gz
  rm -f "$case_dir/serve/$bundle_name" "$case_dir/serve/$bundle_name.sha256"
  (cd "$case_dir/source" && tar -czf "$case_dir/serve/$bundle_name" "relayshelf-deploy-$version")
  (cd "$case_dir/serve" && sha256sum "$bundle_name" >"$bundle_name.sha256")
}

run_plain() {
  case_dir=$1
  shift
  set +e
  "$case_dir/run" "$@" >"$case_dir/output" 2>&1
  status=$?
  set -e
}

run_interactive() {
  case_dir=$1
  shift
  set +e
  printf 'YES\n' | script -qefc "$case_dir/run $*" /dev/null >"$case_dir/output" 2>&1
  status=$?
  set -e
}

make_case help
"$test_root/help/run" --help >"$test_root/help/output"
assert_has "$test_root/help/output" 'relayshelf-upgrade VERSION'

make_case invalid
run_plain "$test_root/invalid" 1.2
[ "$status" -ne 0 ] || fail "invalid version unexpectedly succeeded"
assert_lacks "$test_root/invalid/calls" 'curl '
run_plain "$test_root/invalid" v1.2.3
[ "$status" -ne 0 ] || fail "leading-v version unexpectedly succeeded"
assert_has "$test_root/invalid/output" 'without a leading v'

make_case prerelease 1.2.3-rc.1
run_plain "$test_root/prerelease" 1.2.3-rc.1
[ "$status" -ne 0 ] || fail "noninteractive prerelease unexpectedly passed confirmation"
assert_has "$test_root/prerelease/output" 'backup confirmation requires an interactive terminal'

make_case root-required
echo 1000 >"$test_root/root-required/mock-uid"
run_plain "$test_root/root-required" 1.2.3
[ "$status" -ne 0 ] || fail "non-root invocation unexpectedly succeeded"
assert_has "$test_root/root-required/output" 'relayshelf-upgrade must be run as root'
assert_lacks "$test_root/root-required/calls" 'curl '

make_case download-404
rm "$test_root/download-404/serve/relayshelf-deploy-1.2.3.tar.gz"
run_plain "$test_root/download-404" 1.2.3
[ "$status" -ne 0 ] || fail "missing bundle unexpectedly succeeded"
assert_has "$test_root/download-404/output" 'could not download RelayShelf deployment bundle'
assert_lacks "$test_root/download-404/calls" 'upgrade '

make_case checksum-missing
rm "$test_root/checksum-missing/serve/relayshelf-deploy-1.2.3.tar.gz.sha256"
run_plain "$test_root/checksum-missing" 1.2.3
[ "$status" -ne 0 ] || fail "missing checksum unexpectedly succeeded"
assert_has "$test_root/checksum-missing/output" 'could not download RelayShelf deployment bundle checksum'

make_case checksum-mismatch
printf '%064d  relayshelf-deploy-1.2.3.tar.gz\n' 0 >"$test_root/checksum-mismatch/serve/relayshelf-deploy-1.2.3.tar.gz.sha256"
run_plain "$test_root/checksum-mismatch" 1.2.3
[ "$status" -ne 0 ] || fail "checksum mismatch unexpectedly succeeded"
assert_has "$test_root/checksum-mismatch/output" 'checksum verification failed'
assert_lacks "$test_root/checksum-mismatch/calls" 'upgrade '

make_case checksum-malformed
printf 'not-a-checksum\n' >"$test_root/checksum-malformed/serve/relayshelf-deploy-1.2.3.tar.gz.sha256"
run_plain "$test_root/checksum-malformed" 1.2.3
[ "$status" -ne 0 ] || fail "malformed checksum unexpectedly succeeded"
assert_has "$test_root/checksum-malformed/output" 'checksum is malformed'

make_case archive-malformed
printf 'not a tar archive\n' >"$test_root/archive-malformed/serve/relayshelf-deploy-1.2.3.tar.gz"
(cd "$test_root/archive-malformed/serve" && sha256sum relayshelf-deploy-1.2.3.tar.gz >relayshelf-deploy-1.2.3.tar.gz.sha256)
run_plain "$test_root/archive-malformed" 1.2.3
[ "$status" -ne 0 ] || fail "malformed archive unexpectedly succeeded"
assert_lacks "$test_root/archive-malformed/calls" 'upgrade '

make_case traversal
case_dir=$test_root/traversal
rm "$case_dir/serve/relayshelf-deploy-1.2.3.tar.gz"
mkdir "$case_dir/traversal-input"
printf unsafe >"$case_dir/traversal-input/escape"
tar -czf "$case_dir/serve/relayshelf-deploy-1.2.3.tar.gz" -C "$case_dir/traversal-input" --transform='s|escape|relayshelf-deploy-1.2.3/../../escape|' escape
(cd "$case_dir/serve" && sha256sum relayshelf-deploy-1.2.3.tar.gz >relayshelf-deploy-1.2.3.tar.gz.sha256)
run_plain "$case_dir" 1.2.3
[ "$status" -ne 0 ] || fail "path traversal archive unexpectedly succeeded"
assert_has "$case_dir/output" 'unsafe path'

make_case unexpected-top
case_dir=$test_root/unexpected-top
mv "$case_dir/source/relayshelf-deploy-1.2.3" "$case_dir/source/other-top"
rm "$case_dir/serve/"*
(cd "$case_dir/source" && tar -czf "$case_dir/serve/relayshelf-deploy-1.2.3.tar.gz" other-top)
(cd "$case_dir/serve" && sha256sum relayshelf-deploy-1.2.3.tar.gz >relayshelf-deploy-1.2.3.tar.gz.sha256)
run_plain "$case_dir" 1.2.3
[ "$status" -ne 0 ] || fail "unexpected archive root succeeded"
assert_has "$case_dir/output" 'unexpected top-level path'

make_case symlink
case_dir=$test_root/symlink
ln -s /bin/sh "$case_dir/source/relayshelf-deploy-1.2.3/scripts/unsafe-link"
repack "$case_dir" 1.2.3
run_plain "$case_dir" 1.2.3
[ "$status" -ne 0 ] || fail "archive symlink unexpectedly succeeded"
assert_has "$case_dir/output" 'unsupported links or special files'

for scenario in metadata-missing version-mismatch image-mismatch commit-malformed arbitrary-image; do
  make_case "$scenario"
done
rm "$test_root/metadata-missing/source/relayshelf-deploy-1.2.3/RELEASE-METADATA"
sed -i 's/^VERSION=.*/VERSION=9.9.9/' "$test_root/version-mismatch/source/relayshelf-deploy-1.2.3/RELEASE-METADATA"
sed -i 's|^IMAGE=.*|IMAGE=ghcr.io/zephyrleex/relayshelf:9.9.9|' "$test_root/image-mismatch/source/relayshelf-deploy-1.2.3/RELEASE-METADATA"
sed -i 's/^GIT_COMMIT=.*/GIT_COMMIT=unknown/' "$test_root/commit-malformed/source/relayshelf-deploy-1.2.3/RELEASE-METADATA"
sed -i 's|^IMAGE=.*|IMAGE=evil.example/relayshelf:1.2.3|' "$test_root/arbitrary-image/source/relayshelf-deploy-1.2.3/RELEASE-METADATA"
for scenario in metadata-missing version-mismatch image-mismatch commit-malformed arbitrary-image; do
  repack "$test_root/$scenario" 1.2.3
  run_plain "$test_root/$scenario" 1.2.3
  [ "$status" -ne 0 ] || fail "$scenario unexpectedly succeeded"
  assert_lacks "$test_root/$scenario/calls" 'upgrade '
done
assert_has "$test_root/metadata-missing/output" 'missing RELEASE-METADATA'
assert_has "$test_root/version-mismatch/output" 'VERSION does not match'
assert_has "$test_root/image-mismatch/output" 'IMAGE does not match'
assert_has "$test_root/commit-malformed/output" 'GIT_COMMIT is malformed'
assert_lacks "$test_root/arbitrary-image/calls" 'evil.example'

make_case success
run_interactive "$test_root/success" 1.2.3
[ "$status" -eq 0 ] || { sed -n '1,200p' "$test_root/success/output" >&2; fail "valid upgrade returned $status"; }
[ "$(grep -c '^upgrade ' "$test_root/success/calls")" -eq 1 ] || fail "bundled upgrade was not called exactly once"
assert_has "$test_root/success/calls" 'upgrade --image ghcr.io/zephyrleex/relayshelf:1.2.3 --backup-confirmed'
assert_has "$test_root/success/calls" 'expected 0123456789abcdef0123456789abcdef01234567 2026-09-03T00:00:00Z'
assert_has "$test_root/success/output" 'RelayShelf upgrade completed successfully: 1.2.3'
[ -z "$(find "$test_root/success/tmp" -mindepth 1 -print -quit)" ] || fail "temporary files survived successful upgrade"
[ "$(grep -c -- '--retry 3' "$test_root/success/calls")" -eq 2 ] || fail "curl retry count is not bounded at three"

make_case upgrade-failure
echo 23 >"$test_root/upgrade-failure/upgrade-status"
set +e
printf 'YES\n' | script -qefc "$test_root/upgrade-failure/run 1.2.3" /dev/null >"$test_root/upgrade-failure/output" 2>&1
status=$?
set -e
[ "$status" -eq 23 ] || fail "bundled upgrade failure status was not propagated (got $status)"
[ -z "$(find "$test_root/upgrade-failure/tmp" -mindepth 1 -print -quit)" ] || fail "temporary files survived failed upgrade"

make_case final-mismatch
echo 9.9.9 >"$test_root/final-mismatch/final-version"
set +e
printf 'YES\n' | script -qefc "$test_root/final-mismatch/run 1.2.3" /dev/null >"$test_root/final-mismatch/output" 2>&1
status=$?
set -e
[ "$status" -ne 0 ] || fail "final version mismatch unexpectedly succeeded"
assert_has "$test_root/final-mismatch/output" 'installed RelayShelf version does not match'

make_case same-version
echo 1.2.3 >"$test_root/same-version/current-version"
set +e
printf 'YES\n' | script -qefc "$test_root/same-version/run 1.2.3" /dev/null >"$test_root/same-version/output" 2>&1
status=$?
set -e
[ "$status" -eq 0 ] || fail "same-version reconciliation failed"
assert_has "$test_root/same-version/output" 'reconciling deployment components'
[ "$(grep -c '^upgrade ' "$test_root/same-version/calls")" -eq 1 ] || fail "same-version reconciliation skipped upgrade.sh"

make_case downgrade
echo 1.10.0 >"$test_root/downgrade/current-version"
run_plain "$test_root/downgrade" 1.2.3
[ "$status" -ne 0 ] || fail "stable downgrade unexpectedly succeeded"
assert_has "$test_root/downgrade/output" 'Refusing to downgrade RelayShelf from 1.10.0 to 1.2.3'
assert_lacks "$test_root/downgrade/calls" 'upgrade '

make_case lock-busy
set +e
flock "$test_root/lock-busy/state/upgrade.lock" "$test_root/lock-busy/run" 1.2.3 >"$test_root/lock-busy/output" 2>&1
status=$?
set -e
[ "$status" -ne 0 ] || fail "concurrent upgrade unexpectedly succeeded"
assert_has "$test_root/lock-busy/output" 'Another RelayShelf upgrade is already running'
assert_lacks "$test_root/lock-busy/calls" 'curl '

! grep -Eq '(^|[[:space:]])git([[:space:]]|$)' "$updater" || fail "production updater depends on git"
echo "RelayShelf release updater tests: PASS"
