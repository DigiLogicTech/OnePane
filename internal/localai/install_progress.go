package localai

import (
	"context"
	"os"
	"path/filepath"
	"strings"
)

func currentArtifactBytes(path string, expected int64) int64 {
	if expected <= 0 { return 0 }
	if st,err:=os.Stat(path);err==nil&&!st.IsDir(){if st.Size()>expected{return expected};return st.Size()}
	if st,err:=os.Stat(path+".partial");err==nil&&!st.IsDir(){if st.Size()>expected{return expected};return st.Size()}
	return 0
}

func (s *Service) enrichInstallJobProgress(ctx context.Context,j *InstallJob){
	if j==nil{return}
	switch j.Status {
	case InstallJobQueued:
		j.ProgressPct=0;j.CurrentArtifact="Queued"
	case InstallJobResolving:
		j.ProgressPct=1;j.CurrentArtifact="Resolving trusted catalogue"
	case InstallJobStarting:
		j.ProgressPct=96;j.CurrentArtifact="Starting managed runtime"
	case InstallJobQualifying:
		j.ProgressPct=98;j.CurrentArtifact="Qualifying model deployment"
	case InstallJobReady:
		j.ProgressPct=100;j.CurrentArtifact="Ready"
	case InstallJobInterrupted:
		j.Resumable=true;j.CurrentArtifact="Interrupted download — resumable"
	case InstallJobFailed:
		j.CurrentArtifact="Install failed"
	case InstallJobCancelled:
		j.CurrentArtifact="Cancelled"
	}
	if j.Status!=InstallJobProvisioning||j.CatalogID==nil||s.catalog==nil{return}
	p,err:=s.Plan(ctx,j.PlanID);if err!=nil{return}
	_,runtime,model,err:=s.catalog.ResolvePlanWithCatalog(ctx,*j.CatalogID,p);if err!=nil{return}
	_,cat,err:=s.catalog.ByID(ctx,*j.CatalogID);if err!=nil{return}
	var runtimeEntry *RuntimeCatalogEntry
	for i:=range cat.Runtimes{x:=&cat.Runtimes[i];if strings.EqualFold(x.Name,runtime.Name)&&strings.EqualFold(x.Version,runtime.Version)&&strings.EqualFold(x.Backend,runtime.Backend)&&strings.EqualFold(x.OS,runtime.OS)&&strings.EqualFold(x.Architecture,runtime.Architecture){runtimeEntry=x;break}}
	var total,done int64
	current:="Provisioning"
	backend:=strings.ToLower(strings.TrimSpace(runtime.Backend));if backend==""{backend="generic"}
	runtimeDone:=false
	if runtimeEntry!=nil{
		total+=runtimeEntry.SizeBytes
		for _,d:=range runtimeEntry.Dependencies{total+=d.SizeBytes}
		if s.trustedRuntimeAlreadyInstalled(ctx,p.NodeID,*runtimeEntry){
			done+=runtimeEntry.SizeBytes;for _,d:=range runtimeEntry.Dependencies{done+=d.SizeBytes};runtimeDone=true
		}else{
			archive:=filepath.Join(s.dataDir,"downloads","runtime-"+runtime.Name+"-"+runtime.Version+"-"+backend)
			rb:=currentArtifactBytes(archive,runtimeEntry.SizeBytes);done+=rb
			if rb<runtimeEntry.SizeBytes{current="Downloading llama.cpp "+strings.ToUpper(backend)+" runtime"}
			for i,d:=range runtimeEntry.Dependencies{pth:=filepath.Join(s.dataDir,"downloads","runtime-"+runtime.Name+"-"+runtime.Version+"-"+backend+"-dependency-"+pad2(i+1));n:=currentArtifactBytes(pth,d.SizeBytes);done+=n;if current=="Provisioning"&&n<d.SizeBytes{current="Downloading "+d.Name}}
		}
	}
	total+=model.SizeBytes
	filename:=filepath.Base(model.Filename);if model.ExpectedSHA256!=""&&len(model.ExpectedSHA256)>=16{filename=strings.ToLower(model.ExpectedSHA256[:16])+"-"+filename}
	modelPath:=filepath.Join(s.modelRoot,filename);mb:=currentArtifactBytes(modelPath,model.SizeBytes);done+=mb
	if (current=="Provisioning"||runtimeDone)&&mb<model.SizeBytes{current="Downloading "+p.ModelRef+" "+p.Quantization}
	j.BytesTotal=total;j.BytesDownloaded=done;j.CurrentArtifact=current
	if total>0{pct:=float64(done)*94/float64(total);if pct<2{pct=2};if pct>95{pct=95};j.ProgressPct=pct}
}

func pad2(n int) string {
	if n<10{return "0"+string(rune('0'+n))}
	return string(rune('0'+n/10))+string(rune('0'+n%10))
}
