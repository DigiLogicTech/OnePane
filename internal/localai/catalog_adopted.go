package localai

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

type AdoptExternalModelCommand struct {
	ModelRef           string `json:"model_ref"`
	DisplayName        string `json:"display_name"`
	Provider           string `json:"provider"`
	Quantization       string `json:"quantization"`
	RuntimeName        string `json:"runtime_name"`
	SourceRef          string `json:"source_ref"`
	SourceURL          string `json:"source_url"`
	SHA256             string `json:"sha256"`
	Filename           string `json:"filename"`
	SizeBytes          int64  `json:"size_bytes"`
	VerificationSource string `json:"verification_source"`
	CreatedBy          string `json:"-"`
}

var paramsPattern = regexp.MustCompile(`(?i)(\d+(?:\.\d+)?)\s*[bB](?:\b|[-_])`)

func externalParamsB(ref, name, quant string, size int64) float64 {
	for _, s := range []string{ref, name} {
		if m := paramsPattern.FindStringSubmatch(s); len(m) == 2 {
			if v, err := strconv.ParseFloat(m[1], 64); err == nil && v > 0 {
				return v
			}
		}
	}
	bytesPerParam := 0.58
	q := strings.ToUpper(strings.TrimSpace(quant))
	switch {
	case strings.HasPrefix(q, "Q8"):
		bytesPerParam = 1.05
	case strings.HasPrefix(q, "Q6"):
		bytesPerParam = 0.82
	case strings.HasPrefix(q, "Q5"):
		bytesPerParam = 0.70
	case strings.HasPrefix(q, "Q3"):
		bytesPerParam = 0.48
	case strings.HasPrefix(q, "Q2"):
		bytesPerParam = 0.36
	}
	if size <= 0 {
		return 1
	}
	return math.Max(0.1, math.Round((float64(size)/1e9/bytesPerParam)*10)/10)
}

func externalModelSpec(cmd AdoptExternalModelCommand) ModelSpec {
	display := strings.TrimSpace(cmd.DisplayName)
	if display == "" {
		display = strings.TrimSpace(cmd.ModelRef)
	}
	provider := strings.TrimSpace(cmd.Provider)
	if provider == "" {
		provider = "External"
	}
	return ModelSpec{
		ModelRef: strings.TrimSpace(cmd.ModelRef), DisplayName: display, Provider: provider,
		Architecture: "transformer", ParamsB: externalParamsB(cmd.ModelRef, display, cmd.Quantization, cmd.SizeBytes),
		ContextLength: 8192, UseCases: []UseCase{UseGeneral, UseChat, UseReasoning, UseCoding},
		QualityScore: 70, SourceRef: strings.TrimSpace(cmd.SourceRef), Runtime: "llamacpp",
		Quantizations: []string{strings.TrimSpace(cmd.Quantization)},
	}
}

