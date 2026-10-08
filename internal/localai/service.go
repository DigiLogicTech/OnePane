package localai

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"sync"

	"github.com/DigiLogicTech/OnePane/internal/clock"
	"github.com/DigiLogicTech/OnePane/internal/event"
	"github.com/DigiLogicTech/OnePane/internal/id"
	"github.com/DigiLogicTech/OnePane/internal/inference"
	"github.com/DigiLogicTech/OnePane/internal/storage"
)

type PlanStatus string

const (
	PlanProposed          PlanStatus = "proposed"
	PlanApproved          PlanStatus = "approved"
	PlanInstallingRuntime PlanStatus = "installing_runtime"
	PlanDownloadingModel  PlanStatus = "downloading_model"
	PlanQualifying        PlanStatus = "qualifying"
	PlanReady             PlanStatus = "ready"
	PlanFailed            PlanStatus = "failed"
	PlanCancelled         PlanStatus = "cancelled"
	PlanSuperseded        PlanStatus = "superseded"
)

type InstallPlan struct {
	ID, NodeID, HardwareProfileID, RoleName, UseCase, ModelRef, SourceRef, RuntimeName, Quantization string
	WorkspaceID                                                                                      *string
	ContextTokens                                                                                    int64
	FitLevel                                                                                         FitLevel
	RunMode                                                                                          RunMode
	EstimatedTPS                                                                                     *float64
	MemoryRequiredBytes, DiskRequiredBytes, DownloadScratchBytes                                     int64
	PlanJSON                                                                                         json.RawMessage
	Status                                                                                           PlanStatus
	CreatedBy, ApprovedBy                                                                            *string
	FailureReason                                                                                    *string
	Revision, CreatedAt, UpdatedAt                                                                   int64
}

type CreatePlanCommand struct {
	WorkspaceID                         *string
	NodeID, HardwareProfileID, RoleName string
	UseCase                             UseCase
	Recommendation                      Recommendation
	ActorPrincipalID                    *string
}

type Service struct {
	db           *sql.DB
	tx           storage.Transactor
	events       event.Store
	ids          id.Generator
	clock        clock.Clock
	detector     *Detector
	inference    *inference.Service
	dataDir      string
	modelRoot    string
	fetcher      *HTTPFetcher
	supervisor   *RuntimeSupervisor
	qualifier    *Qualifier
	catalog      *CatalogService
	catalogTrust *CatalogTrustStore
	llmfit       *LLMFitClient
	// A single node can service concurrent UI/remote install requests. Serialize
	// shared runtime provisioning, without serializing entire model downloads.
	runtimeInstallMu sync.Mutex
	installQueueMu   sync.Mutex
}

func NewService(db *sql.DB, tx storage.Transactor, clk clock.Clock, inf *inference.Service, dataDir string, trust *CatalogTrustStore) *Service {
	if trust == nil {
		trust = NewCatalogTrustStore()
	}
	modelRoot := filepath.Join(dataDir, "models", "managed")
	s := &Service{db: db, tx: tx, events: event.Store{}, ids: id.Generator{}, clock: clk, detector: NewDetector(), inference: inf, dataDir: dataDir, modelRoot: modelRoot, fetcher: NewHTTPFetcher(256 << 30), catalogTrust: trust}
	s.supervisor = NewRuntimeSupervisor(db, tx, clk, dataDir, nil)
	s.supervisor.SetModelRoot(modelRoot)
	s.qualifier = NewQualifier(db, tx, clk, s.supervisor, inf, nil)
	s.catalog = NewCatalogService(db, tx, clk, trust)
	return s
}

func (s *Service) Catalog() *CatalogService { return s.catalog }

// ConfigureModelPool moves future managed-model installs to an operator-selected
// absolute path (for example a large local SSD or a mounted NAS). Existing
// deployment inventory remains authoritative and must still resolve beneath the
// configured pool before execution.
func (s *Service) ConfigureModelPool(path string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		path = filepath.Join(s.dataDir, "models", "managed")
	}
	if !filepath.IsAbs(path) {
		return errors.New("model pool path must be absolute")
	}
	path = filepath.Clean(path)
	if err := os.MkdirAll(path, 0o750); err != nil {
		return fmt.Errorf("create model pool: %w", err)
	}
	s.modelRoot = path
	if s.supervisor != nil {
		s.supervisor.SetModelRoot(path)
	}
	return nil
}

func (s *Service) ConfigureLLMFit(raw string) error {
	c, err := NewLLMFitClient(raw)
	if err != nil {
		return err
	}
	s.llmfit = c
	return nil
}

func (s *Service) ConfigureResidencyHeadroom(percent int) error {
	if percent < 0 || percent > 50 {
		return errors.New("residency headroom must be between 0 and 50 percent")
	}
	if s.supervisor == nil {
		return errors.New("local runtime supervisor unavailable")
	}
	s.supervisor.SetResidencyHeadroom(percent)
	return nil
}

func (s *Service) RecoverManagedRuntimes(ctx context.Context) error {
	if s == nil || s.supervisor == nil {
		return errors.New("local runtime supervisor unavailable")
	}
	return s.supervisor.Recover(ctx)
}

func (s *Service) StartManagedDeployment(ctx context.Context, deploymentID string) (RuntimeInstance, error) {
	return s.supervisor.Start(ctx, deploymentID)
}

func (s *Service) StopManagedDeployment(ctx context.Context, deploymentID string) error {
	return s.supervisor.Stop(ctx, deploymentID)
}

func (s *Service) QualifyManagedDeployment(ctx context.Context, req QualificationRequest) (QualificationRun, error) {
	return s.qualifier.Qualify(ctx, req)
}

