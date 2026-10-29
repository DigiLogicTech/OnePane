package releasearchive

import (
	"archive/tar"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestWindowsReleaseArchiveOmitsUbuntuDraft(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	excludes := filepath.Join(root, "packaging", "windows", "release-archive-excludes.txt")
	text, err := os.ReadFile(excludes)
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, line := range strings.Split(string(text), "\n") {
		line = strings.TrimSpace(strings.SplitN(line, "#", 2)[0])
		if line == "" {
			continue
		}
		paths = append(paths, filepath.ToSlash(line))
		full := filepath.Join(root, filepath.FromSlash(line))
		info, err := os.Lstat(full)
		if err != nil {
			t.Fatalf("preserved path missing: %s", line)
		}
		if strings.HasSuffix(line, ".go") {
			body, err := os.ReadFile(full)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(body), "//go:build linux") {
				t.Fatalf("%s is not build-tagged off Windows", line)
			}
		}
		if info.IsDir() {
			continue
		}
	}
	if len(paths) < 5 {
		t.Fatalf("exclude list = %v", paths)
	}
	cmd := exec.Command("bash", filepath.Join(root, "scripts", "export-windows-release-archive.sh"))
	cmd.Dir = root
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(out)
	seen := map[string]bool{}
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		name := filepath.ToSlash(hdr.Name)
		seen[name] = true
		for _, excluded := range paths {
			if name == excluded || strings.HasPrefix(name, excluded+"/") {
				t.Fatalf("Windows archive contains Ubuntu draft path %s", name)
			}
		}
	}
	if err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}
	if !seen["packaging/windows/msi/OnePane.wxs"] {
		t.Fatal("Windows archive dropped the MSI authoring")
	}
}

func TestWindowsBuildDoesNotCompileLinuxStorageDraft(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "list", "-f", "{{.GoFiles}} {{.TestGoFiles}}", "./internal/serviceinstall")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "GOOS=windows", "GOARCH=amd64", "CGO_ENABLED=0")
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	text := string(out)
	for _, name := range []string{"readiness_linux.go", "storage_linux.go", "package_recovery_linux_test.go"} {
		if strings.Contains(text, name) {
			t.Fatalf("Windows file list includes %s: %s", name, text)
		}
	}
}
