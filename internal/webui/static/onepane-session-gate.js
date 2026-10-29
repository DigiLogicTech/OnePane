/* Waits for the honest-UI module before the session paints its first view.
   The module marks ready; a short timeout keeps sign-in working if it fails. */
window.onepaneUIReady = new Promise(resolve => { window.__onepaneMarkReady = resolve; });
const onepaneEnterBase = enterOnePane;
enterOnePane = async function() {
  try {
    await Promise.race([
      window.onepaneUIReady,
      new Promise(resolve => setTimeout(resolve, 2500))
    ]);
  } catch { /* sign-in still proceeds */ }
  return onepaneEnterBase.apply(this, arguments);
};
