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
for command_name in podman systemctl findmnt install df tar; do require_command "$command_name"; done
require_secure_file /etc/relayshelf/relayshelf.env
require_secure_file /etc/relayshelf/postgres.env
reject_placeholders /etc/relayshelf/relayshelf.env
[ -f /etc/containers/systemd/relayshelf-app.container ] || die "RelayShelf is not installed"

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
backup_unit=/etc/containers/systemd/relayshelf-app.container.previous.$(date -u +%Y%m%dT%H%M%SZ)

echo "UPGRADE 1/4: stop application"
systemctl stop relayshelf-app.service

echo "UPGRADE 2/4: apply embedded migrations"
if ! podman run --rm --network relayshelf --env-file /etc/relayshelf/relayshelf.env "$image_ref" migrate; then
  die "migration failed; the old app unit is unchanged and stopped. Investigate before restarting it"
fi

echo "UPGRADE 3/4: install exact image unit and restart"
install -m 0644 /etc/containers/systemd/relayshelf-app.container "$backup_unit"
install -m 0644 "$rendered" /etc/containers/systemd/relayshelf-app.container
systemctl daemon-reload
systemctl start relayshelf-app.service

echo "UPGRADE 4/4: readiness"
if ! podman exec --env RELAYSHELF_HEALTHCHECK_URL=http://127.0.0.1:8080/health/ready relayshelf-app /relayshelf healthcheck; then
  die "readiness failed; previous unit retained at $backup_unit. Database downgrade is unsupported; consult rollback guidance"
fi

echo "RelayShelf upgrade complete: $image_ref"
