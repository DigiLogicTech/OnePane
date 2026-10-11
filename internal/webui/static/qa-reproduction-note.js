/* OnePane RC11: opt-in, locally authored QA reproduction note.
 * Operator text is NEVER claimed sanitised: it can include secrets. Nothing
 * is fetched, persisted, attached to support ZIPs, or uploaded automatically.
 */
(function(root){
 "use strict";
 const CATEGORIES=new Set(["ui","workspace","task","model","node","installer","provider","other"]);
 const IMPACTS=new Set(["blocker","high","medium","low"]);
 const MAX_NOTE_BYTES=8192,REVIEW_LIFETIME_MS=120000;
 function clean(value,limit,multiline){
  if(typeof value!=="string"||value.length>limit)throw Error("A report field exceeds its safe length limit");
  const output=value.replace(/\r\n?/g,"\n").replace(/[\u0000-\u0008\u000b-\u001f\u007f]/g,"").trim();
  return multiline?output:output.replace(/\s+/g," ");
 }
 function fields(input){
  const source=input&&typeof input==="object"&&!Array.isArray(input)?input:{};
  const category=CATEGORIES.has(source.category)?source.category:"other";
  const impact=IMPACTS.has(source.impact)?source.impact:"medium";
  const data={
   category,impact,
   title:clean(source.title,120,false),
   steps:clean(source.steps,1600,true),
   expected:clean(source.expected,800,true),
   actual:clean(source.actual,800,true)
  };
  if(!data.title||!data.steps||!data.expected||!data.actual){
   throw Error("A title, reproduction steps, expected and actual outcome are required");
  }
  return data;
 }
 function prepare(input,now){
  if(!Number.isFinite(now)||now<0)throw Error("Invalid review time");
  const data=fields(input);
  // Plain text, never injected as HTML or executable Markdown. User owns
  // notes. Clear warnings; do not silently mask/remove possible secrets.
  const text=[
   "OnePane RC11 — operator-authored QA reproduction report",
   "Privacy: manually supplied text; NOT automatically redacted",
   "Source: local explicit review; no diagnostic payload attached",
   "Subsystem: "+data.category,
   "Impact: "+data.impact,
   "Title: "+data.title,
   "",
   "Steps to reproduce:",data.steps,
   "",
   "Expected result:",data.expected,
   "",
   "Actual result:",data.actual,
   "",
   "Browser/backend trace correlation: not asserted by this report",
   "Supporting evidence: attach a separately reviewed authorised QA ZIP only if appropriate"
  ].join("\n")+"\n";
  if(new TextEncoder().encode(text).length>MAX_NOTE_BYTES)throw Error("Reproduction note is too large");
  return {text,fields:data,reviewedAt:now};
 }
 function stillValid(review,rawFields,preview,now,consent){
  if(!review||consent!==true||!Number.isFinite(now)||
   now<review.reviewedAt||now-review.reviewedAt>REVIEW_LIFETIME_MS||
   preview!==review.text)return false;
  try{return JSON.stringify(fields(rawFields))===JSON.stringify(review.fields)}
  catch(_){return false}
 }
 root.a60QAReproduction={fields,prepare,stillValid};
 if(typeof module==="object"&&module.exports)module.exports={fields,prepare,stillValid};
})(typeof globalThis!=="undefined"?globalThis:this);
