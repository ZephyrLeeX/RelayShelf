#!/usr/bin/env bash
# T126 reference hardware baseline. Produces machine-readable JSON plus a
# human summary covering: VM-local sequential R/W, NFS sequential R/W, NFS
# fsync + atomic rename, 2 GiB streaming SHA-256, thumbnail pipeline, and
# PostgreSQL search latency at target scale.
#
# Required environment (all paths must already contain the safety marker
# file .relayshelf-qual-test — the script refuses to operate otherwise and
# never deletes anything except files it created itself):
#   LOCAL_BENCH_DIR       VM-local benchmark directory (e.g. /var/tmp/relayshelf-bench)
#   NFS_BENCH_DIR         directory on the real NFS export
#   DATABASE_URL          PostgreSQL the production deployment uses
#   RELAYSHELF_TEST_DESTRUCTIVE=1
# Optional:
#   SHA_TEST_BYTES (default 2147483648)
#   SEQ_TEST_BYTES (default 1073741824)
#   OUTPUT (JSON path, default ./hardware-baseline-results.json)
set -euo pipefail

fail() { echo "FAIL: $*" >&2; exit 1; }
[ "${RELAYSHELF_TEST_DESTRUCTIVE:-}" = "1" ] || fail "set RELAYSHELF_TEST_DESTRUCTIVE=1"
local_dir="${LOCAL_BENCH_DIR:-}"
nfs_dir="${NFS_BENCH_DIR:-}"
database_url="${DATABASE_URL:-}"
[ -n "$local_dir" ] && [ -d "$local_dir" ] || fail "LOCAL_BENCH_DIR must exist"
[ -n "$nfs_dir" ] && [ -d "$nfs_dir" ] || fail "NFS_BENCH_DIR must exist"
[ -n "$database_url" ] || fail "DATABASE_URL is required"
for dir in "$local_dir" "$nfs_dir"; do
  [ -f "$dir/.relayshelf-qual-test" ] || fail "missing $dir/.relayshelf-qual-test marker"
done

sha_bytes="${SHA_TEST_BYTES:-2147483648}"
seq_bytes="${SEQ_TEST_BYTES:-1073741824}"
output="${OUTPUT:-hardware-baseline-results.json}"
stamp_prefix="relayshelf-bench-$$"
timestamp="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
commit="$(git rev-parse HEAD 2>/dev/null || echo unknown)"
go_version="$(go version | awk '{print $3}')"
kernel="$(uname -r)"
cpu="$(grep -m1 'model name' /proc/cpuinfo | cut -d: -f2 | xargs || echo unknown)"
mem_total_kb="$(awk '/MemTotal/ {print $2}' /proc/cpuinfo >/dev/null 2>&1 || true; grep -m1 MemTotal /proc/meminfo | awk '{print $2}')"
local_fs="$(stat -f -c %T "$local_dir" 2>/dev/null || echo unknown)"
nfs_fs="$(stat -f -c %T "$nfs_dir" 2>/dev/null || echo unknown)"

cleanup() { rm -f "$local_dir/${stamp_prefix}"-* "$nfs_dir/${stamp_prefix}"-* 2>/dev/null || true; }
trap cleanup EXIT

json() { python3 -c 'import json,sys; print(json.dumps(sys.argv[1]))' "$1"; }
num() { python3 -c 'import sys; print(float(sys.argv[1]))' "$1"; }

# seq_rw <dir> <label>: sequential write throughput, fsync, read throughput.
seq_rw() {
  local dir=$1 file block started written synced read
  file="$dir/${stamp_prefix}-seq.bin"
  block="$(head -c 1048576 /dev/zero | tr '\0' 'B')"
  started=$(date +%s%N)
  for (( i=0; i<seq_bytes/1048576; i++ )); do printf '%s' "$block"; done > "$file"
  written=$(date +%s%N)
  sync
  synced=$(date +%s%N)
  cat "$file" > /dev/null
  read=$(date +%s%N)
  rm -f "$file"
  python3 - "$seq_bytes" "$written" "$started" "$synced" "$written" "$read" <<'PY'
import sys
size, w_end, w_start, s_end, s_start, r_end = (int(v) for v in sys.argv[1:7])
print(f"write_mbps={size/((w_end-w_start)/1e9)/1e6:.1f} fsync_s={(s_end-s_start)/1e9:.3f} read_mbps={size/((r_end-s_start)/1e9)/1e6:.1f}")
PY
}

