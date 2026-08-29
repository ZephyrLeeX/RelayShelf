# Phase 11 Qualification Runbook

Status: implementation complete; reference-hardware qualification passed.
The Phase 12 authority handoff records T123-T128 as executed and passed on the
real reference environment. This file preserves the qualification commands and
the pre-execution record that existed at the Phase 11 implementation baseline.

## Automated coverage already in CI

- Contract/codegen idempotence (`make generate`, then `git diff --exit-code`)
- Go format, vet, and lint checks (`gofmt`, `go vet ./...`, `golangci-lint run`)
- Go unit tests (`go test ./...`)
- PostgreSQL internal integration with race detection (`go test -race -tags=integration ./internal/...`)
- Review harness safety (`./scripts/review-harness-tests.sh`)
- Frontend typecheck, lint, unit tests, and build (frontend job)
- `make e2e` — Playwright journeys against the real Go binary + PostgreSQL
- Container image build and injected-metadata smoke (container job)
- Application secret logging sentinel (`DATABASE_URL=... go test -tags=integration -run TestApplicationLogsNeverContainSecrets -count=1 -v ./cmd/...`)
- Crash-window process tests (`DATABASE_URL=... RELAYSHELF_TEST_DESTRUCTIVE=1 go test -tags=integration -run TestCrashWindow -count=1 -v ./cmd/...`)
- NFS-outage service tests (included in the raced `./internal/...` integration command)
- TOTP integration tests (included in the raced `./internal/...` integration command)

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
# The default -race run requires CGO and a C compiler.
sudo apt-get install -y podman build-essential
CONTAINER_RUNTIME=podman ./scripts/test-integration.sh   # includes TestNFSOutage*
```

The harness uses the fully qualified
`docker.io/library/postgres:17-alpine` image by default so Podman does not
need an unqualified-search registry. Set `POSTGRES_TEST_IMAGE` to use a
pre-approved mirror instead. `GO_TEST_FLAGS` continues to override the default
`-race`; when race detection is requested, the harness enables CGO and reports
a missing C toolchain before starting PostgreSQL.

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
`RELAYSHELF_KEY_BACKUP_CONFIRMED=yes`). After actually qualifying the external
TLS terminator and nginx forwarding/cache/buffering rules, attest them with
`RELAYSHELF_TLS_TERMINATION_CONFIRMED=yes` and
`RELAYSHELF_PROXY_CONFIG_CONFIRMED=yes`. These values are operator attestations,
not technical verification; never set them before performing the checks. All active administrators must
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

The completed reference-hardware review retained the frozen defaults. Future
tuning changes still require new J4125/reference-NFS evidence; development-host
measurements are not authority. Status:

```text
T127 IMPLEMENTATION COMPLETE
REFERENCE HARDWARE QUALIFICATION PASSED
```

## Gate status

```text
PHASE 11 CODE IMPLEMENTATION: COMPLETE
PHASE 11 REFERENCE HARDWARE QUALIFICATION: PASSED
PHASE 11 EXIT GATE: PASSED
```
