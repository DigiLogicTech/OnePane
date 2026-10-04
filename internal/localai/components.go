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
	"strings"
	"sync"
	"time"
)

type ManagedComponent struct {
	ID               string          `json:"id"`
	DisplayName      string          `json:"display_name"`
	AvailableVersion string          `json:"available_version,omitempty"`
	InstalledVersion *string         `json:"installed_version,omitempty"`
	DesiredState     string          `json:"desired_state"`
	State            string          `json:"state"`
	Installed        bool            `json:"installed"`
	Enabled          bool            `json:"enabled"`
	Sandbox          string          `json:"sandbox"`
	GPU              bool            `json:"gpu"`
	ModelPool        bool            `json:"model_pool"`
	Internet         bool            `json:"internet"`
	TrustedNodes     bool            `json:"trusted_nodes"`
	Inbound          bool            `json:"inbound"`
	LastError        string          `json:"last_error,omitempty"`
	ActiveJobID      *string         `json:"active_job_id,omitempty"`
	Metadata         json.RawMessage `json:"metadata,omitempty"`
	Revision         int64           `json:"revision"`
	UpdatedAt        int64           `json:"updated_at"`
}

type ComponentJob struct {
	ID             string          `json:"id"`
	ComponentID    string          `json:"component_id"`
	Action         string          `json:"action"`
	TargetVersion  *string         `json:"target_version,omitempty"`
	Status         string          `json:"status"`
	Stage          string          `json:"stage"`
	AttemptCount   int             `json:"attempt_count"`
	FailureReason  *string         `json:"failure_reason,omitempty"`
	RequestedBy    *string         `json:"requested_by,omitempty"`
	Detail         json.RawMessage `json:"detail"`
	Revision       int64           `json:"revision"`
	CreatedAt      int64           `json:"created_at"`
	StartedAt      *int64          `json:"started_at,omitempty"`
	CompletedAt    *int64          `json:"completed_at,omitempty"`
	UpdatedAt      int64           `json:"updated_at"`
}

type ColibriArtifact struct {
	Manifest  RuntimeManifest
	EngineRel string
}

var componentMu sync.Mutex

func colibriArtifact() (ColibriArtifact, error) {
	const version = "1.12.1"
	switch {
	case goruntime.GOOS == "windows" && goruntime.GOARCH == "amd64":
		return ColibriArtifact{Manifest: RuntimeManifest{
			Name:"colibri", Version:version, Backend:"colibri", OS:"windows", Architecture:"amd64",
			SourceURL:"https://github.com/JustVugg/colibri/releases/download/v1.12.1/colibri-v1.12.1-windows-x86_64.zip",
			SHA256:"1d0cc6760a6e53fcafe77376ca20ec254c6ff8995e55fba1bcd883278787f794",
			ArchiveFormat:"zip", ExecutableRel:"openai_server.py",
		}, EngineRel:"colibri.exe"}, nil
	case goruntime.GOOS == "linux" && goruntime.GOARCH == "amd64":
		return ColibriArtifact{Manifest: RuntimeManifest{
			Name:"colibri", Version:version, Backend:"colibri", OS:"linux", Architecture:"amd64",
			SourceURL:"https://github.com/JustVugg/colibri/releases/download/v1.12.1/colibri-v1.12.1-linux-x86_64.tar.gz",
			SHA256:"9d5afd587d7c429b2cf0ce0c520d42f4c2883bdc9c560ed9fb5afa0b2c2e0d12",
			ArchiveFormat:"tar.gz", ExecutableRel:"openai_server.py",
		}, EngineRel:"colibri"}, nil
	default:
		return ColibriArtifact{}, fmt.Errorf("Colibri %s/%s is not supported by the Alpha 3.1 managed installer", goruntime.GOOS, goruntime.GOARCH)
	}
}

func componentDefinition(id string) (ManagedComponent, bool) {
	if id != "colibri" { return ManagedComponent{}, false }
	return ManagedComponent{ID:"colibri",DisplayName:"Colibri Large Model",AvailableVersion:"1.12.1",Sandbox:"managed_component",GPU:true,ModelPool:true,Internet:true,TrustedNodes:true,Inbound:false},true
}

