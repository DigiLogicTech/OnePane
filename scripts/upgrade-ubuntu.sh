#!/usr/bin/env bash
set -euo pipefail
VERSION="${ONEPANE_VERSION:-latest}"
RELEASE_BASE="${ONEPANE_RELEASE_BASE_URL:-https://github.com/DigiLogicTech/OnePane/releases}"
PREFIX="${ONEPANE_PREFIX:-/usr/local}"
CONFIG_DIR="${ONEPANE_CONFIG_DIR:-/etc/onepane}"
BINARY_OVERRIDE="${ONEPANE_BINARY:-}"
RECOVERY_ROOT=/var/lib/onepane-package-recovery
[[ "${EUID:-$(id -u)}" -eq 0 ]] || { echo 'run as root' >&2; exit 1; }
case "$(uname -m)" in x86_64|amd64) ARCH=amd64;; aarch64|arm64) ARCH=arm64;; *) echo 'unsupported architecture' >&2; exit 1;; esac

# ONEPANE RECOVERY COMMON BEGIN
recovery_require_directory() {
  [ ! -L "$1" ] && [ -d "$1" ] || return 1
  [ "$(stat -c %u -- "$1")" = 0 ] || return 1
  recovery_mode="$(stat -c %a -- "$1")"
  case "$recovery_mode" in ''|*[!0-7]*) return 1 ;; esac
  [ "$((0$recovery_mode & 022))" -eq 0 ]
}

recovery_require_file() {
  [ ! -L "$1" ] && [ -f "$1" ] || return 1
  [ "$(stat -c %u -- "$1")" = 0 ] && [ "$(stat -c %h -- "$1")" = 1 ] || return 1
  recovery_mode="$(stat -c %a -- "$1")"
  case "$recovery_mode" in ''|*[!0-7]*) return 1 ;; esac
  [ "$((0$recovery_mode & 022))" -eq 0 ] || return 1
  recovery_size="$(stat -c %s -- "$1")"
  [ "$recovery_size" -gt 0 ] && [ "$recovery_size" -le "$2" ]
}

recovery_require_executable_directory() {
  recovery_require_directory "$1" || return 1
  [ "$((0$recovery_mode & 001))" -eq 1 ]
}

recovery_initialize_root() {
  # The production root is fixed below these trusted ancestors. Never place
  # executable recovery or pointer files below the service-owned data home.
  for recovery_parent in / /var /var/lib; do
    recovery_require_executable_directory "$recovery_parent" || return 1
  done
  if [ ! -e "$RECOVERY_ROOT" ] && [ ! -L "$RECOVERY_ROOT" ]; then
    mkdir -m 0755 -- "$RECOVERY_ROOT" || return 1
  fi
  recovery_require_executable_directory "$RECOVERY_ROOT"
}

recovery_candidate_valid() {
  [ "$(dirname -- "$1")" = "$RECOVERY_ROOT" ] || return 1
  recovery_leaf="$(basename -- "$1")"
  case "$recovery_leaf" in snapshot.*) ;; *) return 1 ;; esac
  case "$recovery_leaf" in *[!A-Za-z0-9._-]*) return 1 ;; esac
  recovery_require_executable_directory "$1" || return 1
  recovery_require_file "$1/onepane" 536870912 && [ -x "$1/onepane" ] || return 1
  [ "$((0$recovery_mode & 001))" -eq 1 ] || return 1
  recovery_require_file "$1/binary.sha256" 65 || return 1
  recovery_digest="$(cat -- "$1/binary.sha256")"
  [ "${#recovery_digest}" = 64 ] || return 1
  case "$recovery_digest" in *[!0-9a-f]*) return 1 ;; esac
  recovery_actual="$(sha256sum -- "$1/onepane")"
  recovery_actual="${recovery_actual%% *}"
  [ "$recovery_digest" = "$recovery_actual" ]
}

