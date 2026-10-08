const assert=require("node:assert/strict");
const fs=require("node:fs");
const vm=require("node:vm");
const providers=[
 {id:"chatgpt",name:"ChatGPT",url:"https://chatgpt.com/"},
 {id:"claude",name:"Claude",url:"https://claude.ai/new"},
 {id:"gemini",name:"Gemini",url:"https://gemini.google.com/app"}
];
const tabs=[];
const responses=[];
const drafts=new Map();
let persistCalls=0;
const ctx=vm.createContext({
 A39_WEB_PROVIDERS:providers,A40_WEB_MAX_TABS:16,
 a40WebTabs:(ws="workspace-1")=>tabs.filter(t=>t.workspace_id===ws),
 a40WebNewSession:(provider,title)=>{
  const t={id:"tab-"+(tabs.length+1),workspace_id:"workspace-1",provider_id:provider,title,turn_id:""};
  tabs.push(t);return t;
 },a40WebDrafts:drafts,onepaneWorkspace:"workspace-1",persist:()=>persistCalls++,
 Date,Math,JSON,Set,Map,Number,Array,String,Promise,Error,encodeURIComponent,
 apiRequest:async(path,opts)=>{responses.push({path,body:JSON.parse(opts.body)});return {id:path,status:"awaiting_approval"}},
 notice:()=>{},confirm:()=>true,
 $:()=>({value:"operator edited Chair agenda"}),renderWebChat:()=>{}
});
vm.runInContext(fs.readFileSync("internal/webui/static/web-chat-council-launch.js","utf8"),ctx);
vm.runInContext(fs.readFileSync("internal/webui/static/web-chat-chair.js","utf8"),ctx);
const val=vm.runInContext("a41ValidateWebCouncilDraft",ctx);
const draft={
 name:"Web Research Council",objective:"Compare independent algorithms against objective evidence",
 critique_rounds:2,synthesis_pass:true,synthesis_index:1,chair_mode:"manual",
 chair_provider_id:"chatgpt",chair_model_label:"Selected ChatGPT model",
 chair_require_approval:true,
 seats:[
  {provider_id:"chatgpt",model_label:"ChatGPT A",role_name:"Independent researcher"},
  {provider_id:"claude",model_label:"Claude B",role_name:"Critical analyst"}
 ]
};
assert.equal(val(draft).chair_mode,"manual");
assert.equal(val(draft).synthesis_index,1);
assert.throws(()=>val({...draft,chair_model_label:""}),/Chair/);
assert.throws(()=>val({...draft,seats:[...draft.seats,...Array(6).fill(draft.seats[0])]}),/2–7/);
assert.throws(()=>val({...draft,chair_mode:"local"}),/manual Web Chat Chair/);
assert.equal(val({...draft,chair_mode:"none",chair_model_label:""}).chair_mode,"none");
const setup=vm.runInContext("a41WebCouncilResearch",ctx)(draft,["member-1","member-2","chair"]);
assert.equal(setup.research_mode,true);
assert.equal(setup.research.chair_mode,"manual");
assert.equal(setup.research.chair_member_id,"chair");
assert.equal(setup.research.synthesis_member_id,"member-2");
assert.equal(setup.research.chair_require_approval,true);
assert.equal(setup.research.independent_first_pass,true);
assert.equal(setup.research.require_all_seats,true);
assert.equal(setup.research.disable_model_substitution,true);
const result={workspace_id:"workspace-1",session_id:"session-1",member_ids:["member-1","member-2","chair"],draft,tabs_created:false};
const count=vm.runInContext("a41ProvisionWebCouncilTabs",ctx)(result);
assert.equal(count,3);
assert.equal(tabs.filter(x=>x.council_chair).length,1);
assert.equal(tabs[2].member_id,"chair");
assert.equal(tabs[1].member_id,"member-2");
assert.equal(result.tabs_created,true);

const queued=[
 {id:"agenda-1",session_id:"session-1",member_id:"chair",stage:"agenda",after_round:0,
  status:"awaiting_input",prompt_text:"Chair: create research agenda",created_at:1},
 {id:"wrong-session",session_id:"alien",member_id:"chair",status:"awaiting_input",created_at:900}
];
vm.runInContext("a42BindChairQueue",ctx)(queued);
assert.equal(tabs[2].chair_turn_id,"agenda-1");
assert.equal(tabs[0].chair_turn_id,undefined);
ctx.a42ChairTurns=queued;
const get=vm.runInContext("a42ChairActive",ctx);
assert.equal(get(tabs[2])?.id,"agenda-1");
async function test(){
 drafts.set(tabs[2].id,{response:"Research agenda: hypotheses and independent criteria"});
 await vm.runInContext("a42ChairSubmit",ctx)(tabs[2],queued[0]);
 assert.equal(responses[0].path,"/v1/manual-web/chair-turns/agenda-1/submit");
 assert.equal(responses[0].body.response_text,"Research agenda: hypotheses and independent criteria");
 assert.equal(drafts.has(tabs[2].id),false);
 await vm.runInContext("a42ChairApprove",ctx)(tabs[2],{id:"agenda-1",status:"awaiting_approval",response_text:"Research agenda"});
 assert.equal(responses[1].path,"/v1/manual-web/chair-turns/agenda-1/approve");
 assert.equal(responses[1].body.approved_text,"operator edited Chair agenda");
 assert.equal(responses.some(r=>r.path.includes("/inference/")),false);
 const after=[{id:"agenda-1",session_id:"session-1",member_id:"chair",status:"approved",created_at:1},
              {id:"review-1",session_id:"session-1",member_id:"chair",stage:"review",status:"awaiting_input",created_at:2}];
 vm.runInContext("a42BindChairQueue",ctx)(after);
 assert.equal(tabs[2].chair_turn_id,"review-1");
 assert.equal(tabs[0].member_id,"member-1");
 assert.ok(persistCalls>0);
 console.log("Council Chair config, synthesis choice, session-isolated Chair tabs, agenda submission, human approval and review handoff: PASS");
}
test().catch(e=>{console.error(e);process.exitCode=1});
