package localai

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type RuntimeDependency struct {
	Name          string `json:"name"`
	SourceURL     string `json:"source_url"`
	SHA256        string `json:"sha256"`
	ArchiveFormat string `json:"archive_format"`
}

type RuntimeManifest struct {
	Name          string              `json:"name"`
	Version       string              `json:"version"`
	Backend       string              `json:"backend,omitempty"`
	OS            string              `json:"os"`
	Architecture  string              `json:"architecture"`
	SourceURL     string              `json:"source_url"`
	SHA256        string              `json:"sha256"`
	ArchiveFormat string              `json:"archive_format"` // tar.gz | zip | binary
	ExecutableRel string              `json:"executable_rel"`
	Dependencies  []RuntimeDependency `json:"dependencies,omitempty"`
}

func runtimeInstallFingerprint(runtime RuntimeManifest) string {
	type fingerprint struct {
		SourceURL     string              `json:"source_url"`
		SHA256        string              `json:"sha256"`
		ArchiveFormat string              `json:"archive_format"`
		Dependencies  []RuntimeDependency `json:"dependencies,omitempty"`
	}
	raw, _ := json.Marshal(fingerprint{SourceURL: runtime.SourceURL, SHA256: strings.ToLower(runtime.SHA256), ArchiveFormat: runtime.ArchiveFormat, Dependencies: runtime.Dependencies})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

type ModelArtifact struct {
	ModelRef       string `json:"model_ref"`
	SourceURL      string `json:"source_url"`
	ExpectedSHA256 string `json:"expected_sha256,omitempty"`
	Filename       string `json:"filename"`
	SizeBytes      int64  `json:"size_bytes,omitempty"`
}

type DownloadResult struct {
	Path   string
	SHA256 string
	Size   int64
}

type resumableDownloadError struct{ err error }

func (e *resumableDownloadError) Error() string { return e.err.Error() }
func (e *resumableDownloadError) Unwrap() error { return e.err }
func markResumableDownload(err error) error {
	if err == nil {
		return nil
	}
	return &resumableDownloadError{err: err}
}
func isResumableDownloadError(err error) bool {
	var target *resumableDownloadError
	return errors.As(err, &target)
}

type HTTPFetcher struct {
	Client   *http.Client
	MaxBytes int64
}

func NewHTTPFetcher(maxBytes int64) *HTTPFetcher {
	return &HTTPFetcher{Client: &http.Client{Timeout: 30 * time.Minute, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("too many redirects")
		}
		if req.URL.Scheme != "https" {
			return errors.New("redirect to non-HTTPS URL refused")
		}
		return nil
	}}, MaxBytes: maxBytes}
}

func existingDownload(path string) (DownloadResult, error) {
	f, err := os.Open(path)
	if err != nil {
		return DownloadResult{}, err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return DownloadResult{}, err
	}
	return DownloadResult{Path: path, SHA256: hex.EncodeToString(h.Sum(nil)), Size: n}, nil
}

