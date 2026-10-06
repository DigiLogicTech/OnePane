(()=>{
  if(new URLSearchParams(location.search).get("onepane_release_smoke")!=="1")return;

  const results=[],sleep=ms=>new Promise(r=>setTimeout(r,ms));
  const check=(v,label)=>{if(!v)throw new Error(label);results.push(label);return v};
  const waitFor=async(fn,label,timeout=20000)=>{const end=Date.now()+timeout;while(Date.now()<end){try{const v=await fn();if(v)return v}catch{}await sleep(75)}throw new Error("Timed out: "+label)};
  const clone=v=>JSON.parse(JSON.stringify(v));
  const json=(body,status=200)=>new Response(JSON.stringify(body),{status,headers:{"content-type":"application/json"}});
  const workspace={id:"pws-release",name:"Main workspace",widgets:[{id:"pw-release-chat",type:"chat",title:"Workspace chat",width:6,height:5,x:0,y:0,col:6,row:5}],orchestration:{mode:"direct",supervisor:{model:"auto",agent:"onepane-default"},team:{model:"auto",agent:"onepane-default",count:2},council:{model:"auto",agent:"onepane-default",count:2}}};
  let project={id:"project-release",workspace_id:"workspace-release",name:"Release QA Project",description:"Installed behavioural acceptance",status:"active",revision:1,project_policy:{onepane_ui:{workspaces:[workspace]}}};
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
    if(path==="/v1/nodes")return json({nodes:[{id:"node-release",node_id:"node-release",display_name:"Release Node",status:"ready",architecture:"amd64",os_name:"Windows"}]});
    if(path==="/v1/projects"&&method==="GET")return json([clone(project)]);
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
  const route=async name=>{
    const target=document.querySelector(`#primaryNav [data-route="${name}"]`)||document.querySelector(`[data-route="${name}"]`);
    check(target,`nav:${name}`);target.click();
    await waitFor(()=>document.querySelector("#viewHost")?.dataset.renderedRoute===name,`render:${name}`,30000);
    check(currentTab()?.route===name,`active:${name}`);
  };

  async function testProjectLayout(){
    await route("projects");check(document.querySelector("#qa4WorkspaceGrid"),"project workspace rendered");
    const edit=check(document.querySelector("#qa4EditWorkspace"),"workspace edit control");if(!state.projectWorkspaceEdit)edit.click();
    await waitFor(()=>document.querySelector("[data-pw-preset]"),"workspace preset");
    for(const size of ["wide","full"]){
      const sel=check(document.querySelector("[data-pw-preset]"),`preset:${size}`);sel.value=size;sel.dispatchEvent(new Event("change",{bubbles:true}));
      await waitFor(()=>!document.querySelector("#qa4WorkspaceGrid")?.dataset.layoutSaving,`save:${size}`,30000);await sleep(100);
    }
    check(projectPatchCount>=2,"sequential project revisions accepted");
    await route("operations");await route("projects");
    const ws=check(a31CurrentWorkspace(),"workspace reload");check(Number(ws.widgets?.[0]?.width)===12,"latest layout persisted");
  }

  async function run(){
    localStorage.setItem(TOUR_KEY,TOUR_COMPLETE_VALUE);
    state.operationsWidgets=defaultState().operationsWidgets.map((x,i)=>({...x,x:i,y:0,width:2,height:3,col:2,row:3}));
    state.operationsLayoutVersion=2;
    persist();
    check(a31RepairPersistedUIState(),"broken persisted Operations layout repaired");
    check(!a31OperationsLayoutBroken(state.operationsWidgets),"repaired Operations layout is valid");
    await waitFor(()=>document.querySelector("#app")&&!document.querySelector("#app").classList.contains("hidden"),"application shell",30000);
    check(onepaneWorkspace==="workspace-release","mock workspace authenticated");
    renderInspector();
    check(document.querySelector("#inspector")?.dataset.tabMode==="single","Inspector Overview is implicit");
    check(getComputedStyle(document.querySelector("#inspector .inspector-tabs")).display==="none","single Inspector Overview rail is hidden");
    if(document.documentElement.dataset.productTour==="active"){
      document.querySelector("#tourSkip")?.click();
      await waitFor(()=>!document.documentElement.dataset.productTour,"welcome Tour cleanup");
      check(!document.querySelector(".tour-target"),"welcome Tour target cleanup");
    }

    await route("operations");check(document.querySelector("#operationsLayout"),"Operations overview");
    state.operationsWidgets=defaultState().operationsWidgets.map((x,i)=>({...x,x:i,y:0,width:3,col:3,height:3,row:3}));a31RenderOperationsGrid();
    check(!a31OperationsLayoutBroken(state.operationsWidgets),"Operations render repairs injected overlap");
    const ops=a31Array(state.operationsWidgets);for(let i=0;i<ops.length;i++)for(let j=i+1;j<ops.length;j++)check(!a31Overlap(ops[i],ops[j]),`Operations no overlap ${i}/${j}`);

    await route("tasks");check(!document.querySelector("#tasksBody .error"),"Tasks route");
    await route("models");check(document.querySelector("#a31ModelsRoot")&&!document.querySelector("#a31ModelsRoot .error"),"Models route");
    await route("nodes");check(document.querySelector("#a31Nodes")&&!document.querySelector("#a31Nodes .error"),"Nodes envelope");
    document.querySelector("#a31AddNode")?.click();await waitFor(()=>document.querySelector("#pairNodeForm"),"pairing modal");check(document.querySelector("#pairNodeForm"),"Add Node pairing flow");closeModal();
    await route("agents");check(!document.querySelector("#viewHost .error"),"Agents route");

    await route("skills");const bundles=check(document.querySelector('[data-a31-skills-tab="bundles"]'),"Tool Bundles tab");bundles.click();
    await waitFor(()=>document.querySelector("#a31SkillsBody")?.textContent?.includes("Tool Bundles"),"Tool Bundles view");check(!document.querySelector("#a31SkillsBody .error"),"Skills route");

    await route("settings");check(document.querySelector("#a31SettingsContent"),"Settings route");
    document.querySelector('[data-a31-settings="appearance"]')?.click();await waitFor(()=>a31SettingsView==="appearance","Appearance settings");check(document.querySelector("[data-settings-theme]"),"Themes rendered");

    await testProjectLayout();

    const command=check(document.querySelector('[data-action="command-palette"]'),"command launcher");command.click();await waitFor(()=>document.querySelector("#paletteInput"),"command palette");
    check(document.querySelectorAll("[data-palette-index]").length>0,"command actions populated");document.querySelector("#paletteInput").dispatchEvent(new KeyboardEvent("keydown",{key:"Escape",bubbles:true}));await waitFor(()=>!document.querySelector("#paletteInput"),"command close");

    const tour=check(document.querySelector('[data-action="product-tour"]'),"Tour launcher");tour.click();const tourCard=await waitFor(()=>document.querySelector('#tourCard[data-positioned="true"]'),"Tour positioned");
    const tourStyle=getComputedStyle(tourCard);check(tourStyle.visibility!=="hidden"&&tourStyle.display!=="none"&&Number(tourStyle.opacity||1)>0,"Tour card is visible");
    const hostRect=document.querySelector("#viewHost").getBoundingClientRect(),cardRect=tourCard.getBoundingClientRect(),hostCx=(hostRect.left+hostRect.right)/2,hostCy=(hostRect.top+hostRect.bottom)/2,cardCx=(cardRect.left+cardRect.right)/2,cardCy=(cardRect.top+cardRect.bottom)/2;
    check(Math.abs(hostCx-cardCx)<40&&Math.abs(hostCy-cardCy)<40,"Tour defaults to page centre");
    const pane=document.querySelector(".tour-pane"),paneStyle=getComputedStyle(pane);check(pane&&paneStyle.backgroundColor!=="rgba(0, 0, 0, 0)"&&paneStyle.backgroundColor!=="transparent","Tour focus mask dims background");
    check(document.elementFromPoint(Math.min(innerWidth-1,Math.max(1,cardRect.left+20)),Math.min(innerHeight-1,Math.max(1,cardRect.top+20)))?.closest("#tourCard"),"Tour card receives pointer input");
    document.querySelector("#tourNext")?.click();await waitFor(()=>document.querySelector('#tourCard[data-positioned="true"]')?.querySelector("h2")?.textContent==="Navigation","Tour next step positioned");
    document.querySelector("#tourSkip")?.click();await waitFor(()=>!document.documentElement.dataset.productTour,"Tour cleanup");check(!document.querySelector(".tour-target"),"Tour target cleanup");check(!document.querySelector(".tour-overlay"),"Tour overlay removed");

    const launcher=check(document.querySelector("#controlChatLauncher"),"Chat launcher");launcher.click();await waitFor(()=>document.querySelector("#a31ControlChatForm"),"Assistant chat");
    const form=document.querySelector("#a31ControlChatForm");form.querySelector("textarea").value="hello";form.requestSubmit(form.querySelector('button:not([name="run"])'));
    await waitFor(()=>document.querySelector("#controlChatBody")?.textContent?.includes("No eligible reasoning model is configured."),"no-model Assistant response",30000);
    const panel=document.querySelector("#controlChatPanel"),before=panel.dataset.collapsed;document.querySelector("#controlChatToggle")?.click();check(panel.dataset.collapsed!==before,"Chat collapse");document.querySelector("#controlChatToggle")?.click();a31CloseControlChat();

    results.push("installed behavioural acceptance complete");post("PASS");
  }

  window.onepaneReleaseSmoke=()=>run().catch(ex=>{results.push("FAIL: "+String(ex?.message||ex));post("FAIL")});
})();