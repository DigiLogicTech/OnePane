/* Alpha 3.2 Tasks page remediation.
 * Loaded after the frozen compatibility foundation and before canonical boot.
 * Owns the effective Tasks page flows without expanding app.js past its review budget.
 */
function qaTaskProjectOptions(){
  const rows=(qa4ProjectHub?.projects||[]).filter(p=>p&&p.status!=="archived");
  return '<option value="">No project / Global</option>'+rows.map(p=>`<option value="${escapeHtml(p.id)}">${escapeHtml(p.name||p.id)}</option>`).join('');
}

async function qaTaskBindScope(projectSelector,workspaceSelector){
  const project=$(projectSelector),workspace=$(workspaceSelector);
  if(!project||!workspace)return;
  const refresh=async()=>{
    const projectID=project.value;
    if(!projectID){
      workspace.innerHTML='<option value="">No workspace / Global</option>';
      workspace.disabled=true;
      return;
    }
    workspace.disabled=true;
    workspace.innerHTML='<option value="">Loading workspaces…</option>';
    try{
      const rows=await apiRequest(`/v1/projects/${encodeURIComponent(projectID)}/workspaces`);
      const list=Array.isArray(rows)?rows:[];
      workspace.innerHTML='<option value="">Project default / No specific workspace</option>'+list.map(w=>`<option value="${escapeHtml(w.id)}">${escapeHtml(w.name||w.id)}</option>`).join('');
      workspace.disabled=false;
    }catch{
      workspace.innerHTML='<option value="">Workspace list unavailable</option>';
      workspace.disabled=true;
    }
  };
  project.onchange=refresh;
  await refresh();
}

openNewTask=async function(){
  await qa4LoadProjectHub(false);
  openModal('New Task',`<form id="newTaskForm" class="qa-form">
    <label>Objective<textarea name="objective" rows="4" required placeholder="What should OnePane accomplish?"></textarea></label>
    <div class="form-grid">
      <label>Priority<input name="priority" type="number" min="-100" max="100" value="0"></label>
      <label>Scheduling<select name="scheduling_class"><option value="user_interactive">Interactive</option><option value="normal_task">Normal</option><option value="background_routine">Background</option></select></label>
    </div>
    <div class="form-grid">
      <label>Project <span class="page-subtitle">(optional)</span><select id="newTaskProject" name="project_id">${qaTaskProjectOptions()}</select></label>
      <label>Workspace <span class="page-subtitle">(optional)</span><select id="newTaskWorkspace" name="project_workspace_id" disabled><option value="">No workspace / Global</option></select></label>
    </div>
    <div class="error" id="newTaskError"></div>
    <button class="btn primary" type="submit">Create task</button>
  </form>`);
  await qaTaskBindScope('#newTaskProject','#newTaskWorkspace');
  $('#newTaskForm').onsubmit=async e=>{
    e.preventDefault();
    const f=Object.fromEntries(new FormData(e.currentTarget));
    const payload={workspace_id:onepaneWorkspace,objective:f.objective,priority:Number(f.priority||0),scheduling_class:f.scheduling_class};
    if(f.project_id)payload.project_id=f.project_id;
    if(f.project_workspace_id)payload.project_workspace_id=f.project_workspace_id;
    try{
      await apiRequest('/v1/tasks',{method:'POST',body:JSON.stringify(payload)});
      liveOps.lastRefresh=0;
      closeModal();
      await renderTasks();
      notice('Task created and handed to the OnePane scheduler.');
    }catch(ex){
      $('#newTaskError').textContent=ex.message;
    }
  };
};

function qaTaskTriggerLabel(r){
  const t=qa4JSON(r?.TriggerJSON||r?.trigger_json||{},{});
  if(t.kind==='interval')return `Every ${Math.max(1,Math.round(Number(t.every_seconds||0)/60))} min`;
  if(t.kind==='daily'){
    const names=['Mon','Tue','Wed','Thu','Fri','Sat','Sun'];
    const days=Array.isArray(t.weekdays)&&t.weekdays.length?` · ${t.weekdays.map(day=>names[Number(day)-1]||day).join(', ')}`:'';
    return `${t.local_time||'Daily'}${days}`;
  }
  return t.kind||'Scheduled';
}

