package localai

import (
 "bytes"
 "context"
 "encoding/json"
 "errors"
 "fmt"
 "io"
 "math"
 "net/http"
 "time"
 "github.com/DigiLogicTech/OnePane/internal/inference"
)

// embeddingDimensions validates an actual numeric embedding response rather
// than accepting an HTTP 200 as evidence of model capability.
func embeddingDimensions(raw []byte)(int,error){
 var response struct {
  Data []struct{ Embedding []float64 `json:"embedding"` } `json:"data"`
 }
 if err:=json.Unmarshal(raw,&response);err!=nil{return 0,err}
 if len(response.Data)!=1 {return 0,errors.New("embedding probe did not return one vector")}
 values:=response.Data[0].Embedding
 if len(values)==0||len(values)>65536{return 0,errors.New("embedding vector has invalid dimensions")}
 norm:=float64(0)
 for _,v:=range values {
  if math.IsInf(v,0)||math.IsNaN(v){return 0,errors.New("embedding vector contains non-finite values")}
  norm+=v*v
 }
 if norm<=0{return 0,errors.New("embedding vector is all zeros")}
 return len(values),nil
}

func (q *Qualifier) qualifyEmbedding(ctx context.Context, req QualificationRequest, dep inference.ModelDeployment, model inference.Model, run QualificationRun, port int)(QualificationRun,error){
 if port<1024||port>65535{return run,q.failWithEvidence(ctx,run,"invalid managed embedding service port")}
 payload,_:=json.Marshal(map[string]any{"model":model.ModelRef,"input":"search_query: OnePane embedding qualification.","encoding_format":"float"})
 request,err:=http.NewRequestWithContext(ctx,http.MethodPost,fmt.Sprintf("http://127.0.0.1:%d/v1/embeddings",port),bytes.NewReader(payload))
 if err!=nil{return run,q.failWithEvidence(ctx,run,err.Error())}
 request.Header.Set("Content-Type","application/json")
 client:=&http.Client{Timeout:90*time.Second,CheckRedirect:func(_ *http.Request,_ []*http.Request)error{return http.ErrUseLastResponse}}
 started:=time.Now()
 response,err:=client.Do(request)
 if err!=nil{return run,q.failWithEvidence(ctx,run,"embedding probe connection failed: "+err.Error())}
 defer response.Body.Close()
 body,err:=io.ReadAll(io.LimitReader(response.Body,4<<20))
 if err!=nil{return run,q.failWithEvidence(ctx,run,"embedding probe response unreadable: "+err.Error())}
 if response.StatusCode<200||response.StatusCode>=300{return run,q.failWithEvidence(ctx,run,fmt.Sprintf("embedding probe HTTP %d: %s",response.StatusCode,string(body[:min(len(body),384)])))}
 dimensions,err:=embeddingDimensions(body)
 if err!=nil{return run,q.failWithEvidence(ctx,run,"invalid embedding result: "+err.Error())}
 now:=q.clock.UnixMilli()
 level:="L0"
 run.Status=QualificationPassed
 run.ProtocolLevel=&level
 run.CompletedAt=&now
 run.EvidenceJSON,_=json.Marshal(map[string]any{"embedding_ok":true,"dimensions":dimensions,"probe":"v1/embeddings","model_ref":model.ModelRef})
 run.MetricsJSON,_=json.Marshal(map[string]any{"capability":"inference.embedding","embedding_dimensions":dimensions,"elapsed_ms":time.Since(started).Milliseconds(),"requested_context":req.RequestedContext})
 if err:=q.publish(ctx,run,req,dep,model);err!=nil{return run,q.failWithEvidence(ctx,run,err.Error())}
 return q.Run(ctx,run.ID)
}
