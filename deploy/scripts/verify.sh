#!/bin/sh
set -eu

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
bundle_root=$(CDPATH= cd -- "$script_dir/.." && pwd)
. "$script_dir/common.sh"

minimum_quadlet_version=5.2.0

version_at_least() {
  awk -v found="$1" -v required="$2" 'BEGIN {
    split(found, f, "."); split(required, r, ".")
    for (i = 1; i <= 3; i++) {
      if ((f[i] + 0) > (r[i] + 0)) exit 0
      if ((f[i] + 0) < (r[i] + 0)) exit 1
    }
    exit 0
  }'
}

for script in "$script_dir"/*.sh "$bundle_root/tests"/*.sh "$bundle_root/libexec/relayshelf-host-storage-check" "$bundle_root/libexec/relayshelf-storage-common" "$bundle_root/libexec/relayshelf-storage-recover"; do
  sh -n "$script"
done

postgres_unit=$bundle_root/quadlet/relayshelf-postgres.container
app_template=$bundle_root/quadlet/relayshelf-app.container.in
network_unit=${RELAYSHELF_NETWORK_UNIT:-$bundle_root/quadlet/relayshelf.network}
nginx_config=$bundle_root/nginx/openwrt-relayshelf.conf
recovery_service=$bundle_root/systemd/relayshelf-storage-recovery.service
recovery_timer=$bundle_root/systemd/relayshelf-storage-recovery.timer

grep -Fxq 'ExecStart=/usr/local/libexec/relayshelf-storage-recover /mnt/relayshelf' "$recovery_service" || die "storage recovery service helper mismatch"
grep -Fxq 'OnUnitActiveSec=1min' "$recovery_timer" || die "storage recovery timer cadence mismatch"
grep -Fxq 'WantedBy=timers.target' "$recovery_timer" || die "storage recovery timer is not enableable"

grep -Fxq 'Image=docker.io/library/postgres:17.11-bookworm' "$postgres_unit" || die "PostgreSQL image must use the approved exact tag"
! grep -Eq '(^|:)latest([[:space:]]|$)' "$bundle_root"/quadlet/* || die "latest tag found in Quadlet"
! grep -q '^PublishPort=' "$postgres_unit" || die "PostgreSQL must not publish any host port"
grep -Fxq 'Driver=bridge' "$network_unit" || die "application network must use the bridge driver"
! grep -Eq '^[[:space:]]*Internal[[:space:]]*=[[:space:]]*(true|yes|1)([[:space:]]|$)' "$network_unit" ||
  die "a rootful internal network cannot be combined with the app PublishPort"
[ "$(grep -c '^PublishPort=' "$app_template")" -eq 1 ] || die "app must publish exactly one host port"
grep -Fxq 'PublishPort=@@RELAYSHELF_LISTEN_ADDRESS@@:8080:8080' "$app_template" ||
  die "app must publish only TCP 8080 on the configured LAN address"
grep -Fxq 'RequiresMountsFor=/mnt/relayshelf /var/lib/relayshelf/staging' "$app_template" || die "app mount dependency missing"
grep -Fq 'relayshelf-host-storage-check /mnt/relayshelf' "$app_template" || die "host NFS preflight missing"
grep -Fxq 'User=65532' "$app_template" || die "app container must enforce its non-root UID"
grep -Fxq 'Group=65532' "$app_template" || die "app container must enforce its non-root GID"
grep -Fxq 'ReadOnly=true' "$app_template" || die "app container must use a read-only root filesystem"
grep -Fxq 'NoNewPrivileges=true' "$app_template" || die "app container must prevent new privileges"
grep -Fxq 'DropCapability=all' "$app_template" || die "app container must drop all capabilities"
grep -Fxq 'Volume=/mnt/relayshelf:/storage:rw' "$app_template" || die "storage bind mount missing"
grep -Fxq 'Volume=/var/lib/relayshelf/staging:/staging:rw' "$app_template" || die "staging bind mount missing"
grep -Fxq 'HealthCmd=["/relayshelf","healthcheck"]' "$app_template" || die "app healthcheck must use shell-free exec form"
grep -Fxq 'Notify=healthy' "$app_template" || die "app must notify systemd only after becoming healthy"
grep -Fxq 'Notify=healthy' "$postgres_unit" || die "PostgreSQL must notify systemd only after becoming healthy"

for directive in \
  'client_max_body_size 32m' \
  'proxy_set_header X-Forwarded-For   $remote_addr' \
  'proxy_set_header X-Forwarded-Proto https' \
  'proxy_buffering off' \
  'proxy_request_buffering off' \
  'proxy_cache off'; do
  grep -Fq "$directive" "$nginx_config" || die "nginx directive missing: $directive"
done

tmp_dir=$(mktemp -d)
trap 'rm -rf "$tmp_dir"' EXIT HUP INT TERM
mkdir "$tmp_dir/quadlet"
cp "$network_unit" "$tmp_dir/quadlet/relayshelf.network"
cp "$bundle_root/quadlet/relayshelf-postgres.container" "$tmp_dir/quadlet/"
"$script_dir/render-quadlet.sh" docker.io/example/relayshelf:0.0.0-verification 192.0.2.10 "$tmp_dir/quadlet/relayshelf-app.container"

generator=${QUADLET_GENERATOR:-}
if [ -n "$generator" ]; then
  [ -x "$generator" ] || die "configured Quadlet generator is not executable: $generator"
else
  for candidate in \
    /usr/lib/systemd/system-generators/podman-system-generator \
    /usr/libexec/podman/quadlet; do
    if [ -x "$candidate" ]; then generator=$candidate; break; fi
  done
fi
[ -n "$generator" ] || die "Quadlet generator not found (Podman >= $minimum_quadlet_version required)"

generator_version=$("$generator" --version 2>&1 | sed -n 's/.*\([0-9][0-9]*\.[0-9][0-9]*\.[0-9][0-9]*\).*/\1/p' | head -n 1)
[ -n "$generator_version" ] || die "could not determine Quadlet generator version from: $generator"
echo "Quadlet generator: $generator (version $generator_version; minimum $minimum_quadlet_version)"
version_at_least "$generator_version" "$minimum_quadlet_version" ||
  die "Podman/Quadlet is older than the required deployment baseline. Found: $generator_version. Required: >= $minimum_quadlet_version."

