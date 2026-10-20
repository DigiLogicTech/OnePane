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
    // A Colibri Python server can exit while a child engine still holds
    // inherited stdout/stderr pipes open. Bound Wait's I/O pump cleanup
    // so a dead parent does not appear alive forever in the supervisor.
    cmd.WaitDelay=5*time.Second
	// A nil exec.Cmd.Env inherits the parent process environment (including
	// cloud/API credentials). Force a non-nil, explicitly constructed slice
	// even for callers with no approved host variables.
	cmd.Env = make([]string, 0, len(spec.Env))
	cmd.Env = append(cmd.Env, spec.Env...)
	if spec.Dir != "" {
		cmd.Dir = spec.Dir
	}
	// A managed runtime may write indefinitely without restarting. Route
	// both streams through one capped writer while continuing to drain their
	// pipes; rotating only on the NEXT launch did not protect a small disk.
 var logFile *os.File
 if spec.LogPath != "" {
  var err error
  logFile,err=openManagedRuntimeLog(spec.LogPath)
  if err!=nil{return 0,err}
  capWriter,err:=newBoundedRuntimeLogWriter(logFile)
  if err!=nil{_ = logFile.Close();return 0,err}
  cmd.Stdout,cmd.Stderr=capWriter,capWriter
 } else {cmd.Stdout, cmd.Stderr=io.Discard,io.Discard}
	if err := cmd.Start(); err != nil {
        if logFile!=nil{_ = logFile.Close()}
		return 0, err
	}
	pid := cmd.Process.Pid
	b.mu.Lock()
	b.cmds[pid] = cmd
	b.mu.Unlock()
	go func() {
  _=cmd.Wait()
  // Only close after Wait has drained all stdout/stderr pumps.
  if logFile!=nil{_ = logFile.Close()}
  b.mu.Lock();delete(b.cmds,pid);b.mu.Unlock()
 }()
	return pid, nil
}
// Stream a model's diagnostics without ever letting a long-lived process
// grow the current log beyond 4 MiB. Writes beyond the cap are acknowledged
// and discarded, so a noisy model cannot block inference or fill the disk.
const maxManagedRuntimeLogBytes int64=4<<20
type boundedRuntimeLogWriter struct{
 mu sync.Mutex
 file *os.File
 remaining int64
}
func newBoundedRuntimeLogWriter(file *os.File)(*boundedRuntimeLogWriter,error){
 if file==nil{return nil,errors.New("managed runtime log unavailable")}
 info,err:=file.Stat()
 if err!=nil{return nil,err}
 if !info.Mode().IsRegular()||info.Size()<0{
  return nil,errors.New("invalid managed runtime log target")
 }
 remaining:=maxManagedRuntimeLogBytes-info.Size()
 if remaining<0{remaining=0}
 return &boundedRuntimeLogWriter{file:file,remaining:remaining},nil
}
func(w *boundedRuntimeLogWriter)Write(p []byte)(int,error){
 if w==nil{return 0,errors.New("managed runtime log writer unavailable")}
 w.mu.Lock()
 defer w.mu.Unlock()
 original:=len(p)
 if w.remaining<=0{return original,nil}
 permitted:=original
 if int64(permitted)>w.remaining{permitted=int(w.remaining)}
 n,err:=w.file.Write(p[:permitted])
 w.remaining-=int64(n)
 if err!=nil{return n,err}
 if n!=permitted{return n,io.ErrShortWrite}
 return original,nil
}

// Managed inference logs may contain private paths or request data. The
// launcher must not truncate a previous log, follow a linked log file or
// operate in a shared directory. All checks precede the subprocess spawn;
// no existing arbitrary file is opened with O_TRUNC.
// Existing or archived logs from earlier releases can inherit broad Windows
// DACLs. Protect their NTFS ACL without writing/truncating any log bytes.
// The returned handle is closed before rename to avoid Windows share issues.
func protectExistingRuntimeLogOnDisk(path string)error{
 observed,err:=os.Lstat(path)
 if errors.Is(err,os.ErrNotExist){return nil}
 if err!=nil{return err}
 if !observed.Mode().IsRegular(){return errors.New("unsafe archived managed runtime log")}
 f,err:=os.OpenFile(path,os.O_WRONLY|os.O_APPEND,0o600)
 if err!=nil{return err}
 defer f.Close()
 held,err:=f.Stat()
 if err!=nil||!os.SameFile(observed,held){
  return errors.New("managed runtime log changed before protection")
 }
 if err:=protectManagedRuntimeLog(f);err!=nil{return err}
 named,err:=os.Lstat(path)
 if err!=nil||!named.Mode().IsRegular()||!os.SameFile(held,named){
  return errors.New("managed runtime log path changed during security update")
 }
 return nil
}

