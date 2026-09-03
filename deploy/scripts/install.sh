#!/bin/sh
set -eu

script_dir=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
bundle_root=$(CDPATH= cd -- "$script_dir/.." && pwd)
. "$script_dir/common.sh"

usage() {
  echo "usage: $0 --image <fully-qualified-semver-image> --listen-address <debian-lan-ipv4> --app-env <file> --postgres-env <file>" >&2
  exit 2
}

image_ref=
listen_address=
app_env=
postgres_env=
while [ "$#" -gt 0 ]; do
  case "$1" in
    --image) [ "$#" -ge 2 ] || usage; image_ref=$2; shift 2 ;;
    --listen-address) [ "$#" -ge 2 ] || usage; listen_address=$2; shift 2 ;;
    --app-env) [ "$#" -ge 2 ] || usage; app_env=$2; shift 2 ;;
    --postgres-env) [ "$#" -ge 2 ] || usage; postgres_env=$2; shift 2 ;;
    *) usage ;;
  esac
done

[ -n "$image_ref" ] && [ -n "$listen_address" ] && [ -n "$app_env" ] && [ -n "$postgres_env" ] || usage
require_root
validate_image_ref "$image_ref"
require_secure_file "$app_env"
require_secure_file "$postgres_env"
reject_placeholders "$app_env"
reject_placeholders "$postgres_env"
for command_name in podman systemctl findmnt install chown tar curl timeout flock setpriv; do require_command "$command_name"; done

for target in \
  /etc/relayshelf/relayshelf.env \
  /etc/relayshelf/postgres.env \
  /etc/containers/systemd/relayshelf.network \
  /etc/containers/systemd/relayshelf-postgres.container \
  /etc/containers/systemd/relayshelf-app.container \
  /usr/local/libexec/relayshelf-host-storage-check \
  /usr/local/libexec/relayshelf-storage-common \
  /usr/local/libexec/relayshelf-storage-recover \
  /etc/systemd/system/relayshelf-storage-recovery.service \
  /etc/systemd/system/relayshelf-storage-recovery.timer; do
  [ ! -e "$target" ] || die "refusing to overwrite existing production file: $target"
done

"$bundle_root/libexec/relayshelf-host-storage-check" /mnt/relayshelf

install -d -m 0750 /etc/relayshelf /etc/containers/systemd /usr/local/libexec
install -d -m 0700 /var/lib/relayshelf/postgres
install -d -m 0750 /var/lib/relayshelf/staging
chown 999:999 /var/lib/relayshelf/postgres
chown 65532:65532 /var/lib/relayshelf/staging
install -m 0600 "$app_env" /etc/relayshelf/relayshelf.env
install -m 0600 "$postgres_env" /etc/relayshelf/postgres.env
install -m 0755 "$bundle_root/libexec/relayshelf-host-storage-check" /usr/local/libexec/relayshelf-host-storage-check
install -m 0644 "$bundle_root/libexec/relayshelf-storage-common" /usr/local/libexec/relayshelf-storage-common
install -m 0755 "$bundle_root/libexec/relayshelf-storage-recover" /usr/local/libexec/relayshelf-storage-recover
install -m 0644 "$bundle_root/systemd/relayshelf-storage-recovery.service" /etc/systemd/system/relayshelf-storage-recovery.service
install -m 0644 "$bundle_root/systemd/relayshelf-storage-recovery.timer" /etc/systemd/system/relayshelf-storage-recovery.timer
install -m 0644 "$bundle_root/quadlet/relayshelf.network" /etc/containers/systemd/relayshelf.network
install -m 0644 "$bundle_root/quadlet/relayshelf-postgres.container" /etc/containers/systemd/relayshelf-postgres.container
rendered=$(mktemp)
trap 'rm -f "$rendered"' EXIT HUP INT TERM
"$script_dir/render-quadlet.sh" "$image_ref" "$listen_address" "$rendered"
install -m 0644 "$rendered" /etc/containers/systemd/relayshelf-app.container

postgres_image=$(sed -n 's/^Image=//p' "$bundle_root/quadlet/relayshelf-postgres.container")
podman pull "$postgres_image"
podman pull "$image_ref"
CONTAINER_RUNTIME=podman "$bundle_root/scripts/verify-image.sh" "$image_ref"
podman run --rm --env-file /etc/relayshelf/relayshelf.env "$image_ref" config check

systemctl daemon-reload
systemctl start relayshelf-postgres.service
podman run --rm --network relayshelf --env-file /etc/relayshelf/relayshelf.env "$image_ref" migrate
podman run --rm --user 65532:65532 --env STORAGE_ROOT=/storage --volume /mnt/relayshelf:/storage:rw "$image_ref" storage check
systemctl start relayshelf-app.service
podman exec --env RELAYSHELF_HEALTHCHECK_URL=http://127.0.0.1:8080/health/ready relayshelf-app /relayshelf healthcheck
curl --fail --silent --show-error --max-time 10 "http://$listen_address:8080/health/live" >/dev/null ||
  die "published HTTP endpoint $listen_address:8080 is unreachable after installation"
curl --fail --silent --show-error --max-time 10 "http://$listen_address:8080/health/ready" >/dev/null ||
  die "published HTTP endpoint $listen_address:8080 is not ready after installation"
postgres_ports=$(podman port relayshelf-postgres)
[ -z "$postgres_ports" ] || die "PostgreSQL unexpectedly publishes a host port: $postgres_ports"
systemctl enable --now relayshelf-storage-recovery.timer

echo "RelayShelf installation complete. Configure OpenWrt and verify PUBLIC_ORIGIN before setting operator attestations."
