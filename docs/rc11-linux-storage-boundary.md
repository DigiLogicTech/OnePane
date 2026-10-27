# Linux service storage boundary

The packaged systemd service previously allowed writes only below `/var/lib/onepane` and `/run/onepane`. A preserved configuration selecting a model pool or Project/data root on another volume could therefore fail after installation even though the paths were valid application configuration.

The backend now supports two explicit installation modes, both requiring `-config /absolute/path/config.yaml` and exiting before normal startup, SQLite migration or listeners:

- `-preview-systemd-storage`: read the configuration and dedicated `onepane` account home; print the proposed drop-in without filesystem changes.
- `-configure-systemd-storage`: Linux root only; validate the actual preserved configuration, provision missing dedicated directories, and atomically write `/etc/systemd/system/onepane.service.d/10-onepane-storage.conf`. It does not reload or start the service. The Debian postinst and standalone installer call this before their existing reload/start steps.

The drop-in includes the service home, actual data/model/Project roots and `/run/onepane`, with mount dependencies and systemd quoting for spaces, quotes and literal percent signs. Nested roots are covered by their selected parent. ProtectSystem, ProtectHome, user identity and command execution remain governed by the existing unit. Locations masked by ProtectHome, system/ephemeral locations, root-level selections, control characters and ancestor traversal are rejected with a diagnostic rather than broadening the sandbox.

Privileged writes walk directory descriptors with `O_NOFOLLOW`; existing ownership and permissions must already permit the dedicated service. Only new directories are assigned to `onepane`. Existing data, model weights, configuration, ownership, administrator drop-ins and database contents are preserved. The configuration must be a bounded, unlinked, root-owned regular file below trusted ancestors; it is rechecked after provisioning. Existing managed drop-ins must have the expected marker, safe ownership/mode and one link. FIFOs and symlinks are rejected. New drop-in writes use exclusive staging, fsync and atomic rename under a trusted directory.

Source tests cover path defaults and exclusions, literal systemd escaping, root deduplication, directory symlinks/world-writable ancestors, existing data preservation, managed/admin/symlink/hardlink/FIFO drop-ins and bounded/unlinked trusted configuration. Windows tests and Linux cross-compilation can check only portable logic/compilation. Hosted Linux execution of these filesystem tests is required separately. None of these tests proves installed systemd behavior, mount availability, rootless Podman or Ubuntu upgrade/rollback/data preservation; those acceptance gates remain open.

The existing binary rollback hook is not a database/configuration rollback. Follow the reviewed backup/migration and Windows-first/Ubuntu installed acceptance requirements before release packaging. This change does not select new storage locations or move existing data automatically.
