package projectworkspace

import "testing"

func TestGoalSignalTriageClassificationRequiresBothAdverseClassesAndDistinctSources(t *testing.T){
 cases:=[]struct{
  name string
  sources,tcpNegative,nodeNegative int
  seenTCP,seenNode bool
  want string
 }{
  {"none",0,0,0,false,false,"no_recent_measurements"},
  {"only TCP positive",0,0,0,true,false,"single_measurement_class"},
  {"only Node positive",0,0,0,false,true,"single_measurement_class"},
  {"both positive",0,0,0,true,true,"mixed_classes_no_adverse_indicator"},
  {"only TCP negative",1,1,0,true,false,"adverse_indicator_needs_review"},
  {"only Node negative",1,0,1,false,true,"adverse_indicator_needs_review"},
  {"two TCP negatives with healthy Node",2,2,0,true,true,"adverse_indicator_needs_review"},
  {"two Node negatives with healthy TCP",2,0,2,true,true,"adverse_indicator_needs_review"},
  {"both classes negative same source",1,1,1,true,true,"adverse_indicator_needs_review"},
  {"independent source IDs in two adverse classes",2,1,1,true,true,"multiple_adverse_indicators_investigate"},
  {"three adverse sources in two classes",3,2,1,true,true,"multiple_adverse_indicators_investigate"},
 }
 for _,tc:=range cases{
  t.Run(tc.name,func(t *testing.T){
   got:=classifyGoalSignalTriage(tc.sources,tc.tcpNegative,
    tc.nodeNegative,tc.seenTCP,tc.seenNode)
   if got!=tc.want{t.Fatalf("got %q; want %q",got,tc.want)}
  })
 }
}
