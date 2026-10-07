package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"

	"github.com/DigiLogicTech/OnePane/internal/localai"
)

type externalArtifactInspection struct {
	Source       string `json:"source"`
	ModelRef     string `json:"model_ref"`
	DisplayName  string `json:"display_name"`
	Provider     string `json:"provider,omitempty"`
	SourceURL    string `json:"source_url,omitempty"`
	Trust        string `json:"trust"`
	Message      string `json:"message,omitempty"`
	CanAdopt     bool   `json:"can_adopt"`
	Artifacts    []externalArtifact `json:"artifacts"`
}

type externalArtifact struct {
	Filename     string `json:"filename"`
	SizeBytes    int64  `json:"size_bytes"`
	SHA256       string `json:"sha256,omitempty"`
	Quantization string `json:"quantization,omitempty"`
	SourceURL    string `json:"source_url,omitempty"`
	VerifiedBy   string `json:"verified_by,omitempty"`
}

var ggufQuantPattern = regexp.MustCompile(`(?i)(Q[2-8](?:_[A-Z0-9]+)*)`)

func inferGGUFQuantization(filename string) string {
	m := ggufQuantPattern.FindStringSubmatch(strings.ToUpper(filename))
	if len(m) > 1 {
		return m[1]
	}
	return "GGUF"
}

func safeHFRepoPath(repo string) (string, error) {
	repo = strings.TrimSpace(repo)
	parts := strings.Split(repo, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", errors.New("invalid Hugging Face repository id")
	}
	for _, p := range parts {
		if p == "." || p == ".." || strings.ContainsAny(p, "\\?#") {
			return "", errors.New("invalid Hugging Face repository id")
		}
	}
	return url.PathEscape(parts[0]) + "/" + url.PathEscape(parts[1]), nil
}

func inspectHuggingFace(ctx context.Context, repo string) (externalArtifactInspection, error) {
	var out externalArtifactInspection
	repoPath, err := safeHFRepoPath(repo)
	if err != nil {
		return out, err
	}
	endpoint := "https://huggingface.co/api/models/" + repoPath + "?blobs=true"
	var meta struct {
		ID       string `json:"id"`
		Author   string `json:"author"`
		Gated    any    `json:"gated"`
		Siblings []struct {
			Filename string `json:"rfilename"`
			Size     int64  `json:"size"`
			LFS      *struct {
				Size   int64  `json:"size"`
				SHA256 string `json:"sha256"`
			} `json:"lfs"`
		} `json:"siblings"`
	}
	if err := fetchDiscoveryJSON(ctx, endpoint, &meta); err != nil {
		return out, err
	}
	if meta.ID == "" {
		meta.ID = repo
	}
	if hfGated(meta.Gated) {
		return out, errors.New("gated Hugging Face repositories require an authenticated download flow and cannot be adopted by this public verifier")
	}
	name := meta.ID
	if i := strings.LastIndex(name, "/"); i >= 0 {
		name = name[i+1:]
	}
	out = externalArtifactInspection{Source:"huggingface",ModelRef:meta.ID,DisplayName:name,Provider:meta.Author,SourceURL:"https://huggingface.co/"+meta.ID,Trust:"huggingface-lfs-sha256",Artifacts:[]externalArtifact{}}
	for _, s := range meta.Siblings {
		if !strings.HasSuffix(strings.ToLower(s.Filename), ".gguf") || s.LFS == nil || !localSHA256(s.LFS.SHA256) {
			continue
		}
		size := s.Size
		if s.LFS.Size > 0 {
			size = s.LFS.Size
		}
		if size <= 0 {
			continue
		}
		segments := strings.Split(s.Filename, "/")
		for i := range segments {
			segments[i] = url.PathEscape(segments[i])
		}
		artifactURL := "https://huggingface.co/" + repoPath + "/resolve/main/" + strings.Join(segments, "/")
		out.Artifacts = append(out.Artifacts, externalArtifact{Filename:s.Filename,SizeBytes:size,SHA256:strings.ToLower(s.LFS.SHA256),Quantization:inferGGUFQuantization(s.Filename),SourceURL:artifactURL,VerifiedBy:"Hugging Face LFS SHA-256"})
	}
	out.CanAdopt = len(out.Artifacts) > 0
	if !out.CanAdopt {
		out.Message = "No public GGUF artifact with SHA-256 metadata was found in this repository."
	}
	return out, nil
}

func localSHA256(v string) bool {
	if len(v) != 64 {
		return false
	}
	for _, c := range strings.ToLower(v) {
		if !strings.ContainsRune("0123456789abcdef", c) {
			return false
		}
	}
	return true
}

