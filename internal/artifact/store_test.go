package artifact

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"testing"
)

func TestLocalStoreContentAddressedDedupAndVerify(t *testing.T) {
	store, err := NewLocalStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	content := []byte("immutable evidence\n")
	first, err := store.Put(context.Background(), bytes.NewReader(content))
	if err != nil {
		t.Fatal(err)
	}
	if !first.Created || first.SizeBytes != int64(len(content)) {
		t.Fatalf("first blob=%+v", first)
	}
	second, err := store.Put(context.Background(), bytes.NewReader(content))
	if err != nil {
		t.Fatal(err)
	}
	if second.Created || second.ContentHash != first.ContentHash || second.StorageRef != first.StorageRef {
		t.Fatalf("dedup mismatch first=%+v second=%+v", first, second)
	}
	if err := store.Verify(context.Background(), first.StorageRef, first.ContentHash, first.SizeBytes); err != nil {
		t.Fatal(err)
	}
	r, err := store.Open(context.Background(), first.StorageRef)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	got, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, content) {
		t.Fatalf("content=%q", got)
	}
}

func TestLocalStoreRejectsTraversalAndDetectsCorruption(t *testing.T) {
	root := t.TempDir()
	store, err := NewLocalStore(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Open(context.Background(), "../../etc/passwd"); !errors.Is(err, ErrUnsafeStorageRef) {
		t.Fatalf("traversal err=%v", err)
	}
	blob, err := store.Put(context.Background(), bytes.NewReader([]byte("good")))
	if err != nil {
		t.Fatal(err)
	}
	path, err := store.resolve(blob.StorageRef)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("bad"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := store.Verify(context.Background(), blob.StorageRef, blob.ContentHash, blob.SizeBytes); !errors.Is(err, ErrCorruptContent) {
		t.Fatalf("verify err=%v", err)
	}
}
