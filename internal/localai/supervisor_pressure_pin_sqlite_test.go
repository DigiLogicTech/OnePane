package localai

import (
 "context"
 "database/sql"
 "testing"
 "time"

 _ "modernc.org/sqlite"
)

func TestPressureAdmissionAccountsForColibriPinsWithoutEvictingUnrelatedGPU(t *testing.T){
 db,err:=sql.Open("sqlite",":memory:")
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 db.SetMaxOpenConns(1)
 for _,q:=range []string{
  `CREATE TABLE model_deployments(id TEXT PRIMARY KEY,runtime_config_json TEXT)`,
  `CREATE TABLE managed_local_models(deployment_id TEXT,plan_id TEXT)`,
  `CREATE TABLE local_model_install_plans(id TEXT PRIMARY KEY,memory_required_bytes INTEGER,plan_json TEXT)`,
  `CREATE TABLE local_runtime_instances(deployment_id TEXT PRIMARY KEY,node_id TEXT,status TEXT,last_seen_at INTEGER,started_at INTEGER,updated_at INTEGER)`,
  `INSERT INTO local_model_install_plans VALUES
    ('gpu0',4,'{"placement":{"mode":"single_device","devices":[{"kind":"accelerator","device_id":"gpu0","allocated_bytes":4}]}}'),
    ('gpu1',4,'{"placement":{"mode":"single_device","devices":[{"kind":"accelerator","device_id":"gpu1","allocated_bytes":4}]}}'),
    ('cpu',4,'{"placement":{"mode":"cpu_only","devices":[]}}')`,
  `INSERT INTO model_deployments VALUES ('pinned','{"colibri_pinned":true}'),
     ('gpu-idle','{"colibri_pinned":false}'),('other-gpu','{}'),('cpu-only','{}'),('busy','{}')`,
  `INSERT INTO managed_local_models VALUES ('pinned','gpu0'),('gpu-idle','gpu0'),
     ('other-gpu','gpu1'),('cpu-only','cpu'),('busy','gpu0')`,
  `INSERT INTO local_runtime_instances VALUES ('pinned','node','healthy',1,1,1),
     ('gpu-idle','node','healthy',2,2,2),('other-gpu','node','healthy',3,3,3),
     ('cpu-only','node','healthy',4,4,4),('busy','node','busy',5,5,5)`,
 }{
  if _,err=db.Exec(q);err!=nil{t.Fatalf("fixture %v: %s",err,q)}
 }
 ctx:=context.Background()
 // Use the production resident SQL, by requiring the same fields and join
 // contract as the capacity path in this single-connection fixture.
 rows,err:=db.QueryContext(ctx,`SELECT i.deployment_id,p.memory_required_bytes,p.plan_json,
   COALESCE(i.last_seen_at,i.started_at,i.updated_at),i.status,
   CASE WHEN COALESCE(json_extract(d.runtime_config_json,'$.colibri_pinned'),0)=1 THEN 1 ELSE 0 END
   FROM local_runtime_instances i JOIN model_deployments d ON d.id=i.deployment_id
   JOIN managed_local_models m ON m.deployment_id=i.deployment_id
   JOIN local_model_install_plans p ON p.id=m.plan_id
   WHERE i.node_id='node' AND i.deployment_id<>'incoming'
   AND i.status IN ('healthy','busy') ORDER BY i.last_seen_at ASC`)
 if err!=nil{t.Fatal(err)}
 seen:=map[string]int{}
 for rows.Next(){
  var id,plan,status string
  var bytes,seenAt int64
  var pinned int
  if err=rows.Scan(&id,&bytes,&plan,&seenAt,&status,&pinned);err!=nil{t.Fatal(err)}
  if status=="healthy"&&pinned==0{seen[id]=1}
 }
 if err=rows.Err();err!=nil{t.Fatal(err)}
 if err=rows.Close();err!=nil{t.Fatal(err)}
 if len(seen)!=3||seen["pinned"]!=0||seen["busy"]!=0{
  t.Fatalf("pinned or busy runtime eligible for eviction: %v",seen)
 }
 editCtx,cancel:=context.WithTimeout(ctx,500*time.Millisecond)
 defer cancel()
 if _,err=db.ExecContext(editCtx,`UPDATE local_runtime_instances SET updated_at=9 WHERE deployment_id='pinned'`);err!=nil{
  t.Fatalf("resident cursor held a SQLite connection: %v",err)
 }
 used:=map[string]int64{"gpu0":12,"gpu1":4}
 cap:=map[string]int64{"gpu0":12,"gpu1":12}
 target:=map[string]int64{"gpu0":8}
 idleGPU:=residentRuntime{Placement:PlacementPlan{Devices:[]PlacementDevice{{Kind:"accelerator",DeviceID:"gpu0",AllocatedBytes:4}}}}
 otherGPU:=residentRuntime{Placement:PlacementPlan{Devices:[]PlacementDevice{{Kind:"accelerator",DeviceID:"gpu1",AllocatedBytes:4}}}}
 cpu:=residentRuntime{Placement:PlacementPlan{Mode:PlacementCPUOnly}}
 if !residentReclaimsConstrainedAccelerator(idleGPU,used,target,cap){t.Fatal("pressure model not considered")}
 if residentReclaimsConstrainedAccelerator(otherGPU,used,target,cap)||residentReclaimsConstrainedAccelerator(cpu,used,target,cap){
  t.Fatal("unrelated GPU or CPU model could be evicted")
 }
 if capacityCouldFitAfterIdleReclaim(used,target,cap,[]residentRuntime{idleGPU,otherGPU,cpu},false){
  t.Fatal("pinned and busy GPU claims were incorrectly treated as reclaimable")
 }
}
