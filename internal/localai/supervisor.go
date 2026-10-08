package localai

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/DigiLogicTech/OnePane/internal/clock"
	"github.com/DigiLogicTech/OnePane/internal/event"
	"github.com/DigiLogicTech/OnePane/internal/id"
	"github.com/DigiLogicTech/OnePane/internal/storage"
)

type RuntimeInstanceStatus string

const (
	RuntimeStopped  RuntimeInstanceStatus = "stopped"
	RuntimeStarting RuntimeInstanceStatus = "starting"
	RuntimeHealthy  RuntimeInstanceStatus = "healthy"
	RuntimeBusy     RuntimeInstanceStatus = "busy"
	RuntimeDraining RuntimeInstanceStatus = "draining"
	RuntimeFailed   RuntimeInstanceStatus = "failed"
	RuntimeOrphaned RuntimeInstanceStatus = "orphaned"
)

type RuntimeInstance struct {
	ID, NodeID, DeploymentID, RuntimeID, ManagedModelID, BindAddress, ProcessFingerprint string
	Port, PID                                                                            int
	Status                                                                               RuntimeInstanceStatus
	LaunchJSON, HealthJSON                                                               json.RawMessage
	StartedAt, LastSeenAt, StoppedAt                                                     *int64
	FailureReason                                                                        *string
	Revision, CreatedAt, UpdatedAt                                                       int64
}

type LaunchSpec struct {
	Executable string
	Args       []string
	Env        []string
	Dir        string
 LogPath string
}

type ProcessBackend interface {
	Start(context.Context, LaunchSpec) (int, error)
	Signal(int, os.Signal) error
	Alive(int) bool
}

type ProcessIdentity struct {
	Executable string
	Args       []string
}

type ProcessInspector interface {
	Identity(int) (ProcessIdentity, error)
}

type OSProcessBackend struct {
	mu   sync.Mutex
	cmds map[int]*exec.Cmd
}

