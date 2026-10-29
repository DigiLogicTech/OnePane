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

The service stays LocalSystem so the existing key ACLs and the QA7 service
identity are unchanged. The restricted-child implementation is in
`internal/runtimeaccount` and is called from llama-server, Colibri plan,
OmniRoute, and managed llmfit launch. It is **off by default**.

Set `ONEPANE_RESTRICT_MODEL_RUNTIME=1` on the service to turn it on. While that
variable is unset, children keep the current LocalSystem token. That is
deliberate: the token and ACL path has not been proven on a disposable
install, and a failed restriction would stop model launch. There is no silent
fallback to SYSTEM once the switch is on. A non-system parent does nothing
even when the switch is on.

When enabled on a LocalSystem parent, each child gets a restricted token:

- privileges removed except `SeChangeNotifyPrivilege`
- restricting SIDs `NT SERVICE\OnePane` and `BUILTIN\Users`
- an allow ACE for `NT SERVICE\OnePane` on the model root, runtime home, logs,
  and runtime binaries

The ACE is added beside existing ACEs. It is not applied to `data\keys` or
`data\vault`. No root is moved and no key is rotated.

### Migration for the current install

1. Install the newer MSI over the existing product. Do not set
   `ONEPANE_RESTRICT_MODEL_RUNTIME`. Data, model, and project roots stay on
   `D:\OnePaneData`, and model processes keep starting as they do today.
2. Leave `C:\ProgramData\OnePane` alone if it still contains the empty
   identity file. Setup reports it and does not select it.
3. After a disposable copy of the install passes the checks below, set
   `ONEPANE_RESTRICT_MODEL_RUNTIME=1` in the service environment and restart.
   The first opted-in launch adds the runtime ACE. Reparse points are not
   followed. If the account or ACL step fails, that child is not started.
4. Confirm the child token is restricted and lacks `SeDebugPrivilege`,
   `SeBackupPrivilege`, and `SeTcbPrivilege`.
5. Run one CPU completion and one GTX 1060 GPU completion. If CUDA cannot open
   the device, add an allow ACE for `NT SERVICE\OnePane` on that device. Do
   not turn the switch off as a silent fallback inside the product; unset the
   variable only as an operator rollback of this deferred step.

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
