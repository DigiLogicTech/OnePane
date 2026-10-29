package artifact

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"math"
	"os"
	"testing"
)

type countedArtifactReader struct {
	bytes int
	limit int
}

func (r *countedArtifactReader) Read(p []byte) (int, error) {
	if r.bytes >= r.limit {
		return 0, errors.New("verification read beyond immutable size budget")
	}
	for i := range p {
		p[i] = 'x'
	}
	r.bytes += len(p)
	return len(p), nil
}
func TestArtifactVerificationStopsAfterExpectedSizePlusOne(t *testing.T) {
	for _, size := range []int64{0, 1, 4096} {
		r := &countedArtifactReader{limit: int(size + 1)}
		if err := verifyBoundedContent(context.Background(), r, "sha256:wrong", size); !errors.Is(err, ErrCorruptContent) {
			t.Fatalf("unexpected bounded verification %v", err)
		}
		if r.bytes != int(size+1) {
			t.Fatalf("read %d expected <=%d", r.bytes, size+1)
		}
	}
	for _, size := range []int64{-1, math.MaxInt64} {
		r := &countedArtifactReader{}
		if err := verifyBoundedContent(context.Background(), r, "bad", size); !errors.Is(err, ErrCorruptContent) || r.bytes != 0 {
			t.Fatalf("invalid size read content %d %v", r.bytes, err)
		}
	}
	body := []byte("bounded content")
	sum := sha256.Sum256(body)
	hash := "sha256:" + hex.EncodeToString(sum[:])
	if err := verifyBoundedContent(context.Background(), bytes.NewReader(body), hash, int64(len(body))); err != nil {
		t.Fatal(err)
	}
	if err := verifyBoundedContent(context.Background(), bytes.NewReader(body[:3]), hash, int64(len(body))); !errors.Is(err, ErrCorruptContent) {
		t.Fatal("truncated content verified")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := verifyBoundedContent(ctx, bytes.NewReader(body), hash, int64(len(body))); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled verification lost cause: %v", err)
	}
}
func TestArtifactVerificationRejectsAppendedRealBlob(t *testing.T) {
	s, err := NewLocalStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	blob, err := s.Put(context.Background(), bytes.NewReader([]byte("original")))
	if err != nil {
		t.Fatal(err)
	}
	path, err := s.resolve(blob.StorageRef)
	if err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = io.Copy(f, bytes.NewReader(bytes.Repeat([]byte("corrupt"), 1<<16))); err != nil {
		t.Fatal(err)
	}
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}
	if err = s.Verify(context.Background(), blob.StorageRef, blob.ContentHash, blob.SizeBytes); !errors.Is(err, ErrCorruptContent) {
		t.Fatalf("appended real blob accepted %v", err)
	}
}
