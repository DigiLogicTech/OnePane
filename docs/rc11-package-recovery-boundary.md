# Protected Debian executable recovery

`UBUNTU-RECOVERY-002` identified that legacy maintainer scripts stored executable
snapshots and rollback pointers below the service-owned application-data home.
That cannot establish a protected source for privileged upgrade operations.

Debian preinst/postinst now use `/var/lib/onepane-package-recovery`, below
checked root-owned, unlinked, non-group/world-writable ancestors. Unique
root-controlled snapshot directories retain a regular root-owned executable and its SHA-256.
Reuse requires an exact single-child snapshot path, safe directory/file owners,
modes, single file links, bounded metadata and a matching binary digest. Pointer
publication uses a private temporary file, filesystem sync and atomic rename;
unsafe existing pointers are withheld rather than followed or overwritten.
Postinst validates the protected root before cleanup as well as before fallback.
The root/snapshot directories and binary permit the unprivileged service to
traverse/execute, while all writes and private checksum/pointer metadata remain
root-controlled. Fixtures independently check executable-but-not-writable access
after dropping the child process to UID/GID65534; a root-only execute check is
insufficient for a systemd service running as `onepane`.

An active legacy service-writable recovery record prevents automatic upgrade.
Its files remain untouched; an operator must independently reconcile a trusted
candidate and the prior installed state. The scripts never import that record
automatically. Healthy upgrades retain the existing package-manager behavior;
failed configuration remains a genuine failed package upgrade even if an older
protected binary becomes healthy again.

Marked validation helpers are identical in both hooks. The mandatory hosted
Linux source family runs them as root only in new Go-managed temporary fixtures,
with separate temporary Go/module caches. It executes no maintainer-script main,
installed program, service/account operation, real upgrade or application-data
action. Cases check retained exact pointers and denial after checksum changes,
writable/foreign ownership, symlinks, hardlinks, path escape/traversal and linked
pointer destinations. This is source filesystem evidence; it does not validate
an installed Ubuntu package, rootless runtime, power loss, model lifecycle or
populated Alpha/RC upgrade/rollback.

The standalone upgrade script's service-writable previous-binary path and its
actual configured backup root, staging and custom-prefix/unit quoting still
require remediation. Binary recovery is separate from database/configuration
backup and compatibility; no automatic downgrade of migrated user data is added.
The full release and installed acceptance gates remain open.
