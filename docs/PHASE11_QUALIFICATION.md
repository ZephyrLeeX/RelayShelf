# Phase 11 Qualification Runbook

Status: implementation complete; reference-hardware execution pending.
This file lists the exact commands the Intel J4125 reference environment
must run to close the Phase 11 exit gate, and records the decisions already
made from available evidence.

## Automated coverage already in CI

- `make generate && git diff --exit-code` — contract/codegen idempotence
- `make lint` — gofmt, go vet, golangci-lint, frontend lint + typecheck
- `make test` — Go unit, frontend unit, PostgreSQL integration (real server)
- `make e2e` — Playwright journeys against the real Go binary + PostgreSQL
- `make container` — image build + metadata smoke
- Crash-window process tests (`go test -tags=integration -run TestCrashWindow ./cmd/...`)
- NFS-outage service tests (`go test -tags=integration -run TestNFSOutage ./internal/uploads/...`)
- TOTP integration tests (`go test -tags=integration -run TOTP ./internal/auth/...`)

## Reference-hardware qualification commands

Run on the Debian 13 VM (J4125) with the real NFSv4 NAS mounted, after
placing a `.relayshelf-qual-test` marker file inside each scratch directory.

### T123 — real 2 GiB release test

```bash
# With the production relayshelf instance running on the VM:
RELAYSHELF_TEST_DESTRUCTIVE=1 \
BASE_URL=https://<public-origin> \
E2E_USERNAME=<test-user> E2E_PASSWORD='<test-user-password>' \
./scripts/release-test-2gib.sh
```

Leaving `RELAYSHELF_RELEASE_TEST_SIZE` unset is what makes this the real
2 GiB qualification. The script prints `RESULT: PASS — real 2 GiB ...` only
when every stage (interrupt, resume, triple SHA-256, commit, range) passed.

### T124 — real NFS outage

```bash
mkdir -p /mnt/relayshelf/qual-test && touch /mnt/relayshelf/qual-test/.relayshelf-qual-test
RELAYSHELF_TEST_DESTRUCTIVE=1 NFS_TEST_ROOT=/mnt/relayshelf/qual-test \
  ./scripts/nfs-outage-test.sh
```

Take the NAS export offline when prompted, restore it when prompted. Pair
with the automated boundary proof:

```bash
CONTAINER_RUNTIME=podman ./scripts/test-integration.sh   # includes TestNFSOutage*
```

### T125 — crash/restart windows

Already fully automated with real child processes; CI runs them against a
containerized PostgreSQL. On the reference VM, additionally repeat once
against the production-shaped database:

```bash
DATABASE_URL=<production-shaped-url> \
  go test -tags=integration -run TestCrashWindow -count=1 ./cmd/...
```

### T126 — hardware baseline

```bash
mkdir -p /var/tmp/relayshelf-bench /mnt/relayshelf/qual-bench
touch /var/tmp/relayshelf-bench/.relayshelf-qual-test \
      /mnt/relayshelf/qual-bench/.relayshelf-qual-test
RELAYSHELF_TEST_DESTRUCTIVE=1 \
LOCAL_BENCH_DIR=/var/tmp/relayshelf-bench \
NFS_BENCH_DIR=/mnt/relayshelf/qual-bench \
DATABASE_URL=<production-database-url> \
OUTPUT=hardware-baseline-results.json \
  ./scripts/hardware-baseline.sh
```

Keep `hardware-baseline-results.json` as the qualification artifact. The
PostgreSQL search stage seeds ~100k rows and takes several minutes.

### T128 — public exposure gate

```bash
relayshelf security check
```

Every automated check must print PASS and every MANUAL line must be
resolved by an operator (TLS health at the proxy, forwarded-header rules,
and a confirmed off-machine APP_ENCRYPTION_KEY backup recorded via
`RELAYSHELF_KEY_BACKUP_CONFIRMED=yes`). All active administrators must
have confirmed TOTP enrollments; the admin status page shows the same
projection.

## T127 — performance review record

Reference defaults are unchanged:

```text
FILE_FINALIZE_CONCURRENCY=1
THUMBNAIL_WORKERS=1
MAX_ACTIVE_CHUNK_WRITES=8
pgx MaxConns=10
chunk=8MiB
```

No tuning change is justified by the evidence collected so far: the only
measurements available come from a development machine (AMD Ryzen 7800X3D,
tmpfs scratch), which the execution authority explicitly disallows as a
basis for production defaults. The reference-hardware baseline above is
the required evidence for any future change. Status:

```text
T127 IMPLEMENTATION READY
REFERENCE HARDWARE QUALIFICATION PENDING
```

## Gate status

```text
PHASE 11 CODE GATE: see CI results for this commit
PHASE 11 REFERENCE HARDWARE QUALIFICATION: PENDING (run the commands above)
PHASE 11 EXIT GATE: NOT PASSED until the reference environment executes T123-T128
```
