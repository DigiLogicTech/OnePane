package localai

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	goruntime "runtime"
	"sort"
	"strings"
)

type LlamaRuntimeBackendStatus struct {
	Backend string `json:"backend"`
	Version string `json:"version"`
	Installed bool `json:"installed"`
	Recommended bool `json:"recommended"`
	Reason string `json:"reason,omitempty"`
	DriverVersion string `json:"driver_version,omitempty"`
	ActiveInstances int `json:"active_instances"`
	DependentModels int `json:"dependent_models"`
}

func (s *Service) latestHardwareProfile(ctx context.Context) (HardwareProfile, error) {
	var idv string
	if err := s.db.QueryRowContext(ctx, "SELECT id FROM local_hardware_profiles ORDER BY detected_at DESC LIMIT 1").Scan(&idv); err != nil { return HardwareProfile{}, err }
	return s.hardwareProfile(ctx, idv)
}

func llamaRecommendedBackends(p HardwareProfile) map[string]string {
	out := map[string]string{"cpu":"CPU fallback is always available"}
	for _, g := range p.GPUs {
		for _, b := range g.Backends {
			switch strings.ToLower(strings.TrimSpace(b)) {
			case "cuda":
				if _, ok := out["cuda"]; !ok { out["cuda"] = "NVIDIA GPU detected" }
			case "vulkan":
				if _, cuda := out["cuda"]; !cuda { if _, ok := out["vulkan"]; !ok { out["vulkan"] = "Vulkan-capable GPU detected" } }
			}
		}
	}
	return out
}

func (s *Service) llamaRuntimeEntries(ctx context.Context, p HardwareProfile) ([]RuntimeCatalogEntry, error) {
	if s.catalog == nil { return nil, errors.New("trusted Local AI catalogue is unavailable") }
	_, cat, err := s.catalog.Active(ctx); if err != nil { return nil, err }
	var out []RuntimeCatalogEntry
	for _, x := range cat.Runtimes {
		if strings.EqualFold(x.Name,"llamacpp") && strings.EqualFold(x.OS,goruntime.GOOS) && strings.EqualFold(x.Architecture,goruntime.GOARCH) { out=append(out,x) }
	}
	sort.SliceStable(out, func(i,j int) bool { return out[i].Backend < out[j].Backend })
	if len(out)==0 { return nil, fmt.Errorf("trusted catalogue has no llama.cpp runtime for %s/%s",goruntime.GOOS,goruntime.GOARCH) }
	return out,nil
}

func (s *Service) llamaRuntimeInUse(ctx context.Context) bool {
	var n int
	_ = s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM local_runtime_instances i JOIN managed_local_runtimes r ON r.id=i.runtime_id WHERE r.runtime_name LIKE 'llamacpp@%' AND i.status IN ('starting','healthy','busy','draining')").Scan(&n)
	return n>0
}

func (s *Service) llamaManagedModels(ctx context.Context) int {
	var n int
	_ = s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM managed_local_models m JOIN managed_local_runtimes r ON r.id=m.runtime_id WHERE r.runtime_name LIKE 'llamacpp@%' AND m.status NOT IN ('removed','failed')").Scan(&n)
	return n
}

