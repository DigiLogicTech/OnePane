// RC11 contract test for human-reviewed Project Task graph UI. No browser,
// host network, filesystem mutation or Task Gateway access is required.
const assert=require("node:assert/strict");
const fs=require("node:fs");
const vm=require("node:vm");

const src=fs.readFileSync("internal/webui/static/project-task-graphs.js","utf8");
const html=fs.readFileSync("internal/webui/static/index.html","utf8");
const development=fs.readFileSync("internal/webui/static/project-development-page.js","utf8");
assert(html.includes('<script src="/project-task-graphs.js"></script>'));
assert(development.includes('await a61MountProjectTaskGraphs(project,workspace,container)'));
assert(src.includes('name="approved"'));
assert(src.includes("operator-approved"));
assert(src.includes('orchestrator/task-graphs'));
assert(!src.includes("window.open("));
const escapeHtml=value=>String(value).replace(/&/g,"&amp;").replace(/</g,"&lt;")
 .replace(/>/g,"&gt;").replace(/"/g,"&quot;").replace(/'/g,"&#39;");
const scope=vm.runInNewContext(src+"\n({a61NodesFromDraft,a61GraphCard,a61GraphBuilderMarkup})",
 {Map,Date,escapeHtml,apiRequest:()=>{throw Error("no network expected");}});
const example={nodes:[
 {project_workspace_id:"canonical-world",objective:"Build assets",depends:"",priority:0},
 {project_workspace_id:"canonical-story",objective:"Compose narrative",depends:"step1",priority:20}
]};
const canonical=scope.a61NodesFromDraft(example);
assert.deepEqual(JSON.parse(JSON.stringify(canonical)).map(n=>n.depends_on),[[],["step1"]]);
assert.equal(canonical[1].project_workspace_id,"canonical-story");
for(const [name,nodes] of Object.entries({
 "self_cycle":[{...example.nodes[0],depends:"step1"}],
 "unknown_dep":[{...example.nodes[0],depends:"step9"}],
 "duplicate_deps":[example.nodes[0],{...example.nodes[1],depends:"step1, step1"}],
 "no_scope":[{...example.nodes[0],project_workspace_id:""}],
 "empty_task":[{...example.nodes[0],objective:"  "}],
 "unsafe_priority":[{...example.nodes[0],priority:999}],
})){
 assert.throws(()=>scope.a61NodesFromDraft({nodes}),undefined,name);
}
const attack='<img src=x onerror="alert(1)">';
const card=scope.a61GraphCard({name:attack,status:attack,progress:{},nodes:[{
 key:attack,project_workspace_id:attack,readiness:attack,next_action:attack,
 blocked_by:[attack],failed_or_intervened_on:[attack]}]});
assert(!card.includes("<img"));
assert(card.includes("&lt;img"));
const builder=scope.a61GraphBuilderMarkup({
 name:attack,idempotency_key:"safe-key",approved:false,
 nodes:[{project_workspace_id:"canonical-world",objective:attack,depends:"",priority:0}]
},[{id:"canonical-world",name:attack,status:"active"}]);
assert(!builder.includes("<img"));
assert(builder.includes("Approve and create Task graph"));
assert(builder.includes("eligible Tasks"));
process.stdout.write("PASS: canonical Workspace Task DAG builder, approval, validation, escaping and read-only status\n");
