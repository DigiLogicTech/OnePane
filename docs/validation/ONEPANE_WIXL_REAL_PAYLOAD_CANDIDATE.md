# OnePane Linux wixl — real Windows payload candidate (Stage 2)

**NON-RELEASE EXPERIMENT. DO NOT INSTALL ON A ONEPANE MACHINE.**

The CI workflow `wixl-onepane-candidate.yml` cross-compiles the real Windows x64
Backend, Service, and Desktop Go binaries with an ICO. A restricted Ubuntu 26.04
container then packages these files via `wixl`, extracts them again and compares
their bytes with the original binaries.

Run: `bash packaging/wixl/build-onepane-candidate.sh`

The MSI includes candidate ServiceInstall, ServiceControl, Registry and Upgrade
tables. They prove the static installer structures were generated, **not** that
Windows can start a service. This is deliberately a distinct MSI identity:
OnePaneWixlCandidate for service, directory and registry, and test-only
ProductCode/UpgradeCode values so it cannot upgrade the production OnePane MSI.

**Not implemented or verified:** production upgrade and component identities;
legacy EXE migration; WiX 4 storage-root selection wizard and registry search
that preserves models/projects; production Start Menu integration; executable
service runtime and dependency loading on Windows; signing; clean install,
uninstall/reinstall, repair, and persistent data preservation tests.

Never publish this MSI or treat a passing Linux CI job as Windows release QA.
The production `packaging/windows/msi/OnePane.wxs` and installer remain intact.
