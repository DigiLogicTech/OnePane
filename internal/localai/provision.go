package localai

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type RuntimeManifest struct {
	Name          string `json:"name"`
	Version       string `json:"version"`
	Backend       string `json:"backend,omitempty"`
	OS            string `json:"os"`
	Architecture  string `json:"architecture"`
	SourceURL     string `json:"source_url"`
	SHA256        string `json:"sha256"`
	ArchiveFormat string `json:"archive_format"` // tar.gz | zip | binary
	ExecutableRel string `json:"executable_rel"`
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
	if expectedSHA != "" && (len(expectedSHA) != 64 || strings.ContainsAny(expectedSHA, " /\\")) {
		return DownloadResult{}, errors.New("invalid expected sha256")
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o700); err != nil {
		return DownloadResult{}, err
	}
	tmp, err := os.CreateTemp(filepath.Dir(dest), ".download-*")
	if err != nil {
		return DownloadResult{}, err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sourceURL, nil)
	if err != nil {
		tmp.Close()
		return DownloadResult{}, err
	}
	resp, err := f.Client.Do(req)
	if err != nil {
		tmp.Close()
		return DownloadResult{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		tmp.Close()
		return DownloadResult{}, fmt.Errorf("download returned HTTP %d", resp.StatusCode)
	}
	if resp.ContentLength > f.MaxBytes {
		tmp.Close()
		return DownloadResult{}, fmt.Errorf("download exceeds maximum size")
	}
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(tmp, h), io.LimitReader(resp.Body, f.MaxBytes+1))
	if err != nil {
		tmp.Close()
		return DownloadResult{}, err
	}
	if n > f.MaxBytes {
		tmp.Close()
		return DownloadResult{}, fmt.Errorf("download exceeds maximum size")
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return DownloadResult{}, err
	}
	if err := tmp.Close(); err != nil {
		return DownloadResult{}, err
	}
	actual := hex.EncodeToString(h.Sum(nil))
	if expectedSHA != "" && !strings.EqualFold(actual, expectedSHA) {
		return DownloadResult{}, fmt.Errorf("sha256 mismatch: expected %s got %s", expectedSHA, actual)
	}
	if err := os.Chmod(tmpName, 0o600); err != nil {
		return DownloadResult{}, err
	}
	if err := os.Rename(tmpName, dest); err != nil {
		return DownloadResult{}, err
	}
	return DownloadResult{Path: dest, SHA256: actual, Size: n}, nil
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
