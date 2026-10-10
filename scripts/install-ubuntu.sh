#!/usr/bin/env bash
set -euo pipefail

VERSION="${ONEPANE_VERSION:-latest}"
RELEASE_BASE="${ONEPANE_RELEASE_BASE_URL:-https://github.com/DigiLogicTech/OnePane/releases}"
BINARY_OVERRIDE="${ONEPANE_BINARY:-}"
PREFIX="${ONEPANE_PREFIX:-/usr/local}"
CONFIG_DIR="${ONEPANE_CONFIG_DIR:-/etc/onepane}"
DATA_DIR="${ONEPANE_DATA_DIR:-/var/lib/onepane}"
MODEL_POOL="${ONEPANE_MODEL_POOL:-$DATA_DIR/models}"
SERVICE_NAME="onepane"

log(){ printf '[OnePane] %s\n' "$*"; }
die(){ printf '[OnePane] ERROR: %s\n' "$*" >&2; exit 1; }
[[ "${EUID:-$(id -u)}" -eq 0 ]] || die "run this installer as root (for example: sudo bash install.sh)"
[[ "$(uname -s)" == "Linux" ]] || die "this installer supports Linux only"
command -v systemctl >/dev/null || die "systemd is required for this installer"

case "$(uname -m)" in
  x86_64|amd64) ARCH=amd64 ;;
  aarch64|arm64) ARCH=arm64 ;;
  *) die "unsupported architecture: $(uname -m)" ;;
esac

if command -v apt-get >/dev/null; then
  log "Installing rootless sandbox prerequisites"
  export DEBIAN_FRONTEND=noninteractive
  apt-get update -qq
  apt-get install -y --no-install-recommends ca-certificates curl podman uidmap slirp4netns fuse-overlayfs >/dev/null
else
  command -v podman >/dev/null || die "Podman is required; automatic package installation is currently implemented for apt-based systems"
fi

if ! id onepane >/dev/null 2>&1; then
  log "Creating dedicated onepane service account"
  useradd --system --user-group --home-dir "$DATA_DIR" --create-home --shell /usr/sbin/nologin onepane
fi
ONEPANE_UID="$(id -u onepane)"
ONEPANE_GID="$(id -g onepane)"

# Rootless Podman needs subordinate UID/GID ranges. Never overwrite an existing
# administrator-defined allocation.
if ! grep -q '^onepane:' /etc/subuid 2>/dev/null; then echo 'onepane:200000:65536' >> /etc/subuid; fi
if ! grep -q '^onepane:' /etc/subgid 2>/dev/null; then echo 'onepane:200000:65536' >> /etc/subgid; fi

install -d -o onepane -g onepane -m 0700 "$DATA_DIR" "$MODEL_POOL" "$DATA_DIR/.config" "$DATA_DIR/.config/containers" "$DATA_DIR/.local" "$DATA_DIR/.local/share"
install -d -o root -g onepane -m 0750 "$CONFIG_DIR"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" 2>/dev/null && pwd || true)"
if [[ -n "$BINARY_OVERRIDE" ]]; then
  [[ -f "$BINARY_OVERRIDE" ]] || die "ONEPANE_BINARY does not exist: $BINARY_OVERRIDE"
  install -m 0755 "$BINARY_OVERRIDE" "$PREFIX/bin/onepane.new"
else
  tmp="$(mktemp -d)"; trap 'rm -rf "$tmp"' EXIT
  artifact="onepane-linux-${ARCH}"
  if [[ "$VERSION" == "latest" ]]; then
    url="$RELEASE_BASE/latest/download/$artifact"
  else
    url="$RELEASE_BASE/download/$VERSION/$artifact"
  fi
  log "Downloading OnePane $VERSION"
  curl --fail --location --proto '=https' --tlsv1.2 -o "$tmp/$artifact" "$url"
  curl --fail --location --proto '=https' --tlsv1.2 -o "$tmp/$artifact.sha256" "$url.sha256"
  (cd "$tmp" && sha256sum -c "$artifact.sha256") || die "release checksum verification failed"
  install -m 0755 "$tmp/$artifact" "$PREFIX/bin/onepane.new"