func (s *Service) componentRuntimeRoot() string {
	return filepath.Join(s.dataDir,"runtimes","colibri","1.12.1")
}
func (s *Service) legacyComponentStatePath() string {
	return filepath.Join(s.dataDir,"components","state.json")
}

func scanComponent(row interface{Scan(...any) error}) (ManagedComponent,error) {
	var idv,available,desired,state,meta string
	var installed,lastErr,active sql.NullString
	var revision,updated int64
	if err:=row.Scan(&idv,&installed,&available,&desired,&state,&lastErr,&active,&meta,&revision,&updated);err!=nil{return ManagedComponent{},err}
	d,ok:=componentDefinition(idv);if !ok{return ManagedComponent{},errors.New("unknown managed component")}
	d.AvailableVersion=available;d.DesiredState=desired;d.State=state;d.Revision=revision;d.UpdatedAt=updated;d.Metadata=json.RawMessage(meta)
	if installed.Valid{v:=installed.String;d.InstalledVersion=&v}
	if lastErr.Valid{d.LastError=lastErr.String};if active.Valid{d.ActiveJobID=&active.String}
	d.Installed=d.InstalledVersion!=nil&&state!="not_installed"&&state!="removed"
	d.Enabled=desired=="enabled"&&(state=="running"||state=="enabling"||state=="degraded")
	return d,nil
}
func scanComponentJob(row interface{Scan(...any) error})(ComponentJob,error){
	var j ComponentJob;var target,fail,actor sql.NullString;var detail string;var started,completed sql.NullInt64
	if err:=row.Scan(&j.ID,&j.ComponentID,&j.Action,&target,&j.Status,&j.Stage,&j.AttemptCount,&fail,&actor,&detail,&j.Revision,&j.CreatedAt,&started,&completed,&j.UpdatedAt);err!=nil{return ComponentJob{},err}
	if target.Valid{j.TargetVersion=&target.String};if fail.Valid{j.FailureReason=&fail.String};if actor.Valid{j.RequestedBy=&actor.String};if started.Valid{v:=started.Int64;j.StartedAt=&v};if completed.Valid{v:=completed.Int64;j.CompletedAt=&v};j.Detail=json.RawMessage(detail);return j,nil
}

func (s *Service) ManagedComponents(ctx context.Context) (map[string]ManagedComponent,error) {
	now:=s.clock.UnixMilli()
	_,_ = s.db.ExecContext(ctx,`INSERT OR IGNORE INTO managed_component_states(component_id,available_version,desired_state,observed_state,metadata_json,revision,updated_at) VALUES('colibri','1.12.1','disabled','not_installed','{}',1,?)`,now)
	x,err:=scanComponent(s.db.QueryRowContext(ctx,`SELECT component_id,installed_version,COALESCE(available_version,''),desired_state,observed_state,last_error,active_job_id,metadata_json,revision,updated_at FROM managed_component_states WHERE component_id='colibri'`))
	if err!=nil{return nil,err};return map[string]ManagedComponent{"colibri":x},nil
}
func (s *Service) ComponentJob(ctx context.Context,idv string)(ComponentJob,error){
	return scanComponentJob(s.db.QueryRowContext(ctx,`SELECT id,component_id,action,target_version,status,stage,attempt_count,failure_reason,requested_by,detail_json,revision,created_at,started_at,completed_at,updated_at FROM managed_component_jobs WHERE id=?`,strings.TrimSpace(idv)))
}

func validComponentAction(a string) bool {
	switch a {case "install","enable","disable","update","repair","retry","resume","remove":return true};return false
}

