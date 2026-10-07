package api

import (
	"encoding/base64"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/DigiLogicTech/OnePane/internal/localai")

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
	EstimatedTPS float64 `json:"estimated_tps,omitempty"`
	MemoryRequiredGB float64 `json:"memory_required_gb,omitempty"`
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

type discoverySourceStatus struct {
	Status string `json:"status"`
	Message string `json:"message,omitempty"`
	Count int `json:"count"`
}
type discoveryEnvelope struct {
	Models []discoveredModel `json:"models"`
	NextCursor string `json:"next_cursor,omitempty"`
	HasMore bool `json:"has_more"`
	Errors map[string]string `json:"source_errors,omitempty"`
	Sources map[string]discoverySourceStatus `json:"sources,omitempty"`
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


func fetchDiscoveryPage(ctx context.Context, raw string, target any) (http.Header,error) {
    req,err:=http.NewRequestWithContext(ctx,http.MethodGet,raw,nil);if err!=nil{return nil,err}
    req.Header.Set("User-Agent","OnePane/alpha3.2 model-discovery")
    resp,err:=discoveryClient().Do(req);if err!=nil{return nil,err}
    defer resp.Body.Close()
    if resp.StatusCode<200||resp.StatusCode>=300{return nil,fmt.Errorf("upstream HTTP %d",resp.StatusCode)}
    body,err:=io.ReadAll(io.LimitReader(resp.Body,8<<20));if err!=nil{return nil,err}
    if err:=json.Unmarshal(body,target);err!=nil{return nil,fmt.Errorf("unexpected upstream response: %w",err)}
    return resp.Header,nil
}
func nextHFCursor(header http.Header) string {
    for _,link:=range strings.Split(header.Get("Link"),","){
        if !strings.Contains(link,"rel=\"next\""){continue}
        i,j:=strings.Index(link,"<"),strings.Index(link,">")
        if i<0||j<=i{continue}
        u,err:=url.Parse(strings.TrimSpace(link[i+1:j]))
        if err==nil&&u.Scheme=="https"&&u.Hostname()=="huggingface.co"{
            return u.Query().Get("cursor")
        }
    }
    return ""
}
// The Hub returns an opaque next cursor in its Link header. Never construct one
// from repo IDs or assume that one response covers the full Hub inventory.
func discoverHuggingFacePage(ctx context.Context,q string,limit int,cursor,order string)([]discoveredModel,string,error){
    u,_:=url.Parse("https://huggingface.co/api/models")
    v:=u.Query();v.Set("sort",order);v.Set("direction","-1");v.Set("limit",strconv.Itoa(limit))
    if q!=""{v.Set("search",q)}
    if cursor!=""{v.Set("cursor",cursor)}
    u.RawQuery=v.Encode()
    var rows []struct{ID string `json:"id"`;Author string `json:"author"`;Downloads int64 `json:"downloads"`;Likes int64 `json:"likes"`;PipelineTag string `json:"pipeline_tag"`;Tags []string `json:"tags"`;Gated any `json:"gated"`}
    header,err:=fetchDiscoveryPage(ctx,u.String(),&rows);if err!=nil{return nil,"",err}
    out:=make([]discoveredModel,0,len(rows))
    for _,r:=range rows{
        if r.ID==""{continue}
        name:=r.ID;if i:=strings.LastIndex(name,"/");i>=0{name=name[i+1:]}
        reason:="Verify an upstream artifact and resolve a compatible runtime before install."
        trust:="upstream-metadata"
        if hfGated(r.Gated){trust="gated";reason="Access requires approval/authentication from the model publisher."}
        out=append(out,discoveredModel{Source:"huggingface",ID:r.ID,DisplayName:name,Author:r.Author,Category:r.PipelineTag,SourceURL:"https://huggingface.co/"+r.ID,Trust:trust,Verified:false,Downloads:r.Downloads,Likes:r.Likes,License:hfLicense(r.Tags),Tags:r.Tags,Installable:false,InstallReason:reason})
    }
    return out,nextHFCursor(header),nil
}
func discoverHuggingFace(ctx context.Context,q string,limit int)([]discoveredModel,error){
    rows,_,err:=discoverHuggingFacePage(ctx,q,limit,"","downloads");return rows,err
}
// Hugging Bay documents a maximum limit of 500 but no pagination parameter.
// Fetch that complete supported window once, then paginate locally. Do not
// silently imply completeness if the source reports exactly the ceiling.
func discoverHuggingBayPage(ctx context.Context,q string,limit,offset int)([]discoveredModel,string,error){
    u,_:=url.Parse("https://thehuggingbay.io/api/torrents")
    v:=u.Query();v.Set("sort","seeds");v.Set("limit","500")
    if q!=""{v.Set("q",q)}
    u.RawQuery=v.Encode()
    var rows []struct{Infohash string `json:"infohash"`;Name string `json:"name"`;Category string `json:"category"`;SizeBytes int64 `json:"size_bytes"`;Seeds int64 `json:"seeds"`;License string `json:"license"`;SourceURL string `json:"source_url"`;Verified int `json:"verified"`}
    if _,err:=fetchDiscoveryPage(ctx,u.String(),&rows);err!=nil{return nil,"",err}
    if offset>len(rows){offset=len(rows)}
    end:=offset+limit;if end>len(rows){end=len(rows)}
    out:=make([]discoveredModel,0,end-offset)
    for _,r:=range rows[offset:end]{
        trust:="community-unverified";verified:=false
        if r.Verified>=2{trust="captain-verified";verified=true}else if r.Verified==1{trust="community-verified";verified=true}
        out=append(out,discoveredModel{Source:"huggingbay",ID:r.Infohash,DisplayName:r.Name,Category:r.Category,SourceURL:r.SourceURL,Trust:trust,Verified:verified,SizeBytes:r.SizeBytes,Seeds:r.Seeds,License:r.License,Installable:false,InstallReason:"Inspect a manifest and verify its SHA-256 before installing."})
    }
    next:="";if end<len(rows){next=strconv.Itoa(end)}
    return out,next,nil
}
func discoverHuggingBay(ctx context.Context,q string,limit int)([]discoveredModel,error){
    rows,_,err:=discoverHuggingBayPage(ctx,q,limit,0);return rows,err
}

func (s *Server) discoverLLMFit(ctx context.Context,q string,limit int)([]discoveredModel,error){
	if s.localAI==nil{return nil,nil}
	provider,ok:=s.localAI.(interface{ DiscoverLLMFit(context.Context,string,int)([]localai.LLMFitAdvisory,error) });if !ok{return nil,nil}
	rows,err:=provider.DiscoverLLMFit(ctx,q,limit);if err!=nil{return nil,err};out:=make([]discoveredModel,0,len(rows))
	for _,r:=range rows{ctxv:=int64(0);if r.UsableContext!=nil{ctxv=*r.UsableContext}else if r.NativeContext!=nil{ctxv=*r.NativeContext};speed:=float64(0);if r.EstimatedTPS!=nil{speed=*r.EstimatedTPS};memory:=float64(0);if r.MemoryRequiredGB!=nil{memory=*r.MemoryRequiredGB};out=append(out,discoveredModel{EstimatedTPS:speed,MemoryRequiredGB:memory,Source:"llmfit",ID:r.ModelRef,DisplayName:r.ModelRef,Trust:"advisory",Verified:false,License:r.License,FitLevel:r.FitLevel,RunMode:r.RunMode,Quantization:r.BestQuant,Runtime:r.Runtime,ContextTokens:ctxv,Tags:r.Capabilities,Installable:false,InstallReason:"llmfit is an advisory catalogue; OnePane still requires a verified artifact source before install."})}
	return out,nil
}


func (s *Server) discoverLLMFitPage(ctx context.Context,q string,limit,offset int,order string)([]discoveredModel,string,error){
    // llmfit does not document an offset parameter. Request its full fit
    // inventory (up to a transparent safety ceiling) and slice locally.
    const maxLLMFitRows=10000
    rows,err:=s.discoverLLMFit(ctx,q,maxLLMFitRows)
    if err!=nil{return nil,"",err}
    if order=="name"{sort.SliceStable(rows,func(i,j int)bool{return strings.ToLower(rows[i].DisplayName)<strings.ToLower(rows[j].DisplayName)})}
    if order=="memory"{sort.SliceStable(rows,func(i,j int)bool{return rows[i].MemoryRequiredGB<rows[j].MemoryRequiredGB})}
    if order=="speed"{sort.SliceStable(rows,func(i,j int)bool{return rows[i].EstimatedTPS>rows[j].EstimatedTPS})}
    if offset>len(rows){offset=len(rows)}
    end:=offset+limit;if end>len(rows){end=len(rows)}
    next:="";if end<len(rows){next=strconv.Itoa(end)}
    return rows[offset:end],next,nil
}
func discoveryOffset(token string)(int,error){
    if token==""{return 0,nil}
    n,err:=strconv.Atoi(token)
    if err!=nil||n<0||n>1000000{return 0,fmt.Errorf("invalid discovery offset")}
    return n,nil
}
func decodeDiscoveryCursor(raw string)(map[string]string,error){
    state:=map[string]string{}
    if raw==""{return state,nil}
    if len(raw)>8192{return nil,fmt.Errorf("discovery cursor too long")}
    b,err:=base64.RawURLEncoding.DecodeString(raw)
    if err!=nil{return nil,fmt.Errorf("invalid discovery cursor")}
    if err:=json.Unmarshal(b,&state);err!=nil{return nil,fmt.Errorf("invalid discovery cursor")}
    return state,nil
}
func encodeDiscoveryCursor(v map[string]string)string{
    b,_:=json.Marshal(v)
    return base64.RawURLEncoding.EncodeToString(b)
}
func (s *Server) discoverLocalAIModels(w http.ResponseWriter,r *http.Request){
    if _,ok:=s.authenticate(w,r);!ok{return}
    source:=strings.ToLower(strings.TrimSpace(r.URL.Query().Get("source")))
    if source==""{source="all"}
    if source!="all"&&source!="huggingface"&&source!="huggingbay"&&source!="llmfit"{
        writeError(w,http.StatusBadRequest,"unsupported discovery source");return
    }
    q:=strings.TrimSpace(r.URL.Query().Get("q"))
    if len(q)>256{writeError(w,http.StatusBadRequest,"search query too long");return}
    limit,_:=strconv.Atoi(r.URL.Query().Get("limit"))
    if limit<=0{limit=40};if limit>100{limit=100}
    requestedSort:=strings.ToLower(strings.TrimSpace(r.URL.Query().Get("sort")))
    if requestedSort==""{requestedSort="popular"}
    allowed:=map[string]map[string]bool{
      "all":{"popular":true},
      "huggingface":{"popular":true,"trending":true,"downloads":true,"likes":true,"newest":true,"updated":true},
      "huggingbay":{"popular":true,"seeds":true},
      "llmfit":{"popular":true,"fit":true,"speed":true,"memory":true,"name":true},
    }
    if !allowed[source][requestedSort]{writeError(w,http.StatusBadRequest,"sort not supported by this source");return}
    cursor,err:=decodeDiscoveryCursor(r.URL.Query().Get("cursor"))
    if err!=nil{writeError(w,http.StatusBadRequest,err.Error());return}
    // Bind continuation cursors to the source/query/sort. A cursor from a
    // different filter cannot accidentally mix results from two inventories.
    if len(cursor)>0 && (cursor["__source"]!=source||cursor["__query"]!=q||cursor["__sort"]!=requestedSort) {
       writeError(w,http.StatusBadRequest,"discovery cursor does not match active filters");return
    }
    names:=[]string{"huggingface","huggingbay","llmfit"}
    if source!="all"{names=[]string{source}}
    type result struct{name string;rows []discoveredModel;next string;err error}
    ch:=make(chan result,len(names))
    var wg sync.WaitGroup
    for _,name:=range names{
        token,seen:=cursor[name]
        if seen&&token=="-" {continue}
        wg.Add(1)
        go func(name,token string){
            defer wg.Done()
            var rows []discoveredModel;var next string;var err error
            switch name{
            case "huggingface":
                hfSort:="downloads"
                switch requestedSort{case "trending":hfSort="likes7d";case "likes":hfSort="likes";case "newest":hfSort="createdAt";case "updated":hfSort="lastModified"}
                rows,next,err=discoverHuggingFacePage(r.Context(),q,limit,token,hfSort)
            case "huggingbay":
                var offset int;offset,err=discoveryOffset(token)
                if err==nil{rows,next,err=discoverHuggingBayPage(r.Context(),q,limit,offset)}
            case "llmfit":
                var offset int;offset,err=discoveryOffset(token)
                if err==nil{rows,next,err=s.discoverLLMFitPage(r.Context(),q,limit,offset,requestedSort)}
            }
            ch<-result{name:name,rows:rows,next:next,err:err}
        }(name,token)
    }
    go func(){wg.Wait();close(ch)}()
    out:=discoveryEnvelope{Models:[]discoveredModel{},Errors:map[string]string{},Sources:map[string]discoverySourceStatus{}}
    nextState:=map[string]string{}
    for k,v:=range cursor{nextState[k]=v}
    nextState["__source"]=source;nextState["__query"]=q;nextState["__sort"]=requestedSort
    for x:=range ch{
        if x.err!=nil{
            out.Errors[x.name]=x.err.Error()
            out.Sources[x.name]=discoverySourceStatus{Status:"unavailable",Message:x.err.Error()}
            // Don't repeatedly fetch a failed source on Load more. Searching
            // again restarts the source and allows explicit retry.
            nextState[x.name]="-"
            continue
        }
        out.Models=append(out.Models,x.rows...)
        nextState[x.name]="-"
        if x.next!=""{nextState[x.name]=x.next;out.HasMore=true}
        status:="connected";message:="More models available with Load more."
        if len(x.rows)==0{status="connected_empty";message="No matching entries returned by this source."}
        if x.next==""{message="End of the source's published API results."}
        if x.name=="huggingbay"&&x.next==""&&len(x.rows)>=500{message="Reached the Hugging Bay API maximum of 500 listings; this endpoint does not document further pagination."}
        out.Sources[x.name]=discoverySourceStatus{Status:status,Message:message,Count:len(x.rows)}
    }
    if len(out.Errors)==0{out.Errors=nil}
    if out.HasMore{out.NextCursor=encodeDiscoveryCursor(nextState)}
    writeJSON(w,http.StatusOK,out)
}
