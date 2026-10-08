/* Host performance telemetry — only polls while the Metrics drawer is visible. */
let a50Latest=null,a50History=[],a50Loading=false,a50Polling=false;
function a50PrettyBytes(n){if(!Number.isFinite(Number(n)))return "Unavailable";return bytesQA(Number(n))}
function a50Pct(n){return Number.isFinite(Number(n))?Math.max(0,Math.min(100,Number(n))).toFixed(1)+"%":"Unavailable"}
function a50Progress(value,max){
 const n=max>0?Math.max(0,Math.min(100,100*value/max)):0;
 return `<div class="host-meter"><span style="width:${n.toFixed(2)}%"></span></div>`;
}
function a50HistorySVG(key){
 const values=a50History.map(x=>x[key]).filter(x=>Number.isFinite(x));
 if(values.length<2)return '<div class="page-subtitle">Collecting samples…</div>';
 const points=values.map((v,i)=>`${(100*i/(values.length-1)).toFixed(2)},${(28-Math.max(0,Math.min(100,v))*0.26).toFixed(2)}`).join(" ");
 return `<svg class="host-spark" viewBox="0 0 100 30" preserveAspectRatio="none" role="img" aria-label="${escapeHtml(key)} history"><polyline points="${points}" fill="none" stroke="currentColor" stroke-width="1.2" vector-effect="non-scaling-stroke"/></svg>`;
}
function a50MetricsMarkup(x){
 if(!x)return '<div class="widget-body">Collecting host metrics…</div>';
 const cpu=x.cpu_percent,memUsed=Math.max(0,(x.memory_total_bytes||0)-(x.memory_available_bytes||0)),memPct=x.memory_total_bytes?100*memUsed/x.memory_total_bytes:null;
 const diskUsed=Math.max(0,(x.disk_total_bytes||0)-(x.disk_free_bytes||0)),diskPct=x.disk_total_bytes?100*diskUsed/x.disk_total_bytes:null;
 const cards=[
  {name:"CPU",now:cpu===null||cpu===undefined?"Warming up":a50Pct(cpu),note:"Live host utilisation",meter:cpu??0,chart:"cpu"},
  {name:"Memory",now:x.memory_total_bytes?`${a50PrettyBytes(memUsed)} / ${a50PrettyBytes(x.memory_total_bytes)}`:"Unavailable",note:memPct===null?"":a50Pct(memPct)+" in use",meter:memPct??0,chart:"memory"},
  {name:"Storage",now:x.disk_total_bytes?`${a50PrettyBytes(x.disk_free_bytes)} free`:"Unavailable",note:x.disk_path||"System volume",meter:diskPct??0,chart:""},
  {name:"OnePane backend",now:x.process_rss_bytes?a50PrettyBytes(x.process_rss_bytes):"Unavailable",note:`${x.goroutines||0} goroutines · working set`,meter:0,chart:""},
  {name:"Network RX",now:x.network_rx_bytes_per_sec==null?"Unavailable":a50PrettyBytes(x.network_rx_bytes_per_sec)+"/s",note:"Host receive throughput",meter:0,chart:""},
  {name:"Network TX",now:x.network_tx_bytes_per_sec==null?"Unavailable":a50PrettyBytes(x.network_tx_bytes_per_sec)+"/s",note:"Host transmit throughput",meter:0,chart:""}
 ];
 const g=a31Array(x.gpus).map(g=>`<article class="host-metric-tile"><strong>${escapeHtml(g.name)}</strong><div class="host-metric-value">${g.usage_percent==null?"Utilisation unavailable":a50Pct(g.usage_percent)}</div><span class="page-subtitle">${a50PrettyBytes(g.vram_used_bytes)} / ${a50PrettyBytes(g.vram_total_bytes)} VRAM · ${g.temperature_c==null?"Temperature unavailable":g.temperature_c+" °C"}</span>${a50Progress(g.usage_percent??0,100)}</article>`).join("");
 return `<div class="host-metrics"><div class="host-metrics-head"><strong>${escapeHtml(x.hostname||"Local host")} · Live performance</strong><span class="list-meta">5-second samples · ${new Date(x.timestamp).toLocaleTimeString()}</span></div><div class="host-metrics-grid">${cards.map(c=>`<article class="host-metric-tile"><strong>${c.name}</strong><div class="host-metric-value">${c.now}</div><span class="page-subtitle">${c.note}</span>${c.chart?a50HistorySVG(c.chart):a50Progress(c.meter,100)}</article>`).join("")}${g||'<article class="host-metric-tile"><strong>GPU telemetry</strong><div class="page-subtitle">Unavailable (NVIDIA nvidia-smi is optional; integrated GPU usage requires a supported sensor).</div></article>'}</div></div>`;
}
async function a50FetchMetrics(){
 if(a50Loading||activeDrawerTab!=="metrics"||state.drawer!=="open")return;
 a50Loading=true;
 try{
  const x=await apiRequest("/v1/system/metrics?workspace_id="+encodeURIComponent(onepaneWorkspace));
  a50Latest=x;
  const mem=x.memory_total_bytes?100*(x.memory_total_bytes-x.memory_available_bytes)/x.memory_total_bytes:null;
  a50History.push({cpu:x.cpu_percent,memory:mem});
  if(a50History.length>120)a50History.shift();
  const c=document.querySelector("#a50MetricsRoot");if(c)c.innerHTML=a50MetricsMarkup(x);
 }catch(e){const c=document.querySelector("#a50MetricsRoot");if(c)c.innerHTML=`<div class="error">${escapeHtml(e.message)}</div>`}
 finally{a50Loading=false}
}
const a50RenderDrawerBase=renderDrawer;
renderDrawer=function(){
 a50RenderDrawerBase();
 if(activeDrawerTab!=="metrics")return;
 const c=document.querySelector("#drawerContent");if(!c)return;
 c.innerHTML='<div id="a50MetricsRoot"></div>';
 document.querySelector("#a50MetricsRoot").innerHTML=a50MetricsMarkup(a50Latest);
 a50FetchMetrics();
};
setInterval(()=>{if(activeDrawerTab==="metrics"&&state.drawer==="open")a50FetchMetrics()},5000);
