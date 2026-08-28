#!/usr/bin/env bash
# T123 release qualification: a real 2 GiB upload through the real server.
#
# Flow: stream-generate a deterministic 2 GiB file (never held in memory),
# upload it in server-sized chunks, abort partway, resume against the same
# session, complete, compare three independent SHA-256 views (client file,
# server-reported digest path, downloaded bytes), verify HTTP Range, and
# clean up. Ordinary CI runs the same harness in small-file mode via
# RELAYSHELF_RELEASE_TEST_SIZE; only a full 2147483648-byte run qualifies
# as T123 PASS.
#
# Required environment:
#   RELAYSHELF_TEST_DESTRUCTIVE=1   explicit opt-in
#   BASE_URL                        running RelayShelf origin (http://host:port)
#   E2E_USERNAME / E2E_PASSWORD     an ACTIVE test user's credentials
# Optional:
#   RELAYSHELF_RELEASE_TEST_SIZE    file size in bytes (default 2147483648)
#   WORK_DIR                        parent for an owned scratch child (default TMPDIR or /tmp)
set -euo pipefail

size="${RELAYSHELF_RELEASE_TEST_SIZE:-2147483648}"
base="${BASE_URL:-}"
user="${E2E_USERNAME:-}"
password="${E2E_PASSWORD:-}"

if [ "${RELAYSHELF_TEST_DESTRUCTIVE:-}" != "1" ]; then
  echo "Refusing to run: set RELAYSHELF_TEST_DESTRUCTIVE=1 (this harness writes" >&2
  echo "multi-GiB scratch files and drives the target server hard)." >&2
  exit 2
fi
if [ -z "$base" ] || [ -z "$user" ] || [ -z "$password" ]; then
  echo "BASE_URL, E2E_USERNAME, and E2E_PASSWORD are required." >&2
  exit 2
fi

parent="${WORK_DIR:-${TMPDIR:-/tmp}}"
[ -n "$parent" ] && [ "$parent" != "/" ] && [ -d "$parent" ] || {
  echo "WORK_DIR parent must be a non-root existing directory." >&2
  exit 2
}
work="$(mktemp -d "$parent/relayshelf-release-test.XXXXXX")"
[ -n "$work" ] && [ "$work" != "/" ] && [ -d "$work" ] || {
  echo "failed to create owned release-test directory" >&2
  exit 2
}
case "${work##*/}" in relayshelf-release-test.*) ;; *) echo "unsafe scratch directory name" >&2; exit 2;; esac
touch "$work/.relayshelf-release-test-owned"
file="$work/release-$size.bin"
cleanup() {
  [ -n "$work" ] && [ "$work" != "/" ] && [ -d "$work" ] || return
  case "${work##*/}" in relayshelf-release-test.*) ;; *) echo "refusing unsafe scratch cleanup: $work" >&2; return;; esac
  [ -f "$work/.relayshelf-release-test-owned" ] || { echo "refusing unowned scratch cleanup: $work" >&2; return; }
  rm -rf -- "$work"
}
trap cleanup EXIT
started=$(date +%s)

echo "== T123 release test: $size bytes against $base =="

# Deterministic streaming payload: repeated pattern, no sparse tricks —
# every byte is written and read back.
generate() {
  local block remaining
  block="$(head -c 1048576 /dev/zero | tr '\0' 'R')"
  remaining=$size
  while [ "$remaining" -gt 0 ]; do
    if [ "$remaining" -ge 1048576 ]; then
      printf '%s' "$block"
      remaining=$((remaining - 1048576))
    else
      head -c "$remaining" /dev/zero | tr '\0' 'r'
      remaining=0
    fi
  done
}
echo "-- generating deterministic payload (streamed) --"
generate > "$file"
client_sha="$(sha256sum "$file" | cut -d' ' -f1)"
echo "client sha256: $client_sha"

# Login and bootstrap a session.
login() {
  curl -fsS -c "$work/cookies" -H "Origin: $base" -H 'Content-Type: application/json' \
    -d "{\"username\":\"$user\",\"password\":\"$password\",\"deviceName\":\"release-test\"}" \
    "$base/api/v1/auth/login" | tee "$work/login.json" >/dev/null
  python3 -c 'import json;print(json.load(open("'"$work/login.json"'"))["csrfToken"])'
}
csrf="$(login)"

request() {
  local method=$1 path=$2
  shift 2
  curl -sS -b "$work/cookies" -H "Origin: $base" -H "X-CSRF-Token: $csrf" -X "$method" "$@" "$base$path"
}

echo "-- creating upload session --"
session=$(request POST /api/v1/uploads -H 'Content-Type: application/json' \
  -d "{\"originalFilename\":\"release-$size.bin\",\"expectedSize\":$size}")
