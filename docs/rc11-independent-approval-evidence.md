# RC11 P0-V — Independent approval binding for offline instrument evidence

**Source scope:** The human-mediated \`onepane-instrument-probe verify-receipt\` command now requires the original, separately retained authenticated OnePane challenge, the private human-approved one-shot plan and the enrolled instrument Ed25519 public key obtained over an independent trusted operator channel.

\`\`\`bash
./onepane-instrument-probe verify-receipt \
  --receipt ./local-probe-receipt.json \
  --challenge ./original-server-challenge.json \
  --plan ./original-private-probe-plan.json \
  --public-key YOUR_INDEPENDENTLY_VERIFIED_BASE64_PUBLIC_KEY
\`\`\`

Only the original report, nonce, scope and exact signed challenge bytes are acceptable. The verifier checks the unmodified server-approved private IPv4/port/timeout and exact challenge digest as of the report's observed time, plus the report signature under the independently enrolled key. It opens **zero sockets**, cannot repeat a spent probe and never imports an incident or authorises remediation.

**Honest boundary:** Original server documents must have been retained and transferred through an authenticated private human channel. They do not contain a separately pinned OnePane server signature; a party who can substitute *all* those inputs can mislead the reviewer. This provides audit-grade consistency checks conditional on original file authenticity, **not proof of physical execution, trustworthy network vantage, real device identity, or active Level 5 monitoring**. Paired-Node execution proof and same-target correlation remain outstanding, as do reversible response, low-resource soak, security review and the final human-centred UI/UX review before the QA build.

Source checkpoint: RC11 draft integration branch; exact-head hosted CI required before closing P0-V.
