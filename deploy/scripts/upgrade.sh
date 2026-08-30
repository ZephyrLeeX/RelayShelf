#!/bin/sh
set -eu

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
bundle_root=$(CDPATH= cd -- "$script_dir/.." && pwd)
. "$script_dir/common.sh"

usage() {
  echo "usage: $0 --image <fully-qualified-semver-image> --backup-confirmed" >&2
  exit 2
}

image_ref=
backup_confirmed=no
while [ "$#" -gt 0 ]; do
  case "$1" in
    --image) [ "$#" -ge 2 ] || usage; image_ref=$2; shift 2 ;;
    --backup-confirmed) backup_confirmed=yes; shift ;;
    *) usage ;;
  esac
done

[ -n "$image_ref" ] || usage
[ "$backup_confirmed" = yes ] || die "upgrade requires --backup-confirmed after verifying a current PostgreSQL and encryption-key backup"
require_root
validate_image_ref "$image_ref"
for command_name in podman systemctl findmnt install df tar curl; do require_command "$command_name"; done
require_secure_file /etc/relayshelf/relayshelf.env
require_secure_file /etc/relayshelf/postgres.env
reject_placeholders /etc/relayshelf/relayshelf.env
[ -f /etc/containers/systemd/relayshelf-app.container ] || die "RelayShelf is not installed"
[ -f /etc/containers/systemd/relayshelf.network ] || die "installed RelayShelf network authority is missing"
[ -f /etc/containers/systemd/relayshelf-postgres.container ] || die "installed PostgreSQL unit is missing"

echo "PREFLIGHT 1/7: NFS mount and storage access"
"$bundle_root/libexec/relayshelf-host-storage-check" /mnt/relayshelf

echo "PREFLIGHT 2/7: local disk free space"
minimum_kib=${RELAYSHELF_UPGRADE_MIN_FREE_KIB:-1048576}
for disk_path in /var/lib/relayshelf/postgres /var/lib/relayshelf/staging; do
  available_kib=$(df -Pk "$disk_path" | awk 'NR==2 {print $4}')
  [ "$available_kib" -ge "$minimum_kib" ] || die "$disk_path has less than ${minimum_kib} KiB free"
done

echo "PREFLIGHT 3/7: exact candidate image"
podman pull "$image_ref"
CONTAINER_RUNTIME=podman "$bundle_root/scripts/verify-image.sh" "$image_ref"
version_output=$(podman run --rm --entrypoint /relayshelf "$image_ref" version 2>&1)
printf '%s\n' "$version_output"
image_tag=${image_ref##*:}
echo "$version_output" | grep -Fq "RelayShelf ${image_tag#v} (" || die "candidate binary version does not match image tag $image_tag"
echo "$version_output" | grep -q 'commit unknown' && die "candidate image has unknown Git commit metadata"
echo "$version_output" | grep -q 'built unknown' && die "candidate image has unknown build time metadata"
echo "$version_output" | grep -q 'RelayShelf development ' && die "candidate image has development version metadata"

echo "PREFLIGHT 4/7: complete candidate configuration"
podman run --rm --env-file /etc/relayshelf/relayshelf.env "$image_ref" config check

echo "PREFLIGHT 5/7: database reachability and migration direction"
podman run --rm --network relayshelf --env-file /etc/relayshelf/relayshelf.env "$image_ref" migrate status

echo "PREFLIGHT 6/7: storage capability"
podman run --rm --user 65532:65532 --env STORAGE_ROOT=/storage --volume /mnt/relayshelf:/storage:rw "$image_ref" storage check

echo "PREFLIGHT 7/7: backup acknowledgement recorded for this invocation"
echo "All upgrade preflight checks passed."

rendered=$(mktemp)
trap 'rm -f "$rendered"' EXIT HUP INT TERM
listen_address=$(sed -n 's/^PublishPort=\([^:]*\):8080:8080$/\1/p' /etc/containers/systemd/relayshelf-app.container)
[ -n "$listen_address" ] || die "installed app unit does not contain the expected LAN port binding"
"$script_dir/render-quadlet.sh" "$image_ref" "$listen_address" "$rendered"
timestamp=$(date -u +%Y%m%dT%H%M%SZ)
backup_app_unit=/etc/containers/systemd/relayshelf-app.container.previous.$timestamp
backup_network_unit=/etc/containers/systemd/relayshelf.network.previous.$timestamp
install -m 0644 /etc/containers/systemd/relayshelf-app.container "$backup_app_unit"
install -m 0644 /etc/containers/systemd/relayshelf.network "$backup_network_unit"

network_internal=$(podman network inspect --format '{{.Internal}}' relayshelf 2>/dev/null) ||
  die "installed Podman network 'relayshelf' is missing; repair the current installation before upgrading"
case "$network_internal" in true|false) ;; *) die "unexpected Internal value for relayshelf network: $network_internal" ;; esac
network_recreate=no
if [ "$network_internal" = true ] ||
   grep -Eq '^[[:space:]]*Internal[[:space:]]*=[[:space:]]*(true|yes|1)([[:space:]]|$)' /etc/containers/systemd/relayshelf.network; then
  network_recreate=yes
