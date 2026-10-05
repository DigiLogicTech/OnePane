(function(){
  "use strict";

  window.ONEPANE_APPLY_RC3=function(){
    let viewEpoch=0;
    let modelView=localStorage.getItem("onepane:models-view")||"local";

    function currentRoute(){return currentTab()?.route||"operations";}
    function routeIs(route){return currentRoute()===route;}
    function modelPageTabs(){
      return '<div class="models-section-tabs" role="tablist" aria-label="Models pages">'+
        '<button class="subtab '+(modelView==="local"?"active":"")+'" data-model-page="local" role="tab" aria-selected="'+(modelView==="local")+'">Local Models</button>'+
        '<button class="subtab '+(modelView==="cloud"?"active":"")+'" data-model-page="cloud" role="tab" aria-selected="'+(modelView==="cloud")+'">Cloud Models</button>'+
      '</div>';
    }
    function setModelView(view){
      modelView=view==="cloud"?"cloud":"local";
      localStorage.setItem("onepane:models-view",modelView);
      const existing=state.tabs.find(t=>t.route==="models");
      if(existing)activateTab(existing.id);else openRoute("models");
    }
    function modelNavTreeHTML(){
      return '<div class="model-nav-tree" aria-label="Model pages">'+
        '<button class="model-nav-child '+(modelView==="local"?"active":"")+'" data-model-nav-view="local"><span class="project-nav-branch" aria-hidden="true"></span><span>Local Models</span></button>'+
        '<button class="model-nav-child '+(modelView==="cloud"?"active":"")+'" data-model-nav-view="cloud"><span class="project-nav-branch" aria-hidden="true"></span><span>Cloud Models</span></button>'+
      '</div>';
    }
    function renderModelNavTree(){
      const root=$("#primaryNav"),modelsButton=root?.querySelector('[data-route="models"]');
      if(!root||!modelsButton)return;
      root.querySelector(".model-nav-tree")?.remove();
      modelsButton.insertAdjacentHTML("afterend",modelNavTreeHTML());
      $$("[data-model-nav-view]",root).forEach(b=>b.onclick=e=>{e.stopPropagation();setModelView(b.dataset.modelNavView);});
    }

    const navBase=renderNav;
    renderNav=function(){
      navBase();
      renderModelNavTree();
      syncPanelRestoreButtons();
    };

    function bindModelPageTabs(){
      $$("[data-model-page]").forEach(b=>b.onclick=()=>setModelView(b.dataset.modelPage));
    }
    function cloudProviderCards(presets,connections){
      return presets.map(p=>{
        const match=connections.find(c=>String(c.provider||"")===String(p.id)||String(c.display_name||"").toLowerCase()===String(p.display_name||"").toLowerCase());
        const connected=match&&String(match.status||"").toLowerCase()!=="revoked";
        const oauth=p.auth_type==="oauth2-pkce";
        const action=connected
          ? '<button class="btn danger" data-revoke-cloud="'+escapeHtml(match.id)+'">Revoke</button>'
          : oauth
            ? '<button class="btn" data-oauth-info="'+escapeHtml(p.id)+'">Connect OAuth</button>'
            : '<button class="btn primary" data-connect-cloud="'+escapeHtml(p.id)+'">Connect</button>';
        return '<article class="provider-tile">'+
          '<div class="provider-tile-head"><strong>'+escapeHtml(p.display_name)+'</strong><span class="pill '+(connected?"good":"")+'">'+
          (connected?(oauth?"OAuth connected":"Connected"):(match?.status==="revoked"?"Revoked":"Available"))+'</span></div>'+
          '<p>'+escapeHtml(p.description||"")+'</p>'+
          '<div class="provider-meta"><span>'+(oauth?"OAuth":escapeHtml(p.auth_type||"API key"))+'</span><span>'+escapeHtml(p.cost_hint||"")+'</span></div>'+
          '<div class="toolbar">'+action+'</div></article>';
      }).join("");
    }
    function bindCloudCards(presets){
      $$("[data-revoke-cloud]").forEach(b=>b.onclick=()=>qa4RevokeProvider(b.dataset.revokeCloud));
      $$("[data-connect-cloud]").forEach(b=>b.onclick=()=>qa4ConnectCloudProvider(b.dataset.connectCloud));
      $$("[data-oauth-info]").forEach(b=>b.onclick=()=>{
        const preset=presets.find(p=>String(p.id)===b.dataset.oauthInfo);
        openModal("OAuth connection",'<div class="widget-body"><strong>'+escapeHtml(preset?.display_name||"OAuth provider")+'</strong><p>When an OAuth broker is available, OnePane records the connection here and exposes status and revoke controls. This alpha will not fake an OAuth consent flow.</p></div>');
      });
    }
    async function revokeOmni(id){
      if(!id)return;
      try{
        await apiRequest("/v1/providers/"+encodeURIComponent(id)+"/revoke",{method:"POST",body:"{}"});
        notice("OmniRoute connection revoked.");
        if(routeIs("models"))renderModels();
      }catch(ex){notice(ex.message,"bad");}
    }

    async function renderLocal(catalog,deployments,components,runtimePref){
      const colibri=components?.colibri||{};
      const stateName=String(colibri.state||"not_installed");
      const pillClass=["running","installed_disabled"].includes(stateName)?"good":(["failed","degraded","interrupted"].includes(stateName)?"warn":"");
      $("#qa5ModelsRoot").innerHTML=
        '<div class="models-single-column">'+
          '<section class="panel-card models-local-card">'+
            '<div class="card-header models-card-header">'+
              '<div class="models-header-copy"><div class="card-title">Local models</div><div class="list-meta">Managed Hot Swap for ordinary models; Colibri Large Model for compatible sparse/MoE models spanning VRAM + RAM + NVMe.</div></div>'+
              '<div class="header-actions qa31-local-models-controls"><button class="btn qa31-detect-hardware" id="detectLocal">Detect hardware</button>'+
                '<select class="qa31-runtime-strategy" id="qa5RuntimeStrategy">'+
                  '<option value="auto" '+(runtimePref==="auto"?"selected":"")+'>Auto</option>'+
                  '<option value="hot-swap" '+(runtimePref==="hot-swap"?"selected":"")+'>Managed Hot Swap</option>'+
                  '<option value="colibri" '+(runtimePref==="colibri"?"selected":"")+'>Colibri Large Model</option>'+
                '</select></div>'+
            '</div>'+
            '<div id="localResult" class="model-result"></div>'+
            '<div class="local-managed-section"><div class="subsection-title">Installed / registered</div><div id="qa5ManagedModels" class="model-tile-scroll"></div></div>'+
            '<div class="subsection-title">Available catalogue</div><input id="qa4LocalFilter" class="catalogue-filter" placeholder="Filter local models…"><div id="qa4LocalModels" class="model-tile-scroll"></div>'+
          '</section>'+
          '<section class="panel-card colibri-separate">'+
            '<div class="card-header models-card-header"><div class="models-header-copy"><div class="card-title">Colibri Large Model</div><div class="list-meta">Optional Apache-2.0 backend v1.12.1 for very large sparse/MoE models.</div></div><span class="pill '+pillClass+'">'+escapeHtml(titleCase(stateName.replaceAll("_"," ")))+'</span></div>'+
            '<div class="widget-body"><p>OnePane remains scheduler/admission authority; Colibri manages model placement across VRAM, RAM and NVMe. Registered models remain quarantined until Agent Check/Testbed qualification.</p>'+
              '<div class="toolbar" id="qa31ColibriActions">'+qa31ColibriActionButtons(colibri)+(colibri.installed?'<button class="btn" id="qa5ColibriRegister">Register model folder</button>':"")+'</div>'+
              '<div id="qa5ColibriInlineStatus" class="page-subtitle">'+
                (colibri.last_error?'<span class="warn">'+escapeHtml(colibri.last_error)+'</span>':(colibri.installed?"Runtime installed. Use Enable/Disable for availability; Update/Repair/Remove are lifecycle operations.":"Install the verified runtime before registering compatible models."))+
              '</div>'+
            '</div>'+
          '</section>'+
        '</div>';

      $("#detectLocal").onclick=detectLocalQA;
      $("#qa5RuntimeStrategy").onchange=e=>qa5SavePrefs({default_runtime:e.target.value});
      $$("[data-qa31-colibri-action]").forEach(b=>b.onclick=()=>qa5ComponentAction("colibri",b.dataset.qa31ColibriAction,"#qa5ColibriInlineStatus"));
      $("#qa5ColibriRegister")?.addEventListener("click",qa5RegisterColibri);

      const drawManaged=()=>{
        $("#qa5ManagedModels").innerHTML=deployments.length?deployments.map((d,i)=>{
          const ctx=d.context_max_verified?(formatContextQA(d.context_max_verified)+" verified context"):(d.context_max_reported?(formatContextQA(d.context_max_reported)+" reported context"):"context pending qualification");
          return '<article class="model-tile inspectable" data-model-inspect="'+i+'"><div><strong>'+escapeHtml(d.display_name||d.model_ref||"Local model")+'</strong>'+
            '<div class="list-meta">'+escapeHtml(d.runtime_name||d.runtime_backend||"managed")+' '+escapeHtml(d.runtime_version||"")+' · '+escapeHtml(d.status||"unknown")+' · admission '+escapeHtml(d.admission_status||"pending")+'</div>'+
            '<div class="list-meta">'+escapeHtml(d.quantization||"")+' · '+ctx+'</div></div>'+
            '<div class="toolbar"><button class="btn" data-model-spec="'+i+'">Spec sheet</button><button class="btn primary" data-agent-check="'+i+'">Agent Check</button></div></article>';
        }).join(""):'<div class="empty-state compact">No managed local models yet.</div>';
        $$("[data-model-spec]").forEach(b=>b.onclick=e=>{e.stopPropagation();qa5InspectModel(deployments[Number(b.dataset.modelSpec)]);});
        $$("[data-agent-check]").forEach(b=>b.onclick=e=>{e.stopPropagation();qa5AgentCheck(deployments[Number(b.dataset.agentCheck)]);});
        $$("[data-model-inspect]").forEach(el=>el.onclick=e=>{if(e.target.closest("button"))return;qa5InspectModel(deployments[Number(el.dataset.modelInspect)]);});
      };
      drawManaged();

      const drawLocal=()=>{
        const q=($("#qa4LocalFilter").value||"").toLowerCase();
        const rows=catalog.filter(m=>JSON.stringify(m).toLowerCase().includes(q));
        $("#qa4LocalModels").innerHTML=rows.length?rows.map((m,i)=>{
          const quant=Array.isArray(m.quantizations)?m.quantizations.join(", "):(m.quantization||"llama.cpp");
          return '<article class="model-tile"><div><strong>'+escapeHtml(m.display_name||m.name||m.model_ref||"Model")+'</strong>'+
            '<div class="list-meta">'+escapeHtml(String(m.parameter_count||m.parameter_scale||"—"))+' · '+formatContextQA(m.max_context_tokens||m.context_tokens)+' context</div>'+
            '<div class="list-meta">'+escapeHtml(quant)+'</div></div><button class="btn primary" data-download-model="'+i+'">Download</button></article>';
        }).join(""):'<div class="empty-state compact">No local models match this filter.</div>';
        $$("[data-download-model]").forEach(b=>b.onclick=()=>qa4InstallLocalModel(rows[Number(b.dataset.downloadModel)]));
      };
      $("#qa4LocalFilter").oninput=drawLocal;
      drawLocal();
      queueMicrotask(()=>{try{qa8ManagedDeployments=deployments;qa8DecorateModels();}catch{}});
    }

    async function renderCloud(presets,connections){
      const cloud=presets.filter(p=>p.id!=="omniroute");
      const omni=connections.find(p=>String(p.provider||"").toLowerCase()==="omniroute"&&String(p.status||"").toLowerCase()!=="revoked");
      const saved=localStorage.getItem("onepane:omniroute-url")||omni?.connection?.base_url||"http://127.0.0.1:20128/v1";
      $("#qa5ModelsRoot").innerHTML=
        '<div class="models-single-column">'+
          '<section class="panel-card cloud-provider-card">'+
            '<div class="card-header models-card-header"><div class="models-header-copy"><div class="card-title">Cloud providers</div><div class="list-meta">Direct cloud model/API connections. These remain separate from OmniRoute.</div></div><div class="header-actions"><button class="btn" id="qa4OpenSecrets">API keys</button></div></div>'+
            '<div id="qa4CloudProviders" class="provider-tile-grid">'+cloudProviderCards(cloud,connections)+'</div>'+
          '</section>'+
          '<section class="panel-card omniroute-separate">'+
            '<div class="card-header models-card-header"><div class="models-header-copy"><div class="card-title">OmniRoute</div><div class="list-meta">Optional external provider/router connection. It is not installed or managed as a local runtime.</div></div><span class="pill '+(omni?"good":"")+'">'+(omni?"Connected":"Optional")+'</span></div>'+
            '<div class="widget-body omni-provider-layout">'+
              '<label>Gateway URL<input id="omniUrl" value="'+escapeHtml(saved)+'"></label>'+
              '<label>Gateway credential<select id="omniCredential"><option value="">No gateway credential</option></select></label>'+
              '<label class="inline-check"><input id="omniStrict" type="checkbox" checked> Require verified strict zero-cost</label>'+
              '<div class="toolbar"><button class="btn" id="omniProbe">Probe</button><button class="btn primary" id="omniConnect" '+(omni?"":"disabled")+'>'+(omni?"Reconnect":"Connect")+'</button>'+(omni?'<button class="btn danger" id="qa31OmniRevoke">Revoke</button>':"")+'</div>'+
              '<div id="omniResult" class="page-subtitle">'+(omni?'<span class="good">Connected</span> · '+escapeHtml(omni.status||"configured"):"Probe the gateway before connecting. OmniRoute failure never makes OnePane unhealthy.")+'</div>'+
            '</div>'+
          '</section>'+
        '</div>';

      $("#qa4OpenSecrets").onclick=()=>openRoute("secrets");
      bindCloudCards(cloud);
      $("#omniProbe").onclick=()=>omniQA(false);
      $("#omniConnect").onclick=()=>omniQA(true);
      $("#qa31OmniRevoke")?.addEventListener("click",()=>revokeOmni(omni?.id));
      $("#omniUrl").addEventListener("change",()=>localStorage.setItem("onepane:omniroute-url",$("#omniUrl").value.trim()));
      populateOmniCredentials();
    }

    renderModels=async function(){
      const epoch=viewEpoch;
      const view=modelView==="cloud"?"cloud":"local";
      const subtitle=view==="local"?"Local model downloads, qualification, runtime strategy and managed Colibri lifecycle.":"Direct cloud providers and optional OmniRoute provider routing.";
      $("#viewHost").innerHTML='<section class="page models-page">'+pageHeader("Models",subtitle,'<button class="btn" id="modelSettings">Local AI settings</button>')+modelPageTabs()+'<div id="qa5ModelsRoot" class="widget-body">Loading '+(view==="local"?"local models":"cloud providers")+'…</div></section>';
      $("#modelSettings").onclick=()=>openRoute("settings");
      bindModelPageTabs();
      const qs=encodeURIComponent(onepaneWorkspace);
      try{
        if(view==="local"){
          const data=await Promise.all([apiRequest("/v1/local-ai/catalog"),qa5LoadManagedDeployments(),qa5ComponentStatus()]);
          if(epoch!==viewEpoch||!routeIs("models")||modelView!=="local")return;
          qa4ProjectHub.catalog=Array.isArray(data[0])?data[0]:[];
          await renderLocal(qa4ProjectHub.catalog,Array.isArray(data[1])?data[1]:[],data[2]||{},qa5Prefs().default_runtime||"auto");
        }else{
          const data=await Promise.all([providerPresetsQA(),apiRequest("/v1/providers?workspace_id="+qs)]);
          if(epoch!==viewEpoch||!routeIs("models")||modelView!=="cloud")return;
          qa4ProjectHub.providerPresets=Array.isArray(data[0])?data[0]:[];
          qa4ProjectHub.providers=Array.isArray(data[1])?data[1]:[];
          await renderCloud(qa4ProjectHub.providerPresets,qa4ProjectHub.providers);
        }
        bindViewActions($("#viewHost"));
      }catch(ex){
        if(epoch===viewEpoch&&routeIs("models")){
          const root=$("#qa5ModelsRoot");if(root)root.innerHTML='<div class="error">'+escapeHtml(ex.message)+'</div>';
        }
      }
    };

    const projectsBase=renderProjects;
    renderProjects=async function(){
      const epoch=viewEpoch;
      await projectsBase();
      if(epoch!==viewEpoch||!routeIs("projects"))return;
      qa31RenderProjectNavTree?.();
    };

    renderActiveView=async function(){
      const epoch=++viewEpoch;
      const t=currentTab();if(!t)return;
      if(QA4_ROUTE_ALIASES[t.route]){t.route=QA4_ROUTE_ALIASES[t.route];t.title=pages[t.route]?.title||t.route;persist();}
      if(t.state==="suspended")t.state="active";
      const route=t.route;
      const renderers={
        operations:renderOperations,tasks:renderTasks,projects:renderProjects,models:renderModels,nodes:renderNodes,agents:renderAgents,
        routines:renderTasks,integrations:renderIntegrations,secrets:renderSecrets,
        evidence:()=>renderPlaceholder("Evidence / Audit","Event Ledger, Artifacts, Observations, Verifications, Operations and CapabilityLease activity."),
        settings:renderSettings
      };
      try{await Promise.resolve((renderers[route]||renderOperations)());}
      finally{
        const host=$("#viewHost");
        if(epoch===viewEpoch){
          if(host)host.dataset.renderedRoute=route;
        }else if(currentRoute()!==route){
          const active=currentRoute();
          if(host?.dataset?.renderedRoute!==active)queueMicrotask(()=>{if(currentRoute()===active)renderActiveView();});
        }
      }
    };

    const assistantBase=qa31OpenAssistant;
    qa31OpenAssistant=async function(){
      await assistantBase();
      const surface=$(".qa31-assistant");
      if(surface)surface.classList.add("rc3-assistant-visible");
    };
    openCommandPalette=qa31OpenAssistant;
    qa31WireAssistantButton();

    const tourBase=startProductTour;
    startProductTour=function(opts){
      document.documentElement.dataset.productTour="active";
      const observer=new MutationObserver(()=>{
        if(!document.querySelector("#qa31TourRoot")){
          delete document.documentElement.dataset.productTour;
          observer.disconnect();
        }
      });
      observer.observe(document.body,{childList:true,subtree:true});
      return tourBase(opts||{});
    };

    renderNav();
  };
})();