func (s *Service) DetectAndPersist(ctx context.Context, nodeID string) (HardwareProfile, error) {
	p, err := s.detector.Detect(ctx, nodeID, s.modelRoot)
	if err != nil {
		return p, err
	}
	idv, err := s.ids.New("hw")
	if err != nil {
		return p, err
	}
	p.ID = idv
	cpu, _ := json.Marshal(p.CPU)
	mem, _ := json.Marshal(p.Memory)
	gpus, _ := json.Marshal(p.GPUs)
	runtimes, _ := json.Marshal(p.RuntimeProbes)
	stor, _ := json.Marshal(p.Storage)
	now := s.clock.UnixMilli()
	p.DetectedAt = now
	err = s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		var existing string
		err := tx.QueryRowContext(ctx, `SELECT id FROM local_hardware_profiles WHERE node_id=? AND fingerprint=?`, p.NodeID, p.Fingerprint).Scan(&existing)
		if err == nil {
			p.ID = existing
			return nil
		}
		if err != sql.ErrNoRows {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO local_hardware_profiles(id,node_id,fingerprint,os_name,os_version,architecture,kernel_version,cpu_json,memory_json,accelerators_json,runtimes_json,storage_json,detected_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`, p.ID, p.NodeID, p.Fingerprint, p.OSName, p.OSVersion, p.Architecture, p.KernelVersion, string(cpu), string(mem), string(gpus), string(runtimes), string(stor), p.DetectedAt); err != nil {
			return err
		}
		eid, _ := s.ids.New("evt")
		payload, _ := json.Marshal(map[string]any{"hardware_profile_id": p.ID, "node_id": p.NodeID, "fingerprint": p.Fingerprint})
		return s.events.Append(ctx, tx, event.Event{ID: eid, Type: "local_ai.hardware_detected", AggregateType: "local_hardware_profile", AggregateID: p.ID, Payload: payload, OccurredAt: now})
	})
	return p, err
}

func installableModelSpecifications(cat ArtifactCatalog) []ModelSpec {
	allowed := map[string]map[string]bool{}
	for _, artifact := range cat.Models {
		key := strings.ToLower(strings.TrimSpace(artifact.ModelRef)) + "|" + strings.ToLower(strings.TrimSpace(artifact.RuntimeName))
		if allowed[key] == nil {
			allowed[key] = map[string]bool{}
		}
		allowed[key][strings.ToUpper(strings.TrimSpace(artifact.Quantization))] = true
	}
	out := make([]ModelSpec, 0, len(cat.ModelSpecs))
	for _, spec := range cat.ModelSpecs {
		key := strings.ToLower(strings.TrimSpace(spec.ModelRef)) + "|" + strings.ToLower(strings.TrimSpace(spec.Runtime))
		var quantizations []string
		for _, q := range spec.Quantizations {
			q = strings.ToUpper(strings.TrimSpace(q))
			if allowed[key][q] {
				quantizations = append(quantizations, q)
			}
		}
		if len(quantizations) == 0 {
			continue
		}
		copy := spec
		copy.Quantizations = quantizations
		out = append(out, copy)
	}
	return out
}

func filterHardwareForCatalog(p HardwareProfile, cat ArtifactCatalog) HardwareProfile {
	allowed := map[string]bool{"cpu": false}
	for _, r := range cat.Runtimes {
		if !strings.EqualFold(r.OS, p.OSName) || !strings.EqualFold(r.Architecture, p.Architecture) {
			continue
		}
		backend := strings.ToLower(strings.TrimSpace(r.Backend))
		if backend == "" {
			backend = "cpu"
		}
		allowed[backend] = true
	}
	copy := p
	copy.GPUs = append([]GPU(nil), p.GPUs...)
	for i := range copy.GPUs {
		var backends []string
		for _, b := range gpuBackends(copy.GPUs[i]) {
			if allowed[b] {
				backends = append(backends, b)
			}
		}
		copy.GPUs[i].Backends = backends
		copy.GPUs[i].Backend = preferredBackend(backends)
	}
	return copy
}

func trustedModelArtifact(cat ArtifactCatalog, rec Recommendation) (ModelCatalogEntry, bool) {
	for _, artifact := range cat.Models {
		if strings.EqualFold(artifact.ModelRef, rec.Model.ModelRef) &&
			strings.EqualFold(artifact.Quantization, rec.Quantization) &&
			strings.EqualFold(artifact.RuntimeName, rec.Model.Runtime) {
			return artifact, true
		}
	}
	return ModelCatalogEntry{}, false
}

func trustedRuntimeArtifact(cat ArtifactCatalog, p HardwareProfile, rec Recommendation) (RuntimeCatalogEntry, bool) {
	wanted := strings.ToLower(strings.TrimSpace(rec.Placement.Backend))
	if wanted == "" {
		wanted = "cpu"
	}
	var fallback *RuntimeCatalogEntry
	for i := range cat.Runtimes {
		entry := cat.Runtimes[i]
		if !strings.EqualFold(entry.Name, rec.Model.Runtime) || !strings.EqualFold(entry.OS, p.OSName) || !strings.EqualFold(entry.Architecture, p.Architecture) {
			continue
		}
		backend := strings.ToLower(strings.TrimSpace(entry.Backend))
		if backend == wanted {
			return entry, true
		}
		if backend == "" && fallback == nil {
			copy := entry
			fallback = &copy
		}
	}
	// A generic/CPU runtime may never be substituted for an accelerated
	// placement: its --device table will not contain the requested CUDA/Vulkan
	// identifier. Fail closed and let the operator choose CPU explicitly.
	if fallback != nil && wanted == "cpu" {
		return *fallback, true
	}
	return RuntimeCatalogEntry{}, false
}

func storageProbePath(path string) string {
	path = filepath.Clean(strings.TrimSpace(path))
	if path == "" || path == "." {
		return "."
	}
	for {
		if st, err := os.Stat(path); err == nil && st.IsDir() {
			return path
		}
		parent := filepath.Dir(path)
		if parent == path {
			return path
		}
		path = parent
	}
}

func usableStorageBytes(available int64, headroomPct int) int64 {
	if available <= 0 {
		return 0
	}
	if headroomPct < 0 {
		headroomPct = 0
	}
	if headroomPct > 90 {
		headroomPct = 90
	}
	return int64(float64(available) * float64(100-headroomPct) / 100)
}

func safeAddBytes(values ...int64) int64 {
	const maxInt64 = int64(^uint64(0) >> 1)
	var total int64
	for _, value := range values {
		if value <= 0 {
			continue
		}
		if total > maxInt64-value {
			return maxInt64
		}
		total += value
	}
	return total
}

func runtimeDownloadBudget(entry RuntimeCatalogEntry) int64 {
	const unknownMain = int64(256 << 20)
	const unknownDependency = int64(512 << 20)
	total := entry.SizeBytes
	if total <= 0 {
		total = unknownMain
	}
	for _, dep := range entry.Dependencies {
		size := dep.SizeBytes
		if size <= 0 {
			size = unknownDependency
		}
		total = safeAddBytes(total, size)
	}
	return total
}

func runtimeInstallReserve(downloadBytes int64) int64 {
	const minimum = int64(256 << 20)
	if downloadBytes <= 0 {
		return minimum
	}
	reserve := safeAddBytes(downloadBytes, downloadBytes, downloadBytes, downloadBytes)
	if reserve < minimum {
		return minimum
	}
	return reserve
}

func remainingArtifactBytes(dest string, expected int64) int64 {
	if expected <= 0 {
		return 0
	}
	if st, err := os.Stat(dest); err == nil && !st.IsDir() && st.Size() >= expected {
		return 0
	}
	if st, err := os.Stat(dest + ".partial"); err == nil && !st.IsDir() && st.Size() > 0 && st.Size() < expected {
		return expected - st.Size()
	}
	return expected
}

func pathWithin(root, path string) bool {
	root = filepath.Clean(root)
	path = filepath.Clean(path)
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func runtimeManifestFromCatalog(entry RuntimeCatalogEntry) RuntimeManifest {
	return RuntimeManifest{
		Name: entry.Name, Version: entry.Version, Backend: entry.Backend, OS: entry.OS, Architecture: entry.Architecture,
		SourceURL: entry.SourceURL, SHA256: entry.SHA256, ArchiveFormat: entry.ArchiveFormat, ExecutableRel: entry.ExecutableRel,
		Dependencies: runtimeDependenciesFromCatalog(entry.Dependencies),
	}
}

func (s *Service) trustedRuntimeAlreadyInstalled(ctx context.Context, nodeID string, entry RuntimeCatalogEntry) bool {
	manifest := runtimeManifestFromCatalog(entry)
	inventoryName := manifest.Name
	if backend := strings.ToLower(strings.TrimSpace(manifest.Backend)); backend != "" {
		inventoryName += "@" + backend
	}
	var version, fingerprint, executable string
	err := s.db.QueryRowContext(ctx, `SELECT runtime_version,source_sha256,executable_path FROM managed_local_runtimes WHERE node_id=? AND runtime_name=? AND status='ready'`, nodeID, inventoryName).Scan(&version, &fingerprint, &executable)
	if err != nil || version != manifest.Version || !strings.EqualFold(fingerprint, runtimeInstallFingerprint(manifest)) {
		return false
	}
	st, err := os.Stat(executable)
	return err == nil && !st.IsDir()
}

func (s *Service) applyTrustedArtifactStorage(ctx context.Context, p HardwareProfile, cat ArtifactCatalog, req RecommendRequest, recs []Recommendation) []Recommendation {
	modelStorage := currentStorage(storageProbePath(s.modelRoot))
	runtimeStorage := currentStorage(storageProbePath(s.dataDir))
	modelUsable := usableStorageBytes(modelStorage.AvailableBytes, req.StorageHeadroomPct)
	runtimeUsable := usableStorageBytes(runtimeStorage.AvailableBytes, req.StorageHeadroomPct)
	sameTree := pathWithin(s.dataDir, s.modelRoot)
	var out []Recommendation
	for _, rec := range recs {
		model, ok := trustedModelArtifact(cat, rec)
		if !ok || model.SizeBytes <= 0 {
			continue
		}
		runtimeArtifact, ok := trustedRuntimeArtifact(cat, p, rec)
		if !ok {
			continue
		}
		runtimeDownload := runtimeDownloadBudget(runtimeArtifact)
		runtimeReserve := runtimeInstallReserve(runtimeDownload)
		if s.trustedRuntimeAlreadyInstalled(ctx, p.NodeID, runtimeArtifact) {
			runtimeDownload = 0
			runtimeReserve = 0
		}
		modelNeed := safeAddBytes(model.SizeBytes, model.SizeBytes)
		runtimeNeed := safeAddBytes(runtimeDownload, runtimeReserve)
		if sameTree {
			available := modelUsable
			if runtimeUsable > 0 && (available == 0 || runtimeUsable < available) {
				available = runtimeUsable
			}
			if available <= 0 || safeAddBytes(modelNeed, runtimeNeed) > available {
				continue
			}
		} else if modelUsable <= 0 || runtimeUsable <= 0 || modelNeed > modelUsable || runtimeNeed > runtimeUsable {
			continue
		}
		rec.ModelArtifactBytes = model.SizeBytes
		rec.RuntimeDownloadBytes = runtimeDownload
		rec.RuntimeInstallReserve = runtimeReserve
		rec.DiskRequired = safeAddBytes(model.SizeBytes, runtimeReserve)
		rec.DownloadScratch = safeAddBytes(model.SizeBytes, runtimeDownload)
		rec.Notes = append(rec.Notes, "storage budget includes the verified model artifact and managed runtime dependencies")
		out = append(out, rec)
	}
	return out
}

func (s *Service) Recommendations(ctx context.Context, profileID string, req RecommendRequest) ([]Recommendation, error) {
	p, err := s.hardwareProfile(ctx, profileID)
	if err != nil {
		return nil, err
	}
	catalog := BuiltinCatalog()
	var artifacts *ArtifactCatalog
	if s.catalog != nil {
		if _, active, e := s.catalog.Active(ctx); e == nil {
			if signed := installableModelSpecifications(active); len(signed) > 0 {
				catalog = signed
				artifacts = &active
				p = filterHardwareForCatalog(p, active)
			}
		}
	}
	baseReq := req
	if len(catalog) > baseReq.Limit && len(catalog) <= 50 {
		baseReq.Limit = len(catalog)
	}
	recs, err := Recommend(p, catalog, baseReq)
	if err != nil {
		return nil, err
	}
	if artifacts != nil {
		recs = s.applyTrustedArtifactStorage(ctx, p, *artifacts, req, recs)
		if len(recs) > req.Limit {
			recs = recs[:req.Limit]
		}
	}
	if s.llmfit != nil {
		if advisory, e := s.llmfit.ModelAdvisories(ctx, req); e == nil {
			for i := range recs {
				if a, ok := advisory[normalizeModelKey(recs[i].Model.ModelRef)]; ok {
					x := a
					recs[i].LLMFit = &x
					recs[i].EstimateSource = "llmfit"
					recs[i].EstimateConfidence = a.EstimateConfidence
					if a.MeasuredTPS != nil {
						recs[i].EstimatedTPS = a.MeasuredTPS
					} else if a.EstimatedTPS != nil {
						recs[i].EstimatedTPS = a.EstimatedTPS
					}
					recs[i].PrefillTPS = a.PrefillTPS
					recs[i].TTFTMS = a.TTFTMS
					if a.UsableContext != nil && *a.UsableContext < recs[i].ContextTokens {
						recs[i].Notes = append(recs[i].Notes, fmt.Sprintf("llmfit reports usable context %d; OnePane will verify empirically", *a.UsableContext))
					}
				}
			}
		}
	}
	return recs, nil
}
func (s *Service) hardwareProfile(ctx context.Context, idv string) (HardwareProfile, error) {
	var p HardwareProfile
	var cpu, mem, gpus, runtimes, stor string
	err := s.db.QueryRowContext(ctx, `SELECT id,node_id,fingerprint,os_name,COALESCE(os_version,''),architecture,COALESCE(kernel_version,''),cpu_json,memory_json,accelerators_json,runtimes_json,storage_json,detected_at FROM local_hardware_profiles WHERE id=?`, idv).Scan(&p.ID, &p.NodeID, &p.Fingerprint, &p.OSName, &p.OSVersion, &p.Architecture, &p.KernelVersion, &cpu, &mem, &gpus, &runtimes, &stor, &p.DetectedAt)
	if err != nil {
		return p, err
	}
	if json.Unmarshal([]byte(cpu), &p.CPU) != nil || json.Unmarshal([]byte(mem), &p.Memory) != nil || json.Unmarshal([]byte(gpus), &p.GPUs) != nil || json.Unmarshal([]byte(runtimes), &p.RuntimeProbes) != nil || json.Unmarshal([]byte(stor), &p.Storage) != nil {
		return p, errors.New("stored hardware profile is corrupt")
	}
	return p, p.Validate()
}

func (s *Service) CreatePlan(ctx context.Context, cmd CreatePlanCommand) (InstallPlan, error) {
	if strings.TrimSpace(cmd.NodeID) == "" || strings.TrimSpace(cmd.HardwareProfileID) == "" || strings.TrimSpace(cmd.RoleName) == "" || strings.TrimSpace(cmd.Recommendation.Model.ModelRef) == "" {
		return InstallPlan{}, errors.New("invalid local model plan")
	}
	p, err := s.hardwareProfile(ctx, cmd.HardwareProfileID)
	if err != nil {
		return InstallPlan{}, err
	}
	if p.NodeID != cmd.NodeID {
		return InstallPlan{}, errors.New("hardware profile belongs to another node")
	}
	idv, _ := s.ids.New("lmp")
	now := s.clock.UnixMilli()
	raw, _ := json.Marshal(cmd.Recommendation)
	useCase := cmd.UseCase
	if !validateUseCase(useCase) && len(cmd.Recommendation.Model.UseCases) > 0 {
		useCase = cmd.Recommendation.Model.UseCases[0]
	}
	if !validateUseCase(useCase) {
		return InstallPlan{}, errors.New("plan use case required")
	}
	plan := InstallPlan{ID: idv, WorkspaceID: cmd.WorkspaceID, NodeID: cmd.NodeID, HardwareProfileID: cmd.HardwareProfileID, RoleName: cmd.RoleName, UseCase: string(useCase), ModelRef: cmd.Recommendation.Model.ModelRef, SourceRef: cmd.Recommendation.Model.SourceRef, RuntimeName: cmd.Recommendation.Model.Runtime, Quantization: cmd.Recommendation.Quantization, ContextTokens: cmd.Recommendation.ContextTokens, FitLevel: cmd.Recommendation.FitLevel, RunMode: cmd.Recommendation.RunMode, EstimatedTPS: cmd.Recommendation.EstimatedTPS, MemoryRequiredBytes: cmd.Recommendation.MemoryRequired, DiskRequiredBytes: cmd.Recommendation.DiskRequired, DownloadScratchBytes: cmd.Recommendation.DownloadScratch, PlanJSON: raw, Status: PlanProposed, CreatedBy: cmd.ActorPrincipalID, Revision: 1, CreatedAt: now, UpdatedAt: now}
	err = s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		if cmd.WorkspaceID != nil {
			var st string
			if err := tx.QueryRowContext(ctx, `SELECT status FROM workspaces WHERE id=?`, *cmd.WorkspaceID).Scan(&st); err != nil {
				return err
			}
			if st != "active" {
				return errors.New("workspace inactive")
			}
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO local_model_install_plans(id,workspace_id,node_id,hardware_profile_id,role_name,use_case,model_ref,source_ref,runtime_name,quantization,context_tokens,fit_level,run_mode,estimated_tps,memory_required_bytes,disk_required_bytes,download_scratch_bytes,plan_json,status,created_by,revision,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, plan.ID, plan.WorkspaceID, plan.NodeID, plan.HardwareProfileID, plan.RoleName, plan.UseCase, plan.ModelRef, plan.SourceRef, plan.RuntimeName, plan.Quantization, plan.ContextTokens, plan.FitLevel, plan.RunMode, plan.EstimatedTPS, plan.MemoryRequiredBytes, plan.DiskRequiredBytes, plan.DownloadScratchBytes, string(plan.PlanJSON), plan.Status, plan.CreatedBy, plan.Revision, plan.CreatedAt, plan.UpdatedAt)
		if err != nil {
			return err
		}
		eid, _ := s.ids.New("evt")
		payload, _ := json.Marshal(map[string]any{"plan_id": plan.ID, "model_ref": plan.ModelRef, "runtime": plan.RuntimeName, "fit": plan.FitLevel})
		return s.events.Append(ctx, tx, event.Event{ID: eid, WorkspaceID: plan.WorkspaceID, Type: "local_ai.plan_created", AggregateType: "local_model_install_plan", AggregateID: plan.ID, ActorPrincipalID: cmd.ActorPrincipalID, Payload: payload, OccurredAt: now})
	})
	return plan, err
}

func (s *Service) ApprovePlan(ctx context.Context, planID string, expectedRevision int64, approver string) (InstallPlan, error) {
	if planID == "" || expectedRevision < 1 || approver == "" {
		return InstallPlan{}, errors.New("plan, revision and approver required")
	}
	now := s.clock.UnixMilli()
	err := s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		p, err := scanPlan(tx.QueryRowContext(ctx, planSelect, planID))
		if err != nil {
			return err
		}
		if p.Revision != expectedRevision || p.Status != PlanProposed {
			return errors.New("plan revision/state conflict")
		}
		if p.CreatedBy != nil && *p.CreatedBy == approver {
			return errors.New("plan proposer cannot self-approve")
		}
		res, err := tx.ExecContext(ctx, `UPDATE local_model_install_plans SET status='approved',approved_by=?,revision=revision+1,updated_at=? WHERE id=? AND revision=? AND status='proposed'`, approver, now, planID, expectedRevision)
		if err != nil {
			return err
		}
		n, _ := res.RowsAffected()
		if n != 1 {
			return errors.New("plan revision conflict")
		}
		eid, _ := s.ids.New("evt")
		payload, _ := json.Marshal(map[string]any{"plan_id": planID})
		return s.events.Append(ctx, tx, event.Event{ID: eid, WorkspaceID: p.WorkspaceID, Type: "local_ai.plan_approved", AggregateType: "local_model_install_plan", AggregateID: planID, ActorPrincipalID: &approver, Payload: payload, OccurredAt: now})
	})
	if err != nil {
		return InstallPlan{}, err
	}
	return s.Plan(ctx, planID)
}

const planSelect = `SELECT id,workspace_id,node_id,hardware_profile_id,role_name,use_case,model_ref,source_ref,runtime_name,quantization,context_tokens,fit_level,run_mode,estimated_tps,memory_required_bytes,disk_required_bytes,download_scratch_bytes,plan_json,status,created_by,approved_by,failure_reason,revision,created_at,updated_at FROM local_model_install_plans WHERE id=?`

type scanner interface{ Scan(...any) error }

func scanPlan(r scanner) (InstallPlan, error) {
	var p InstallPlan
	var ws, created, approved, fail sql.NullString
	var tps sql.NullFloat64
	var raw string
	if err := r.Scan(&p.ID, &ws, &p.NodeID, &p.HardwareProfileID, &p.RoleName, &p.UseCase, &p.ModelRef, &p.SourceRef, &p.RuntimeName, &p.Quantization, &p.ContextTokens, &p.FitLevel, &p.RunMode, &tps, &p.MemoryRequiredBytes, &p.DiskRequiredBytes, &p.DownloadScratchBytes, &raw, &p.Status, &created, &approved, &fail, &p.Revision, &p.CreatedAt, &p.UpdatedAt); err != nil {
		return p, err
	}
	if ws.Valid {
		p.WorkspaceID = &ws.String
	}
	if created.Valid {
		p.CreatedBy = &created.String
	}
	if approved.Valid {
		p.ApprovedBy = &approved.String
	}
	if fail.Valid {
		p.FailureReason = &fail.String
	}
	if tps.Valid {
		p.EstimatedTPS = &tps.Float64
	}
	p.PlanJSON = json.RawMessage(raw)
	return p, nil
}
func (s *Service) Plan(ctx context.Context, id string) (InstallPlan, error) {
	return scanPlan(s.db.QueryRowContext(ctx, planSelect, id))
}

// ProvisionApprovedPlan installs a managed runtime/model from trusted resolved artifacts.
// It deliberately stops at QUALIFYING: empirical qualification must prove the deployment before scheduling it.
func (s *Service) ProvisionApprovedPlan(ctx context.Context, planID string, runtime RuntimeManifest, model ModelArtifact) (inference.ModelDeployment, error) {
	// Two models may request the same runtime concurrently. Prevent one job
	// from seeing the other's newly renamed but not-yet-inventoried runtime.
	s.runtimeInstallMu.Lock()
	runtimeLocked := true
	defer func() { if runtimeLocked { s.runtimeInstallMu.Unlock() } }()
	p, err := s.Plan(ctx, planID)
	if err != nil {
		return inference.ModelDeployment{}, err
	}
	if p.Status != PlanApproved {
		return inference.ModelDeployment{}, fmt.Errorf("plan is %s, not approved", p.Status)
	}
	if runtime.Name != p.RuntimeName || model.ModelRef != p.ModelRef {
		return inference.ModelDeployment{}, errors.New("resolved artifacts do not match approved plan")
	}
	if runtime.Architecture == "" || !(strings.EqualFold(runtime.OS, "linux") || strings.EqualFold(runtime.OS, "windows") || strings.EqualFold(runtime.OS, "darwin")) {
		return inference.ModelDeployment{}, errors.New("unsupported runtime manifest")
	}
	if runtime.SHA256 == "" {
		return inference.ModelDeployment{}, errors.New("managed runtime requires trusted sha256")
	}
	backendDir := strings.ToLower(strings.TrimSpace(runtime.Backend))
	if backendDir == "" {
		backendDir = "generic"
	}
	runtimeFingerprint := runtimeInstallFingerprint(runtime)
	runtimeRoot := filepath.Join(s.dataDir, "runtimes", runtime.Name, runtime.Version, backendDir)
	if len(runtime.Dependencies) > 0 {
		runtimeRoot = filepath.Join(s.dataDir, "runtimes", runtime.Name, runtime.Version, backendDir+"-"+runtimeFingerprint[:16])
	}
	execPath := filepath.Join(runtimeRoot, runtime.ExecutableRel)
	var runtimeDownloads []string
	reuseRuntime := false
	var installedVersion, installedSHA, installedExec, installedRoot string
	runtimeInventoryName := runtime.Name
	if b := strings.ToLower(strings.TrimSpace(runtime.Backend)); b != "" {
		runtimeInventoryName += "@" + b
	}
	err = s.db.QueryRowContext(ctx, `SELECT runtime_version,source_sha256,executable_path,install_root FROM managed_local_runtimes WHERE node_id=? AND runtime_name=? AND status='ready'`, p.NodeID, runtimeInventoryName).Scan(&installedVersion, &installedSHA, &installedExec, &installedRoot)
	if err == nil && installedVersion == runtime.Version && strings.EqualFold(installedSHA, runtimeFingerprint) {
		// Runtime updates can use a versioned .rev-N generation. Trust the
		// inventory's exact fingerprint + executable, not a fixed path guess.
		managedRoot := filepath.Join(s.dataDir, "runtimes")
		if pathWithin(managedRoot, installedRoot) && pathWithin(installedRoot, installedExec) {
			if st, statErr := os.Stat(installedExec); statErr == nil && !st.IsDir() {
				reuseRuntime = true
				runtimeRoot, execPath = installedRoot, installedExec
			}
		}
	} else if err != nil && err != sql.ErrNoRows {
		return inference.ModelDeployment{}, err
	}
	if !reuseRuntime {
		if _, statErr := os.Stat(runtimeRoot); statErr == nil {
			return inference.ModelDeployment{}, errors.New("managed runtime target already exists but is not the trusted catalog installation")
		} else if !os.IsNotExist(statErr) {
			return inference.ModelDeployment{}, statErr
		}
		archive := filepath.Join(s.dataDir, "downloads", "runtime-"+runtime.Name+"-"+runtime.Version+"-"+backendDir)
		if _, err := s.fetcher.Fetch(ctx, runtime.SourceURL, archive, runtime.SHA256); err != nil {
			if !isResumableDownloadError(err) {
				_ = s.failPlan(ctx, p, err.Error())
			}
			return inference.ModelDeployment{}, err
		}
		runtimeDownloads = append(runtimeDownloads, archive)
		type runtimeDependencyDownload struct {
			dependency RuntimeDependency
			path       string
		}
		dependencyDownloads := make([]runtimeDependencyDownload, 0, len(runtime.Dependencies))
		for i, dependency := range runtime.Dependencies {
			dependencyPath := filepath.Join(s.dataDir, "downloads", fmt.Sprintf("runtime-%s-%s-%s-dependency-%02d", runtime.Name, runtime.Version, backendDir, i+1))
			if _, err := s.fetcher.Fetch(ctx, dependency.SourceURL, dependencyPath, dependency.SHA256); err != nil {
				if !isResumableDownloadError(err) {
					_ = s.failPlan(ctx, p, fmt.Sprintf("runtime dependency %s: %v", dependency.Name, err))
				}
				return inference.ModelDeployment{}, err
			}
			dependencyDownloads = append(dependencyDownloads, runtimeDependencyDownload{dependency: dependency, path: dependencyPath})
			runtimeDownloads = append(runtimeDownloads, dependencyPath)
		}

		expandedBytes, err := RuntimeArchiveExpandedBytes(archive, runtime.ArchiveFormat)
		if err != nil {
			_ = s.failPlan(ctx, p, fmt.Sprintf("inspect runtime archive: %v", err))
			return inference.ModelDeployment{}, err
		}
		for _, downloaded := range dependencyDownloads {
			n, err := RuntimeArchiveExpandedBytes(downloaded.path, downloaded.dependency.ArchiveFormat)
			if err != nil {
				_ = s.failPlan(ctx, p, fmt.Sprintf("inspect runtime dependency %s: %v", downloaded.dependency.Name, err))
				return inference.ModelDeployment{}, err
			}
			expandedBytes = safeAddBytes(expandedBytes, n)
		}
		const runtimeStorageSafety = int64(128 << 20)
		runtimeStorage := currentStorage(storageProbePath(s.dataDir))
		runtimeNeeded := safeAddBytes(expandedBytes, runtimeStorageSafety)
		if runtimeStorage.AvailableBytes <= 0 || runtimeNeeded > runtimeStorage.AvailableBytes {
			return inference.ModelDeployment{}, markResumableDownload(fmt.Errorf("insufficient storage to expand managed runtime: need %d bytes free, have %d", runtimeNeeded, runtimeStorage.AvailableBytes))
		}

		staging := runtimeRoot + ".installing"
		_ = os.RemoveAll(staging)
		if runtime.ArchiveFormat == "binary" {
			if err := os.MkdirAll(staging, 0o700); err != nil {
				return inference.ModelDeployment{}, err
			}
			dst := filepath.Join(staging, filepath.Base(runtime.ExecutableRel))
			b, err := os.ReadFile(archive)
			if err != nil {
				return inference.ModelDeployment{}, err
			}
			if err := os.WriteFile(dst, b, 0o700); err != nil {
				return inference.ModelDeployment{}, err
			}
			runtime.ExecutableRel = filepath.Base(runtime.ExecutableRel)
		} else if err := ExtractRuntimeArchive(archive, runtime.ArchiveFormat, staging); err != nil {
			_ = os.RemoveAll(staging)
			_ = s.failPlan(ctx, p, err.Error())
			return inference.ModelDeployment{}, err
		}
		for _, downloaded := range dependencyDownloads {
			if err := ExtractRuntimeArchive(downloaded.path, downloaded.dependency.ArchiveFormat, staging); err != nil {
				_ = os.RemoveAll(staging)
				_ = s.failPlan(ctx, p, fmt.Sprintf("extract runtime dependency %s: %v", downloaded.dependency.Name, err))
				return inference.ModelDeployment{}, err
			}
		}
		stagedExec := filepath.Join(staging, runtime.ExecutableRel)
		if st, err := os.Stat(stagedExec); err != nil || st.IsDir() {
			_ = os.RemoveAll(staging)
			return inference.ModelDeployment{}, errors.New("runtime executable missing after install")
		}
		if err := os.Rename(staging, runtimeRoot); err != nil {
			_ = os.RemoveAll(staging)
			return inference.ModelDeployment{}, err
		}
		execPath = filepath.Join(runtimeRoot, runtime.ExecutableRel)
	}
	if st, err := os.Stat(execPath); err != nil || st.IsDir() {
		return inference.ModelDeployment{}, errors.New("runtime executable missing after install")
	}
	runtimeID, err := s.ensureManagedRuntime(ctx, p.NodeID, runtime, runtimeRoot, execPath)
	if err != nil {
		return inference.ModelDeployment{}, err
	}
	s.runtimeInstallMu.Unlock()
	runtimeLocked = false
	for _, downloaded := range runtimeDownloads {
		_ = os.Remove(downloaded)
	}

	modelDir := s.modelRoot
	if err := os.MkdirAll(modelDir, 0o700); err != nil {
		return inference.ModelDeployment{}, err
	}
	filename := filepath.Base(model.Filename)
	if filename == "." || filename == "" {
		return inference.ModelDeployment{}, errors.New("invalid model filename")
	}
	if model.ExpectedSHA256 != "" {
		filename = strings.ToLower(model.ExpectedSHA256[:16]) + "-" + filename
	}
	modelPath := filepath.Join(modelDir, filename)
	if model.SizeBytes > 0 {
		remaining := remainingArtifactBytes(modelPath, model.SizeBytes)
		const modelStorageSafety = int64(128 << 20)
		modelStorage := currentStorage(storageProbePath(modelDir))
		needed := safeAddBytes(remaining, modelStorageSafety)
		if modelStorage.AvailableBytes <= 0 || needed > modelStorage.AvailableBytes {
			return inference.ModelDeployment{}, markResumableDownload(fmt.Errorf("insufficient storage for managed model download: need %d bytes free, have %d", needed, modelStorage.AvailableBytes))
		}
	}
	dr, err := s.fetcher.Fetch(ctx, model.SourceURL, modelPath, model.ExpectedSHA256)
	if err != nil {
		if !isResumableDownloadError(err) {
			_ = s.failPlan(ctx, p, err.Error())
		}
		return inference.ModelDeployment{}, err
	}
	provider := "local"
	arch := p.ModelRef
	quant := p.Quantization
	metadata, _ := json.Marshal(map[string]any{"managed": true, "source_ref": p.SourceRef, "install_plan_id": p.ID, "observed_sha256": dr.SHA256, "expected_sha256": model.ExpectedSHA256, "size_bytes": dr.Size})
	trust := inference.ModelQuarantined
	m, err := s.findOrRegisterManagedModel(ctx, provider, arch, quant, p, dr, metadata, trust)
	if err != nil {
		return inference.ModelDeployment{}, err
	}
	rn := runtime.Name
	rv := runtime.Version
	var rec Recommendation
	_ = json.Unmarshal(p.PlanJSON, &rec)
	cfg, _ := json.Marshal(map[string]any{"managed": true, "executable": execPath, "model_path": modelPath, "context_tokens": p.ContextTokens, "plan_id": p.ID, "placement": rec.Placement, "runtime_backend": runtime.Backend, "runtime_name": runtime.Name, "model_ref": p.ModelRef})
	dep, err := s.findOrRegisterManagedDeployment(ctx, m.ID, p, rn, rv, cfg)
	if err != nil {
		return inference.ModelDeployment{}, err
	}
	if err := s.recordManagedModel(ctx, p, m.ID, dep.ID, runtimeID, modelPath, model.ExpectedSHA256, dr); err != nil {
		return inference.ModelDeployment{}, err
	}
	if dep.Status == inference.DeploymentDiscovered {
		dep, err = s.inference.SetDeploymentStatus(ctx, inference.SetDeploymentStatusCommand{DeploymentID: dep.ID, ExpectedRevision: dep.Revision, Status: inference.DeploymentQualifying, ActorPrincipalID: p.ApprovedBy, Reason: "managed local model installed; empirical qualification required"})
		if err != nil {
			return inference.ModelDeployment{}, err
		}
	} else if dep.Status != inference.DeploymentQualifying && dep.Status != inference.DeploymentDegraded && dep.Status != inference.DeploymentReady {
		return inference.ModelDeployment{}, fmt.Errorf("existing managed deployment is in incompatible state %s", dep.Status)
	}
	_ = s.markQualifying(ctx, p.ID, p.Revision, p.ApprovedBy)
	return dep, nil
}
func (s *Service) markQualifying(ctx context.Context, idv string, rev int64, actor *string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE local_model_install_plans SET status='qualifying',revision=revision+1,updated_at=? WHERE id=? AND revision=? AND status='approved'`, s.clock.UnixMilli(), idv, rev)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return fmt.Errorf("local AI install plan revision conflict")
	}
	_ = actor // reserved for causal event attribution when plan transition events are added.
	return nil
}
func (s *Service) failPlan(ctx context.Context, p InstallPlan, reason string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE local_model_install_plans SET status='failed',failure_reason=?,revision=revision+1,updated_at=? WHERE id=? AND revision=? AND status NOT IN ('ready','cancelled','superseded')`, reason, s.clock.UnixMilli(), p.ID, p.Revision)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return fmt.Errorf("local AI install plan state conflict")
	}
	return nil
}

func (s *Service) findOrRegisterManagedModel(ctx context.Context, provider, arch, quant string, p InstallPlan, dr DownloadResult, metadata json.RawMessage, trust inference.ModelTrustState) (inference.Model, error) {
	var existing string
	err := s.db.QueryRowContext(ctx, `SELECT id FROM models WHERE model_ref=? AND COALESCE(revision_ref,'')='' AND COALESCE(weights_hash,'')=? AND COALESCE(quantization,'')=? LIMIT 1`, p.ModelRef, dr.SHA256, quant).Scan(&existing)
	if err == nil {
		m, err := s.inference.Model(ctx, existing)
		if err != nil {
			return inference.Model{}, err
		}
		if m.TrustState == inference.ModelRevoked {
			return inference.Model{}, errors.New("matching managed model is revoked")
		}
		return m, nil
	}
	if err != sql.ErrNoRows {
		return inference.Model{}, err
	}
	return s.inference.RegisterModel(ctx, inference.RegisterModelCommand{ProviderName: &provider, ModelRef: p.ModelRef, Architecture: &arch, WeightsHash: &dr.SHA256, Quantization: &quant, StaticMetadataJSON: metadata, TrustState: trust, ActorPrincipalID: p.ApprovedBy})
}

func (s *Service) findOrRegisterManagedDeployment(ctx context.Context, modelID string, p InstallPlan, runtimeName, runtimeVersion string, cfg json.RawMessage) (inference.ModelDeployment, error) {
	var existing string
	err := s.db.QueryRowContext(ctx, `SELECT id FROM model_deployments WHERE model_id=? AND node_id=? AND COALESCE(runtime_name,'')=? AND json_extract(runtime_config_json,'$.plan_id')=? LIMIT 1`, modelID, p.NodeID, runtimeName, p.ID).Scan(&existing)
	if err == nil {
		return s.inference.Deployment(ctx, existing)
	}
	if err != sql.ErrNoRows {
		return inference.ModelDeployment{}, err
	}
	rn := runtimeName
	rv := runtimeVersion
	return s.inference.RegisterDeployment(ctx, inference.RegisterDeploymentCommand{ModelID: modelID, NodeID: &p.NodeID, RuntimeName: &rn, RuntimeVersion: &rv, RuntimeConfigJSON: cfg, ContextMaxReported: &p.ContextTokens, ActorPrincipalID: p.ApprovedBy})
}

func (s *Service) ensureManagedRuntime(ctx context.Context, nodeID string, manifest RuntimeManifest, installRoot, executable string) (string, error) {
	installFingerprint := runtimeInstallFingerprint(manifest)
	now := s.clock.UnixMilli()
	runtimeInventoryName := manifest.Name
	if b := strings.ToLower(strings.TrimSpace(manifest.Backend)); b != "" {
		runtimeInventoryName += "@" + b
	}
	var existing string
	err := s.db.QueryRowContext(ctx, `SELECT id FROM managed_local_runtimes WHERE node_id=? AND runtime_name=?`, nodeID, runtimeInventoryName).Scan(&existing)
	if err == nil {
		_, err = s.db.ExecContext(ctx, `UPDATE managed_local_runtimes SET runtime_version=?,install_root=?,executable_path=?,source_url=?,source_sha256=?,status='ready',managed_by_harness=1,installed_at=COALESCE(installed_at,?),updated_at=?,revision=revision+1 WHERE id=?`, manifest.Version, installRoot, executable, manifest.SourceURL, installFingerprint, now, now, existing)
		return existing, err
	}
	if err != sql.ErrNoRows {
		return "", err
	}
	idv, err := s.ids.New("runtime")
	if err != nil {
		return "", err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO managed_local_runtimes(id,node_id,runtime_name,runtime_version,install_root,executable_path,source_url,source_sha256,status,managed_by_harness,installed_at,revision,updated_at) VALUES(?,?,?,?,?,?,?,?, 'ready',1,?,1,?)`, idv, nodeID, runtimeInventoryName, manifest.Version, installRoot, executable, manifest.SourceURL, installFingerprint, now, now)
	return idv, err
}

