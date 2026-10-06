package localai

import (
	"encoding/json"
	"testing"
)

func TestContextStepsIncludesRequestedMaximum(t *testing.T) {
	got := contextSteps(12000)
	want := []int64{2048, 4096, 8192, 12000}
	if len(got) != len(want) {
		t.Fatalf("steps=%v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("steps=%v", got)
		}
	}
}

func TestProtocolResponseParsers(t *testing.T) {
	plain := json.RawMessage(`{"choices":[{"message":{"content":"OK"}}]}`)
	if !hasAssistantContent(plain) {
		t.Fatal("plain response not recognized")
	}
	js := json.RawMessage(`{"choices":[{"message":{"content":"{\"ok\":true}"}}]}`)
	if !assistantJSONHasOK(js) {
		t.Fatal("json response not recognized")
	}
	tool := json.RawMessage(`{"choices":[{"message":{"tool_calls":[{"function":{"name":"qualification_ping"}}]}}]}`)
	if !hasToolCall(tool, "qualification_ping") {
		t.Fatal("tool call not recognized")
	}
}

func TestArgsContainPair(t *testing.T) {
	args := []string{"llama-server", "-m", "/models/a.gguf", "--host", "127.0.0.1", "--port", "8081"}
	if !argsContainPair(args, "-m", "/models/a.gguf") || !argsContainPair(args, "--port", "8081") {
		t.Fatal("expected identity pair")
	}
	if argsContainPair(args, "--port", "8082") {
		t.Fatal("accepted wrong runtime identity")
	}
}
