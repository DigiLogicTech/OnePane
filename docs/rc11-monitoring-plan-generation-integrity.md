# RC11 — Monotonic Goal plan versions prevent silent consent resurrection

A Goal's approved monitoring plan is versioned by canonical JSON SHA-256 and a monotonically increasing plan_generation. Restoring the EXACT OLD JSON can reproduce its hash but MUST NEVER restore old permission, current signal receipts or an assumed device identity.

Migration 0085 recorded original generation on human-approved paired-Node source telemetry grants. Migration 0089 extends generation to every operator-approved TCP4 permit and creates an immutable generation trigger. Existing historical grants get generation zero and deliberately fail closed until a human revokes old permission and reapproves a current exact scope. Installed models, Projects and original approval audit records are retained.

At each network effect boundary, OnePane checks original human-approved exact RFC1918 TCP address/port, Project, Goal, Workspace, source, monitoring plan, permit revision/type/timeout, status, expiry and CURRENT plan generation. An old permit cannot claim a new TCP connection, and the derived target/TLS/instrument approval path refuses to inherit old permission. Human-facing permit, target, pin and instrument inventories distinguish historical approval from CURRENT effective authority.

Read-only Goal triage excludes old-generation Node transport reports, TCP attempt results, TLS certificate possession checks, instrument key challenges and signed instrument self-reports. A self-reported measurement or Ed25519 signature proves software key possession, not physical host, independent network vantage, actual device security, same-device correlation or independent corroboration. Multiple distinct source IDs still do not establish security incident confidence or allow remediation.

## Human reauthorisation

After editing a Goal plan, old permissions remain historical even if identical JSON is restored. The human reviewer must explicitly inspect, revoke where appropriate and reissue fresh exact TCP, target, TLS, instrument and paired-Node grants under the CURRENT plan generation. Original challenge and signer scopes are still short-lived and bound to an independent human operator. No implicit grant migration, agent action, network scan, cloud fallback or live remediation is introduced.

## Source validation and remaining gates

Actual SQLite service tests create an approved exact TCP/target/TLS/instrument chain. After an edited and restored monitoring plan, every effective state becomes inactive, a fresh TCP claim is denied and an attempt to rewrite original plan generation fails. Synthetic database-only TLS and instrument signature fields exist purely to test evidence-join integrity; they are NOT true cryptographic attestations or actual physical sensor reports. Full live human API signing and test lifecycle are validated separately under unchanged plan generations.

Hosted exact-source CI #38391066762 passed Go/SQLite regressions, race checks, Windows/Ubuntu builds and MSI log reader. Physical Node trust, real hardware sensor execution, independently corroborated actual target identity, post-export immediate revocation, installed Windows ACL behaviour, low-resource soak and security code review remain UNVERIFIED. The explicit human-control and customisation UI/UX review stays LAST before authorised Windows/Ubuntu QA installer packaging. PR #227 is unmerged.