recovery_publish_pointer() {
  recovery_candidate_valid "$2" || return 1
  case "$1" in "$RECOVERY_ROOT/pending"|"$RECOVERY_ROOT/active") ;; *) return 1 ;; esac
  if [ -e "$1" ] || [ -L "$1" ]; then
    recovery_require_file "$1" 4096 || return 1
  fi
  recovery_temporary="$(mktemp "$RECOVERY_ROOT/.pointer.XXXXXXXXXX")" || return 1
  if ! printf '%s\n' "$2" > "$recovery_temporary"; then rm -f -- "$recovery_temporary"; return 1; fi
  if ! sync -f "$recovery_temporary"; then rm -f -- "$recovery_temporary"; return 1; fi
  if ! mv -fT -- "$recovery_temporary" "$1"; then rm -f -- "$recovery_temporary"; return 1; fi
  sync -f "$RECOVERY_ROOT"
}
recovery_trusted_path() {
  case "$1" in /*) ;; *) return 1 ;; esac
  [ "${#1}" -le 4096 ] || return 1
  if printf '%s' "$1" | LC_ALL=C grep -q '[[:cntrl:]]'; then return 1; fi
  case "$1" in /) ;; */../*|*/..|*/./*|*/.|*//*|*/) return 1 ;; esac
  recovery_walk="$1"
  recovery_depth=0
  while :; do
    if [ "${2:-readonly}" = executable ]; then
      recovery_require_executable_directory "$recovery_walk" || return 1
    else
      recovery_require_directory "$recovery_walk" || return 1
    fi
    if [ "$recovery_walk" = "$RECOVERY_ROOT" ]; then recovery_initialize_root; return $?; fi
    [ "$recovery_walk" != / ] || return 0
    recovery_depth=$((recovery_depth+1))
    [ "$recovery_depth" -le 128 ] || return 1
    recovery_walk="$(dirname -- "$recovery_walk")"
  done
}

recovery_snapshot_binary() {
  recovery_initialize_root || return 1
  recovery_trusted_path "$(dirname -- "$1")" executable || return 1
  recovery_require_file "$1" 536870912 || return 1
  recovery_snapshot="$(mktemp -d "$RECOVERY_ROOT/snapshot.XXXXXXXXXX")" || return 1
  cp -- "$1" "$recovery_snapshot/onepane" || return 1
  chown root:root "$recovery_snapshot/onepane" || return 1
  chmod 0755 "$recovery_snapshot/onepane" || return 1
  recovery_copied="$(sha256sum -- "$recovery_snapshot/onepane")" || return 1
  recovery_original="$(sha256sum -- "$1")" || return 1
  [ "${recovery_copied%% *}" = "${recovery_original%% *}" ] || return 1
  printf '%s\n' "${recovery_copied%% *}" > "$recovery_snapshot/binary.sha256" || return 1
  chmod 0600 "$recovery_snapshot/binary.sha256" || return 1
  chmod 0755 "$recovery_snapshot" || return 1
  sync -f "$recovery_snapshot/onepane" && sync -f "$recovery_snapshot/binary.sha256" && sync -f "$recovery_snapshot" || return 1
  recovery_candidate_valid "$recovery_snapshot" || return 1
  printf '%s\n' "$recovery_snapshot"
}

recovery_replace_binary() {
  recovery_trusted_path "$(dirname -- "$1")" readonly || return 1
  recovery_trusted_path "$(dirname -- "$2")" executable || return 1
  recovery_require_file "$1" 536870912 || return 1
  if [ -e "$2" ] || [ -L "$2" ]; then recovery_require_file "$2" 536870912 || return 1; fi
  recovery_stage="$(mktemp "$(dirname -- "$2")/.onepane-upgrade.XXXXXXXXXX")" || return 1
  if ! cp -- "$1" "$recovery_stage"; then rm -f -- "$recovery_stage"; return 1; fi
  if ! chmod 0755 "$recovery_stage"; then rm -f -- "$recovery_stage"; return 1; fi
  recovery_copied="$(sha256sum -- "$recovery_stage")" || { rm -f -- "$recovery_stage"; return 1; }
  recovery_original="$(sha256sum -- "$1")" || { rm -f -- "$recovery_stage"; return 1; }
  if [ "${recovery_copied%% *}" != "${recovery_original%% *}" ]; then rm -f -- "$recovery_stage"; return 1; fi
  if ! sync -f "$recovery_stage"; then rm -f -- "$recovery_stage"; return 1; fi
  if ! mv -fT -- "$recovery_stage" "$2"; then rm -f -- "$recovery_stage"; return 1; fi
  sync -f "$(dirname -- "$2")"
}
# ONEPANE RECOVERY COMMON END

recovery_initialize_root || { echo 'Protected executable recovery is unavailable.' >&2; exit 1; }
umask 077
if [[ -e "$RECOVERY_ROOT/standalone-upgrade.lock" || -L "$RECOVERY_ROOT/standalone-upgrade.lock" ]]; then
  recovery_require_file "$RECOVERY_ROOT/standalone-upgrade.lock" 128 || { echo 'Upgrade lock requires reconciliation.' >&2; exit 1; }
