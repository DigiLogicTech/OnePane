package webui

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPolicyDecisionReviewerIsPackagedAndLoadedBeforeDebugCentre(t *testing.T) {
	h:=Handler()
	for _,tc:=range []struct{path,marker string}{
        {"/policy-diagnostic-review.js","local_operator_policy_decisions"},
        {"/debug-centre.js","a58DebugCentre"},
        {"/debug-centre.css",".a58-debug-centre"},
    } {
        path:=tc.path
		out:=httptest.NewRecorder()
		h.ServeHTTP(out,httptest.NewRequest(http.MethodGet,path,nil))
		if out.Code!=http.StatusOK||out.Body.Len()==0{
			t.Fatalf("missing packaged diagnostic asset %q: code=%d bytes=%d",path,out.Code,out.Body.Len())
		}
		if !strings.Contains(out.Body.String(),tc.marker) { t.Fatalf("asset %q fell back to unrelated shell",path) }
		if !strings.Contains(out.Header().Get("Cache-Control"),"no-store") {
			t.Fatalf("diagnostic asset %q can be cached across upgrades",path)
		}
	}
	out:=httptest.NewRecorder()
	h.ServeHTTP(out,httptest.NewRequest(http.MethodGet,"/",nil))
	shell:=out.Body.String()
	review:=strings.Index(shell,"/policy-diagnostic-review.js")
	centre:=strings.Index(shell,"/debug-centre.js")
	if review<0||centre<0||review>=centre {
		t.Fatalf("policy evidence reviewer not loaded before Debug Centre: review=%d centre=%d",review,centre)
	}
}
