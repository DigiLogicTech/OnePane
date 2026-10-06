package localai

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestBundledCPURuntimeArtifactSmoke(t *testing.T) {
	if os.Getenv("ONEPANE_RUNTIME_ARTIFACT_SMOKE") != "1" {
		t.Skip("set ONEPANE_RUNTIME_ARTIFACT_SMOKE=1 to verify the pinned runtime artifact")
	}
	if runtime.GOARCH != "amd64" || (runtime.GOOS != "windows" && runtime.GOOS != "linux") {
		t.Skipf("runtime artifact smoke unsupported on %s/%s", runtime.GOOS, runtime.GOARCH)
	}

	catalog := BundledArtifactCatalog()
	var selected *RuntimeCatalogEntry
	for i := range catalog.Runtimes {
		entry := catalog.Runtimes[i]
		if strings.EqualFold(entry.Backend, "cpu") && strings.EqualFold(entry.OS, runtime.GOOS) && strings.EqualFold(entry.Architecture, runtime.GOARCH) {
			copy := entry
			selected = &copy
			break
		}
	}
	if selected == nil {
		t.Fatalf("no bundled CPU runtime for %s/%s", runtime.GOOS, runtime.GOARCH)
	}

	root := t.TempDir()
	archive := filepath.Join(root, "runtime-artifact")
	fetcher := NewHTTPFetcher(1 << 30)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	got, err := fetcher.Fetch(ctx, selected.SourceURL, archive, selected.SHA256)
	if err != nil {
		t.Fatalf("fetch bundled runtime: %v", err)
	}
	if !strings.EqualFold(got.SHA256, selected.SHA256) {
		t.Fatalf("runtime digest=%s want=%s", got.SHA256, selected.SHA256)
	}

	installRoot := filepath.Join(root, "runtime")
	if err := ExtractRuntimeArchive(archive, selected.ArchiveFormat, installRoot); err != nil {
		t.Fatalf("extract bundled runtime: %v", err)
	}
	executable := filepath.Join(installRoot, selected.ExecutableRel)
	if runtime.GOOS != "windows" {
		if err := os.Chmod(executable, 0o700); err != nil {
			t.Fatalf("chmod runtime: %v", err)
		}
	}

	runCtx, runCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer runCancel()
	cmd := exec.CommandContext(runCtx, executable, "--version")
	cmd.Dir = filepath.Dir(executable)
	if runtime.GOOS == "linux" {
		cmd.Env = append(os.Environ(), "LD_LIBRARY_PATH="+filepath.Dir(executable))
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("execute managed runtime: %v\n%s", err, string(out))
	}
	if len(strings.TrimSpace(string(out))) == 0 {
		t.Fatal("managed runtime --version returned no output")
	}
	t.Logf("verified %s %s CPU artifact: %s", selected.Name, selected.Version, strings.TrimSpace(string(out)))
}

func TestBundledCUDAEntriesCarryManagedCompanionArchives(t *testing.T) {
	catalog := BundledArtifactCatalog()
	seen := map[string]bool{}
	for _, entry := range catalog.Runtimes {
		if !strings.EqualFold(entry.Backend, "cuda") || entry.Architecture != "amd64" {
			continue
		}
		key := fmt.Sprintf("%s/%s", entry.OS, entry.Architecture)
		seen[key] = true
		if len(entry.Dependencies) == 0 {
			t.Fatalf("%s CUDA runtime has no managed companion archive", key)
		}
		for _, dep := range entry.Dependencies {
			if !strings.HasPrefix(dep.SourceURL, "https://") || len(dep.SHA256) != 64 || (dep.ArchiveFormat != "zip" && dep.ArchiveFormat != "tar.gz") {
				t.Fatalf("%s dependency invalid: %+v", key, dep)
			}
		}
	}
	for _, key := range []string{"windows/amd64", "linux/amd64"} {
		if !seen[key] {
			t.Fatalf("missing bundled CUDA runtime %s", key)
		}
	}
}
