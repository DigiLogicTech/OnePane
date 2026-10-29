/* Pure presentation helpers for the honest UI. No DOM, no network.
   The Go status and memory endpoints are the source of truth when they respond.
   These functions keep the same words when a field is missing. */

export function healthLabel(status, reported) {
  if (!reported) return { text: "Not reported", kind: "warn" };
  const value = String(status || "").toLowerCase();
  if (value === "ok" || value === "healthy") return { text: "Healthy", kind: "good" };
  if (value === "halted") return { text: "Halted", kind: "bad" };
  if (value === "degraded") return { text: "Degraded", kind: "bad" };
  if (value === "unknown") return { text: "Unknown", kind: "warn" };
  if (value === "unobserved" || value === "") return { text: "Not reported", kind: "warn" };
  return { text: "Degraded", kind: "bad", detail: value.replaceAll("_", " ") };
}

export function confirmedAgo(at, now) {
  const ts = Number(at);
  if (!Number.isFinite(ts) || ts <= 0) return "not confirmed yet";
  const seconds = Math.max(0, Math.round((Number(now) - ts) / 1000));
  if (seconds < 5) return "last confirmed just now";
  if (seconds < 90) return `last confirmed ${seconds}s ago`;
  const minutes = Math.round(seconds / 60);
  return `last confirmed ${minutes} min ago`;
}

export function healthReport({ health, reported = {}, confirmed = {}, now = Date.now(), components = null } = {}) {
  const byID = new Map();
  const supplied = Array.isArray(components) ? components : health?.components;
  if (Array.isArray(supplied)) {
    for (const row of supplied) {
      if (row && row.id) byID.set(String(row.id), row);
    }
  }
  const control = partLabel(health?.status, health?.reason, !!reported.health);
  const rows = [
    { id: "control", label: "Control plane", ...control },
    { id: "workers", label: "Workers", ...partLabel(health?.worker, "", health?.worker != null && health.worker !== "") },
    { id: "verification", label: "Verification", ...partLabel(health?.assurance, "", health?.assurance != null && health.assurance !== "") },
    { id: "runtime", label: "Model runtime", ...partLabel(health?.runtime, "", health?.runtime != null && health.runtime !== "") },
    { id: "watchdog", label: "Watchdog", ...partLabel(health?.watchdog, "", health?.watchdog != null && health.watchdog !== "") }
  ].map(row => {
    const extra = byID.get(row.id);
    if (extra) {
      const mapped = healthLabel(extra.status || extra.value, extra.reported !== false);
      return { ...row, text: extra.text || mapped.text, kind: extra.kind || mapped.kind, ago: confirmedAgo(extra.confirmed_at || confirmed[row.id], now) };
    }
    return { ...row, ago: confirmedAgo(confirmed[row.id], now) };
  });
  return { pill: control, rows };
}

function partLabel(status, reason, reported) {
  const mapped = healthLabel(status, reported);
  const extra = String(reason || mapped.detail || "").replace(/[\r\n]/g, " ").trim().slice(0, 240);
  const text = extra && mapped.text !== "Healthy" ? `${mapped.text}: ${extra}` : mapped.text;
  return { text, kind: mapped.kind };
}

export function emptyState({ kind, title, detail, actionLabel }) {
  const names = {
    empty: { title: title || "Nothing here yet", detail: detail || "There is nothing to show." },
    filtered: { title: title || "Filters are hiding results", detail: detail || "Clear the filters to see everything that loaded." },
    error: { title: title || "Could not load", detail: detail || "The request failed." }
  };
  const row = names[kind] || names.empty;
  return { kind: names[kind] ? kind : "empty", title: row.title, detail: row.detail, actionLabel: actionLabel || (kind === "filtered" ? "Clear" : kind === "error" ? "Retry" : "") };
}

export function emptyKind({ error = "", loaded = true, total = 0, visible = 0, filtersActive = false } = {}) {
  if (error) return "error";
  if (!loaded) return "error";
  if (total <= 0) return "empty";
  if (visible <= 0 && filtersActive) return "filtered";
  if (visible <= 0) return "empty";
  return "ready";
}

const RESOURCE_WORDS = /memory|vram|ram|model/i;