else
  printf '%s\n' 'onepane-standalone-upgrade/v1' > "$RECOVERY_ROOT/standalone-upgrade.lock"
fi
exec 9>>"$RECOVERY_ROOT/standalone-upgrade.lock"
flock -n 9 || { echo 'Another standalone upgrade is active.' >&2; exit 1; }
recovery_trusted_path "$PREFIX/bin" executable && recovery_require_file "$PREFIX/bin/onepane" 536870912 || { echo 'The installed binary prefix is not trusted or requires reconciliation.' >&2; exit 1; }
recovery_trusted_path "$CONFIG_DIR" readonly && recovery_require_file "$CONFIG_DIR/config.yaml" 65536 || { echo 'A trusted existing configuration is required.' >&2; exit 1; }

tmp="$(mktemp -d "$RECOVERY_ROOT/.incoming.XXXXXXXXXX")"
artifact="onepane-linux-${ARCH}"
service_stopped=false
replacement_attempted=false
upgrade_finished=false
previous=''

finish_upgrade() {
  result=$?
  trap - EXIT
  if [[ "$upgrade_finished" != true && "$service_stopped" == true ]]; then
    systemctl stop onepane.service >/dev/null 2>&1 || true
    if [[ "$replacement_attempted" == true ]]; then
      if recovery_candidate_valid "$previous" && recovery_replace_binary "$previous/onepane" "$PREFIX/bin/onepane"; then
        echo 'Protected previous executable restored; database/configuration were not downgraded.' >&2
      else
        echo 'Protected executable recovery failed; retain snapshots and reconcile manually.' >&2
      fi
    fi
    systemctl start onepane.service >/dev/null 2>&1 || true
  fi
  # Only remove known downloads from our new protected incoming root.
  # Unknown files and all recovery/data snapshots are retained.
  if [[ "$(dirname -- "$tmp")" == "$RECOVERY_ROOT" && "$(basename -- "$tmp")" == .incoming.* && ! -L "$tmp" ]] && recovery_require_directory "$tmp"; then
    rm -f -- "$tmp/onepane" "$tmp/$artifact" "$tmp/$artifact.sha256"
    rmdir -- "$tmp" >/dev/null 2>&1 || true
  fi
  exit "$result"
}
trap finish_upgrade EXIT

if [[ -n "$BINARY_OVERRIDE" ]]; then
  [[ -f "$BINARY_OVERRIDE" ]] || { echo 'Selected upgrade binary is unavailable.' >&2; exit 1; }
  cp -- "$BINARY_OVERRIDE" "$tmp/onepane"
else
  if [[ "$VERSION" == latest ]]; then url="$RELEASE_BASE/latest/download/$artifact"; else url="$RELEASE_BASE/download/$VERSION/$artifact"; fi
  curl --fail --location --proto '=https' --tlsv1.2 -o "$tmp/$artifact" "$url"
  curl --fail --location --proto '=https' --tlsv1.2 --max-filesize 4096 -o "$tmp/$artifact.sha256" "$url.sha256"
  checksum_record="$(cat -- "$tmp/$artifact.sha256")"
  [[ "$checksum_record" != *$'\n'* ]] || { echo 'Invalid release checksum record.' >&2; exit 1; }
  read -r expected_digest expected_name <<< "$checksum_record"
  [[ "$expected_digest" =~ ^[0-9a-f]{64}$ && ( "$expected_name" == "$artifact" || "$expected_name" == "*$artifact" ) ]] || { echo 'Release checksum must identify this exact artifact.' >&2; exit 1; }
  actual_digest="$(sha256sum -- "$tmp/$artifact")"
  [[ "${actual_digest%% *}" == "$expected_digest" ]] || { echo 'Release checksum verification failed.' >&2; exit 1; }
  mv -- "$tmp/$artifact" "$tmp/onepane"
fi
chmod 0755 "$tmp/onepane"
recovery_require_file "$tmp/onepane" 536870912 || { echo 'Upgrade binary requires reconciliation.' >&2; exit 1; }

