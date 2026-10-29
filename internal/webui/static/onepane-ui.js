/* Honest UI installer. Classic script so it can see the shell's let/const
   bindings. Presentation helpers arrive from the onepane-present module. */

let present = null;

const $ = selector => document.querySelector(selector);
const $$ = selector => [...document.querySelectorAll(selector)];

function esc(value) {
  return typeof escapeHtml === "function" ? escapeHtml(value) : String(value ?? "");
}

function install() {
  present = window.onepanePresent;
  installLive();
  installThemes();
  installHealth();
  installMemory();
  installTabs();
  installTasks();
  installModels();
  installProject();
  installAssistant();
  installSupport();
  window.onepaneLive.subscribe(() => {
    renderMemory();
    renderHealthPill();
    decorateTasks();
  });
  window.__onepaneMarkReady?.();
}

function installThemes() {
  const extra = {
    aurora: { mode: "dark", caption: "#10241e", text: "#e7f6ef", border: "#245443" },
    cobalt: { mode: "dark", caption: "#10243d", text: "#e8f1ff", border: "#2a4d78" }
  };
  if (typeof THEME_PALETTES === "object") Object.assign(THEME_PALETTES, extra);
  const baseApply = window.applyTheme;
  window.applyTheme = function(name) {
    const pack = typeof qa5ThemePacks === "function" ? qa5ThemePacks()[name] : null;
    const vars = pack?.vars || {};
    if (pack) {
      const check = present.themeContrastOK(vars["--text"], vars["--bg"]);
      if (!check.ok) {
        if (typeof notice === "function") notice(check.detail + " The theme was not applied.", "bad");
        return;
      }
    }
    baseApply?.(name);
  };
  window.qa5ThemeButtons = function() {
    const builtin = Object.keys(typeof THEME_PALETTES === "object" ? THEME_PALETTES : {});
    const packs = typeof qa5ThemePacks === "function" ? Object.keys(qa5ThemePacks()).filter(id => !builtin.includes(id)) : [];
    return `<section class="theme-collection"><div class="theme-collection-label">Built-in themes</div><div class="theme-compact-grid">${builtin.map(themeSwatch).join("")}</div></section>` +
      (packs.length ? `<section class="theme-collection"><div class="theme-collection-label">Installed theme packages</div><div class="theme-compact-grid">${packs.map(themeSwatch).join("")}</div></section>` : "");
  };
  window.uiThemeSwatch = function(id) {
    const colors = themeColors(id);
    return { name: title(id), description: colors.detail || "Theme", bg: colors.bg, panel: colors.panel, accent: colors.accent, text: colors.text };
  };
  window.openThemePopover = function(anchor) {
    const ids = Object.keys(typeof THEME_PALETTES === "object" ? THEME_PALETTES : {});
    if (typeof popoverFor !== "function") return;
    popoverFor(anchor, `<div class="popover"><h3>Theme</h3><div class="theme-grid">${ids.map(themeSwatch).join("")}</div></div>`);
    $$("[data-settings-theme], [data-theme-choice]").forEach(button => button.onclick = () => {
      applyTheme(button.dataset.settingsTheme || button.dataset.themeChoice);
      const root = $("#overlayRoot");
      if (root) root.innerHTML = "";
    });
  };
}

function themeColors(id) {
  const sheet = [...document.styleSheets].find(item => item.href && item.href.includes("theme-tokens.css"));
  const found = { bg: "", panel: "", accent: "", text: "", detail: "" };
  const read = style => {
    found.bg = style.getPropertyValue("--bg").trim() || found.bg;
    found.panel = style.getPropertyValue("--panel").trim() || found.panel;
    found.accent = style.getPropertyValue("--accent").trim() || found.accent;
    found.text = style.getPropertyValue("--text").trim() || found.text;
  };
  try {
    for (const rule of sheet?.cssRules || []) {
      if (rule.selectorText === `:root[data-theme="${id}"]`) read(rule.style);
      if (rule.cssRules) {
        for (const inner of rule.cssRules) {
          if (inner.selectorText === `:root[data-theme="${id}"]`) read(inner.style);
        }
      }
    }
  } catch { /* a stylesheet that cannot be read keeps the empty swatch */ }
  const pack = typeof qa5ThemePacks === "function" ? qa5ThemePacks()[id] : null;
  if (pack?.vars) {
    found.bg = pack.vars["--bg"] || found.bg;
    found.panel = pack.vars["--panel"] || found.panel;
    found.accent = pack.vars["--accent"] || found.accent;
    found.text = pack.vars["--text"] || found.text;
  }
  const check = present.themeContrastOK(found.text, found.bg);
  found.detail = check.detail;
  found.ok = check.ok;
  return found;
}

function themeSwatch(id) {
  const colors = themeColors(id);
  const active = typeof state !== "undefined" && state.theme === id;
  const warn = colors.ok === false ? `<span class="pill warn">Contrast</span>` : "";
  return `<button type="button" class="theme-choice theme-preview ${active ? "active" : ""}" data-settings-theme="${esc(id)}" data-theme-choice="${esc(id)}" aria-pressed="${active ? "true" : "false"}" aria-label="Use ${esc(title(id))} theme">
    <span class="theme-compact-head"><strong>${esc(title(id))}</strong>${warn}</span>
    <span class="theme-palette-strip" aria-label="Theme colours from the stylesheet">${["bg", "panel", "accent", "text"].map(key => `<span class="theme-palette-chip" style="background:${esc(colors[key] || "transparent")}" title="${esc(key)} ${esc(colors[key] || "not set")}"></span>`).join("")}</span>
    <span class="list-meta">${esc(colors.detail || "")}</span>
  </button>`;
}

