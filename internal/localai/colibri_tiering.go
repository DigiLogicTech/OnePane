package localai

import (
    "context"
    "database/sql"
    "encoding/json"
    "errors"
    "fmt"
    "os"
    "os/exec"
    "path/filepath"
    "runtime"
    "strconv"
    "strings"
    "time"
)

// ColibriTierSettings configures Colibri's own disk/RAM/VRAM expert cache.
// These settings do not change llama.cpp placements or OnePane's host scheduler.
type ColibriTierSettings struct {
    Mode string `json:"mode"`
    Backend string `json:"backend"`
    RAMGB int `json:"ram_gb"`
    ExpertCap int `json:"expert_cap"`
    VulkanExperts int `json:"vulkan_experts"`
    GPUIndex int `json:"gpu_index"`
    RepinTokens int `json:"repin_tokens"`
}

type ColibriTierState struct {
    DeploymentID string `json:"deployment_id"`
    Settings ColibriTierSettings `json:"settings"`
    PlanAvailable bool `json:"plan_available"`
    PlanNote string `json:"plan_note,omitempty"`
}

type ColibriTierCommand struct {
    DeploymentID string
    Settings ColibriTierSettings
}

func defaultColibriTier() ColibriTierSettings {
    return ColibriTierSettings{Mode:"automatic", Backend:"auto", VulkanExperts:96}
}

func validateColibriTier(c ColibriTierSettings) (ColibriTierSettings,error) {
    c.Mode = strings.ToLower(strings.TrimSpace(c.Mode))
    c.Backend = strings.ToLower(strings.TrimSpace(c.Backend))
    if c.Mode == "" { c.Mode="automatic" }
    if c.Backend == "" { c.Backend="auto" }
    switch c.Mode {
    case "automatic","balanced","manual":
    default: return c,errors.New("Colibri tier mode must be automatic, balanced, or manual")
    }
    switch c.Backend {
    case "auto","cpu","vulkan","cuda":
    default: return c,errors.New("Colibri backend must be auto, cpu, vulkan, or cuda")
    }
    if c.RAMGB<0||c.RAMGB>512 { return c,errors.New("Colibri RAM budget must be 0–512 GB (0 is automatic)") }
    if c.ExpertCap<0||c.ExpertCap>4096 { return c,errors.New("Colibri expert cap must be 0–4096 (0 is automatic)") }
    if c.VulkanExperts<0||c.VulkanExperts>2048 { return c,errors.New("Colibri Vulkan experts must be 0–2048") }
    if c.GPUIndex<0||c.GPUIndex>15 { return c,errors.New("Colibri GPU index must be 0–15") }
    if c.RepinTokens<0||c.RepinTokens>65536 { return c,errors.New("Colibri repin interval must be 0–65536") }
    // A conservative Vulkan default leaves space for dense weights and buffers
    // on small GPUs such as a GTX 1060. Colibri's upstream default is larger.
    if c.VulkanExperts==0 && c.Mode!="manual" && c.Backend=="vulkan" { c.VulkanExperts=96 }
    return c,nil
}

func colibriTierEnv(c ColibriTierSettings) []string {
    out:=[]string{}
    if c.Mode=="balanced" { out=append(out,"COLI_POLICY=balanced","REPIN=64") }
    if c.Mode=="manual" {
        if c.RAMGB>0 { out=append(out,"RAM_GB="+strconv.Itoa(c.RAMGB)) }
        if c.RepinTokens>0 { out=append(out,"REPIN="+strconv.Itoa(c.RepinTokens)) }
    }
    switch c.Backend {
    case "cpu":
        out=append(out,"COLI_CUDA=0","COLI_VULKAN=0","COLI_GPU=none")
    case "cuda":
        out=append(out,"COLI_CUDA=1","COLI_VULKAN=0","COLI_GPU="+strconv.Itoa(c.GPUIndex))
    case "vulkan":
        experts:=c.VulkanExperts
        if experts==0&&c.Mode!="manual" { experts=96 }
        out=append(out,"COLI_CUDA=0","COLI_VULKAN=1",
            "COLI_VK_DEV="+strconv.Itoa(c.GPUIndex),
            "COLI_VK_EXPERTS="+strconv.Itoa(experts))
    }
    return out
}

func colibriTierCapArgs(c ColibriTierSettings) []string {
    if c.Mode=="manual"&&c.ExpertCap>0 { return []string{"--cap",strconv.Itoa(c.ExpertCap)} }
    return nil
}

// colibriTierSnapshot only accepts managed Colibri deployments. No arbitrary
// executable or model path is accepted from the HTTP request.
func (s *Service) colibriTierSnapshot(ctx context.Context, id string) (map[string]json.RawMessage, error) {
    var raw string
    err:=s.db.QueryRowContext(ctx,
        "SELECT d.runtime_config_json FROM model_deployments d JOIN managed_local_models m ON m.deployment_id=d.id WHERE d.id=? AND LOWER(d.runtime_name)='colibri' AND m.status<>'removed'",id).Scan(&raw)
    if err!=nil { return nil,err }
    var cfg map[string]json.RawMessage
    if err=json.Unmarshal([]byte(raw),&cfg);err!=nil { return nil,err }
    return cfg,nil
}

func tierSettingsFromConfig(cfg map[string]json.RawMessage) ColibriTierSettings {
    c:=defaultColibriTier()
    if len(cfg["colibri_tier"])!=0 { _=json.Unmarshal(cfg["colibri_tier"],&c) }
    if c.Mode=="" { c.Mode="automatic" }
    if c.Backend=="" { c.Backend="auto" }
    return c
}

