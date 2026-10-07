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
	Backend string
	Version string
	Installed bool
	Recommended bool
	Reason string
	DriverVersion string
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
	if st,err:=os.Stat(root); err==nil && st.IsDir() { _=os.RemoveAll(root) }
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
	_,err:=s.ensureManagedRuntime(ctx,p.NodeID,manifest,root,filepath.Join(root,manifest.ExecutableRel)); return err
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
		rows=append(rows,LlamaRuntimeBackendStatus{Backend:entry.Backend,Version:entry.Version,Installed:installed,Recommended:rec,Reason:reason,DriverVersion:driver})
	}
	return rows,nil
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