function title(id) {
  return String(id || "").replace(/^./, ch => ch.toUpperCase());
}

function installHealth() {
  window.openHealthPopover = async function(anchor) {
    if (typeof dismissActivePopover === "function" && dismissActivePopover(anchor)) return;
    if (window.onepaneLive) await window.onepaneLive.refresh(true);
    const report = currentHealth();
    const rows = report.rows.map(row => `<div class="popover-row health-popover-row"><span>${esc(row.label)}</span><span class="${esc(row.kind)}">${esc(row.text)}</span><span class="list-meta">${esc(row.ago)}</span></div>`).join("");
    if (typeof popoverFor === "function") popoverFor(anchor, `<div class="popover" role="dialog" aria-label="System health"><h3>System health</h3>${rows}<p class="list-meta">One calculation. Missing feeds stay “Not reported”.</p></div>`);
  };
  if (typeof a31OperationsHealth === "function") {
    window.a31OperationsHealth = function() {
      const report = currentHealth();
      const cards = report.rows.map(row => `<article class="panel-card"><strong>${esc(row.label)}</strong><div class="${esc(row.kind)}">${esc(row.text)}</div><div class="list-meta">${esc(row.ago)}</div></article>`).join("");
      return `<div class="health-grid">${cards}</div>`;
    };
  }
  renderHealthPill();
}

function currentHealth() {
  const snap = window.onepaneLive?.snapshot?.() || {};
  const reported = typeof liveOps !== "undefined" ? { ...liveOps.reported } : {};
  const health = snap.health || (typeof liveOps !== "undefined" ? { status: liveOps.health } : null);
  const nodes = snap.nodes || (typeof liveOps !== "undefined" ? liveOps.nodes : []);
  const providers = snap.providers || (typeof liveOps !== "undefined" ? liveOps.providers : []);
  const tasks = snap.tasks || (typeof liveOps !== "undefined" ? liveOps.tasks : []);
  reported.workers = !!reported.tasks;
  reported.workersText = reported.tasks ? `${tasks.filter(task => String(task.state) === "running").length} running` : "";
  reported.verification = !!reported.events;
  reported.verificationText = reported.events ? "Recent events loaded" : "";
  const runtimeKnown = Array.isArray(snap.deployments);
  reported.runtime = runtimeKnown && !snap.errors?.deployments;
  reported.runtimeText = reported.runtime ? `${snap.deployments.filter(row => /resident|busy|starting|healthy/i.test(String(row.residency_state || ""))).length} resident` : "";
  reported.watchdog = !!reported.health;
  const watchdog = String(health?.watchdog || health?.status || "");
  reported.watchdogText = reported.health ? (watchdog && watchdog !== "ok" ? watchdog.replaceAll("_", " ") : "Responding") : "";
  reported.nodesText = nodes.length ? `${nodes.length} reported` : "";
  reported.providersText = providers.length ? `${providers.length} reported` : "";
  return present.healthReport({
    health,
    reported,
    confirmed: snap.confirmed || {},
    now: Date.now(),
    components: health?.components || health?.parts || null
  });
}

function renderHealthPill() {
  const report = currentHealth();
  for (const button of [$("#healthButton"), $("#mobileHealthButton")].filter(Boolean)) {
    button.classList.toggle("health-ok", report.pill.kind === "good");
    button.classList.toggle("bad", report.pill.kind === "bad");
    button.classList.toggle("health-unknown", report.pill.kind !== "good");
    const text = button.querySelector("span:last-child");
    if (text && !text.classList.contains("status-dot")) text.textContent = report.pill.text;
    button.setAttribute("aria-label", `System health: ${report.pill.text}`);
  }
}

function installMemory() {
  const bar = document.createElement("button");
  bar.type = "button";
  bar.id = "onepaneMemoryBar";
  bar.className = "onepane-memory";
  bar.setAttribute("aria-label", "Memory budget");
  const health = $("#healthButton");
  if (health?.parentElement) health.parentElement.insertBefore(bar, health);
  bar.onclick = () => unloadIdle(bar);
  renderMemory();
}

