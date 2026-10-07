package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

type discoveredModel struct {
	Source string `json:"source"`
	ID string `json:"id"`
	DisplayName string `json:"display_name"`
	Author string `json:"author,omitempty"`
	Category string `json:"category,omitempty"`
	SourceURL string `json:"source_url,omitempty"`
	Trust string `json:"trust"`
	Verified bool `json:"verified"`
	Downloads int64 `json:"downloads,omitempty"`
	Likes int64 `json:"likes,omitempty"`
	SizeBytes int64 `json:"size_bytes,omitempty"`
	Seeds int64 `json:"seeds,omitempty"`
	License string `json:"license,omitempty"`
	FitLevel string `json:"fit_level,omitempty"`
	RunMode string `json:"run_mode,omitempty"`
	Quantization string `json:"quantization,omitempty"`
	Runtime string `json:"runtime,omitempty"`
	ContextTokens int64 `json:"context_tokens,omitempty"`
	Tags []string `json:"tags,omitempty"`
	Installable bool `json:"installable"`
	InstallReason string `json:"install_reason,omitempty"`
}

type discoveryEnvelope struct {
	Models []discoveredModel `json:"models"`
	Errors map[string]string `json:"source_errors,omitempty"`
}

func discoveryClient() *http.Client {
	return &http.Client{Timeout:6*time.Second,CheckRedirect:func(req *http.Request,via []*http.Request)error{
		if len(via)>=5{return fmt.Errorf("too many redirects")}
		if req.URL.Scheme!="https"{return fmt.Errorf("non-HTTPS discovery redirect refused")}
		return nil
	}}
}

func fetchDiscoveryJSON(ctx context.Context, raw string, target any) error {
	req,err:=http.NewRequestWithContext(ctx,http.MethodGet,raw,nil);if err!=nil{return err}
	req.Header.Set("User-Agent","OnePane/alpha3.2 model-discovery")
	resp,err:=discoveryClient().Do(req);if err!=nil{return err};defer resp.Body.Close()
	if resp.StatusCode<200||resp.StatusCode>=300{return fmt.Errorf("HTTP %d",resp.StatusCode)}
	body,err:=io.ReadAll(io.LimitReader(resp.Body,8<<20));if err!=nil{return err}
	return json.Unmarshal(body,target)
}

func hfGated(v any) bool {
	switch x:=v.(type){case bool:return x;case string:return strings.TrimSpace(x)!=""&&x!="false"}
	return false
}

func hfLicense(tags []string) string {
	for _,t:=range tags{if strings.HasPrefix(strings.ToLower(t),"license:"){return strings.TrimSpace(strings.TrimPrefix(t,"license:"))}}
	return ""
}

func discoverHuggingFace(ctx context.Context,q string,limit int)([]discoveredModel,error){
	u,_:=url.Parse("https://huggingface.co/api/models");v:=u.Query();v.Set("filter","gguf");v.Set("sort","downloads");v.Set("direction","-1");v.Set("limit",strconv.Itoa(limit));if q!=""{v.Set("search",q)};u.RawQuery=v.Encode()
	var rows []struct{ID string `json:"id"`;Author string `json:"author"`;Downloads int64 `json:"downloads"`;Likes int64 `json:"likes"`;LastModified string `json:"lastModified"`;PipelineTag string `json:"pipeline_tag"`;Tags []string `json:"tags"`;Gated any `json:"gated"`}
	if err:=fetchDiscoveryJSON(ctx,u.String(),&rows);err!=nil{return nil,err}
	out:=make([]discoveredModel,0,len(rows))
	for _,r:=range rows{if r.ID==""||hfGated(r.Gated){continue};name:=r.ID;if i:=strings.LastIndex(name,"/");i>=0{name=name[i+1:]}
		out=append(out,discoveredModel{Source:"huggingface",ID:r.ID,DisplayName:name,Author:r.Author,Category:r.PipelineTag,SourceURL:"https://huggingface.co/"+r.ID,Trust:"upstream-metadata",Verified:false,Downloads:r.Downloads,Likes:r.Likes,License:hfLicense(r.Tags),Tags:r.Tags,Installable:false,InstallReason:"Inspect and pin a GGUF artifact digest before OnePane can install this external model."})}
	return out,nil
}