func NewOSProcessBackend() *OSProcessBackend { return &OSProcessBackend{cmds: map[int]*exec.Cmd{}} }
func (b *OSProcessBackend) Start(ctx context.Context, spec LaunchSpec) (int, error) {
	if strings.TrimSpace(spec.Executable) == "" || filepath.IsAbs(spec.Executable) == false {
		return 0, errors.New("managed runtime executable must be an absolute path")
	}
	cmd := exec.CommandContext(ctx, spec.Executable, spec.Args...)
	cmd.Env = append([]string(nil), spec.Env...)
	if spec.Dir != "" {
		cmd.Dir = spec.Dir
	}
	// Preserve bounded startup diagnostics; the process owns no global stdout.
 if spec.LogPath != "" {
  if err:=os.MkdirAll(filepath.Dir(spec.LogPath),0o700);err!=nil{return 0,err}
  if st,err:=os.Stat(spec.LogPath);err==nil&&st.Size()>4<<20{_ = os.Rename(spec.LogPath,spec.LogPath+".previous")}
  logFile,err:=os.OpenFile(spec.LogPath,os.O_CREATE|os.O_TRUNC|os.O_WRONLY,0o600);if err!=nil{return 0,err}
  defer logFile.Close();cmd.Stdout,cmd.Stderr=logFile,logFile
 } else {cmd.Stdout, cmd.Stderr = io.Discard, io.Discard}
	if err := cmd.Start(); err != nil {
		return 0, err
	}
	pid := cmd.Process.Pid
	b.mu.Lock()
	b.cmds[pid] = cmd
	b.mu.Unlock()
	go func() { _ = cmd.Wait(); b.mu.Lock(); delete(b.cmds, pid); b.mu.Unlock() }()
	return pid, nil
}
func (b *OSProcessBackend) Signal(pid int, sig os.Signal) error {
	if pid <= 1 {
		return errors.New("invalid pid")
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	if runtime.GOOS == "windows" {
		return p.Kill()
	}
	return p.Signal(sig)
}
func (b *OSProcessBackend) Alive(pid int) bool {
	if pid <= 1 {
		return false
	}
	b.mu.Lock()
	cmd := b.cmds[pid]
	b.mu.Unlock()
	if cmd != nil {
		return cmd.ProcessState == nil || !cmd.ProcessState.Exited()
	}
	if runtime.GOOS == "windows" {
		return false
	}
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return p.Signal(syscall.Signal(0)) == nil
}
func (b *OSProcessBackend) Identity(pid int) (ProcessIdentity, error) {
	if pid <= 1 {
		return ProcessIdentity{}, errors.New("invalid pid")
	}
	b.mu.Lock()
	cmd := b.cmds[pid]
	b.mu.Unlock()
	if cmd != nil {
		return ProcessIdentity{Executable: filepath.Clean(cmd.Path), Args: append([]string(nil), cmd.Args...)}, nil
	}
	if runtime.GOOS == "windows" {
		return ProcessIdentity{}, errors.New("process identity unavailable after Windows service restart")
	}
	exe, err := os.Readlink(fmt.Sprintf("/proc/%d/exe", pid))
	if err != nil {
		return ProcessIdentity{}, err
	}
	raw, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
	if err != nil {
		return ProcessIdentity{}, err
	}
	parts := bytes.Split(bytes.TrimRight(raw, "\x00"), []byte{0})
	args := make([]string, 0, len(parts))
	for _, part := range parts {
		args = append(args, string(part))
	}
	return ProcessIdentity{Executable: filepath.Clean(exe), Args: args}, nil
}

type RuntimeSupervisor struct {
	db        *sql.DB
	tx        storage.Transactor
	events    event.Store
	ids       id.Generator
	clock     clock.Clock
	dataDir   string
	modelRoot string
	processes ProcessBackend
	http      *http.Client

	residencyMu          sync.Mutex
	activityMu           sync.Mutex
	activeRequests       map[string]int
	residencyHeadroomPct int
}

func NewRuntimeSupervisor(db *sql.DB, tx storage.Transactor, clk clock.Clock, dataDir string, backend ProcessBackend) *RuntimeSupervisor {
	if backend == nil {
		backend = NewOSProcessBackend()
	}
	return &RuntimeSupervisor{db: db, tx: tx, events: event.Store{}, ids: id.Generator{}, clock: clk, dataDir: dataDir, modelRoot: filepath.Join(dataDir, "models", "managed"), processes: backend, activeRequests: map[string]int{}, residencyHeadroomPct: 10, http: &http.Client{Timeout: 3 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}}
}

func (s *RuntimeSupervisor) SetModelRoot(path string) {
	path = strings.TrimSpace(path)
	if path == "" {
		path = filepath.Join(s.dataDir, "models", "managed")
	}
	s.modelRoot = filepath.Clean(path)
}

func (s *RuntimeSupervisor) SetResidencyHeadroom(percent int) {
	if percent < 0 {
		percent = 0
	}
	if percent > 50 {
		percent = 50
	}
	s.residencyHeadroomPct = percent
}

// SetBusy reference-counts active inference requests so an idle reaper or
// pressure eviction can never stop a runtime that is serving work.
func (s *RuntimeSupervisor) SetBusy(ctx context.Context, deploymentID string, busy bool) error {
	deploymentID = strings.TrimSpace(deploymentID)
	if deploymentID == "" {
		return errors.New("deployment id required")
	}
	s.activityMu.Lock()
	defer s.activityMu.Unlock()
	n := s.activeRequests[deploymentID]
	if busy {
		n++
		s.activeRequests[deploymentID] = n
	} else if n > 1 {
		n--
		s.activeRequests[deploymentID] = n
	} else {
		delete(s.activeRequests, deploymentID)
		n = 0
	}
	now := s.clock.UnixMilli()
	if n > 0 {
		_, err := s.db.ExecContext(ctx, `UPDATE local_runtime_instances SET status='busy',last_seen_at=?,updated_at=?,revision=revision+1 WHERE deployment_id=? AND status IN ('healthy','busy')`, now, now, deploymentID)
		return err
	}
	_, err := s.db.ExecContext(ctx, `UPDATE local_runtime_instances SET status='healthy',last_seen_at=?,updated_at=?,revision=revision+1 WHERE deployment_id=? AND status='busy'`, now, now, deploymentID)
	return err
}

type runtimeConfig struct {
	Managed        bool          `json:"managed"`
	Executable     string        `json:"executable"`
	ModelPath      string        `json:"model_path"`
	ContextTokens  int64         `json:"context_tokens"`
	PlanID         string        `json:"plan_id"`
	Placement      PlacementPlan `json:"placement"`
	RuntimeBackend string        `json:"runtime_backend,omitempty"`
	EnginePath     string        `json:"engine_path,omitempty"`
	ModelRef       string        `json:"model_ref,omitempty"`
}

type managedRefs struct{ RuntimeID, ModelID string }

func (s *RuntimeSupervisor) resolve(ctx context.Context, deploymentID string) (runtimeConfig, managedRefs, string, error) {
	var raw, node string
	if err := s.db.QueryRowContext(ctx, `SELECT runtime_config_json,node_id FROM model_deployments WHERE id=? AND node_id IS NOT NULL`, deploymentID).Scan(&raw, &node); err != nil {
		return runtimeConfig{}, managedRefs{}, "", err
	}
	var cfg runtimeConfig
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return cfg, managedRefs{}, "", err
	}
	if !cfg.Managed || cfg.ContextTokens <= 0 {
		return cfg, managedRefs{}, "", errors.New("deployment is not a managed local runtime")
	}
	var refs managedRefs
	if err := s.db.QueryRowContext(ctx, `SELECT runtime_id,id FROM managed_local_models WHERE deployment_id=? AND status IN ('qualifying','ready')`, deploymentID).Scan(&refs.RuntimeID, &refs.ModelID); err != nil {
		return cfg, refs, "", err
	}
	var exe, modelPath string
	if err := s.db.QueryRowContext(ctx, `SELECT executable_path FROM managed_local_runtimes WHERE id=? AND status='ready'`, refs.RuntimeID).Scan(&exe); err != nil {
		return cfg, refs, "", err
	}
	if err := s.db.QueryRowContext(ctx, `SELECT local_path FROM managed_local_models WHERE id=?`, refs.ModelID).Scan(&modelPath); err != nil {
		return cfg, refs, "", err
	}
	// managed_local_runtimes records the currently preferred version of a runtime.
	// Existing deployments may remain pinned to an older versioned executable;
	// their immutable deployment config remains authoritative as long as it is
	// still under the managed runtime root. Model inventory remains exact.
	_ = exe
	if filepath.Clean(modelPath) != filepath.Clean(cfg.ModelPath) {
		return cfg, refs, "", errors.New("managed model inventory does not match deployment config")
	}
	root := filepath.Join(s.dataDir, "runtimes")
	rel, err := filepath.Rel(root, exe)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return cfg, refs, "", errors.New("runtime executable escapes managed runtime root")
	}
	modelRoot := s.modelRoot
	if strings.TrimSpace(modelRoot) == "" {
		modelRoot = filepath.Join(s.dataDir, "models", "managed")
	}
	rel, err = filepath.Rel(modelRoot, modelPath)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return cfg, refs, "", errors.New("model path escapes managed model root")
	}
	if st, err := os.Stat(exe); err != nil || st.IsDir() || (runtime.GOOS != "windows" && st.Mode()&0111 == 0) {
		return cfg, refs, "", errors.New("managed runtime executable unavailable")
	}
	if st, err := os.Stat(modelPath); err != nil {
        return cfg, refs, "", fmt.Errorf("managed model file missing or inaccessible at %s: %w",modelPath,err)
    } else if st.IsDir() && !strings.EqualFold(cfg.RuntimeBackend, "colibri") {
        return cfg, refs, "", fmt.Errorf("managed model path is not a file: %s",modelPath)
    }
	if strings.EqualFold(cfg.RuntimeBackend, "colibri") {
		if strings.TrimSpace(cfg.EnginePath) == "" {
			engineName := "colibri"
			if runtime.GOOS == "windows" {
				engineName = "colibri.exe"
			}
			cfg.EnginePath = filepath.Join(filepath.Dir(cfg.Executable), engineName)
		}
		if st, err := os.Stat(cfg.EnginePath); err != nil || st.IsDir() {
			return cfg, refs, "", errors.New("Colibri engine unavailable")
		}
	}
	return cfg, refs, node, nil
}

