package localai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLLMFitAdvisoryParsingAndProvenance(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/models" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"models":[{"name":"example/model","params_b":7.0,"context_length":131072,"usable_context":65536,"effective_context_length":8192,"fit_level":"good","best_quant":"Q6_K","runtime":"llamacpp","memory_required_gb":8.4,"memory_available_gb":24.0,"disk_size_gb":6.2,"estimated_tps":42.5,"measured_tps":39.25,"prefill_tps":812.3,"ttft_ms":125,"estimate_confidence":"measured_local","capability_ids":["tool_use"],"supports_tp":[1,2,4],"installed":false,"license":"apache-2.0","verify_command":"llama-bench -m model.gguf","estimate_basis":{"method":"roofline"}}]}`))
	}))
	defer ts.Close()
	c, err := NewLLMFitClient(ts.URL)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := c.ModelAdvisories(context.Background(), RecommendRequest{UseCase: UseCoding, Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	a, ok := rows[normalizeModelKey("example/model")]
	if !ok {
		t.Fatalf("missing advisory: %#v", rows)
	}
	if a.Source != "llmfit" || a.BestQuant != "Q6_K" || a.MeasuredTPS == nil || *a.MeasuredTPS != 39.25 || a.UsableContext == nil || *a.UsableContext != 65536 || a.EffectiveContext == nil || *a.EffectiveContext != 8192 || a.DiskSizeGB == nil || *a.DiskSizeGB != 6.2 || len(a.Capabilities) != 1 || len(a.SupportsTP) != 3 || a.TensorParallelSupported == nil || !*a.TensorParallelSupported || a.VerifyCommand == "" || len(a.EstimateBasis) == 0 || len(a.Raw) == 0 {
		t.Fatalf("unexpected advisory: %+v", a)
	}
}

func TestLLMFitRejectsPlaintextRemoteEndpoint(t *testing.T) {
	if _, err := NewLLMFitClient("http://192.0.2.10:8787"); err == nil {
		t.Fatal("expected remote plaintext llmfit endpoint to be rejected")
	}
}