function renderMemory() {
  const bar = $("#onepaneMemoryBar");
  if (!bar) return;
  const snap = window.onepaneLive?.snapshot?.() || {};
  const budget = snap.memory || present.memoryFallback(snap.metrics);
  if (!budget?.available) {
    bar.innerHTML = `<span class="onepane-memory-label">Memory not reported</span>`;
    bar.classList.remove("is-tight");
    return;
  }
  const segments = (budget.segments || []).filter(segment => Number(segment.bytes) > 0);
  const total = segments.reduce((sum, segment) => sum + Number(segment.bytes || 0), 0) || 1;
  const tight = budget.next_task_fits === false;
  bar.classList.toggle("is-tight", tight);
  const idle = segments.find(segment => segment.id === "models_idle");
  bar.innerHTML = `<span class="onepane-memory-label">Memory</span><span class="onepane-memory-track" role="img" aria-label="${esc(memoryLabel(budget))}">${segments.map(segment => `<i class="onepane-memory-${esc(segment.id)}" style="width:${(100 * Number(segment.bytes) / total).toFixed(2)}%" title="${esc(segment.label)} ${esc(present.formatBytes(segment.bytes))}"></i>`).join("")}</span><span class="onepane-memory-readout">${esc(present.formatBytes(budget.free_bytes))} free${idle ? " · Unload idle" : ""}${tight ? " · Tight" : ""}</span>`;
  bar.title = memoryLabel(budget);
}

function memoryLabel(budget) {
  const parts = (budget.segments || []).map(segment => `${segment.label} ${present.formatBytes(segment.bytes)}`);
  if (budget.vram_known) parts.push(`VRAM free ${present.formatBytes(budget.vram_free_bytes)}`);
  if (budget.note) parts.push(budget.note);
  if (budget.next_task_reason) parts.push(budget.next_task_reason);
  if (budget.admission_wired === false) parts.push("Not the admission decision.");
  return parts.join(". ");
}

async function unloadIdle(button) {
  const snap = window.onepaneLive?.snapshot?.() || {};
  const idle = Number(snap.memory?.idle_model_bytes || 0);
  if (!idle) {
    if (typeof notice === "function") notice(snap.memory?.note || "No idle model is reported to unload.");
    return;
  }
  button.disabled = true;
  try {
    const result = await apiRequest("/v1/local-ai/deployments/unload-idle", { method: "POST", body: JSON.stringify({ workspace_id: onepaneWorkspace }) });
    if (typeof notice === "function") notice(result.reason || "Idle unload finished.", result.available === false ? "bad" : "good");
    await window.onepaneLive?.refresh(true);
  } catch (error) {
    if (typeof notice === "function") notice(error.message, "bad");
  } finally { button.disabled = false; }
}

function installTabs() {
  const baseOpen = window.openRoute;
  const baseTabs = window.renderTabs;
  const shell = new Set(["tasks", "projects", "workspaces"]);
  window.openRoute = function(route, options = {}) {
    if (!options.newTab && !shell.has(route) && typeof state !== "undefined") {
      let view = state.tabs.find(tab => tab.onepaneView);
      if (!view) view = state.tabs.find(tab => !shell.has(tab.route) && !tab.pinned) || state.tabs[0];
      if (view) {
        view.route = route;
        view.title = (typeof pages !== "undefined" && pages[route]?.title) || route;
        view.onepaneView = true;
        activateTab(view.id);
        return;
      }
    }
    if (!options.newTab) {
      const existing = state.tabs.find(tab => tab.route === route);
      if (existing) return activateTab(existing.id);
    }
    const open = state.tabs.filter(tab => !tab.pinned);
    if (open.length >= 6) {
      if (typeof notice === "function") notice("Close a tab before opening another. Full names stay readable when fewer tabs are open.");
      return;
    }
    return baseOpen?.(route, options);
  };
  window.renderTabs = function() {
    baseTabs?.();
    $$(".tab").forEach(tab => {
      const label = tab.querySelector(".tab-label");
      if (label) {
        tab.title = label.textContent;
        label.removeAttribute("title");
      }
    });
  };
  window.openNewTabPicker = function() {
    const choices = [["tasks", "Tasks"], ["projects", "Workspaces"]];
    if (typeof openModal !== "function") return;
    openModal("Open a tab", `<div class="component-picker-grid">${choices.map(([route, label]) => `<button class="component-choice" data-new-tab-route="${route}"><strong>${esc(label)}</strong><span>Open ${esc(label)}</span></button>`).join("")}</div>`);
    $$("[data-new-tab-route]").forEach(button => button.onclick = () => {
      if (typeof closeModal === "function") closeModal();
      openRoute(button.dataset.newTabRoute, { newTab: true });
    });
  };
}

function taskSummary(task) {
  const snap = window.onepaneLive?.snapshot?.() || {};
  return (snap.taskStatus || []).find(row => row.task_id === task?.id) || present.classifyTask(task);
}

function installTasks() {
  const baseOpen = window.openNewTask;
  window.openNewTask = () => openGuidedTask(baseOpen);
  const baseRender = window.renderTasks;
  window.renderTasks = async function() {
    await baseRender?.();
    decorateTasks();
  };
  const baseRefresh = window.qaTaskRefreshLiveView;
  window.qaTaskRefreshLiveView = function() {
    baseRefresh?.();
    decorateTasks();
  };
  const baseOverview = window.qa4InspectorOverview;
  window.qa4InspectorOverview = function() {
    const html = baseOverview?.() || "";
    if (typeof qa4Inspector !== "undefined" && qa4Inspector.kind === "task") return html + taskInspector(qa4Inspector.id);
    if (html.includes("No additional structured detail")) {
      return presentEmpty("empty", "Nothing selected", "Choose a task, model, or node to see what OnePane knows about it.", "Open Tasks", () => openRoute("tasks"));
    }
    return html;
  };
}

