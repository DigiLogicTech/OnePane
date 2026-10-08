const fs=require("node:fs"),vm=require("node:vm"),assert=require("node:assert/strict");
const html=fs.readFileSync("internal/webui/static/index.html","utf8");
const src=fs.readFileSync("internal/webui/static/colibri-registration-dialog.js","utf8");
const form={elements:{
 model_path:{value:"",addEventListener:()=>{}},model_ref:{value:""},display_name:{value:""},
 context_tokens:{value:"8192"}
}};
const buttons={"#a44ColibriRegisterCancel":{},"#a44ColibriRegisterSubmit":{disabled:false},
 "#a44ColibriBrowse":{disabled:false},"#a44ColibriRegisterStatus":{textContent:"",innerHTML:""}};
const actions=[],ctx=vm.createContext({
 qa5RegisterColibri:async()=>{},openModal:(title,markup)=>actions.push({title,markup}),
 $:name=>name==="#a44ColibriRegister"?form:buttons[name],
 escapeHtml:x=>String(x),apiRequest:async(path,options)=>{
  actions.push({path,body:JSON.parse(options.body)});
  if(path==="/desktop/folder-picker")return{path:"D:\\OnePane\\Models\\qwen-colibri"};
  return{id:"dep-colibri"};
 },onepaneWorkspace:"ws",localProfileQA:{id:"hardware"},
 a31DetectHardware:async()=>{},closeModal:()=>{},notice:()=>{},renderModels:async()=>{},
 currentTab:()=>({route:"models"}),
 qa5LoadManagedDeployments:async()=>[{deployment_id:"dep-colibri",model_ref:"qwen-colibri"}],
 qa5InspectModel:async()=>{},Number,JSON,Promise,String
});
vm.runInContext(src,ctx);
assert.ok(html.includes('src="/colibri-registration-dialog.js"'),"registration dialogue must be loaded by index.html");
(async()=>{
 await vm.runInContext("qa5RegisterColibri()",ctx);
 assert.equal(actions[0].title,"Register Colibri model");
 for(const v of ["name=\"model_path\"","name=\"model_ref\"","name=\"display_name\"","name=\"context_tokens\"","config.json","OnePane model pool"]){
  assert.ok(actions[0].markup.includes(v),"missing useful form field/requirement "+v)
 }
 assert.ok(!src.includes("prompt("),"no native browser prompt in registration");
 await buttons["#a44ColibriBrowse"].onclick();
 assert.equal(form.elements.model_path.value,"D:\\OnePane\\Models\\qwen-colibri");
 assert.equal(form.elements.model_ref.value,"qwen-colibri");
 await form.onsubmit({preventDefault:()=>{}});
 const call=actions.find(x=>x.path==="/v1/local-ai/colibri/register");
 assert.ok(call,"expected managed Colibri registration POST");
 assert.equal(call.body.model_ref,"qwen-colibri");
 assert.equal(call.body.context_tokens,8192);
 form.elements.model_path.value="D:\\OnePane\\Models";
 await form.onsubmit({preventDefault:()=>{}});
 assert.ok(buttons["#a44ColibriRegisterStatus"].innerHTML.includes("Models pool root"),
  "must reject selecting just the model pool root");
 console.log("Theme-aware Colibri registration, managed folder validation and submit contract: PASS");
})().catch(e=>{console.error(e);process.exitCode=1});
