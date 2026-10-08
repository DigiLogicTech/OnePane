const assert=require("node:assert/strict");
const fs=require("node:fs");
const vm=require("node:vm");
const code=fs.readFileSync("internal/webui/static/web-chat-council-launch.js","utf8");
const providers=[
  {id:"chatgpt",name:"ChatGPT",url:"https://chatgpt.com/"},
  {id:"claude",name:"Claude",url:"https://claude.ai/new"},
  {id:"gemini",name:"Gemini",url:"https://gemini.google.com/app"}
];
const tabs=[];
let saves=0;
const context=vm.createContext({
  A39_WEB_PROVIDERS:providers,onepaneWorkspace:"workspace-1",
  state:{},Date,Math,Map,Set,JSON,Number,String,Array,Error,Promise,
  a40WebTabs:(ws)=>tabs.filter(t=>t.workspace_id===ws),
  a40WebNewSession:(id,title)=>{
    const entry={id:"tab"+(tabs.length+1),workspace_id:"workspace-1",provider_id:id,title,turn_id:""};
    tabs.push(entry);return entry
  },
  A40_WEB_MAX_TABS:16,
  a40WebDrafts:new Map(),persist:()=>saves++,
  a39WebTurns:[],encodeURIComponent,
  notice:()=>{},
  // These references are not invoked by the pure launch flow.
  apiRequest:()=>{throw Error("Network request must use explicit injection");}
});
vm.runInContext(code,context,{filename:"web-chat-council-launch.js"});
function run(js){return vm.runInContext(js,context)}
const draft={
  name:"Research across three subscriptions",
  objective:"Assess the sample-efficiency research architecture against a baseline",
  critique_rounds:2,synthesis_pass:true,seats:[
    {provider_id:"chatgpt",model_label:"Model A",role_name:"Independent researcher"},
    {provider_id:"claude",model_label:"Model B",role_name:"Critical analyst"},
    {provider_id:"chatgpt",model_label:"Model C",role_name:"Alternative reasoning"}
  ]
};
assert.equal(run("a41ValidateWebCouncilDraft")(draft).seats.length,3);
assert.throws(()=>run("a41ValidateWebCouncilDraft")({...draft,seats:[draft.seats[0]]}),/2 and 8/);
assert.throws(()=>run("a41ValidateWebCouncilDraft")({...draft,seats:[...draft.seats,{provider_id:"bad.site",model_label:"x",role_name:"Critic"}]}),/supported cloud provider/);
assert.throws(()=>run("a41ValidateWebCouncilDraft")({...draft,seats:draft.seats.map(s=>({...s,model_label:""}))}),/model/);
assert.throws(()=>run("a41ValidateWebCouncilDraft")({...draft,critique_rounds:0}),/critique rounds/);
assert.throws(()=>run("a41ValidateWebCouncilDraft")({...draft,objective:"x"}),/objective/);
async function test(){
  const calls=[];
  let interrupt=true;
  const count={teams:0,members:0,tasks:0,sessions:0,configs:0};
  const send=async(url,options)=>{
    const body=JSON.parse(options.body);
    calls.push({url,body,method:options.method});
    if(url==="/v1/teams"){
      count.teams++;
      return {ID:"team-1",Revision:1};
    }
    if(url.endsWith("/members")){
      if(interrupt && count.members===1)throw Error("Temporary outage in seat registration");
      count.members++;
      return {ID:"member-"+count.members};
    }
    if(url.endsWith("/configuration")){
      count.configs++;
      assert.equal(body.expected_revision,1);
      assert.equal(body.configuration.research_mode,true);
      assert.equal(body.configuration.research.disable_model_substitution,true);
      assert.equal(body.configuration.research.require_all_seats,true);
      assert.equal(body.configuration.research.independent_first_pass,true);
      assert.equal(body.configuration.research.critique_rounds,2);
      assert.equal(body.configuration.research.synthesis_pass,true);
      return {ID:"team-1",Revision:2};
    }
    if(url==="/v1/tasks"){
      count.tasks++;
      assert.equal(body.completion.type,"operator_review");
      assert.equal(body.scheduling_class,"user_interactive");
      assert.equal(body.objective,draft.objective);
      return {id:"task-1"};
    }
    if(url.endsWith("/team-session")){
      count.sessions++;
      assert.equal(body.execution_mode,"council");
      assert.equal(body.team_id,"team-1");
      assert.equal(body.config.manual_web_only,true);
      assert.equal(body.config.task_execution,false);
      return {ID:"session-1"};
    }
    throw Error("Unknown endpoint "+url);
  };
  let failed=false;
  try {
    await run("a41CreateWebOnlyCouncil")("workspace-1",draft,null,{request:send});
  } catch(e){failed=true;assert.match(e.message,/outage/)}
  assert.equal(failed,true,"first launch should pause on a failed seat");
  const checkpoint=run("a41WebCouncilLaunch");
  assert.equal(checkpoint.team_id,"team-1");
  assert.equal(checkpoint.member_ids.length,1);
  assert.equal(count.teams,1);
  interrupt=false;
  const result=await run("a41CreateWebOnlyCouncil")("workspace-1",draft,checkpoint,{request:send});
  assert.equal(count.teams,1,"resume must not create a second team");
  assert.equal(count.members,3,"resume must create only missing seats");
  assert.equal(count.tasks,1);
  assert.equal(count.sessions,1);
  assert.equal(count.configs,1);
  assert.equal(result.session_id,"session-1");
  const added=run("a41ProvisionWebCouncilTabs")(result);
  assert.equal(added,3,"launch should create one tab per Council seat");
  assert.equal(tabs.length,3);
  assert.equal(tabs.filter(t=>t.provider_id==="chatgpt").length,2,"multiple same-provider web chats must be independent");
  assert.equal(tabs[0].member_id,"member-1");
  assert.equal(tabs[2].member_id,"member-3");
  assert.equal(run("a41ProvisionWebCouncilTabs")(result),0,"repeated provisioning should not duplicate tabs");

  const responseDrafts=run("a40WebDrafts");
  context.a39WebTurns=[];
  const turns=[
    {turn_id:"turn-1",session_id:"session-1",member_id:"member-1",provider_id:"chatgpt",status:"awaiting_input",conversation_generation:1,created_at:1},
    {turn_id:"turn-2",session_id:"session-1",member_id:"member-2",provider_id:"claude",status:"awaiting_input",conversation_generation:1,created_at:1},
    {turn_id:"turn-3",session_id:"session-1",member_id:"member-3",provider_id:"chatgpt",status:"awaiting_input",conversation_generation:1,created_at:1},
    {turn_id:"alien",session_id:"another-session",member_id:"member-1",provider_id:"chatgpt",status:"awaiting_input",conversation_generation:1,created_at:999}
  ];
  run("a41AutoBindWebCouncilHandoffs")(turns);
  assert.equal(tabs[0].turn_id,"turn-1");
  assert.equal(tabs[1].turn_id,"turn-2");
  assert.equal(tabs[2].turn_id,"turn-3","same provider must not take another member's turn");
  responseDrafts.set(tabs[0].id,{response:"unsent draft"});
  const round2=[
    {...turns[0],status:"submitted"},
    {...turns[1],status:"submitted"},
    {...turns[2],status:"submitted"},
    {turn_id:"turn-4",session_id:"session-1",member_id:"member-1",provider_id:"chatgpt",status:"awaiting_input",conversation_generation:1,created_at:2},
    {turn_id:"turn-5",session_id:"session-1",member_id:"member-2",provider_id:"claude",status:"awaiting_input",conversation_generation:1,created_at:2},
    {turn_id:"turn-6",session_id:"session-1",member_id:"member-3",provider_id:"chatgpt",status:"awaiting_input",conversation_generation:1,created_at:2}
  ];
  run("a41AutoBindWebCouncilHandoffs")(round2);
  assert.equal(tabs[0].turn_id,"turn-1","must not discard an unsent draft");
  assert.equal(tabs[1].turn_id,"turn-5");
  assert.equal(tabs[2].turn_id,"turn-6");
  responseDrafts.delete(tabs[0].id);
  run("a41AutoBindWebCouncilHandoffs")(round2);
  assert.equal(tabs[0].turn_id,"turn-4","auto-attach subsequent research round");

  const urls=[...new Set(calls.map(x=>x.url))];
  assert.ok(!urls.some(x=>/inference|execute|run-tool|providers/.test(x)),"no cloud API inference or task action should be requested");
  assert.ok(saves>0);
  console.log("Web-only Council: input limits, 3 providers/seats, staged resume, research policy, independent tabs and round-2 handoffs: PASS");
}
test().catch(e=>{console.error(e);process.exitCode=1});
