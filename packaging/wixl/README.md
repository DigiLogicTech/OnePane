# Ubuntu `wixl` MSI toolchain — OnePane

**Status:** Independent packaging toolchain proof of concept, **not a release installer**.

Ubuntu 26.04 LTS includes `wixl` 0.106 (from GNOME `msitools`) in the `universe` repository. For consistency across OnePane and MangaMesh, this directory defines the **same disposable, version-family-pinned Ubuntu 26.04 container**. It builds a tiny fake MSI and checks its MSI tables and extracted payload; it never registers a service, installs a package on the host or accesses installed application data.

## Run on Ubuntu / GitHub Actions

From the repository root:

```bash
bash packaging/wixl/run-container.sh OnePane
```

Requirements: an already authorised Docker CLI/daemon, network access for the initial container-image build and sufficient build cache/disk. This reuses the Ubuntu host **without a Windows VM**.

The resulting `build/wixl-toolchain-probe/OnePane-WIXL-TOOLCHAIN-PROBE-NOT-FOR-INSTALL.msi` is for internal structural checks only. **Never ship or install it.** It contains only a `probe.txt` marker; it is not a deployable OnePane package. Runtime `docker run` uses `--network none`, a read-only root filesystem, dropped Linux capabilities, non-root user, ephemeral tmpfs and explicitly bounded output mounts.

An independent `.github/workflows/wixl-toolchain.yml` also builds and validates this probe when the tooling changes, but does **not** replace existing release jobs.

If an engineer specifically wants `wixl` installed on Ubuntu 26.04 **outside Docker**, use the Ubuntu archive rather than third-party downloads:

```bash
sudo apt-get update
sudo apt-get install -y wixl msitools
wixl --version
command -v msiinfo
command -v msiextract
```

If `apt` cannot locate `wixl`, verify the Ubuntu **universe** component is enabled; do not add another distribution's package archive.

## OnePane migration / compatibility caveats

- The current `packaging/windows/msi/OnePane.wxs` is **WiX 4 schema**, whereas `wixl` accepts only a subset of classic WiX XML; it is **not** directly compatible. OnePane uses a service, per-machine configuration, secure storage-root properties, upgrade detection and a custom storage-page UI.
- OnePane's Windows x64 binaries can be cross-compiled from Go on Linux, but **cross-compilation alone does not validate** the MSI, service lifecycle, desktop launcher, first-install storage choices or protection of existing model/project data. A future standalone `wixl` manifest would require deliberate adaptation and full Windows QA.
- The current WiX4 package source, OnePane upgrades and Windows QA workflows remain unchanged.

## Required before real Linux-generated Windows installers

1. Build native Windows payloads, correctly package all files and reproduce service/launcher/registry/UI behavior with an appropriate Windows Installer schema (not necessarily the existing WiX XML).
2. Inspect MSI tables with `msiinfo`, validate extracted payloads, file architecture, upgrade codes, first-install settings, and add static regression checks.
3. Perform **real Windows x64** clean-install, upgrade, uninstall and reinstall tests in a disposable environment, with explicit protection of previously installed user data. `wixl` cannot prove Windows runtime/service behavior.
4. Only then consider replacing any existing MSI pipeline. No switch, deployment or release is authorised by this PoC.

Tools: <https://gitlab.gnome.org/GNOME/msitools>; Ubuntu package: <https://packages.ubuntu.com/resolute/wixl>.
