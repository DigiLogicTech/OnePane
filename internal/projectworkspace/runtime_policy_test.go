package projectworkspace

import (
	"encoding/json"
	"testing"
)

func TestNormalizeRuntimeNetworkPolicy(t *testing.T) {
	isolated, err := normalizeRuntimeNetworkPolicy(json.RawMessage(`{"mode":"deny_by_default","ingress":"proxy_only","egress":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	if string(isolated) != `{"mode":"deny_by_default","ingress":"proxy_only","egress":[]}` {
		t.Fatalf("isolated=%s", isolated)
	}

	external, err := normalizeRuntimeNetworkPolicy(json.RawMessage(`{"mode":"external","ingress":"proxy_only","egress":[{"kind":"external"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	var p map[string]any
	if err := json.Unmarshal(external, &p); err != nil {
		t.Fatal(err)
	}
	if p["mode"] != "external" {
		t.Fatalf("external=%s", external)
	}

	if _, err := normalizeRuntimeNetworkPolicy(json.RawMessage(`{"mode":"external","ingress":"host"}`)); err == nil {
		t.Fatal("expected unsafe ingress to fail")
	}
}

func TestNormalizeRuntimeFilesystemPolicyRejectsPrivilegeEscapes(t *testing.T) {
	if _, err := normalizeRuntimeFilesystemPolicy(json.RawMessage(`{"docker_socket":true,"device_passthrough":false,"no_new_privileges":true}`)); err == nil {
		t.Fatal("expected docker socket policy to fail")
	}
	if _, err := normalizeRuntimeFilesystemPolicy(defaultFilesystemPolicy()); err != nil {
		t.Fatal(err)
	}
}
