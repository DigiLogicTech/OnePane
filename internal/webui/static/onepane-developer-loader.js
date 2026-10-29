/* Loads QA and release-smoke scripts only for developer mode or the
   installed UI smoke query. Normal sessions keep the script tags inert. */
(function() {
  var query = location.search || "";
  var enabled = false;
  try { enabled = localStorage.getItem("onepane:developer-mode") === "1"; } catch (error) { enabled = false; }
  if (query.indexOf("onepane_release_smoke=1") !== -1 || query.indexOf("onepane_developer=1") !== -1) enabled = true;
  document.documentElement.dataset.developer = enabled ? "on" : "off";
  window.onepaneDeveloperMode = enabled;
  if (!enabled) return;
  var files = [
    "/release-smoke.js",
    "/qa-round2-discovery-logs.js",
    "/qa-report-summary.js",
    "/qa-incident-capture.js",
    "/qa-consolidated-support.js",
    "/qa-trace-correlation.js",
    "/qa-execution-provenance.js",
    "/qa-reproduction-note.js",
    "/qa-api-capture.js"
  ];
  for (var i = 0; i < files.length; i++) {
    document.write('<script src="' + files[i] + '"><\/script>');
  }
})();
