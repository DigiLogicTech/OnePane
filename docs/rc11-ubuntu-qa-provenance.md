# Current-source Ubuntu QA packaging boundary

The `rc11-ubuntu-qa.yml` workflow packages only an explicitly reviewed current
source commit. It never restores historical Alpha archives or patches. This is
an unsigned QA candidate source path, not final release qualification. No package
was produced while adding this workflow.

The authoritative release manifest and issue #86 still require ordered code,
security and final UI review/sign-offs, then Windows installed acceptance before
Ubuntu installed QA. A passing source guard does not establish those reviews.

Before an authorized operator dispatches packaging:

1. Configure the GitHub environment `rc11-ubuntu-qa-approval` with required human
   reviewers and applicable branch restrictions. Creating a workflow reference
   does not configure environment protection or record approval.
2. Set repository variable `ONEPANE_UBUNTU_QA_REVIEWED_SHA` to the full reviewed
   commit after required code/security/final UI sign-offs.
3. Set environment variables `ONEPANE_UBUNTU_QA_APPROVAL_SHA` and
   `ONEPANE_UBUNTU_QA_APPROVAL_REFERENCE` to that exact commit and a durable bounded
   review/authorization reference. Set `ONEPANE_WINDOWS_QA_ACCEPTED_SHA` and
   `ONEPANE_WINDOWS_QA_ACCEPTANCE_REFERENCE` to the same commit and the actual
   completed Windows installed acceptance record.
4. Dispatch the workflow from that source with its full expected SHA and
   `AUTHORIZE_RC11_UBUNTU_QA`. Approve the protected environment only after checking
   the recorded references. Declarations are operator assertions, not independent
   machine verification of review or Windows acceptance evidence.

The builder refuses another event, repository, workflow path, workflow/source SHA,
missing review/Windows declaration, wrong platform/architecture/version, dirty
checkout, reused output, or output inside source. The package job also requires
the latest authoritative integration workflow run for that exact SHA to have
completed successfully. An older successful run cannot hide a newer failed,
cancelled or running integration run. No legacy workflow is dispatched.

Artifacts are assembled outside the source tree in the disposable runner's
`onepane-ubuntu-rc11-qa` directory. The builder checks Debian Package/Version/
Architecture fields, ELF64 amd64 identity, embedded Go version/revision/build-time
and VCS clean identity, and exact service/lifecycle-hook hashes against source.
It checks source cleanliness again after assembly. Failed checks fail the job;
artifact upload requires the build to succeed.

The resulting GitHub artifact contains the versioned `.deb`, package SHA-256
sidecar, `SHA256SUMS`, `candidate-manifest.json` and `QA-README.txt`. The manifest
records source SHA, workflow and integration run IDs, tools, source/package file
hashes and operator references. Its status explicitly says unsigned QA candidate,
installed acceptance not performed and physical-model acceptance not established.
Extraction for inspection never installs the package or runs its hooks.

`node scripts/test_rc11_ubuntu_qa_guard.js` exercises 39 source-level guards and
provenance checks with disposable directories and injected command results. It
covers denied dispatch/identity/review/Windows/CI/source/output cases, successful
manifest/checksum serialization, and rejected wrong package architecture, ELF,
binary provenance, lifecycle hook and final source mutation. It does not execute
Go, dpkg, the Debian builder, an installer, a service, a model or a Node. Hosted
Linux syntax/guard checks and actual approved package assembly remain distinct
evidence; installed clean/upgrade/restart/rollback/data-preservation acceptance
and the final release regression remain outstanding.
