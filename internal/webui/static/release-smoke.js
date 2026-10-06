(()=>{
  if(new URLSearchParams(location.search).get("onepane_release_smoke")!=="1")return;

  const results=[],sleep=ms=>new Promise(r=>setTimeout(r,ms));
  const check=(v,label)=>{if(!v)throw new Error(label);results.push(label);return v};
  const waitFor=async(fn,label,timeout=20000)=>{const end=Date.now()+timeout;while(Date.now()<end){try{const v=await fn();if(v)return v}catch{}await sleep(75)}throw new Error("Timed out: "+label)};
  const clone=v=>JSON.parse(JSON.stringify(v));
  const json=(body,status=200)=>new Response(JSON.stringify(body),{status,headers:{"content-type":"application/json"}});
  const workspace={id:"pws-release",name:"Main workspace",widgets:[
    {id:"pw-release-follow",type:"follow",title:"Follow",col:4,row:5},
    {id:"pw-release-modelstack",type:"modelstack",title:"Model Stack",col:4,row:5},
    {id:"pw-release-chat",type:"chat",title:"Workspace chat",col:4,row:5},
    {id:"pw-release-tasks",type:"tasks",title:"Tasks",col:6,row:4},
    {id:"pw-release-notes",type:"notes",title:"Notes",col:6,row:4},
    {id:"pw-release-settings",type:"settings",title:"Workspace settings",col:6,row:5}
  ],orchestration:{mode:"direct",supervisor:{model:"auto",agent:"onepane-default"},team:{model:"auto",agent:"onepane-default",count:2},council:{model:"auto",agent:"onepane-default",count:2}}};
  const workspace2={id:"pws-release-2",name:"Disposable workspace",widgets:[{id:"pw-release-2-notes",type:"notes",title:"Notes",col:6,row:4}],orchestration:{mode:"direct",supervisor:{model:"auto",agent:"onepane-default"},team:{model:"auto",agent:"onepane-default",count:2},council:{model:"auto",agent:"onepane-default",count:2}}};
  let project={id:"project-release",workspace_id:"workspace-release",name:"Release QA Project",description:"Installed behavioural acceptance",status:"active",revision:1,project_policy:{onepane_ui:{workspaces:[workspace,workspace2]}}};
  let projectPatchCount=0,turns=[];
  const originalFetch=window.fetch.bind(window);
  window.EventSource=class{addEventListener(){}close(){}};

  window.fetch=async(input,opts={})=>{
    const u=new URL(typeof input==="string"?input:input.url,location.origin),path=u.pathname,method=String(opts.method||"GET").toUpperCase();
    if(!path.startsWith("/v1/"))return originalFetch(input,opts);
    if(path==="/v1/setup/status")return json({required:false});
    if(path==="/v1/auth/me")return json({principal_id:"qa-release",display_name:"Release QA",workspaces:[{id:"workspace-release"}],capabilities:[]});
    if(path==="/v1/about")return originalFetch(input,opts);
    if(path==="/v1/health")return json({status:"ok"});
    if(path==="/v1/nodes")return json({nodes:[{id:"node-release",name:"RELEASE-PC",local:true,trust_state:"local",status:"ready",architecture:"amd64",os_name:"Windows"}]});
    if(path==="/v1/projects"&&method==="GET")return json(project.status==="active"?[clone(project)]:[]);
    if(path==="/v1/projects/project-release"&&method==="DELETE"){
      const body=JSON.parse(opts.body||"{}");if(Number(body.expected_revision)!==Number(project.revision))return json({error:"revision conflict"},409);
      project={...project,status:"archived",revision:project.revision+1};return json(clone(project));
    }
    if(path==="/v1/projects/project-release"&&method==="PATCH"){
      const body=JSON.parse(opts.body||"{}");
      if(Number(body.expected_revision)!==Number(project.revision))return json({error:"revision conflict"},409);
      project={...project,revision:project.revision+1,project_policy:clone(body.project_policy||project.project_policy)};projectPatchCount++;return json(clone(project));
    }
    if(path==="/v1/skills/tool-bundles")return json([{id:"bundle-release",name:"Release Tools",tools:["read"]}]);
    if(path==="/v1/assistant/threads"&&method==="GET")return json([{id:"thread-release",title:"Release Assistant"}]);
    if(path==="/v1/assistant/threads/thread-release/turns"&&method==="GET")return json(clone(turns));
    if(path==="/v1/assistant/threads/thread-release/turns"&&method==="POST"){
      const body=JSON.parse(opts.body||"{}");turns.push({role:"user",content:body.content||""},{role:"assistant",content:"No eligible reasoning model is configured. Configure a model to run reasoning."});return json(turns.at(-1),201);
    }
    if(["/v1/tasks","/v1/routines","/v1/providers","/v1/events","/v1/provider-presets","/v1/agent-runtime-presets","/v1/scheduler/candidates","/v1/local-ai/catalog","/v1/skills/packages","/v1/skills/assignments","/v1/agent-profiles","/v1/teams","/v1/team-presets","/v1/provider-oauth/configs"].includes(path))return json([]);
    if(path==="/v1/local-ai/deployments")return json({deployments:[]});
    if(path==="/v1/local-ai/components")return json({});
    if(path==="/v1/settings/local-ai")return json({model_pool_path:""});
    return json({});
  };

  const post=status=>{try{window.chrome?.webview?.postMessage(`onepane-ui-e2e|${status}|${results.join("; ")}`)}catch{}};
  let pointerSeq=40;
  const pointer=(el,type,x,y,id)=>{
    const Ctor=window.PointerEvent||window.MouseEvent;
    el.dispatchEvent(new Ctor(type,{bubbles:true,cancelable:true,composed:true,pointerId:id,pointerType:"mouse",isPrimary:true,button:0,buttons:type==="pointerup"?0:1,clientX:x,clientY:y}));
  };
  const gesture=async(el,dx,dy)=>{
    const r=el.getBoundingClientRect(),x=r.left+Math.max(2,Math.min(r.width-2,r.width/2)),y=r.top+Math.max(2,Math.min(r.height-2,r.height/2)),id=++pointerSeq;
    pointer(el,"pointerdown",x,y,id);await sleep(20);pointer(el,"pointermove",x+dx,y+dy,id);await sleep(35);pointer(el,"pointerup",x+dx,y+dy,id);await sleep(75);
  };
  const noOverlap=rows=>!rows.some((a,i)=>rows.slice(i+1).some(b=>a31Overlap(a,b)));
  const route=async name=>{
    const target=document.querySelector(`#primaryNav [data-route="${name}"]`)||document.querySelector(`[data-route="${name}"]`);
    check(target,`nav:${name}`);target.click();
    await waitFor(()=>document.querySelector("#viewHost")?.dataset.renderedRoute===name,`render:${name}`,30000);
    check(currentTab()?.route===name,`active:${name}`);
  };

  async function testProjectLayout(){
    await route("projects");check(document.querySelector("#qa4WorkspaceGrid"),"project workspace rendered");
    const edit=check(document.querySelector("#qa4EditWorkspace"),"workspace edit control");if(!state.projectWorkspaceEdit)edit.click();
    await waitFor(()=>document.querySelector('[data-pw-resize][data-resize-edge="e"]'),"workspace native resize handle");
    check(document.querySelector(".dashboard-edit-bar[data-pw-drag]"),"Workspace edit header is drag surface");
    check(document.querySelectorAll("[data-pw-resize]").length>=48,"Workspace exposes edge and corner resize handles for multiple components");

    let ws=check(a31CurrentWorkspace(),"workspace active for native layout");
    const follow=check(ws.widgets.find(w=>w.id==="pw-release-follow"),"workspace resize state"),beforeW=Number(follow.width);
    let root=check(document.querySelector("#qa4WorkspaceGrid"),"workspace layout root"),handle=check(document.querySelector('[data-pw-widget="pw-release-follow"] [data-pw-resize][data-resize-edge="e"]'),"workspace east resize handle");
    const rs=getComputedStyle(root),gap=parseFloat(rs.columnGap)||0,colW=(root.getBoundingClientRect().width-gap*(A31_LAYOUT_COLUMNS-1))/A31_LAYOUT_COLUMNS;
    const patchesBeforeResize=projectPatchCount;await gesture(handle,colW+gap+3,0);
    await waitFor(()=>projectPatchCount>patchesBeforeResize&&!document.querySelector("#qa4WorkspaceGrid")?.dataset.layoutSaving,"workspace pointer resize saved",30000);
    ws=check(a31CurrentWorkspace(),"workspace after pointer resize");check(Number(ws.widgets.find(w=>w.id==="pw-release-follow")?.width)>beforeW,"Workspace pointer resize changes geometry");
    check(noOverlap(ws.widgets),"Workspace pointer resize resolves overlap");

    root=check(document.querySelector("#qa4WorkspaceGrid"),"workspace root after resize");
    const drag=check(root.querySelector('[data-pw-widget="pw-release-settings"] .dashboard-edit-bar[data-pw-drag]'),"workspace native drag surface"),dragBefore=Number(ws.widgets.find(w=>w.id==="pw-release-settings")?.y),rowStep=A31_LAYOUT_ROW_PX+(parseFloat(getComputedStyle(root).rowGap)||0),patchesBeforeDrag=projectPatchCount;
    await gesture(drag,0,rowStep*2+3);
    await waitFor(()=>projectPatchCount>patchesBeforeDrag&&!document.querySelector("#qa4WorkspaceGrid")?.dataset.layoutSaving,"workspace pointer drag saved",30000);
    ws=check(a31CurrentWorkspace(),"workspace after pointer drag");check(Number(ws.widgets.find(w=>w.id==="pw-release-settings")?.y)>dragBefore,"Workspace pointer drag changes geometry");
    check(noOverlap(ws.widgets),"Workspace pointer drag resolves overlap");
    const expectedFollow=clone(ws.widgets.find(w=>w.id==="pw-release-follow")),expectedSettings=clone(ws.widgets.find(w=>w.id==="pw-release-settings"));

    await route("operations");await route("projects");
    ws=check(a31CurrentWorkspace(),"workspace reload");
    const reloadedFollow=ws.widgets.find(w=>w.id==="pw-release-follow"),reloadedSettings=ws.widgets.find(w=>w.id==="pw-release-settings");
    check(Number(reloadedFollow?.width)===Number(expectedFollow.width)&&Number(reloadedFollow?.x)===Number(expectedFollow.x)&&Number(reloadedFollow?.y)===Number(expectedFollow.y),"Workspace resized geometry survives project reload");
    check(Number(reloadedSettings?.x)===Number(expectedSettings.x)&&Number(reloadedSettings?.y)===Number(expectedSettings.y),"Workspace dragged geometry survives project reload");
    check(noOverlap(ws.widgets),"Workspace persisted geometry remains collision free");

    const disposable=check(document.querySelector('[data-qa4-workspace="pws-release-2"]'),"second workspace available for deletion");disposable.click();
    await waitFor(()=>a31CurrentWorkspace()?.id==="pws-release-2"&&document.querySelector("#a32DeleteWorkspace"),"second workspace rendered with delete action");
    const deleteButton=check(document.querySelector("#a32DeleteWorkspace"),"workspace delete action");check(!deleteButton.disabled,"workspace delete enabled when alternatives exist");const patchBeforeDelete=projectPatchCount;deleteButton.click();
    const confirmDelete=await waitFor(()=>document.querySelector("#a32ConfirmDeleteWorkspace"),"workspace delete confirmation");confirmDelete.click();
    await waitFor(()=>projectPatchCount>patchBeforeDelete&&a31CurrentWorkspace()?.id==="pws-release","workspace delete saved",30000);
    check(!qa4Workspaces(a31CurrentProject()).some(x=>x.id==="pws-release-2"),"workspace removed from durable project policy");
    check(document.querySelector("#a32DeleteWorkspace")?.disabled===true,"last workspace delete is guarded");
  }

  async function run(){
    localStorage.setItem(TOUR_KEY,TOUR_COMPLETE_VALUE);
    state.operationsWidgets=defaultState().operationsWidgets.map(x=>({...x}));
    state.operationsWidgets[0].width="NaN";state.operationsWidgets[0].height="not-a-number";state.operationsWidgets[1].x="broken";
    state.operationsLayoutVersion=4;
    persist();
    check(a31RepairPersistedUIState(),"broken persisted Operations layout repaired");
    check(!a31OperationsLayoutBroken(state.operationsWidgets),"repaired Operations layout is valid");
    check(state.operationsWidgets.every(w=>[w.x,w.y,w.width,w.height].every(Number.isFinite)),"Operations repair emits only finite geometry");
    check(Number(state.operationsWidgets.find(w=>w.id==="op-metrics")?.width)===12,"Operations malformed legacy geometry resets to meaningful default");
    await waitFor(()=>document.querySelector("#app")&&!document.querySelector("#app").classList.contains("hidden"),"application shell",30000);
    check(onepaneWorkspace==="workspace-release","mock workspace authenticated");
    renderNav();
    const projectDisclosure=check(document.querySelector('[data-a34-nav-toggle="projects"]'),"Projects disclosure"),modelDisclosure=check(document.querySelector('[data-a34-nav-toggle="models"]'),"Models disclosure");
    projectDisclosure.click();check(a34NavTreeState().projects===true&&!document.querySelector(".project-nav-tree"),"Projects tree collapses");check(JSON.parse(localStorage.getItem(STORAGE_KEY)||"{}").navTreeCollapsed?.projects===true,"Projects collapse persists");a34SetNavExpanded("projects",true);
    modelDisclosure.click();check(a34NavTreeState().models===true&&!document.querySelector(".model-nav-tree"),"Models tree collapses");a34SetNavExpanded("models",true);
    setSidebarExpanded(false);const chatIcon=check(document.querySelector("#controlChatLauncher .nav-icon"),"collapsed Chat icon"),commandIcon=check(document.querySelector('[data-action="command-palette"] .sidebar-action-icon'),"collapsed Command icon");check(chatIcon.getBoundingClientRect().width>0&&getComputedStyle(chatIcon).display!=="none","Collapsed sidebar keeps OnePane Chat icon");check(commandIcon.getBoundingClientRect().width>0&&getComputedStyle(commandIcon).display!=="none","Collapsed sidebar keeps Command icon");setSidebarExpanded(true);
    await route("projects");const workspaceDisclosure=await waitFor(()=>document.querySelector('[data-a34-project-toggle="project-release"]'),"Project Workspaces disclosure after Projects load",10000);workspaceDisclosure.click();check(a34NavTreeState()["project:project-release"]===true&&!document.querySelector('[data-a31-workspace-nav="workspace-release"]'),"Workspace list collapses per Project");check(JSON.parse(localStorage.getItem(STORAGE_KEY)||"{}").navTreeCollapsed?.["project:project-release"]===true,"Workspace collapse persists per Project");a34SetNavExpanded("project:project-release",true);
    renderInspector();
    check(document.querySelector("#inspector")?.dataset.tabMode==="single","Inspector Overview is implicit");
    check(getComputedStyle(document.querySelector("#inspector .inspector-tabs")).display==="none","single Inspector Overview rail is hidden");
    const inspectorToggle=check(document.querySelector("#inspectorRestore"),"Inspector edge toggle"),drawerToggle=check(document.querySelector("#drawerToggle"),"Logs edge toggle");
    setInspectorOpen(true);setDrawerOpen(true);syncPanelRestoreButtons();
    const inspectorOpenRect=inspectorToggle.getBoundingClientRect(),drawerOpenRect=drawerToggle.getBoundingClientRect(),inspectorOpenSize=inspectorOpenRect.height,drawerOpenSize=drawerOpenRect.width,inspectorPosBefore=a32PanelTogglePosition("inspectorTogglePosition"),drawerPosBefore=a32PanelTogglePosition("drawerTogglePosition");
    check(Math.abs(inspectorOpenRect.height-drawerOpenRect.width)<1&&Math.abs(inspectorOpenRect.width-drawerOpenRect.height)<1,"Inspector and Logs expanded controls share rotated geometry");
    check(getComputedStyle(inspectorToggle).borderRadius===getComputedStyle(drawerToggle).borderRadius,"Inspector and Logs expanded controls share shape");
    await gesture(inspectorToggle,0,70);check(a32PanelTogglePosition("inspectorTogglePosition")!==inspectorPosBefore,"Inspector toggle moves vertically");check(state.inspector==="open","Inspector drag does not collapse panel");
    await gesture(drawerToggle,90,0);check(a32PanelTogglePosition("drawerTogglePosition")!==drawerPosBefore,"Logs toggle moves horizontally");check(state.drawer==="open","Logs drag does not collapse drawer");
    setInspectorOpen(false);setDrawerOpen(false);syncPanelRestoreButtons();
    const inspectorClosedSize=inspectorToggle.getBoundingClientRect().height,drawerRestore=check(document.querySelector("#drawerRestore"),"Logs restore toggle"),drawerClosedSize=drawerRestore.getBoundingClientRect().width;
    check(inspectorClosedSize>inspectorOpenSize,"Inspector collapsed control uses longer restore shape");
    check(drawerClosedSize>drawerOpenSize,"Logs collapsed control uses longer restore shape");
    check(Math.abs(parseFloat(drawerRestore.style.left)-a32PanelTogglePosition("drawerTogglePosition"))<.2,"Logs restore retains moved position");
    setInspectorOpen(true);setDrawerOpen(true);syncPanelRestoreButtons();
    if(document.documentElement.dataset.productTour==="active"){
      document.querySelector("#tourSkip")?.click();
      await waitFor(()=>!document.documentElement.dataset.productTour,"welcome Tour cleanup");
      check(!document.querySelector(".tour-target"),"welcome Tour target cleanup");
    }

    await route("operations");check(document.querySelector("#operationsLayout"),"Operations overview");await waitFor(()=>document.querySelector('[data-op-widget="op-nodes"]')?.textContent?.includes("RELEASE-PC (Local)"),"Operations Nodes component uses hostname");
    await waitFor(()=>innerWidth>700&&document.querySelector("#operationsLayout")?.getBoundingClientRect().width>100,"visible desktop Operations geometry",30000);
    check(innerWidth>700,"Installed acceptance is exercising desktop layout");
    state.operationsWidgets=defaultState().operationsWidgets.map(x=>({...x}));a31NormalizeLayout(state.operationsWidgets);a31PersistOperationsLayout();renderOperations();
    const committedBeforeEdit=clone(state.operationsWidgets),storedBeforeEdit=clone((JSON.parse(localStorage.getItem(STORAGE_KEY)||"{}").operationsWidgets)||[]);
    check(document.querySelector("#editOperations"),"Operations edit control").click();await waitFor(()=>a31OperationsEditing()&&document.querySelector(".dashboard-edit-bar[data-op-drag]"),"Operations edit session");
    let opRoot=check(document.querySelector("#operationsLayout"),"Operations native layout root");
    await waitFor(()=>opRoot.getBoundingClientRect().width>100&&document.querySelector('[data-op-widget="op-metrics"]')?.getBoundingClientRect().width>0,"rendered Operations geometry",10000);
    check(document.querySelectorAll("[data-op-resize]").length>=56,"Operations exposes edge and corner resize handles");
    const metricsCard=check(document.querySelector('[data-op-widget="op-metrics"]'),"Operations metrics card"),metricsRect=metricsCard.getBoundingClientRect(),rootRect=opRoot.getBoundingClientRect(),rootStyle=getComputedStyle(opRoot),metricsStyle=getComputedStyle(metricsCard),metricGrid=check(metricsCard.querySelector(".operations-metrics"),"Operations metrics grid"),metricGridRect=metricGrid.getBoundingClientRect(),metricTiles=[...metricGrid.querySelectorAll(".metric-card")];
    results.push(`ops-geometry inner=${innerWidth} root=${Math.round(rootRect.width)} card=${Math.round(metricsRect.width)} display=${rootStyle.display} columns=${rootStyle.gridTemplateColumns} start=${metricsStyle.gridColumnStart} end=${metricsStyle.gridColumnEnd} inline=${metricsCard.getAttribute("style")||""}`);
    check(metricsRect.width>rootRect.width*.9,"Operations default metrics component spans the dashboard");
    const metricRows=new Map();for(const tile of metricTiles){const r=tile.getBoundingClientRect(),key=Math.round(r.top);if(!metricRows.has(key))metricRows.set(key,[]);metricRows.get(key).push(r)}
    check(metricRows.size>0&&[...metricRows.values()].every(row=>Math.abs(Math.max(...row.map(r=>r.right))-metricGridRect.right)<3),"Operations metrics fill every rendered row");

    const resizeCard=check(document.querySelector('[data-op-widget="op-tasks"]'),"Operations resize card"),resizeEast=check(resizeCard.querySelector('[data-op-resize][data-resize-edge="e"]'),"Operations east resize handle"),resizeCardRect=resizeCard.getBoundingClientRect(),resizeEastRect=resizeEast.getBoundingClientRect();
    check(resizeEastRect.left>=resizeCardRect.left-1&&resizeEastRect.right<=resizeCardRect.right+1,"Operations resize hit target stays inside card");
    const draftItems=a31OperationsLayoutItems(),taskState=draftItems.find(w=>w.id==="op-tasks"),taskWidthBefore=Number(taskState.width),opsStyle=getComputedStyle(opRoot),opsGap=parseFloat(opsStyle.columnGap)||0,opsCol=(opRoot.getBoundingClientRect().width-opsGap*(A31_LAYOUT_COLUMNS-1))/A31_LAYOUT_COLUMNS;
    await gesture(resizeEast,opsCol+opsGap+3,0);
    await waitFor(()=>Number(a31OperationsLayoutItems().find(w=>w.id==="op-tasks")?.width)>taskWidthBefore&&!document.querySelector("#operationsLayout")?.dataset.layoutSaving,"Operations pointer resize updates draft");
    check(noOverlap(a31OperationsLayoutItems()),"Operations pointer resize resolves overlap in draft");
    check(JSON.stringify(state.operationsWidgets)===JSON.stringify(committedBeforeEdit),"Operations edit does not mutate committed layout before Done");
    check(JSON.stringify((JSON.parse(localStorage.getItem(STORAGE_KEY)||"{}").operationsWidgets)||[])===JSON.stringify(storedBeforeEdit),"Operations draft is not persisted before Done");

    opRoot=check(document.querySelector("#operationsLayout"),"Operations root after resize");
    const attentionDrag=check(opRoot.querySelector('[data-op-widget="op-attention"] .dashboard-edit-bar[data-op-drag]'),"Operations native drag surface"),attentionY=Number(a31OperationsLayoutItems().find(w=>w.id==="op-attention")?.y),opRow=A31_LAYOUT_ROW_PX+(parseFloat(getComputedStyle(opRoot).rowGap)||0);
    await gesture(attentionDrag,0,opRow*2+3);
    await waitFor(()=>Number(a31OperationsLayoutItems().find(w=>w.id==="op-attention")?.y)>attentionY&&!document.querySelector("#operationsLayout")?.dataset.layoutSaving,"Operations pointer drag updates draft");
    check(noOverlap(a31OperationsLayoutItems()),"Operations pointer drag resolves overlap in draft");

    await route("tasks");check(!a31OperationsEditing(),"Operations navigation cancels edit mode");check(JSON.stringify(state.operationsWidgets)===JSON.stringify(committedBeforeEdit),"Operations navigation discards draft changes");
    await route("operations");check(!document.querySelector(".dashboard-widget.editable"),"Operations returns in view mode after cancelled edit");
    const storedAfterCancel=(JSON.parse(localStorage.getItem(STORAGE_KEY)||"{}").operationsWidgets)||[];check(JSON.stringify(storedAfterCancel)===JSON.stringify(storedBeforeEdit),"Operations cancelled edit leaves persisted layout unchanged");

    check(document.querySelector("#editOperations"),"Operations second edit control").click();await waitFor(()=>a31OperationsEditing(),"Operations second edit session");
    const custom=check(a31OperationsLayoutItems().find(w=>w.id==="op-activity"),"Operations custom layout probe");custom.x=0;custom.y=24;custom.width=6;custom.height=5;custom.col=6;custom.row=5;a31ResolveLayout(a31OperationsLayoutItems(),custom.id);a31RenderOperationsGrid();
    check(document.querySelector("#editOperations"),"Operations Done control").click();await waitFor(()=>!a31OperationsEditing(),"Operations edit commit");
    opRoot=check(document.querySelector("#operationsLayout"),"Operations layout after Done");
    const rootBeforeRefresh=opRoot,revisionBefore=Number(state.operationsLayoutRevision||0);await refreshOperationalDataQA(true);
    check(document.querySelector("#operationsLayout")===rootBeforeRefresh,"Operations polling preserves layout DOM");
    const storedOps=JSON.parse(localStorage.getItem(STORAGE_KEY)||"{}"),storedActivity=(storedOps.operationsWidgets||[]).find(w=>w.id==="op-activity");
    check(Number(storedActivity?.y)===24&&Number(state.operationsWidgets.find(w=>w.id==="op-activity")?.y)===24,"Operations Done persists custom geometry");
    check(Number(state.operationsLayoutRevision||0)===revisionBefore,"Operations polling does not rewrite layout revision");
    check(noOverlap(state.operationsWidgets),"Operations persisted geometry remains collision free");

    await route("tasks");check(!document.querySelector("#tasksBody .error"),"Tasks route");
    await route("models");check(document.querySelector("#a31ModelsRoot")&&!document.querySelector("#a31ModelsRoot .error"),"Models route");
    await route("nodes");check(document.querySelector("#a31Nodes")&&!document.querySelector("#a31Nodes .error"),"Nodes envelope");
    document.querySelector("#a31AddNode")?.click();await waitFor(()=>document.querySelector("#pairNodeForm"),"pairing modal");check(document.querySelector("#pairNodeForm"),"Add Node pairing flow");closeModal();
    await route("nodes");await waitFor(()=>document.querySelector('[data-a31-node="node-release"]'),"Node card");check(document.querySelector('[data-a31-node="node-release"] .card-title')?.textContent==="RELEASE-PC (Local)","Nodes prefer machine name and mark local device");check(document.querySelector('[data-a31-node="node-release"] .list-meta')?.textContent?.includes("node-release"),"Node ID remains secondary metadata");
    await route("agents");check(!document.querySelector("#viewHost .error"),"Agents route");

    await route("skills");const bundles=check(document.querySelector('[data-a31-skills-tab="bundles"]'),"Tool Bundles tab");bundles.click();
    await waitFor(()=>document.querySelector("#a31SkillsBody")?.textContent?.includes("Tool Bundles"),"Tool Bundles view");check(!document.querySelector("#a31SkillsBody .error"),"Skills route");

    await route("settings");check(document.querySelector("#a31SettingsContent"),"Settings route");
    document.querySelector('[data-a31-settings="appearance"]')?.click();await waitFor(()=>a31SettingsView==="appearance","Appearance settings");check(document.querySelector("[data-settings-theme]"),"Themes rendered");

    await testProjectLayout();
    const workspaceThemeSelect=check(document.querySelector(".qa7-workspace-settings select"),"Workspace settings themed select"),themeBefore=document.documentElement.dataset.theme;
    document.documentElement.dataset.theme="dark";await sleep(60);
    const workspaceSelectStyle=getComputedStyle(workspaceThemeSelect);check(workspaceSelectStyle.colorScheme.includes("dark"),"Workspace settings selects use dark native color scheme");check(workspaceSelectStyle.backgroundColor!=="rgba(0, 0, 0, 0)","Workspace settings selects keep themed background");
    document.documentElement.dataset.theme=themeBefore||"system";

    const command=check(document.querySelector('[data-action="command-palette"]'),"command launcher");command.click();await waitFor(()=>document.querySelector("#paletteInput"),"command palette");
    check(document.querySelectorAll("[data-palette-index]").length>0,"command actions populated");document.querySelector("#paletteInput").dispatchEvent(new KeyboardEvent("keydown",{key:"Escape",bubbles:true}));await waitFor(()=>!document.querySelector("#paletteInput"),"command close");

    const tour=check(document.querySelector('[data-action="product-tour"]'),"Tour launcher");tour.click();const tourCard=await waitFor(()=>document.querySelector('#tourCard[data-positioned="true"]'),"Tour positioned");
    const tourStyle=getComputedStyle(tourCard);check(tourStyle.visibility!=="hidden"&&tourStyle.display!=="none"&&Number(tourStyle.opacity||1)>0,"Tour card is visible");
    const hostRect=document.querySelector("#viewHost").getBoundingClientRect(),cardRect=tourCard.getBoundingClientRect(),hostCx=(hostRect.left+hostRect.right)/2,hostCy=(hostRect.top+hostRect.bottom)/2,cardCx=(cardRect.left+cardRect.right)/2,cardCy=(cardRect.top+cardRect.bottom)/2;
    check(Math.abs(hostCx-cardCx)<40&&Math.abs(hostCy-cardCy)<40,"Tour defaults to page centre");
    const overlay=document.querySelector(".tour-overlay"),overlayStyle=getComputedStyle(overlay);check(overlay.dataset.focus==="none"&&overlayStyle.backgroundColor!=="rgba(0, 0, 0, 0)","Tour non-target step dims background");
    check(document.elementFromPoint(Math.min(innerWidth-1,Math.max(1,cardRect.left+20)),Math.min(innerHeight-1,Math.max(1,cardRect.top+20)))?.closest("#tourCard"),"Tour card receives pointer input");
    document.querySelector("#tourNext")?.click();await waitFor(()=>document.querySelector('#tourCard[data-positioned="true"]')?.querySelector("h2")?.textContent==="Navigation","Tour next step positioned");
    const spotlight=check(document.querySelector("#tourSpotlight:not([hidden])"),"Tour target spotlight visible"),spotStyle=getComputedStyle(spotlight),targetOverlay=document.querySelector(".tour-overlay"),tourTarget=document.querySelector(".tour-target"),sidebarRect=document.querySelector(".sidebar").getBoundingClientRect(),spotRect=spotlight.getBoundingClientRect();check(targetOverlay.dataset.focus==="target","Tour target step uses spotlight focus mode");check(spotStyle.boxShadow.includes("9999px"),"Tour target has proven surrounding focus shade");check(Number.parseInt(getComputedStyle(targetOverlay).zIndex||"0",10)>=2000&&getComputedStyle(tourTarget).zIndex==="auto","Tour spotlight owns top stacking layer");check(Math.abs(sidebarRect.left-spotRect.left)<3&&Math.abs(sidebarRect.top-spotRect.top)<3&&Math.abs(sidebarRect.width-spotRect.width)<3&&Math.abs(sidebarRect.height-spotRect.height)<3,"Tour Navigation outline hugs sidebar bounds");
    document.querySelector("#tourSkip")?.click();await waitFor(()=>!document.documentElement.dataset.productTour,"Tour cleanup");check(!document.querySelector(".tour-target"),"Tour target cleanup");check(!document.querySelector(".tour-overlay"),"Tour overlay removed");

    const launcher=check(document.querySelector("#controlChatLauncher"),"Chat launcher");launcher.click();await waitFor(()=>document.querySelector("#a31ControlChatForm"),"Assistant chat");
    const panel=check(document.querySelector("#controlChatPanel"),"Chat panel"),chatToggle=check(document.querySelector("#controlChatToggle"),"Chat header toggle"),chatPosBefore=a33ControlChatPosition();
    await gesture(chatToggle,0,-90);check(a33ControlChatPosition()!==chatPosBefore,"Chat moves vertically");check(a33ControlChatOpen(),"Chat drag keeps panel open");
    const form=document.querySelector("#a31ControlChatForm");form.querySelector("textarea").value="hello";form.requestSubmit(form.querySelector('button:not([name="run"])'));
    await waitFor(()=>document.querySelector("#controlChatBody")?.textContent?.includes("No eligible reasoning model is configured."),"no-model Assistant response",30000);
    const expandedTransform=getComputedStyle(panel.querySelector(".control-chat-chevron")).transform;chatToggle.click();check(panel.dataset.collapsed==="true","Chat collapses from header");await sleep(220);const collapsedTransform=getComputedStyle(panel.querySelector(".control-chat-chevron")).transform;check(expandedTransform!=="none"&&collapsedTransform==="none","Chat chevron direction matches collapse state");chatToggle.click();check(panel.dataset.collapsed==="false","Chat expands from header");await sleep(220);
    launcher.click();check(!a33ControlChatOpen(),"Chat launcher closes open chat");launcher.click();await waitFor(()=>a33ControlChatOpen(),"Chat launcher reopens closed chat");a31CloseControlChat();

    await route("projects");const projectDelete=await waitFor(()=>document.querySelector("#a33DeleteProject"),"Delete project action",10000);projectDelete.click();const confirmProjectDelete=await waitFor(()=>document.querySelector("#a33ConfirmDeleteProject"),"Delete project confirmation");confirmProjectDelete.click();await waitFor(()=>project.status==="archived"&&!qa4ProjectHub.projects.some(x=>x.id==="project-release"),"Project lifecycle delete persisted",30000);check(document.querySelector(".empty-state")?.textContent?.includes("No projects yet"),"Deleted project leaves active Projects list");

    results.push("installed behavioural acceptance complete");post("PASS");
  }

  window.onepaneReleaseSmoke=()=>run().catch(ex=>{results.push("FAIL: "+String(ex?.message||ex));post("FAIL")});
})();