function qaTaskRoutineObjective(r){
  const p=qa4JSON(r?.PolicyJSON||r?.policy_json||{},{});
  return p.objective||r.name||'Scheduled task';
}

function qaTaskNormaliseRoutine(r){
  return {...r,id:r.id??r.ID,name:r.name??r.Name,status:r.status??r.Status,
    timezone:r.timezone??r.Timezone,updated_at:r.updated_at??r.UpdatedAt,
    trigger_json:r.trigger_json??r.TriggerJSON,policy_json:r.policy_json??r.PolicyJSON};
}

function qaTaskRoutineEndLabel(r){
  const t=qa4JSON(r?.TriggerJSON||r?.trigger_json||{},{}),p=qa4JSON(r?.PolicyJSON||r?.policy_json||{},{});
  if(Number(p.max_occurrences||0)>0)return `After ${Number(p.max_occurrences)} run${Number(p.max_occurrences)===1?'':'s'}`;
  if(Number(t.end_at_utc||0)>0)return `Until ${new Date(Number(t.end_at_utc)).toLocaleString()}`;
  return 'No end';
}

function qaTaskProjectName(id){
  const p=(qa4ProjectHub.projects||[]).find(x=>x.id===id);
  return p?.name||id||'';
}

async function qaTaskArchive(task,restore=false){
  try{
    await apiRequest(`/v1/tasks/${encodeURIComponent(task.id)}/${restore?'unarchive':'archive'}`,{method:'POST',body:JSON.stringify({expected_revision:Number(task.revision||1)})});
    liveOps.lastRefresh=0;
    await renderTasks();
    notice(restore?'Task restored.':'Task archived.');
  }catch(ex){
    notice(ex.message,'bad');
  }
}

function qaTaskFilterRows(rows,{query='',project='',state=''}={}){
  const search=String(query).trim().toLocaleLowerCase();
  return rows.filter(t=>(!project||(project==='global'?!t.project_id:t.project_id===project))&&
    (!state||t.state===state)&&(!search||[t.objective,t.id,qaTaskProjectName(t.project_id)].some(value=>String(value||'').toLocaleLowerCase().includes(search))));
}

let qaTaskLiveDraw=null;
function qaTaskRefreshLiveView(){if(currentTab()?.route==='tasks')qaTaskLiveDraw?.();}
function qaTaskSyncStateOptions(select,rows){
  const selected=select.value;
  const states=[...new Set([...rows.map(t=>t.state),selected].filter(Boolean))].sort();
  const markup='<option value="">All states</option>'+states.map(value=>`<option value="${escapeHtml(value)}">${escapeHtml(titleCase(value.replaceAll('_',' ')))}</option>`).join('');
  if(select.innerHTML!==markup){select.innerHTML=markup;select.value=selected;}
}