func openManagedRuntimeLog(path string)(*os.File,error){
 dir:=filepath.Dir(path)
 if err:=os.MkdirAll(dir,0o700);err!=nil{return nil,err}
 di,err:=os.Lstat(dir)
 if err!=nil{return nil,err}
 if !di.IsDir()||di.Mode()&os.ModeSymlink!=0||
  (runtime.GOOS!="windows"&&di.Mode().Perm()&0o077!=0){
  return nil,errors.New("unsafe managed runtime log directory")
 }
 // Audit an older rotated log even when a new model launch does not rotate.
 if runtime.GOOS=="windows"{
  if err:=protectExistingRuntimeLogOnDisk(path+".previous");err!=nil{return nil,err}
 }
 st,err:=os.Lstat(path)
 if err!=nil&&!errors.Is(err,os.ErrNotExist){return nil,err}
 if err==nil{
  if !st.Mode().IsRegular()||
   (runtime.GOOS!="windows"&&st.Mode().Perm()&0o077!=0){
   return nil,errors.New("unsafe existing managed runtime log")
  }
  // Preserve old bounded log rollover semantics without overwriting a
  // file that was replaced or linked during the preflight.
  // A log already at the live-write cap must rotate too; reopening
  // it would silently discard every diagnostic from the next launch.
  if st.Size()>=maxManagedRuntimeLogBytes{
   // Without this step a legacy 0644/Everyone log becomes a readable
   // archived .previous when the first launch rotates it.
   if runtime.GOOS=="windows"{
    if e:=protectExistingRuntimeLogOnDisk(path);e!=nil{return nil,e}
   }
   archive:=path+".previous"
   if _,e:=os.Lstat(archive);e==nil{
    if e=os.Remove(archive);e!=nil{return nil,e}
   }else if !errors.Is(e,os.ErrNotExist){return nil,e}
   if e:=os.Rename(path,archive);e!=nil{return nil,e}
   st=nil
  }
 }
 if st!=nil{
  f,e:=os.OpenFile(path,os.O_WRONLY|os.O_APPEND,0o600)
  if e!=nil{return nil,e}
  fi,e:=f.Stat()
  if e!=nil||!fi.Mode().IsRegular()||!os.SameFile(st,fi){
   _=f.Close()
   return nil,errors.New("managed runtime log changed before opening")
  }
  if e:=protectManagedRuntimeLog(f);e!=nil{_ = f.Close();return nil,e}
  named,e:=os.Lstat(path)
  if e!=nil||!named.Mode().IsRegular()||!os.SameFile(fi,named){
   _=f.Close()
   return nil,errors.New("managed runtime log changed during ACL protection")
  }
  return f,nil
 }
 f,e:=os.OpenFile(path,os.O_WRONLY|os.O_CREATE|os.O_EXCL,0o600)
 if e!=nil{return nil,e}
 if e=protectManagedRuntimeLog(f);e!=nil{
  // A DACL failure must never leave a new unprotected file behind.
  opened,statErr:=f.Stat()
  named,nameErr:=os.Lstat(path)
  _=f.Close()
  if statErr==nil&&nameErr==nil&&named.Mode().IsRegular()&&os.SameFile(opened,named){
   _=os.Remove(path)
  }
  return nil,e
 }
 fi,e:=f.Stat()
 named,nameErr:=os.Lstat(path)
 if e!=nil||nameErr!=nil||!named.Mode().IsRegular()||!os.SameFile(fi,named){
  _=f.Close()
  return nil,errors.New("new managed runtime log changed during protected creation")
 }
 return f,nil
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
		// Colibri runs an inference-engine child beneath its Python server.
		// Killing only the parent on Windows leaks the child and its VRAM.
		// This is called only after VerifyIdentity authenticates the managed
		// process, and taskkill's /T closes its descendants as well.
        // taskkill output is not an authority or diagnostic stream.
        // Do not buffer potentially unbounded OS utility stdout/stderr or
        // echo it into UI-visible errors when stopping a model process.
        killErr := exec.Command("taskkill.exe", "/PID", strconv.Itoa(pid), "/T", "/F").Run()
		if killErr != nil && b.Alive(pid) {
			return fmt.Errorf("stop verified managed process tree: %w", killErr)
		}
		return nil
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
// SetBusy changes in-memory active request accounting ONLY after its
// persisted runtime transition succeeds. On SQLite error, cancelled context,
// or an unready deployment (zero updated rows), keep the old lease count.
// Otherwise a failed Acquire can strand a permanent busy lease or a failed
// Release can silently allow eviction while a request is still running.
func (s *RuntimeSupervisor) SetBusy(ctx context.Context, deploymentID string, busy bool) error {
 deploymentID=strings.TrimSpace(deploymentID)
 if deploymentID==""{return errors.New("deployment id required")}
 s.activityMu.Lock()
 defer s.activityMu.Unlock()
 previous:=s.activeRequests[deploymentID]
 if !busy&&previous==0{
  // An already-released request never changes the observed runtime state.
  return nil
 }
 next:=previous
 if busy{next++}else{next--}
 now:=s.clock.UnixMilli()
 var res sql.Result
 var err error
 if next>0{
  res,err=s.db.ExecContext(ctx,`UPDATE local_runtime_instances
   SET status='busy',last_seen_at=?,updated_at=?,revision=revision+1
   WHERE deployment_id=? AND status IN ('healthy','busy')`,now,now,deploymentID)
 }else{
  res,err=s.db.ExecContext(ctx,`UPDATE local_runtime_instances
   SET status='healthy',last_seen_at=?,updated_at=?,revision=revision+1
   WHERE deployment_id=? AND status='busy'`,now,now,deploymentID)
 }
 if err!=nil{return err}
 affected,err:=res.RowsAffected()
 if err!=nil{return err}
 if affected!=1{
  return errors.New("managed runtime is not available for the requested lease transition")
 }
 if next==0{
  delete(s.activeRequests,deploymentID)
 }else{
  if s.activeRequests==nil{s.activeRequests=make(map[string]int)}
  s.activeRequests[deploymentID]=next
 }
 return nil
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
	ColibriTier    ColibriTierSettings `json:"colibri_tier,omitempty"`
}

// Explicit, minimal host variables necessary for managed Python/llama
// subprocess launch, diagnostics and selected GPU drivers. Never inherit
// arbitrary service/API secrets or network proxy credentials. Environment
// values remain process-private and are not logged or returned to clients.
func managedRuntimeHostEnvironment(host []string) []string {
 allowed:=map[string]bool{
  "PATH":true,"PATHEXT":true,"SYSTEMROOT":true,"WINDIR":true,
  "COMSPEC":true,"TMP":true,"TEMP":true,"TMPDIR":true,
  "LANG":true,"LANGUAGE":true,"LC_ALL":true,"LC_CTYPE":true,
  "LC_MESSAGES":true,"TZ":true,
  "CUDA_VISIBLE_DEVICES":true,"CUDA_PATH":true,
  "HIP_VISIBLE_DEVICES":true,"ROCR_VISIBLE_DEVICES":true,
  "ROCM_PATH":true,"HSA_OVERRIDE_GFX_VERSION":true,
  "VK_ICD_FILENAMES":true,"VK_LAYER_PATH":true,"VULKAN_SDK":true,
  "NVIDIA_VISIBLE_DEVICES":true,
 }
 result:=make([]string,0,12)
 seen:=map[string]bool{}
 for _,entry:=range host{
  name,_,ok:=strings.Cut(entry,"=")
  normalized:=strings.ToUpper(name)
  if !ok||!allowed[normalized]||seen[normalized]{continue}
  // Block malformed names rather than relying on platform-dependent
  // subprocess environment parsing.
  if name==""||strings.TrimSpace(name)!=name||
   strings.ContainsAny(name,"\r\n\x00"){continue}
  result=append(result,entry)
  seen[normalized]=true
 }
 return result
}

type managedRefs struct{ RuntimeID, ModelID string }

// A backend repair/update may replace the registered executable generation.
// Use the original pinned executable if it still exists; otherwise rebind only
// to the live, managed executable for the EXACT same registered backend.
// Never translate CUDA to CPU/Vulkan or launch a path outside our runtime pool.
func chooseManagedLlamaExecutable(configured,registered,runtimeName,backend,managedRoot string)(string,error){
 if !strings.EqualFold(strings.TrimSpace(runtimeName),"llamacpp@"+strings.ToLower(strings.TrimSpace(backend))){
  return "",fmt.Errorf("registered llama.cpp runtime %s differs from deployment backend %s; repair the matching backend",runtimeName,backend)
 }
 within:=func(path string)bool{
  if !filepath.IsAbs(path){return false}
  rel,err:=filepath.Rel(managedRoot,filepath.Clean(path))
  return err==nil&&rel!=".."&&!strings.HasPrefix(rel,".."+string(os.PathSeparator))
 }
 valid:=func(path string)bool{
  if !within(path){return false}
  st,err:=os.Stat(path)
  return err==nil&&!st.IsDir()&&(runtime.GOOS=="windows"||st.Mode()&0111!=0)
 }
 if valid(configured){return configured,nil}
 if !valid(registered){return "",fmt.Errorf("managed llama.cpp %s executable is missing or inaccessible; repair or reinstall this backend from Models → Local → llama.cpp Settings before Agent Check",backend)}
 return registered,nil
}

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
	var exe, runtimeName, modelPath string
	if err := s.db.QueryRowContext(ctx, `SELECT executable_path,runtime_name FROM managed_local_runtimes WHERE id=? AND status='ready'`, refs.RuntimeID).Scan(&exe,&runtimeName); err != nil {
		return cfg, refs, "", err
	}
	if err := s.db.QueryRowContext(ctx, `SELECT local_path FROM managed_local_models WHERE id=?`, refs.ModelID).Scan(&modelPath); err != nil {
		return cfg, refs, "", err
	}
	// managed_local_runtimes records the currently preferred trusted executable.
	// Older pinned generations remain valid if present, but an obsolete path is
	// rebound at launch to the registered SAME-backend executable. No model
	// weights, model placement, or cloud credentials are changed.
	if strings.HasPrefix(strings.ToLower(runtimeName),"llamacpp@") {
		var chooseErr error
		cfg.Executable,chooseErr=chooseManagedLlamaExecutable(cfg.Executable,exe,runtimeName,cfg.RuntimeBackend,filepath.Join(s.dataDir,"runtimes"))
		if chooseErr!=nil{return cfg,refs,"",chooseErr}
	}
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

// isOperatorMemoryOverride cannot be triggered merely by an experimental
// runtime feature. Only a too-tight estimate with an explicit opt-in marker
// allows an attempt beyond the normal hardware memory budget.
func isOperatorMemoryOverride(rec Recommendation) bool {
 if !rec.Placement.Experimental || rec.FitLevel!=FitTooTight {return false}
 for _, note := range rec.Placement.Notes {
  if note=="explicit operator override of estimated resource fit" {return true}
 }
 return false
}

// ensureCapacity performs per-device pressure-aware hot swapping. It never
// treats heterogeneous accelerators as one fictional VRAM pool. Only healthy,
// idle runtimes are evicted; busy runtimes are protected by the runtime lease.
// Reject obviously impossible CPU-only allocations on low-resource hosts.
// This bounds total memory only; simultaneous CPU residents and changing
// available RAM still require installed host acceptance and monitoring.
func cpuModelFitsPhysicalRAM(required,total int64)bool{
 return cpuModelFitsConcurrentRAM(required,total,0)
}

// Admission uses an aggregate physical-RAM ceiling, not per-model fit
// alone. Subtract the same OS/OnePane reserve before evaluating existing
// managed CPU-model estimates; never add signed sizes before checking fit.
func cpuModelFitsConcurrentRAM(required,total,resident int64)bool{
 if required<=0||total<=0||resident<0{return false}
 reserve:=total/10
 if reserve>1<<30{reserve=1<<30}
 available:=total-reserve
 return resident<=available&&required<=available-resident
}

// Estimate still-resident CPU model commitments for a single local Node.
// This does not claim precise RSS or unmanaged-process memory visibility.
// Count unresolved live-PID failure states conservatively, and reject
// malformed legacy placement plans rather than inventing free RAM.
func (s *RuntimeSupervisor) estimatedResidentCPUMemory(ctx context.Context,nodeID,deploymentID string)(int64,error){
 rows,err:=s.db.QueryContext(ctx,`SELECT p.memory_required_bytes,p.plan_json
 FROM local_runtime_instances i
 JOIN managed_local_models m ON m.deployment_id=i.deployment_id
 JOIN local_model_install_plans p ON p.id=m.plan_id
 WHERE i.node_id=? AND i.deployment_id<>?
 AND (i.status IN ('starting','healthy','busy','draining')
 OR (i.status IN ('failed','orphaned') AND i.pid>1))
 ORDER BY i.deployment_id LIMIT 513`,nodeID,deploymentID)
 if err!=nil{return 0,err}
 defer rows.Close()
 var resident int64
 count:=0
 for rows.Next(){
  count++
  if count>512{return 0,errors.New("too many resident managed model records for bounded RAM admission")}
  var estimate int64
  var planJSON string
  if err=rows.Scan(&estimate,&planJSON);err!=nil{return 0,err}
  var rec Recommendation
  if err=json.Unmarshal([]byte(planJSON),&rec);err!=nil||rec.Placement.Mode==""{
   return 0,errors.New("resident model memory placement is unverified")
  }
  if rec.Placement.Mode!=PlacementCPUOnly{continue}
  next,ok:=addManagedMemoryBytes(resident,estimate)
  if !ok{return 0,errors.New("invalid or overflowing resident CPU model memory estimate")}
  resident=next
 }
 if err=rows.Err();err!=nil{return 0,err}
 if err=rows.Close();err!=nil{return 0,err}
 return resident,nil
}

// Hardware/plan memory quantities must never wrap signed int64 on a
// corrupt or exceptional hardware profile. Refuse the plan instead of
// treating overflowed negative bytes as reclaimed capacity.
func addManagedMemoryBytes(current,increment int64)(int64,bool){
 if current<0||increment<=0||increment>int64(^uint64(0)>>1)-current{
  return 0,false
 }
 return current+increment,true
}

// Predict whether evicting ALL healthy, idle candidates could satisfy a
// deployment. Refuse impossible placements before any process is signalled.
// Actual stops, identity checks and resource accounting remain unchanged.
func capacityCouldFitAfterIdleReclaim(used,target,capacity map[string]int64,
 idle []residentRuntime, operatorOverride bool) bool {
 projected:=make(map[string]int64,len(used))
 for key,n:=range used{projected[key]=n}
 for _,r:=range idle{
  for _,device:=range r.Placement.Devices{
   if device.Kind!="accelerator"{continue}
   key:=placementKey(device)
   // A malformed idle model cannot reclaim more memory than was
   // counted as resident. Do not let negative or inconsistent records
   // fabricate free GPU capacity.
   if device.AllocatedBytes<=0||projected[key]<device.AllocatedBytes{
    return false
   }
   projected[key]-=device.AllocatedBytes
  }
 }
 if operatorOverride{
  // Explicit over-budget launch can proceed only if no other resident
  // retains a claim on any of the requested accelerators.
  for key,n:=range target{
   if n<=0||projected[key]<0||projected[key]>0{return false}
  }
  return true
 }
 for key,n:=range target{
  occupied,limit:=projected[key],capacity[key]
  // Subtract from a verified positive budget instead of adding signed
  // allocations that could overflow into an apparent free-memory fit.
  if n<=0||occupied<0||limit<=0||occupied>limit||n>limit-occupied{
   return false
  }
 }
 return true
}

// Pressure eviction must free memory on the accelerator that is actually
// constrained. An unrelated CPU model or different-GPU model is not a
// legitimate victim merely because it is older.
func residentReclaimsConstrainedAccelerator(r residentRuntime,
 used,target,capacity map[string]int64) bool {
 for _,d:=range r.Placement.Devices {
  if d.Kind!="accelerator" || d.AllocatedBytes<=0 {continue}
  key:=placementKey(d)
  want:=target[key]
  if want<=0 {continue}
  occupied,limit:=used[key],capacity[key]
  if occupied<0||limit<=0||occupied>limit||want>limit-occupied{return true}
 }
 return false
}

// Compute retained accelerator capacity without multiplying full VRAM bytes
// by a percentage first: extremely large or corrupt hardware estimates can
// overflow int64 before division and fabricate a positive headroom value.
func usableManagedVRAM(total int64,headroomPct int)(int64,bool){
 if total<0||headroomPct<0||headroomPct>50{return 0,false}
 keep:=int64(100-headroomPct)
 // The quotient term cannot exceed total, and the remainder term is
 // bounded by 99*100. Both operations are safe across full int64 range.
 return (total/100)*keep+((total%100)*keep)/100,true
}

func (s *RuntimeSupervisor) ensureCapacity(ctx context.Context, deploymentID, nodeID string) error {
	var targetBytes int64
	var targetPlanJSON string
	if err := s.db.QueryRowContext(ctx, `SELECT p.memory_required_bytes,p.plan_json FROM managed_local_models m JOIN local_model_install_plans p ON p.id=m.plan_id WHERE m.deployment_id=?`, deploymentID).Scan(&targetBytes, &targetPlanJSON); err != nil {
		return err
	}
	var targetRec Recommendation
	if err:=json.Unmarshal([]byte(targetPlanJSON),&targetRec);err!=nil{
		return errors.New("managed model install plan JSON is invalid")
	}
	var raw,rawMemory string
	if err := s.db.QueryRowContext(ctx, `SELECT accelerators_json,memory_json FROM local_hardware_profiles WHERE node_id=? ORDER BY detected_at DESC LIMIT 1`, nodeID).Scan(&raw,&rawMemory); err != nil {
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
		var profileMemory Memory
		if err:=json.Unmarshal([]byte(rawMemory),&profileMemory);err!=nil{return err}
		// Cached *free* memory is volatile, so use the immutable total
		// physical RAM as a conservative hard upper bound. Keep an OS
		// reserve even when the model is the sole active local process.
		resident,err:=s.estimatedResidentCPUMemory(ctx,nodeID,deploymentID)
		if err!=nil{return err}
		if !cpuModelFitsConcurrentRAM(targetBytes,profileMemory.TotalBytes,resident){
			return errors.New("CPU model estimated memory exceeds remaining physical system RAM budget; wait for a model to unload or choose a smaller quantisation or Node")
		}
		return nil
	}
	capacity := map[string]int64{}
	for _,g:=range gpus {
		usable,ok:=usableManagedVRAM(g.VRAMBytes,s.residencyHeadroomPct)
		if !ok{return errors.New("managed accelerator memory profile or reserve is invalid")}
		for _,b:=range gpuBackends(g){
			k:=placementKey(PlacementDevice{DeviceID:g.DeviceID,Backend:b,DeviceIndex:g.DeviceIndex})
			if usable>capacity[k]{capacity[k]=usable}
		}
	}
	target := map[string]int64{}
	for _, d := range targetRec.Placement.Devices {
		if d.Kind == "accelerator" {
			key:=placementKey(d)
			next,ok:=addManagedMemoryBytes(target[key],d.AllocatedBytes)
			if !ok{return errors.New("invalid or overflowing managed accelerator memory allocation")}
			target[key]=next
		}
	}
	operatorOverride := isOperatorMemoryOverride(targetRec)
	for k, n := range target {
		if capacity[k] <= 0 || (n > capacity[k] && !operatorOverride) {
			return fmt.Errorf("managed placement requires %d bytes on %s but usable device memory is %d", n, k, capacity[k])
		}
	}

	rows, err := s.db.QueryContext(ctx, `SELECT i.deployment_id,p.memory_required_bytes,p.plan_json,COALESCE(i.last_seen_at,i.started_at,i.updated_at),i.status,CASE WHEN COALESCE(json_extract(d.runtime_config_json,'$.colibri_pinned'),0)=1 THEN 1 ELSE 0 END FROM local_runtime_instances i JOIN model_deployments d ON d.id=i.deployment_id JOIN managed_local_models m ON m.deployment_id=i.deployment_id JOIN local_model_install_plans p ON p.id=m.plan_id WHERE i.node_id=? AND i.deployment_id<>? AND i.status IN ('healthy','busy') ORDER BY COALESCE(i.last_seen_at,i.started_at,i.updated_at) ASC`, nodeID, deploymentID)
	if err != nil {
		return err
	}
	defer rows.Close()
	var residents []residentRuntime
	used := map[string]int64{}
	for rows.Next() {
		var x residentRuntime
		var planJSON, status string
        var pinned int
		if err := rows.Scan(&x.DeploymentID, &x.MemoryBytes, &planJSON, &x.LastUsedAt, &status, &pinned); err != nil {
			return err
		}
		var rec Recommendation
		if err:=json.Unmarshal([]byte(planJSON),&rec);err!=nil||rec.Placement.Mode==""{
			return errors.New("active managed runtime placement is unverified; reconcile or reinstall model plan")
		}
		x.Placement=rec.Placement
		for _, d := range x.Placement.Devices {
			if d.Kind == "accelerator" {
				key:=placementKey(d)
				next,ok:=addManagedMemoryBytes(used[key],d.AllocatedBytes)
				if !ok{return errors.New("invalid or overflowing resident accelerator allocation")}
				used[key]=next
			}
		}
		if status == string(RuntimeHealthy) && pinned==0 {
			residents = append(residents, x)
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	// No SQLite read cursor may remain open while stop/eviction causes
	// another instance lookup, state write or model process operation.
	if err := rows.Close(); err != nil {
		return err
	}
	fits := func() bool {
		for k, n := range target {
			if n<=0||used[k]<0||capacity[k]<=0||
				used[k]>capacity[k]||n>capacity[k]-used[k]{
				return false
			}
		}
		return true
	}
	if fits() {
		return nil
	}
	// Predict the *best possible* memory recovery from idle residents
	// before stopping anyone. Otherwise a busy model can make admission
	// impossible, yet every healthy idle neighbour would be needlessly
	// evicted first. This is conservative accounting, not VRAM telemetry.
	if !capacityCouldFitAfterIdleReclaim(used,target,capacity,residents,operatorOverride){
		return errors.New("insufficient reclaimable accelerator memory; working models preserved")
	}
	for _,x:=range residents {
		if !residentReclaimsConstrainedAccelerator(x,used,target,capacity){continue}
		// Reselect after the snapshot: an Agent Check, pin change, new lease
		// or fresh use must prevent eviction. Caller holds residencyMu.
		if x.LastUsedAt<0||x.LastUsedAt==int64(^uint64(0)>>1){continue}
		stopped,stopErr:=s.stopIfIdleBeforeLocked(ctx,x.DeploymentID,x.LastUsedAt+1)
		if stopErr!=nil||!stopped{continue}
		for _,d:=range x.Placement.Devices {
			if d.Kind!="accelerator"{continue}
			k:=placementKey(d)
			if used[k]<d.AllocatedBytes{return errors.New("resident accelerator ledger changed during eviction")}
			used[k]-=d.AllocatedBytes
		}
		if fits(){return nil}
	}
	// A genuine explicit memory override may attempt an out-of-budget launch
	// ONLY when no other resident uses its selected accelerators. This never
	// evicts a busy inference request or implies that physical VRAM will fit.
	if operatorOverride {
		for k := range target {
			if used[k] > 0 {
				return fmt.Errorf("experimental model launch refused: accelerator %s is occupied by another model; stop it first", k)
			}
		}
		return nil
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
 if err!=nil{return fmt.Errorf("list devices from managed %s backend failed: %w: %s; use Models → Local → llama.cpp Settings → Repair runtime to restore the exact backend executable and its dependencies",backend,err,strings.TrimSpace(string(out)))}
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

// A durable non-running state is not evidence that its OS process exited.
// No replacement is permitted while that PID is still alive, even when
// the process identity cannot be re-established safely.
func requiresOperatorReconciliationBeforeRestart(inst RuntimeInstance, alive bool) bool {
 if inst.PID <= 1 || !alive { return false }
 switch inst.Status {
 case RuntimeHealthy, RuntimeBusy, RuntimeStarting:
  return false // reusable only after the separate VerifyIdentity boundary
 default:
  return true
 }
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
	// An already-running target requires no eviction or new launcher.
	var existing *RuntimeInstance
	if old, err := s.Instance(ctx, deploymentID); err == nil {
		existing = &old
		alive := old.PID > 1 && s.processes.Alive(old.PID)
		// A failed, stopped, draining or orphaned instance may still own a
		// process and GPU/CPU memory. Never overwrite its original PID or
		// spawn a second engine until the operator reconciles that process.
		// In particular, a restart must not erase a recovered orphan's PID.
		if requiresOperatorReconciliationBeforeRestart(old, alive) {
			return RuntimeInstance{}, errors.New("previous managed runtime process is still alive in a non-running state; operator reconciliation required before restart")
		}
		if (old.Status == RuntimeHealthy || old.Status == RuntimeBusy ||
			old.Status == RuntimeStarting) && alive {
			// PIDs are recycled. An old "healthy" SQLite row does not
			// prove the PID still owns this exact approved executable,
			// model path, loopback address and port. Do not send prompts
			// to an unverified reused process or signal it on failure.
			if verifyErr:=s.VerifyIdentity(ctx,old);verifyErr!=nil{
				return RuntimeInstance{},fmt.Errorf("refusing to reuse unverified local runtime process: %w",verifyErr)
			}
			if old.Status == RuntimeStarting{
				return s.waitHealthy(ctx, old.ID, 120*time.Second)
			}
			return old, nil
		}
	}
	// Resolve the Colibri launcher BEFORE evicting a healthy predecessor.
	// Otherwise ordinary inference acquisition could lose its working
	// model merely because Python was missing, with no swap rollback.
	var colibriPython string
	if strings.EqualFold(cfg.RuntimeBackend, "colibri") {
		colibriPython,err=findPythonExecutable()
		if err!=nil{return RuntimeInstance{},err}
		// Tiering uses Colibri's managed runtime; never replace it with
		// another backend if its prerequisites are unavailable.
		if _, err = s.prepareColibriSwapLocked(ctx, nodeID, deploymentID); err != nil {
			return RuntimeInstance{}, err
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
		py := colibriPython
		executable = py
		args = nil
		if strings.EqualFold(filepath.Base(py), "py.exe") {
			args = append(args, "-3")
		}
				// Preserve legacy installations until the operator explicitly enables
		// a tier profile. The coli launcher supplies --auto-tier and model-aware
		// planning; openai_server.py remains a supported compatibility fallback.
		launcher := ""
		if cfg.ColibriTier.Mode != "" { launcher = colibriLauncher(filepath.Dir(cfg.Executable)) }
		if launcher != "" {
			args = append(args, launcher, "serve", "--model", cfg.ModelPath,
				"--host", "127.0.0.1", "--port", strconv.Itoa(port),
				"--model-id", firstNonEmpty(cfg.ModelRef, "onepane-colibri"),
				"--ctx", strconv.FormatInt(cfg.ContextTokens, 10))
			if cfg.ColibriTier.Mode == "automatic" || cfg.ColibriTier.Mode == "balanced" {
				args = append(args, "--auto-tier")
			}
			if cfg.ColibriTier.Mode == "balanced" { args = append(args, "--policy", "balanced") }
			if cfg.ColibriTier.Mode == "manual" {
				if cfg.ColibriTier.RAMGB > 0 { args = append(args, "--ram", strconv.Itoa(cfg.ColibriTier.RAMGB)) }
				if cfg.ColibriTier.RepinTokens > 0 { args = append(args, "--repin", strconv.Itoa(cfg.ColibriTier.RepinTokens)) }
			}
			args = append(args, colibriTierCapArgs(cfg.ColibriTier)...)
		} else {
			args = append(args, cfg.Executable, "--model", cfg.ModelPath, "--engine", cfg.EnginePath, "--host", "127.0.0.1", "--port", strconv.Itoa(port), "--model-id", firstNonEmpty(cfg.ModelRef, "onepane-colibri"))
			args = append(args, colibriTierCapArgs(cfg.ColibriTier)...)
		}
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
            // PID persistence must bind to this particular restart, not
            // merely to a reused instance ID that another actor replaced.
            inst.Revision=existing.Revision+1
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
	// Never forward the OnePane service's full environment into managed
 // third-party inference engines: it may contain cloud/API credentials,
 // proxy passwords, service tokens and unrelated private application state.
 env := managedRuntimeHostEnvironment(os.Environ())
	env = append(env, "ONEPANE_MANAGED_RUNTIME=1", "HOME="+runtimeHome)
	if runtime.GOOS != "windows" {
		env = append(env, "LD_LIBRARY_PATH="+filepath.Dir(cfg.Executable))
	}
	if strings.EqualFold(cfg.RuntimeBackend, "colibri") {
		env = append(env, "COLI_MODEL="+cfg.ModelPath, "COLI_MODEL_ID="+firstNonEmpty(cfg.ModelRef, "onepane-colibri"), "COLI_SERVE_PORT="+strconv.Itoa(port))
		env = append(env, colibriTierEnv(cfg.ColibriTier)...)
	}
	pid, err := s.processes.Start(context.Background(), LaunchSpec{Executable: executable, Args: args, Env: env, Dir: filepath.Dir(cfg.Executable),LogPath: filepath.Join(s.dataDir,"components","logs","local-runtime-"+inst.ID+".log")})
	if err != nil {
		_ = s.markFailed(context.Background(), inst.ID, err.Error())
		return RuntimeInstance{}, err
	}
	result, err := s.db.ExecContext(ctx, `UPDATE local_runtime_instances SET pid=?,started_at=?,updated_at=?,revision=revision+1 WHERE id=? AND status='starting' AND pid IS NULL AND revision=? AND process_fingerprint=?`, pid, now, now, inst.ID, inst.Revision, inst.ProcessFingerprint)
 if err==nil{
  var count int64
  count,err=result.RowsAffected()
  if err==nil && count!=1{
   err=errors.New("managed model launch ownership changed before PID persistence")
  }
 }
 if err!=nil{
  // A managed process has ALREADY started. Never silently forget it if
  // SQLite rejected or changed the owning row (even when RowsAffected=0).
  return RuntimeInstance{},s.reconcileUnrecordedLaunch(ctx,inst,pid,err)
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
 // A cancelled chat/task context must not prevent a completed request's
 // cleanup and strand its model as busy forever. Do not silently drop the
 // in-memory lease on database failure: SetBusy preserves it until a
 // persisted transition succeeds. Keep cleanup independently bounded.
 releaseCtx,cancel:=context.WithTimeout(context.WithoutCancel(ctx),3*time.Second)
 defer cancel()
 return s.SetBusy(releaseCtx,deploymentID,false)
}

// Diagnoses failures without exposing arbitrary large runtime output to clients.
// Runtime process output may contain paths, prompt text, bearer tokens or
// system diagnostics. Failure messages leave the host, so never put raw log
// bytes into an API/AgentCheck error. Only reveal that an owner-reviewable
// local log exists, and never follow a symlink or caller-chosen path.
func (s *RuntimeSupervisor) runtimeLogReviewHint(idv string) string {
 if len(idv)<1||len(idv)>128{return ""}
 for _,ch:=range idv{
  if (ch>='a'&&ch<='z')||(ch>='A'&&ch<='Z')||
   (ch>='0'&&ch<='9')||ch=='_'||ch=='-'{
   continue
  }
  return ""
 }
 path:=filepath.Join(s.dataDir,"components","logs","local-runtime-"+idv+".log")
 st,err:=os.Lstat(path)
 if err!=nil||!st.Mode().IsRegular()||st.Size()<1{return ""}
 return " · a local runtime log is available for operator review"
}
func (s *RuntimeSupervisor) waitHealthy(ctx context.Context, idv string, timeout time.Duration) (RuntimeInstance, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		inst, err := s.instanceByID(ctx, idv)
		if err != nil {
			return inst, err
		}
		if !s.processes.Alive(inst.PID) {
			reason:="runtime exited before becoming healthy"+s.runtimeLogReviewHint(idv)
   _ = s.markFailed(ctx,idv,reason)
   return RuntimeInstance{}, errors.New(reason)
		}
		// A model health server on a loopback port does not prove that
		// the intended managed process still owns that server. First
		// authenticate the expected executable/CLI model+port binding.
		// No HTTP probe or process signal may follow an identity mismatch.
		if verifyErr:=s.VerifyIdentity(ctx,inst);verifyErr!=nil{
			return RuntimeInstance{},errors.New("managed runtime process identity unverified during startup")
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
	reason:="managed runtime health check timed out"+s.runtimeLogReviewHint(idv)
 _ = s.markFailed(ctx,idv,reason)
 return RuntimeInstance{}, errors.New(reason)
}

// Health is a bounded, direct-loopback readiness probe, not a general
// HTTP client. Do not honour injected proxies, redirect hooks or dialers:
// those could leak a local engine's health data to an unrelated endpoint.
func (s *RuntimeSupervisor) Health(ctx context.Context, inst RuntimeInstance) (map[string]any, error) {
 if inst.Port<1024||inst.Port>65535{
  return nil,errors.New("managed runtime health port invalid")
 }
 req,err:=http.NewRequestWithContext(ctx,http.MethodGet,
  fmt.Sprintf("http://127.0.0.1:%d/health",inst.Port),nil)
 if err!=nil{return nil,err}
 transport:=&http.Transport{
  Proxy:nil,
  DialContext:(&net.Dialer{Timeout:3*time.Second}).DialContext,
  ResponseHeaderTimeout:3*time.Second,
 }
 defer transport.CloseIdleConnections()
 client:=&http.Client{Transport:transport,Timeout:3*time.Second,
  CheckRedirect:func(_ *http.Request,_ []*http.Request)error{
   return http.ErrUseLastResponse
  },
 }
 resp,err:=client.Do(req)
 if err!=nil{return nil,err}
 defer resp.Body.Close()
 if resp.StatusCode<200||resp.StatusCode>=300{
  return nil,fmt.Errorf("managed runtime health HTTP %d",resp.StatusCode)
 }
 const maxHealthResponse=1<<20
 body,err:=io.ReadAll(io.LimitReader(resp.Body,maxHealthResponse+1))
 if err!=nil{return nil,err}
 if len(body)>maxHealthResponse{
  return nil,errors.New("managed runtime health response oversized")
 }
 if len(bytes.TrimSpace(body))==0{
  return nil,errors.New("managed runtime health response empty")
 }
 var out map[string]any
 if err=json.Unmarshal(body,&out);err!=nil||len(out)==0{
  return nil,errors.New("managed runtime health response invalid JSON object")
 }
 status,ok:=out["status"].(string)
 if !ok{return nil,errors.New("managed runtime health status missing")}
 switch strings.ToLower(strings.TrimSpace(status)){
 case "ok","ready","healthy","no slot available":
  // A third-party engine's /health JSON is untrusted process output.
  // Only explicitly reviewed scalar readiness data may become durable
  // local_runtime_instances.health_json or leave the host through an API.
  safe:=map[string]any{"status":strings.ToLower(strings.TrimSpace(status))}
  for _,key:=range []string{"slots_idle","slots_processing"}{
   if raw,exists:=out[key];exists{
    if n,ok:=raw.(float64);ok&&n>=0&&n<=4096&&n==float64(int64(n)){
     safe[key]=int64(n)
    }
   }
  }
  return safe,nil
 default:
  return nil,errors.New("managed runtime not yet healthy")
 }
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
		allowed := []string{cfg.Executable}
		if cfg.ColibriTier.Mode != "" {
			if launcher := colibriLauncher(filepath.Dir(cfg.Executable)); launcher != "" {
				allowed = append(allowed, launcher)
			}
		}
		if !managedProcessCLIIdentified("colibri",ident,py,allowed,cfg.ModelPath,inst.Port){
			return errors.New("Colibri runtime process identity mismatch")
		}
		return nil
	}
	if !managedProcessCLIIdentified(cfg.RuntimeBackend,ident,cfg.Executable,nil,cfg.ModelPath,inst.Port){
		return errors.New("runtime process identity mismatch")
	}
	return nil
}

// Check exact managed process arguments independently from live host
// introspection. This single backend-aware predicate is used for startup,
// inference reuse, Stop and daemon recovery via VerifyIdentity. It is not
// independent hardware/physical Node attestation.
func managedProcessCLIIdentified(backend string,ident ProcessIdentity,
 executable string,allowedColibriScripts []string,model string,port int)bool{
 if port<1024||port>65535||filepath.Clean(ident.Executable)!=filepath.Clean(executable){
  return false
 }
 modelFlag:="-m"
 if strings.EqualFold(backend,"colibri"){
  modelFlag="--model"
  found:=false
  for _,a:=range ident.Args{
   for _,expected:=range allowedColibriScripts{
    if filepath.Clean(a)==filepath.Clean(expected){found=true;break}
   }
   if found{break}
  }
  if !found{return false}
 }
 // Duplicated command-line options can be interpreted last-wins by
 // the engine. Merely finding one matching pair could admit a later
 // conflicting model, non-loopback address or listener port.
 alternate:="--model"
 if modelFlag=="--model"{alternate="-m"}
 for _,arg:=range ident.Args{
  if arg==alternate{return false}
 }
 return oneExactManagedOption(ident.Args,modelFlag,model)&&
  oneExactManagedOption(ident.Args,"--host","127.0.0.1")&&
  oneExactManagedOption(ident.Args,"--port",strconv.Itoa(port))
}

// Require a unique option occurrence; never trust one matching value
// when another copy of the same flag can override it at runtime.
func oneExactManagedOption(args []string,name,want string)bool{
 count:=0
 for i:=0;i<len(args);i++{
  if args[i]!=name{continue}
  count++
  if i+1>=len(args)||filepath.Clean(args[i+1])!=filepath.Clean(want){
   return false
  }
 }
 return count==1
}

// StopIfIdle releases CPU/GPU memory after an Agent Check without evicting a
// runtime used by concurrent inference. Both locks are held through Stop:
// Acquire takes residencyMu then SetBusy (activityMu), so a new inference
// cannot enter between the safety check and process termination.
func (s *RuntimeSupervisor) StopIfIdle(ctx context.Context, deploymentID string) (bool, error) {
 return s.stopIfIdleBefore(ctx,deploymentID,0)
}

// Check liveness, the active inference lease, a current pin, and (for the
// idle reaper) the last-observed activity before signalling a process. The
// lock order matches Acquire: residencyMu, then activityMu.
func (s *RuntimeSupervisor) stopIfIdleBefore(ctx context.Context, deploymentID string, cutoff int64)(bool,error){
 if s==nil{return false,nil}
 deploymentID=strings.TrimSpace(deploymentID)
 if deploymentID==""{return false,errors.New("deployment id required")}
 s.residencyMu.Lock()
 defer s.residencyMu.Unlock()
 return s.stopIfIdleBeforeLocked(ctx,deploymentID,cutoff)
}

// Caller already owns residencyMu; retain the active-lease lock through
// process stop so it cannot race with a new inference acquisition.
func (s *RuntimeSupervisor) stopIfIdleBeforeLocked(ctx context.Context,deploymentID string,cutoff int64)(bool,error){
 s.activityMu.Lock()
 defer s.activityMu.Unlock()
 if s.activeRequests[deploymentID]>0{return false,nil}
 inst,err:=s.Instance(ctx,deploymentID)
 if errors.Is(err,sql.ErrNoRows){return false,nil}
 if err!=nil{return false,err}
 if cutoff>0{
  // Do not reap a model touched, restarted or checked since selection.
  last:=inst.UpdatedAt
  if inst.StartedAt!=nil&&*inst.StartedAt>last{last=*inst.StartedAt}
  if inst.LastSeenAt!=nil&&*inst.LastSeenAt>last{last=*inst.LastSeenAt}
  if last>=cutoff{return false,nil}
 }
 var pinned int
 if err=s.db.QueryRowContext(ctx,`SELECT CASE WHEN
  COALESCE(json_extract(runtime_config_json,'$.colibri_pinned'),0)=1
  THEN 1 ELSE 0 END FROM model_deployments WHERE id=?`,
  deploymentID).Scan(&pinned);err!=nil{return false,err}
 if pinned!=0{return false,nil}
 switch inst.Status{
 case RuntimeHealthy,RuntimeStarting:
  // An idle reaper never terminates a new model still starting up.
  if cutoff>0&&inst.Status!=RuntimeHealthy{return false,nil}
  if err:=s.stopWithActivityLock(ctx,deploymentID);err!=nil{return false,err}
  return true,nil
 default:return false,nil
 }
}

// Stop must serialize with inference SetBusy/Release. The caller-facing stop
// refuses to unload an active runtime, without persisting drain intent or
// sending any process signal. Internal eviction/reaper call sites already own
// activityMu and invoke stopWithActivityLock directly to avoid re-locking.
func (s *RuntimeSupervisor) Stop(ctx context.Context, deploymentID string) error {
 if s==nil{return errors.New("local runtime supervisor unavailable")}
 // Lock ordering matches Acquire/startLocked: residency then activity.
 // A user stop must not race with launch, recovery or a model swap.
 s.residencyMu.Lock()
 defer s.residencyMu.Unlock()
 return s.stopWhileResidencyLocked(ctx,deploymentID)
}

// Internal Colibri rollback already holds residencyMu but not activityMu.
func (s *RuntimeSupervisor) stopWhileResidencyLocked(ctx context.Context, deploymentID string) error {
 s.activityMu.Lock()
 defer s.activityMu.Unlock()
 return s.stopWithActivityLock(ctx,deploymentID)
}

// Call only while activityMu is held (including Colibri swap and idle reaper).
func (s *RuntimeSupervisor) stopWithActivityLock(ctx context.Context, deploymentID string, daemonShutdown ...bool) error {
 deploymentID=strings.TrimSpace(deploymentID)
 if deploymentID==""{return errors.New("deployment ID is required")}
 teardown:=len(daemonShutdown)>0 && daemonShutdown[0]
 // Manual stops/evictions must preserve live inference. Daemon teardown
 // follows HTTP-server drain and has explicit authority to clean up managed
 // children even if a timed-out request still owns an in-process lease.
 if !teardown&&s.activeRequests[deploymentID]>0{
  return errors.New("managed model is serving active inference requests; wait for them to complete before stopping")
 }
 inst, err := s.Instance(ctx, deploymentID)
	if err != nil {
		return err
	}
 // A persisted busy status without a corresponding in-memory lease may
 // represent an unknown request after daemon restart. Refuse to signal it
 // until operator reconciliation; never infer idleness from map absence.
 if inst.Status==RuntimeBusy&&!teardown {
  return errors.New("managed model has a persisted busy status with no safely releasable inference lease; reconcile before stopping")
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
			// The stop intent was durably set to draining, but the
			// managed process is still alive. An unresolved signal must
			// not be presented as normal drain/reclaimed capacity.
			if e:=markOrphaned("managed runtime graceful stop failed; operator reconciliation required");e!=nil{
				return fmt.Errorf("failed to persist unverified graceful-stop outcome: %w",e)
			}
			return errors.New("managed runtime graceful termination failed; process requires operator reconciliation")
		}
	}
	deadline := time.Now().Add(5 * time.Second)
	// Colibri's SIGTERM handler drains the child engine and flushes expert
	// cache state before exit. Avoid forcing it down at the generic 5s limit.
	var runtimeName string
	_ = s.db.QueryRowContext(ctx, "SELECT runtime_name FROM model_deployments WHERE id=?", deploymentID).Scan(&runtimeName)
	if strings.EqualFold(runtimeName, "colibri") && runtime.GOOS != "windows" {
		deadline = time.Now().Add(35 * time.Second)
	}
	for inst.PID > 1 && s.processes.Alive(inst.PID) && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	if inst.PID > 1 && s.processes.Alive(inst.PID) {
		// SIGTERM and drain may have taken 35 seconds for Colibri.
		// Recheck identity before escalation: a recycled PID must NEVER
		// receive SIGKILL merely because it was our managed process at
		// the beginning of the stop attempt.
		if err := s.VerifyIdentity(ctx, inst); err != nil {
			if e := markOrphaned("refused forced termination of an unverified runtime PID"); e != nil {
				return fmt.Errorf("forced termination denied; orphan-state persistence failed: %w", e)
			}
			return errors.New("forced termination denied: runtime process identity changed")
		}
		if err := s.processes.Signal(inst.PID, syscall.SIGKILL); err != nil && s.processes.Alive(inst.PID) {
			if e:=markOrphaned("managed runtime forced stop failed; operator reconciliation required");e!=nil{
				return fmt.Errorf("failed to persist unverified forced-stop outcome: %w",e)
			}
			return errors.New("managed runtime forced termination failed; process requires operator reconciliation")
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
	// The managed process is now independently confirmed stopped. Keep the
	// public deployment residency status aligned so Agent Check does not leave
	// a false CPU/GPU "resident" indicator on an unloaded model.
	_,err=s.db.ExecContext(ctx,`UPDATE model_deployments
	 SET residency_state='stopped',updated_at=?,revision=revision+1
	 WHERE id=? AND (residency_state IS NULL OR residency_state<>'stopped')`,now,deploymentID)
	if err!=nil{return fmt.Errorf("persist stopped model residency: %w",err)}
 // This is called only after Stop independently proved the managed process
 // exited. Abandoning daemon-owned lease counts on explicit teardown stops
 // late request cleanup from falsely holding a defunct model busy.
 if teardown{delete(s.activeRequests,deploymentID)}
	return nil
}

// Recover snapshots candidate rows and closes their SQLite cursor before
// inspecting any processes, reading another row, or applying a transition.
// An open cursor can block those operations when the service has only one
// SQLite connection (or retain an old WAL reader under sustained activity).
type recoverableRuntime struct {
 id string
 pid int
 revision int64
}
func recoverableRuntimeSnapshot(ctx context.Context,db *sql.DB)([]recoverableRuntime,error){
 const maxRecoveryCandidates=512
 rows,err:=db.QueryContext(ctx,`SELECT id,pid,revision
  FROM local_runtime_instances
  WHERE status IN ('starting','healthy','busy','draining')
  ORDER BY id LIMIT 513`)
 if err!=nil{return nil,err}
 xs:=make([]recoverableRuntime,0,16)
 for rows.Next(){
  var x recoverableRuntime
  var pid sql.NullInt64
  if err=rows.Scan(&x.id,&pid,&x.revision);err!=nil{break}
  if pid.Valid{x.pid=int(pid.Int64)}
  xs=append(xs,x)
  if len(xs)>maxRecoveryCandidates{
   err=errors.New("too many managed model instances for bounded daemon recovery")
   break
  }
 }
 if err==nil{err=rows.Err()}
 if closeErr:=rows.Close();err==nil{err=closeErr}
 if err!=nil{return nil,err}
 return xs,nil
}
func (s *RuntimeSupervisor) Recover(ctx context.Context) error {
 s.residencyMu.Lock()
 defer s.residencyMu.Unlock()
 // Do not discard live request counts if a recovery snapshot cannot even
 // be read. Recovery is expected at daemon startup, never on active work.
 xs,err:=recoverableRuntimeSnapshot(ctx,s.db)
 if err!=nil{return err}
 s.activityMu.Lock()
 s.activeRequests=map[string]int{}
 s.activityMu.Unlock()
 now:=s.clock.UnixMilli()
	for _, x := range xs {
		// Recovery must use exactly the same checked process identity
		// policy as local inference reuse and Stop. The old hard-coded
		// "-m" flag only matched llama.cpp, wrongly orphaning Colibri's
		// Python server after a daemon restart. Never contact a health
		// socket or signal an unverified/recycled PID.
		inst, ierr := s.instanceByID(ctx, x.id)
		trusted := ierr==nil && inst.PID==x.pid && inst.Revision==x.revision &&
			s.VerifyIdentity(ctx, inst)==nil
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

// Never hold a SQLite SELECT reader open while a reaper tries to update
// process rows. Bounded snapshot selection is read-only; each candidate is
// independently rechecked under model-residency and active-request locks.
func (s *RuntimeSupervisor) idleReaperCandidates(ctx context.Context,cutoff int64,limit int)([]string,error){
 rows,err:=s.db.QueryContext(ctx,`SELECT i.deployment_id
  FROM local_runtime_instances i
  JOIN model_deployments d ON d.id=i.deployment_id
  WHERE i.status='healthy'
   AND COALESCE(json_extract(d.runtime_config_json,'$.colibri_pinned'),0)<>1
   AND COALESCE(i.last_seen_at,i.started_at,i.updated_at)<?
  ORDER BY COALESCE(i.last_seen_at,i.started_at,i.updated_at) ASC LIMIT ?`,cutoff,limit)
 if err!=nil{return nil,err}
 ids:=make([]string,0,limit)
 for rows.Next(){
  var id string
  if err=rows.Scan(&id);err!=nil{break}
  ids=append(ids,id)
 }
 if err==nil{err=rows.Err()}
 if closeErr:=rows.Close();err==nil{err=closeErr}
 if err!=nil{return nil,err}
 return ids,nil
}

func (s *RuntimeSupervisor) ReapIdle(ctx context.Context, idle time.Duration, limit int) (int,error){
 if idle<=0{return 0,nil}
 if limit<=0||limit>100{limit=10}
 cutoff:=s.clock.UnixMilli()-idle.Milliseconds()
 ids,err:=s.idleReaperCandidates(ctx,cutoff,limit)
 if err!=nil{return 0,err}
 stopped:=0
 for _,id:=range ids{
  didStop,e:=s.stopIfIdleBefore(ctx,id,cutoff)
  if e!=nil{continue}
  if didStop{stopped++}
 }
 return stopped,nil
}

// stopForDaemonShutdown is private to deliberate daemon teardown, after
// HTTP graceful drain has had an opportunity to complete active inference.
// It uses the ordinary PID identity checks and owned-process stop logic;
// only the normal active-lease veto is bypassed during final teardown.
func (s *RuntimeSupervisor) stopForDaemonShutdown(ctx context.Context, deploymentID string) error {
 s.residencyMu.Lock()
 defer s.residencyMu.Unlock()
 s.activityMu.Lock()
 defer s.activityMu.Unlock()
 return s.stopWithActivityLock(ctx,deploymentID,true)
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
	// Shutdown must work with a single SQLite connection. Stop performs
	// other reads and CAS writes; leave no SELECT cursor pinned.
	if err := rows.Close(); err != nil {
		return err
	}
	var first error
	for _, idv := range ids {
		if err := s.stopForDaemonShutdown(ctx, idv); err != nil && first == nil {
			first = err
		}
	}
	return first
}
