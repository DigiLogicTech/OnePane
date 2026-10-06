package localai

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
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

func TestHTTPFetcherResumesPartialDownload(t *testing.T) {
	payload := []byte(strings.Repeat("onepane-resume-", 4096))
	split := len(payload) / 3
	sum := fmt.Sprintf("%x", sha256.Sum256(payload))
	var calls atomic.Int32
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		call := calls.Add(1)
		if call == 1 {
			w.Header().Set("Content-Length", fmt.Sprint(len(payload)))
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(payload[:split])
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			panic(http.ErrAbortHandler)
		}
		wantRange := fmt.Sprintf("bytes=%d-", split)
		if got := r.Header.Get("Range"); got != wantRange {
			t.Errorf("Range=%q want %q", got, wantRange)
		}
		w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", split, len(payload)-1, len(payload)))
		w.Header().Set("Content-Length", fmt.Sprint(len(payload)-split))
		w.WriteHeader(http.StatusPartialContent)
		_, _ = w.Write(payload[split:])
	}))
	defer srv.Close()

	fetcher := &HTTPFetcher{Client: srv.Client(), MaxBytes: int64(len(payload) * 2)}
	dest := filepath.Join(t.TempDir(), "model.gguf")
	if _, err := fetcher.Fetch(context.Background(), srv.URL, dest, sum); err == nil {
		t.Fatal("first interrupted fetch unexpectedly succeeded")
	} else if !isResumableDownloadError(err) {
		t.Fatalf("interrupted transfer must be resumable, got %T: %v", err, err)
	}
	st, err := os.Stat(dest + ".partial")
	if err != nil {
		t.Fatalf("partial download was not preserved: %v", err)
	}
	if st.Size() != int64(split) {
		t.Fatalf("partial size=%d want %d", st.Size(), split)
	}
	got, err := fetcher.Fetch(context.Background(), srv.URL, dest, sum)
	if err != nil {
		t.Fatal(err)
	}
	if got.Size != int64(len(payload)) || got.SHA256 != sum {
		t.Fatalf("unexpected result: %+v", got)
	}
	body, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != string(payload) {
		t.Fatal("resumed artifact does not match source")
	}
	if _, err := os.Stat(dest + ".partial"); !os.IsNotExist(err) {
		t.Fatalf("partial file should be removed after completion, err=%v", err)
	}
}

func TestHTTPFetcherReusesVerifiedCompletedArtifact(t *testing.T) {
	payload := []byte("verified local model artifact")
	sum := fmt.Sprintf("%x", sha256.Sum256(payload))
	dest := filepath.Join(t.TempDir(), "model.gguf")
	if err := os.WriteFile(dest, payload, 0o600); err != nil {
		t.Fatal(err)
	}
	fetcher := &HTTPFetcher{Client: &http.Client{}, MaxBytes: 1024}
	got, err := fetcher.Fetch(context.Background(), "https://invalid.example/not-requested", dest, sum)
	if err != nil {
		t.Fatal(err)
	}
	if got.SHA256 != sum || got.Size != int64(len(payload)) {
		t.Fatalf("unexpected result: %+v", got)
	}
}
func TestHTTPFetcherRestartsWhenRangeIgnored(t *testing.T) {
	payload := []byte(strings.Repeat("range-fallback-", 1024))
	split := len(payload) / 4
	sum := fmt.Sprintf("%x", sha256.Sum256(payload))
	var gotRange string
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotRange = r.Header.Get("Range")
		w.Header().Set("Content-Length", fmt.Sprint(len(payload)))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(payload)
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "model.gguf")
	if err := os.WriteFile(dest+".partial", payload[:split], 0o600); err != nil {
		t.Fatal(err)
	}
	fetcher := &HTTPFetcher{Client: srv.Client(), MaxBytes: int64(len(payload) * 2)}
	got, err := fetcher.Fetch(context.Background(), srv.URL, dest, sum)
	if err != nil {
		t.Fatal(err)
	}
	if gotRange != fmt.Sprintf("bytes=%d-", split) {
		t.Fatalf("Range=%q", gotRange)
	}
	if got.Size != int64(len(payload)) || got.SHA256 != sum {
		t.Fatalf("unexpected result: %+v", got)
	}
	body, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != string(payload) {
		t.Fatal("range fallback duplicated or corrupted bytes")
	}
}

