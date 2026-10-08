package localai

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strings"
)

type omniRouteNodeArtifact struct {
	Version string
	SourceURL string
	SHA256 string
	ArchiveFormat string
	RootDir string
}

func omniRouteManagedNodeArtifact() (omniRouteNodeArtifact,error) {
	if goruntime.GOOS!="windows" || goruntime.GOARCH!="amd64" { return omniRouteNodeArtifact{},errors.New("managed OmniRoute CLI Node runtime is currently Windows amd64 only") }
	return omniRouteNodeArtifact{Version:"22.22.2",SourceURL:"https://nodejs.org/download/release/v22.22.2/node-v22.22.2-win-x64.zip",SHA256:"7c93e9d92bf68c07182b471aa187e35ee6cd08ef0f24ab060dfff605fcc1c57c",ArchiveFormat:"zip",RootDir:"node-v22.22.2-win-x64"},nil
}

func (s *Service) omniRouteCLINodePath(root string) string {
	a,_:=omniRouteManagedNodeArtifact()
	return filepath.Join(root,a.RootDir,"node.exe")
}
func (s *Service) omniRouteCLINPMPath(root string) string {
	a,_:=omniRouteManagedNodeArtifact()
	return filepath.Join(root,a.RootDir,"node_modules","npm","bin","npm-cli.js")
}
func (s *Service) omniRouteCLIEntryPath(root string) string {
	return filepath.Join(root,"app","node_modules","omniroute","bin","omniroute.mjs")
}