renderTasks=async function(){
  qaTaskLiveDraw=null;
  $('#viewHost').innerHTML=`<section class="page tasks-page">${pageHeader('Tasks','Track work, resolve blockers and review results.','<button class="btn" id="newScheduledTask">Schedule task</button><button class="btn primary" id="newTaskButton">New task</button>')}
    <div class="subtabs">
      <button class="subtab active" data-task-tab="current">Task list</button>
      <button class="subtab" data-task-tab="scheduled">Recurring / Scheduled</button>
      <button class="subtab" data-task-tab="archived">Archived</button>
    </div>
    <div class="task-filters" id="taskFilters"><input type="search" id="taskSearch" aria-label="Find a task" placeholder="Find a task…"><select id="taskProjectFilter" aria-label="Filter tasks by project"><option value="">All projects</option></select><select id="taskStateFilter" aria-label="Filter tasks by state"><option value="">All states</option></select><span id="taskResultCount" class="project-result-count" role="status"></span></div>
    <div id="tasksBody" class="table-shell"><div class="widget-body">Loading tasks…</div></div>
  </section>`;
  const host=$('#tasksBody');
  $('#newTaskButton').onclick=openNewTask;
  $('#newScheduledTask').onclick=qa4OpenScheduledTask;
  await qa4LoadProjectHub(false);

  let tasks=[],archived=[],routines=[];
  try{
    [tasks,archived,routines]=await Promise.all([
      apiRequest(`/v1/tasks?workspace_id=${encodeURIComponent(onepaneWorkspace)}&limit=250`),
      apiRequest(`/v1/tasks?workspace_id=${encodeURIComponent(onepaneWorkspace)}&limit=250&archived=1`),
      apiRequest(`/v1/routines?workspace_id=${encodeURIComponent(onepaneWorkspace)}`)
    ]);
  }catch(ex){
    if(host.isConnected)host.innerHTML=`<div class="widget-body error">${escapeHtml(ex.message)}</div>`;
    return;
  }
  if(!host.isConnected)return;

  liveOps.tasks=Array.isArray(tasks)?tasks:[];
  liveOps.routines=Array.isArray(routines)?routines.map(qaTaskNormaliseRoutine):[];
  liveOps.reported.tasks=true;
  liveOps.reported.routines=true;
  qa4ProjectHub.routines=liveOps.routines;
  syncLiveNotifications();
  const archivedTasks=Array.isArray(archived)?archived:[];
  $('#taskProjectFilter').innerHTML='<option value="">All projects</option><option value="global">Global tasks</option>'+qa4ProjectHub.projects.map(p=>`<option value="${escapeHtml(p.id)}">${escapeHtml(p.name||p.id)}</option>`).join('');

  const taskRows=(rows,restore=false)=>rows.length?rows.map(t=>`<tr data-inspect-kind="task" data-inspect-id="${escapeHtml(t.id)}">
    <td><strong>${escapeHtml(t.objective||t.id)}</strong><div class="list-meta">${escapeHtml(t.project_id?qaTaskProjectName(t.project_id):t.id||'')}${t.project_workspace_id?` · ${escapeHtml(t.project_workspace_id)}`:''}</div></td>
    <td><span class="pill ${['failed','blocked','waiting_approval'].includes(t.state)?'warn':t.state==='complete'?'good':''}">${escapeHtml(titleCase(String(t.state||'').replaceAll('_',' ')))}</span></td>
    <td>${escapeHtml(({user_interactive:'Interactive',normal_task:'Normal',background_routine:'Background'})[t.scheduling_class]||t.scheduling_class||'')}</td>
    <td>${Number(t.priority||0)}</td>
    <td>${t.updated_at?escapeHtml(new Date(Number(t.updated_at)).toLocaleString()):''}</td>
    <td><div class="task-row-actions"><button class="btn tiny" data-task-open="${escapeHtml(t.id)}" aria-label="Inspect task: ${escapeHtml(t.objective||t.id)}">Inspect</button><button class="btn tiny" data-task-archive="${escapeHtml(t.id)}" data-task-restore="${restore?'1':'0'}">${restore?'Restore':'Archive'}</button></div></td>
  </tr>`).join(''):`<tr><td colspan="6" class="muted-cell">${$('#taskSearch').value||$('#taskProjectFilter').value||$('#taskStateFilter').value?'No tasks match these filters.':restore?'No archived tasks.':'No tasks yet. Create a task to get started.'}</td></tr>`;

  const bindTaskActions=()=>{
    $$('[data-task-open]',host).forEach(b=>b.onclick=e=>{e.stopPropagation();const t=[...liveOps.tasks,...archivedTasks].find(t=>t.id===b.dataset.taskOpen);if(t)qa4Inspect('task',t.id,t.objective||t.id);});
    $$('[data-task-archive]',$('#tasksBody')).forEach(b=>b.onclick=e=>{
      e.preventDefault();e.stopPropagation();
      const rows=b.dataset.taskRestore==='1'?archivedTasks:liveOps.tasks;
      const t=rows.find(x=>x.id===b.dataset.taskArchive);
      if(t)qaTaskArchive(t,b.dataset.taskRestore==='1');
    });
    bindViewActions($('#tasksBody'));
  };

  const draw=(tab='current')=>{
    qaTaskSyncStateOptions($('#taskStateFilter'),[...liveOps.tasks,...archivedTasks]);
    $('#taskFilters').hidden=tab==='scheduled';
    $$('[data-task-tab]').forEach(b=>b.classList.toggle('active',b.dataset.taskTab===tab));
    if(tab==='scheduled'){
      $('#tasksBody').innerHTML=`<table class="data-table"><thead><tr><th>Name / Objective</th><th>Schedule</th><th>Status</th><th>Timezone</th><th>Updated</th></tr></thead><tbody>${qa4ProjectHub.routines.length?qa4ProjectHub.routines.map(r=>`<tr data-inspect-kind="routine" data-inspect-id="${escapeHtml(r.id)}"><td><strong>${escapeHtml(r.name||qaTaskRoutineObjective(r))}</strong><div class="list-meta">${escapeHtml(qaTaskRoutineObjective(r))}</div></td><td><div>${escapeHtml(qaTaskTriggerLabel(r))}</div><div class="list-meta">${escapeHtml(qaTaskRoutineEndLabel(r))}</div></td><td><span class="pill ${r.status==='active'?'good':''}">${escapeHtml(r.status||'active')}</span></td><td>${escapeHtml(r.timezone||'')}</td><td>${r.updated_at?escapeHtml(new Date(Number(r.updated_at)).toLocaleString()):''}</td></tr>`).join(''):'<tr><td colspan="5" class="muted-cell">No recurring or scheduled tasks yet.</td></tr>'}</tbody></table>`;
      bindViewActions($('#tasksBody'));
      return;
    }
    const all=tab==='archived'?archivedTasks:liveOps.tasks;
    const filtered=qaTaskFilterRows(all,{query:$('#taskSearch').value,project:$('#taskProjectFilter').value,state:$('#taskStateFilter').value});
    $('#taskResultCount').textContent=`${filtered.length} of ${all.length} loaded tasks`;
    $('#tasksBody').innerHTML=`<table class="data-table"><thead><tr><th>Objective</th><th>State</th><th>Scheduling</th><th>Priority</th><th>Updated</th><th>Actions</th></tr></thead><tbody>${taskRows(filtered,tab==='archived')}</tbody></table>`;
    bindTaskActions();
  };

  let selectedTab='current';
  $$('[data-task-tab]').forEach(b=>b.onclick=()=>{selectedTab=b.dataset.taskTab;draw(selectedTab)});
  $('#taskSearch').oninput=()=>draw(selectedTab);
  $('#taskProjectFilter').onchange=()=>draw(selectedTab);
  $('#taskStateFilter').onchange=()=>draw(selectedTab);
  qaTaskLiveDraw=()=>{if(host.isConnected)draw(selectedTab);};
  draw('current');
};