type residentRuntime struct {
	DeploymentID string
	Placement    PlacementPlan
	MemoryBytes  int64
	LastUsedAt   int64
}

func placementKey(d PlacementDevice) string {
	if strings.TrimSpace(d.DeviceID) != "" {
		return strings.TrimSpace(d.DeviceID)
	}
	return fmt.Sprintf("%s:%d", strings.ToLower(d.Backend), d.DeviceIndex)
}

// ensureCapacity performs per-device pressure-aware hot swapping. It never
// treats heterogeneous accelerators as one fictional VRAM pool. Only healthy,
// idle runtimes are evicted; busy runtimes are protected by the runtime lease.
func (s *RuntimeSupervisor) ensureCapacity(ctx context.Context, deploymentID, nodeID string) error {
	var targetBytes int64
	var targetPlanJSON string
	if err := s.db.QueryRowContext(ctx, `SELECT p.memory_required_bytes,p.plan_json FROM managed_local_models m JOIN local_model_install_plans p ON p.id=m.plan_id WHERE m.deployment_id=?`, deploymentID).Scan(&targetBytes, &targetPlanJSON); err != nil {
		return err
	}
	var targetRec Recommendation
	_ = json.Unmarshal([]byte(targetPlanJSON), &targetRec)
	var raw string
	if err := s.db.QueryRowContext(ctx, `SELECT accelerators_json FROM local_hardware_profiles WHERE node_id=? ORDER BY detected_at DESC LIMIT 1`, nodeID).Scan(&raw); err != nil {
		return err
	}
	var gpus []GPU
	if err := json.Unmarshal([]byte(raw), &gpus); err != nil {
		return err
	}
	if targetRec.Placement.Mode == "" {
		p := HardwareProfile{NodeID: nodeID, OSName: "linux", Architecture: "unknown", CPU: CPU{Name: "cpu", LogicalCores: 1}, Memory: Memory{TotalBytes: 1, AvailableBytes: 1}, Storage: Storage{AvailableBytes: 1}, GPUs: gpus}
		targetRec.Placement, _, _, _ = bestPlacement(p, targetBytes, "")
	}
	if targetRec.Placement.Mode == PlacementCPUOnly {
		return nil
	}
	capacity := map[string]int64{}
	for _, g := range gpus {
		for _, b := range gpuBackends(g) {
			k := placementKey(PlacementDevice{DeviceID: g.DeviceID, Backend: b, DeviceIndex: g.DeviceIndex})
			usable := g.VRAMBytes * int64(100-s.residencyHeadroomPct) / 100
			if usable > capacity[k] {
				capacity[k] = usable
			}
		}
	}
	target := map[string]int64{}
	for _, d := range targetRec.Placement.Devices {
		if d.Kind == "accelerator" {
			target[placementKey(d)] += d.AllocatedBytes
		}
	}
	for k, n := range target {
		if capacity[k] <= 0 || n > capacity[k] {
			return fmt.Errorf("managed placement requires %d bytes on %s but usable device memory is %d", n, k, capacity[k])
		}
	}

	rows, err := s.db.QueryContext(ctx, `SELECT i.deployment_id,p.memory_required_bytes,p.plan_json,COALESCE(i.last_seen_at,i.started_at,i.updated_at),i.status FROM local_runtime_instances i JOIN managed_local_models m ON m.deployment_id=i.deployment_id JOIN local_model_install_plans p ON p.id=m.plan_id WHERE i.node_id=? AND i.deployment_id<>? AND i.status IN ('healthy','busy') ORDER BY COALESCE(i.last_seen_at,i.started_at,i.updated_at) ASC`, nodeID, deploymentID)
	if err != nil {
		return err
	}
	defer rows.Close()
	var residents []residentRuntime
	used := map[string]int64{}
	for rows.Next() {
		var x residentRuntime
		var planJSON, status string
		if err := rows.Scan(&x.DeploymentID, &x.MemoryBytes, &planJSON, &x.LastUsedAt, &status); err != nil {
			return err
		}
		var rec Recommendation
		_ = json.Unmarshal([]byte(planJSON), &rec)
		x.Placement = rec.Placement
		if x.Placement.Mode == "" {
			continue
		}
		for _, d := range x.Placement.Devices {
			if d.Kind == "accelerator" {
				used[placementKey(d)] += d.AllocatedBytes
			}
		}
		if status == string(RuntimeHealthy) {
			residents = append(residents, x)
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	fits := func() bool {
		for k, n := range target {
			if used[k]+n > capacity[k] {
				return false
			}
		}
		return true
	}
	if fits() {
		return nil
	}
	for _, x := range residents {
		if err := s.Stop(ctx, x.DeploymentID); err != nil {
			continue
		}
		for _, d := range x.Placement.Devices {
			if d.Kind == "accelerator" {
				k := placementKey(d)
				used[k] -= d.AllocatedBytes
				if used[k] < 0 {
					used[k] = 0
				}
			}
		}
		if fits() {
			return nil
		}
	}
	return fmt.Errorf("insufficient evictable accelerator memory for managed placement")
}

func llamaPlacementArgs(plan PlacementPlan) []string {
	var accelerators []PlacementDevice
	for _, d := range plan.Devices {
		if d.Kind == "accelerator" {
			accelerators = append(accelerators, d)
		}
	}
	deviceArgs := func(devs []PlacementDevice) []string {
		names := make([]string, 0, len(devs))
		for _, d := range devs {
			name := strings.TrimSpace(d.RuntimeDevice)
			if name == "" {
				name = runtimeDeviceName(d.Backend, d.DeviceIndex)
			}
			if name != "" {
				names = append(names, name)
			}
		}
		if len(names) == len(devs) && len(names) > 0 {
			return []string{"--device", strings.Join(names, ",")}
		}
		return nil
	}
	splitArgs := func(mode string, devs []PlacementDevice) []string {
		selected := deviceArgs(devs)
		args := append([]string{}, selected...)
		mainGPU := 0
		if len(selected) == 0 && len(devs) > 0 {
			mainGPU = devs[0].DeviceIndex
		}
		args = append(args, "--split-mode", mode, "--main-gpu", strconv.Itoa(mainGPU), "--n-gpu-layers", "all")
		if len(devs) > 1 {
			shares := make([]string, 0, len(devs))
			for _, d := range devs {
				v := d.Share
				if v <= 0 && d.CapacityBytes > 0 {
					v = float64(d.AllocatedBytes) / float64(d.CapacityBytes)
				}
				shares = append(shares, strconv.FormatFloat(v, 'f', 6, 64))
			}
			args = append(args, "--tensor-split", strings.Join(shares, ","))
		}
		return args
	}

	switch plan.Mode {
	case PlacementCPUOnly:
		return []string{"--device", "none", "--n-gpu-layers", "0"}
	case PlacementSingleDevice:
		if len(accelerators) == 0 {
			return nil
		}
		return splitArgs("none", accelerators[:1])
	case PlacementLayerSharded:
		return splitArgs("layer", accelerators)
	case PlacementRowSharded:
		return splitArgs("row", accelerators)
	case PlacementTensorSharded:
		return splitArgs("tensor", accelerators)
	case PlacementCPUOffload:
		args := append([]string{}, deviceArgs(accelerators)...)
		args = append(args, "--n-gpu-layers", "auto")
		if len(accelerators) > 1 {
			args = append(args, "--split-mode", "layer")
			shares := make([]string, 0, len(accelerators))
			for _, d := range accelerators {
				v := d.Share
				if v <= 0 && d.CapacityBytes > 0 {
					v = float64(d.AllocatedBytes) / float64(d.CapacityBytes)
				}
				shares = append(shares, strconv.FormatFloat(v, 'f', 6, 64))
			}
			args = append(args, "--tensor-split", strings.Join(shares, ","))
		}
		return args
	default:
		return nil
	}
}

// Validate accelerator identity against the selected runtime build. A detected
// CUDA0 GPU is not proof that a Vulkan-only executable can address CUDA0.
func validateLlamaPlacementDevices(executable,backend string,plan PlacementPlan) error {
 args:=llamaPlacementArgs(plan)
 var selected string
 for i:=0;i+1<len(args);i++{if args[i]=="--device"{selected=args[i+1];break}}
 if selected==""||selected=="none"{return nil}
 for _,name:=range strings.Split(selected,","){
  name=strings.TrimSpace(name)
  if strings.EqualFold(backend,"vulkan")&&strings.HasPrefix(strings.ToLower(name),"cuda")||
    strings.EqualFold(backend,"cuda")&&strings.HasPrefix(strings.ToLower(name),"vulkan")||
    strings.EqualFold(backend,"cpu") {
    return fmt.Errorf("runtime backend %s cannot address device %s; change Compute placement or repair the matching llama.cpp backend",backend,name)
  }
 }
 ctx,cancel:=context.WithTimeout(context.Background(),8*time.Second)
 defer cancel()
 out,err:=exec.CommandContext(ctx,executable,"--list-devices").CombinedOutput()
 if err!=nil{return fmt.Errorf("list devices from managed %s backend: %w: %s",backend,err,strings.TrimSpace(string(out)))}
 known:=map[string]bool{}
 // Different llama.cpp builds format the list as either "CUDA0: ..." or
 // "- CUDA0: ..."; some put devices after a log prefix. Match complete
 // device tokens rather than assuming they occupy the first column.
 for _,line:=range strings.Split(string(out),"\n"){
  for _,word:=range strings.Fields(line){
   candidate:=strings.Trim(word," \t:,*()[]")
   lower:=strings.ToLower(candidate)
   if (strings.HasPrefix(lower,"cuda")||strings.HasPrefix(lower,"vulkan")||
       strings.HasPrefix(lower,"rocm")||strings.HasPrefix(lower,"sycl")||
       strings.HasPrefix(lower,"metal"))&&len(lower)>4 {
    known[lower]=true
   }
  }
 }
 for _,name:=range strings.Split(selected,","){
  if !known[strings.ToLower(strings.TrimSpace(name))]{
   return fmt.Errorf("selected runtime %s does not advertise %s in --list-devices; re-detect hardware or change Compute placement",backend,name)
  }
 }
 return nil
}

func reserveLoopbackPort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

func launchFingerprint(exe, model string, context int64, port int) string {
	raw, _ := json.Marshal(map[string]any{"executable": filepath.Clean(exe), "model": filepath.Clean(model), "context": context, "host": "127.0.0.1", "port": port})
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
func findPythonExecutable() (string, error) {
	for _, name := range []string{"py.exe", "python.exe", "python3", "python"} {
		if p, err := exec.LookPath(name); err == nil {
			if abs, e := filepath.Abs(p); e == nil {
				return abs, nil
			}
			return p, nil
		}
	}
	return "", errors.New("Colibri serve mode requires Python 3.10+; install the Colibri component from Models")
}

func (s *RuntimeSupervisor) Start(ctx context.Context, deploymentID string) (RuntimeInstance, error) {
	s.residencyMu.Lock()
	defer s.residencyMu.Unlock()
	return s.startLocked(ctx, deploymentID)
}

func (s *RuntimeSupervisor) startLocked(ctx context.Context, deploymentID string) (RuntimeInstance, error) {
	cfg, refs, nodeID, err := s.resolve(ctx, deploymentID)
	if err != nil {
		return RuntimeInstance{}, err
	}
	var existing *RuntimeInstance
	if old, err := s.Instance(ctx, deploymentID); err == nil {
		existing = &old
		if (old.Status == RuntimeHealthy || old.Status == RuntimeBusy) && s.processes.Alive(old.PID) {
			return old, nil
		}
		if old.Status == RuntimeStarting && s.processes.Alive(old.PID) {
			return s.waitHealthy(ctx, old.ID, 120*time.Second)
		}
	}
	if !strings.EqualFold(cfg.RuntimeBackend,"colibri"){
		if err:=validateLlamaPlacementDevices(cfg.Executable,cfg.RuntimeBackend,cfg.Placement);err!=nil{return RuntimeInstance{},err}
	}
	if err := s.ensureCapacity(ctx, deploymentID, nodeID); err != nil {
		return RuntimeInstance{}, err
	}
	port, err := reserveLoopbackPort()
	if err != nil {
		return RuntimeInstance{}, err
	}
	executable := cfg.Executable
	args := []string{"-m", cfg.ModelPath, "--host", "127.0.0.1", "--port", strconv.Itoa(port), "-c", strconv.FormatInt(cfg.ContextTokens, 10)}
	if strings.EqualFold(cfg.RuntimeBackend, "colibri") {
		py, err := findPythonExecutable()
		if err != nil {
			return RuntimeInstance{}, err
		}
		executable = py
		args = nil
		if strings.EqualFold(filepath.Base(py), "py.exe") {
			args = append(args, "-3")
		}
		args = append(args, cfg.Executable, "--model", cfg.ModelPath, "--engine", cfg.EnginePath, "--host", "127.0.0.1", "--port", strconv.Itoa(port), "--model-id", firstNonEmpty(cfg.ModelRef, "onepane-colibri"))
	} else {
        // Embedding models use a pooled-vector endpoint, not chat completions.
        // Preserve the model's declared use case from its approved install plan.
        var purpose string
        if cfg.PlanID!="" {
          _ = s.db.QueryRowContext(ctx,"SELECT use_case FROM local_model_install_plans WHERE id=?",cfg.PlanID).Scan(&purpose)
        }
        if purpose==string(UseEmbedding) {args=append(args,"--embeddings","--pooling","mean")}
		args = append(args, llamaPlacementArgs(cfg.Placement)...)
	}
	launch, _ := json.Marshal(map[string]any{"executable": executable, "args": args, "context_tokens": cfg.ContextTokens, "host": "127.0.0.1", "port": port, "runtime_backend": cfg.RuntimeBackend})
	now := s.clock.UnixMilli()
	instanceID, _ := s.ids.New("lri")
	if existing != nil {
		instanceID = existing.ID
	}
	inst := RuntimeInstance{ID: instanceID, NodeID: nodeID, DeploymentID: deploymentID, RuntimeID: refs.RuntimeID, ManagedModelID: refs.ModelID, BindAddress: "127.0.0.1", Port: port, Status: RuntimeStarting, ProcessFingerprint: launchFingerprint(executable, cfg.ModelPath, cfg.ContextTokens, port), LaunchJSON: launch, HealthJSON: json.RawMessage(`{}`), Revision: 1, CreatedAt: now, UpdatedAt: now}
	err = s.tx.Within(ctx, func(ctx context.Context, tx storage.Tx) error {
		if existing == nil {
			if _, err := tx.ExecContext(ctx, `INSERT INTO local_runtime_instances(id,node_id,deployment_id,runtime_id,managed_model_id,bind_address,port,status,process_fingerprint,launch_json,health_json,revision,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, inst.ID, inst.NodeID, inst.DeploymentID, inst.RuntimeID, inst.ManagedModelID, inst.BindAddress, inst.Port, inst.Status, inst.ProcessFingerprint, string(inst.LaunchJSON), string(inst.HealthJSON), inst.Revision, now, now); err != nil {
				return err
			}
		} else {
			res, err := tx.ExecContext(ctx, `UPDATE local_runtime_instances SET runtime_id=?,managed_model_id=?,bind_address=?,port=?,pid=NULL,status='starting',process_fingerprint=?,launch_json=?,health_json='{}',started_at=NULL,last_seen_at=NULL,stopped_at=NULL,failure_reason=NULL,revision=revision+1,updated_at=? WHERE id=? AND status IN ('stopped','failed','orphaned')`, inst.RuntimeID, inst.ManagedModelID, inst.BindAddress, inst.Port, inst.ProcessFingerprint, string(inst.LaunchJSON), now, inst.ID)
			if err != nil {
				return err
			}
			n, _ := res.RowsAffected()
			if n != 1 {
				return errors.New("runtime instance restart state conflict")
			}
		}
		eid, _ := s.ids.New("evt")
		payload, _ := json.Marshal(map[string]any{"runtime_instance_id": inst.ID, "deployment_id": deploymentID, "port": port})
		return s.events.Append(ctx, tx, event.Event{ID: eid, Type: "local_ai.runtime_starting", AggregateType: "local_runtime_instance", AggregateID: inst.ID, Payload: payload, OccurredAt: now})
	})
	if err != nil {
		return RuntimeInstance{}, err
	}
	runtimeHome := filepath.Join(s.dataDir, "runtime-home")
	if err := os.MkdirAll(runtimeHome, 0o700); err != nil {
		_ = s.markFailed(ctx, inst.ID, err.Error())
		return RuntimeInstance{}, err
	}
	env := append([]string(nil), os.Environ()...)
	env = append(env, "ONEPANE_MANAGED_RUNTIME=1", "HOME="+runtimeHome)
	if runtime.GOOS != "windows" {
		env = append(env, "LD_LIBRARY_PATH="+filepath.Dir(cfg.Executable))
	}
	if strings.EqualFold(cfg.RuntimeBackend, "colibri") {
		env = append(env, "COLI_MODEL="+cfg.ModelPath, "COLI_MODEL_ID="+firstNonEmpty(cfg.ModelRef, "onepane-colibri"))
	}
	pid, err := s.processes.Start(context.Background(), LaunchSpec{Executable: executable, Args: args, Env: env, Dir: filepath.Dir(cfg.Executable),LogPath: filepath.Join(s.dataDir,"components","logs","local-runtime-"+inst.ID+".log")})
	if err != nil {
		_ = s.markFailed(context.Background(), inst.ID, err.Error())
		return RuntimeInstance{}, err
	}
	_, err = s.db.ExecContext(ctx, `UPDATE local_runtime_instances SET pid=?,started_at=?,updated_at=?,revision=revision+1 WHERE id=? AND status='starting'`, pid, now, now, inst.ID)
	if err != nil {
		_ = s.processes.Signal(pid, syscall.SIGTERM)
		return RuntimeInstance{}, err
	}
	return s.waitHealthy(ctx, inst.ID, 120*time.Second)
}

func (s *RuntimeSupervisor) Acquire(ctx context.Context, deploymentID string) (RuntimeInstance, error) {
	s.residencyMu.Lock()
	defer s.residencyMu.Unlock()
	inst, err := s.startLocked(ctx, deploymentID)
	if err != nil {
		return RuntimeInstance{}, err
	}
	if err := s.SetBusy(ctx, deploymentID, true); err != nil {
		return RuntimeInstance{}, err
	}
	return inst, nil
}

func (s *RuntimeSupervisor) Release(ctx context.Context, deploymentID string) error {
	return s.SetBusy(ctx, deploymentID, false)
}

// Diagnoses failures without exposing arbitrary large runtime output to clients.
func (s *RuntimeSupervisor) runtimeStartLogTail(idv string) string {
 path:=filepath.Join(s.dataDir,"components","logs","local-runtime-"+idv+".log")
 f,err:=os.Open(path);if err!=nil{return ""}
 defer f.Close()
 st,err:=f.Stat();if err!=nil{return ""}
 start:=st.Size()-900;if start<0{start=0}
 if _,err=f.Seek(start,io.SeekStart);err!=nil{return ""}
 buf,err:=io.ReadAll(io.LimitReader(f,900));if err!=nil{return ""}
 tail:=strings.TrimSpace(string(buf))
 if tail==""{return ""}
 return " · runtime log tail: "+tail
}
func (s *RuntimeSupervisor) waitHealthy(ctx context.Context, idv string, timeout time.Duration) (RuntimeInstance, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		inst, err := s.instanceByID(ctx, idv)
		if err != nil {
			return inst, err
		}
		if !s.processes.Alive(inst.PID) {
			reason:="runtime exited before becoming healthy"+s.runtimeStartLogTail(idv)
   _ = s.markFailed(ctx,idv,reason)
   return RuntimeInstance{}, errors.New(reason)
		}
		health, err := s.Health(ctx, inst)
		if err == nil {
			now := s.clock.UnixMilli()
			raw, _ := json.Marshal(health)
			if _, err := s.db.ExecContext(ctx, `UPDATE local_runtime_instances SET status='healthy',health_json=?,last_seen_at=?,updated_at=?,revision=revision+1 WHERE id=?`, string(raw), now, now, idv); err != nil {
				return RuntimeInstance{}, err
			}
			return s.instanceByID(ctx, idv)
		}
		select {
		case <-ctx.Done():
			return RuntimeInstance{}, ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
	reason:="managed runtime health check timed out"+s.runtimeStartLogTail(idv)
 _ = s.markFailed(ctx,idv,reason)
 return RuntimeInstance{}, errors.New(reason)
}

func (s *RuntimeSupervisor) Health(ctx context.Context, inst RuntimeInstance) (map[string]any, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("http://127.0.0.1:%d/health", inst.Port), nil)
	resp, err := s.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("health HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if len(bytes.TrimSpace(body)) == 0 {
		out = map[string]any{"status": "ok"}
	} else if err := json.Unmarshal(body, &out); err != nil {
		out = map[string]any{"status": "ok", "raw": string(body)}
	}
	return out, nil
}

func (s *RuntimeSupervisor) VerifyIdentity(ctx context.Context, inst RuntimeInstance) error {
	if inst.PID <= 1 || !s.processes.Alive(inst.PID) {
		return errors.New("runtime process is not alive")
	}
	inspector, ok := s.processes.(ProcessInspector)
	if !ok {
		return errors.New("runtime process identity cannot be inspected")
	}
	cfg, _, _, err := s.resolve(ctx, inst.DeploymentID)
	if err != nil {
		return err
	}
	ident, err := inspector.Identity(inst.PID)
	if err != nil {
		return err
	}
	if strings.EqualFold(cfg.RuntimeBackend, "colibri") {
		py, err := findPythonExecutable()
		if err != nil {
			return err
		}
		serverFound := false
		for _, a := range ident.Args {
			if filepath.Clean(a) == filepath.Clean(cfg.Executable) {
				serverFound = true
				break
			}
		}
		if filepath.Clean(ident.Executable) != filepath.Clean(py) || !serverFound || !argsContainPair(ident.Args, "--model", cfg.ModelPath) || !argsContainPair(ident.Args, "--host", "127.0.0.1") || !argsContainPair(ident.Args, "--port", strconv.Itoa(inst.Port)) {
			return errors.New("Colibri runtime process identity mismatch")
		}
		return nil
	}
	if filepath.Clean(ident.Executable) != filepath.Clean(cfg.Executable) || !argsContainPair(ident.Args, "-m", cfg.ModelPath) || !argsContainPair(ident.Args, "--host", "127.0.0.1") || !argsContainPair(ident.Args, "--port", strconv.Itoa(inst.Port)) {
		return errors.New("runtime process identity mismatch")
	}
	return nil
}

// StopIfIdle releases CPU/GPU memory after an Agent Check without evicting a
// runtime used by concurrent inference. Both locks are held through Stop:
// Acquire takes residencyMu then SetBusy (activityMu), so a new inference
// cannot enter between the safety check and process termination.
func (s *RuntimeSupervisor) StopIfIdle(ctx context.Context, deploymentID string) (bool, error) {
 if s==nil {return false,nil}
 deploymentID=strings.TrimSpace(deploymentID)
 if deploymentID=="" {return false,errors.New("deployment id required")}
 s.residencyMu.Lock()
 defer s.residencyMu.Unlock()
 s.activityMu.Lock()
 defer s.activityMu.Unlock()
 if s.activeRequests[deploymentID]>0 {return false,nil}
 inst,err:=s.Instance(ctx,deploymentID)
 if errors.Is(err,sql.ErrNoRows){return false,nil}
 if err!=nil{return false,err}
 switch inst.Status {
 case RuntimeHealthy,RuntimeStarting:
  if err:=s.Stop(ctx,deploymentID);err!=nil{return false,err}
  return true,nil
 case RuntimeBusy:
  // A stale busy marker is not proof that no other process owns the model.
  return false,nil
 default:
  // Already stopped, failed or orphaned: never signal an unknown process.
  return false,nil
 }
}

func (s *RuntimeSupervisor) Stop(ctx context.Context, deploymentID string) error {
	inst, err := s.Instance(ctx, deploymentID)
	if err != nil {
		return err
	}
	markOrphaned := func(reason string) error {
		now := s.clock.UnixMilli()
		res, e := s.db.ExecContext(ctx, `UPDATE local_runtime_instances SET status='orphaned',failure_reason=?,updated_at=?,revision=revision+1 WHERE id=? AND revision=?`, reason, now, inst.ID, inst.Revision)
		if e != nil {
			return e
		}
		n, e := res.RowsAffected()
		if e != nil {
			return e
		}
		if n != 1 {
			return fmt.Errorf("runtime instance state conflict")
		}
		return nil
	}
	if inst.PID > 1 && s.processes.Alive(inst.PID) {
		if err := s.VerifyIdentity(ctx, inst); err != nil {
			if e := markOrphaned("refused to signal process: " + err.Error()); e != nil {
				return fmt.Errorf("refusing to signal unverified runtime process (%v); additionally failed to persist orphaned state: %w", err, e)
			}
			return fmt.Errorf("refusing to signal unverified runtime process: %w", err)
		}
	}
	now := s.clock.UnixMilli()
	res, err := s.db.ExecContext(ctx, `UPDATE local_runtime_instances SET status='draining',updated_at=?,revision=revision+1 WHERE id=? AND revision=?`, now, inst.ID, inst.Revision)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return fmt.Errorf("runtime instance state conflict")
	}
	inst.Revision++
	if inst.PID > 1 && s.processes.Alive(inst.PID) {
		if err := s.processes.Signal(inst.PID, syscall.SIGTERM); err != nil && s.processes.Alive(inst.PID) {
			return fmt.Errorf("signal managed runtime: %w", err)
		}
	}
	deadline := time.Now().Add(5 * time.Second)
	for inst.PID > 1 && s.processes.Alive(inst.PID) && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	if inst.PID > 1 && s.processes.Alive(inst.PID) {
		if err := s.processes.Signal(inst.PID, syscall.SIGKILL); err != nil && s.processes.Alive(inst.PID) {
			return fmt.Errorf("kill managed runtime: %w", err)
		}
		deadline = time.Now().Add(2 * time.Second)
		for s.processes.Alive(inst.PID) && time.Now().Before(deadline) {
			time.Sleep(50 * time.Millisecond)
		}
		if s.processes.Alive(inst.PID) {
			if e := markOrphaned("managed runtime remained alive after SIGKILL"); e != nil {
				return e
			}
			return errors.New("managed runtime remained alive after SIGKILL")
		}
	}
	now = s.clock.UnixMilli()
	res, err = s.db.ExecContext(ctx, `UPDATE local_runtime_instances SET status='stopped',stopped_at=?,updated_at=?,revision=revision+1 WHERE id=? AND revision=? AND status='draining'`, now, now, inst.ID, inst.Revision)
	if err != nil {
		return err
	}
	n, err = res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return fmt.Errorf("runtime instance stop state conflict")
	}
	return nil
}

func (s *RuntimeSupervisor) Recover(ctx context.Context) error {
	s.activityMu.Lock()
	s.activeRequests = map[string]int{}
	s.activityMu.Unlock()
	rows, err := s.db.QueryContext(ctx, `SELECT i.id,i.pid,i.port,i.revision,r.executable_path,m.local_path,d.runtime_config_json
		FROM local_runtime_instances i
		JOIN managed_local_runtimes r ON r.id=i.runtime_id
		JOIN managed_local_models m ON m.id=i.managed_model_id
		JOIN model_deployments d ON d.id=i.deployment_id
		WHERE i.status IN ('starting','healthy','busy','draining')`)
	if err != nil {
		return err
	}
	defer rows.Close()
	type item struct {
		id                 string
		pid, port          int
		revision           int64
		exe, model, config string
	}
	var xs []item
	for rows.Next() {
		var x item
		var pid sql.NullInt64
		if err := rows.Scan(&x.id, &pid, &x.port, &x.revision, &x.exe, &x.model, &x.config); err != nil {
			return err
		}
		if pid.Valid {
			x.pid = int(pid.Int64)
		}
		xs = append(xs, x)
	}
	now := s.clock.UnixMilli()
	inspector, canInspect := s.processes.(ProcessInspector)
	for _, x := range xs {
		trusted := false
		if x.pid > 1 && s.processes.Alive(x.pid) && canInspect {
			ident, ierr := inspector.Identity(x.pid)
			if ierr == nil && filepath.Clean(ident.Executable) == filepath.Clean(x.exe) && argsContainPair(ident.Args, "-m", x.model) && argsContainPair(ident.Args, "--host", "127.0.0.1") && argsContainPair(ident.Args, "--port", strconv.Itoa(x.port)) {
				trusted = true
			}
		}
		if !trusted {
			res, e := s.db.ExecContext(ctx, `UPDATE local_runtime_instances SET status='orphaned',failure_reason='runtime process identity could not be re-established after daemon restart',updated_at=?,revision=revision+1 WHERE id=? AND revision=?`, now, x.id, x.revision)
			if e != nil {
				return e
			}
			if n, e := res.RowsAffected(); e != nil || n != 1 {
				if e != nil {
					return e
				}
				return fmt.Errorf("runtime recovery state conflict")
			}
			continue
		}
		inst, ierr := s.instanceByID(ctx, x.id)
		if ierr != nil {
			return ierr
		}
		health, herr := s.Health(ctx, inst)
		if herr != nil {
			res, e := s.db.ExecContext(ctx, `UPDATE local_runtime_instances SET status='orphaned',failure_reason='recovered process failed independent health probe',updated_at=?,revision=revision+1 WHERE id=? AND revision=?`, now, x.id, x.revision)
			if e != nil {
				return e
			}
			if n, e := res.RowsAffected(); e != nil || n != 1 {
				if e != nil {
					return e
				}
				return fmt.Errorf("runtime recovery state conflict")
			}
			continue
		}
		raw, _ := json.Marshal(health)
		res, e := s.db.ExecContext(ctx, `UPDATE local_runtime_instances SET status='healthy',health_json=?,last_seen_at=?,failure_reason=NULL,updated_at=?,revision=revision+1 WHERE id=? AND revision=?`, string(raw), now, now, x.id, x.revision)
		if e != nil {
			return e
		}
		if n, e := res.RowsAffected(); e != nil || n != 1 {
			if e != nil {
				return e
			}
			return fmt.Errorf("runtime recovery state conflict")
		}
	}
	return nil
}

func argsContainPair(args []string, key, value string) bool {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == key && filepath.Clean(args[i+1]) == filepath.Clean(value) {
			return true
		}
	}
	return false
}

func (s *RuntimeSupervisor) Instance(ctx context.Context, deploymentID string) (RuntimeInstance, error) {
	var idv string
	if err := s.db.QueryRowContext(ctx, `SELECT id FROM local_runtime_instances WHERE deployment_id=?`, deploymentID).Scan(&idv); err != nil {
		return RuntimeInstance{}, err
	}
	return s.instanceByID(ctx, idv)
}
func (s *RuntimeSupervisor) instanceByID(ctx context.Context, idv string) (RuntimeInstance, error) {
	var x RuntimeInstance
	var pid sql.NullInt64
	var started, last, stopped sql.NullInt64
	var fail sql.NullString
	var launch, health string
	err := s.db.QueryRowContext(ctx, `SELECT id,node_id,deployment_id,runtime_id,managed_model_id,bind_address,port,pid,status,process_fingerprint,launch_json,health_json,started_at,last_seen_at,stopped_at,failure_reason,revision,created_at,updated_at FROM local_runtime_instances WHERE id=?`, idv).Scan(&x.ID, &x.NodeID, &x.DeploymentID, &x.RuntimeID, &x.ManagedModelID, &x.BindAddress, &x.Port, &pid, &x.Status, &x.ProcessFingerprint, &launch, &health, &started, &last, &stopped, &fail, &x.Revision, &x.CreatedAt, &x.UpdatedAt)
	if pid.Valid {
		x.PID = int(pid.Int64)
	}
	if started.Valid {
		v := started.Int64
		x.StartedAt = &v
	}
	if last.Valid {
		v := last.Int64
		x.LastSeenAt = &v
	}
	if stopped.Valid {
		v := stopped.Int64
		x.StoppedAt = &v
	}
	if fail.Valid {
		x.FailureReason = &fail.String
	}
	x.LaunchJSON = json.RawMessage(launch)
	x.HealthJSON = json.RawMessage(health)
	return x, err
}
func (s *RuntimeSupervisor) markFailed(ctx context.Context, idv, reason string) error {
	now := s.clock.UnixMilli()
	_, err := s.db.ExecContext(ctx, `UPDATE local_runtime_instances SET status='failed',failure_reason=?,updated_at=?,revision=revision+1 WHERE id=?`, reason, now, idv)
	return err
}

// Shutdown stops all harness-managed local model processes that are still
// attached to this daemon. It is best-effort per instance and returns the first
// error after attempting every deployment.
func (s *RuntimeSupervisor) Touch(ctx context.Context, deploymentID string) error {
	now := s.clock.UnixMilli()
	_, err := s.db.ExecContext(ctx, `UPDATE local_runtime_instances SET last_seen_at=?,updated_at=?,revision=revision+1 WHERE deployment_id=? AND status IN ('healthy','busy')`, now, now, deploymentID)
	return err
}

func (s *RuntimeSupervisor) ReapIdle(ctx context.Context, idle time.Duration, limit int) (int, error) {
	if idle <= 0 {
		return 0, nil
	}
	if limit <= 0 || limit > 100 {
		limit = 10
	}
	cutoff := s.clock.UnixMilli() - idle.Milliseconds()
	rows, err := s.db.QueryContext(ctx, `SELECT deployment_id FROM local_runtime_instances WHERE status='healthy' AND COALESCE(last_seen_at,started_at,updated_at) < ? ORDER BY COALESCE(last_seen_at,started_at,updated_at) ASC LIMIT ?`, cutoff, limit)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var idv string
		if err := rows.Scan(&idv); err != nil {
			return 0, err
		}
		ids = append(ids, idv)
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	stopped := 0
	for _, idv := range ids {
		if err := s.Stop(ctx, idv); err != nil {
			continue
		}
		stopped++
	}
	return stopped, nil
}

func (s *RuntimeSupervisor) Shutdown(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, `SELECT deployment_id FROM local_runtime_instances WHERE status IN ('starting','healthy','busy','draining') ORDER BY deployment_id`)
	if err != nil {
		return err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var idv string
		if err := rows.Scan(&idv); err != nil {
			return err
		}
		ids = append(ids, idv)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	var first error
	for _, idv := range ids {
		if err := s.Stop(ctx, idv); err != nil && first == nil {
			first = err
		}
	}
	return first
}