func (f *HTTPFetcher) Fetch(ctx context.Context, sourceURL, dest, expectedSHA string) (DownloadResult, error) {
	if f == nil || f.Client == nil || f.MaxBytes <= 0 {
		return DownloadResult{}, errors.New("invalid fetcher")
	}
	if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(sourceURL)), "https://") {
		return DownloadResult{}, errors.New("downloads require HTTPS")
	}
	if strings.TrimSpace(dest) == "" {
		return DownloadResult{}, errors.New("destination required")
	}
	expectedSHA = strings.ToLower(strings.TrimSpace(expectedSHA))
	if expectedSHA != "" {
		if len(expectedSHA) != 64 {
			return DownloadResult{}, errors.New("invalid expected sha256")
		}
		if _, err := hex.DecodeString(expectedSHA); err != nil {
			return DownloadResult{}, errors.New("invalid expected sha256")
		}
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
		return DownloadResult{}, err
	}

	// A completed verified artifact is durable install state. Reuse it instead
	// of downloading the same multi-gigabyte model again after a daemon restart.
	if st, err := os.Stat(dest); err == nil && !st.IsDir() {
		got, hashErr := existingDownload(dest)
		if hashErr != nil {
			return DownloadResult{}, hashErr
		}
		if expectedSHA == "" || strings.EqualFold(got.SHA256, expectedSHA) {
			return got, nil
		}
		if err := os.Remove(dest); err != nil {
			return DownloadResult{}, fmt.Errorf("remove corrupt completed download: %w", err)
		}
	} else if err != nil && !os.IsNotExist(err) {
		return DownloadResult{}, err
	}

	partial := dest + ".partial"
	h := sha256.New()
	var offset int64
	if st, err := os.Stat(partial); err == nil {
		if st.IsDir() {
			return DownloadResult{}, errors.New("partial download path is a directory")
		}
		if st.Size() > f.MaxBytes {
			_ = os.Remove(partial)
			return DownloadResult{}, errors.New("partial download exceeds maximum size")
		}
		offset = st.Size()
		in, err := os.Open(partial)
		if err != nil {
			return DownloadResult{}, err
		}
		_, hashErr := io.Copy(h, in)
		closeErr := in.Close()
		if hashErr != nil {
			return DownloadResult{}, hashErr
		}
		if closeErr != nil {
			return DownloadResult{}, closeErr
		}
	} else if err != nil && !os.IsNotExist(err) {
		return DownloadResult{}, err
	}

	// Digest-pinned catalogue artifacts can be resumed safely because the final
	// SHA-256 verifies the entire object. Unverified partials restart instead of
	// risking an append from a changed upstream object.
	if offset > 0 && expectedSHA == "" {
		_ = os.Remove(partial)
		offset = 0
		h = sha256.New()
	}

	out, err := os.OpenFile(partial, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return DownloadResult{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sourceURL, nil)
	if err != nil {
		_ = out.Close()
		return DownloadResult{}, err
	}
	if offset > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", offset))
	}
	resp, err := f.Client.Do(req)
	if err != nil {
		_ = out.Close()
		return DownloadResult{}, markResumableDownload(err)
	}
	defer resp.Body.Close()

	if offset > 0 && resp.StatusCode == http.StatusOK {
		// Origin ignored Range or changed the selected representation. Restart
		// safely with the full response instead of appending duplicate bytes.
		if err := out.Close(); err != nil {
			return DownloadResult{}, err
		}
		out, err = os.OpenFile(partial, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
		if err != nil {
			return DownloadResult{}, err
		}
		offset = 0
		h = sha256.New()
	} else if resp.StatusCode == http.StatusRequestedRangeNotSatisfiable && offset > 0 {
		// A crash can happen after the final byte was flushed but before the
		// .partial file was renamed. If its digest is already complete, promote it.
		if err := out.Close(); err != nil {
			return DownloadResult{}, err
		}
		actual := hex.EncodeToString(h.Sum(nil))
		if expectedSHA != "" && strings.EqualFold(actual, expectedSHA) {
			if err := os.Chmod(partial, 0o600); err != nil {
				return DownloadResult{}, err
			}
			if err := os.Rename(partial, dest); err != nil {
				return DownloadResult{}, err
			}
			return DownloadResult{Path: dest, SHA256: actual, Size: offset}, nil
		}
		_ = os.Remove(partial)
		return DownloadResult{}, markResumableDownload(errors.New("origin rejected partial range; partial reset"))
	} else if resp.StatusCode == http.StatusPartialContent {
		want := fmt.Sprintf("bytes %d-", offset)
		if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(resp.Header.Get("Content-Range"))), want) {
			_ = out.Close()
			_ = os.Remove(partial)
			return DownloadResult{}, markResumableDownload(errors.New("download server returned an incompatible content range; partial reset"))
		}
	} else if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_ = out.Close()
		err := fmt.Errorf("download returned HTTP %d", resp.StatusCode)
		if resp.StatusCode == http.StatusRequestTimeout || resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			return DownloadResult{}, markResumableDownload(err)
		}
		return DownloadResult{}, err
	}

	if resp.ContentLength >= 0 && offset+resp.ContentLength > f.MaxBytes {
		_ = out.Close()
		return DownloadResult{}, errors.New("download exceeds maximum size")
		_ = os.Remove(partial)
	}
	remaining := f.MaxBytes - offset
	n, copyErr := io.Copy(io.MultiWriter(out, h), io.LimitReader(resp.Body, remaining+1))
	if copyErr != nil {
		_ = out.Sync()
		_ = out.Close()
		// Deliberately retain .partial so a subsequent install tick can resume.
		return DownloadResult{}, markResumableDownload(copyErr)
	}
	total := offset + n
	if n > remaining || total > f.MaxBytes {
		_ = out.Close()
		_ = os.Remove(partial)
		return DownloadResult{}, errors.New("download exceeds maximum size")
	}
	if err := out.Sync(); err != nil {
		_ = out.Close()
		return DownloadResult{}, err
	}
	if err := out.Close(); err != nil {
		return DownloadResult{}, err
	}
	actual := hex.EncodeToString(h.Sum(nil))
	if expectedSHA != "" && !strings.EqualFold(actual, expectedSHA) {
		_ = os.Remove(partial)
		return DownloadResult{}, fmt.Errorf("sha256 mismatch: expected %s got %s", expectedSHA, actual)
	}
	if err := os.Chmod(partial, 0o600); err != nil {
		return DownloadResult{}, err
	}
	if err := os.Rename(partial, dest); err != nil {
		return DownloadResult{}, err
	}
	return DownloadResult{Path: dest, SHA256: actual, Size: total}, nil
}