qa4OpenScheduledTask=async function(){
  await qa4LoadProjectHub(false);
  const tz=Intl.DateTimeFormat().resolvedOptions().timeZone||'UTC';
  openModal('New scheduled task',`<form id="qa4RoutineForm" class="qa-form">
    <label>Name<input name="name" required placeholder="Daily system review"></label>
    <label>Objective<textarea name="objective" rows="3" required></textarea></label>
    <div class="form-grid">
      <label>Project <span class="page-subtitle">(optional)</span><select id="qa4RoutineProject" name="project_id">${qaTaskProjectOptions()}</select></label>
      <label>Workspace <span class="page-subtitle">(optional)</span><select id="qa4RoutineWorkspace" name="project_workspace_id" disabled><option value="">No workspace / Global</option></select></label>
    </div>
    <div class="form-grid">
      <label>Schedule<select name="kind" id="qa4RoutineKind"><option value="interval">Interval</option><option value="daily">Daily / selected weekdays</option></select></label>
      <label>Timezone<input name="timezone" value="${escapeHtml(tz)}"></label>
    </div>
    <div id="qa4Interval"><label>Every (minutes)<input name="minutes" type="number" min="1" value="60"></label></div>
    <div id="qa4Daily" class="hidden"><label>Local time<input name="local_time" type="time" value="08:00"></label><fieldset class="schedule-weekdays"><legend>Run on</legend>${['Monday','Tuesday','Wednesday','Thursday','Friday','Saturday','Sunday'].map((day,i)=>`<label class="inline-check"><input type="checkbox" name="weekdays" value="${i+1}" ${i<5?'checked':''}>${day}</label>`).join('')}</fieldset></div>
    <div class="form-grid">
      <label>Priority<input name="priority" type="number" value="0" min="-100" max="100"></label>
      <label>End condition<select name="end_mode" id="qa4RoutineEndMode"><option value="none">No end</option><option value="count">After N runs</option><option value="until">Until date / time</option></select></label>
    </div>
    <div id="qa4RoutineEndCount" class="hidden"><label>Number of runs<input name="end_count" type="number" min="1" max="100000" value="5"></label></div>
    <div id="qa4RoutineEndUntil" class="hidden"><label>Run until<input name="end_at" type="datetime-local"></label></div>
    <div class="error" id="qa4RoutineError"></div>
    <button class="btn primary" type="submit">Create scheduled task</button>
  </form>`);
  await qaTaskBindScope('#qa4RoutineProject','#qa4RoutineWorkspace');

  $('#qa4RoutineKind').onchange=()=>{
    $('#qa4Interval').classList.toggle('hidden',$('#qa4RoutineKind').value!=='interval');
    $('#qa4Daily').classList.toggle('hidden',$('#qa4RoutineKind').value!=='daily');
  };
  $('#qa4RoutineEndMode').onchange=()=>{
    const mode=$('#qa4RoutineEndMode').value;
    $('#qa4RoutineEndCount').classList.toggle('hidden',mode!=='count');
    $('#qa4RoutineEndUntil').classList.toggle('hidden',mode!=='until');
  };

  $('#qa4RoutineForm').onsubmit=async e=>{
    e.preventDefault();
    const formData=new FormData(e.currentTarget),f=Object.fromEntries(formData);
    const trigger=f.kind==='interval'
      ?{kind:'interval',every_seconds:Math.max(60,Number(f.minutes||1)*60)}
      :{kind:'daily',local_time:f.local_time||'08:00',weekdays:formData.getAll('weekdays').map(Number).filter(x=>x>=1&&x<=7)};
    if(f.kind==='daily'&&!trigger.weekdays.length){$('#qa4RoutineError').textContent='Select at least one day.';return}
    const policy={catch_up:'latest',max_catch_up:10,objective:f.objective,priority:Number(f.priority||0)};
    if(f.project_id)policy.project_id=f.project_id;
    if(f.project_workspace_id)policy.project_workspace_id=f.project_workspace_id;
    if(f.end_mode==='count'){
      policy.max_occurrences=Math.max(1,Number(f.end_count||1));
    }else if(f.end_mode==='until'){
      const endAt=Date.parse(f.end_at||'');
      if(!Number.isFinite(endAt)||endAt<=Date.now()){
        $('#qa4RoutineError').textContent='Run until must be a future date and time.';
        return;
      }
      trigger.end_at_utc=endAt;
    }
    try{
      await apiRequest('/v1/routines',{method:'POST',body:JSON.stringify({workspace_id:onepaneWorkspace,name:f.name,timezone:f.timezone||tz,trigger,policy})});
      closeModal();
      await renderTasks();
      notice('Scheduled task created.');
    }catch(ex){
      $('#qa4RoutineError').textContent=ex.message;
    }
  };
};
