package inference

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type LocalEndpointResolver interface {
	ResolveLocalEndpoint(context.Context, string) (string, error)
}

type LocalRuntimeActivity interface {
	SetLocalRuntimeBusy(context.Context, string, bool) error
}

type LocalEndpointLeaseResolver interface {
	AcquireLocalEndpoint(context.Context, string) (string, func(), error)
}

type LocalOpenAITransport struct {
	Resolver LocalEndpointResolver
	Client   *http.Client
}

// validatedLoopbackBase accepts an exact HTTP loopback origin with a
// numeric port. A prefix check is unsafe: 127.0.0.1:8080@evil.example
// parses as user-info with an off-host destination. No DNS aliases,
// credentials, path, fragment, or query are permitted here.
func validatedLoopbackBase(raw string)(string,error){
 u,err:=url.Parse(raw)
 if err!=nil||u==nil||u.Scheme!="http"||u.User!=nil||u.Opaque!=""||
   u.RawQuery!=""||u.ForceQuery||u.Fragment!=""||u.RawFragment!=""||
   (u.Path!=""&&u.Path!="/")||u.RawPath!=""{
  return "",fmt.Errorf("unsafe local runtime endpoint")
 }
 host:=u.Hostname()
 if host!="127.0.0.1"&&host!="::1"{
  return "",fmt.Errorf("local runtime endpoint must use numeric loopback")
 }
 rawPort:=u.Port()
 port,err:=strconv.Atoi(rawPort)
 if err!=nil||port<1||port>65535{
  return "",fmt.Errorf("local runtime endpoint needs a valid TCP port")
 }
 canonical:=(&url.URL{Scheme:"http",Host:net.JoinHostPort(host,strconv.Itoa(port))}).String()
 return canonical,nil
}

func (t LocalOpenAITransport) Dispatch(ctx context.Context, req DispatchRequest, _ SecretResolver) (DispatchResult, error) {
	if t.Resolver == nil {
		return DispatchResult{}, &TransportError{Code: "local_runtime_unavailable", OutcomeKnown: true}
	}
	var base string
	var release func()
	var err error
	if leased, ok := t.Resolver.(LocalEndpointLeaseResolver); ok {
		base, release, err = leased.AcquireLocalEndpoint(ctx, req.Deployment.ID)
		if release != nil {
			defer release()
		}
	} else {
		base, err = t.Resolver.ResolveLocalEndpoint(ctx, req.Deployment.ID)
		if err == nil {
			if activity, ok := t.Resolver.(LocalRuntimeActivity); ok {
				if err = activity.SetLocalRuntimeBusy(ctx, req.Deployment.ID, true); err == nil {
					defer func() { _ = activity.SetLocalRuntimeBusy(context.Background(), req.Deployment.ID, false) }()
				}
			}
		}
	}
	if err != nil {
		return DispatchResult{}, &TransportError{Code: "local_runtime_unavailable", OutcomeKnown: true, Err: err}
	}
	// Resolve the *parsed* target origin before constructing a request.
	// Never forward the model prompt to a user-info substituted remote host.
	base,err=validatedLoopbackBase(base)
	if err!=nil{
		return DispatchResult{}, &TransportError{Code:"unsafe_local_endpoint",OutcomeKnown:true,Err:err}
	}
	var bodyObj map[string]any
	if err := json.Unmarshal(req.RequestJSON, &bodyObj); err != nil {
		return DispatchResult{}, &TransportError{Code: "invalid_request_json", OutcomeKnown: true, Err: err}
	}
	bodyObj["model"] = req.Model.ModelRef
	bodyObj["stream"] = false
	body, err := json.Marshal(bodyObj)
	if err != nil {
		return DispatchResult{}, &TransportError{Code: "encode_request", OutcomeKnown: true, Err: err}
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(base, "/")+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return DispatchResult{}, &TransportError{Code: "build_request", OutcomeKnown: true, Err: err}
	}
	httpReq.Header.Set("Content-Type", "application/json")
	client := t.Client
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Minute}
	}
	clone := *client
	// Default proxy environment variables must not redirect private model
	// prompts out of process, even if the destination URL is loopback.
	if clone.Transport==nil {
		baseTransport:=http.DefaultTransport.(*http.Transport).Clone()
		baseTransport.Proxy=nil
		clone.Transport=baseTransport
	}
	clone.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := clone.Do(httpReq)
	if err != nil {
		return DispatchResult{}, &TransportError{Code: "local_runtime_network_error", OutcomeKnown: false, Err: err}
	}
	defer resp.Body.Close()
	const maxResponse = 32 << 20
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponse+1))
	if err != nil {
		return DispatchResult{}, &TransportError{Code: "read_response", OutcomeKnown: false, Err: err}
	}
	if len(data) > maxResponse {
		return DispatchResult{}, &TransportError{Code: "response_too_large", HTTPStatus: resp.StatusCode, OutcomeKnown: true}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Read only the bounded error summary from this host-local runtime.
  // Avoid returning arbitrary HTML or any model-generated content in errors.
  detail := ""
  var remote struct {Error struct {Message string `json:"message"`} `json:"error"`}
  if json.Unmarshal(data,&remote)==nil {detail=strings.TrimSpace(remote.Error.Message)}
  if len(detail)>320 {detail=detail[:320]}
  return DispatchResult{}, &TransportError{Code: fmt.Sprintf("http_%d", resp.StatusCode), HTTPStatus: resp.StatusCode, OutcomeKnown: true, Err: fmt.Errorf("local runtime rejected inference (HTTP %d): %s", resp.StatusCode, detail)}
	}
	if !json.Valid(data) {
		return DispatchResult{}, &TransportError{Code: "invalid_json_response", HTTPStatus: resp.StatusCode, OutcomeKnown: true}
	}
	var envelope struct {
		Usage json.RawMessage `json:"usage"`
	}
	_ = json.Unmarshal(data, &envelope)
	return DispatchResult{ResponseJSON: append(json.RawMessage(nil), data...), UsageJSON: envelope.Usage}, nil
}