func safeArchivePath(root, name string) (string, error) {
	name = strings.ReplaceAll(name, "\\", "/")
	clean := filepath.Clean(name)
	if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("unsafe archive path %q", name)
	}
	dest := filepath.Join(root, clean)
	rel, err := filepath.Rel(root, dest)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("archive escapes root")
	}
	return dest, nil
}

func safeArchiveSymlinkTarget(root, linkPath, target string) error {
	target = strings.ReplaceAll(strings.TrimSpace(target), "\\", "/")
	if target == "" || strings.HasPrefix(target, "/") {
		return fmt.Errorf("unsafe archive symlink target %q", target)
	}
	resolved := filepath.Clean(filepath.Join(filepath.Dir(linkPath), filepath.FromSlash(target)))
	rel, err := filepath.Rel(root, resolved)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("archive symlink escapes root: %q", target)
	}
	return nil
}

func ExtractRuntimeArchive(archivePath, format, destRoot string) error {
	if err := os.MkdirAll(destRoot, 0o700); err != nil {
		return err
	}
	switch format {
	case "tar.gz":
		f, err := os.Open(archivePath)
		if err != nil {
			return err
		}
		defer f.Close()
		gz, err := gzip.NewReader(f)
		if err != nil {
			return err
		}
		defer gz.Close()
		tr := tar.NewReader(gz)
		for {
			h, err := tr.Next()
			if err == io.EOF {
				break
			}
			if err != nil {
				return err
			}
			dst, err := safeArchivePath(destRoot, h.Name)
			if err != nil {
				return err
			}
			switch h.Typeflag {
			case tar.TypeDir:
				if err := os.MkdirAll(dst, 0o700); err != nil {
					return err
				}
			case tar.TypeReg:
				if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
					return err
				}
				out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o700)
				if err != nil {
					return err
				}
				_, cp := io.Copy(out, io.LimitReader(tr, 1<<31))
				ce := out.Close()
				if cp != nil {
					return cp
				}
				if ce != nil {
					return ce
				}
			case tar.TypeSymlink:
				if err := safeArchiveSymlinkTarget(destRoot, dst, h.Linkname); err != nil {
					return err
				}
				if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
					return err
				}
				if _, err := os.Lstat(dst); err == nil {
					return fmt.Errorf("duplicate archive entry %s", h.Name)
				} else if !os.IsNotExist(err) {
					return err
				}
				if err := os.Symlink(filepath.FromSlash(strings.ReplaceAll(h.Linkname, "\\", "/")), dst); err != nil {
					return err
				}
			default:
				return fmt.Errorf("unsupported archive entry type for %s", h.Name)
			}
		}
	case "zip":
		zr, err := zip.OpenReader(archivePath)
		if err != nil {
			return err
		}
		defer zr.Close()
		for _, z := range zr.File {
			dst, err := safeArchivePath(destRoot, z.Name)
			if err != nil {
				return err
			}
			if z.FileInfo().IsDir() {
				if err := os.MkdirAll(dst, 0o700); err != nil {
					return err
				}
				continue
			}
			if z.Mode()&os.ModeSymlink != 0 {
				return fmt.Errorf("symlinks are not allowed in runtime archive")
			}
			if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
				return err
			}
			rc, err := z.Open()
			if err != nil {
				return err
			}
			out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o700)
			if err != nil {
				rc.Close()
				return err
			}
			_, cp := io.Copy(out, io.LimitReader(rc, 1<<31))
			rc.Close()
			ce := out.Close()
			if cp != nil {
				return cp
			}
			if ce != nil {
				return ce
			}
		}
	default:
		return fmt.Errorf("unsupported archive format %q", format)
	}
	return nil
}
