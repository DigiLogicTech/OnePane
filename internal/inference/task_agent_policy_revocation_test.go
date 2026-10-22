package inference

import (
 "context"
 "database/sql"
 "errors"
 "testing"

 "github.com/DigiLogicTech/OnePane/internal/featurepolicy"
 _ "modernc.org/sqlite"
)

// A model lease can wait indefinitely on small hardware. This tests the
// independent Agent policy head, not only edits to agent.md instructions.
func TestTaskAgentPolicyRevisionMustRemainStableAcrossInferenceQueue(t *testing.T){
 ctx:=context.Background()
 db,err:=sql.Open("sqlite",t.TempDir()+"/agent-inference-policy.db")
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 for _,ddl:=range []string{
  `CREATE TABLE tasks(id TEXT PRIMARY KEY,workspace_id TEXT NOT NULL,completion_json TEXT NOT NULL)`,
  `CREATE TABLE agent_profiles(id TEXT PRIMARY KEY,workspace_id TEXT,status TEXT,revision INTEGER)`,
  `CREATE TABLE agent_policy_heads(profile_id TEXT PRIMARY KEY,revision INTEGER NOT NULL)`,
  `CREATE TABLE agent_policy_revisions(profile_id TEXT,workspace_id TEXT,revision INTEGER,PRIMARY KEY(profile_id,revision))`,
  `INSERT INTO tasks VALUES('task','tenant','{"onepane_routing":{"agent_profile":"agent"}}')`,
  `INSERT INTO agent_profiles VALUES('agent','tenant','active',4)`,
 }{
  if _,err:=db.ExecContext(ctx,ddl);err!=nil{t.Fatal(err)}
 }
 svc:=&Service{scopes:featurepolicy.StoredScopes{DB:db}}
 taskID:="task"
 cmd:=ExecuteCommand{TaskID:&taskID,WorkspaceID:"tenant"}
 first,err:=svc.pinnedAgentProfileAtInference(ctx,cmd)
 if err!=nil||first.ProfileID!="agent"||first.Revision!=4||first.PolicyRevision!=0{
  t.Fatalf("legacy custom Agent should bind inherited policy: %+v %v",first,err)
 }
 if err:=svc.revalidateTaskAgentBeforeTransport(ctx,cmd,first);err!=nil{t.Fatal(err)}
 for _,ddl:=range []string{
  `INSERT INTO agent_policy_revisions VALUES('agent','tenant',1)`,
  `INSERT INTO agent_policy_heads VALUES('agent',1)`,
 }{if _,err:=db.ExecContext(ctx,ddl);err!=nil{t.Fatal(err)}}
 if err:=svc.revalidateTaskAgentBeforeTransport(ctx,cmd,first);!errors.Is(err,ErrInformationFlow){
  t.Fatalf("new Agent policy head did not invalidate queued inference: %v",err)
 }
 bound,err:=svc.pinnedAgentProfileAtInference(ctx,cmd)
 if err!=nil||bound.PolicyRevision!=1{t.Fatalf("new Agent policy head not bound: %+v %v",bound,err)}
 if _,err=db.ExecContext(ctx,`INSERT INTO agent_policy_revisions VALUES('agent','tenant',2)`);err!=nil{t.Fatal(err)}
 if _,err=db.ExecContext(ctx,`UPDATE agent_policy_heads SET revision=2 WHERE profile_id='agent'`);err!=nil{t.Fatal(err)}
 if err:=svc.revalidateTaskAgentBeforeTransport(ctx,cmd,bound);!errors.Is(err,ErrInformationFlow){
  t.Fatalf("independent policy revision changed while waiting: %v",err)
 }
 latest,err:=svc.pinnedAgentProfileAtInference(ctx,cmd)
 if err!=nil||latest.PolicyRevision!=2{t.Fatalf("latest policy not resolved %+v %v",latest,err)}
 if _,err=db.ExecContext(ctx,`UPDATE agent_profiles SET revision=5 WHERE id='agent'`);err!=nil{t.Fatal(err)}
 if err:=svc.revalidateTaskAgentBeforeTransport(ctx,cmd,latest);!errors.Is(err,ErrInformationFlow){
  t.Fatalf("prompt revision changed while waiting: %v",err)
 }
 if _,err=db.ExecContext(ctx,`UPDATE agent_profiles SET status='archived' WHERE id='agent'`);err!=nil{t.Fatal(err)}
 if _,err:=svc.pinnedAgentProfileAtInference(ctx,cmd);!errors.Is(err,ErrInformationFlow){
  t.Fatalf("archived Agent was admitted: %v",err)
 }
}

func TestTaskAgentPolicyRequiresValidCanonicalWorkspaceHead(t *testing.T){
 ctx:=context.Background()
 db,err:=sql.Open("sqlite",t.TempDir()+"/agent-inference-foreign.db")
 if err!=nil{t.Fatal(err)}
 defer db.Close()
 for _,ddl:=range []string{
  `CREATE TABLE tasks(id TEXT PRIMARY KEY,workspace_id TEXT,completion_json TEXT)`,
  `CREATE TABLE agent_profiles(id TEXT PRIMARY KEY,workspace_id TEXT,status TEXT,revision INTEGER)`,
  `CREATE TABLE agent_policy_heads(profile_id TEXT PRIMARY KEY,revision INTEGER)`,
  `CREATE TABLE agent_policy_revisions(profile_id TEXT,workspace_id TEXT,revision INTEGER)`,
  `INSERT INTO tasks VALUES('task','tenant','{"onepane_routing":{"agent_profile":"agent"}}')`,
  `INSERT INTO agent_profiles VALUES('agent','foreign','active',1)`,
  `INSERT INTO agent_policy_heads VALUES('agent',1)`,
  `INSERT INTO agent_policy_revisions VALUES('agent','foreign',1)`,
 }{if _,err:=db.ExecContext(ctx,ddl);err!=nil{t.Fatal(err)}}
 taskID:="task";cmd:=ExecuteCommand{TaskID:&taskID,WorkspaceID:"tenant"}
 svc:=&Service{scopes:featurepolicy.StoredScopes{DB:db}}
 if _,err:=svc.pinnedAgentProfileAtInference(ctx,cmd);!errors.Is(err,ErrInformationFlow){
  t.Fatalf("foreign Agent profile admitted: %v",err)
 }
 if _,err:=db.ExecContext(ctx,`UPDATE agent_profiles SET workspace_id='tenant' WHERE id='agent'`);err!=nil{t.Fatal(err)}
 if _,err:=svc.pinnedAgentProfileAtInference(ctx,cmd);!errors.Is(err,ErrInformationFlow){
  t.Fatalf("head linked to foreign Agent policy revision admitted: %v",err)
 }
}
