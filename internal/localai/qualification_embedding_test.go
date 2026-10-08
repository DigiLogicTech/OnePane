package localai

import "testing"

func TestEmbeddingDimensions(t *testing.T){
 cases:=[]struct{name string;raw string;dims int;ok bool}{
  {"valid",`{"data":[{"embedding":[0.1,-0.2,0.3]}]}`,3,true},
  {"empty",`{"data":[{"embedding":[]}]}`,0,false},
  {"zero",`{"data":[{"embedding":[0,0,0]}]}`,0,false},
  {"missing",`{"data":[]}`,0,false},
  {"bad type",`{"data":[{"embedding":"not a vector"}]}`,0,false},
  {"multiple vectors",`{"data":[{"embedding":[1]},{"embedding":[2]}]}`,0,false},
 }
 for _,c:=range cases{t.Run(c.name,func(t *testing.T){
  dims,err:=embeddingDimensions([]byte(c.raw))
  if (err==nil)!=c.ok||dims!=c.dims{t.Fatalf("got dimensions %d error %v; expected %d, success %v",dims,err,c.dims,c.ok)}
 })}
}
