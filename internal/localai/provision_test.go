package localai

import (
	"archive/tar"
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
)

func makeTar(t *testing.T, name string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "x.tar.gz")
	f, _ := os.Create(p)
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)
	_ = tw.WriteHeader(&tar.Header{Name: name, Mode: 0700, Size: 1, Typeflag: tar.TypeReg})
	_, _ = tw.Write([]byte("x"))
	tw.Close()
	gz.Close()
	f.Close()
	return p
}
func TestExtractRejectsTraversal(t *testing.T) {
	p := makeTar(t, "../../escape")
	if err := ExtractRuntimeArchive(p, "tar.gz", t.TempDir()); err == nil {
		t.Fatal("expected traversal rejection")
	}
}
func TestExtractAllowsNormalFile(t *testing.T) {
	p := makeTar(t, "bin/llama-server")
	root := t.TempDir()
	if err := ExtractRuntimeArchive(p, "tar.gz", root); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "bin", "llama-server")); err != nil {
		t.Fatal(err)
	}
}
