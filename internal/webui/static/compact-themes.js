function a11ThemeColors(id){
 const sample=uiThemeSwatch(id),pack=qa5ThemePacks()[id]||{},vars=pack.vars||{};
 const safe=(v,fallback)=>typeof v==="string"&&/^#[0-9a-f]{3,8}$/i.test(v)?v:fallback;
 return [safe(vars["--bg"],sample.bg),safe(vars["--panel"],sample.panel),safe(vars["--panel-3"],sample.panel),safe(vars["--accent"],sample.accent),safe(vars["--text"],sample.text)];
}
function a11ThemeTile(id){
 const info=uiThemeSwatch(id),colors=a11ThemeColors(id),selected=id===state.theme;
 const names=["Background","Panel","Elevated","Accent","Text"];
 return `<button type="button" class="theme-choice theme-compact ${selected?"active":""}" data-settings-theme="${escapeHtml(id)}" aria-pressed="${selected}" aria-label="Select ${escapeHtml(info.name)} theme">
   <span class="theme-compact-header"><strong>${escapeHtml(info.name)}</strong><span aria-hidden="true">${selected?"✓":""}</span></span>
   <span class="theme-compact-swatches" aria-label="Theme colour palette">${colors.map((c,i)=>`<span title="${names[i]}: ${escapeHtml(c)}" style="background:${escapeHtml(c)}"></span>`).join("")}</span>
  </button>`
}
qa5ThemeButtons=function(){
 const installed=Object.keys(qa5ThemePacks()).filter(id=>!Object.prototype.hasOwnProperty.call(THEME_PALETTES,id));
 const builtins=Object.keys(THEME_PALETTES);
 return `<section class="theme-collection"><div class="theme-collection-label">Built-in themes</div><div class="theme-compact-grid">${builtins.map(a11ThemeTile).join("")}</div></section>`+
  (installed.length?`<section class="theme-collection"><div class="theme-collection-label">Installed theme packages</div><div class="theme-compact-grid">${installed.map(a11ThemeTile).join("")}</div></section>`:"");
};
