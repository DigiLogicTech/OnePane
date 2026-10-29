import assert from "node:assert/strict";
import {
  classifyTask, healthLabel, healthReport, emptyKind, emptyState, fitDecision,
  contrastRatio, themeContrastOK, gpuRole, urgencyChoice, createdTaskMessage,
  redactPreview, memoryFallback, confirmedAgo
} from "../internal/webui/static/onepane-present.js";

const waiting = classifyTask({ state: "waiting_dependency", wait: { kind: "model_resources", reason: "No eligible local model" } });
assert.equal(waiting.category, "Waiting for resources");
assert.equal(waiting.next_action_id, "setup_model");
assert.equal(waiting.reason, "No eligible local model");

const memory = classifyTask({ state: "waiting_dependency", wait: { kind: "model_resources", reason: "Not enough VRAM" } });
assert.equal(memory.next_action_id, "unload_idle");

const dependency = classifyTask({ state: "waiting_dependency", event_reason: "Predecessor still running" });
assert.equal(dependency.category, "Waiting for another task");
assert.equal(classifyTask({ state: "waiting_approval" }).category, "Needs you");
assert.equal(classifyTask({ state: "blocked" }).category, "Blocked/needs fix");
assert.equal(classifyTask({ state: "failed" }).category, "Failed");
assert.equal(classifyTask({ state: "complete" }).category, "Done");
assert.equal(classifyTask({ state: "running" }).reason, "Work is in progress.");
assert.equal(classifyTask({ state: "ready", updated_at: 1000 }, 2500).time_in_state_ms, 1500);
assert.equal(classifyTask({ state: "waiting_dependency", reason: "bearer token abc" }).reason, "Waiting for an earlier task to finish.");

assert.equal(healthLabel("ok", true).text, "Healthy");
assert.equal(healthLabel("degraded", true).text, "Degraded");
assert.equal(healthLabel("halted", true).text, "Halted");
assert.equal(healthLabel("unobserved", true).text, "Not reported");
assert.equal(healthLabel("ok", false).text, "Not reported");
const report = healthReport({
  health: { status: "degraded", reason: "watchdog heartbeat is stale", worker: "ok", assurance: "unobserved", runtime: "degraded", watchdog: "stale_or_unavailable" },
  reported: { health: true },
  confirmed: { control: Date.now() - 4000, workers: Date.now() - 4000, verification: Date.now() - 4000, runtime: Date.now() - 4000, watchdog: Date.now() - 4000 },
  now: Date.now()
});
assert.equal(report.pill.text.startsWith("Degraded"), true);
assert.equal(report.rows.length, 5);
assert.match(report.rows[0].text, /watchdog heartbeat is stale/);
assert.equal(report.rows[1].text, "Healthy");
assert.equal(report.rows[2].text, "Not reported");
assert.equal(report.rows[3].text, "Degraded");
assert.match(report.rows[4].text, /stale or unavailable/);
assert.match(report.rows[0].ago, /last confirmed/);
assert.equal(classifyTask({ state: "waiting_swap", reason: "Need 6.0 GiB RAM, 1.2 GiB free" }).next_action_id, "unload_idle");
assert.match(classifyTask({ state: "waiting_swap", reason: "Need 6.0 GiB RAM, 1.2 GiB free" }).reason, /6\.0 GiB/);
assert.equal(confirmedAgo(0, 10), "not confirmed yet");

assert.equal(emptyKind({ total: 0 }), "empty");
assert.equal(emptyKind({ total: 4, visible: 0, filtersActive: true }), "filtered");
assert.equal(emptyKind({ error: "nope" }), "error");
assert.equal(emptyState({ kind: "filtered" }).actionLabel, "Clear");
assert.equal(emptyState({ kind: "error" }).actionLabel, "Retry");

const fits = fitDecision({ neededRAM: 100, freeRAM: 200, device: "CPU" });
assert.equal(fits.id, "fits_now");
assert.match(fits.detail, /CPU/);
const later = fitDecision({ neededRAM: 150, freeRAM: 100, idleRAM: 80, device: "GPU" });
assert.equal(later.id, "fits_after_unload");
assert.equal(fitDecision({ neededRAM: 500, freeRAM: 100, idleRAM: 50 }).id, "wont_fit");
assert.equal(fitDecision({ cloud: true, consent: false }).id, "cloud_off");
assert.match(fitDecision({ cloud: true, consent: false }).detail, /will not send/);
assert.equal(fitDecision({ freeRAM: 10 }).id, "unknown");

assert.ok(contrastRatio("#17202a", "#ffffff") > 4.5);
assert.equal(themeContrastOK("#777777", "#767676").ok, false);
assert.equal(gpuRole({ name: "Intel(R) HD Graphics 530", vram_bytes: 1e9 }).label, "display only (excluded)");
assert.equal(gpuRole({ name: "NVIDIA GeForce GTX 1060", vram_bytes: 6e9 }).label, "used for models");
assert.equal(urgencyChoice("now").scheduling_class, "user_interactive");
assert.equal(urgencyChoice("background").priority, -20);
assert.match(createdTaskMessage({ state: "ready", id: "t" }), /^Created\. It is Waiting for resources\./);
assert.equal(redactPreview({ api_key: "abcd", note: "user@example.com", hash: "a".repeat(40) }).api_key, "[removed]");
assert.match(JSON.stringify(redactPreview({ note: "user@example.com" })), /\[email\]/);
assert.equal(memoryFallback(null).available, false);
assert.equal(memoryFallback({ memory_total_bytes: 100, memory_available_bytes: 40 }).segments[1].bytes, 40);

console.log("PASS: task reasons, health, empty states, fit, contrast, GPU roles, urgency, redaction");
