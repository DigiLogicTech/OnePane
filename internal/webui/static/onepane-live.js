/* One shared live-state store.
   SSE (/v1/events/stream) is the event feed. One refresh fills health, tasks,
   nodes, providers, metrics and the memory budget. The existing 15s timer
   calls that refresh. The 5s metrics and node timers stand down while
   onepaneLive.ownsPolling is set. app-foundation.js stays frozen. */

const state = {
  sse: "idle",
  revision: 0,
  confirmed: {},
  health: null,
  tasks: [],
  taskStatus: [],
  routines: [],
  nodes: [],
  providers: [],
  events: [],
  metrics: null,
  memory: null,
  deployments: [],
  errors: {}
};
const listeners = new Set();
let stream = null;
let streamWorkspace = "";
let refreshing = null;
let eventRefresh = 0;

function snapshot() {
  return { ...state, confirmed: { ...state.confirmed }, errors: { ...state.errors } };
}
function subscribe(fn) {
  listeners.add(fn);
  return () => listeners.delete(fn);
}

function workspace() {
  return typeof onepaneWorkspace === "string" ? onepaneWorkspace : "";
}

function publish() {
  state.revision += 1;
  const snap = snapshot();
  mirror(snap);
  listeners.forEach(fn => { try { fn(snap); } catch { /* a renderer must not break the store */ } });
  document.dispatchEvent(new CustomEvent("onepane:live", { detail: snap }));
}

function mirror(snap) {
  if (typeof liveOps === "undefined") return;
  liveOps.reported = liveOps.reported || {};
  if (snap.health) {
    liveOps.health = String(snap.health.status || "unknown").toLowerCase();
    liveOps.healthReason = String(snap.health.reason || "");
    liveOps.reported.health = !snap.errors.health;
  }
  if (!snap.errors.tasks) { liveOps.tasks = snap.tasks; liveOps.reported.tasks = true; }
  if (!snap.errors.routines) {
    liveOps.routines = snap.routines;
    liveOps.reported.routines = true;
    if (typeof qa4ProjectHub !== "undefined") qa4ProjectHub.routines = snap.routines;
  }
  if (!snap.errors.nodes) { liveOps.nodes = snap.nodes; liveOps.reported.nodes = true; }
  if (!snap.errors.providers) { liveOps.providers = snap.providers; liveOps.reported.providers = true; }
  if (!snap.errors.events && state.sse !== "open") { liveOps.events = snap.events; liveOps.reported.events = true; }
  if (state.sse === "open") liveOps.reported.events = true;
  liveOps.lastRefresh = Date.now();
  if (typeof syncLiveNotifications === "function") syncLiveNotifications();
  if (typeof applyHealthStatus === "function" && snap.health && !snap.errors.health) applyHealthStatus(snap.health);
  if (typeof qaTaskRefreshLiveView === "function") qaTaskRefreshLiveView();
  paintMetrics(snap.metrics);
  paintNodes(snap.metrics);
}

function paintMetrics(metrics) {
  if (!metrics) return;
  if (typeof a50Latest !== "undefined") window.a50Latest = metrics;
  const host = document.querySelector("#a50MetricsRoot");
  if (host && typeof a50MetricsMarkup === "function") host.innerHTML = a50MetricsMarkup(metrics);
}

function paintNodes(metrics) {
  if (!metrics || typeof nextNodeUI === "undefined" || typeof a31RouteIs !== "function" || !a31RouteIs("nodes")) return;
  if (nextNodeUI.tab && nextNodeUI.tab !== "overview") return;
  const node = (nextNodeUI.nodes || []).find(n => (n.id || n.node_id) === nextNodeUI.selected);
  if (!node?.local || typeof nextNodeLocalManifest !== "function" || typeof nextNodeDraw !== "function") return;
  nextNodeUI.localMetrics = metrics;
  nextNodeUI.manifest = nextNodeLocalManifest(nextNodeUI.localHardware, metrics, nextNodeUI.localModels, nextNodeUI.localModelsAvailable);
  nextNodeDraw();
}

async function settle(key, run) {
  try {
    const value = await run();
    state.errors[key] = "";
    state.confirmed[key] = Date.now();
    return value;
  } catch (error) {
    state.errors[key] = error?.message || "Request failed";
    return null;
  }
}

async function refresh(force = false) {
  if (refreshing) return refreshing;
  const recent = state.confirmed.bundle && Date.now() - state.confirmed.bundle < 8000;
  if (!force && recent) return snapshot();
  refreshing = pull().finally(() => { refreshing = null; });
  return refreshing;
}