generated=$tmp_dir/generated.txt
QUADLET_UNIT_DIRS="$tmp_dir/quadlet" "$generator" --dryrun >"$generated"
grep -Fq 'Requires=relayshelf-postgres.service' "$generated" || die "generated app unit does not require PostgreSQL service"
grep -Fq -- '--network-alias relayshelf-postgres' "$generated" || die "generated PostgreSQL DNS alias missing"
grep -Fq -- '--sdnotify=healthy' "$generated" || die "generated healthy startup notification missing"
grep -Fq -- '--health-cmd "[\"/relayshelf\",\"healthcheck\"]"' "$generated" ||
  die "generated app healthcheck is not the expected JSON exec form"
! grep -Fq -- '--health-cmd "/relayshelf healthcheck"' "$generated" ||
  die "generated app healthcheck would require /bin/sh -c"
grep -Fq -- '--publish 192.0.2.10:8080:8080' "$generated" || die "generated app HTTP binding mismatch"
grep -Fq 'podman network create --ignore --driver bridge relayshelf' "$generated" ||
  die "generated network is not the expected managed bridge"
! grep -Fq -- '--internal' "$generated" ||
  die "generated network still disables rootful published-port forwarding"
publish_count=$(grep -o -- '--publish ' "$generated" | wc -l | tr -d ' ')
[ "$publish_count" -eq 1 ] || die "generated units must contain exactly one published port (found $publish_count)"
echo "Quadlet generator verification: PASS"

echo "RelayShelf deployment policy verification: PASS"