export function classifyTask(task = {}, now = Date.now()) {
  const state = String(task.state || "").toLowerCase();
  const wait = task.wait || {};
  let reason = cleanReason(wait.reason) || cleanReason(task.event_reason) || cleanReason(task.reason);
  const resource = wait.kind === "model_resources" || wait.kind === "toolchain_resources" || RESOURCE_WORDS.test(reason);
  const memory = /memory|vram|ram|unload/i.test(reason);
  let category = "Waiting for resources";
  let fallback = "In the queue. No resource hold has been reported.";
  let action = "";
  let actionID = "";
  if (state === "running" || state === "verifying" || state === "completion_requested") {
    category = "Running"; fallback = "Work is in progress.";
  } else if (state === "waiting_approval") {
    category = "Needs you"; fallback = "This task needs your approval before it can continue."; action = "Approve"; actionID = "approve";
  } else if (state === "paused") {
    category = "Needs you"; fallback = "Paused. It will not continue until you resume it."; action = "Inspect"; actionID = "inspect";
  } else if (state === "waiting_swap") {
    category = "Waiting for resources";
    fallback = "Waiting for memory before another model can load.";
    action = "Unload idle model"; actionID = "unload_idle";
  } else if (state === "waiting_dependency" && resource) {
    category = "Waiting for resources";
    if (memory) { fallback = "Waiting for memory to free up."; action = "Unload idle model"; actionID = "unload_idle"; }
    else { fallback = "Waiting for a model or for memory to free up."; action = "Set up a model"; actionID = "setup_model"; }
  } else if (state === "waiting_dependency") {
    category = "Waiting for another task"; fallback = "Waiting for an earlier task to finish.";
  } else if (state === "blocked") {
    category = "Blocked/needs fix"; fallback = "Blocked. Something needs a fix before this can continue."; action = "Review the blocker"; actionID = "inspect";
  } else if (state === "failed") {
    category = "Failed"; fallback = "Failed. The last attempt did not finish."; action = "Review the failure"; actionID = "inspect";
  } else if (state === "complete") {
    category = "Done"; fallback = "Finished.";
  } else if (["cancelled", "cancelling", "cancel_requested", "compensating"].includes(state)) {
    category = "Stopped"; fallback = "Cancelled. No further work will run.";
  } else if (state && state !== "created" && state !== "ready") {
    fallback = "This state has no clearer explanation yet.";
  }
  const updated = Number(task.updated_at || 0);
  return {
    task_id: task.id || "",
    category,
    reason: reason || fallback,
    next_action: action,
    next_action_id: actionID,
    time_in_state_ms: updated > 0 && now >= updated ? now - updated : 0
  };
}

function cleanReason(value) {
  const text = String(value || "").trim();
  if (!text || text.length > 180 || /password|secret|token|bearer|api_key|authorization|cookie/i.test(text)) return "";
  return text;
}

export function fitDecision({ neededRAM = null, neededVRAM = null, freeRAM = null, freeVRAM = null, idleRAM = 0, idleVRAM = 0, device = "", cloud = false, consent = false } = {}) {
  const place = /gpu|partial|offload/i.test(device) ? "GPU" : device ? "CPU" : "";
  if (cloud && !consent) {
    return { id: "cloud_off", label: "Cloud only (off)", detail: "Cloud use is off. OnePane will not send this model to a provider." };
  }
  const ramKnown = Number.isFinite(Number(neededRAM)) && Number(neededRAM) > 0;
  const vramKnown = Number.isFinite(Number(neededVRAM)) && Number(neededVRAM) > 0;
  if (!ramKnown && !vramKnown) {
    return { id: "unknown", label: "Size not reported", detail: "Live memory is in the header. This catalogue entry did not report a size, so fit is not guessed." };
  }
  const freeR = Number(freeRAM);
  const freeV = Number(freeVRAM);
  const needR = ramKnown ? Number(neededRAM) : 0;
  const needV = vramKnown ? Number(neededVRAM) : 0;
  const ramOK = !ramKnown || (Number.isFinite(freeR) && needR <= freeR);
  const vramOK = !vramKnown || (Number.isFinite(freeV) && needV <= freeV);
  const ramAfter = !ramKnown || (Number.isFinite(freeR) && needR <= freeR + Number(idleRAM || 0));
  const vramAfter = !vramKnown || (Number.isFinite(freeV) && needV <= freeV + Number(idleVRAM || 0));
  const where = place ? ` on ${place}` : "";
  if (ramOK && vramOK && (Number.isFinite(freeR) || Number.isFinite(freeV))) {
    return { id: "fits_now", label: "Fits now", detail: `Fits in free memory${where}.` };
  }
  if (ramAfter && vramAfter && (Number(idleRAM) > 0 || Number(idleVRAM) > 0)) {
    return { id: "fits_after_unload", label: "Fits after unloading idle models", detail: `Free memory is short${where}. Unloading an idle model would make room.` };
  }
  if (!Number.isFinite(freeR) && !Number.isFinite(freeV)) {
    return { id: "unknown", label: "Fit not measured", detail: "Live free memory has not been reported yet." };
  }
  return { id: "wont_fit", label: "Won't fit", detail: `Does not fit in free memory${where}, even after unloading idle models.` };
}

export function contrastRatio(foreground, background) {
  const a = relativeLuminance(foreground);
  const b = relativeLuminance(background);
  if (a == null || b == null) return null;
  const hi = Math.max(a, b);
  const lo = Math.min(a, b);
  return (hi + 0.05) / (lo + 0.05);
}