function decorateTasks() {
  const host = $("#tasksBody");
  if (!host) return;
  host.querySelectorAll("tr[data-inspect-id]").forEach(row => {
    const task = (typeof liveOps !== "undefined" ? liveOps.tasks : []).find(item => item.id === row.dataset.inspectId);
    const summary = taskSummary(task || { id: row.dataset.inspectId, state: "" });
    let line = row.querySelector(".onepane-task-reason");
    if (!line) {
      const cell = row.querySelector("td");
      if (!cell) return;
      line = document.createElement("div");
      line.className = "onepane-task-reason list-meta";
      cell.appendChild(line);
    }
    const time = present.durationLabel(summary.time_in_state_ms);
    line.textContent = [summary.category, summary.reason, time].filter(Boolean).join(" · ");
  });
}

function taskInspector(id) {
  const task = (typeof liveOps !== "undefined" ? liveOps.tasks : []).find(item => item.id === id) || { id, state: "" };
  const summary = taskSummary(task);
  const ahead = (summary.ahead || []).join(", ");
  const ram = summary.ram_needed_bytes ? `${present.formatBytes(summary.ram_needed_bytes)} needed` : "RAM need not reported";
  const free = summary.ram_free_bytes != null ? `${present.formatBytes(summary.ram_free_bytes)} free` : "free RAM not reported";
  const vram = summary.vram_needed_bytes ? `${present.formatBytes(summary.vram_needed_bytes)} VRAM needed` : "";
  const queue = summary.queue_position ? `Queue position ${summary.queue_position}` : "";
  const action = summary.next_action_id ? `<button type="button" class="btn primary" id="onepaneTaskNext">${esc(summary.next_action)}</button>` : "";
  queueMicrotask(() => {
    const button = $("#onepaneTaskNext");
    if (!button) return;
    button.onclick = () => runTaskAction(summary.next_action_id, task);
  });
  return `<section class="onepane-task-inspector"><h3>Why it is waiting</h3><p><strong>${esc(summary.category)}</strong> · ${esc(summary.reason)}</p><p class="list-meta">${esc([queue, ahead && `Ahead: ${ahead}`, ram, free, vram, present.durationLabel(summary.time_in_state_ms)].filter(Boolean).join(" · "))}</p>${action}</section>`;
}

function runTaskAction(id) {
  if (id === "setup_model") openRoute("models");
  else if (id === "unload_idle") unloadIdle($("#onepaneMemoryBar") || document.body);
  else if (id === "approve") { if (typeof notice === "function") notice("Open the task approval control for this item. OnePane will not approve it from the summary."); }
  else if (typeof notice === "function") notice("Inspect the task detail above.");
}

async function openGuidedTask() {
  if (typeof qa4LoadProjectHub === "function") await qa4LoadProjectHub(false);
  const projects = typeof qaTaskProjectOptions === "function" ? qaTaskProjectOptions() : `<option value="">No project / Global</option>`;
  if (typeof openModal !== "function") return;
  openModal("New task", `<form id="newTaskForm" class="qa-form onepane-new-task">
    <label>Objective<textarea name="objective" rows="4" required placeholder="What should OnePane accomplish?"></textarea></label>
    <fieldset><legend>What does done look like?</legend><div id="onepaneAcceptance"></div><button type="button" class="btn" id="onepaneAddAcceptance">Add a check</button></fieldset>
    <label>Urgency<select name="urgency" aria-label="Urgency"><option value="now">Now</option><option value="normal" selected>Normal</option><option value="background">Background</option></select></label>
    <div class="form-grid">
      <label>Project<select id="newTaskProject" name="project_id">${projects}</select></label>
      <label>Workspace<select id="newTaskWorkspace" name="project_workspace_id" disabled><option value="">No workspace / Global</option></select></label>
    </div>
    <label>Model<select name="model" id="onepaneTaskModel"><option value="auto">Automatic</option></select></label>
    <p id="onepaneRoutePreview" class="list-meta" role="status">Checking the local route…</p>
    <div class="error" id="newTaskError"></div>
    <div class="toolbar"><button class="btn primary" type="submit">Create task</button><button class="btn" type="button" id="onepaneSetupModel" hidden>Set up a model</button></div>
  </form>`);
  addAcceptanceRow();
  $("#onepaneAddAcceptance").onclick = () => addAcceptanceRow();
  if (typeof qaTaskBindScope === "function") await qaTaskBindScope("#newTaskProject", "#newTaskWorkspace");
  const preview = await routePreview();
  const previewHost = $("#onepaneRoutePreview");
  if (previewHost) previewHost.textContent = preview.message;
  const setup = $("#onepaneSetupModel");
  if (setup) {
    setup.hidden = !preview.setup;
    setup.onclick = () => { if (typeof closeModal === "function") closeModal(); openRoute("models"); };
  }
  const select = $("#onepaneTaskModel");
  if (select && preview.option) select.insertAdjacentHTML("beforeend", preview.option);
  $("#newTaskForm").onsubmit = async event => {
    event.preventDefault();
    const form = event.currentTarget;
    const data = Object.fromEntries(new FormData(form));
    const urgency = present.urgencyChoice(data.urgency);
    const checks = [...form.querySelectorAll("[data-acceptance]")].map(input => input.value.trim()).filter(Boolean).slice(0, 12);
    const payload = { workspace_id: onepaneWorkspace, objective: data.objective, priority: urgency.priority, scheduling_class: urgency.scheduling_class, completion: { type: "operator_review", acceptance: checks, model: data.model || "auto" } };
    if (data.project_id) payload.project_id = data.project_id;
    if (data.project_workspace_id) payload.project_workspace_id = data.project_workspace_id;
    try {
      const created = await apiRequest("/v1/tasks", { method: "POST", body: JSON.stringify(payload) });
      if (typeof liveOps !== "undefined") liveOps.lastRefresh = 0;
      await window.onepaneLive?.refresh(true);
      const summary = (window.onepaneLive?.snapshot()?.taskStatus || []).find(row => row.task_id === created.id) || present.classifyTask(created);
      if (typeof closeModal === "function") closeModal();
      if (typeof renderTasks === "function") await renderTasks();
      if (typeof notice === "function") notice(present.createdTaskMessage(created, summary));
    } catch (error) {
      const slot = $("#newTaskError");
      if (slot) slot.textContent = error.message;
    }
  };
}