func discoverHuggingBay(ctx context.Context,q string,limit int)([]discoveredModel,error){
	u,_:=url.Parse("https://thehuggingbay.io/api/torrents");v:=u.Query();v.Set("cat","llm");v.Set("sort","seeds");v.Set("limit",strconv.Itoa(limit));if q!=""{v.Set("q",q)};u.RawQuery=v.Encode()
	var rows []struct{Infohash string `json:"infohash"`;Name string `json:"name"`;Category string `json:"category"`;SizeBytes int64 `json:"size_bytes"`;Seeds int64 `json:"seeds"`;License string `json:"license"`;SourceURL string `json:"source_url"`;Verified int `json:"verified"`}
	if err:=fetchDiscoveryJSON(ctx,u.String(),&rows);err!=nil{return nil,err}
	out:=make([]discoveredModel,0,len(rows))
	for _,r:=range rows{trust:="community-unverified";verified:=false;if r.Verified>=2{trust="captain-verified";verified=true}else if r.Verified==1{trust="community-verified";verified=true};out=append(out,discoveredModel{Source:"huggingbay",ID:r.Infohash,DisplayName:r.Name,Category:r.Category,SourceURL:r.SourceURL,Trust:trust,Verified:verified,SizeBytes:r.SizeBytes,Seeds:r.Seeds,License:r.License,Installable:false,InstallReason:"Torrent/webseed installation is not yet registered as a managed OnePane artifact."})}
	return out,nil
}

func (s *Server) discoverLLMFit(ctx context.Context,q string,limit int)([]discoveredModel,error){
	if s.localAI==nil{return nil,nil};rows,err:=s.localAI.DiscoverLLMFit(ctx,q,limit);if err!=nil{return nil,err};out:=make([]discoveredModel,0,len(rows))
	for _,r:=range rows{ctxv:=int64(0);if r.UsableContext!=nil{ctxv=*r.UsableContext}else if r.NativeContext!=nil{ctxv=*r.NativeContext};out=append(out,discoveredModel{Source:"llmfit",ID:r.ModelRef,DisplayName:r.ModelRef,Trust:"advisory",Verified:false,License:r.License,FitLevel:r.FitLevel,RunMode:r.RunMode,Quantization:r.BestQuant,Runtime:r.Runtime,ContextTokens:ctxv,Tags:r.Capabilities,Installable:false,InstallReason:"llmfit is an advisory catalogue; OnePane still requires a verified artifact source before install."})}
	return out,nil
}

func (s *Server) discoverLocalAIModels(w http.ResponseWriter,r *http.Request){
	if _,ok:=s.authenticate(w,r);!ok{return}
	source:=strings.ToLower(strings.TrimSpace(r.URL.Query().Get("source")));if source==""{source="all"}
	q:=strings.TrimSpace(r.URL.Query().Get("q"));limit,_:=strconv.Atoi(r.URL.Query().Get("limit"));if limit<=0{limit=30};if limit>60{limit=60}
	wanted:=map[string]bool{};if source=="all"{wanted["huggingface"]=true;wanted["huggingbay"]=true;wanted["llmfit"]=true}else{wanted[source]=true}
	type result struct{name string;rows []discoveredModel;err error};ch:=make(chan result,3);var wg sync.WaitGroup
	launch:=func(name string,fn func()( []discoveredModel,error)){wg.Add(1);go func(){defer wg.Done();rows,err:=fn();ch<-result{name:name,rows:rows,err:err}}()}
	if wanted["huggingface"]{launch("huggingface",func()([]discoveredModel,error){return discoverHuggingFace(r.Context(),q,limit)})}
	if wanted["huggingbay"]{launch("huggingbay",func()([]discoveredModel,error){return discoverHuggingBay(r.Context(),q,limit)})}
	if wanted["llmfit"]{launch("llmfit",func()([]discoveredModel,error){return s.discoverLLMFit(r.Context(),q,limit)})}
	go func(){wg.Wait();close(ch)}()
	out:=discoveryEnvelope{Models:[]discoveredModel{},Errors:map[string]string{}}
	for x:=range ch{if x.err!=nil{out.Errors[x.name]=x.err.Error();continue};out.Models=append(out.Models,x.rows...)}
	if len(out.Errors)==0{out.Errors=nil};writeJSON(w,http.StatusOK,out)
}
