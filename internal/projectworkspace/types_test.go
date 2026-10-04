package projectworkspace

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestValidateSafeSpecRejectsHostEscapeControls(t *testing.T) {
	cases := []string{
		`{"privileged":true}`,
		`{"nested":{"host_path":"/etc"}}`,
		`{"docker_socket":true}`,
		`{"devices":["/dev/kvm"]}`,
		`{"host_network":true}`,
	}
	for _, tc := range cases {
		if _, err := validateSafeSpec(json.RawMessage(tc)); !errors.Is(err, ErrUnsafeSandboxSpec) {
			t.Fatalf("expected unsafe sandbox error for %s, got %v", tc, err)
		}
	}
}

func TestValidateSafeSpecAllowsSandboxInternalConfig(t *testing.T) {
	got, err := validateSafeSpec(json.RawMessage(`{"image":"ghcr.io/example/app@sha256:abc","working_dir":"/workspace","command":["./app"]}`))
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(got) {
		t.Fatalf("not json: %s", got)
	}
}

func TestEnvironmentBindingsRequireSecretReferences(t *testing.T) {
	if _, err := validateEnvironmentBindings(json.RawMessage(`{"API_KEY":{"secret_ref":"secret:project/openai"},"MODE":{"literal":"dev"}}`)); err != nil {
		t.Fatalf("valid bindings rejected: %v", err)
	}
	bad := []string{
		`{"API_KEY":"sk-live"}`,
		`{"API_KEY":{"literal":"not-even-a-real-secret"}}`,
		`{"MODE":{"literal":"sk-live-looks-secret"}}`,
		`{"API_KEY":{"secret":"sk-live"}}`,
		`{"API_KEY":{"secret_ref":"sk-live"}}`,
		`{"API_KEY":{"secret_ref":"secret:a","literal":"oops"}}`,
	}
	for _, tc := range bad {
		if _, err := validateEnvironmentBindings(json.RawMessage(tc)); err == nil {
			t.Fatalf("unsafe binding accepted: %s", tc)
		}
	}
}

func TestDefaultPoliciesAreFailClosed(t *testing.T) {
	if string(defaultNetworkPolicy()) != `{"mode":"deny_by_default","ingress":"proxy_only","egress":[]}` {
		t.Fatal("network default changed")
	}
	var fs map[string]any
	if err := json.Unmarshal(defaultFilesystemPolicy(), &fs); err != nil {
		t.Fatal(err)
	}
	if fs["docker_socket"] != false || fs["device_passthrough"] != false || fs["no_new_privileges"] != true {
		t.Fatalf("unsafe filesystem defaults: %#v", fs)
	}
}

func TestApplicationSourcesCannotEscapeToHost(t *testing.T) {
	bad := []struct {
		kind AppSourceKind
		ref  string
	}{
		{AppGit, "file:///etc/passwd"}, {AppGit, "/srv/repo"},
		{AppPackage, "../../host"}, {AppArtifact, "/tmp/app.tar"},
		{AppCompose, "C:\\Users\\me\\compose.yml"}, {AppOCIImage, "file://image"},
	}
	for _, tc := range bad {
		if _, err := validateSourceRef(tc.kind, tc.ref); err == nil {
			t.Fatalf("accepted host source %s %q", tc.kind, tc.ref)
		}
	}
	good := []struct {
		kind AppSourceKind
		ref  string
	}{
		{AppGit, "https://github.com/example/app.git"}, {AppPackage, "@scope/app@1.2.3"},
		{AppArtifact, "artifact:art_123"}, {AppCompose, "artifact:compose_123"},
		{AppOCIImage, "ghcr.io/example/app@sha256:abc"},
	}
	for _, tc := range good {
		if _, err := validateSourceRef(tc.kind, tc.ref); err != nil {
			t.Fatalf("rejected %s %q: %v", tc.kind, tc.ref, err)
		}
	}
}