# fsync_rename <dir>: per-op fsync and atomic rename latency on the export.
fsync_rename() {
  local dir=$1 results="" i
  for i in 1 2 3 4 5; do
    local f="$dir/${stamp_prefix}-rename-$i" started done
    started=$(date +%s%N)
    printf 'relayshelf-fsync-rename-probe' > "$f"
    sync -f "$f" 2>/dev/null || fsync "$f" 2>/dev/null || true
    mv "$f" "$f.final"
    done=$(date +%s%N)
    results="$results $(( (done - started) / 1000000 ))"
    rm -f "$f.final"
  done
  echo "latencies_ms=$(echo $results | tr ' ' ',')"
}

# stream_sha <dir>: production-shaped sequential hashing of a large file.
stream_sha() {
  local dir=$1 file block started ended remaining
  file="$dir/${stamp_prefix}-sha.bin"
  block="$(head -c 1048576 /dev/zero | tr '\0' 'S')"
  ( for (( remaining=sha_bytes; remaining>0; )); do
      if [ "$remaining" -ge 1048576 ]; then printf '%s' "$block"; remaining=$((remaining-1048576));
      else head -c "$remaining" /dev/zero | tr '\0' 's'; remaining=0; fi
    done ) > "$file"
  started=$(date +%s%N)
  sha256sum "$file" > /dev/null
  ended=$(date +%s%N)
  rm -f "$file"
  python3 - "$sha_bytes" "$ended" "$started" <<'PY'
import sys
size, e, s = (int(v) for v in sys.argv[1:4])
print(f"sha_mbps={size/((e-s)/1e9)/1e6:.1f} elapsed_s={(e-s)/1e9:.3f}")
PY
}

echo "== RelayShelf hardware baseline =="
echo "timestamp=$timestamp commit=$commit"
echo "cpu=$cpu kernel=$kernel go=$go_version"
echo "local=$local_dir fs=$local_fs"
echo "nfs=$nfs_dir fs=$nfs_fs"

local_seq="$(seq_rw "$local_dir")" || fail "local sequential benchmark failed"
nfs_seq="$(seq_rw "$nfs_dir")" || fail "nfs sequential benchmark failed"
nfs_rename_raw="$(fsync_rename "$nfs_dir")" || fail "nfs fsync/rename benchmark failed"
local_sha="$(stream_sha "$local_dir")" || fail "local sha benchmark failed"
nfs_sha="$(stream_sha "$nfs_dir")" || fail "nfs sha benchmark failed"

field() { printf '%s' "$2" | { grep -o "$1=[0-9.,]*" || true; } | head -1 | cut -d= -f2; }
local_write="$(field write_mbps "$local_seq")";   local_fsync="$(field fsync_s "$local_seq")";   local_read="$(field read_mbps "$local_seq")"
nfs_write="$(field write_mbps "$nfs_seq")";       nfs_fsync="$(field fsync_s "$nfs_seq")";        nfs_read="$(field read_mbps "$nfs_seq")"
nfs_rename="$(field latencies_ms "$nfs_rename_raw")"
local_sha_t="$(field sha_mbps "$local_sha")";     local_sha_s="$(field elapsed_s "$local_sha")"
nfs_sha_t="$(field sha_mbps "$nfs_sha")";         nfs_sha_s="$(field elapsed_s "$nfs_sha")"

echo "-- PostgreSQL search at target scale (seeds ~100k messages; slow) --"
search_log="$(mktemp)"
if DATABASE_URL="$database_url" go test -tags 'integration,searchbench' -run TestSearchBenchmark100k -v ./internal/search/... 2>&1 | tee "$search_log" >/dev/null; then
  search_status=PASS
else
  search_status=FAIL
fi
search_p95="$(grep -o 'p95=[0-9.]*[a-z]*' "$search_log" | head -1 | cut -d= -f2 || true)"
rm -f "$search_log"

echo "-- thumbnail pipeline --"
thumb_log="$(mktemp)"
if go test -tags 'integration,thumbnailbench' -run TestThumbnailBenchmark -v ./internal/files/... 2>&1 | tee "$thumb_log" >/dev/null; then
  thumb_status=PASS
else
  thumb_status=FAIL
fi
thumb_mean="$(grep -o 'mean=[0-9.]*[a-z]*' "$thumb_log" | head -1 | cut -d= -f2 || true)"
rm -f "$thumb_log"

