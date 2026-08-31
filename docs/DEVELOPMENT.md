# RelayShelf development environment

RelayShelf Dev Stack runs the real Vue frontend, Go backend, PostgreSQL,
migrations, authentication, uploads, and local filesystem storage. It is an
isolated development environment, not a production or staging deployment.

## Start

Start the loopback-only local environment:

```bash
make dev
```

The command returns after PostgreSQL, the backend, and Vite are healthy. Vue,
TypeScript, and CSS changes update through Vite HMR. Run `make dev` again after
Go changes to rebuild and restart the backend while preserving development data.

## Codex Preview

```bash
make dev-preview
```

This binds only Vite to `0.0.0.0:5173`; PostgreSQL remains on
`127.0.0.1:55432` and the Go API remains on `127.0.0.1:8080`. Open the
workspace/Codex port preview for port 5173. If the preview proxy uses a host
that Vite does not accept, explicitly allow only the required hostnames:

```bash
RELAY_SHELF_DEV_ALLOWED_HOSTS=preview.example.test make dev-preview
```

## Stop

```bash
make dev-down
```

This stops Vite and the Go backend and removes the Dev PostgreSQL container. It
keeps the database volume, storage, staging data, and persistent Dev secrets.

## Status

```bash
make dev-status
```

## Reset

```bash
make dev-reset
make dev
```

Reset removes only the `relayshelf-dev-postgres-data` volume and the contents
of `.local/dev/storage` and `.local/dev/staging`. It preserves
`.local/dev/secrets.env`, then the next start migrates a clean database and
seeds the development users again.

## Quick checks

```bash
make dev-check
```

This runs Go formatting, vet and unit tests plus frontend typecheck, lint, and
unit tests. Full CI, integration, E2E, container, and deployment checks remain
required before a release.

## Test accounts

| Role | Username | Password |
| --- | --- | --- |
| User | `e2e-alice` | `e2e-alice-pass-12345` |
| User | `e2e-bob` | `e2e-bob-pass-123456` |
| Administrator | `e2e-admin` | `e2e-admin-pass-12345` |

These users are created by the existing deterministic browser-test seed helper;
no development-only endpoint or production-binary command is added.

## Data locations

- PostgreSQL container: `relayshelf-dev-postgres`
- PostgreSQL volume: `relayshelf-dev-postgres-data`
- Runtime, PID files, logs, binaries, storage and staging: `.local/dev/*`
- Persistent secrets: `.local/dev/secrets.env` (created with mode `0600` when possible)

The encryption and CSRF secrets are generated on first start and reused so
encrypted Sensitive content remains decryptable across restarts. Secret values
are never printed and `.local/` is ignored by Git.

## Production isolation

The Dev Stack does not load `.env`, production Quadlets, production database
settings, NFS paths, or production secrets. It starts backend commands with an
explicit clean environment and overrides all database, storage, staging,
origin, listen-address, and secret settings. Never connect this stack to a
production database or reuse production NFS paths or secrets.
