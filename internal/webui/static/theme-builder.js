/* RC8 user-created themes built on the existing OnePane theme-pack mechanism.
 * Only concrete HEX colours are accepted; no untrusted CSS expressions. */
const QA8_THEME_COLORS=[
 ["--bg","App background","#111720"],["--panel","Panels","#23282f"],
 ["--panel-2","Cards and dropdowns","#2c323a"],["--panel-3","Hover surface","#39434e"],
 ["--text","Text","#ecf1f6"],["--muted","Secondary text","#a2afc0"],
 ["--border","Borders","#42505d"],["--accent","Accent","#65b6e8"],
 ["--on-accent","Accent text","#081421"],["--accent-soft","Accent highlight","#243b50"],
 ["--good","Success","#35cc91"],["--warn","Warning","#f2bf56"],["--bad","Errors","#f17476"],
 ["--scrollbar-track","Scrollbar track","#222831"],["--scrollbar-thumb","Scrollbar thumb","#56687b"],
 ["--scrollbar-thumb-hover","Scrollbar hover","#6c8297"],["--scrollbar-thumb-active","Scrollbar active","#81a0c1"]
];
function qa8HexColor(text,fallback){
 const raw=String(text||"").trim();
 if(/^#[0-9a-f]{6}$/i.test(raw))return raw.toLowerCase();
 if(/^#[0-9a-f]{3}$/i.test(raw))return "#"+[...raw.slice(1)].map(c=>c+c).join("").toLowerCase();
 const rgb=/^rgba?\(\s*(\d+)\s*,\s*(\d+)\s*,\s*(\d+)/i.exec(raw);
 if(rgb){return "#"+rgb.slice(1,4).map(x=>Math.min(255,+x).toString(16).padStart(2,"0")).join("");}
 return fallback;
}
function qa8ThemeSeed(){
 const pack=qa5ThemePacks()[state.theme];
 const style=getComputedStyle(document.documentElement);
 return Object.fromEntries(QA8_THEME_COLORS.map(([key,,fallback])=>
  [key,qa8HexColor(pack?.vars?.[key]||style.getPropertyValue(key),fallback)]));
}
function qa5OpenThemeBuilder(){
 const vars=qa8ThemeSeed(),origin=qa5ThemePacks()[state.theme],seedName=origin?.name||"My OnePane Theme";
 const fields=QA8_THEME_COLORS.map(([key,label])=>`<label class="qa8-theme-field"><span>${escapeHtml(label)}</span><input type="color" data-theme-color="${key}" value="${vars[key]}" aria-label="${escapeHtml(label)}"><input type="text" maxlength="7" data-theme-hex="${key}" value="${vars[key]}" spellcheck="false" aria-label="${escapeHtml(label)} HEX"></label>`).join("");
 openModal("Create custom theme",`<form id="qa8ThemeForm" class="qa-form">
   <p class="page-subtitle">Choose colours from the current theme, save your own named palette, and apply it throughout OnePane. Built-in themes are never modified.</p>
   <label>Theme name<input name="name" maxlength="60" required value="${escapeHtml(seedName)}"></label>
   <label>Theme style<select name="mode"><option value="dark">Dark</option><option value="light">Light</option></select></label>
   <div class="qa8-theme-colors">${fields}</div>
   <div class="toolbar"><button type="submit" class="btn primary">Save and apply theme</button></div><p id="qa8ThemeError" class="error" role="alert"></p>
 </form>`);
 const form=$("#qa8ThemeForm");if(!form)return;
 form.elements.mode.value=origin?.mode||effectiveThemePalette().mode||"dark";
 $$("[data-theme-color]",form).forEach(input=>{
   input.addEventListener("input",()=>{form.querySelector('[data-theme-hex="'+input.dataset.themeColor+'"]').value=input.value;});
 });
 $$("[data-theme-hex]",form).forEach(input=>{
   input.addEventListener("change",()=>{
     const k=input.dataset.themeHex,hex=qa8HexColor(input.value,"");
     if(hex){input.value=hex;form.querySelector('[data-theme-color="'+k+'"]').value=hex}
   });
 });
 form.onsubmit=e=>{
  e.preventDefault();
  const name=form.elements.name.value.trim();
  if(!name){$("#qa8ThemeError").textContent="Enter a theme name.";return}
  const chosen={};
  for(const [key] of QA8_THEME_COLORS){
   const hex=qa8HexColor(form.querySelector('[data-theme-hex="'+key+'"]').value,"");
   if(!hex){$("#qa8ThemeError").textContent="Choose a valid HEX colour for "+key;return}
   chosen[key]=hex;
  }
  const id="user-"+Date.now().toString(36);
  const packs=qa5ThemePacks();
  packs[id]={name,mode:form.elements.mode.value,vars:chosen,caption:chosen["--panel"],caption_text:chosen["--text"],border:chosen["--border"]};
  qa5SaveStorage(QA5_THEME_PACK_KEY,packs);
  closeModal();applyTheme(id);renderSettings();notice("Custom theme saved and applied.");
 };
}
function qa5ExportTheme(){
 const pack=qa5ThemePacks()[state.theme];
 if(!pack){notice("Select a custom theme to export, or create one first.","bad");return}
 const data={id:state.theme,...pack},json=JSON.stringify(data,null,2);
 const blob=new Blob([json],{type:"application/json"});
 const url=URL.createObjectURL(blob);
 const a=document.createElement("a");a.href=url;a.download=state.theme+".json";a.click();
 setTimeout(()=>URL.revokeObjectURL(url),1500);
}