# One read-only snapshot binds the data root and port to the same config hash.
settings="$("$tmp/onepane" -config "$CONFIG_DIR/config.yaml" -print-systemd-upgrade-settings)"
mapfile -t selected_settings <<< "$settings"
[[ "${#selected_settings[@]}" == 5 && "${selected_settings[0]}" == onepane.service-upgrade/v1 && "${selected_settings[1]}" == /* && "${selected_settings[2]}" =~ ^[0-9]+$ && "${selected_settings[2]}" -ge 1 && "${selected_settings[2]}" -le 65535 && "${selected_settings[3]}" =~ ^[0-9a-f]{64}$ ]] || { echo 'Configured service storage/health metadata is unavailable.' >&2; exit 1; }
DATA_DIR="${selected_settings[1]}"
HEALTH_PORT="${selected_settings[2]}"
HEALTH_URL="${selected_settings[4]}"
[[ "$HEALTH_URL" == "http://[::1]:$HEALTH_PORT/v1/health" || "$HEALTH_URL" == "http://localhost:$HEALTH_PORT/v1/health" || "$HEALTH_URL" =~ ^http://127\.[0-9]+\.[0-9]+\.[0-9]+:$HEALTH_PORT/v1/health$ ]] || { echo 'Upgrade health verification requires a configured loopback listener.' >&2; exit 1; }
prior_pid="$(systemctl show --property=MainPID --value onepane.service)"
[[ "$prior_pid" =~ ^[1-9][0-9]*$ ]] && systemctl is-active --quiet onepane.service && [[ "$(readlink -- "/proc/$prior_pid/exe")" == "$PREFIX/bin/onepane" ]] || { echo 'Standalone upgrade requires the active service to use this exact installed binary prefix.' >&2; exit 1; }
previous="$(recovery_snapshot_binary "$PREFIX/bin/onepane")"

service_stopped=true
systemctl stop onepane.service
# Copy into a fresh private backup as the service account, never as root
# through service-writable data. SQLite/WAL is quiescent after service stop.
backup="$(runuser -u onepane -- sh -eu -c '
# ONEPANE DATA BACKUP BEGIN
  data=$1
  current=$data
  depth=0
  while :; do
    [ ! -L "$current" ] && [ -d "$current" ] || exit 1
    [ "$current" != / ] || break
    depth=$((depth+1)); [ "$depth" -le 128 ] || exit 1
    current=$(dirname -- "$current")
  done
  umask 077
  [ ! -L "$data/state" ] && [ -d "$data/state" ] || exit 1
  [ ! -L "$data/state/harness.db" ] && [ -f "$data/state/harness.db" ] || exit 1
  if [ ! -e "$data/backups" ] && [ ! -L "$data/backups" ]; then mkdir -m 0700 -- "$data/backups"; fi
  [ ! -L "$data/backups" ] && [ -d "$data/backups" ] || exit 1
  selected=$(mktemp -d "$data/backups/pre-upgrade.XXXXXXXXXX")
  for name in harness.db harness.db-wal harness.db-shm; do
    source=$data/state/$name
    if [ -e "$source" ] || [ -L "$source" ]; then
      [ ! -L "$source" ] && [ -f "$source" ] || exit 1
      cp -p -- "$source" "$selected/$name"
    fi
  done
  printf "%s\n" "$selected"
# ONEPANE DATA BACKUP END
' onepane-data-backup "$DATA_DIR")"
echo "Retained data backup: $backup"

[[ "$("$tmp/onepane" -config "$CONFIG_DIR/config.yaml" -print-systemd-upgrade-settings)" == "$settings" ]] || { echo 'Configuration changed during backup; upgrade withheld.' >&2; exit 1; }
current_digest="$(sha256sum -- "$PREFIX/bin/onepane")"
[[ "${current_digest%% *}" == "$(cat -- "$previous/binary.sha256")" ]] || { echo 'Installed binary changed during backup; upgrade withheld.' >&2; exit 1; }
replacement_attempted=true
recovery_replace_binary "$tmp/onepane" "$PREFIX/bin/onepane"
systemctl start onepane.service
for _ in $(seq 1 30); do
  service_pid="$(systemctl show --property=MainPID --value onepane.service)"
  if [[ "$service_pid" =~ ^[1-9][0-9]*$ ]] && systemctl is-active --quiet onepane.service &&
     "$tmp/onepane" -config "$CONFIG_DIR/config.yaml" -verify-systemd-upgrade-pid "$service_pid" -upgrade-config-sha256 "${selected_settings[3]}" >/dev/null 2>&1 &&
     [[ "$(systemctl show --property=MainPID --value onepane.service)" == "$service_pid" ]] && systemctl is-active --quiet onepane.service; then
    upgrade_finished=true
    echo 'OnePane upgraded successfully'
    exit 0
  fi
  sleep 1
done
echo 'Upgrade health check failed; attempting protected executable recovery.' >&2
exit 1