fi
mv -f "$PREFIX/bin/onepane.new" "$PREFIX/bin/onepane"

if [[ ! -f "$CONFIG_DIR/config.yaml" ]]; then
  cat > "$CONFIG_DIR/config.yaml" <<CFG
server:
  listen: "127.0.0.1:8080"
  preview_listen: "127.0.0.1:8081"
storage:
  data_dir: "$DATA_DIR"
logging:
  level: "info"
local_ai:
  model_pool_path: "$MODEL_POOL"
  idle_unload_minutes: 15
  residency_headroom_pct: 10
CFG
  chown root:onepane "$CONFIG_DIR/config.yaml"
  chmod 0640 "$CONFIG_DIR/config.yaml"
fi

cat > "$DATA_DIR/.config/containers/containers.conf" <<'CFG'
[engine]
cgroup_manager="cgroupfs"
events_logger="file"

[containers]
netns="private"
ipcns="private"
utsns="private"
CFG
chown onepane:onepane "$DATA_DIR/.config/containers/containers.conf"
chmod 0600 "$DATA_DIR/.config/containers/containers.conf"

cat > /etc/systemd/system/onepane.service <<EOF_SERVICE
[Unit]
Description=OnePane local-first AI control plane
Documentation=https://github.com/DigiLogicTech/OnePane
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=onepane
Group=onepane
ExecStart=$PREFIX/bin/onepane -config $CONFIG_DIR/config.yaml
Restart=on-failure
RestartSec=3s
TimeoutStopSec=30s
KillSignal=SIGTERM
UMask=0077
RuntimeDirectory=onepane
RuntimeDirectoryMode=0700
Environment=HOME=$DATA_DIR
Environment=ONEPANE_STARTUP_EVIDENCE_ROOT=$DATA_DIR
Environment=XDG_CONFIG_HOME=$DATA_DIR/.config
Environment=XDG_DATA_HOME=$DATA_DIR/.local/share
Environment=XDG_RUNTIME_DIR=/run/onepane
Delegate=yes
# Host daemon must permit newuidmap/newgidmap for rootless Podman.
# Untrusted Project containers independently enforce no_new_privileges.
NoNewPrivileges=no
PrivateTmp=yes
ProtectSystem=strict
ProtectHome=yes
ProtectKernelTunables=yes
ProtectKernelModules=yes
ProtectKernelLogs=yes
ProtectClock=yes
LockPersonality=yes
RestrictRealtime=yes
CapabilityBoundingSet=CAP_SETUID CAP_SETGID
AmbientCapabilities=
ReadWritePaths=$DATA_DIR /run/onepane

[Install]
WantedBy=multi-user.target
EOF_SERVICE

systemctl daemon-reload
systemctl enable --now onepane.service

log "Waiting for the local control plane"
for _ in $(seq 1 30); do
  if curl --silent --fail http://127.0.0.1:8080/v1/health >/dev/null; then
    log "OnePane is running"
    rootless="$(runuser -u onepane -- env HOME="$DATA_DIR" XDG_CONFIG_HOME="$DATA_DIR/.config" XDG_DATA_HOME="$DATA_DIR/.local/share" XDG_RUNTIME_DIR=/run/onepane podman info --format '{{.Host.Security.Rootless}}' 2>/dev/null || true)"
    [[ "$rootless" == "true" ]] || die "Podman is installed but rootless operation for the onepane service account could not be verified"
    log "Rootless Podman verified"
    cat <<MSG

Open OnePane securely from another computer with an SSH tunnel:

  ssh -L 8080:127.0.0.1:8080 -L 8081:127.0.0.1:8081 <user>@<server>

Then open:
  http://localhost:8080

The first page will create your local OnePane administrator and continue setup.
MSG
    exit 0
  fi
  sleep 1
done
systemctl --no-pager --full status onepane.service || true
die "service did not become healthy"
