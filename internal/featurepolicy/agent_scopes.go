package featurepolicy

import (
 "context"
 "fmt"
 "strings"
)

// forAgentWithReader resolves separately versioned Agent restrictions from
// the canonical Task owner. The Agent may narrow but can never grant more
// than its Project and selected Workspace allow. Missing/archived/foreign
// custom Agents are denied rather than falling back to the default profile.
// An existing custom Agent without a policy head retains inherited rights.
func forAgentWithReader(ctx context.Context,reader scopedRowReader,tenantID,profileID string)(Policy,bool,error){
 if reader==nil||strings.TrimSpace(tenantID)==""||strings.TrimSpace(profileID)==""{
  return Policy{},false,fmt.Errorf("Agent scope unavailable")
 }
 var owner,status string
 err:=reader.QueryRowContext(ctx,`SELECT workspace_id,status FROM agent_profiles
  WHERE id=? AND workspace_id=?`,profileID,tenantID).Scan(&owner,&status)
 if err!=nil||status!="active"||owner!=tenantID{
  return Policy{},false,fmt.Errorf("pinned Agent profile unavailable, inactive or outside Task Workspace")
 }
 var document string
 var revision int64
 err=reader.QueryRowContext(ctx,`SELECT r.policy_json,r.revision
  FROM agent_policy_heads h
  JOIN agent_policy_revisions r ON r.profile_id=h.profile_id AND r.revision=h.revision
  WHERE h.profile_id=? AND r.workspace_id=?`,profileID,tenantID).Scan(&document,&revision)
 if err!=nil{
  // A legacy Agent with no head inherits Project restrictions. A dangling
  // head is a data-integrity failure and is never treated as unrestricted.
  var headCount int
  if e:=reader.QueryRowContext(ctx,`SELECT COUNT(*) FROM agent_policy_heads WHERE profile_id=?`,profileID).Scan(&headCount);e!=nil{return Policy{},false,e}
  if headCount!=0{return Policy{},false,fmt.Errorf("current Agent policy revision unavailable")}
  return Policy{},false,nil
 }
 if revision<1{return Policy{},false,fmt.Errorf("invalid Agent policy revision")}
 p,configured,err:=parseFeatureDocument([]byte(document),Policy{})
 if err!=nil{return Policy{},false,fmt.Errorf("invalid Agent policy revision: %w",err)}
 if !configured{return Policy{},false,fmt.Errorf("Agent policy head missing feature_policy")}
 return p,true,nil
}