func (s *Service) installLlamaRuntimeEntry(ctx context.Context, entry RuntimeCatalogEntry, p HardwareProfile) error {
	manifest := runtimeManifestFromCatalog(entry)
	if s.trustedRuntimeAlreadyInstalled(ctx,p.NodeID,entry) { return nil }
	backend := strings.ToLower(strings.TrimSpace(entry.Backend)); if backend=="" { backend="generic" }
	fingerprint := runtimeInstallFingerprint(manifest)
	root := filepath.Join(s.dataDir,"runtimes",manifest.Name,manifest.Version,backend)
	if len(manifest.Dependencies)>0 { root=filepath.Join(s.dataDir,"runtimes",manifest.Name,manifest.Version,backend+"-"+fingerprint[:16]) }
	// Record the currently registered generation, not merely the default
	// destination: a previous update may already have moved it to .rev-N.
	// Never discard model weights; stop only runtime processes using this backend.
	name:=manifest.Name+"@"+backend
	oldRoot:=""
	err:=s.db.QueryRowContext(ctx,"SELECT install_root FROM managed_local_runtimes WHERE node_id=? AND runtime_name=? AND status='ready'",p.NodeID,name).Scan(&oldRoot)
	if err!=nil&&err!=sql.ErrNoRows{return err}
	if err:=s.drainLlamaBackend(ctx,p.NodeID,name);err!=nil{return err}
	base:=root
	if st,e:=os.Stat(root);e==nil&&st.IsDir(){
		root=fmt.Sprintf("%s.rev-%d",base,s.clock.UnixMilli())
	} else if e!=nil&&!os.IsNotExist(e){return e}
	// Refuse an endless sequence of generations when a previous Windows DLL
	// lock prevented cleanup; the operator can drain/clean the old runtimes.
	entries,e:=os.ReadDir(filepath.Dir(base))
	if e!=nil&&!os.IsNotExist(e){return e}
	stale:=0
	for _,entry:=range entries{
		if entry.IsDir()&&strings.HasPrefix(entry.Name(),filepath.Base(base)+".rev-")&&
			!strings.EqualFold(filepath.Join(filepath.Dir(base),entry.Name()),oldRoot){stale++}
	}
	if stale>=2{return fmt.Errorf("llama.cpp %s update blocked: %d obsolete runtime generations require storage cleanup",backend,stale)}
	// A CUDA archive, its extracted runtime and rollback generation can be
	// present simultaneously. Keep at least 5 GiB available before staging.
	if err:=checkRuntimeDiskBudget(filepath.Join(s.dataDir,"runtimes"),5<<30);err!=nil{return err}
	downloads := filepath.Join(s.dataDir,"components","downloads"); if err:=os.MkdirAll(downloads,0o700); err!=nil{return err}
	archive := filepath.Join(downloads,fmt.Sprintf("llamacpp-%s-%s",manifest.Version,backend))
	if _,err:=s.fetcher.Fetch(ctx,manifest.SourceURL,archive,manifest.SHA256); err!=nil{return err}
	defer os.Remove(archive)
	type depDownload struct { dep RuntimeDependency; path string }
	var deps []depDownload
	for i,dep:=range manifest.Dependencies {
		pth:=filepath.Join(downloads,fmt.Sprintf("llamacpp-%s-%s-dep-%02d",manifest.Version,backend,i+1))
		if _,err:=s.fetcher.Fetch(ctx,dep.SourceURL,pth,dep.SHA256); err!=nil{return fmt.Errorf("runtime dependency %s: %w",dep.Name,err)}
		deps=append(deps,depDownload{dep:dep,path:pth}); defer os.Remove(pth)
	}
	staging:=root+".installing"; _=os.RemoveAll(staging)
	if manifest.ArchiveFormat=="binary" {
		if err:=os.MkdirAll(staging,0o700); err!=nil{return err}
		raw,err:=os.ReadFile(archive); if err!=nil{return err}
		dst:=filepath.Join(staging,filepath.Base(manifest.ExecutableRel)); if err:=os.WriteFile(dst,raw,0o700); err!=nil{return err}
		manifest.ExecutableRel=filepath.Base(manifest.ExecutableRel)
	} else if err:=ExtractRuntimeArchive(archive,manifest.ArchiveFormat,staging); err!=nil { _=os.RemoveAll(staging); return err }
	for _,d:=range deps { if err:=ExtractRuntimeArchive(d.path,d.dep.ArchiveFormat,staging); err!=nil { _=os.RemoveAll(staging); return fmt.Errorf("extract runtime dependency %s: %w",d.dep.Name,err) } }
	execPath:=filepath.Join(staging,manifest.ExecutableRel)
	if st,err:=os.Stat(execPath); err!=nil || st.IsDir() { _=os.RemoveAll(staging); return errors.New("llama.cpp executable missing after install") }
	if goruntime.GOOS!="windows" { _=os.Chmod(execPath,0o700) }
	if err:=os.Rename(staging,root); err!=nil { _=os.RemoveAll(staging); return err }
	runtimeID,err:=s.ensureManagedRuntime(ctx,p.NodeID,manifest,root,filepath.Join(root,manifest.ExecutableRel))
 if err!=nil{return err}
 // Old generation cleanup is best effort. Locked DLL files can be removed
 // later after processes have exited; they never block the new install.
 if oldRoot!=""&&!strings.EqualFold(filepath.Clean(oldRoot),filepath.Clean(root)){
   managed:=filepath.Clean(filepath.Join(s.dataDir,"runtimes","llamacpp"))
   clean:=filepath.Clean(oldRoot)
   if strings.HasPrefix(strings.ToLower(clean),strings.ToLower(managed)+string(os.PathSeparator)){
     if err:=os.RemoveAll(clean);err!=nil{
       // Keep the new, registered generation. A later guarded storage
       // cleanup can reclaim the old folder after Windows releases locks.
       fmt.Printf("llama.cpp obsolete generation cleanup deferred (%s): %v\\n",clean,err)
     }
   }
 }
 // Restored runtimes may reactivate preserved, previously unavailable
 // deployments without requiring another download of their model weights.
 _,err=s.db.ExecContext(ctx,"UPDATE model_deployments SET status='ready',revision=revision+1,updated_at=? WHERE status='unavailable' AND id IN (SELECT deployment_id FROM managed_local_models WHERE runtime_id=? AND status='ready')",s.clock.UnixMilli(),runtimeID)
 return err
}