func (s *Service) RequestComponentAction(ctx context.Context,idv,action string,actor *string)(ComponentJob,error){
	idv,action=strings.ToLower(strings.TrimSpace(idv)),strings.ToLower(strings.TrimSpace(action))
	if idv!="colibri"||!validComponentAction(action){return ComponentJob{},errors.New("unsupported managed component action")}
	componentMu.Lock();defer componentMu.Unlock()
	all,err:=s.ManagedComponents(ctx);if err!=nil{return ComponentJob{},err};c:=all[idv]
	if c.ActiveJobID!=nil {
		if j,e:=s.ComponentJob(ctx,*c.ActiveJobID);e==nil&&(j.Status=="queued"||j.Status=="running"){return ComponentJob{},errors.New("component already has an active lifecycle job")}
	}
	switch action {
	case "enable":
		if !c.Installed{return ComponentJob{},errors.New("component must be installed before it can be enabled")}
	case "disable","remove","repair","update":
		if !c.Installed{return ComponentJob{},errors.New("component is not installed")}
	case "resume":
		if c.State!="interrupted"{return ComponentJob{},errors.New("resume requires an interrupted component job")}
	case "retry":
		if c.State!="failed"{return ComponentJob{},errors.New("retry requires a failed component job")}
	}
	jid,err:=s.ids.New("cjob");if err!=nil{return ComponentJob{},err};now:=s.clock.UnixMilli();target:="1.12.1"
	_,err=s.db.ExecContext(ctx,`INSERT INTO managed_component_jobs(id,component_id,action,target_version,status,stage,attempt_count,failure_reason,requested_by,detail_json,revision,created_at,updated_at) VALUES(?,?,?,?,'queued','queued',0,NULL,?,'{}',1,?,?)`,jid,idv,action,target,actor,now,now);if err!=nil{return ComponentJob{},err}
	_,err=s.db.ExecContext(ctx,`UPDATE managed_component_states SET active_job_id=?,observed_state='queued',last_error=NULL,revision=revision+1,updated_at=? WHERE component_id=?`,jid,now,idv);if err!=nil{return ComponentJob{},err}
	job,err:=s.ComponentJob(ctx,jid);if err!=nil{return ComponentJob{},err}
	go s.runComponentJob(context.Background(),jid)
	return job,nil
}

