package artifact

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type Blob struct {
	ContentHash string
	SizeBytes   int64
	StorageRef  string
	Created     bool
}

type BlobStore interface {
	Put(context.Context, io.Reader) (Blob, error)
	Open(context.Context, string) (io.ReadCloser, error)
	Verify(context.Context, string, string, int64) error
}

type LocalStore struct{ root string }

func NewLocalStore(root string) (*LocalStore, error) {
	if strings.TrimSpace(root) == "" {
		return nil, fmt.Errorf("artifact store root is required")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve artifact store root: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(abs, ".tmp"), 0o700); err != nil {
		return nil, fmt.Errorf("create artifact store: %w", err)
	}
	return &LocalStore{root: abs}, nil
}

func (s *LocalStore) Put(ctx context.Context, r io.Reader) (Blob, error) {
	if s == nil || r == nil {
		return Blob{}, fmt.Errorf("artifact store and content reader are required")
	}
	tmp, err := os.CreateTemp(filepath.Join(s.root, ".tmp"), "blob-*")
	if err != nil {
		return Blob{}, fmt.Errorf("create artifact temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer func() {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
	}()
	if err := tmp.Chmod(0o600); err != nil {
		return Blob{}, fmt.Errorf("chmod artifact temp file: %w", err)
	}

	h := sha256.New()
	written, err := copyContext(ctx, io.MultiWriter(tmp, h), r)
	if err != nil {
		return Blob{}, err
	}
	if err := tmp.Sync(); err != nil {
		return Blob{}, fmt.Errorf("sync artifact temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return Blob{}, fmt.Errorf("close artifact temp file: %w", err)
	}

	hexHash := hex.EncodeToString(h.Sum(nil))
	contentHash := "sha256:" + hexHash
	storageRef := filepath.ToSlash(filepath.Join("sha256", hexHash[:2], hexHash))
	finalPath, err := s.resolve(storageRef)
	if err != nil {
		return Blob{}, err
	}
	if err := os.MkdirAll(filepath.Dir(finalPath), 0o700); err != nil {
		return Blob{}, fmt.Errorf("create artifact shard directory: %w", err)
	}

	created := false
	if err := os.Link(tmpName, finalPath); err != nil {
		if !errors.Is(err, os.ErrExist) {
			return Blob{}, fmt.Errorf("publish artifact blob: %w", err)
		}
		if err := s.Verify(ctx, storageRef, contentHash, written); err != nil {
			return Blob{}, fmt.Errorf("existing artifact blob failed verification: %w", err)
		}
	} else {
		created = true
	}
	return Blob{ContentHash: contentHash, SizeBytes: written, StorageRef: storageRef, Created: created}, nil
}

func (s *LocalStore) Open(_ context.Context, storageRef string) (io.ReadCloser, error) {
	path, err := s.resolve(storageRef)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open artifact blob: %w", err)
	}
	return f, nil
}

func (s *LocalStore) Verify(ctx context.Context, storageRef, contentHash string, sizeBytes int64) error {
	r, err := s.Open(ctx, storageRef)
	if err != nil {
		return err
	}
	defer r.Close()
	h := sha256.New()
	n, err := copyContext(ctx, h, r)
	if err != nil {
		return err
	}
	got := "sha256:" + hex.EncodeToString(h.Sum(nil))
	if n != sizeBytes || got != contentHash {
		return ErrCorruptContent
	}
	return nil
}

func (s *LocalStore) resolve(storageRef string) (string, error) {
	parts := strings.Split(filepath.ToSlash(storageRef), "/")
	if len(parts) != 3 || parts[0] != "sha256" || len(parts[1]) != 2 || len(parts[2]) != 64 || parts[1] != parts[2][:2] {
		return "", ErrUnsafeStorageRef
	}
	if _, err := hex.DecodeString(parts[2]); err != nil {
		return "", ErrUnsafeStorageRef
	}
	path := filepath.Join(s.root, "sha256", parts[1], parts[2])
	rootClean := filepath.Clean(s.root) + string(os.PathSeparator)
	pathClean := filepath.Clean(path)
	if !strings.HasPrefix(pathClean+string(os.PathSeparator), rootClean) {
		return "", ErrUnsafeStorageRef
	}
	return pathClean, nil
}

func copyContext(ctx context.Context, dst io.Writer, src io.Reader) (int64, error) {
	buf := make([]byte, 128*1024)
	var total int64
	for {
		if err := ctx.Err(); err != nil {
			return total, err
		}
		n, readErr := src.Read(buf)
		if n > 0 {
			wn, writeErr := dst.Write(buf[:n])
			total += int64(wn)
			if writeErr != nil {
				return total, fmt.Errorf("write artifact content: %w", writeErr)
			}
			if wn != n {
				return total, io.ErrShortWrite
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return total, nil
			}
			return total, fmt.Errorf("read artifact content: %w", readErr)
		}
	}
}