func (s *Service) drainLlamaBackend(ctx context.Context,nodeID,name string)error{
 rows,err:=s.db.QueryContext(ctx,`SELECT DISTINCT i.deployment_id
 FROM local_runtime_instances i JOIN managed_local_runtimes r ON r.id=i.runtime_id
 WHERE r.node_id=? AND r.runtime_name=? AND i.status IN ('starting','healthy','busy','draining')`,nodeID,name)
 if err!=nil{return err}
 var ids []string
 for rows.Next(){var id string;if err:=rows.Scan(&id);err!=nil{rows.Close();return err};ids=append(ids,id)}
 err=rows.Err();rows.Close();if err!=nil{return err}
 for _,id:=range ids{
  if err:=s.supervisor.Stop(ctx,id);err!=nil{return fmt.Errorf("cannot update %s while deployment %s is active: %w",name,id,err)}
 }
 return nil
}

func (s *Service) llamaRuntimeStatus(ctx context.Context) ([]LlamaRuntimeBackendStatus,error) {
	p,err:=s.latestHardwareProfile(ctx); if err!=nil{return nil,err}
	entries,err:=s.llamaRuntimeEntries(ctx,p); if err!=nil{return nil,err}
	recommended:=llamaRecommendedBackends(p); rows:=make([]LlamaRuntimeBackendStatus,0,len(entries))
	for _,entry:=range entries {
		inventory:=entry.Name+"@"+strings.ToLower(strings.TrimSpace(entry.Backend)); var version string
		err:=s.db.QueryRowContext(ctx,"SELECT runtime_version FROM managed_local_runtimes WHERE node_id=? AND runtime_name=? AND status='ready'",p.NodeID,inventory).Scan(&version)
		installed:=err==nil; if err!=nil && err!=sql.ErrNoRows{return nil,err}
		reason,rec:=recommended[strings.ToLower(strings.TrimSpace(entry.Backend))]; driver:=""
		if strings.EqualFold(entry.Backend,"cuda") { for _,g:=range p.GPUs { if strings.EqualFold(g.Vendor,"nvidia"){driver=g.DriverVersion;break} } }
		var instances,models int
		_ = s.db.QueryRowContext(ctx,"SELECT COUNT(*) FROM local_runtime_instances i JOIN managed_local_runtimes r ON r.id=i.runtime_id WHERE r.node_id=? AND r.runtime_name=? AND i.status IN ('starting','healthy','busy','draining')",p.NodeID,inventory).Scan(&instances)
		_ = s.db.QueryRowContext(ctx,"SELECT COUNT(*) FROM managed_local_models m JOIN managed_local_runtimes r ON r.id=m.runtime_id WHERE r.node_id=? AND r.runtime_name=? AND m.status NOT IN ('removed','failed')",p.NodeID,inventory).Scan(&models)
		rows=append(rows,LlamaRuntimeBackendStatus{Backend:entry.Backend,Version:entry.Version,Installed:installed,Recommended:rec,Reason:reason,DriverVersion:driver,ActiveInstances:instances,DependentModels:models})
	}
	return rows,nil
}

func (s *Service) LlamaRuntimeStatus(ctx context.Context) ([]LlamaRuntimeBackendStatus,error) {
	return s.llamaRuntimeStatus(ctx)
}

func (s *Service) installLlamaCpp(ctx context.Context,jobID string) error {
	p,err:=s.latestHardwareProfile(ctx); if err!=nil{return errors.New("detect hardware before installing llama.cpp runtimes")}
	entries,err:=s.llamaRuntimeEntries(ctx,p); if err!=nil{return err}
	recommended:=llamaRecommendedBackends(p); var installed []string
	for _,entry:=range entries {
		backend:=strings.ToLower(strings.TrimSpace(entry.Backend)); if _,ok:=recommended[backend]; !ok{continue}
		if err:=s.updateComponentProgress(ctx,jobID,"running","downloading-"+backend,"downloading",nil,false); err!=nil{return err}
		if err:=s.installLlamaRuntimeEntry(ctx,entry,p); err!=nil{return err}
		installed=append(installed,backend)
	}
	if len(installed)==0{return errors.New("hardware scan produced no supported llama.cpp runtime backend")}
	status,_:=s.llamaRuntimeStatus(ctx); meta,_:=json.Marshal(map[string]any{"backends":status,"installed_backends":installed,"hardware_profile_id":p.ID})
	now:=s.clock.UnixMilli()
	_,err=s.db.ExecContext(ctx,"UPDATE managed_component_states SET installed_version='b11430',available_version='b11430',desired_state='disabled',observed_state='installed_disabled',last_error=NULL,metadata_json=?,revision=revision+1,updated_at=? WHERE component_id='llamacpp'",string(meta),now)
	return err
}

