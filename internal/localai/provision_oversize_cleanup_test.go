package localai

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFetcherOversizedContentLengthRemovesRejectedPartial(t *testing.T) {
	for _, resume := range []bool{false, true} {
		t.Run(fmt.Sprint(resume), func(t *testing.T) {
			server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Length", "1024")
				if resume {
					if r.Header.Get("Range") != "bytes=4-" {
						t.Errorf("resume range=%q", r.Header.Get("Range"))
					}
					w.Header().Set("Content-Range", "bytes 4-1027/1028")
					w.WriteHeader(http.StatusPartialContent)
				}
			}))
			defer server.Close()
			dest := filepath.Join(t.TempDir(), "rejected.bin")
			if resume {
				if err := os.WriteFile(dest+".partial", []byte("part"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			fetcher := NewHTTPFetcher(32)
			fetcher.Client = server.Client()
			_, err := fetcher.Fetch(context.Background(), server.URL, dest, strings.Repeat("a", 64))
			if err == nil || !strings.Contains(err.Error(), "maximum size") {
				t.Fatalf("oversized response not rejected: %v", err)
			}
			for _, file := range []string{dest, dest + ".partial"} {
				if _, err := os.Stat(file); !os.IsNotExist(err) {
					t.Fatalf("rejected download retained %s: %v", filepath.Base(file), err)
				}
			}
		})
	}
}
