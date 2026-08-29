# Phase 12 deployment and release qualification

Authority baseline: `09c4dd091d111eab1630d0fff8464b902f3cb561`

Working branch: `codex/phase12-deployment-release`

Phase 11 inherited status:

```text
PHASE 11 CODE IMPLEMENTATION: COMPLETE
PHASE 11 REFERENCE HARDWARE QUALIFICATION: PASSED
PHASE 11 EXIT GATE: PASSED
```

## Repository audit at Phase 12 entry

- Entry HEAD was not the authority baseline, but the tree was clean. The exact
  baseline object was fetched and an isolated branch was created directly from
  it; no later-main implementation was adopted.
- The production Dockerfile already had Vue, Go, and distroless runtime stages,
  a non-root final image, embedded SPA, a single binary, default `serve`, and
  Version/GitCommit/BuildTime injection.
- `internal/platform/buildinfo` and CI already tested injected binary metadata.
- `relayshelf migrate`, `serve`, `storage check`, `security check`, and `version`
  existed. Startup already checked DB schema and storage capabilities.
- `infra/quadlet` and `infra/nginx` contained only `.gitkeep`; there was no
  production Quadlet, OpenWrt config, deploy bundle, install flow, upgrade
  preflight, or deployment CI.

## T130-T137 implementation coverage

### T130 — Production image

Existing multi-stage design was retained. The Go builder is pinned to 1.26.0,
OCI release labels mirror injected metadata, and the self-contained
`healthcheck` command permits a real HTTP liveness probe in distroless. Image
verification checks non-root execution, metadata, binary presence, and absence
of Node, pnpm, Go toolchains, and source-tree paths.

### T131 — Quadlet network and PostgreSQL

The bundle defines an internal Podman network and PostgreSQL 17.11 Bookworm
with an exact fully-qualified image tag, VM-local bind storage, protected env
file, healthy notification, journald, and restart/stop policy. No PostgreSQL
port is published.

### T132 — NFS host mount dependency

The App Quadlet has `RequiresMountsFor=/mnt/relayshelf` and an `ExecStartPre`
host check requiring the path to be a real `nfs`/`nfs4` mount. The app then
performs its existing read/write/fsync/rename/delete storage check as UID 65532.
A missing mount cannot silently become local shadow storage.

### T133 — App Quadlet

The app uses internal PostgreSQL DNS, binds local staging and host NFS, exposes
only HTTP 8080, enforces UID/GID 65532, a read-only root filesystem, no added
capabilities, journald, graceful stop, health startup, and conservative frozen
concurrency values.

### T134 — OpenWrt nginx reference

The reference terminates TLS/HSTS, replaces client forwarding claims, disables
private API cache, disables SSE/download buffering and chunk request buffering,
sets suitable streaming timeouts, and caps request bodies at 32 MiB.

### T135 — Deploy bundle

`deploy/` contains Quadlets, two protected env examples, host storage check,
fresh install, validation, image validation, release bundle builder, OpenWrt
reference, upgrade, rollback guidance, lifecycle commands, and troubleshooting.

### T136 — Upgrade preflight

Preflight checks NFS identity, storage capability, local free disk, exact image
availability, binary/OCI metadata, configuration, DB reachability, migration
direction, and explicit backup acknowledgement. Migration occurs before the
candidate unit is installed. Migration/readiness failures return non-zero; the
old image is retained and the previous unit is saved.

### T137 — Version metadata

Release builds require matching SemVer/image tags and a clean Git tree, inject
the real commit/build time, verify the binary, emit OCI labels, and add
`RELEASE-METADATA` to the versioned deploy archive. `latest`, unqualified image
names, and non-SemVer tags are rejected.

## Automated verification

Run:

```bash
make generate
git diff --exit-code
make lint
make test
make build
make e2e
make deploy-verify
```

Deployment-specific verification requires Podman/Quadlet >= 5.2.0 and includes
a real Quadlet generator dry-run, shell syntax, exact image rules,
non-exposure of PostgreSQL, mount dependencies, missing-NFS and missing-env
failures, render policy, nginx directives, migration-before-unit ordering,
readiness failure reporting, and previous-unit retention. CI runs this suite in
Debian 13 with its Podman package pinned to 5.4.2+ds1-2+b2. Container CI also
runs production image inspection.

## Reference-environment qualification checklist

The following must be executed on the real Debian 13/J4125, NFSv4 NAS, and
OpenWrt environment and retained as release evidence:

```text
[ ] Fresh bundle install on a clean Debian 13 VM
[ ] Cold boot with PostgreSQL/NFS/app ordering
[ ] Boot/start with NFS unavailable fails without shadow storage
[ ] PostgreSQL persistence across container and VM restart
[ ] Embedded migration and migration-failure boundary
[ ] App live and ready directly and through OpenWrt HTTPS
[ ] Forwarded host/proto/client boundary and Secure cookie/CSRF
[ ] SSE through OpenWrt without buffering/timeouts
[ ] Chunk upload/resume through OpenWrt without request buffering
[ ] Range/ETag/Content-Disposition download through OpenWrt
[ ] Normal restart and graceful stop
[ ] Successful upgrade and deliberate readiness-failure exercise
[ ] Rollback exercise using retained unit/image and matching DB backup
```

Current status:

```text
REFERENCE-ENVIRONMENT VERIFICATION: NOT EXECUTED / REQUIRES REFERENCE ENVIRONMENT
PHASE 12 EXIT GATE: NOT PASSED
```