func (s *CatalogService) AdoptExternalModel(ctx context.Context, cmd AdoptExternalModelCommand) (ModelCatalogEntry, ModelSpec, error) {
	var out ModelCatalogEntry
	if s == nil || s.db == nil || s.clock == nil {
		return out, ModelSpec{}, errors.New("catalog service unavailable")
	}
	cmd.ModelRef = strings.TrimSpace(cmd.ModelRef)
	cmd.DisplayName = strings.TrimSpace(cmd.DisplayName)
	cmd.Provider = strings.TrimSpace(cmd.Provider)
	cmd.Quantization = strings.TrimSpace(cmd.Quantization)
	cmd.RuntimeName = strings.TrimSpace(cmd.RuntimeName)
	cmd.SourceRef = strings.TrimSpace(cmd.SourceRef)
	cmd.SourceURL = strings.TrimSpace(cmd.SourceURL)
	cmd.SHA256 = strings.ToLower(strings.TrimSpace(cmd.SHA256))
	cmd.Filename = strings.TrimSpace(cmd.Filename)
	cmd.VerificationSource = strings.TrimSpace(cmd.VerificationSource)
	if cmd.RuntimeName == "" {
		cmd.RuntimeName = "llamacpp"
	}
	if cmd.ModelRef == "" || cmd.Quantization == "" || cmd.RuntimeName != "llamacpp" || cmd.SourceRef == "" || !strings.HasPrefix(strings.ToLower(cmd.SourceURL), "https://") || !validateSHA256(cmd.SHA256) || cmd.Filename == "" || cmd.SizeBytes <= 0 {
		return out, ModelSpec{}, errors.New("external model adoption requires a HTTPS artifact with model ref, quantization, size and SHA-256")
	}
	spec := externalModelSpec(cmd)
	if spec.ParamsB <= 0 || spec.ContextLength <= 0 || len(spec.UseCases) == 0 {
		return out, ModelSpec{}, errors.New("could not derive a safe model specification")
	}
	raw, _ := json.Marshal(spec)
	idv, err := s.ids.New("adoptedmodel")
	if err != nil {
		return out, ModelSpec{}, err
	}
	now := s.clock.UnixMilli()
	_, err = s.db.ExecContext(ctx, `INSERT INTO local_ai_adopted_models(id,model_ref,display_name,provider,quantization,runtime_name,source_ref,source_url,sha256,filename,size_bytes,spec_json,verification_source,created_by,created_at,updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(model_ref,quantization,runtime_name) DO UPDATE SET display_name=excluded.display_name,provider=excluded.provider,source_ref=excluded.source_ref,source_url=excluded.source_url,sha256=excluded.sha256,filename=excluded.filename,size_bytes=excluded.size_bytes,spec_json=excluded.spec_json,verification_source=excluded.verification_source,created_by=excluded.created_by,updated_at=excluded.updated_at`,
		idv, cmd.ModelRef, spec.DisplayName, spec.Provider, cmd.Quantization, cmd.RuntimeName, cmd.SourceRef, cmd.SourceURL, cmd.SHA256, cmd.Filename, cmd.SizeBytes, string(raw), cmd.VerificationSource, nullIfEmpty(cmd.CreatedBy), now, now)
	if err != nil {
		return out, ModelSpec{}, err
	}
	out = ModelCatalogEntry{ModelRef: cmd.ModelRef, Quantization: cmd.Quantization, RuntimeName: cmd.RuntimeName, SourceRef: cmd.SourceRef, SourceURL: cmd.SourceURL, SHA256: cmd.SHA256, Filename: cmd.Filename, SizeBytes: cmd.SizeBytes}
	return out, spec, nil
}

func (s *CatalogService) adoptedModelSpecs(ctx context.Context) ([]ModelSpec, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT spec_json FROM local_ai_adopted_models ORDER BY updated_at DESC`)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "no such table") {
			return nil, nil
		}
		return nil, err
	}
	defer rows.Close()
	var out []ModelSpec
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var spec ModelSpec
		if json.Unmarshal([]byte(raw), &spec) == nil && strings.TrimSpace(spec.ModelRef) != "" {
			out = append(out, spec)
		}
	}
	return out, rows.Err()
}

func (s *CatalogService) adoptedInstallability(ctx context.Context, out map[string]map[string]bool) error {
	rows, err := s.db.QueryContext(ctx, `SELECT model_ref,quantization FROM local_ai_adopted_models`)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "no such table") {
			return nil
		}
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var ref, q string
		if err := rows.Scan(&ref, &q); err != nil {
			return err
		}
		m := out[ref]
		if m == nil {
			m = map[string]bool{}
			out[ref] = m
		}
		m[q] = true
	}
	return rows.Err()
}

func (s *CatalogService) adoptedArtifact(ctx context.Context, modelRef, quantization, runtimeName, sourceRef string) (ModelArtifact, bool, error) {
	var sourceURL, sha, filename, storedSource string
	var size int64
	err := s.db.QueryRowContext(ctx, `SELECT source_ref,source_url,sha256,filename,size_bytes FROM local_ai_adopted_models WHERE lower(model_ref)=lower(?) AND lower(quantization)=lower(?) AND lower(runtime_name)=lower(?) LIMIT 1`, modelRef, quantization, runtimeName).Scan(&storedSource, &sourceURL, &sha, &filename, &size)
	if errors.Is(err, sql.ErrNoRows) {
		return ModelArtifact{}, false, nil
	}
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "no such table") {
			return ModelArtifact{}, false, nil
		}
		return ModelArtifact{}, false, err
	}
	if strings.TrimSpace(sourceRef) != "" && !strings.EqualFold(strings.TrimSpace(sourceRef), storedSource) {
		return ModelArtifact{}, false, nil
	}
	if !validateSHA256(sha) || !strings.HasPrefix(strings.ToLower(sourceURL), "https://") || size <= 0 {
		return ModelArtifact{}, false, fmt.Errorf("adopted artifact metadata for %s is invalid", modelRef)
	}
	return ModelArtifact{ModelRef: modelRef, SourceURL: sourceURL, ExpectedSHA256: sha, Filename: filename, SizeBytes: size}, true, nil
}
