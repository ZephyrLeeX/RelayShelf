# RelayShelf production deploy bundle

This bundle installs the frozen production architecture: rootful system Podman
Quadlet on Debian 13, one RelayShelf container, one PostgreSQL 17 container,
PostgreSQL data on the VM local disk, and host-mounted NFSv4 storage. OpenWrt
nginx is the external TLS terminator. Docker Compose is not production
authority.

## Prerequisites

- Debian 13 VM with a fixed LAN address, cgroup v2, systemd, Podman/Quadlet,
  `findmnt`, NFS client tools, `openssl`, and at least 6 GiB RAM.
- An exact, fully-qualified RelayShelf image with a SemVer tag, for example
  `<registry>/relayshelf:0.12.0`. `latest` and unqualified names are rejected.
- An NFSv4 export whose RelayShelf directory is writable by UID/GID 65532.
- An OpenWrt nginx TLS endpoint and firewall rules allowing TCP 8080 on the VM
  only from the OpenWrt proxy/LAN management boundary. PostgreSQL 5432 is never
  published by the bundle.
- Current PostgreSQL and `APP_ENCRYPTION_KEY` backups before every upgrade.

Check the host runtime:

```bash
podman info --format '{{.Host.CgroupsVersion}}'
test "$(podman info --format '{{.Host.CgroupsVersion}}')" = v2
sudo ./scripts/verify.sh
```

## Directory layout and ownership

```text
/etc/containers/systemd/           root:root 0755  generated Quadlets
/etc/relayshelf/relayshelf.env     root:root 0600  app configuration/secrets
/etc/relayshelf/postgres.env       root:root 0600  database bootstrap secret
/var/lib/relayshelf/postgres/      999:999   0700  VM-local PostgreSQL data
/var/lib/relayshelf/staging/       65532     0750  VM-local upload staging
/mnt/relayshelf/                   65532     NAS   host NFSv4 mount
/usr/local/libexec/relayshelf-host-storage-check
```

PostgreSQL data must never be placed on NFS. The app image is additionally
forced to UID/GID 65532 and a read-only root filesystem by its Quadlet.

## Configure the NFS host mount

Install the Debian NFS client, create the mountpoint, and add an operator-
specific `/etc/fstab` entry. Replace every placeholder:

```bash
sudo apt-get install podman nfs-common util-linux openssl
sudo install -d -m 0750 /mnt/relayshelf
```

```fstab
<nas-host>:/<nas-export> /mnt/relayshelf nfs4 rw,hard,_netdev,nofail,x-systemd.mount-timeout=90s,timeo=600,retrans=2 0 0
```

Then mount and verify it. `findmnt` must report `nfs` or `nfs4`; a plain local
directory is deliberately rejected.

```bash
sudo systemctl daemon-reload
sudo mount /mnt/relayshelf
findmnt --mountpoint /mnt/relayshelf --types nfs,nfs4
sudo chown 65532:65532 /mnt/relayshelf
sudo ./libexec/relayshelf-host-storage-check /mnt/relayshelf
```

Use the NAS ownership/ACL mechanism instead of `chown` if the export uses
root-squash. The final requirement is read/write/traverse access for numeric
UID/GID 65532.

## Secrets and environment configuration

Make private working copies; the installer refuses any file not mode 0600 or
still containing `<placeholders>`.

```bash
install -m 0600 env/postgres.env.example ./postgres.env
install -m 0600 env/relayshelf.env.example ./relayshelf.env
openssl rand -base64 32
openssl rand -base64 32
```

Generate a separate high-entropy database password. URL-encode that password
when placing it in `DATABASE_URL`. The hostname must remain
`relayshelf-postgres`, which resolves only on the private Podman network.
Set `PUBLIC_ORIGIN=https://<public-domain>` and set `TRUSTED_PROXIES` to only
the OpenWrt proxy address/CIDR. Do not raise the J4125 reference concurrency
defaults in the example.

Back up `APP_ENCRYPTION_KEY` off the VM before setting
`RELAYSHELF_KEY_BACKUP_CONFIRMED=yes`. Set the TLS/proxy attestations only after
the real checks described below pass. The installer never sets attestations.

## Fresh install

The install script is intentionally fail-safe: it will not overwrite any
existing production env or Quadlet file. It pulls exact images, validates
configuration, starts healthy PostgreSQL, runs embedded migrations, checks NFS
storage as the app UID, starts RelayShelf, and checks readiness.

```bash
sudo ./scripts/install.sh \
  --image <registry>/relayshelf:<version> \
  --listen-address <debian-vm-ip> \
  --app-env ./relayshelf.env \
  --postgres-env ./postgres.env
```

Useful lifecycle commands:

```bash
sudo systemctl start relayshelf-postgres.service relayshelf-app.service
sudo systemctl stop relayshelf-app.service relayshelf-postgres.service
sudo systemctl status relayshelf-postgres.service relayshelf-app.service
sudo journalctl -u relayshelf-postgres.service -u relayshelf-app.service -f
sudo podman exec relayshelf-app /relayshelf version
sudo podman exec relayshelf-app /relayshelf healthcheck
curl --fail http://127.0.0.1:8080/health/live
curl --fail http://127.0.0.1:8080/health/ready
```

Do not put credentials in these commands. Secrets are read only from protected
environment files.

## OpenWrt nginx

Copy `nginx/openwrt-relayshelf.conf` into the OpenWrt nginx configuration and
replace `<public-domain>`, `<debian-vm-ip>`, and certificate placeholders.
Test and reload using the package's normal OpenWrt service commands. The
reference config:

- terminates HTTPS and owns HSTS;
- replaces, rather than appends, client forwarding headers;
- disables cache for private API traffic;
- disables SSE response buffering and extends its timeout;
- disables request buffering for 8 MiB upload chunks;
- disables response buffering for Range downloads;
- limits request bodies to 32 MiB, not 2 GiB.

After reload, verify login/CSRF, Secure cookies, host/origin rejection, login
rate limiting, SSE, interrupted/resumed chunk upload, ETag/Range download, and
Content-Disposition through `https://<public-domain>`. Only then may the
operator set TLS/proxy attestations and run `relayshelf security check`.

## Upgrade

Take a PostgreSQL backup and verify the off-machine encryption-key backup.
Then run:

```bash
sudo ./scripts/upgrade.sh \
  --image <registry>/relayshelf:<new-version> \
  --backup-confirmed
```

Preflight checks NFS identity/capability, local free disk, exact candidate
image and metadata, complete configuration, DB reachability/migration
direction, and explicit backup acknowledgement. Only after all checks pass does
it stop the app, run the candidate binary's embedded migrations, install the
new image unit, restart, and check readiness.

A migration failure leaves the old unit unchanged and the app stopped. A
readiness failure is reported non-zero. The old image is not deleted, and the
prior app unit is retained as:

```text
/etc/containers/systemd/relayshelf-app.container.previous.<UTC timestamp>
```

## Rollback

RelayShelf does not support automatic schema downgrade. If the new schema is
compatible with the old binary, an operator may restore the previous Quadlet,
reload systemd, and start it. Otherwise restore the matching PostgreSQL backup
and the previous Quadlet/image together:

```bash
sudo install -m 0644 \
  /etc/containers/systemd/relayshelf-app.container.previous.<UTC-timestamp> \
  /etc/containers/systemd/relayshelf-app.container
sudo systemctl daemon-reload
sudo systemctl start relayshelf-app.service
sudo podman exec --env RELAYSHELF_HEALTHCHECK_URL=http://127.0.0.1:8080/health/ready \
  relayshelf-app /relayshelf healthcheck
```

Never attempt manual `psql` schema patches or delete the old image during
incident response.

## Release metadata and bundle creation

Release engineering supplies a real SemVer and matching image tag; this bundle
does not invent `v1.0.0`:

```bash
./scripts/build-release.sh <semver> <registry>/relayshelf:<semver> <output.tar.gz>
```

The script requires a clean Git tree, injects Version/Git Commit/Build Time,
verifies the running binary output, and writes `RELEASE-METADATA` into the
archive. The exact image should be pushed by the release process after its
registry, signing, and retention policy is approved.

## Troubleshooting

- `relayshelf-app.service` absent: run `./scripts/verify.sh`, then inspect
  `journalctl -u relayshelf-app.service` and the Quadlet generator dry-run.
- NFS preflight failure: compare `findmnt --mountpoint /mnt/relayshelf` with
  `/etc/fstab`; never create a local fallback directory and bypass the check.
- PostgreSQL unhealthy: inspect its journal and the ownership/free space under
  `/var/lib/relayshelf/postgres`; 5432 remains private.
- Config failure: run the exact candidate image with the protected env file and
  `config check`; the command redacts secret values.
- Migration failure: keep the app stopped, retain logs, and diagnose the
  embedded migration failure before any restart.
- Readiness failure: live means the process exists; ready also checks DB/schema.
  Inspect both unit journals and run `migrate status` with the candidate image.
- Storage failures: run the host identity check first, then container
  `relayshelf storage check`; do not treat a local empty mountpoint as storage.

## Reference-environment qualification

Static verification does not prove the Phase 12 exit gate. A real Debian 13 VM,
rootful Podman/systemd Quadlet, PostgreSQL 17 local persistence, NFSv4 NAS, and
OpenWrt nginx must still demonstrate cold boot, mount failure safety, migration,
health/readiness, SSE, chunk upload, Range download, restart, and upgrade. Until
those runs are recorded, their status is `NOT EXECUTED / REQUIRES REFERENCE
ENVIRONMENT` and the Phase 12 exit gate is not passed.
