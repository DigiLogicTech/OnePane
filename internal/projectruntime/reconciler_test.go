package projectruntime

import (
	"encoding/json"
	"testing"
)

func TestPostconditionsRequireRootlessAndIsolation(t *testing.T) {
	good := json.RawMessage(`{"container":{"status":"running","isolation_verified":true},"engine":{"kind":"podman","rootless":true}}`)
	if !appRunningIsolated(good) {
		t.Fatal("expected verified isolated running app")
	}
	for _, bad := range []json.RawMessage{
		json.RawMessage(`{"container":{"status":"running","isolation_verified":false},"engine":{"rootless":true}}`),
		json.RawMessage(`{"container":{"status":"running","isolation_verified":true},"engine":{"rootless":false}}`),
		json.RawMessage(`{"container":{"status":"stopped","isolation_verified":true},"engine":{"rootless":true}}`),
	} {
		if appRunningIsolated(bad) {
			t.Fatalf("unsafe postcondition accepted: %s", bad)
		}
	}
}

func TestRuntimeStoppedRequiresEveryContainerInactive(t *testing.T) {
	if !runtimeStopped(json.RawMessage(`{"containers":[{"status":"stopped"},{"status":"absent"}],"engine":{"rootless":true}}`)) {
		t.Fatal("stopped runtime rejected")
	}
	if runtimeStopped(json.RawMessage(`{"containers":[{"status":"running"}],"engine":{"rootless":true}}`)) {
		t.Fatal("running container accepted")
	}
}

func TestImagePresentRequiresExactReference(t *testing.T) {
	raw := json.RawMessage(`{"image":{"reference":"ghcr.io/acme/app@sha256:abc","digests":["ghcr.io/acme/app@sha256:abc"]},"engine":{"rootless":true}}`)
	if !imagePresent(raw, "ghcr.io/acme/app@sha256:abc") {
		t.Fatal("image not recognized")
	}
	if imagePresent(raw, "ghcr.io/acme/app:latest") {
		t.Fatal("different image reference accepted")
	}
}
