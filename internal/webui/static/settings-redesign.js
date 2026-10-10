/* Alpha 3.2 Settings redesign.
 * Presentation only: original IDs, save actions, and backend settings remain
 * authoritative in app.js. This module provides the layout and navigation. */
const A36_SETTINGS_META={
 overview:{label:"Overview",icon:"◫",group:"",description:"Application configuration, shortcuts and current preferences.",scope:"Application",keywords:"home dashboard quick start summary"},
 general:{label:"General",icon:"⚙",group:"Personalisation",description:"Language, startup destination and guided setup.",scope:"Application",keywords:"language localisation landing startup tour"},
 appearance:{label:"Appearance",icon:"◐",group:"Personalisation",description:"Choose a theme or install a theme package.",scope:"Application",keywords:"themes light dark graphite visual design"},
 defaults:{label:"Workspace defaults",icon:"▱",group:"Workspace & policy",description:"Starting configuration for newly created Workspaces only.",scope:"New Workspaces only",keywords:"orchestration council teams seats routing browser computer remote"},
 agents:{label:"Agents & Research",icon:"♙",group:"Workspace & policy",description:"Assistant compute preference and new research template defaults.",scope:"Application defaults",keywords:"research assistant compute cpu gpu teams"},
 security:{label:"Security & approvals",icon:"⛨",group:"Workspace & policy",description:"Approval levels for governed operations.",scope:"Application policy",keywords:"security permissions approval levels high medium low"},
 models:{label:"Models & Compute",icon:"◇",group:"Infrastructure",description:"Model storage pool and default execution preferences.",scope:"Local machine",keywords:"model pool path disk storage runtime cpu gpu"},
 providers:{label:"Providers & Auth",icon:"⌁",group:"Infrastructure",description:"OAuth provider configurations and credential entry points.",scope:"Workspace providers",keywords:"oauth api key credential provider auth secrets"},
 nodes:{label:"Nodes & Federation",icon:"⬡",group:"Infrastructure",description:"Device enrollment and remote model management.",scope:"Fleet administration",keywords:"nodes federation remote pairing compute"},
 skills:{label:"Skills & Tools",icon:"✦",group:"Infrastructure",description:"Skill packages, capability packs and tool permissions.",scope:"Governed tools",keywords:"skills tools package upload bundle"},
 updates:{label:"Updates",icon:"▤",group:"Maintenance",description:"Release channel and product tour.",scope:"Application",keywords:"update releases channel tour"},
 diagnostics:{label:"Debug & Diagnostics",icon:"⊙",group:"Maintenance",description:"Opt-in browser incident capture and links to authorised backend evidence.",scope:"Browser-local and per-source",keywords:"debug qa incident capture support zip logs troubleshooting browser"
};
let a36SettingsQuery="";
let a36SettingsRequest=0;
function a36SettingsList(){
 return ["overview","general","appearance","defaults","agents","security","models","providers","nodes","skills","diagnostics","updates"].map(id=>[id,A36_SETTINGS_META[id]]).filter(x=>x[1]);
}
function a36SettingsNav(){
 const rows=a36SettingsList();
 let current="";
 const body=rows.map(([id,m])=>{
  let group="";
  if(m.group!==current){current=m.group;group=current?'<div class="a36-settings-group">'+escapeHtml(current)+'</div>':""}
  const name=escapeHtml(m.label),isCurrent=a31SettingsView===id;
  return group+'<button type="button" class="a36-settings-link '+(isCurrent?"active":"")+'" data-a31-settings="'+id+'" data-settings-search="'+escapeHtml((m.label+" "+m.keywords+" "+m.group).toLowerCase())+'" aria-current="'+(isCurrent?"page":"false")+'"><span class="a36-settings-icon" aria-hidden="true">'+m.icon+'</span><span class="a36-settings-nav-name">'+name+'</span><span class="a36-settings-nav-arrow" aria-hidden="true">›</span></button>'
 }).join("");
 return '<aside class="a36-settings-sidebar" aria-label="Settings categories"><label for="a31SettingsSearch">Find a setting</label><input id="a31SettingsSearch" type="search" autocomplete="off" placeholder="Search settings…" value="'+escapeHtml(a36SettingsQuery)+'"><div class="a36-settings-navigation">'+body+'<p class="a36-settings-empty" hidden>No matching settings</p></div><div class="a36-settings-sidebar-foot"><strong>Workspace-owned controls</strong><span>Configure individual projects, model routing and research policy in their Workspaces.</span><button class="btn" type="button" data-route="projects">Open Projects</button></div></aside>';
}
function a36SettingsPreviewCard(id,kicker,value,detail){
 return '<button class="a36-settings-preview" type="button" data-a31-settings="'+id+'"><span class="a36-settings-preview-kicker">'+escapeHtml(kicker)+'</span><strong>'+escapeHtml(value)+'</strong><span>'+escapeHtml(detail)+'</span><span class="a36-settings-preview-link">Configure <span aria-hidden="true">→</span></span></button>';
}
function a36SettingsOverview(){
 const p=qa5Prefs(),d=p.workspace_defaults||{};
 const locale=qa5AllLanguages()?.[p.language||"en-AU"]?.name||p.language||"Default";
 const approval=String(state.approvalLevel||"medium");
 const theme=String(state.theme||"system");
 return '<div class="a36-settings-overview">'+
  '<div class="a36-settings-intro"><h2>Settings overview</h2><p>Manage how OnePane looks, starts and operates. Workspace-specific configuration stays in its owning Workspace.</p></div>'+
  '<div class="a36-settings-preview-grid">'+
  a36SettingsPreviewCard("general","Language & startup",locale,"Landing page: "+(p.landing||"operations"))+
  a36SettingsPreviewCard("appearance","Appearance",theme,"Theme and presentation")+
  a36SettingsPreviewCard("defaults","New Workspace defaults",d.orchestration||"direct","Routing and capability defaults")+
  a36SettingsPreviewCard("security","Approvals",approval,"Governed execution policy")+
  '</div><section class="a36-settings-quick"><div><h3>Manage your environment</h3><p>Common administration tasks are available in their dedicated pages.</p></div><div class="a36-settings-quick-links">'+
  '<button type="button" class="btn" data-route="models">Models</button>'+
  '<button type="button" class="btn" data-route="nodes">Nodes</button>'+
  '<button type="button" class="btn" data-route="agents">Agents</button>'+
  '<button type="button" class="btn" data-route="skills">Skills</button>'+
  '<button type="button" class="btn" data-route="secrets">Secrets</button></div></section>'+
  '<section class="a36-settings-footnote"><strong>Scope matters</strong><p>Changes here affect global application preferences or defaults for new Workspaces. Existing Workspace settings, model placement and Research Council integrity remain independently configured.</p></section></div>';
}
function a36SettingsAugment(view,section){
 // Existing IDs, endpoints and labels remain in the underlying form.
 if(view==="general")return section.replace(/<\/section>\s*$/,'<div class="a36-settings-save"><button type="button" class="btn primary" id="a36SaveGeneral">Save startup preferences</button></div></section>');
 if(view==="updates")return section.replace(/<\/section>\s*$/,'<div class="a36-settings-save"><button type="button" class="btn primary" id="a36SaveUpdates">Save release channel</button></div></section>');
 return section;
}
async function a36RenderSettingsShell(){
 const epoch=++a36SettingsRequest,view=A36_SETTINGS_META[a31SettingsView]?a31SettingsView:"overview",meta=A36_SETTINGS_META[view];
 const host=document.querySelector("#viewHost");if(!host)return false;
 host.innerHTML='<section class="page a36-settings-page"><div class="a36-settings-top"><div><div class="a36-settings-eyebrow">ONEPANE / CONFIGURATION</div><h1>Settings</h1><p>Personalise OnePane, configure application defaults and manage integrations.</p></div><div class="a36-settings-top-actions"><button class="btn" type="button" id="a36OpenLogs">Open Logs</button></div></div><div class="a36-settings-layout">'+
  a36SettingsNav()+
  '<div class="a36-settings-content" id="a31SettingsContent"><div class="widget-body">Loading settings…</div></div></div></section>';
 const section=view==="overview"?a36SettingsOverview():a36SettingsAugment(view,await a31SettingsSection());
 if(epoch!==a36SettingsRequest||!host.querySelector(".a36-settings-page"))return false;
 const panel=host.querySelector("#a31SettingsContent");
 panel.innerHTML='<div class="a36-settings-panel-heading"><div class="a36-settings-breadcrumb">Settings <span aria-hidden="true">/</span> '+escapeHtml(meta.label)+'</div><div class="a36-settings-heading-row"><div><h2>'+escapeHtml(meta.label)+'</h2><p>'+escapeHtml(meta.description)+'</p></div><span class="a36-settings-scope">'+escapeHtml(meta.scope)+'</span></div></div><div class="a36-settings-panel-body">'+section+'</div>';
 return true;
}
function a36BindSettingsSearch(){
 const field=document.querySelector("#a31SettingsSearch");if(!field)return;
 const apply=()=>{
  const q=String(field.value||"").trim().toLowerCase();
  a36SettingsQuery=field.value;
  const items=[...document.querySelectorAll(".a36-settings-navigation [data-settings-search]")];
  let count=0;
  for(const item of items){const show=!q||item.dataset.settingsSearch.includes(q);item.hidden=!show;if(show)count++}
  const groups=[...document.querySelectorAll(".a36-settings-group")];
  for(const g of groups){let n=g.nextElementSibling,any=false;while(n&&!n.classList.contains("a36-settings-group")){if(n.matches("[data-settings-search]")&&!n.hidden)any=true;n=n.nextElementSibling}g.hidden=!any}
  const empty=document.querySelector(".a36-settings-empty");if(empty)empty.hidden=count!==0;
 };
 field.addEventListener("input",apply);
 field.addEventListener("keydown",e=>{if(e.key!=="Enter")return;const first=document.querySelector(".a36-settings-navigation [data-settings-search]:not([hidden])");if(first){e.preventDefault();first.click()}});
 apply();
}
