/* RC11 UX-04: human-controlled, browser-local presentation settings.
 * These choices never mutate Workspace policy, model placement, agent
 * capabilities, network grants, or autonomous operation levels.
 */
const ONEPANE_UI_PRESENTATION_KEY='onepane.ui.presentation.v1';
const ONEPANE_UI_PRESENTATION_DEFAULTS=Object.freeze({density:'balanced',controls:'guided'});
const ONEPANE_UI_DENSITIES=new Set(['comfortable','balanced','compact']);
const ONEPANE_UI_CONTROL_MODES=new Set(['guided','expanded']);

function a36InterfacePreferences(){
  let raw={};
  try{raw=JSON.parse(localStorage.getItem(ONEPANE_UI_PRESENTATION_KEY)||'{}')}catch{}
  return {
    density:ONEPANE_UI_DENSITIES.has(raw?.density)?raw.density:'balanced',
    controls:ONEPANE_UI_CONTROL_MODES.has(raw?.controls)?raw.controls:'guided'
  };
}
function a36ApplyInterfacePreferences(value=a36InterfacePreferences()){
  const prefs={
    density:ONEPANE_UI_DENSITIES.has(value?.density)?value.density:'balanced',
    controls:ONEPANE_UI_CONTROL_MODES.has(value?.controls)?value.controls:'guided'
  };
  document.documentElement.dataset.onepaneDensity=prefs.density;
  document.documentElement.dataset.onepaneControls=prefs.controls;
  return prefs;
}
function a36SaveInterfacePreferences(next){
  const before=a36InterfacePreferences();
  const prefs={
    density:ONEPANE_UI_DENSITIES.has(next?.density)?next.density:before.density,
    controls:ONEPANE_UI_CONTROL_MODES.has(next?.controls)?next.controls:before.controls
  };
  try{localStorage.setItem(ONEPANE_UI_PRESENTATION_KEY,JSON.stringify(prefs))}
  catch{
    // Browser-private presentation should work even if storage is blocked.
    // Do not misrepresent it as persisted across visits in this case.
    a36ApplyInterfacePreferences(prefs);
    return {prefs,persisted:false};
  }
  a36ApplyInterfacePreferences(prefs);
  return {prefs,persisted:true};
}
function a36RenderInterfacePreferences(){
  const prefs=a36InterfacePreferences();
  const choice=(name,value,title,description,selected)=>`<label class="onepane-ux-preference ${selected?'selected':''}">
    <input type="radio" name="${name}" value="${value}" ${selected?'checked':''}>
    <span><strong>${title}</strong><small>${description}</small></span>
  </label>`;
  return `<section class="settings-section onepane-ux-preferences" aria-labelledby="onepaneUxPreferencesTitle">
    <h2 id="onepaneUxPreferencesTitle">Make OnePane yours</h2>
    <p class="page-subtitle">Choose how much you see and how much space it uses. These preferences only change the interface in this browser; they never grant execution, cloud, network or remediation permissions.</p>
    <fieldset class="onepane-ux-preference-group">
      <legend>Information density</legend>
      <p class="page-subtitle">Adjust the spacing of panels and controls without changing your Workspace layouts.</p>
      <div class="onepane-ux-preference-options">
        ${choice('onepane-density','comfortable','Comfortable','Larger targets and more room to scan.',prefs.density==='comfortable')}
        ${choice('onepane-density','balanced','Balanced','Readable layout with practical spacing (default).',prefs.density==='balanced')}
        ${choice('onepane-density','compact','Compact','More information in the same workspace.',prefs.density==='compact')}
      </div>
    </fieldset>
    <fieldset class="onepane-ux-preference-group">
      <legend>Advanced controls</legend>
      <p class="page-subtitle">Choose how to reveal specialist configuration. Important status, permission revocation and evidence always remain accessible.</p>
      <div class="onepane-ux-preference-options">
        ${choice('onepane-controls','guided','Guided','Everyday actions first; expand advanced setup when needed (default).',prefs.controls==='guided')}
        ${choice('onepane-controls','expanded','Expanded','Show specialist setup by default; approvals remain required.',prefs.controls==='expanded')}
      </div>
    </fieldset>
    <div class="toolbar onepane-ux-preference-actions">
      <button type="button" class="btn" id="onepaneResetInterface">Restore interface defaults</button>
      <span class="page-subtitle" id="onepaneInterfaceStatus" role="status" aria-live="polite">Changes apply immediately to this browser.</span>
    </div>
  </section>`;
}
function a36BindInterfacePreferences(root=document){
  const section=root.querySelector('.onepane-ux-preferences');
  if(!section)return;
  const status=section.querySelector('#onepaneInterfaceStatus');
  const update=()=>{
    const density=section.querySelector('input[name="onepane-density"]:checked')?.value;
    const controls=section.querySelector('input[name="onepane-controls"]:checked')?.value;
    const saved=a36SaveInterfacePreferences({density,controls});
    section.querySelectorAll('.onepane-ux-preference').forEach(label=>
      label.classList.toggle('selected',!!label.querySelector('input:checked')));
    if(status)status.textContent=saved.persisted?
      'Applied and saved in this browser.':'Applied for this session; browser storage is unavailable.';
    // Do not remount a Workspace, reset drag layouts, or change active
    // Project when a personal display preference changes.
  };
  section.addEventListener('change',event=>{
    if(event.target?.matches('input[name="onepane-density"],input[name="onepane-controls"]'))update();
  });
  section.querySelector('#onepaneResetInterface')?.addEventListener('click',()=>{
    for(const [name,value]of Object.entries(ONEPANE_UI_PRESENTATION_DEFAULTS)){
      const radio=section.querySelector(`input[name="onepane-${name}"][value="${value}"]`);
      if(radio)radio.checked=true;
    }
    update();
  });
}
a36ApplyInterfacePreferences();
