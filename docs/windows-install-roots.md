# Windows install roots, runtime account, and signing

This is the install and privilege contract for the Windows milestone. It does
not move an existing installation.

## Roots

Program data for a new install defaults to `C:\ProgramData\OnePane`. Model
weights and projects are explicit local NTFS directories; setup does not invent
them under ProgramData. The running app reports the directories it is actually
using from `GET /v1/settings/storage-roots` and shows them read-only in
Settings → General.

An upgrade reads `HKLM\SOFTWARE\DigiLogic\OnePane` and refuses to record a
different data, model, or project root. The installed QA7 layout
(`D:\OnePaneData`, `D:\OnePaneData\models`, `D:\OnePaneData\Projects`) stays
where it is. A legacy install that recorded only `DataRoot` keeps models and
projects derived under that same root.

Before the service starts, `OnePane.Preflight.exe` checks that each selected
volume exists, is NTFS, and has enough free space. A fresh install needs 1 GiB
for program data, 8 GiB for models, and 1 GiB for projects, summed per volume,
plus 256 MiB on the program-files volume. An upgrade only requires 200 MiB
free on each existing volume. The check does not create or delete directories.

A partial root, such as `C:\ProgramData\OnePane\data\keys\node.identity` at
zero bytes, is reported and left untouched. Setup will not regenerate, repair,
or delete it. Choose a different application data directory, or cancel. The
service also refuses to start when its selected root has a partial
`node.identity` or `master.key`, and it does not write a new config or database
there.

## Model runtime account

The OnePane service remains LocalSystem. That keeps the existing key ACLs and
the installed service identity. llama-server, Colibri, OmniRoute, and the
managed llmfit helper do not inherit a full SYSTEM token.

On a LocalSystem parent, each of those children is started with a restricted
token:

- all privileges are removed except `SeChangeNotifyPrivilege`
- restricting SIDs are `NT SERVICE\OnePane` and `BUILTIN\Users`
- an allow ACE for `NT SERVICE\OnePane` is added on the model root, runtime
  home, log directory, and runtime binaries

The ACE is added beside existing ACEs. It is not applied to `data\keys` or
`data\vault`, and those protected DACLs are not rewritten. No root is moved
and no key is rotated. If the account cannot be resolved or the ACL cannot be
written, the child is not started. There is no SYSTEM fallback.

A normal interactive or test user is not LocalSystem, so this step does
nothing there.

### Migration for the current install

1. Install the newer MSI over the existing product. The data, model, and
   project roots stay on `D:\OnePaneData`.
2. Leave `C:\ProgramData\OnePane` alone if it still contains the empty
   identity file. Setup reports it and does not select it.
3. The first model, Colibri, OmniRoute, or llmfit launch after upgrade adds
   the runtime ACE and starts the child restricted. Expect that launch to take
   longer while the model directory is walked. Reparse points are not followed.
4. Confirm in Process Explorer, or an equivalent token view, that the child is
   a restricted token and does not have SYSTEM privileges such as
   `SeDebugPrivilege`, `SeBackupPrivilege`, or `SeTcbPrivilege`.
5. Run one CPU llama-server completion and, separately, one GPU completion on
   the GTX 1060. GPU device objects that grant only SYSTEM, Administrators, or
   Interactive users will fail the restricted token. The remediation is an
   allow ACE for `NT SERVICE\OnePane` on that device, not a return to SYSTEM.

## Signing

`scripts/sign-windows-payload.ps1` is the signing hook used by the Windows MSI
build. With `ONEPANE_WINDOWS_SIGN_THUMBPRINT` unset, it prints `unsigned` and
leaves the file unchanged. Set the thumbprint of a certificate already in the
machine store, and optionally `ONEPANE_WINDOWS_SIGNTOOL` and
`ONEPANE_WINDOWS_TIMESTAMP_URL`, to Authenticode-sign a payload. The MSI
authoring has no ProductVersion default; the build passes `-d ProductVersion`
explicitly and WiX stops if it is missing.

## Ubuntu draft

`packaging/debian/`, `scripts/upgrade-ubuntu.sh`, and the Linux-only service
install and storage files listed in
`packaging/windows/release-archive-excludes.txt` remain in this branch. Their
Go files are `//go:build linux`. `scripts/export-windows-release-archive.sh`
omits them from the Windows release archive. They are not deleted and they are
not compiled into the Windows service or backend.
