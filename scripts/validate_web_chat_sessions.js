// Browser-independent regression checks for manual Web Chat session isolation.
const assert=require("node:assert/strict");
const fs=require("node:fs");
const vm=require("node:vm");
const code=fs.readFileSync("internal/webui/static/web-chat-session-tabs.js","utf8");
const requests=[];
const opened=[];
const feedback=[];
const state={};
let persists=0;
let renderCalls=0;
const context=vm.createContext({
  state,onepaneWorkspace:"workspace-1",
  Date,Math,Array,Map,String,Number,Set,JSON,
  a39WebTurns:[],
  A39_WEB_PROVIDERS:[
    {id:"chatgpt",name:"ChatGPT",url:"https://chatgpt.com/"},
    {id:"claude",name:"Claude",url:"https://claude.ai/new"}
  ],
  a39WebProviderInfo:id=>[
    {id:"chatgpt",name:"ChatGPT",url:"https://chatgpt.com/"},
    {id:"claude",name:"Claude",url:"https://claude.ai/new"}
  ].find(x=>x.id===id),
  persist:()=>persists++,
  notice:(...args)=>feedback.push(args),
  confirm:()=>true,
  a39WebOpen:url=>opened.push(url),
  apiRequest:async(url,opt)=>{
    requests.push({url,opt});
    if(url.endsWith("/new-conversation"))return {conversation_generation:2};
    return {status:"submitted"};
  },
  renderWebChat:()=>{},
  $:()=>null
});
vm.runInContext(code,context,{filename:"web-chat-session-tabs.js"});
vm.runInContext("renderWebChat=()=>{}",context);
function invoke(source){return vm.runInContext(source,context)}
(async()=>{
  const a=invoke('a40WebNewSession("chatgpt","",true)');
  const b=invoke('a40WebNewSession("chatgpt","",true)');
  const c=invoke('a40WebNewSession("claude","Independent critique",true)');
  assert.notEqual(a.id,b.id,"same provider must create independent conversation tabs");
  assert.equal(a.title,"ChatGPT 1");
  assert.equal(b.title,"ChatGPT 2");
  assert.equal(c.title,"Independent critique");
  assert.equal(invoke("a40WebTabs().length"),3);

  const drafts=invoke("a40WebDrafts");
  drafts.set(a.id,{response:"Answer from ChatGPT 1"});
  drafts.set(b.id,{response:"Different answer from ChatGPT 2"});
  invoke(`a40WebSwitch("${b.id}")`);
  assert.equal(drafts.get(a.id).response,"Answer from ChatGPT 1");
  assert.equal(drafts.get(b.id).response,"Different answer from ChatGPT 2");

  context.a39WebTurns=[
    {turn_id:"turnA",provider_id:"chatgpt",status:"awaiting_input",conversation_generation:1},
    {turn_id:"turnB",provider_id:"chatgpt",status:"awaiting_input",conversation_generation:1}
  ];
  // Switching a tab's assigned handoff must not change its sibling's binding.
  invoke(`a40WebAssignTurn(a40WebTabs()[0],"turnA")`);
  invoke(`a40WebAssignTurn(a40WebTabs()[1],"turnB")`);
  assert.equal(a.turn_id,"turnA");
  assert.equal(b.turn_id,"turnB");
  assert.equal(c.turn_id,"");

  drafts.set(a.id,{response:"This draft should be discarded"});
  drafts.set(b.id,{response:"This one must survive"});
  await invoke('a40WebNewConversation(a40WebTabs()[0], a39WebTurns[0])');
  assert.equal(a.conversation_generation,2);
  assert.equal(opened[0],"https://chatgpt.com/");
  assert.equal(requests[0].url,"/v1/manual-web/turns/turnA/new-conversation");
  assert.equal(JSON.parse(requests[0].opt.body).conversation_generation,1);
  assert.equal(drafts.has(a.id),false,"new conversation must clear its own draft");
  assert.equal(drafts.get(b.id).response,"This one must survive");
  assert.equal(a.turn_id,"turnA","restart must preserve the Council turn");
  assert.equal(b.turn_id,"turnB","restart must preserve other Council turns");

  await invoke('a40WebSubmit(a40WebTabs()[1],a39WebTurns[1])');
  assert.equal(requests[1].url,"/v1/manual-web/turns/turnB/submit");
  assert.equal(JSON.parse(requests[1].opt.body).response_text,"This one must survive");
  assert.equal(drafts.has(b.id),false);
  assert.equal(a.turn_id,"turnA","submitting the other tab must not alter our original Council binding");

  invoke(`a40WebClose("${a.id}")`);
  assert.equal(invoke("a40WebTabs().length"),2);
  assert.equal(b.turn_id,"turnB","closing one tab must not remove another Council turn");
  assert.equal(state.webChatSessions.some(s=>s.id===a.id),false);
  assert.equal(JSON.stringify(state).includes("Answer from ChatGPT"),false,"provider response drafts must never persist in localStorage state");
  assert.ok(persists>0);
  console.log("Manual Web Chat tab isolation, per-provider concurrency, new conversation and handoff submission: PASS");
})().catch(err=>{console.error(err);process.exitCode=1});
