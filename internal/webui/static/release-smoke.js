(()=>{
  if(new URLSearchParams(location.search).get("onepane_release_smoke")!=="1")return;

  const results=[],sleep=ms=>new Promise(r=>setTimeout(r,ms));
  const check=(v,label)=>{if(!v)throw new Error(label);results.push(label);return v};
  const waitFor=async(fn,label,timeout=20000)=>{const end=Date.now()+timeout;while(Date.now()<end){try{const v=await fn();if(v)return v}catch{}await sleep(75)}throw new Error("Timed out: "+label)};
  const clone=v=>JSON.parse(JSON.stringify(v));
  const json=(body,status=200)=>new Response(JSON.stringify(body),{status,headers:{"content-type":"application/json"}});
  const workspace={id:"pws-release",name:"Main workspace",widgets:[
    {id:"pw-release-follow",type:"follow",title:"Follow",col:4,row:5},
    {id:"pw-release-activity",type:"activity",title:"Activity",col:4,row:4},
    {id:"pw-release-logs",type:"logs",title:"Logs",col:4,row:5},
    {id:"pw-release-agents",type:"agents",title:"Agents",col:4,row:4},
    {id:"pw-release-terminal",type:"terminal",title:"Terminal",col:4,row:5},
    {id:"pw-release-verification",type:"verification",title:"Verification",col:4,row:4},
    {id:"pw-release-checkpoints",type:"checkpoints",title:"Checkpoints",col:4,row:4},
    {id:"pw-release-chat",type:"chat",title:"Workspace chat",col:4,row:5},
    {id:"pw-release-tasks",type:"tasks",title:"Tasks",col:6,row:4},
    {id:"pw-release-scheduled",type:"scheduled",title:"Scheduled tasks",col:6,row:4},
    {id:"pw-release-models",type:"models",title:"Model routing",col:4,row:5},
    {id:"pw-release-modelstack",type:"modelstack",title:"Legacy Model Stack",col:4,row:5},
    {id:"pw-release-nodes",type:"nodes",title:"Nodes",col:4,row:4},
    {id:"pw-release-attention",type:"attention",title:"Attention",col:4,row:4},
    {id:"pw-release-notes",type:"notes",title:"Notes",col:6,row:4},
    {id:"pw-release-settings",type:"settings",title:"Workspace settings",col:6,row:5}
  ],inspector:{tabs:["follow","notes"],tiles:[],tab_config:{}},orchestration:{mode:"direct",supervisor:{model:"auto",agent:"onepane-default"},team:{model:"auto",agent:"onepane-default",count:2},council:{model:"auto",agent:"onepane-default",count:2}}};
  const workspace2={id:"pws-release-2",name:"Disposable workspace",widgets:[{id:"pw-release-2-notes",type:"notes",title:"Notes",col:6,row:4}],orchestration:{mode:"direct",supervisor:{model:"auto",agent:"onepane-default"},team:{model:"auto",agent:"onepane-default",count:2},council:{model:"auto",agent:"onepane-default",count:2}}};
  let project={id:"project-release",workspace_id:"workspace-release",name:"Release QA Project",description:"Installed behavioural acceptance",status:"active",revision:1,project_policy:{onepane_ui:{workspaces:[workspace,workspace2]}}};
  let taskRows=[{id:"task-release",workspace_id:"workspace-release",project_id:"project-release",project_workspace_id:"pws-release",objective:"Release task",state:"complete",scheduling_class:"user_interactive",priority:0,revision:1,created_at:1700000000000,updated_at:1700000000000}],archivedTaskRows=[],routineRows=[],lastRoutinePayload=null;
  let projectPatchCount=0,turns=[],discoverInstalledFixture=false,assistantModelChoice=null;
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
    if(path==="/v1/projects/project-release/workspaces"&&method==="GET")return json([{id:"pws-release",project_id:"project-release",name:"Main workspace",status:"active"},{id:"pws-release-2",project_id:"project-release",name:"Disposable workspace",status:"active"}]);
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
    if(path==="/v1/assistant/threads"&&method==="GET")return json([{id:"thread-release",title:"Release Assistant",preferred_model_deployment_id:assistantModelChoice}]);
    if(path==="/v1/assistant/threads/thread-release/model"&&method==="PUT"){
      const selection=JSON.parse(opts.body||"{}");assistantModelChoice=selection.deployment_id||null;
      return json({id:"thread-release",preferred_model_deployment_id:assistantModelChoice});
    }
    if(path==="/v1/scheduler/candidates")return json([{id:"dep-test-gpu",kind:"model_deployment",display_name:"Test GPU Model",provider:"local",local:true,cost_class:"local",schedulable:true,status:"ready",qualification:"verified"}]);
    if(path==="/v1/assistant/threads/thread-release/turns"&&method==="GET")return json(clone(turns));
    if(path==="/v1/assistant/threads/thread-release/turns"&&method==="POST"){
      const body=JSON.parse(opts.body||"{}");turns.push({role:"user",content:body.content||""},{role:"assistant",content:"No eligible reasoning model is configured. Configure a model to run reasoning."});return json(turns.at(-1),201);
    }
    if(path==="/v1/tasks"&&method==="GET")return json(clone(u.searchParams.get("archived")==="1"?archivedTaskRows:taskRows));
    if(path==="/v1/tasks"&&method==="POST"){
      const body=JSON.parse(opts.body||"{}"),task={id:"task-created-"+(taskRows.length+archivedTaskRows.length+1),...body,state:"created",revision:1,created_at:Date.now(),updated_at:Date.now()};taskRows.unshift(task);return json(clone(task),201);
    }
    if(path==="/v1/tasks/task-release/archive"&&method==="POST"){
      const body=JSON.parse(opts.body||"{}"),i=taskRows.findIndex(x=>x.id==="task-release");if(i<0)return json({error:"not found"},404);if(Number(body.expected_revision)!==Number(taskRows[i].revision))return json({error:"revision conflict"},409);const t={...taskRows.splice(i,1)[0],archived_at:Date.now(),revision:Number(body.expected_revision)+1,updated_at:Date.now()};archivedTaskRows.unshift(t);return json(clone(t));
    }
    if(path==="/v1/tasks/task-release/unarchive"&&method==="POST"){
      const body=JSON.parse(opts.body||"{}"),i=archivedTaskRows.findIndex(x=>x.id==="task-release");if(i<0)return json({error:"not found"},404);if(Number(body.expected_revision)!==Number(archivedTaskRows[i].revision))return json({error:"revision conflict"},409);const t={...archivedTaskRows.splice(i,1)[0],archived_at:null,revision:Number(body.expected_revision)+1,updated_at:Date.now()};taskRows.unshift(t);return json(clone(t));
    }
    if(path==="/v1/routines"&&method==="GET")return json(clone(routineRows));
    if(path==="/v1/routines"&&method==="POST"){
      const body=JSON.parse(opts.body||"{}");lastRoutinePayload=clone(body);const row={id:"routine-release-"+(routineRows.length+1),name:body.name,timezone:body.timezone,status:"active",trigger_json:body.trigger,policy_json:body.policy,updated_at:Date.now()};routineRows.unshift(row);return json(clone(row),201);
    }
    if(path==="/v1/provider-presets")return json([{id:"openai_chatgpt_plan",display_name:"ChatGPT plan",description:"OAuth subscription fallback",auth_type:"oauth2-pkce",credential_provider:"openai-chatgpt-plan",cost_hint:"included_subscription"},{id:"openai",display_name:"OpenAI API",description:"Direct API",auth_type:"bearer",credential_provider:"openai",cost_hint:"paid_or_provider_managed"}]);
    if(path==="/v1/provider-oauth/connections")return json([]);
    if(path==="/v1/local-ai/discovery")return json({models:[],source_errors:{}});
    if(path==="/v1/local-ai/llama-runtimes")return json([]);
    if(["/v1/providers","/v1/events","/v1/agent-runtime-presets","/v1/scheduler/candidates","/v1/skills/packages","/v1/skills/assignments","/v1/agent-profiles","/v1/teams","/v1/team-presets","/v1/provider-oauth/configs"].includes(path))return json([]);
    if(path==="/v1/local-ai/catalog")return json(discoverInstalledFixture?[{model_ref:"google/gemma-3-1b-it",display_name:"Gemma 3 1B IT",installable:true,installable_quantizations:["Q4_K_M"],context_length:8192}]:[]);
    if(path==="/v1/local-ai/deployments")return json({deployments:discoverInstalledFixture?[{deployment_id:"dep-gemma-qa",model_ref:"google/gemma-3-1b-it",display_name:"Gemma 3 1B IT",status:"ready",runtime_name:"llamacpp"}]:[]});
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

  // Every desktop route must use only the shell's remaining height when
  // the Logs drawer expands. Exercise primary pages and meaningful subviews
  // against live layout geometry (not source-string assertions).
  async function testAllDrawerAwarePages(){
    if(innerWidth<=700)return; // mobile Logs is an overlay
    const main=check(document.querySelector(".main-shell"),"Drawer layout shell");
    const previous={drawer:state.drawer,height:state.drawerHeight,transition:main.style.transition,
      operations:a31OperationsView,models:a31ModelView,agents:a31AgentView,
      skills:a31SkillsView,settings:a31SettingsView};
    const root=document.documentElement,host=check(document.querySelector("#viewHost"),"Drawer page host");
    const frame=async(height)=>{
      state.drawerHeight=height;root.style.setProperty("--drawer",height+"px");
      await new Promise(ok=>requestAnimationFrame(()=>requestAnimationFrame(ok)));
      return host.getBoundingClientRect();
    };
    const verify=async(label)=>{
      const small=await frame(145),page=check(host.querySelector(":scope > .page"),label+" page container");
      const large=await frame(355),rect=host.getBoundingClientRect(),bounds=page.getBoundingClientRect();
      check(small.height-large.height>175,label+" shrinks with expanded Logs");
      check(bounds.top>=rect.top-3&&bounds.bottom<=rect.bottom+3,label+" remains within the shell viewport");
      const scroller=page.querySelector(".a36-settings-content")||page.querySelector("#nextNodeRoot")||
        page.querySelector(":scope > :is(#a31OperationsBody,#tasksBody,.project-hub,#a31AgentsBody,#a31SkillsBody,.settings-shell,#secretCatalogue,#a31ModelsRoot)");
      const overflow=getComputedStyle(scroller||page).overflowY;
      check(overflow==="auto"||overflow==="scroll",label+" has a reachable vertical scroller");
    };
    try{
      main.style.transition="none";
      state.drawerHeight=145;setDrawerOpen(true);
      for(const routeName of ["operations","tasks","projects","models","nodes","agents","skills","settings","secrets"]){
        await route(routeName);
        await verify(routeName);
        const modes={
          operations:["overview","activity","health","recovery"],
          models:["local","cloud","routing","discover"],
          agents:["profiles","teams","sessions","councils"],
          skills:["installed","catalogue","assignments","matrix","packages","bundles"],
          settings:["general","appearance","defaults","models","providers","nodes","agents","skills","security","updates"]
        }[routeName]||[];
        for(const mode of modes){
          if(routeName==="operations"){a31OperationsView=mode;await renderOperations()}
          if(routeName==="models"){a31ModelView=mode;await renderModels()}
          if(routeName==="agents"){a31AgentView=mode;await renderAgents()}
          if(routeName==="skills"){a31SkillsView=mode;await renderSkills()}
          if(routeName==="settings"){a31SettingsView=mode;await renderSettings()}
          await verify(routeName+" / "+mode);
        }
      }
      // Workspaces are a distinct nested route reached from the Project tile.
      await route("projects");
      const open=check(document.querySelector('[data-a35-open-project="project-release"]'),"Workspace route entry for Logs audit");
      open.click();
      await waitFor(()=>currentTab()?.route==="workspaces"&&document.querySelector("#qa4WorkspaceGrid"),"Workspaces Logs audit entry");
      await verify("workspaces");
      results.push("All primary desktop pages and subviews resize with Logs");
    } finally {
      a31OperationsView=previous.operations;a31ModelView=previous.models;
      a31AgentView=previous.agents;a31SkillsView=previous.skills;
      a31SettingsView=previous.settings;
      state.drawerHeight=previous.height;
      setDrawerOpen(previous.drawer==="open");
      root.style.setProperty("--drawer",previous.drawer==="open"?previous.height+"px":"0px");
      main.style.transition=previous.transition;
    }
  }

  async function testProjectLayout(){
    await route("projects");
    check(document.querySelector("#a35ProjectGrid"),"Projects overview tile grid");
    check(document.querySelector('[data-a35-project-card="project-release"]'),"Project management tile");
    check(!document.querySelector(".project-rail"),"Workspace page no longer duplicates Project list");
    check(document.querySelector('[data-a35-delete-project="project-release"]'),"Project delete exists only on Project tile");

    const openProject=check(document.querySelector('.project-overview-actions [data-a35-open-project="project-release"]'),"Open Project workspace action");
    openProject.click();
    await waitFor(()=>currentTab()?.route==="workspaces"&&document.querySelector("#qa4WorkspaceGrid"),"Project opens dedicated Workspace route",30000);
    check(!document.querySelector('[data-a35-delete-project],#a33DeleteProject'),"Workspace surface has no Project delete action");
    check(!document.querySelector(".project-rail"),"Workspace surface has no duplicate Project rail");

    const inspectorTypes=a35InspectorComponentTypes(),fixtureWidgets=workspace.widgets;
    check(inspectorTypes.every(type=>fixtureWidgets.some(w=>a35InspectorType(w.type)===type)),"Inspector smoke covers every registered Workspace widget");
    check(fixtureWidgets.some(w=>w.type==="modelstack"),"Inspector smoke includes legacy Model Stack alias");
    for(const widget of fixtureWidgets){
      const target=a35InspectorType(widget.type),button=check(document.querySelector(`[data-pw-widget="${widget.id}"] [data-qa6-inspector="${widget.id}"]`),`Send to Inspector button: ${widget.type}`);
      check(button.dataset.qa6InspectorTarget===target,`Inspector button target registered: ${widget.type} -> ${target}`);
      button.click();
      await waitFor(()=>qa4Inspector.kind==="workspace"&&qa4InspectorTab===target&&document.querySelector(`[data-qa6-inspector-tab="${target}"].active`),`Inspector opens exact component tab: ${widget.type} -> ${target}`,30000);
      check(qa4InspectorTab!=="overview",`Inspector does not fall back to Overview: ${widget.type}`);
    }

    let ws=check(a31CurrentWorkspace(),"workspace active for native layout"),committedBefore=clone(ws.widgets);
    const edit=check(document.querySelector("#qa4EditWorkspace"),"workspace edit control");edit.click();
    await waitFor(()=>a35WorkspaceEditing()&&document.querySelector('[data-pw-resize][data-resize-edge="e"]'),"workspace native resize handle");
    check(document.querySelector(".dashboard-edit-bar[data-pw-drag]"),"Workspace edit header is drag surface");
    check(document.querySelectorAll("[data-pw-resize]").length>=48,"Workspace exposes edge and corner resize handles for multiple components");

    let draft=a35WorkspaceItems(a31CurrentProject(),a31CurrentWorkspace()),follow=check(draft.find(w=>w.id==="pw-release-follow"),"workspace draft resize state"),beforeW=Number(follow.width);
    let root=check(document.querySelector("#qa4WorkspaceGrid"),"workspace layout root"),handle=check(document.querySelector('[data-pw-widget="pw-release-follow"] [data-pw-resize][data-resize-edge="e"]'),"workspace east resize handle");
    const rs=getComputedStyle(root),gap=parseFloat(rs.columnGap)||0,colW=(root.getBoundingClientRect().width-gap*(A31_LAYOUT_COLUMNS-1))/A31_LAYOUT_COLUMNS,patchesBeforeDraft=projectPatchCount;
    await gesture(handle,colW+gap+3,0);
    await waitFor(()=>Number(a35WorkspaceItems(a31CurrentProject(),a31CurrentWorkspace()).find(w=>w.id==="pw-release-follow")?.width)>beforeW,"workspace pointer resize updates draft");
    check(projectPatchCount===patchesBeforeDraft,"Workspace draft resize does not persist before Done");
    check(JSON.stringify(a31CurrentWorkspace().widgets)===JSON.stringify(committedBefore),"Workspace draft does not mutate committed geometry");
    check(noOverlap(a35WorkspaceItems(a31CurrentProject(),a31CurrentWorkspace())),"Workspace draft resize resolves overlap");

    await route("operations");
    check(!a35WorkspaceEditing(),"Workspace navigation cancels edit mode");
    check(JSON.stringify(a31CurrentWorkspace().widgets)===JSON.stringify(committedBefore),"Workspace navigation discards draft geometry");
    const nestedWorkspace=await waitFor(()=>document.querySelector('[data-a31-project-nav="project-release"][data-a31-workspace-nav="pws-release"]'),"nested Workspace navigation");
    nestedWorkspace.click();
    await waitFor(()=>currentTab()?.route==="workspaces"&&document.querySelector("#qa4WorkspaceGrid"),"nested Workspace opens dedicated route",30000);
    check(document.querySelector("#qa4EditWorkspace")?.textContent==="Edit layout","Workspace returns outside edit mode");

    ws=check(a31CurrentWorkspace(),"workspace before committed layout edit");const settingsBefore=clone(ws.widgets.find(w=>w.id==="pw-release-settings")),patchesBeforeCommit=projectPatchCount;
    check(document.querySelector("#qa4EditWorkspace"),"workspace second edit control").click();
    await waitFor(()=>a35WorkspaceEditing()&&document.querySelector('[data-pw-widget="pw-release-settings"] [data-pw-drag]'),"workspace second edit session");
    root=check(document.querySelector("#qa4WorkspaceGrid"),"workspace root for committed drag");
    const drag=check(root.querySelector('[data-pw-widget="pw-release-settings"] .dashboard-edit-bar[data-pw-drag]'),"workspace native drag surface"),rowStep=A31_LAYOUT_ROW_PX+(parseFloat(getComputedStyle(root).rowGap)||0);
    await gesture(drag,0,rowStep*2+3);
    await waitFor(()=>Number(a35WorkspaceItems(a31CurrentProject(),a31CurrentWorkspace()).find(w=>w.id==="pw-release-settings")?.y)>Number(settingsBefore.y),"Workspace pointer drag changes draft geometry");
    check(projectPatchCount===patchesBeforeCommit,"Workspace drag remains provisional before Done");
    check(noOverlap(a35WorkspaceItems(a31CurrentProject(),a31CurrentWorkspace())),"Workspace pointer drag resolves overlap");
    check(document.querySelector("#qa4EditWorkspace"),"Workspace Done control").click();
    await waitFor(()=>projectPatchCount>patchesBeforeCommit&&!a35WorkspaceEditing(),"Workspace Done persists layout",30000);
    ws=check(a31CurrentWorkspace(),"workspace after Done");const expectedSettings=clone(ws.widgets.find(w=>w.id==="pw-release-settings"));
    check(Number(expectedSettings.y)>Number(settingsBefore.y),"Workspace committed drag changes geometry");

    await route("operations");
    const nestedReload=await waitFor(()=>document.querySelector('[data-a31-project-nav="project-release"][data-a31-workspace-nav="pws-release"]'),"nested Workspace reload navigation");nestedReload.click();
    await waitFor(()=>currentTab()?.route==="workspaces"&&document.querySelector("#qa4WorkspaceGrid"),"workspace reload");
    ws=check(a31CurrentWorkspace(),"workspace persisted reload");
    check(Number(ws.widgets.find(w=>w.id==="pw-release-settings")?.y)===Number(expectedSettings.y),"Workspace dragged geometry survives route reload");
    check(noOverlap(ws.widgets),"Workspace persisted geometry remains collision free");
    const preservedInspectorTabs=qa6InspectorConfig(ws).tabs;
    check(a35InspectorComponentTypes().every(type=>preservedInspectorTabs.includes(type)),"Workspace layout save preserves Inspector component tabs");

    const settingsButton=check(document.querySelector("#qa4WorkspaceSettings"),"Workspace settings action");settingsButton.click();
    await waitFor(()=>document.querySelector('.inspector-tab-draggable [data-qa6-inspector-tab="follow"]'),"draggable Inspector tabs",30000);
    check(!document.querySelector("[data-qa6-tab-move]")&&!document.querySelector("[data-qa6-tab-remove]"),"Inspector tab movement arrows remain removed");
    const followTab=check(document.querySelector('.inspector-tab-draggable [data-qa6-inspector-tab="follow"]')?.closest(".inspector-tab-draggable"),"Inspector Follow drag target"),closeTab=check(followTab.querySelector("[data-a35-inspector-close]"),"Inspector tab compact close control");
    closeTab.click();await waitFor(()=>document.querySelector("#a35CancelInspectorRemove"),"Inspector close confirmation");document.querySelector("#a35CancelInspectorRemove").click();
    check(!document.querySelector("#a35ConfirmInspectorRemove"),"Inspector close can be cancelled");
    const patchesBeforeInspectorDrag=projectPatchCount,tabBefore=clone(qa6InspectorConfig(a31CurrentWorkspace()).tabs),tabWidth=followTab.getBoundingClientRect().width;
    await gesture(followTab,tabWidth+12,0);
    await waitFor(()=>projectPatchCount>patchesBeforeInspectorDrag&&qa6InspectorConfig(a31CurrentWorkspace()).tabs[0]!==tabBefore[0],"Inspector tab drag persists order",30000);
    check(document.querySelectorAll(".qa4-inspector-tab-tools").length===qa6InspectorConfig(a31CurrentWorkspace()).tabs.length,"Inspector draggable tabs expose one compact close control each");

    const attentionWrap=check(document.querySelector('.inspector-tab-draggable [data-qa6-inspector-tab="attention"]')?.closest(".inspector-tab-draggable"),"Inspector Attention removable tab"),attentionClose=check(attentionWrap.querySelector("[data-a35-inspector-close]"),"Inspector Attention close control"),patchesBeforeClose=projectPatchCount;
    attentionClose.click();const confirmAttentionClose=await waitFor(()=>document.querySelector("#a35ConfirmInspectorRemove"),"Inspector Attention close confirmation");confirmAttentionClose.click();
    await waitFor(()=>projectPatchCount>patchesBeforeClose&&!qa6InspectorConfig(a31CurrentWorkspace()).tabs.includes("attention")&&!document.querySelector('[data-qa6-inspector-tab="attention"]'),"Inspector confirmed close persists without stale rerender",30000);
    check(!document.querySelector("[data-qa6-tab-move]")&&!document.querySelector("[data-qa6-tab-remove]"),"Legacy Inspector arrow controls do not reappear after close");
    const attentionWidgetButton=check(document.querySelector('[data-pw-widget="pw-release-attention"] [data-qa6-inspector="pw-release-attention"]'),"Attention widget Send to Inspector after close");attentionWidgetButton.click();
    await waitFor(()=>qa4InspectorTab==="attention"&&document.querySelector('[data-qa6-inspector-tab="attention"].active'),"Closed Inspector tab reopens in current implementation",30000);
    const reopenedAttention=check(document.querySelector('[data-qa6-inspector-tab="attention"]')?.closest(".inspector-tab-draggable"),"Reopened Attention draggable tab");
    check(!!reopenedAttention.querySelector("[data-a35-inspector-close]")&&!reopenedAttention.querySelector("[data-qa6-tab-move],[data-qa6-tab-remove]"),"Reopened Inspector tab uses only compact close chrome");

    const disposable=check(document.querySelector('[data-a35-workspace="pws-release-2"]'),"second workspace available for deletion");disposable.click();
    await waitFor(()=>a31CurrentWorkspace()?.id==="pws-release-2"&&document.querySelector("#a32DeleteWorkspace"),"second workspace rendered with delete action");
    const deleteButton=check(document.querySelector("#a32DeleteWorkspace"),"workspace delete action");check(!deleteButton.disabled,"workspace delete enabled when alternatives exist");const patchBeforeDelete=projectPatchCount;deleteButton.click();
    const confirmDelete=await waitFor(()=>document.querySelector("#a35ConfirmDeleteWorkspace"),"workspace delete confirmation");confirmDelete.click();
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
    const newTaskButton=check(document.querySelector("#newTaskButton"),"New task control");newTaskButton.click();
    const taskForm=await waitFor(()=>document.querySelector("#newTaskForm"),"New task modal");
    const taskBackdrop=check(document.querySelector(".modal-backdrop"),"Task modal backdrop"),inspectorResizer=check(document.querySelector("#inspectorResizer"),"Inspector resizer behind modal");
    check(Number.parseInt(getComputedStyle(taskBackdrop).zIndex||"0",10)>Number.parseInt(getComputedStyle(inspectorResizer).zIndex||"0",10),"Task modal is above Inspector controls");
    const taskProject=check(document.querySelector("#newTaskProject"),"New task Project selector"),taskWorkspace=check(document.querySelector("#newTaskWorkspace"),"New task Workspace selector");
    check(taskProject.tagName==="SELECT"&&taskProject.querySelector('option[value="project-release"]'),"New task Project uses live dropdown");
    taskProject.value="project-release";taskProject.dispatchEvent(new Event("change",{bubbles:true}));
    await waitFor(()=>!taskWorkspace.disabled&&taskWorkspace.querySelector('option[value="pws-release"]'),"New task Workspace scoped options");closeModal();

    const scheduledButton=check(document.querySelector("#newScheduledTask"),"New scheduled task control");scheduledButton.click();
    const routineForm=await waitFor(()=>document.querySelector("#qa4RoutineForm"),"Scheduled task modal");
    const routineProject=check(document.querySelector("#qa4RoutineProject"),"Scheduled Project selector"),routineWorkspace=check(document.querySelector("#qa4RoutineWorkspace"),"Scheduled Workspace selector");
    routineProject.value="project-release";routineProject.dispatchEvent(new Event("change",{bubbles:true}));
    await waitFor(()=>!routineWorkspace.disabled&&routineWorkspace.querySelector('option[value="pws-release"]'),"Scheduled Workspace scoped options");routineWorkspace.value="pws-release";
    const endMode=check(document.querySelector("#qa4RoutineEndMode"),"Scheduled end condition");endMode.value="count";endMode.dispatchEvent(new Event("change",{bubbles:true}));
    check(!document.querySelector("#qa4RoutineEndCount").classList.contains("hidden"),"After N runs control is exposed");
    routineForm.elements.name.value="Finite release task";routineForm.elements.objective.value="Run exactly three times";routineForm.elements.end_count.value="3";
    routineForm.dispatchEvent(new Event("submit",{bubbles:true,cancelable:true}));
    await waitFor(()=>lastRoutinePayload&&lastRoutinePayload.name==="Finite release task","bounded scheduled task submitted");
    check(Number(lastRoutinePayload.policy?.max_occurrences)===3,"Scheduled task preserves maximum run count");
    check(lastRoutinePayload.policy?.project_id==="project-release"&&lastRoutinePayload.policy?.project_workspace_id==="pws-release","Scheduled task preserves Project and Workspace scope");
    await waitFor(()=>!document.querySelector("#qa4RoutineForm"),"Scheduled modal closes after create");

    const archiveButton=check(document.querySelector('[data-task-archive="task-release"][data-task-restore="0"]'),"Task Archive action");archiveButton.click();
    await waitFor(()=>taskRows.length===0&&archivedTaskRows.some(t=>t.id==="task-release"),"Task archived without deletion");
    await waitFor(()=>!document.querySelector('[data-task-archive="task-release"][data-task-restore="0"]')&&document.querySelector('[data-task-tab="archived"]'),"Task archive render settled");
    const archivedTab=check(document.querySelector('[data-task-tab="archived"]'),"Archived Tasks tab");archivedTab.click();
    const restoreButton=await waitFor(()=>document.querySelector('[data-task-archive="task-release"][data-task-restore="1"]'),"Task Restore action");restoreButton.click();
    await waitFor(()=>taskRows.some(t=>t.id==="task-release")&&!archivedTaskRows.some(t=>t.id==="task-release"),"Archived task restored");
    await waitFor(()=>document.querySelector('[data-task-archive="task-release"][data-task-restore="0"]')&&!document.querySelector('[data-task-archive="task-release"][data-task-restore="1"]'),"Task restore render settled");
    discoverInstalledFixture=true;
    await route("models");check(document.querySelector("#a31ModelsRoot")&&!document.querySelector("#a31ModelsRoot .error"),"Models route");check(document.querySelectorAll("[data-a31-model-tab]").length===4,"Models exposes Local Cloud Routing Discover tabs");check(document.querySelector('#primaryNav [data-route="secrets"]'),"Secrets is primary navigation");a31SetModelView("discover");await waitFor(()=>document.querySelector("#a31DiscoverCatalog"),"Discover model catalogue");check(document.querySelector("#a31DiscoverSource"),"Discover source selector");await waitFor(()=>document.querySelector('[data-a31-discover-install-ref="google/gemma-3-1b-it"]'),"Installed Gemma Discover action");
    const installedDiscoverButton=check(document.querySelector('[data-a31-discover-install-ref="google/gemma-3-1b-it"]'),"Installed model discover button");
    check(installedDiscoverButton.disabled&&installedDiscoverButton.textContent.trim()==="Installed","Already-installed catalogue model cannot be installed again");
    discoverInstalledFixture=false;a31SetModelView("cloud");await waitFor(()=>document.querySelector('[data-cloud-provider-row][data-auth="oauth"]'),"Cloud OAuth provider is visible");a31SetModelView("local");await waitFor(()=>document.querySelector(".hardware-card"),"Local Models hardware surface");check(document.querySelector("#a31-llamacpp-status"),"llama.cpp managed runtime card");
    await route("nodes");
    const nodeRoot=document.querySelector("#a31Nodes"),nodeError=nodeRoot?.querySelector(".error")?.textContent||"";
    check(nodeRoot&&!nodeError,"Nodes envelope"+(nodeError?": "+nodeError:""));
    document.querySelector("#a31AddNode")?.click();await waitFor(()=>document.querySelector("#pairNodeForm"),"pairing modal");check(document.querySelector("#pairNodeForm"),"Add Node pairing flow");closeModal();
    await route("nodes");await waitFor(()=>document.querySelector('[data-a31-node="node-release"]'),"Node card");check(document.querySelector('[data-a31-node="node-release"] .card-title')?.textContent==="RELEASE-PC (Local)","Nodes prefer machine name and mark local device");check(document.querySelector('[data-a31-node="node-release"] .list-meta')?.textContent?.includes("node-release"),"Node ID remains secondary metadata");
    // Nodes must track the available shell viewport as the Logs drawer
    // expands, rather than extending behind the drawer.
    const nodeScrollPane=check(document.querySelector(".nodes-page #nextNodeRoot"),"Nodes own scrolling viewport");
    check(getComputedStyle(nodeScrollPane).overflowY==="auto","Nodes scroll content within the page");
    const drawerInitiallyOpen=state.drawer==="open",initialDrawerHeight=state.drawerHeight;
    setDrawerOpen(true);
    state.drawerHeight=150;document.documentElement.style.setProperty("--drawer","150px");
    await sleep(240);
    const nodesHostHeightBefore=document.querySelector("#viewHost").getBoundingClientRect().height;
    state.drawerHeight=360;document.documentElement.style.setProperty("--drawer","360px");
    await sleep(240);
    const nodesHost=document.querySelector("#viewHost").getBoundingClientRect();
    const nodeViewport=nodeScrollPane.getBoundingClientRect();
    check(nodesHostHeightBefore-nodesHost.height>150,"Nodes shell area shrinks when Logs expands");
    check(nodeViewport.bottom<=nodesHost.bottom+3,"Nodes stay above the expanded Logs drawer");
    state.drawerHeight=initialDrawerHeight;
    document.documentElement.style.setProperty("--drawer",initialDrawerHeight+"px");
    setDrawerOpen(drawerInitiallyOpen);
    await route("agents");check(!document.querySelector("#viewHost .error"),"Agents route");

    await route("skills");const bundles=check(document.querySelector('[data-a31-skills-tab="bundles"]'),"Tool Bundles tab");bundles.click();
    await waitFor(()=>document.querySelector("#a31SkillsBody")?.textContent?.includes("Tool Bundles"),"Tool Bundles view");check(!document.querySelector("#a31SkillsBody .error"),"Skills route");

    await route("settings");check(document.querySelector("#a31SettingsContent"),"Settings route");
    check(document.querySelector(".a36-settings-page .a36-settings-sidebar"),"Redesigned Settings navigation");
    check(document.querySelectorAll(".a36-settings-navigation [data-a31-settings]").length===11,"All settings categories and Overview retained");
    check(document.querySelector(".a36-settings-panel-heading"),"Settings content has contextual heading");
    const settingsSearch=check(document.querySelector("#a31SettingsSearch"),"Search settings control");
    settingsSearch.value="oauth";settingsSearch.dispatchEvent(new Event("input",{bubbles:true}));
    check(!document.querySelector('[data-a31-settings="providers"]').hidden,"Settings search finds provider configuration");
    check(document.querySelector('[data-a31-settings="appearance"]').hidden,"Settings search hides unrelated categories");
    settingsSearch.value="";settingsSearch.dispatchEvent(new Event("input",{bubbles:true}));
    check(!document.querySelector('[data-a31-settings="appearance"]').hidden,"Settings search restores all categories");
    document.querySelector('[data-a31-settings="general"]').click();
    await waitFor(()=>document.querySelector("#a36SaveGeneral"),"Settings startup save action");
    const savedLanding=qa5Prefs().landing||"operations";
    document.querySelector("#a31Landing").value="tasks";
    document.querySelector("#a36SaveGeneral").click();
    check(qa5Prefs().landing==="tasks","Settings saves startup destination");
    document.querySelector("#a31Landing").value=savedLanding;
    document.querySelector("#a36SaveGeneral").click();
    document.querySelector('[data-a31-settings="updates"]').click();
    await waitFor(()=>document.querySelector("#a36SaveUpdates"),"Settings release-channel save action");
    const savedChannel=qa5Prefs().update_channel||"alpha";
    document.querySelector("#a31UpdateChannel").value="stable";
    document.querySelector("#a36SaveUpdates").click();
    check(qa5Prefs().update_channel==="stable","Settings saves release channel");
    document.querySelector("#a31UpdateChannel").value=savedChannel;
    document.querySelector("#a36SaveUpdates").click();
    document.querySelector('[data-a31-settings="defaults"]').click();
    await waitFor(()=>document.querySelector("#a31SaveDefaults"),"Workspace defaults retained");
    check(document.querySelectorAll("#a31DefaultMode,#a31DefaultSeats,#a31DefaultRouting,#a31DefaultRemote,#a31DefaultBrowser,#a31DefaultComputer").length===6,"All Workspace defaults retained");
    document.querySelector('[data-a31-settings="models"]').click();
    await waitFor(()=>document.querySelector("#a31SaveModelSettings"),"Models configuration retained");
    check(document.querySelector("#a31ModelPool")&&document.querySelector("#a31ComputeDefault"),"Model storage and compute controls retained");
    document.querySelector('[data-a31-settings="agents"]').click();
    await waitFor(()=>document.querySelector("#a31SaveAgentSettings"),"Agent and research defaults retained");
    document.querySelector('[data-a31-settings="security"]').click();
    await waitFor(()=>document.querySelectorAll("[data-approval-default]").length===3,"All approval levels retained");
    document.querySelector('[data-a31-settings="providers"]').click();
    await waitFor(()=>document.querySelector("#a31AddOAuthConfig"),"Provider OAuth configuration retained");
    document.querySelector('[data-a31-settings="nodes"]').click();
    await waitFor(()=>document.querySelector('[data-route="nodes"]'),"Node management link retained");
    document.querySelector('[data-a31-settings="skills"]').click();
    await waitFor(()=>document.querySelector('[data-route="skills"]'),"Skills management link retained");
    document.querySelector('[data-a31-settings="appearance"]').click();
    await waitFor(()=>document.querySelector("[data-settings-theme]"),"Appearance settings");check(document.querySelector("[data-settings-theme]"),"Themes rendered");

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
    const assistantSelector=check(document.querySelector("#a31AssistantModel"),"Assistant model selector is visible");
    check(assistantSelector.value==="","Assistant defaults to automatic routing");
    assistantSelector.value="dep-test-gpu";
    assistantSelector.dispatchEvent(new Event("change",{bubbles:true}));
    await waitFor(()=>document.querySelector("#a31AssistantModel")?.value==="dep-test-gpu"&&assistantModelChoice==="dep-test-gpu","Assistant model selection persists");
    check(document.querySelector("#a31AssistantModel")?.options[document.querySelector("#a31AssistantModel")?.selectedIndex]?.textContent?.includes("Test GPU Model"),"Assistant selected model remains visible");
    document.querySelector("#a31AssistantModel").value="";
    document.querySelector("#a31AssistantModel").dispatchEvent(new Event("change",{bubbles:true}));
    await waitFor(()=>assistantModelChoice===null&&document.querySelector("#a31AssistantModel")?.value==="","Assistant Auto selection restores routing");
    const form=document.querySelector("#a31ControlChatForm");form.querySelector("textarea").value="hello";form.requestSubmit(form.querySelector('button:not([name="run"])'));
    await waitFor(()=>document.querySelector("#controlChatBody")?.textContent?.includes("No eligible reasoning model is configured."),"no-model Assistant response",30000);
    const expandedTransform=getComputedStyle(panel.querySelector(".control-chat-chevron")).transform;chatToggle.click();check(panel.dataset.collapsed==="true","Chat collapses from header");await sleep(220);const collapsedTransform=getComputedStyle(panel.querySelector(".control-chat-chevron")).transform;check(expandedTransform!=="none"&&collapsedTransform==="none","Chat chevron direction matches collapse state");chatToggle.click();check(panel.dataset.collapsed==="false","Chat expands from header");await sleep(220);
    launcher.click();check(!a33ControlChatOpen(),"Chat launcher closes open chat");launcher.click();await waitFor(()=>a33ControlChatOpen(),"Chat launcher reopens closed chat");a31CloseControlChat();

    await testAllDrawerAwarePages();
    await route("projects");const projectDelete=await waitFor(()=>document.querySelector('[data-a35-delete-project="project-release"]'),"Delete project action",10000);projectDelete.click();const confirmProjectDelete=await waitFor(()=>document.querySelector("#a33ConfirmDeleteProject"),"Delete project confirmation");confirmProjectDelete.click();await waitFor(()=>project.status==="archived"&&!qa4ProjectHub.projects.some(x=>x.id==="project-release"),"Project lifecycle delete persisted",30000);check(document.querySelector(".empty-state")?.textContent?.includes("No projects yet"),"Deleted project leaves active Projects list");

    results.push("installed behavioural acceptance complete");post("PASS");
  }

  window.onepaneReleaseSmoke=()=>run().catch(ex=>{results.push("FAIL: "+String(ex?.message||ex));post("FAIL")});
})();