func (s *Service) updateComponentProgress(ctx context.Context,jobID,jobStatus,stage,state string,failure *string,terminal bool) error {
	now:=s.clock.UnixMilli()
	var completed any=nil;if terminal{completed=now}
	_,err:=s.db.ExecContext(ctx,`UPDATE managed_component_jobs SET status=?,stage=?,failure_reason=?,attempt_count=CASE WHEN status='queued' THEN attempt_count+1 ELSE attempt_count END,started_at=COALESCE(started_at,?),completed_at=?,revision=revision+1,updated_at=? WHERE id=?`,jobStatus,stage,failure,now,completed,now,jobID);if err!=nil{return err}
	var cid string;if err:=s.db.QueryRowContext(ctx,`SELECT component_id FROM managed_component_jobs WHERE id=?`,jobID).Scan(&cid);err!=nil{return err}
	active:=any(jobID);if terminal{active=nil}
	_,err=s.db.ExecContext(ctx,`UPDATE managed_component_states SET observed_state=?,last_error=?,active_job_id=?,revision=revision+1,updated_at=? WHERE component_id=?`,state,failure,active,now,cid);return err
}
func (s *Service) failComponentJob(ctx context.Context,jobID string,err error){
	msg:=err.Error();_ = s.updateComponentProgress(ctx,jobID,"failed","failed","failed",&msg,true)
}
func (s *Service) colibriInUse(ctx context.Context) bool {
	var n int
	_ = s.db.QueryRowContext(ctx,`SELECT COUNT(*) FROM local_runtime_instances i JOIN managed_local_runtimes r ON r.id=i.runtime_id WHERE r.runtime_name LIKE 'colibri%' AND i.status IN ('starting','healthy','busy','stopping')`).Scan(&n)
	return n>0
}
func (s *Service) installColibri(ctx context.Context,jobID string) error {
	a,err:=colibriArtifact();if err!=nil{return err}
	if s.colibriInUse(ctx){return errors.New("stop active Colibri model runtimes before updating or repairing Colibri")}
	if err:=s.updateComponentProgress(ctx,jobID,"running","downloading","downloading",nil,false);err!=nil{return err}
	dlDir:=filepath.Join(s.dataDir,"components","downloads");if err:=os.MkdirAll(dlDir,0o700);err!=nil{return err}
	ext:=".zip";if a.Manifest.ArchiveFormat=="tar.gz"{ext=".tar.gz"}
	archive:=filepath.Join(dlDir,fmt.Sprintf("colibri-%s-%s-%s%s",a.Manifest.Version,a.Manifest.OS,a.Manifest.Architecture,ext))
	if _,err:=s.fetcher.Fetch(ctx,a.Manifest.SourceURL,archive,a.Manifest.SHA256);err!=nil{return err}
	if err:=s.updateComponentProgress(ctx,jobID,"running","verifying","verifying",nil,false);err!=nil{return err}
	root:=s.componentRuntimeRoot();staging:=root+".installing";_ = os.RemoveAll(staging)
	if err:=s.updateComponentProgress(ctx,jobID,"running","installing","installing",nil,false);err!=nil{return err}
	if err:=ExtractRuntimeArchive(archive,a.Manifest.ArchiveFormat,staging);err!=nil{_ = os.RemoveAll(staging);return err}
	server:=filepath.Join(staging,a.Manifest.ExecutableRel);engine:=filepath.Join(staging,a.EngineRel)
	if st,err:=os.Stat(server);err!=nil||st.IsDir(){_ = os.RemoveAll(staging);return errors.New("Colibri openai_server.py missing from release archive")}
	if st,err:=os.Stat(engine);err!=nil||st.IsDir(){_ = os.RemoveAll(staging);return errors.New("Colibri engine missing from release archive")}
	if goruntime.GOOS!="windows"{if err:=os.Chmod(engine,0o700);err!=nil{_ = os.RemoveAll(staging);return err}}
	backup:=root+".previous";_ = os.RemoveAll(backup)
	if _,err:=os.Stat(root);err==nil{if err:=os.Rename(root,backup);err!=nil{_ = os.RemoveAll(staging);return err}}
	if err:=os.Rename(staging,root);err!=nil{if _,e:=os.Stat(backup);e==nil{_ = os.Rename(backup,root)};return err};_ = os.RemoveAll(backup)
	now:=s.clock.UnixMilli()
	meta,_:=json.Marshal(map[string]any{"os":a.Manifest.OS,"arch":a.Manifest.Architecture,"source_url":a.Manifest.SourceURL,"sha256":a.Manifest.SHA256,"engine_rel":a.EngineRel,"server_rel":a.Manifest.ExecutableRel})
	_,err=s.db.ExecContext(ctx,`UPDATE managed_component_states SET installed_version=?,available_version=?,desired_state='disabled',observed_state='installed_disabled',last_error=NULL,metadata_json=?,revision=revision+1,updated_at=? WHERE component_id='colibri'`,a.Manifest.Version,a.Manifest.Version,string(meta),now);return err
}
func (s *Service) runComponentJob(ctx context.Context,jobID string){
	j,err:=s.ComponentJob(ctx,jobID);if err!=nil{return}
	action:=j.Action
	if action=="retry"||action=="resume" { action="repair" }
	var runErr error
	switch action {
	case "install","update","repair":
		runErr=s.installColibri(ctx,jobID)
	case "enable":
		now:=s.clock.UnixMilli();_,runErr=s.db.ExecContext(ctx,`UPDATE managed_component_states SET desired_state='enabled',observed_state='running',last_error=NULL,revision=revision+1,updated_at=? WHERE component_id='colibri' AND installed_version IS NOT NULL`,now)
	case "disable":
		now:=s.clock.UnixMilli();_,runErr=s.db.ExecContext(ctx,`UPDATE managed_component_states SET desired_state='disabled',observed_state='installed_disabled',last_error=NULL,revision=revision+1,updated_at=? WHERE component_id='colibri'`,now)
	case "remove":
		if s.colibriInUse(ctx){runErr=errors.New("stop active Colibri model runtimes before removing Colibri");break}
		_ = s.updateComponentProgress(ctx,jobID,"running","removing","removing",nil,false)
		runErr=os.RemoveAll(s.componentRuntimeRoot())
		if runErr==nil{now:=s.clock.UnixMilli();_,runErr=s.db.ExecContext(ctx,`UPDATE managed_component_states SET installed_version=NULL,desired_state='removed',observed_state='removed',last_error=NULL,metadata_json='{}',revision=revision+1,updated_at=? WHERE component_id='colibri'`,now)}
	default: runErr=errors.New("unsupported component job action")
	}
	if runErr!=nil{s.failComponentJob(ctx,jobID,runErr);return}
	all,err:=s.ManagedComponents(ctx);state:="installed_disabled";if err==nil{state=all["colibri"].State}
	_ = s.updateComponentProgress(ctx,jobID,"succeeded","complete",state,nil,true)
}