func (s *Service) removeLlamaCpp(ctx context.Context) error {
	if s.llamaRuntimeInUse(ctx){return errors.New("stop active llama.cpp model runtimes before uninstalling llama.cpp")}
	if n:=s.llamaManagedModels(ctx); n>0{return fmt.Errorf("llama.cpp is still used by %d managed local model deployment(s); remove those models first",n)}
	p,err:=s.latestHardwareProfile(ctx); if err==nil{_,_=s.db.ExecContext(ctx,"DELETE FROM managed_local_runtimes WHERE node_id=? AND runtime_name LIKE 'llamacpp@%'",p.NodeID)}
	if err:=os.RemoveAll(filepath.Join(s.dataDir,"runtimes","llamacpp")); err!=nil{return err}
	now:=s.clock.UnixMilli(); _,err=s.db.ExecContext(ctx,"UPDATE managed_component_states SET installed_version=NULL,desired_state='removed',observed_state='removed',last_error=NULL,metadata_json='{}',revision=revision+1,updated_at=? WHERE component_id='llamacpp'",now)
	return err
}


// RemoveLlamaBackend removes one unused inference backend while preserving
// model-weight files and historical model/runtime provenance.
func (s *Service) RemoveLlamaBackend(ctx context.Context, backend string) error {
 backend=strings.ToLower(strings.TrimSpace(backend))
 if backend!="cpu"&&backend!="cuda"&&backend!="vulkan"{return errors.New("unsupported llama.cpp backend")}
 p,err:=s.latestHardwareProfile(ctx);if err!=nil{return err}
 name:="llamacpp@"+backend
 var id,root string
 err=s.db.QueryRowContext(ctx,"SELECT id,install_root FROM managed_local_runtimes WHERE node_id=? AND runtime_name=? AND status='ready'",p.NodeID,name).Scan(&id,&root)
 if err==sql.ErrNoRows{return errors.New("llama.cpp backend is not installed")}
 if err!=nil{return err}
 var models,active int
 if err=s.db.QueryRowContext(ctx,"SELECT COUNT(*) FROM managed_local_models WHERE runtime_id=? AND status NOT IN ('removed','failed')",id).Scan(&models);err!=nil{return err}
 if err=s.db.QueryRowContext(ctx,"SELECT COUNT(*) FROM local_runtime_instances WHERE runtime_id=? AND status IN ('starting','healthy','busy','draining')",id).Scan(&active);err!=nil{return err}
 // Stop any active instance through the identity-verified supervisor,
 // and retain all model weights and deployment registrations.
 if active>0 {
   rows,e:=s.db.QueryContext(ctx,"SELECT DISTINCT deployment_id FROM local_runtime_instances WHERE runtime_id=? AND status IN ('starting','healthy','busy','draining')",id)
   if e!=nil{return e}
   var deps []string
   for rows.Next(){var v string;if e:=rows.Scan(&v);e!=nil{rows.Close();return e};deps=append(deps,v)}
   e=rows.Err();rows.Close();if e!=nil{return e}
   for _,deploymentID:=range deps{
     if e:=s.supervisor.Stop(ctx,deploymentID);e!=nil{return fmt.Errorf("cannot safely stop deployment %s: %w",deploymentID,e)}
   }
 }
 // Only remove paths within the managed llmfit-independent llama.cpp root.
 trustedRoot:=filepath.Clean(filepath.Join(s.dataDir,"runtimes","llamacpp"))
 clean:=filepath.Clean(root)
 if !strings.HasPrefix(strings.ToLower(clean),strings.ToLower(trustedRoot)+string(os.PathSeparator)){return errors.New("runtime install path is outside the managed llama.cpp root")}
 // Disable the registered backend before cleaning up physical files.
 // If Windows retains a transient DLL lock, model deployments remain safe and
 // the disabled backend cannot be scheduled; cleanup can be retried later.
 now:=s.clock.UnixMilli()
 tx,err:=s.db.BeginTx(ctx,nil);if err!=nil{return err}
 defer tx.Rollback()
 if _,err=tx.ExecContext(ctx,"UPDATE model_deployments SET status='unavailable',residency_state='stopped',revision=revision+1,updated_at=? WHERE id IN (SELECT deployment_id FROM managed_local_models WHERE runtime_id=?)",now,id);err!=nil{return err}
 if _,err=tx.ExecContext(ctx,"UPDATE managed_local_runtimes SET status='disabled',revision=revision+1,updated_at=? WHERE id=?",now,id);err!=nil{return err}
 if err:=tx.Commit();err!=nil{return err}
 _ = os.RemoveAll(clean)
 return nil
}