function relativeLuminance(color) {
  const hex = String(color || "").trim().replace(/^#/, "");
  const full = hex.length === 3 ? hex.split("").map(ch => ch + ch).join("") : hex.slice(0, 6);
  if (!/^[0-9a-f]{6}$/i.test(full)) return null;
  const channels = [0, 2, 4].map(i => parseInt(full.slice(i, i + 2), 16) / 255).map(v => v <= 0.03928 ? v / 12.92 : ((v + 0.055) / 1.055) ** 2.4);
  return 0.2126 * channels[0] + 0.7152 * channels[1] + 0.0722 * channels[2];
}

export function themeContrastOK(foreground, background) {
  const ratio = contrastRatio(foreground, background);
  if (ratio == null) return { ok: false, ratio: null, detail: "Contrast could not be measured." };
  if (ratio < 4.5) return { ok: false, ratio, detail: `Text contrast is ${ratio.toFixed(2)}:1, below 4.5:1.` };
  return { ok: true, ratio, detail: `Text contrast is ${ratio.toFixed(2)}:1.` };
}

export function gpuRole(device = {}) {
  const name = String(device.name || "");
  const vram = Number(device.vram_bytes || device.vram_total_bytes || device.capacity_bytes || 0);
  const integrated = /intel|hd graphics|uhd|iris|radeon\(tm\) graphics|microsoft basic/i.test(name) || (vram > 0 && vram <= 2 * 1024 ** 3 && /graphics/i.test(name));
  if (integrated) return { role: "display", label: "display only (excluded)", included: false };
  return { role: "models", label: "used for models", included: true };
}

export function urgencyChoice(choice) {
  if (choice === "now") return { scheduling_class: "user_interactive", priority: 50, label: "Now" };
  if (choice === "background") return { scheduling_class: "background_routine", priority: -20, label: "Background" };
  return { scheduling_class: "normal_task", priority: 0, label: "Normal" };
}

export function createdTaskMessage(task = {}, summary = null) {
  const view = summary || classifyTask(task);
  const reason = view.reason ? ` ${view.reason}` : "";
  return `Created. It is ${view.category}.${reason}`;
}

export function memoryFallback(metrics) {
  if (!metrics || !Number(metrics.memory_total_bytes)) {
    return { available: false, note: "Host memory was not reported.", segments: [], next_task_fits: null };
  }
  const total = Number(metrics.memory_total_bytes);
  const free = Math.max(0, Number(metrics.memory_available_bytes || 0));
  const used = Math.max(0, total - free);
  return {
    available: true,
    total_bytes: total,
    free_bytes: free,
    models_measured: false,
    admission_wired: false,
    note: "Model memory is not measured yet. The bar shows host free memory only.",
    segments: [
      { id: "other_apps", label: "Other apps", bytes: used },
      { id: "free", label: "Free", bytes: free }
    ],
    next_task_fits: null
  };
}

const SECRET_KEY = /password|secret|token|authorization|cookie|credential|api[-_]?key/i;

export function redactPreview(value, depth = 0) {
  if (depth > 8) return "[truncated]";
  if (Array.isArray(value)) return value.slice(0, 40).map(item => redactPreview(item, depth + 1));
  if (value && typeof value === "object") {
    const out = {};
    for (const [key, item] of Object.entries(value)) {
      out[key] = SECRET_KEY.test(key) ? "[removed]" : redactPreview(item, depth + 1);
    }
    return out;
  }
  if (typeof value === "string") {
    return value
      .replace(/[A-Z0-9._%+-]+@[A-Z0-9.-]+\.[A-Z]{2,}/gi, "[email]")
      .replace(/\b(sk|pk|api|bearer)[_-][A-Za-z0-9]{8,}\b/g, "[removed]")
      .replace(/\b[0-9a-f]{32,}\b/gi, "[hex]");
  }
  return value;
}

export function formatBytes(value) {
  const n = Number(value);
  if (!Number.isFinite(n)) return "Not reported";
  const units = ["B", "KB", "MB", "GB", "TB"];
  let size = Math.max(0, n);
  let unit = 0;
  while (size >= 1024 && unit < units.length - 1) { size /= 1024; unit += 1; }
  return `${size >= 10 || unit === 0 ? size.toFixed(0) : size.toFixed(1)} ${units[unit]}`;
}

export function durationLabel(ms) {
  const n = Number(ms);
  if (!Number.isFinite(n) || n <= 0) return "";
  const seconds = Math.round(n / 1000);
  if (seconds < 90) return `${seconds}s in this state`;
  const minutes = Math.round(seconds / 60);
  if (minutes < 90) return `${minutes} min in this state`;
  const hours = Math.round(minutes / 60);
  return `${hours} h in this state`;
}