function addAcceptanceRow() {
  const host = $("#onepaneAcceptance");
  if (!host || host.children.length >= 8) return;
  const row = document.createElement("label");
  row.innerHTML = `Check <input data-acceptance placeholder="For example: the build runs">`;
  host.appendChild(row);
}

async function routePreview() {
  try {
    const rows = await apiRequest(`/v1/scheduler/candidates?workspace_id=${encodeURIComponent(onepaneWorkspace)}&capability_id=inference.general&role_name=general`);
    const list = Array.isArray(rows) ? rows : [];
    const local = list.filter(row => row.schedulable && row.local);
    if (!local.length) return { setup: true, message: "No eligible local model, so this will wait. Cloud is not used unless you already turned it on.", option: "" };
    const first = local[0];
    const name = first.display_name || first.provider || first.id;
    return { setup: false, message: `Automatic route preview: ${name}. This is a candidate, not a reservation.`, option: `<option value="candidate:${esc(first.id)}">${esc(name)}</option>` };
  } catch (error) {
    return { setup: false, message: `Could not check models (${error.message}). Creating the task does not turn cloud on.` };
  }
}

function installModels() {
  const baseLocal = window.a31RenderLocalModels;
  if (typeof baseLocal === "function") {
    window.a31RenderLocalModels = async function() {
      await baseLocal();
      const root = $("#a31ModelsRoot");
      if (!root) return;
      root.querySelector(".onepane-starter")?.remove();
      const snap = window.onepaneLive?.snapshot?.() || {};
      root.querySelector(".models-single-column")?.insertAdjacentHTML("afterbegin", `<section class="onepane-starter">${starterPair(snap.deployments || [])}</section>`);
      root.querySelectorAll("[data-a31-agent-check]").forEach(button => {
        const toolbar = button.parentElement;
        if (!toolbar || toolbar.querySelector("[data-onepane-use-model]")) return;
        const use = document.createElement("button");
        use.type = "button";
        use.className = "btn";
        use.dataset.onepaneUseModel = button.dataset.a31AgentCheck;
        use.textContent = "Use in…";
        toolbar.prepend(use);
        const tile = button.closest(".model-tile");
        if (!tile || tile.querySelector(".onepane-model-steps")) return;
        const text = tile.textContent || "";
        const steps = document.createElement("ol");
        steps.className = "onepane-model-steps";
        const checked = /agent check:\s*passed/i.test(text);
        const installed = /installed|ready|resident|stopped/i.test(text);
        [["Found", true], ["Installed (hash verified)", installed], [`Agent Check ${checked ? "passed" : "not passed"}`, checked], ["In use by a workspace", /in use/i.test(text)]].forEach(([label, on]) => {
          const item = document.createElement("li");
          item.textContent = label;
          if (on) item.className = "is-done";
          steps.appendChild(item);
        });
        tile.querySelector("div")?.appendChild(steps);
      });
    };
  }
  const baseDiscover = window.a31RenderDiscoverModels;
  if (typeof baseDiscover === "function") {
    window.a31RenderDiscoverModels = async function() {
      await baseDiscover();
      rewriteFitChips();
      const root = $("#a31DiscoverCatalog");
      if (root && !root.dataset.onepaneFit) {
        root.dataset.onepaneFit = "1";
        new MutationObserver(() => rewriteFitChips()).observe(root, { childList: true, subtree: true });
      }
    };
  }
  document.addEventListener("click", event => {
    const use = event.target.closest?.("[data-onepane-use-model]");
    if (use) assignModel(use.dataset.onepaneUseModel);
  });
}

function starterPair(installed) {
  const wanted = [
    { name: "Qwen3.5-2B Q8", device: "CPU", note: "A small model that can keep working on this class of laptop when the GPU is busy." },
    { name: "qwen3:4b", device: "GPU", note: "A 4B model that fits a 6 GB GTX 1060 when VRAM is free." }
  ];
  return `<h2>Recommended for this laptop</h2><div class="onepane-starter-grid">${wanted.map(item => {
    const have = installed.some(model => String(model.display_name || model.model_ref || "").toLowerCase().includes(item.name.toLowerCase().split(" ")[0]));
    return `<article><strong>${esc(item.name)}</strong><div class="list-meta">${esc(item.device)} · ${esc(item.note)}</div><p>${have ? "Already in the installed list." : "Not in the verified catalogue on this install. Search Discover before treating it as installable."}</p></article>`;
  }).join("")}</div>`;
}