func (s *Service) recordManagedModel(ctx context.Context, p InstallPlan, modelID, deploymentID, runtimeID, modelPath, expectedSHA string, dr DownloadResult) error {
	now := s.clock.UnixMilli()
	var existing string
	err := s.db.QueryRowContext(ctx, `SELECT id FROM managed_local_models WHERE plan_id=?`, p.ID).Scan(&existing)
	if err == nil {
		_, err = s.db.ExecContext(ctx, `UPDATE managed_local_models SET model_id=?,deployment_id=?,runtime_id=?,local_path=?,expected_sha256=?,observed_sha256=?,size_bytes=?,status=CASE WHEN status='ready' THEN 'ready' ELSE 'qualifying' END,updated_at=?,revision=revision+1 WHERE id=?`, modelID, deploymentID, runtimeID, modelPath, nullIfEmpty(expectedSHA), dr.SHA256, dr.Size, now, existing)
		return err
	}
	if err != sql.ErrNoRows {
		return err
	}
	idv, err := s.ids.New("managedmodel")
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO managed_local_models(id,node_id,plan_id,model_id,deployment_id,runtime_id,model_ref,source_ref,local_path,expected_sha256,observed_sha256,size_bytes,status,revision,installed_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?, 'qualifying',1,?,?)`, idv, p.NodeID, p.ID, modelID, deploymentID, runtimeID, p.ModelRef, p.SourceRef, modelPath, nullIfEmpty(expectedSHA), dr.SHA256, dr.Size, now, now)
	return err
}

func nullIfEmpty(v string) any {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	return strings.TrimSpace(v)
}

// AcquireLocalEndpoint atomically loads/resolves a managed runtime and acquires
// a busy lease before releasing the residency lock. This closes the eviction
// race between endpoint resolution and inference dispatch.
func (s *Service) AcquireLocalEndpoint(ctx context.Context, deploymentID string) (string, func(), error) {
	if s == nil || s.supervisor == nil {
		return "", nil, errors.New("local runtime supervisor unavailable")
	}
	inst, err := s.supervisor.Acquire(ctx, deploymentID)
	if err != nil {
		return "", nil, err
	}
	release := func() { _ = s.supervisor.Release(context.Background(), deploymentID) }
	if inst.Status != RuntimeHealthy && inst.Status != RuntimeBusy {
		release()
		return "", nil, fmt.Errorf("managed runtime is %s", inst.Status)
	}
	if inst.BindAddress != "127.0.0.1" && inst.BindAddress != "::1" {
		release()
		return "", nil, errors.New("managed runtime endpoint is not loopback")
	}
	if err := s.supervisor.VerifyIdentity(ctx, inst); err != nil {
		release()
		return "", nil, fmt.Errorf("managed runtime process identity check failed: %w", err)
	}
	if _, err := s.supervisor.Health(ctx, inst); err != nil {
		release()
		return "", nil, fmt.Errorf("managed runtime health check failed: %w", err)
	}
	return fmt.Sprintf("http://127.0.0.1:%d", inst.Port), release, nil
}

// ResolveLocalEndpoint is retained for non-leased callers. Inference transports
// should prefer AcquireLocalEndpoint so residency is protected for the full call.
func (s *Service) ResolveLocalEndpoint(ctx context.Context, deploymentID string) (string, error) {
	if s == nil || s.supervisor == nil {
		return "", errors.New("local runtime supervisor unavailable")
	}
	inst, err := s.supervisor.Instance(ctx, deploymentID)
	if err != nil || (inst.Status != RuntimeHealthy && inst.Status != RuntimeBusy) {
		dep, depErr := s.inference.Deployment(ctx, deploymentID)
		if depErr != nil {
			return "", depErr
		}
		if dep.Status != inference.DeploymentReady && dep.Status != inference.DeploymentDegraded {
			if err != nil {
				return "", err
			}
			return "", fmt.Errorf("managed deployment is %s", dep.Status)
		}
		inst, err = s.supervisor.Start(ctx, deploymentID)
		if err != nil {
			return "", err
		}
	}
	if inst.Status != RuntimeHealthy && inst.Status != RuntimeBusy {
		return "", fmt.Errorf("managed runtime is %s", inst.Status)
	}
	if inst.BindAddress != "127.0.0.1" && inst.BindAddress != "::1" {
		return "", errors.New("managed runtime endpoint is not loopback")
	}
	if err := s.supervisor.VerifyIdentity(ctx, inst); err != nil {
		return "", fmt.Errorf("managed runtime process identity check failed: %w", err)
	}
	if _, err := s.supervisor.Health(ctx, inst); err != nil {
		return "", fmt.Errorf("managed runtime health check failed: %w", err)
	}
	if err := s.supervisor.Touch(ctx, deploymentID); err != nil {
		return "", err
	}
	return fmt.Sprintf("http://127.0.0.1:%d", inst.Port), nil
}

func (s *Service) SetLocalRuntimeBusy(ctx context.Context, deploymentID string, busy bool) error {
	if s == nil || s.supervisor == nil {
		return errors.New("local runtime supervisor unavailable")
	}
	return s.supervisor.SetBusy(ctx, deploymentID, busy)
}

func (s *Service) ReapIdleManagedRuntimes(ctx context.Context, idle time.Duration, limit int) (int, error) {
	if s == nil || s.supervisor == nil {
		return 0, errors.New("local runtime supervisor unavailable")
	}
	return s.supervisor.ReapIdle(ctx, idle, limit)
}

func (s *Service) ShutdownManagedRuntimes(ctx context.Context) error {
	if s == nil || s.supervisor == nil {
		return errors.New("local runtime supervisor unavailable")
	}
	return s.supervisor.Shutdown(ctx)
}