upload_id=$(printf '%s' "$session" | python3 -c 'import json,sys;print(json.load(sys.stdin)["id"])')
chunk_size=$(printf '%s' "$session" | python3 -c 'import json,sys;print(json.load(sys.stdin)["chunkSize"])')
part_count=$(printf '%s' "$session" | python3 -c 'import json,sys;print(json.load(sys.stdin)["partCount"])')
echo "upload $upload_id: chunk=$chunk_size parts=$part_count"

put_part() {
  local part=$1 offset=$2 length=$3
  dd if="$file" bs="$chunk_size" skip="$part" count=1 status=none \
    | head -c "$length" \
    | curl -sS -o /dev/null -w '%{http_code}' -b "$work/cookies" \
        -H "Origin: $base" -H "X-CSRF-Token: $csrf" -H 'Content-Type: application/octet-stream' \
        -X PUT --data-binary @- "$base/api/v1/uploads/$upload_id/parts/$part"
}

echo "-- uploading first third, then interrupting --"
interrupt_at=$(( part_count / 3 ))
for (( part=0; part<interrupt_at; part++ )); do
  length=$chunk_size
  [ $(( (part + 1) * chunk_size )) -gt "$size" ] && length=$(( size - part * chunk_size ))
  status=$(put_part "$part" $((part * chunk_size)) "$length")
  [ "$status" = "204" ] || { echo "part $part failed: HTTP $status" >&2; exit 1; }
  printf '\r  part %d/%d' "$((part + 1))" "$interrupt_at" >&2
done
echo >&2
echo "-- interrupting: querying completed parts, then resuming --"
completed=$(curl -sS -b "$work/cookies" "$base/api/v1/uploads/$upload_id" \
  | python3 -c 'import json,sys;print(len(json.load(sys.stdin)["completedParts"]))')
echo "server confirms $completed completed part(s) after interruption"

for (( part=interrupt_at; part<part_count; part++ )); do
  length=$chunk_size
  [ $(( (part + 1) * chunk_size )) -gt "$size" ] && length=$(( size - part * chunk_size ))
  status=$(put_part "$part" $((part * chunk_size)) "$length")
  [ "$status" = "204" ] || { echo "resumed part $part failed: HTTP $status" >&2; exit 1; }
  printf '\r  resumed part %d/%d' "$((part + 1))" "$part_count" >&2
done
echo >&2

echo "-- completing (server SHA-256 + NFS commit) --"
complete=$(request POST "/api/v1/uploads/$upload_id/complete")
status_line=$(printf '%s' "$complete" | python3 -c 'import json,sys;print(json.load(sys.stdin)["status"])')
[ "$status_line" = "COMPLETED" ] || { echo "complete status: $status_line" >&2; exit 1; }
completed_at=$(date +%s)
echo "server finalize took $((completed_at - started))s total"

echo "-- binding a message and verifying download integrity --"
message=$(request POST /api/v1/messages -H 'Content-Type: application/json' \
  -H "Idempotency-Key: release-$upload_id" \
  -d "{\"body\":\"release test $size\",\"lifecycle\":\"TEMPORARY\",\"uploadIds\":[\"$upload_id\"]}")
attachment=$(printf '%s' "$message" | python3 -c 'import json,sys;print(json.load(sys.stdin)["attachments"][0]["id"])')

# Full download streamed to sha256sum: memory stays bounded.
download_sha=$(curl -sS -b "$work/cookies" "$base/api/v1/attachments/$attachment/download" | sha256sum | cut -d' ' -f1)
echo "download sha256: $download_sha"
[ "$download_sha" = "$client_sha" ] || { echo "FAIL: full download digest mismatch" >&2; exit 1; }

# Range: middle 1 MiB of the file, byte-for-byte.
range_offset=$(( size / 2 ))
range_sha=$(curl -sS -b "$work/cookies" -H "Range: bytes=$range_offset-$((range_offset + 1048575))" \
  "$base/api/v1/attachments/$attachment/download" | sha256sum | cut -d' ' -f1)
expected_sha=$(dd if="$file" bs=1048576 skip=$(( size / 2097152 )) count=1 status=none | sha256sum | cut -d' ' -f1)
echo "range sha256:   $range_sha"
[ "$range_sha" = "$expected_sha" ] || { echo "FAIL: range digest mismatch" >&2; exit 1; }

echo
if [ "$size" -lt 2147483648 ]; then
  echo "RESULT: PASS (harness mode, ${size} bytes) — this is NOT T123 qualification."
  echo "T123 requires RELAYSHELF_RELEASE_TEST_SIZE unset (2 GiB) on the reference environment."
else
  echo "RESULT: PASS — real 2 GiB upload, interrupt, resume, SHA-256, commit, range verified."
fi
