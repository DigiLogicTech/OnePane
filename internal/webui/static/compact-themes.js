function a11ThemeColors(id){
 const sample=uiThemeSwatch(id),pack=qa5ThemePacks()[id]||{},vars=pack.vars||{};
 const safe=(v,fallback)=>typeof v==="string"&&/^#[0-9a-f]{3,8}$/i.test(v)?v:fallback;
 const raised={dark:"#263548",graphite:"#3a4148",midnight:"#203365",ocean:"#19545c",forest:"#264936",violet:"#422e55",ember:"#492d22",light:"#dce5ee",system:"#253647"};
 return [safe(vars["--bg"],sample.bg),safe(vars["--panel"],sample.panel),safe(vars["--panel-3"],raised[id]||sample.panel),safe(vars["--accent"],sample.accent),safe(vars["--text"],sample.text)];
}
function a11ThemeTile(id){
 const info=uiThemeSwatch(id),colors=a11ThemeColors(id),selected=id===state.theme;
 const displayName=String(info.name||id).replace(/^./,letter=>letter.toLocaleUpperCase());
 const names=["Background","Panel","Elevated","Accent","Text"];
 return `<button type="button" class="theme-choice theme-compact ${selected?"active":""}" data-settings-theme="${escapeHtml(id)}" aria-pressed="${selected}" aria-label="Select ${escapeHtml(displayName)} theme">
   <span class="theme-compact-header"><strong>${escapeHtml(displayName)}</strong><span aria-hidden="true">${selected?"✓":""}</span></span>
   <span class="theme-compact-swatches" aria-label="Theme colour palette">${colors.map((c,i)=>`<span title="${names[i]}: ${escapeHtml(c)}" style="background-color:${escapeHtml(c)}!important"></span>`).join("")}</span>
  </button>`
}
qa5ThemeButtons=function(){
 const installed=Object.keys(qa5ThemePacks()).filter(id=>!Object.prototype.hasOwnProperty.call(THEME_PALETTES,id));
 const builtins=Object.keys(THEME_PALETTES);
 return `<section class="theme-collection"><div class="theme-collection-label">Built-in themes</div><div class="theme-compact-grid">${builtins.map(a11ThemeTile).join("")}</div></section>`+
  (installed.length?`<section class="theme-collection"><div class="theme-collection-label">Installed theme packages</div><div class="theme-compact-grid">${installed.map(a11ThemeTile).join("")}</div></section>`:"");
};
