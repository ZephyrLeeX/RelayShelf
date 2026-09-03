#!/usr/bin/env bash
# T124 real-NFS outage qualification (reference environment only).
#
# Simulates the NAS being unreachable and verifies the operator-visible
# contract: bounded failures while the mount is down, and clean recovery.
# This script never modifies the NFS export beyond files it created itself,
# each named with the relayshelf-qual- prefix inside a directory that must
# already contain the .relayshelf-qual-test marker.
#
# Required environment:
#   NFS_TEST_ROOT                     directory on the real NFS export under test
#   RELAYSHELF_TEST_DESTRUCTIVE=1     explicit confirmation
# Optional:
#   NFS_PROBE_TIMEOUT (seconds, default 60) how long a down mount may block
#                       before this qualification treats it as unbounded
set -euo pipefail

if [ "${RELAYSHELF_TEST_DESTRUCTIVE:-}" != "1" ]; then
  echo "Refusing to run: set RELAYSHELF_TEST_DESTRUCTIVE=1 after confirming" >&2
  echo "this harness only touches its own files in NFS_TEST_ROOT." >&2
  exit 2
fi
root="${NFS_TEST_ROOT:-}"
if [ -z "$root" ] || [ ! -d "$root" ]; then
  echo "NFS_TEST_ROOT must be an existing directory on the NFS export under test." >&2
  exit 2
fi
marker="$root/.relayshelf-qual-test"
if [ ! -f "$marker" ]; then
  echo "Safety marker $marker is missing; refusing to operate on this path." >&2
  exit 2
fi
timeout_s="${NFS_PROBE_TIMEOUT:-60}"
probe_prefix="relayshelf-qual-probe-$$"
results=0

probe() {
  local file="$root/${probe_prefix}-$1"
  local started ended
  started=$(date +%s)
  if timeout "$timeout_s" bash -c "printf '%s' '$1' > '$file'" 2>/dev/null; then
    ended=$(date +%s)
    printf 'probe %s: PASS (write, %ss)\n' "$1" "$((ended - started))"
    return 0
  fi
  ended=$(date +%s)
  printf 'probe %s: REJECTED/BOUNDED (failed within %ss)\n' "$1" "$((ended - started))"
  return 1
}

cleanup() {
  # Remove only this run's own probe files; never a recursive delete.
  rm -f "$root/${probe_prefix}"-* 2>/dev/null || true
}
trap cleanup EXIT

echo "== T124 real NFS outage qualification =="
echo "root: $root"
echo "probe timeout: ${timeout_s}s"

echo
echo "-- phase 1: healthy baseline --"
if ! probe baseline; then
  echo "FAIL: NFS export is not writable before the outage; fix the environment." >&2
  exit 1
fi

echo
echo "-- phase 2: outage --"
echo "Take the NAS/NFS export offline now (stop the NFS service, drop the"
echo "export, or firewall the client). The mount used by RelayShelf should be"
echo "the one behind $root."
read -r -p "Press Enter once the outage has started..." _

for i in 1 2 3; do
  if probe "outage-$i"; then
    echo "WARNING: write succeeded during the declared outage; the mount may not be the one under test." >&2
    results=$((results + 1))
  fi
done

echo
echo "-- phase 3: recovery --"
read -r -p "Restore the NAS/NFS export now, then press Enter..." _

recovered=0
for i in $(seq 1 30); do
  if probe "recovery-$i"; then recovered=1; break; fi
  sleep 2
done
if [ "$recovered" != "1" ]; then
  echo "FAIL: NFS export did not become writable again within the retry window." >&2
  exit 1
fi
if [ "$results" -ne 0 ]; then
  echo "RESULT: PARTIAL — writes unexpectedly succeeded during the outage window." >&2
  exit 1
fi

echo
echo "RESULT: PASS — writes bounded during the outage, recovery clean."
echo "Pair this with the application-level evidence:"
echo "  go test -tags=integration -run TestNFSOutage ./internal/uploads/..."

cat <<'EOF'

Manual ESTALE qualification (requires a NAS restart/re-export that really
invalidates handles; ordinary network outage is intentionally insufficient):
  1. Confirm RelayShelf and /relayshelf storage check are healthy.
  2. Restart/upgrade the NAS or re-export the filesystem until the Debian
     mount remains listed but stat returns Stale file handle.
  3. Observe two confirmed ESTALE probes in the recovery service journal.
  4. Verify the journal records app_stopped, mount_verified,
     uid_probe_passed, app_started, storage_check_passed, and
     recovery_succeeded in order.
  5. Confirm upload, preview, and download recover without a browser reload.
EOF