function rewriteFitChips() {
  const snap = window.onepaneLive?.snapshot?.() || {};
  const memory = snap.memory || present.memoryFallback(snap.metrics);
  $$("#a31DiscoverCatalog .model-tile").forEach(card => {
    if (card.querySelector(".onepane-fit")) return;
    const blob = card.textContent || "";
    const cloud = /cloud|provider/i.test(blob) && !/OnePane Verified/i.test(blob);
    const decision = present.fitDecision({
      neededRAM: null,
      neededVRAM: null,
      freeRAM: memory?.free_bytes,
      freeVRAM: memory?.vram_known ? memory.vram_free_bytes : null,
      idleRAM: memory?.idle_model_bytes || 0,
      device: /gpu/i.test(blob) ? "GPU" : "CPU",
      cloud,
      consent: false
    });
    const slot = document.createElement("span");
    slot.className = `pill onepane-fit onepane-fit-${decision.id}`;
    slot.textContent = decision.label;
    slot.title = decision.detail;
    card.querySelectorAll(".pill.good").forEach(pill => {
      const text = pill.textContent.trim();
      if (!/installed|verified|colibri/i.test(text)) pill.remove();
    });
    card.querySelector(".toolbar")?.prepend(slot);
  });
}

async function assignModel(deploymentID) {
  const snap = window.onepaneLive?.snapshot?.() || {};
  const model = (snap.deployments || []).find(row => row.deployment_id === deploymentID) || { deployment_id: deploymentID, display_name: deploymentID };
  if (typeof qa4LoadProjectHub === "function") await qa4LoadProjectHub(false);
  const projects = typeof qa4ProjectHub !== "undefined" ? qa4ProjectHub.projects || [] : [];
  if (!projects.length || typeof openModal !== "function") {
    if (typeof notice === "function") notice("Create a project before assigning a model.");
    return;
  }
  openModal("Use this model", `<form id="onepaneUseModel" class="qa-form"><p>Set the workspace route to <strong>${esc(model.display_name || model.model_ref || deploymentID)}</strong>. This does not grant tools or cloud.</p>
    <label>Project<select name="project">${projects.map(project => `<option value="${esc(project.id)}">${esc(project.name || project.id)}</option>`).join("")}</select></label>
    <label>Workspace<select name="workspace" id="onepaneUseWorkspace"></label>
    <button class="btn primary" type="submit">Set route</button></form>`);
  const fill = () => {
    const project = projects.find(item => item.id === $("#onepaneUseModel")?.project?.value) || projects[0];
    const spaces = typeof qa4Workspaces === "function" ? qa4Workspaces(project) : [];
    const select = $("#onepaneUseWorkspace");
    if (select) select.innerHTML = spaces.map(space => `<option value="${esc(space.id)}">${esc(space.name || space.id)}</option>`).join("") || `<option value="">No workspace</option>`;
  };
  fill();
  $("#onepaneUseModel").project.onchange = fill;
  $("#onepaneUseModel").onsubmit = async event => {
    event.preventDefault();
    const data = Object.fromEntries(new FormData(event.currentTarget));
    const project = projects.find(item => item.id === data.project);
    const spaces = typeof qa4Workspaces === "function" ? qa4Workspaces(project) : [];
    const workspace = spaces.find(item => item.id === data.workspace) || spaces[0];
    if (!project || !workspace) return;
    workspace.orchestration = workspace.orchestration || {};
    workspace.orchestration.supervisor = workspace.orchestration.supervisor || {};
    const candidate = (qa4ProjectHub.candidates || []).find(item => item.deployment_id === deploymentID || item.id === deploymentID);
    workspace.orchestration.supervisor.model = candidate ? `candidate:${candidate.id}` : (model.model_ref || deploymentID);
    try {
      await qa4SaveProjectWorkspaces(project, spaces);
      if (typeof closeModal === "function") closeModal();
      if (typeof notice === "function") notice(`Route set. ${workspace.name} will request ${model.display_name || model.model_ref}.`);
    } catch (error) {
      if (typeof notice === "function") notice(error.message, "bad");
    }
  };
}

function installProject() {
  const base = window.renderWorkspaces;
  window.renderWorkspaces = async function() {
    await base?.();
    decorateProject();
  };
  const baseProjects = window.renderProjects;
  window.renderProjects = async function() {
    await baseProjects?.();
    decorateProject();
  };
  const baseContent = window.qa6ComponentContent;
  window.qa6ComponentContent = function(widget, project, workspace) {
    const html = baseContent?.(widget, project, workspace) || "";
    if (widget?.type === "tasks") {
      const empty = html.includes("No tasks") ? presentEmpty("empty", "No tasks for this project", "Create one when you know what done looks like.", "", null, true) : html;
      return `${empty}<div class="toolbar"><button type="button" class="btn" data-onepane-new-task>Create task</button></div>`;
    }
    if (widget?.type === "chat") return html.replace("Routing on", "Local route").replace("Single path", "Single path");
    return html;
  };
}

