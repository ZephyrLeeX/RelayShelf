#!/usr/bin/env bash
set -euo pipefail

root_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
parent="$(mktemp -d)"
trap 'rm -rf -- "$parent"' EXIT
printf 'keep\n' > "$parent/important-file"

set +e
RELAYSHELF_TEST_DESTRUCTIVE=1 \
RELAYSHELF_RELEASE_TEST_SIZE=1 \
WORK_DIR="$parent" \
BASE_URL=http://127.0.0.1:1 \
E2E_USERNAME=test E2E_PASSWORD=test \
  "$root_dir/scripts/release-test-2gib.sh" >/dev/null 2>&1
status=$?
set -e
[ "$status" -ne 0 ] || { echo "release harness unexpectedly succeeded" >&2; exit 1; }
[ -f "$parent/important-file" ] || { echo "release harness deleted caller-owned content" >&2; exit 1; }
if find "$parent" -mindepth 1 -maxdepth 1 -type d -name 'relayshelf-release-test.*' | grep -q .; then
  echo "release harness left its owned child behind" >&2
  exit 1
fi

grep -Fq 'read_mbps={size/((r_end-r_start)/1e9)/1e6' "$root_dir/scripts/hardware-baseline.sh"
grep -Fq 'flush_file "$file"' "$root_dir/scripts/hardware-baseline.sh"
! grep -Eq 'sync \+ read|r_end-s_start|\|\| true' "$root_dir/scripts/hardware-baseline.sh"
grep -Fq "trace: 'off'" "$root_dir/web/playwright.config.ts"
grep -Fq "screenshot: 'off'" "$root_dir/web/playwright.config.ts"
grep -Fq "video: 'off'" "$root_dir/web/playwright.config.ts"
! grep -Eq 'playwright-report|test-results|trace\.zip' "$root_dir/.github/workflows/ci.yml"

echo "review harness safety tests: PASS"
