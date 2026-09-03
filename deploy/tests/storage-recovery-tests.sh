#!/bin/sh
set -eu

test_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
bundle_root=$(CDPATH= cd -- "$test_dir/.." && pwd)
helper=$bundle_root/libexec/relayshelf-storage-recover
root=$(mktemp -d)
trap 'rm -rf "$root"' EXIT HUP INT TERM

fail() { echo "FAIL: $*" >&2; exit 1; }
assert_has() { grep -Fq "$2" "$1" || fail "$1 missing: $2"; }
assert_lacks() { ! grep -Fq "$2" "$1" || fail "$1 unexpectedly contains: $2"; }

make_mocks() {
  case_name=$1
  dir=$root/$case_name
  mkdir -p "$dir/bin" "$dir/state" "$dir/mount/.commit-tmp"
  : >"$dir/calls"
  for tool_name in id install sleep date timeout; do
    case "$tool_name" in
      id) body='echo 0' ;;
      install) body='while [ "$#" -gt 0 ]; do case "$1" in -*) shift ;; *) mkdir -p "$1"; shift ;; esac; done' ;;
      sleep) body='exit 0' ;;
      date) body='echo 1000' ;;
      timeout) body='shift; exec "$@"' ;;
    esac
    printf '#!/bin/sh\n%s\n' "$body" >"$dir/bin/$tool_name"
    chmod +x "$dir/bin/$tool_name"
  done
  cat >"$dir/bin/findmnt" <<EOF
#!/bin/sh
case " \$* " in *' --fstab '*) echo 'nas.example:/export'; exit 0 ;; esac
echo 'nas.example:/export nfs4'
EOF
  cat >"$dir/bin/stat" <<EOF
#!/bin/sh
count_file='$dir/stat-count'
count=0; [ ! -f "\$count_file" ] || count=\$(sed -n '1p' "\$count_file")
count=\$((count + 1)); echo "\$count" >"\$count_file"
if [ -f '$dir/mounted' ]; then exit 0; fi
case '$case_name' in
  healthy) exit 0 ;;
  transient) [ "\$count" -eq 1 ] && { echo 'Stale file handle' >&2; exit 1; }; exit 0 ;;
  timeout) exit 124 ;;
  permission) echo 'Permission denied' >&2; exit 1 ;;
  full) echo 'No space left on device' >&2; exit 1 ;;
  *) echo 'Stale file handle' >&2; exit 1 ;;
esac
EOF
  cat >"$dir/bin/systemctl" <<EOF
#!/bin/sh
echo "systemctl \$*" >>'$dir/calls'
[ "\${1:-}" != is-active ]
EOF
  cat >"$dir/bin/umount" <<EOF
#!/bin/sh
echo "umount \$*" >>'$dir/calls'
case '$case_name' in
  force-fail) exit 1 ;;
  normal-fail) [ "\${1:-}" = -f ] || exit 1 ;;
esac
exit 0
EOF
  cat >"$dir/bin/mount" <<EOF
#!/bin/sh
echo "mount \$*" >>'$dir/calls'
[ '$case_name' != mount-fail ] || exit 1
touch '$dir/mounted'
EOF
  cat >"$dir/bin/setpriv" <<EOF
#!/bin/sh
echo "setpriv \$*" >>'$dir/calls'
[ '$case_name' != uid-fail ]
EOF
  cat >"$dir/bin/podman" <<EOF
#!/bin/sh
echo "podman \$*" >>'$dir/calls'
[ '$case_name' != storage-fail ]
EOF
  cat >"$dir/bin/flock" <<EOF
#!/bin/sh
echo "flock \$*" >>'$dir/calls'
[ '$case_name' != lock-busy ]
EOF
  chmod +x "$dir/bin/"*
}

run_case() {
  scenario=$1
  cooldown=${2:-0}
  make_mocks "$scenario"
  dir=$root/$scenario
  set +e
  output=$(STATE_DIR="$dir/state" STORAGE_COMMON="$bundle_root/libexec/relayshelf-storage-common" \
    FINDMNT_BIN="$dir/bin/findmnt" STAT_BIN="$dir/bin/stat" TIMEOUT_BIN="$dir/bin/timeout" \
    SYSTEMCTL_BIN="$dir/bin/systemctl" MOUNT_BIN="$dir/bin/mount" UMOUNT_BIN="$dir/bin/umount" \
    PODMAN_BIN="$dir/bin/podman" SET_PRIV_BIN="$dir/bin/setpriv" FLOCK_BIN="$dir/bin/flock" \
    SLEEP_BIN="$dir/bin/sleep" DATE_BIN="$dir/bin/date" ID_BIN="$dir/bin/id" INSTALL_BIN="$dir/bin/install" \
    CONFIRM_DELAY_SECONDS=0 COOLDOWN_SECONDS="$cooldown" "$helper" "$dir/mount" 2>&1)
  status=$?
  set -e
  printf '%s\n' "$output" >"$dir/output"
}

for name in healthy transient timeout permission full; do
  run_case "$name"
  [ "$status" -eq 0 ] || fail "$name returned $status"
  assert_lacks "$root/$name/calls" 'systemctl stop'
  assert_lacks "$root/$name/calls" 'umount '
  assert_lacks "$root/$name/calls" 'mount '
done

run_case recover
[ "$status" -eq 0 ] || fail "recover returned $status"
for call in 'systemctl stop relayshelf-app.service' 'umount ' 'mount ' 'systemctl start relayshelf-app.service' 'podman exec relayshelf-app /relayshelf storage check'; do
  assert_has "$root/recover/calls" "$call"
done
assert_has "$root/recover/output" 'event=recovery_succeeded'

run_case normal-fail
[ "$status" -eq 0 ] || fail "normal-fail returned $status"
assert_has "$root/normal-fail/calls" 'umount -f '

for name in force-fail mount-fail uid-fail storage-fail; do
  run_case "$name"
  [ "$status" -ne 0 ] || fail "$name unexpectedly succeeded"
  assert_has "$root/$name/output" 'event=recovery_failed'
done
! grep -q '^mount ' "$root/force-fail/calls" || fail 'force-fail mounted over the broken mount'
assert_lacks "$root/mount-fail/calls" 'systemctl start relayshelf-app.service'
assert_lacks "$root/uid-fail/calls" 'systemctl start relayshelf-app.service'
assert_has "$root/storage-fail/calls" 'systemctl stop relayshelf-app.service'

run_case lock-busy
[ "$status" -eq 0 ] || fail "lock-busy returned $status"
assert_has "$root/lock-busy/output" 'event=lock_busy'
assert_has "$root/lock-busy/output" 'detail=another_host_operation'
assert_lacks "$root/lock-busy/calls" 'systemctl stop'
[ -f "$root/lock-busy/state/operation.lock" ] || fail "recovery did not use the shared host operation lock"

make_mocks cooldown
echo 900 >"$root/cooldown/state/storage-recovery.last"
run_case cooldown 300
[ "$status" -eq 0 ] || fail "cooldown returned $status"
assert_has "$root/cooldown/output" 'event=cooldown_active'
assert_lacks "$root/cooldown/calls" 'systemctl stop'

echo 'RelayShelf storage recovery tests: PASS'