func (s *Service) RecoverManagedComponents(ctx context.Context) error {
	if err:=s.migrateLegacyComponentState(ctx);err!=nil{return err}
	now:=s.clock.UnixMilli()
	_,_ = s.db.ExecContext(ctx,`UPDATE managed_component_jobs SET status='interrupted',stage='interrupted',failure_reason='OnePane restarted during component lifecycle operation',completed_at=?,updated_at=?,revision=revision+1 WHERE status='running'`,now,now)
	_,_ = s.db.ExecContext(ctx,`UPDATE managed_component_states SET observed_state='interrupted',last_error='OnePane restarted during component lifecycle operation',active_job_id=NULL,revision=revision+1,updated_at=? WHERE observed_state IN ('downloading','verifying','installing','enabling','disabling','updating','repairing','removing')`,now)
	rows,err:=s.db.QueryContext(ctx,`SELECT id FROM managed_component_jobs WHERE status='queued' ORDER BY created_at`);if err!=nil{return err};defer rows.Close();var ids []string;for rows.Next(){var idv string;if rows.Scan(&idv)==nil{ids=append(ids,idv)}};for _,idv:=range ids{go s.runComponentJob(context.Background(),idv)};return rows.Err()
}

func (s *Service) migrateLegacyComponentState(ctx context.Context) error {
	path:=s.legacyComponentStatePath();b,err:=os.ReadFile(path);if errors.Is(err,os.ErrNotExist){return nil};if err!=nil{return err}
	var legacy struct{Components map[string]struct{Installed bool `json:"installed"`;Enabled bool `json:"enabled"`;State string `json:"state"`;LastError string `json:"last_error"`;UpdatedAt int64 `json:"updated_at"`} `json:"components"`}
	if json.Unmarshal(b,&legacy)!=nil{return nil}
	if st,ok:=legacy.Components["colibri"];ok&&st.Installed{desired:="disabled";observed:="installed_disabled";if st.Enabled{desired="enabled";observed="running"};updated:=st.UpdatedAt;if updated==0{updated=s.clock.UnixMilli()};_,_ = s.db.ExecContext(ctx,`UPDATE managed_component_states SET installed_version='1.12.1',desired_state=?,observed_state=?,last_error=NULLIF(?,''),revision=revision+1,updated_at=? WHERE component_id='colibri'`,desired,observed,st.LastError,updated)}
	return os.Rename(path,path+".migrated")
}

// ManageComponent is retained for internal/test compatibility. Product/API
// callers should use RequestComponentAction and poll ComponentJob.
func (s *Service) ManageComponent(ctx context.Context,idv,action string)(ManagedComponent,error){
	job,err:=s.RequestComponentAction(ctx,idv,action,nil);if err!=nil{return ManagedComponent{},err}
	deadline:=time.Now().Add(35*time.Minute)
	for time.Now().Before(deadline){j,e:=s.ComponentJob(ctx,job.ID);if e!=nil{return ManagedComponent{},e};if j.Status=="succeeded"||j.Status=="failed"||j.Status=="interrupted"{all,e:=s.ManagedComponents(ctx);if e!=nil{return ManagedComponent{},e};if j.Status!="succeeded"{if j.FailureReason!=nil{return all[idv],errors.New(*j.FailureReason)};return all[idv],errors.New("component lifecycle failed")};return all[idv],nil};select{case<-ctx.Done():return ManagedComponent{},ctx.Err();case<-time.After(50*time.Millisecond):}}
	return ManagedComponent{},errors.New("component lifecycle timed out")
}
