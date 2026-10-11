/* First-class Project Library route. A cross-Project personal Library is a
 * distinct future tenancy feature; assets here remain Project-owned. */
pages.library={title:"Library",icon:"▤"};
if(!navItems.some(row=>row[0]==="library"))navItems.splice(4,0,["library","▤","Library"]);
let a47LibraryProjectID="";
async function a47RenderLibraryRoute(){
 await qa4LoadProjectHub(false);
 const projects=Array.isArray(qa4ProjectHub.projects)?qa4ProjectHub.projects:[];
 if(!projects.some(p=>p.id===a47LibraryProjectID))a47LibraryProjectID=qa4ActiveProject()?.id||projects[0]?.id||"";
 const project=projects.find(p=>p.id===a47LibraryProjectID);
 const host=$("#viewHost");
 const options=projects.map(p=>'<option value="'+escapeHtml(p.id)+'"'+(p.id===a47LibraryProjectID?' selected':'')+'>'+escapeHtml(p.name||"Project")+'</option>').join("");
 host.innerHTML='<section class="page a47-library-page">'+pageHeader("Library","Versioned Project assets, with explicit Workspace access grants.")+
 '<div class="a47-library-toolbar"><label>Project Library <select id="a47ProjectSelector" aria-label="Project Library">'+options+'</select></label>'+
 '<button class="btn" id="a47RefreshLibrary" type="button">Refresh</button></div>'+
 '<section id="a47LibraryContent" class="panel-card a46-library" data-global-library="true"></section></section>';
 const slot=host.querySelector("#a47LibraryContent");
 if(!project){slot.innerHTML='<div class="empty-state">Create a Project to begin building its Library.</div>';return}
 host.querySelector("#a47ProjectSelector").onchange=e=>{a47LibraryProjectID=e.target.value;a47RenderLibraryRoute()};
 host.querySelector("#a47RefreshLibrary").onclick=()=>a47RenderLibraryRoute();
 await a46RenderLibrary(project,{id:"",name:"Project"},slot);
}
const a47BaseRenderActive=renderActiveView;
renderActiveView=async function(){
 const tab=currentTab();
 if(tab?.route!=="library")return a47BaseRenderActive();
 const epoch=++qa31ViewEpoch;
 if(tab.state==="suspended")tab.state="active";
 try{await a47RenderLibraryRoute()}
 finally{if(epoch===qa31ViewEpoch){const host=$("#viewHost");if(host)host.dataset.renderedRoute="library";renderNav()}}
};