fi

restore_old_network() {
  echo "NETWORK ROLLBACK: restoring the previous network authority" >&2
  systemctl stop relayshelf-app.service relayshelf-postgres.service >/dev/null 2>&1 || true
  systemctl stop relayshelf-network.service >/dev/null 2>&1 || true
  podman network rm relayshelf >/dev/null 2>&1 || true
  install -m 0644 "$backup_network_unit" /etc/containers/systemd/relayshelf.network
  systemctl daemon-reload
  if systemctl start relayshelf-network.service &&
     systemctl start relayshelf-postgres.service &&
     systemctl start relayshelf-app.service; then
    echo "NETWORK ROLLBACK: previous network and services restored" >&2
    return 0
  fi
  echo "NETWORK ROLLBACK FAILED: keep the database backup safe and restore $backup_network_unit manually" >&2
  return 1
}

echo "UPGRADE 1/5: stop application"
systemctl stop relayshelf-app.service

echo "UPGRADE 2/5: install and reconcile network authority"
if [ "$network_recreate" = yes ]; then
  echo "Existing relayshelf network is internal; PostgreSQL must stop briefly so the network can be recreated."
  systemctl stop relayshelf-postgres.service
  systemctl stop relayshelf-network.service
  if ! install -m 0644 "$bundle_root/quadlet/relayshelf.network" /etc/containers/systemd/relayshelf.network ||
     ! systemctl daemon-reload; then
    restore_old_network || true
    die "could not install the replacement network authority; previous authority recovery was attempted"
  fi
  if ! podman network rm relayshelf; then
    restore_old_network || true
    die "could not remove the old relayshelf network; previous authority recovery was attempted"
  fi
  if ! systemctl start relayshelf-network.service; then
    restore_old_network || true
    die "could not create the replacement relayshelf bridge; previous authority recovery was attempted"
  fi
  new_network_internal=$(podman network inspect --format '{{.Internal}}' relayshelf 2>/dev/null) || {
    restore_old_network || true
    die "replacement relayshelf network was not created; previous authority recovery was attempted"
  }
  if [ "$new_network_internal" != false ]; then
    restore_old_network || true
    die "replacement relayshelf network is still internal; previous authority recovery was attempted"
  fi
  if ! systemctl start relayshelf-postgres.service; then
    restore_old_network || true
    die "PostgreSQL did not become healthy on the replacement network; previous authority recovery was attempted"
  fi
else
  install -m 0644 "$bundle_root/quadlet/relayshelf.network" /etc/containers/systemd/relayshelf.network
  systemctl daemon-reload
fi

echo "UPGRADE 3/5: apply embedded migrations"
if ! podman run --rm --network relayshelf --env-file /etc/relayshelf/relayshelf.env "$image_ref" migrate; then
  die "migration failed; the old app unit is unchanged and stopped, PostgreSQL is running, and the network fix is retained"
fi

echo "UPGRADE 4/5: install exact image unit and restart"
install -m 0644 "$rendered" /etc/containers/systemd/relayshelf-app.container
systemctl daemon-reload
systemctl start relayshelf-app.service

echo "UPGRADE 5/5: container, published-port, and exposure checks"
if ! podman exec --env RELAYSHELF_HEALTHCHECK_URL=http://127.0.0.1:8080/health/ready relayshelf-app /relayshelf healthcheck; then
  die "readiness failed; previous units are at $backup_app_unit and $backup_network_unit. Database downgrade is unsupported; consult rollback guidance"
fi
curl --fail --silent --show-error --max-time 10 "http://$listen_address:8080/health/live" >/dev/null ||
  die "published HTTP endpoint $listen_address:8080 is unreachable after upgrade"
curl --fail --silent --show-error --max-time 10 "http://$listen_address:8080/health/ready" >/dev/null ||
  die "published HTTP endpoint $listen_address:8080 is not ready after upgrade"
postgres_ports=$(podman port relayshelf-postgres)
[ -z "$postgres_ports" ] || die "PostgreSQL unexpectedly publishes a host port: $postgres_ports"

echo "RelayShelf upgrade complete: $image_ref"
