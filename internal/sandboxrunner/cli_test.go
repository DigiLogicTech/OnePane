package sandboxrunner

import (
	"os"
	"strings"
	"testing"
)

func TestSpecHashNeverDependsOnSecretPlaintext(t *testing.T) {
	base := ContainerSpec{RuntimeID: "r", ApplicationID: "a", Image: "example/app:1", WorkspacePath: "/tmp/ws", Environment: map[string]string{"API_KEY": "secret-one"}, EnvironmentIdentity: map[string]string{"API_KEY": "vault:secret:v2"}, Limits: ResourceLimits{CPUMillis: 1000, MemoryMB: 512, PIDs: 64}}
	h1 := specHash(base)
	base.Environment["API_KEY"] = "secret-two"
	h2 := specHash(base)
	if h1 != h2 {
		t.Fatalf("plaintext changed spec hash: %s != %s", h1, h2)
	}
	base.EnvironmentIdentity["API_KEY"] = "vault:secret:v3"
	if h3 := specHash(base); h3 == h2 {
		t.Fatal("secret version identity did not change spec hash")
	}
}

func TestEnvFileIsPrivateAndContainsNoCLIEncoding(t *testing.T) {
	path, err := writeEnvFile(map[string]string{"API_KEY": "top-secret", "MODE": "prod"})
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(path)
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm()&0o077 != 0 {
		t.Fatalf("env file permissions=%o", st.Mode().Perm())
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(b)
	if !strings.Contains(text, "API_KEY=top-secret\n") || !strings.Contains(text, "MODE=prod\n") {
		t.Fatalf("unexpected env file: %q", text)
	}
}

func TestEnvFileRejectsMultilineValues(t *testing.T) {
	if _, err := writeEnvFile(map[string]string{"PRIVATE_KEY": "line1\nline2"}); err == nil {
		t.Fatal("expected multiline environment secret rejection")
	}
}

func TestRuntimeNetworkNameIsDeterministicAndBounded(t *testing.T) {
	name := runtimeNetworkName(strings.Repeat("Runtime.With Spaces!", 10))
	if name == "" || len(name) > 63 || name != runtimeNetworkName(strings.Repeat("Runtime.With Spaces!", 10)) {
		t.Fatalf("network name=%q", name)
	}
}