func TestHTTPFetcherPromotesCompletePartialAfter416(t *testing.T) {
	payload := []byte(strings.Repeat("complete-before-rename-", 512))
	sum := fmt.Sprintf("%x", sha256.Sum256(payload))
	var gotRange string
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotRange = r.Header.Get("Range")
		w.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", len(payload)))
		w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "model.gguf")
	if err := os.WriteFile(dest+".partial", payload, 0o600); err != nil {
		t.Fatal(err)
	}
	fetcher := &HTTPFetcher{Client: srv.Client(), MaxBytes: int64(len(payload) * 2)}
	got, err := fetcher.Fetch(context.Background(), srv.URL, dest, sum)
	if err != nil {
		t.Fatal(err)
	}
	if gotRange != fmt.Sprintf("bytes=%d-", len(payload)) {
		t.Fatalf("Range=%q", gotRange)
	}
	if got.Size != int64(len(payload)) || got.SHA256 != sum {
		t.Fatalf("unexpected result: %+v", got)
	}
	if _, err := os.Stat(dest); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dest+".partial"); !os.IsNotExist(err) {
		t.Fatalf("partial should have been promoted, err=%v", err)
	}
}

func TestHTTPFetcherDoesNotResumeUnverifiedPartial(t *testing.T) {
	payload := []byte("new-unverified-object")
	var gotRange string
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotRange = r.Header.Get("Range")
		w.Header().Set("Content-Length", fmt.Sprint(len(payload)))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(payload)
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "artifact.bin")
	if err := os.WriteFile(dest+".partial", []byte("stale-unverified-prefix"), 0o600); err != nil {
		t.Fatal(err)
	}
	fetcher := &HTTPFetcher{Client: srv.Client(), MaxBytes: 1024}
	if _, err := fetcher.Fetch(context.Background(), srv.URL, dest, ""); err != nil {
		t.Fatal(err)
	}
	if gotRange != "" {
		t.Fatalf("unverified partial must restart, got Range=%q", gotRange)
	}
	body, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != string(payload) {
		t.Fatalf("payload=%q", string(body))
	}
}

func TestRuntimeInstallFingerprintIncludesDependencies(t *testing.T) {
	base := RuntimeManifest{
		Name: "llamacpp", Version: "1", Backend: "cuda", OS: "windows", Architecture: "amd64",
		SourceURL: "https://example.invalid/llama.zip",
		SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		ArchiveFormat: "zip", ExecutableRel: "llama-server.exe",
		Dependencies: []RuntimeDependency{{
			Name: "cuda-runtime",
			SourceURL: "https://example.invalid/cudart.zip",
			SHA256: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			ArchiveFormat: "zip",
		}},
	}
	first := runtimeInstallFingerprint(base)
	if len(first) != 64 {
		t.Fatalf("fingerprint=%q", first)
	}
	changed := base
	changed.Dependencies = append([]RuntimeDependency(nil), base.Dependencies...)
	changed.Dependencies[0].SHA256 = "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
	second := runtimeInstallFingerprint(changed)
	if first == second {
		t.Fatal("dependency digest change did not change runtime fingerprint")
	}
}

func TestRuntimeInstallFingerprintIsStable(t *testing.T) {
	runtime := RuntimeManifest{
		Name: "llamacpp", Version: "1", Backend: "cpu", OS: "linux", Architecture: "amd64",
		SourceURL: "https://example.invalid/llama.tar.gz",
		SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		ArchiveFormat: "tar.gz", ExecutableRel: "llama-server",
	}
	if a, b := runtimeInstallFingerprint(runtime), runtimeInstallFingerprint(runtime); a != b {
		t.Fatalf("fingerprint unstable: %s != %s", a, b)
	}
}