func (s *Service) installOmniRouteCLIWindows(ctx context.Context,jobID string) error {
	if goruntime.GOOS!="windows" || goruntime.GOARCH!="amd64" { return errors.New("managed OmniRoute CLI install is unsupported on this platform") }
	a,err:=omniRouteManagedNodeArtifact();if err!=nil{return err}
	if err:=s.updateComponentProgress(ctx,jobID,"running","downloading-node","downloading",nil,false);err!=nil{return err}
	// Node, the verified CLI packages and npm staging can exceed 3 GiB.
	if err:=checkRuntimeDiskBudget(filepath.Join(s.dataDir,"runtimes"),6<<30);err!=nil{return err}
	dlDir:=filepath.Join(s.dataDir,"components","downloads");if err:=os.MkdirAll(dlDir,0o700);err!=nil{return err}
	archive:=filepath.Join(dlDir,"node-v"+a.Version+"-win-x64.zip")
	if _,err:=s.fetcher.Fetch(ctx,a.SourceURL,archive,a.SHA256);err!=nil{return fmt.Errorf("download managed Node runtime: %w",err)}
	root:=s.omniRouteRuntimeRoot();staging:=root+".installing";_ = os.RemoveAll(staging)
	if err:=os.MkdirAll(staging,0o700);err!=nil{return err}
	if err:=s.updateComponentProgress(ctx,jobID,"running","installing-node","installing",nil,false);err!=nil{return err}
	if err:=ExtractRuntimeArchive(archive,a.ArchiveFormat,staging);err!=nil{_ = os.RemoveAll(staging);return fmt.Errorf("extract managed Node runtime: %w",err)}
	node:=s.omniRouteCLINodePath(staging);npm:=s.omniRouteCLINPMPath(staging)
	for _,p:=range []string{node,npm}{if st,err:=os.Stat(p);err!=nil||st.IsDir(){_ = os.RemoveAll(staging);return fmt.Errorf("managed Node runtime is incomplete: %s",filepath.Base(p))}}
	appRoot:=filepath.Join(staging,"app");if err:=os.MkdirAll(appRoot,0o700);err!=nil{_ = os.RemoveAll(staging);return err}
	if err:=s.updateComponentProgress(ctx,jobID,"running","installing-omniroute-cli","installing",nil,false);err!=nil{return err}
	cmd:=exec.CommandContext(ctx,node,npm,"install","--prefix",appRoot,"omniroute@3.8.51","--include=optional","--no-audit","--no-fund","--loglevel=error")
	logDir:=filepath.Join(s.dataDir,"components","logs");_ = os.MkdirAll(logDir,0o700)
	logf,e:=os.OpenFile(filepath.Join(logDir,"omniroute-install.log"),os.O_CREATE|os.O_TRUNC|os.O_WRONLY,0o600);if e!=nil{_ = os.RemoveAll(staging);return e}
	cmd.Stdout,cmd.Stderr=logf,logf
	// npm runs under the OnePane Windows service account. Without a scoped
	// cache it writes to systemprofile\\AppData\\Local\\npm-cache and every
	// retry can accumulate another archive tree on the system drive.
	// Keep the ephemeral npm cache and Node compile cache inside an owned path.
	cacheRoot:=filepath.Join(s.dataDir,"components","install-cache","omniroute")
	if err:=os.MkdirAll(cacheRoot,0o700);err!=nil{_ = logf.Close();_ = os.RemoveAll(staging);return err}
	defer os.RemoveAll(cacheRoot)
	nodeDir:=filepath.Dir(node)
	cmd.Env=append(os.Environ(),
		"PATH="+nodeDir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"npm_config_cache="+filepath.Join(cacheRoot,"npm"),
		"NODE_COMPILE_CACHE="+filepath.Join(cacheRoot,"node-compile"),
		"TEMP="+cacheRoot,"TMP="+cacheRoot,
		"npm_config_update_notifier=false","npm_config_fund=false","npm_config_audit=false")
	err=cmd.Run();_ = logf.Close();if err!=nil{tail:=s.omniRouteInstallLogTail(4096);_ = os.RemoveAll(staging);return fmt.Errorf("install OmniRoute CLI 3.8.51: %v%s",err,tail)}
	cli:=s.omniRouteCLIEntryPath(staging);if st,err:=os.Stat(cli);err!=nil||st.IsDir(){_ = os.RemoveAll(staging);return errors.New("OmniRoute CLI entry is missing after npm install")}
	backup:=root+".previous";_ = os.RemoveAll(backup);if _,err:=os.Stat(root);err==nil{if err:=os.Rename(root,backup);err!=nil{_ = os.RemoveAll(staging);return err}}
	if err:=os.Rename(staging,root);err!=nil{if _,e:=os.Stat(backup);e==nil{_ = os.Rename(backup,root)};return err};_ = os.RemoveAll(backup)
	// The verified Node ZIP is no longer needed after successful extraction.
	// Interrupted transfers retain the .partial file via HTTPFetcher.
	_ = os.Remove(archive)
	meta:=fmt.Sprintf(`{"runtime_kind":"node-cli","node_version":"%s","package":"omniroute@3.8.51","endpoint":"http://127.0.0.1:20128/v1","data_root":%q}`,a.Version,s.omniRouteDataRoot())
	now:=s.clock.UnixMilli();_,err=s.db.ExecContext(ctx,"UPDATE managed_component_states SET installed_version='3.8.51',available_version='3.8.51',desired_state='disabled',observed_state='installed_disabled',last_error=NULL,metadata_json=?,revision=revision+1,updated_at=? WHERE component_id='omniroute'",meta,now)
	return err
}

func (s *Service) omniRouteCLICommand() (string,[]string,error) {
	if goruntime.GOOS!="windows" { return " ",nil,errors.New("managed OmniRoute CLI command is not available on this platform") }
	root:=s.omniRouteRuntimeRoot();node:=s.omniRouteCLINodePath(root);cli:=s.omniRouteCLIEntryPath(root)
	for _,p:=range []string{node,cli}{if st,err:=os.Stat(p);err!=nil||st.IsDir(){return "",nil,errors.New("managed OmniRoute uses the retired desktop artifact; click Update to install the headless CLI runtime")}}
	return node,[]string{cli,"--no-open","--port","20128"},nil
}

func (s *Service) omniRouteInstallLogTail(limit int64) string {
	path:=filepath.Join(s.dataDir,"components","logs","omniroute-install.log");raw,err:=os.ReadFile(path);if err!=nil{return ""}
	if int64(len(raw))>limit{raw=raw[len(raw)-int(limit):]}
	t:=strings.TrimSpace(string(raw));if t==""{return ""};if len(t)>1200{t=t[len(t)-1200:]}
	return ": "+t
}