func colibriLauncher(root string) string {
    for _,relative:=range []string{"coli","c/coli"} {
        path:=filepath.Join(root,filepath.FromSlash(relative))
        if info,err:=os.Stat(path);err==nil&&!info.IsDir() { return path }
    }
    return ""
}

func (s *Service) ColibriTier(ctx context.Context, deploymentID string) (ColibriTierState,error) {
    cfg,err:=s.colibriTierSnapshot(ctx,deploymentID)
    if err!=nil { return ColibriTierState{},err }
    settings:=tierSettingsFromConfig(cfg)
    var exe string
    _=json.Unmarshal(cfg["executable"],&exe)
    launcher:=colibriLauncher(filepath.Dir(exe))
    state:=ColibriTierState{DeploymentID:deploymentID,Settings:settings,PlanAvailable:launcher!=""}
    if launcher=="" { state.PlanNote="This installed Colibri package has no coli launcher; tier settings use the existing compatible server and JSON placement diagnostics are unavailable." }
    return state,nil
}

// SetColibriTier never interrupts active inference. Idle runtimes are drained
// before changing their next-launch configuration.
func (s *Service) SetColibriTier(ctx context.Context, cmd ColibriTierCommand) (ColibriTierState,error) {
    if strings.TrimSpace(cmd.DeploymentID)=="" { return ColibriTierState{},errors.New("deployment id required") }
    settings,err:=validateColibriTier(cmd.Settings)
    if err!=nil { return ColibriTierState{},err }
    cfg,err:=s.colibriTierSnapshot(ctx,cmd.DeploymentID)
    if err!=nil { return ColibriTierState{},err }
    var status string
    err=s.db.QueryRowContext(ctx,"SELECT status FROM local_runtime_instances WHERE deployment_id=?",cmd.DeploymentID).Scan(&status)
    if err!=nil && !errors.Is(err,sql.ErrNoRows) { return ColibriTierState{},err }
    if err==nil && (status=="busy"||status=="draining") {
        return ColibriTierState{},errors.New("Colibri deployment is serving a request; change tier settings after it becomes idle")
    }
    if err==nil && (status=="healthy"||status=="starting") {
        stopped,stopErr:=s.supervisor.StopIfIdle(ctx,cmd.DeploymentID)
        if stopErr!=nil{return ColibriTierState{},fmt.Errorf("stop idle Colibri runtime: %w",stopErr)}
        if !stopped {return ColibriTierState{},errors.New("Colibri runtime became busy; retry after it is idle")}
    }
    raw,_:=json.Marshal(settings)
    cfg["colibri_tier"]=raw
    updated,err:=json.Marshal(cfg)
    if err!=nil{return ColibriTierState{},err}
    now:=s.clock.UnixMilli()
    _,err=s.db.ExecContext(ctx,"UPDATE model_deployments SET runtime_config_json=?,updated_at=?,revision=revision+1 WHERE id=?",string(updated),now,cmd.DeploymentID)
    if err!=nil { return ColibriTierState{},err }
    return s.ColibriTier(ctx,cmd.DeploymentID)
}

// ColibriPlan runs only a managed in-root launcher with a fixed argument set.
// It is read-only and bounded; absent launchers are reported, never guessed.
func (s *Service) ColibriPlan(ctx context.Context, deploymentID string) (json.RawMessage,error) {
    cfg,err:=s.colibriTierSnapshot(ctx,deploymentID)
    if err!=nil { return nil,err }
    var exe,model string
    _=json.Unmarshal(cfg["executable"],&exe)
    _=json.Unmarshal(cfg["model_path"],&model)
    root:=filepath.Dir(exe)
    launcher:=colibriLauncher(root)
    if launcher=="" { return nil,errors.New("installed Colibri archive does not include the coli placement launcher") }
    if !filepath.IsAbs(model) { return nil,errors.New("managed Colibri model path is not absolute") }
    rel,err:=filepath.Rel(s.modelRoot,model)
    if err!=nil||rel==".."||strings.HasPrefix(rel,".."+string(os.PathSeparator)) {return nil,errors.New("Colibri model path is outside the managed model pool")}
    python,err:=findPythonExecutable()
    if err!=nil{return nil,err}
    args:=[]string{}
    if runtime.GOOS=="windows"&&strings.EqualFold(filepath.Base(python),"py.exe"){args=append(args,"-3")}
    args=append(args,launcher,"plan","--model",model,"--json")
    settings:=tierSettingsFromConfig(cfg)
    if settings.Mode=="manual"&&settings.RAMGB>0 {args=append(args,"--ram",strconv.Itoa(settings.RAMGB))}
    cctx,cancel:=context.WithTimeout(ctx,20*time.Second);defer cancel()
    command:=exec.CommandContext(cctx,python,args...)
    command.Dir=root
    command.Env=append(os.Environ(),colibriTierEnv(settings)...)
    output,err:=command.CombinedOutput()
    if err!=nil { return nil,fmt.Errorf("Colibri placement plan unavailable: %w: %s",err,strings.TrimSpace(string(output[:min(len(output),1000)]))) }
    if len(output)>128<<10 {return nil,errors.New("Colibri plan exceeds response limit")}
    if !json.Valid(output) {return nil,errors.New("Colibri launcher did not return machine-readable JSON")}
    return json.RawMessage(output),nil
}