func inspectHuggingBay(ctx context.Context, infohash string) (externalArtifactInspection, error) {
	infohash = strings.TrimSpace(infohash)
	if infohash == "" {
		return externalArtifactInspection{}, errors.New("Hugging Bay infohash required")
	}
	var row struct {
		Infohash  string `json:"infohash"`
		Name      string `json:"name"`
		License   string `json:"license"`
		SourceURL string `json:"source_url"`
		Verified  int    `json:"verified"`
		Files     []struct {
			Path   string `json:"path"`
			Size   int64  `json:"size"`
			SHA256 string `json:"sha256"`
		} `json:"files"`
	}
	if err := fetchDiscoveryJSON(ctx, "https://thehuggingbay.io/api/torrent/"+url.PathEscape(infohash), &row); err != nil {
		return externalArtifactInspection{}, err
	}
	trust := "unverified"
	if row.Verified == 1 { trust = "community-verified" }
	if row.Verified >= 2 { trust = "captain-verified" }
	out := externalArtifactInspection{Source:"huggingbay",ModelRef:row.Name,DisplayName:row.Name,SourceURL:row.SourceURL,Trust:trust,Artifacts:[]externalArtifact{}}
	if u, err := url.Parse(row.SourceURL); err == nil && strings.EqualFold(u.Hostname(), "huggingface.co") {
		repo := strings.Trim(strings.TrimPrefix(u.Path, "/"), "/")
		if strings.Count(repo, "/") == 1 {
			if hf, err := inspectHuggingFace(ctx, repo); err == nil {
				hf.Source = "huggingbay"
				hf.Trust = trust + " · upstream Hugging Face SHA-256"
				hf.Message = "Hugging Bay manifest is linked to the upstream Hugging Face repository; OnePane will pin the upstream LFS SHA-256 before installation."
				return hf, nil
			}
		}
	}
	for _, f := range row.Files {
		if strings.HasSuffix(strings.ToLower(f.Path), ".gguf") && localSHA256(f.SHA256) {
			out.Artifacts = append(out.Artifacts, externalArtifact{Filename:f.Path,SizeBytes:f.Size,SHA256:strings.ToLower(f.SHA256),Quantization:inferGGUFQuantization(f.Path),VerifiedBy:"Hugging Bay manifest"})
		}
	}
	out.Message = "The manifest can be inspected, but this torrent entry does not expose a direct HTTPS GGUF artifact that OnePane can safely adopt."
	return out, nil
}

func (s *Server) inspectExternalModel(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.authenticate(w, r); !ok { return }
	source := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("source")))
	idv := strings.TrimSpace(r.URL.Query().Get("id"))
	var out externalArtifactInspection
	var err error
	switch source {
	case "huggingface":
		out, err = inspectHuggingFace(r.Context(), idv)
	case "huggingbay":
		out, err = inspectHuggingBay(r.Context(), idv)
	case "llmfit":
		out = externalArtifactInspection{Source:"llmfit",ModelRef:idv,DisplayName:idv,Trust:"advisory",CanAdopt:false,Message:"llmfit scores hardware suitability but does not provide a trusted artifact. Search Hugging Face for a GGUF release of this model, then verify that artifact."}
	default:
		err = errors.New("unsupported discovery source")
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) adoptExternalModel(w http.ResponseWriter, r *http.Request) {
	i, ok := s.authenticate(w, r)
	if !ok { return }
	var in struct {
		WorkspaceID string `json:"workspace_id"`
		Source      string `json:"source"`
		ModelID     string `json:"model_id"`
		Filename    string `json:"filename"`
	}
	if !decodeJSON(w, r, &in) { return }
	if !s.authorize(w, r, i, strings.TrimSpace(in.WorkspaceID), "model.write") { return }
	if s.localAI == nil || s.localAI.Catalog() == nil {
		writeError(w, http.StatusServiceUnavailable, "local AI catalog unavailable")
		return
	}
	source := strings.ToLower(strings.TrimSpace(in.Source))
	var inspected externalArtifactInspection
	var err error
	switch source {
	case "huggingface":
		inspected, err = inspectHuggingFace(r.Context(), in.ModelID)
	case "huggingbay":
		inspected, err = inspectHuggingBay(r.Context(), in.ModelID)
	default:
		err = errors.New("only Hugging Face or a Hugging Bay entry linked to Hugging Face can be adopted")
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	var chosen *externalArtifact
	for idx := range inspected.Artifacts {
		if inspected.Artifacts[idx].Filename == strings.TrimSpace(in.Filename) {
			chosen = &inspected.Artifacts[idx]
			break
		}
	}
	if chosen == nil || !localSHA256(chosen.SHA256) || !strings.HasPrefix(strings.ToLower(chosen.SourceURL), "https://huggingface.co/") {
		writeError(w, http.StatusBadRequest, "selected external artifact is not eligible for digest-pinned adoption")
		return
	}
	sourceRef := "hf://" + inspected.ModelRef
	artifact, spec, err := s.localAI.Catalog().AdoptExternalModel(r.Context(), localai.AdoptExternalModelCommand{
		ModelRef: inspected.ModelRef, DisplayName: inspected.DisplayName, Provider: inspected.Provider,
		Quantization: chosen.Quantization, RuntimeName:"llamacpp", SourceRef:sourceRef, SourceURL:chosen.SourceURL,
		SHA256:chosen.SHA256, Filename:path.Base(chosen.Filename), SizeBytes:chosen.SizeBytes,
		VerificationSource:"huggingface-lfs-sha256", CreatedBy:i.PrincipalID,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"artifact":artifact,"model":spec,"verification":"digest pinned; download will re-verify SHA-256"})
}

func parseLimit(v string, def int) int {
	n, _ := strconv.Atoi(v)
	if n <= 0 { return def }
	if n > 100 { return 100 }
	return n
}

var _ = fmt.Sprintf
