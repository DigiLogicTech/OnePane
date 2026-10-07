const UI_THEME_SWATCHES={
 system:{name:"System",description:"Follow Windows appearance",bg:"#111822",panel:"#1b2937",accent:"#4b9ddd",text:"#f2f6fa"},
 light:{name:"Light",description:"Soft blue-grey surfaces",bg:"#e9eef4",panel:"#f6f8fb",accent:"#1676c5",text:"#182532"},
 dark:{name:"Dark",description:"Deep slate and clean blue",bg:"#0c1119",panel:"#141c27",accent:"#67aaff",text:"#edf3f9"},
 graphite:{name:"Graphite",description:"Neutral graphite and silver",bg:"#191c20",panel:"#25292e",accent:"#e2e3df",text:"#f1f3f4"},
 midnight:{name:"Midnight",description:"Blue-black, indigo depth",bg:"#040818",panel:"#0b1534",accent:"#8a9aff",text:"#edf0ff"},
 ocean:{name:"Ocean",description:"Deep teal and cyan",bg:"#031e24",panel:"#08323b",accent:"#3adad2",text:"#defcf9"},
 forest:{name:"Forest",description:"Evergreen and mint",bg:"#07140f",panel:"#10281c",accent:"#58dda4",text:"#e7f8ec"},
 violet:{name:"Violet",description:"Plum surfaces and lilac",bg:"#100a1c",panel:"#211630",accent:"#b58aff",text:"#f6edff"},
 ember:{name:"Ember",description:"Warm charcoal and coral",bg:"#180d09",panel:"#2a1812",accent:"#ff9a60",text:"#fff0e8"}
};
function uiThemeSwatch(id){
 const base=UI_THEME_SWATCHES[id];if(base)return base;
 const pack=qa5ThemePacks()[id]||{},vars=pack.vars||{};
 const safe=x=>typeof x==="string"&&/^#[a-f\d]{3,8}$/i.test(x)?x:null;
 return {name:pack.name||id,description:"Installed theme package",bg:safe(vars["--bg"])||"#15202d",panel:safe(vars["--panel"])||"#203040",accent:safe(vars["--accent"])||"#6aaaff",text:safe(vars["--text"])||"#ffffff"}
}
function uiPreviewTheme(id){
 const s=uiThemeSwatch(id),active=state.theme===id;
 const style=`--preview-bg:${s.bg};--preview-panel:${s.panel};--preview-accent:${s.accent};--preview-text:${s.text}`;
 return `<button class="theme-choice theme-preview ${active?"active":""}" type="button" data-settings-theme="${escapeHtml(id)}" aria-pressed="${active}" aria-label="Use ${escapeHtml(s.name)} theme" title="${escapeHtml(s.description)}">
  <span class="theme-preview-stage" style="${escapeHtml(style)}" aria-hidden="true">
   <span class="theme-preview-rail"></span><span class="theme-preview-work">
    <span class="theme-preview-title"></span><span class="theme-preview-row"></span>
    <span class="theme-preview-row"></span><span class="theme-preview-action"></span>
   </span>
  </span><span class="theme-preview-footer"><strong>${escapeHtml(s.name)}</strong><small>${escapeHtml(s.description)}</small></span></button>`;
}
qa5ThemeButtons=function(){
 const ids=[...Object.keys(THEME_PALETTES),...Object.keys(qa5ThemePacks())];
 return ids.filter((x,i,a)=>a.indexOf(x)===i).map(uiPreviewTheme).join("");
};
Object.assign(THEME_PALETTES.dark,{mode:"dark",caption:"#141c27",text:"#edf3f9",border:"#35455a"});
Object.assign(THEME_PALETTES.graphite,{mode:"dark",caption:"#25292e",text:"#f1f3f4",border:"#515960"});
Object.assign(THEME_PALETTES.midnight,{mode:"dark",caption:"#0b1534",text:"#edf0ff",border:"#354780"});
Object.assign(THEME_PALETTES.ocean,{mode:"dark",caption:"#08323b",text:"#defcf9",border:"#26616b"});
function uiProgressMarkup({label="",stage="",percent=null,done=0,total=0}={}){
 const known=Number.isFinite(Number(percent))&&percent!=null&&Number(total)>0;
 const value=known?Math.min(100,Math.max(0,Number(percent))):null;
 const detail=known?`${value.toFixed(0)}%`:"In progress";
 return `<div class="onepane-progress" role="group" aria-label="${escapeHtml(label||stage||"Installation progress")}">
 <div class="onepane-progress-label"><span>${escapeHtml(stage||label||"Working…")}</span><strong>${escapeHtml(detail)}</strong></div>
 <div class="onepane-progress-track ${known?"":"indeterminate"}" role="progressbar" ${known?`aria-valuemin="0" aria-valuemax="100" aria-valuenow="${value}"`:`aria-valuetext="${escapeHtml(stage||"In progress")}"`}><span ${known?`style="width:${value}%"`:""}></span></div>
 ${known&&Number(total)>0?`<div class="page-subtitle">${escapeHtml(bytesQA(done))} of ${escapeHtml(bytesQA(total))}</div>`:""}
 </div>`;
}
