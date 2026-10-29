/* Starts the honest UI after the presentation module loads. */
import("/onepane-present.js").then(mod => {
  window.onepanePresent = mod;
  install();
}).catch(error => {
  console.error(error);
  window.__onepaneMarkReady?.();
});