async function pull() {
  const id = workspace();
  if (!id || typeof apiRequest !== "function") return snapshot();
  if (state.sse === "idle") ensureStream();
  const qs = encodeURIComponent(id);
  const [health, tasks, status, routines, nodes, providers, metrics, memory, events, deployments] = await Promise.all([
    settle("health", () => apiRequest("/v1/health")),
    settle("tasks", () => apiRequest(`/v1/tasks?workspace_id=${qs}&limit=100`)),
    settle("status", () => apiRequest(`/v1/tasks/status-summary?workspace_id=${qs}`)),
    settle("routines", () => apiRequest(`/v1/routines?workspace_id=${qs}`)),
    settle("nodes", () => apiRequest("/v1/nodes")),
    settle("providers", () => apiRequest(`/v1/providers?workspace_id=${qs}`)),
    settle("metrics", () => apiRequest(`/v1/system/metrics?workspace_id=${qs}`)),
    settle("memory", () => apiRequest(`/v1/system/memory-budget?workspace_id=${qs}`)),
    state.sse === "open" ? Promise.resolve(null) : settle("events", () => apiRequest(`/v1/events?workspace_id=${qs}&limit=100&latest=1`)),
    settle("deployments", () => apiRequest(`/v1/local-ai/deployments?workspace_id=${qs}`))
  ]);
  if (health) state.health = health;
  if (Array.isArray(tasks)) state.tasks = tasks;
  state.taskStatus = Array.isArray(status?.tasks) ? status.tasks : [];
  if (Array.isArray(routines)) state.routines = routines.map(row => typeof qaTaskNormaliseRoutine === "function" ? qaTaskNormaliseRoutine(row) : row);
  if (nodes) state.nodes = Array.isArray(nodes) ? nodes : (nodes.nodes || []);
  if (providers) state.providers = Array.isArray(providers) ? providers : (providers.providers || []);
  if (metrics) state.metrics = metrics;
  if (memory && memory.available !== undefined) state.memory = memory;
  else if (metrics && state.errors.memory) state.memory = null;
  if (Array.isArray(events)) state.events = events;
  const deploymentRows = Array.isArray(deployments) ? deployments : deployments?.deployments;
  if (Array.isArray(deploymentRows)) state.deployments = deploymentRows;
  state.confirmed.bundle = Date.now();
  publish();
  return snapshot();
}

function ensureStream() {
  const id = workspace();
  if (!id || typeof EventSource === "undefined") {
    state.sse = "fallback";
    publish();
    return;
  }
  if (stream && streamWorkspace === id && stream.readyState !== 2) return;
  try { stream?.close(); } catch { /* a dead stream can throw on close */ }
  streamWorkspace = id;
  const after = Math.max(0, ...(typeof liveOps !== "undefined" ? liveOps.events : []).map(event => Number(event.sequence || 0)), ...state.events.map(event => Number(event.sequence || 0)));
  const source = new EventSource(`/v1/events/stream?workspace_id=${encodeURIComponent(id)}&after=${Number.isFinite(after) ? after : 0}&generic=1`);
  stream = source;
  state.sse = "connecting";
  source.onopen = () => { state.sse = "open"; state.confirmed.events = Date.now(); publish(); };
  source.onerror = () => { state.sse = "fallback"; publish(); };
  source.addEventListener("onepane", ev => {
    try {
      const item = JSON.parse(ev.data);
      if (!item?.sequence) return;
      const list = typeof liveOps !== "undefined" ? liveOps.events : state.events;
      if (!list.some(row => Number(row.sequence) === Number(item.sequence))) list.push(item);
      if (typeof liveOps !== "undefined") {
        liveOps.events = list.slice(-500);
        liveOps.reported.events = true;
        liveOps.lastRefresh = Date.now();
      }
      state.events = (typeof liveOps !== "undefined" ? liveOps.events : list).slice(-500);
      state.confirmed.events = Date.now();
      state.sse = "open";
      clearTimeout(eventRefresh);
      eventRefresh = setTimeout(() => { refresh(true); }, 1200);
      publish();
    } catch { /* ignore a malformed event */ }
  });
}

function installLive() {
  const api = { snapshot, subscribe, refresh, ensureStream, ownsPolling: true };
  window.onepaneLive = api;
  window.refreshOperationalDataQA = async function(force = false) {
    return refresh(!!force);
  };
  window.qa6StartEventStream = function() { ensureStream(); };
  if (typeof a50FetchMetrics === "function") {
    window.a50FetchMetrics = async function() {
      await refresh(true);
      paintMetrics(state.metrics);
    };
  }
}
