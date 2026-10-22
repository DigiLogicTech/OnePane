package featurepolicy

import (
    "reflect"
    "testing"
)

func TestTrustedNodeGrantMustBeExplicitAndExact(t *testing.T){
    cases:=[]struct{name,raw string; want bool; err bool}{
        {"other settings",`{"feature_policy":{"Files":"enabled"}}`,false,false},
        {"routing only",`{"onepane_routing":{"enabled":true}}`,false,false},
        {"explicit",`{"onepane_routing":{"trusted_node_compute":{"enabled":true,"allowed_node_ids":["node-b","node-a"]}}}`,true,false},
        {"disabled",`{"onepane_routing":{"trusted_node_compute":{"enabled":false,"allowed_node_ids":["node-a"]}}}`,false,false},
        {"wildcard",`{"onepane_routing":{"trusted_node_compute":{"enabled":true,"allowed_node_ids":["*"]}}}`,false,true},
        {"unknown field",`{"onepane_routing":{"trusted_node_compute":{"enabled":true,"allowed_node_ids":["node-a"],"unrestricted":true}}}`,false,true},
        {"duplicate",`{"onepane_routing":{"trusted_node_compute":{"enabled":true,"allowed_node_ids":["node-a","node-a"]}}}`,false,true},
        {"empty",`{"onepane_routing":{"trusted_node_compute":{"enabled":true,"allowed_node_ids":[]}}}`,false,true},
        {"string grant",`{"onepane_routing":{"trusted_node_compute":"all"}}`,false,true},
        {"invalid routing",`{"onepane_routing":true}`,false,true},
        {"duplicate root routing",`{"onepane_routing":{},"onepane_routing":{"trusted_node_compute":{"enabled":true,"allowed_node_ids":["node-a"]}}}`,false,true},
        {"duplicate enabled",`{"onepane_routing":{"trusted_node_compute":{"enabled":false,"enabled":true,"allowed_node_ids":["node-a"]}}}`,false,true},
        {"Unicode-alias grant key",`{"onepane_routing":{"trusted_node_compute":{},"trusted_node_comput\u0065":{"enabled":true,"allowed_node_ids":["node-a"]}}}`,false,true},
        {"duplicate unrelated settings",`{"other":{"value":false,"value":true},"onepane_routing":{"trusted_node_compute":{"enabled":true,"allowed_node_ids":["node-a"]}}}`,false,true},
    }
    for _,tc:=range cases{
        t.Run(tc.name,func(t *testing.T){
            g,err:=decodeTrustedNodeGrant(tc.raw)
            if (err!=nil)!=tc.err||g.Enabled!=tc.want{
                t.Fatalf("grant=%+v err=%v, want enabled=%v error=%v",g,err,tc.want,tc.err)
            }
        })
    }
}

func TestTrustedNodeGrantParsingPreservesExactNodeIDs(t *testing.T){
    p,err:=decodeTrustedNodeGrant(`{"onepane_routing":{"trusted_node_compute":{"enabled":true,"allowed_node_ids":["node-x","node-y"]}}}`)
    if err!=nil||!reflect.DeepEqual(p.AllowedNodeIDs,[]string{"node-x","node-y"}){
        t.Fatalf("trust grant changed target IDs: %+v %v",p,err)
    }
}