function decorateProject() {
  const page = document.querySelector("#viewHost .workspace-page");
  if (!page || page.querySelector(".onepane-project-home")) return;
  const project = typeof qa4ActiveProject === "function" ? qa4ActiveProject() : null;
  if (!project) return;
  const workspace = typeof qa4ActiveWorkspace === "function" ? qa4ActiveWorkspace() : null;
  const tasks = (typeof liveOps !== "undefined" ? liveOps.tasks : []).filter(task => !task.project_id || task.project_id === project.id);
  const active = tasks.find(task => ["running", "verifying"].includes(task.state));
  const waiting = tasks.map(taskSummary).find(summary => /Waiting|Needs you|Blocked/.test(summary.category));
  const strip = document.createElement("section");
  strip.className = "onepane-project-home";
  strip.setAttribute("aria-label", "Project home");
  const spaces = typeof qa4Workspaces === "function" ? qa4Workspaces(project) : [];
  strip.innerHTML = [
    ["Objective", project.description || project.name || "No objective yet"],
    ["Workspaces", spaces.map(space => space.name || space.id).join(", ") || "No workspace"],
    ["Now", active ? (active.objective || active.id) : "Nothing is running"],
    ["Waiting", waiting ? `${waiting.category}: ${waiting.reason}` : "Nothing is waiting"],
    ["Evidence", typeof liveOps !== "undefined" && liveOps.reported?.events ? `${(liveOps.events || []).length} recent events` : "Evidence not reported"]
  ].map(([label, value]) => `<div><span>${esc(label)}</span><strong>${esc(value)}</strong></div>`).join("");
  const grid = $("#qa4WorkspaceGrid");
  (grid || page).before(strip);
  page.querySelectorAll("[data-onepane-new-task]").forEach(button => button.onclick = () => openNewTask());
  const form = page.querySelector(".qa7-chat-form");
  if (form && !form.querySelector(".onepane-send")) {
    const send = form.querySelector("button");
    if (send) send.classList.add("onepane-send");
  }
}

function presentEmpty(kind, titleText, detail, actionLabel, action, bare = false) {
  const state = present.emptyState({ kind, title: titleText, detail, actionLabel });
  if (bare) return `<div class="empty-state onepane-empty" data-empty="${esc(state.kind)}"><strong>${esc(state.title)}</strong><p>${esc(state.detail)}</p></div>`;
  const button = state.actionLabel ? `<button type="button" class="btn" data-onepane-empty-action>${esc(state.actionLabel)}</button>` : "";
  queueMicrotask(() => {
    document.querySelector("[data-onepane-empty-action]")?.addEventListener("click", () => action?.());
  });
  return `<div class="empty-state onepane-empty" data-empty="${esc(state.kind)}"><strong>${esc(state.title)}</strong><p>${esc(state.detail)}</p>${button}</div>`;
}

function installAssistant() {
  const launcher = $("#controlChatLauncher span:last-child");
  if (launcher) launcher.textContent = "Assistant";
  const panelTitle = $("#controlChatToggle strong");
  if (panelTitle) panelTitle.textContent = "Assistant";
  const web = $("#webChatLauncher");
  if (web) {
    web.hidden = true;
    web.setAttribute("aria-hidden", "true");
  }
  const base = window.a31RenderControlChat;
  window.a31RenderControlChat = async function() {
    await base?.();
    const body = $("#controlChatBody");
    if (!body || body.querySelector(".onepane-assistant-chips")) return;
    const chips = document.createElement("div");
    chips.className = "onepane-assistant-chips";
    chips.innerHTML = `<span class="pill" id="onepaneContextChip">Context: General</span><span class="onepane-mode" role="group" aria-label="Assistant mode">${["Direct", "Supervisor", "Team", "Council", "Manual web"].map((mode, index) => `<button type="button" class="btn tiny ${index === 0 ? "primary" : ""}" data-assistant-mode="${esc(mode)}">${esc(mode)}</button>`).join("")}</span><p class="list-meta" id="onepaneModeNote">Direct answers in this conversation. Tools still need a task.</p>`;
    body.prepend(chips);
    const sync = () => {
      const project = $("#controlChatProject")?.selectedOptions?.[0]?.textContent || "General";
      const workspace = $("#controlChatWorkspace")?.selectedOptions?.[0]?.textContent || "";
      const chip = $("#onepaneContextChip");
      if (chip) chip.textContent = workspace && !/Automatic/.test(workspace) ? `Context: ${project} / ${workspace}` : `Context: ${project}`;
    };
    sync();
    $("#controlChatProject")?.addEventListener("change", () => setTimeout(sync, 400));
    chips.querySelectorAll("[data-assistant-mode]").forEach(button => button.onclick = () => {
      chips.querySelectorAll("[data-assistant-mode]").forEach(item => item.classList.remove("primary"));
      button.classList.add("primary");
      const note = $("#onepaneModeNote");
      const mode = button.dataset.assistantMode;
      if (note) note.textContent = mode === "Manual web"
        ? "Manual web opens the browser handoff. OnePane does not call that provider's API and does not fall back to it."
        : `${mode} is the execution mode for the next governed task. Chat context is not permission.`;
      if (mode === "Manual web") openRoute("webchat");
    });
  };
}