BENCH_KV="$(mktemp)"
printf "timestamp=%s\n" "$timestamp" >> "$BENCH_KV"
printf "commit=%s\n" "$commit" >> "$BENCH_KV"
printf "go_version=%s\n" "$go_version" >> "$BENCH_KV"
printf "kernel=%s\n" "$kernel" >> "$BENCH_KV"
printf "cpu=%s\n" "$cpu" >> "$BENCH_KV"
printf "memory_total_kb=%s\n" "${mem_total_kb:-0}" >> "$BENCH_KV"
printf "local_dir=%s\n" "$local_dir" >> "$BENCH_KV"
printf "local_fs=%s\n" "$local_fs" >> "$BENCH_KV"
printf "local_write_mbps=%s\n" "$local_write" >> "$BENCH_KV"
printf "local_fsync_s=%s\n" "$local_fsync" >> "$BENCH_KV"
printf "local_read_mbps=%s\n" "$local_read" >> "$BENCH_KV"
printf "local_sha_mbps=%s\n" "$local_sha_t" >> "$BENCH_KV"
printf "local_sha_elapsed_s=%s\n" "$local_sha_s" >> "$BENCH_KV"
printf "nfs_dir=%s\n" "$nfs_dir" >> "$BENCH_KV"
printf "nfs_fs=%s\n" "$nfs_fs" >> "$BENCH_KV"
printf "nfs_write_mbps=%s\n" "$nfs_write" >> "$BENCH_KV"
printf "nfs_fsync_s=%s\n" "$nfs_fsync" >> "$BENCH_KV"
printf "nfs_read_mbps=%s\n" "$nfs_read" >> "$BENCH_KV"
printf "nfs_rename_latencies_ms=%s\n" "$nfs_rename" >> "$BENCH_KV"
printf "nfs_sha_mbps=%s\n" "$nfs_sha_t" >> "$BENCH_KV"
printf "nfs_sha_elapsed_s=%s\n" "$nfs_sha_s" >> "$BENCH_KV"
printf "sha_test_bytes=%s\n" "$sha_bytes" >> "$BENCH_KV"
printf "seq_test_bytes=%s\n" "$seq_bytes" >> "$BENCH_KV"
printf "postgres_search_status=%s\n" "$search_status" >> "$BENCH_KV"
printf "postgres_search_p95=%s\n" "${search_p95:-}" >> "$BENCH_KV"
printf "thumbnail_status=%s\n" "$thumb_status" >> "$BENCH_KV"
printf "thumbnail_mean=%s\n" "${thumb_mean:-}" >> "$BENCH_KV"

BENCH_KV="$BENCH_KV" OUTPUT="$output" python3 <<'PY'
import json, os, shlex
values = {}
with open(os.environ["BENCH_KV"]) as handle:
    for line in handle:
        if "=" in line:
            key, raw = line.rstrip("\n").split("=", 1)
            values[key] = raw
def maybe(value):
    try:
        return float(value)
    except (TypeError, ValueError):
        return value
results = {
  "schema": "relayshelf-hardware-baseline-v1",
  "timestamp": values["timestamp"],
  "app_commit": values["commit"],
  "go_version": values["go_version"],
  "kernel": values["kernel"],
  "cpu": values["cpu"],
  "memory_total_kb": maybe(values["memory_total_kb"]),
  "local": {
    "dir": values["local_dir"], "filesystem": values["local_fs"],
    "sequential_write_mbps": maybe(values["local_write_mbps"]),
    "fsync_seconds": maybe(values["local_fsync_s"]),
    "sequential_read_mbps": maybe(values["local_read_mbps"]),
    "sha256_mbps": maybe(values["local_sha_mbps"]),
    "sha256_elapsed_s": maybe(values["local_sha_elapsed_s"]),
  },
  "nfs": {
    "dir": values["nfs_dir"], "filesystem": values["nfs_fs"],
    "sequential_write_mbps": maybe(values["nfs_write_mbps"]),
    "fsync_seconds": maybe(values["nfs_fsync_s"]),
    "sequential_read_mbps": maybe(values["nfs_read_mbps"]),
    "rename_latencies_ms": maybe(values["nfs_rename_latencies_ms"]),
    "sha256_mbps": maybe(values["nfs_sha_mbps"]),
    "sha256_elapsed_s": maybe(values["nfs_sha_elapsed_s"]),
  },
  "sha_test_bytes": int(values["sha_test_bytes"]),
  "seq_test_bytes": int(values["seq_test_bytes"]),
  "postgres_search": {"status": values["postgres_search_status"], "p95_first_query": maybe(values["postgres_search_p95"] or 0), "target_p95_ms": 500},
  "thumbnail": {"status": values["thumbnail_status"], "mean_first_case": maybe(values["thumbnail_mean"] or 0)},
  "notes": "No secrets, credentials, message bodies, or user content are recorded.",
}
with open(os.environ["OUTPUT"], "w") as handle:
    json.dump(results, handle, indent=2)
print(json.dumps(results, indent=2))
PY
rm -f "$BENCH_KV"
echo "results written to $output"
[ "$search_status" = PASS ] && [ "$thumb_status" = PASS ] || exit 1
