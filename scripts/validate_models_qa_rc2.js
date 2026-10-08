const fs=require("node:fs"),vm=require("node:vm"),assert=require("node:assert/strict");
const html=fs.readFileSync("internal/webui/static/index.html","utf8");
const src=fs.readFileSync("internal/webui/static/colibri-registration-dialog.js","utf8");
const modelPath="D:\\OnePane\\Models\\qwen-colibri";
const form={elements:{model_path:{value:"",disabled:false},display_name:{value:""}}};
const buttons={
 "#a44ColibriRegisterCancel":{},"#a44ColibriRegisterSubmit":{disabled:true},
 "#a44ColibriRegisterRefresh":{},"#a44ColibriRegisterStatus":{textContent:"",innerHTML:""},
 "#a44ColibriModelDetails":{textContent:""}
};
let registrationCalls=0,scanCalls=0;
const actions=[],ctx=vm.createContext({
 qa5RegisterColibri:async()=>{},openModal:(title,markup)=>actions.push({title,markup}),
 $:name=>name==="#a44ColibriRegister"?form:buttons[name],
 escapeHtml:x=>String(x),a31Array:x=>Array.isArray(x)?x:[],
 apiRequest:async(path,options)=>{
  if(path.startsWith("/v1/local-ai/colibri/pool-models?")){
   scanCalls++;
   return [{model_path:modelPath,model_ref:"qwen-colibri",display_name:"Qwen Colibri",reported_context_tokens:32768,initial_context_tokens:32768,registered:false},
           {model_path:"D:\\OnePane\\Models\\existing",model_ref:"existing",display_name:"Existing",registered:true}];
  }
  const body=JSON.parse(options.body);
  actions.push({path,body});
  if(path==="/v1/local-ai/colibri/register"){registrationCalls++;return{id:"dep-colibri"}}
  throw Error("unexpected API "+path);
 },
 onepaneWorkspace:"ws",localProfileQA:{id:"hardware"},a31DetectHardware:async()=>{},
 closeModal:()=>{},notice:()=>{},renderModels:async()=>{},
 currentTab:()=>({route:"models"}),
 qa5LoadManagedDeployments:async()=>[{deployment_id:"dep-colibri",model_ref:"qwen-colibri",runtime_name:"colibri"}],
 qa5InspectModel:async()=>{},Number,JSON,Promise,String,encodeURIComponent
});
vm.runInContext(src,ctx);
assert.ok(html.includes('src="/colibri-registration-dialog.js"'));
(async()=>{
 await vm.runInContext("qa5RegisterColibri()",ctx);
 assert.equal(actions[0].title,"Register Colibri model");
 for(const v of ['name="model_path"','name="display_name"',"config.json","Automatic","Agent Check"])
   assert.ok(actions[0].markup.includes(v),"missing "+v);
 assert.ok(!actions[0].markup.includes('name="context_tokens"'),"Context is automatic");
 assert.ok(!actions[0].markup.includes('name="model_ref"'),"Model reference is automatic");
 assert.ok(!src.includes("prompt("),"No native browser prompt");
 assert.ok(scanCalls>0,"Must fetch eligible pool folders from authenticated backend");
 assert.equal(buttons["#a44ColibriRegisterSubmit"].disabled,true,"Must require model selection");
 form.elements.model_path.value=modelPath;
 form.elements.model_path.onchange();
 assert.equal(buttons["#a44ColibriRegisterSubmit"].disabled,false);
 assert.ok(buttons["#a44ColibriModelDetails"].textContent.includes("32,768"));
 await form.onsubmit({preventDefault:()=>{}});
 assert.equal(registrationCalls,1);
 const call=actions.find(x=>x.path==="/v1/local-ai/colibri/register");
 assert.equal(call.body.model_path,modelPath);
 assert.equal(call.body.model_ref,"qwen-colibri");
 assert.equal(call.body.context_tokens,0,"Server derives model's provisional context");
 form.elements.model_path.value="D:\\OnePane\\Models\\existing";
 form.elements.model_path.onchange();
 assert.equal(buttons["#a44ColibriRegisterSubmit"].disabled,true,"Already registered model must be unavailable");
 await form.onsubmit({preventDefault:()=>{}});
 assert.equal(registrationCalls,1,"Already registered model must not be sent again");
 await buttons["#a44ColibriRegisterRefresh"].onclick();
 assert.ok(scanCalls>=2,"Refresh must request new pool inventory");
 console.log("Colibri pool dropdown, auto context, duplicate guard and Agent Check caveat: PASS");
})().catch(e=>{console.error(e);process.exitCode=1});
