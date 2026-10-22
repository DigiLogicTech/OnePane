# RC11 — physical model lifecycle and resource-reclamation acceptance

**Status: NOT PERFORMED / NOT PASSED.** This is the operator's opt-in test
protocol, not evidence from a Windows/Linux hosted CI job. The approved
physical host(s), installed data root, native runtime, backend and model must
be independently identified before any test. Do not dispatch a self-hosted
runner, start a real model, restart an installed service, terminate a process
or install a package as part of ordinary PR/push checks.

## Preconditions and safety boundary

1. Obtain explicit operator approval for the **specific host and test window**,
   including any service interruption. Preserve a backup or reversible copy of
   the selected OnePane data root, configuration, managed models and projects.
   Use a nonproduction Workspace and a known, permitted test model. Never
   remove or alter existing production model weights.
2. Confirm the reviewed full 40-character OnePane commit, OS build, service
   identity, chosen (possibly non-C:) data root, runtime backend/version,
   model digest and exact selected CPU/GPU device. Record metadata in private
   QA evidence without copying prompts, secrets, full command lines or paths.
3. Confirm *independent* process/child enumeration and CPU RSS tracking.
   For GPU/Colibri cases, a supported tool must attribute per-process VRAM
   usage to the verified device. Aggregate free VRAM alone is not process
   ownership evidence. If measurement is unavailable, report that gate as
   **unverified**, not passed.
4. Record the original local model PIDs, parent/child relationship, Node
   allocation, loopback port and source-model command fingerprint before any
   attempted stop/swap. Never identify ownership by PID alone after a crash.
   Do not kill or contact unrelated processes to obtain a clean measurement.
5. Run only one resource-heavy test model at a time on constrained hardware.
   Check RAM/VRAM availability and preserve explicit operator-selected model
   pinning, cloud spend caps and Workspace sandbox permissions. Do not enable
   implicit fallback or higher-autonomy permissions for these tests.

## Evidence capture — read-only host observation

These commands only *observe* OS process and network state. Execute locally
on the approved QA host; omit or redact model paths and command-line content
before sharing results. Neither snippet sends signals or writes files.

**Windows PowerShell (installed SCM and process tree):**

```powershell
Get-CimInstance Win32_Process |
  Select-Object ProcessId,ParentProcessId,Name |
  Sort-Object ParentProcessId,ProcessId
Get-Process | Select-Object Id,ProcessName,WorkingSet64,PrivateMemorySize64
Get-NetTCPConnection -State Listen -ErrorAction SilentlyContinue |
  Where-Object { $_.LocalAddress -eq '127.0.0.1' } |
  Select-Object LocalAddress,LocalPort,OwningProcess
# Optional: use the installed GPU vendor's per-process VRAM monitor.
# For NVIDIA only, if available: nvidia-smi --query-compute-apps=pid,used_gpu_memory --format=csv
```

**Ubuntu/Linux (host process tree and loopback ports):**

```bash
ps -eo pid,ppid,pgid,stat,rss,comm
ss -ltnp '( sport != :0 )'
# Only if installed and supported on the tested device:
# nvidia-smi --query-compute-apps=pid,used_gpu_memory --format=csv
# For AMD, collect the supported amd-smi/rocm-smi process allocation view;
# absence of such telemetry leaves the VRAM acceptance gate unverified.
```

Do not infer that a parent has released a child's GPU allocation from the
parent's RSS alone. On Windows, after service death/restart the historical
PID is not a trusted process identity without independent verification.

## Test matrix — execute only with explicit human authorisation

| Case | Operator-controlled action | Required independent observation |
| --- | --- | --- |
| CPU start and Agent Check | Start one qualified small model and run a bounded local inference | Exact owned executable/model/loopback PID, healthy readiness, no unintended cloud route; measure RSS and timing |
| GPU selected placement | Start an approved backend/model on the intended device | Backend/device and process-attributed VRAM usage agree with approved placement; record headroom |
| Idle Stop | Stop a healthy, unpinned model with no active request | Original engine **and any child** exit, local listener closes, per-process RAM/VRAM holdings are released before capacity is counted free |
| Active lease | Attempt Stop/eviction during an active request | Refusal before signalling, response not truncated, working residency and leases retained |
| Colibri -> Colibri swap | Activate a second qualified Colibri model | Exactly one resident Colibri; original Python/engine descendants release memory before replacement admission |
| Pinned Colibri | Attempt a conflicting automatic/explicit swap | Pin respected; no eviction or implied assistant-routing change |
| Pre-spawn cancellation | Cancel before OS child launch | Exact durable no-child failure, SQL NULL PID, no child start timestamp; retries use same approved model |
| Unknown-PID launch failure | Interrupt PID persistence or restore an ambiguous legacy row in an isolated test DB | Operator reconciliation required; no new engine, Stop-to-stopped transition, memory credit or silent rollback |
| Interrupted drain | Crash service during an approved model drain in isolated QA | Durable state remains unresolved; no mistaken healthy promotion, PID-only signal or automated replay |
| Unknown busy outcome | Crash service with one in-flight request | No automatic new inference or repeated external Tool effect; operator gets an actionable, redacted reconciliation reason |
| Failed replacement | Simulate a failed Colibri replacement after predecessor eviction | Rollback only after independently verified clean stop or exact no-child proof; otherwise withhold rollback rather than double allocate VRAM |
| Child survives parent | Isolate a test in which a child lives after its Python parent exits (if supported) | Do not treat parent exit as proof of GPU/RAM release; identify and reconcile owned child separately before passing |
| Service and data-root upgrade | Independently approved Windows SCM / Ubuntu systemd restart and non-C: data-root upgrade/rollback | User models, Project/Workspace data and settings survive; no foreign PID adoption or uncontrolled service replacement |

For each case, record before/during/after timestamps, the approved device,
the expected and observed process count, RSS, per-process VRAM where
supported, loopback listener, durable runtime status/revision and a
non-secret operator-visible error classification. Measure first-token
latency, decode tokens/s and Colibri cache behavior as observations, not
thresholds invented before baseline collection. Repeat the relevant cases
after service restart and after a failed launch.

## Passing criteria and stop conditions

- **PASS** a case only if its positive and adversarial postconditions are
  independently observed. A successful API response or OS signal alone does
  not establish released GPU memory, terminated child processes, or durable
  database ownership.
- **BLOCK** admission of a second model when a previous engine/child remains,
  PID identity cannot be proven, process-memory telemetry is unavailable for
  the claimed resource outcome, or the service has not preserved the exact
  original launch generation. Leave diagnostics intact for operator review.
- **STOP** physical execution on unapproved paths, foreign/untrusted PIDs,
  unexpected network access, unreconciled Tool outcomes, runaway memory,
  damaged install state or inability to restore the test environment.
  Do not automatically force-kill, prune, reinstall, cloud-fallback or retry.
- Mark CPU, CUDA, Vulkan and Colibri cases **separately** for each physical
  Node; one backend or OS never substitutes for the other. A hosted GitHub
  source-test pass remains **software-only**.

**Release dependencies:** physical Node pairing and safe reversible Level 5
postconditions, installed Windows and Ubuntu QA, final whole-branch
code/security review, mandatory LAST operator-centred UI/UX customisation
review, explicit operator approval, and only then packaging/release. This
document authorises none of those actions by itself.
