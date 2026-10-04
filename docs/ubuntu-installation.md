# Ubuntu installation and upgrade

The finished-product path targets a single unprivileged `onepane` system service on Ubuntu/Debian-class hosts. The installer owns the OS prerequisites needed by the OnePane runtime instead of asking the user to separately install Ollama or a container stack.

## Release artifacts

A release publishes:

```text
onepane-linux-amd64
onepane-linux-amd64.sha256
onepane-linux-arm64
onepane-linux-arm64.sha256
install.sh
upgrade.sh
```

`build-release.sh` builds static Go binaries (`CGO_ENABLED=0`) from `cmd/harnessd` and produces SHA-256 sidecars. The public installer verifies the sidecar before replacing the binary.

## Service identity

Installation creates a dedicated `onepane` system user and group, private `/var/lib/onepane` state, `/etc/onepane/config.yaml`, subordinate UID/GID ranges for rootless containers, and a rootless Podman configuration. Podman/uidmap/slirp4netns/fuse-overlayfs are installed automatically on apt-based systems.

The systemd unit runs OnePane as an unprivileged account with a private tmp directory, a read-only system filesystem except the OnePane state/runtime paths, and cgroup delegation for rootless workloads. The host daemon must permit the tightly constrained `newuidmap`/`newgidmap` setuid helpers used by rootless Podman, so systemd-wide `NoNewPrivileges` cannot be enabled on the daemon itself; the capability bounding set is limited to SETUID/SETGID for that mapping boundary. Untrusted Project containers still independently enforce `no_new_privileges`, dropped capabilities, and the sandbox invariants in the trusted runner.

## Default network exposure

The control plane and preview listener both stay on loopback by default. On a remote server, the installer prints an SSH-tunnel command and the user completes first-run setup at `http://localhost:8080`. Remote HTTPS/wildcard-preview deployment remains an explicit configuration step rather than silently exposing the daemon over clear-text LAN HTTP.

## Upgrades

The upgrade script downloads and verifies the new binary, stops the daemon, takes a quiescent pre-upgrade SQLite/binary backup under `/var/lib/onepane/backups`, atomically swaps the executable, starts OnePane, and requires the local health endpoint to recover. If start/health validation fails, the previous binary is restored.