function installSupport() {
  const nav = $(".sidebar");
  if (nav && !$("#onepaneSupportButton")) {
    const button = document.createElement("button");
    button.type = "button";
    button.id = "onepaneSupportButton";
    button.className = "sidebar-action";
    button.innerHTML = `<span class="nav-icon" aria-hidden="true">!</span><span>Something's wrong</span>`;
    const settings = nav.querySelector('[data-route="settings"]');
    if (settings) nav.insertBefore(button, settings);
    else nav.appendChild(button);
    button.onclick = openSupport;
  }
  const baseSettings = window.renderSettings;
  window.renderSettings = async function() {
    await baseSettings?.();
    const host = $("#a31SettingsContent, .a36-settings-content, .settings-shell");
    if (!host || host.querySelector("#onepaneDeveloperMode")) return;
    const row = document.createElement("label");
    row.className = "inline-check";
    row.innerHTML = `<input type="checkbox" id="onepaneDeveloperMode"> Developer mode <span class="list-meta">Loads QA scripts and the release smoke harness. Off for normal use.</span>`;
    host.appendChild(row);
    const box = $("#onepaneDeveloperMode");
    box.checked = document.documentElement.dataset.developer === "on";
    box.onchange = () => {
      localStorage.setItem("onepane:developer-mode", box.checked ? "1" : "0");
      if (typeof notice === "function") notice(box.checked ? "Developer mode will load QA scripts on the next reload." : "Developer mode will stay off after the next reload.");
    };
  };
}

async function openSupport() {
  if (typeof qa4LoadProjectHub === "function") await qa4LoadProjectHub(false);
  const projects = typeof qa4ProjectHub !== "undefined" ? qa4ProjectHub.projects || [] : [];
  if (typeof openModal !== "function") return;
  openModal("Something's wrong", `<form id="onepaneSupportForm" class="qa-form"><p>Builds a local support bundle. Nothing is uploaded. The preview removes emails, long hex strings, and fields whose names look like secrets.</p>
    <label>Project<select name="project">${projects.map(project => `<option value="${esc(project.id)}">${esc(project.name || project.id)}</option>`).join("") || `<option value="">No project</option>`}</select></label>
    <label>Workspace<select name="workspace" id="onepaneSupportWorkspace"></select></label>
    <button class="btn" type="submit">Show redaction preview</button>
    <pre id="onepaneSupportPreview" class="onepane-support-preview" hidden></pre>
    <button class="btn primary" type="button" id="onepaneSupportDownload" hidden>Download bundle</button>
    <p class="error" id="onepaneSupportError" role="status"></p></form>`);
  const fill = () => {
    const project = projects.find(item => item.id === $("#onepaneSupportForm")?.project?.value);
    const spaces = project && typeof qa4Workspaces === "function" ? qa4Workspaces(project) : [];
    const select = $("#onepaneSupportWorkspace");
    if (select) select.innerHTML = spaces.map(space => `<option value="${esc(space.id)}">${esc(space.name || space.id)}</option>`).join("") || `<option value="">No workspace</option>`;
  };
  fill();
  const form = $("#onepaneSupportForm");
  if (form?.project) form.project.onchange = fill;
  let scope = null;
  form.onsubmit = async event => {
    event.preventDefault();
    const data = Object.fromEntries(new FormData(form));
    scope = { workspace_id: onepaneWorkspace, project_id: data.project, project_workspace_id: data.workspace };
    const error = $("#onepaneSupportError");
    try {
      const snapshot = await apiRequest(`/v1/qa/workspace-snapshot?workspace_id=${encodeURIComponent(scope.workspace_id)}&project_id=${encodeURIComponent(scope.project_id)}&project_workspace_id=${encodeURIComponent(scope.project_workspace_id)}`);
      const preview = $("#onepaneSupportPreview");
      preview.hidden = false;
      preview.textContent = JSON.stringify(present.redactPreview(snapshot), null, 2).slice(0, 20000);
      $("#onepaneSupportDownload").hidden = false;
      if (error) error.textContent = "";
    } catch (ex) {
      if (error) error.textContent = ex.message;
    }
  };
  $("#onepaneSupportDownload").onclick = async () => {
    if (!scope) return;
    const response = await fetch("/v1/qa/workspace-bundle", { method: "POST", headers: { "content-type": "application/json", "X-OnePane-CSRF": csrf() }, body: JSON.stringify(scope) });
    if (!response.ok) {
      const error = $("#onepaneSupportError");
      if (error) error.textContent = "The bundle was not created.";
      return;
    }
    const blob = await response.blob();
    const url = URL.createObjectURL(blob);
    const link = document.createElement("a");
    link.href = url;
    link.download = "onepane-support.zip";
    link.click();
    URL.revokeObjectURL(url);
  };
}

function csrf() {
  const row = document.cookie.split("; ").find(item => item.startsWith("onepane_csrf="));
  return row ? decodeURIComponent(row.split("=").slice(1).join("=")) : "";
}

