package localai

import (
 "context"
 "encoding/json"
 "errors"
 "fmt"
 "strings"
)

// selectOneClickModel handles the exact requested artifact independently from the
// recommendation ranking. Model use-case labels are advisory, never install gates.
// Only digest-pinned signed/adopted artifacts and supported runtime manifests can install.
func (s *Service) selectOneClickModel(ctx context.Context, detected HardwareProfile, req OneClickInstallRequest, placement PlacementMode, preferGPU bool) (Recommendation, UseCase, error) {
 var zero Recommendation
 specs,err:=s.catalog.ModelSpecifications(ctx)
 if err!=nil{return zero,"",fmt.Errorf("load trusted model specifications: %w",err)}
 quant:=strings.ToUpper(strings.TrimSpace(req.Quantization))
 if _,ok:=quants[quant];!ok {return zero,"",fmt.Errorf("quantization %s is not supported by this managed install planner",quant)}
 var selected ModelSpec
 for _,spec:=range specs {
  if !strings.EqualFold(spec.ModelRef,req.ModelRef){continue}
  selected=spec
  break
 }
 if selected.ModelRef=="" {return zero,"",fmt.Errorf("model %s is not registered in the active catalogue",req.ModelRef)}
 hasQuant:=false
 for _,q:=range selected.Quantizations {if strings.EqualFold(q,quant){hasQuant=true;break}}
 if !hasQuant {
  adopted,e:=s.catalog.adoptedModelSpecs(ctx);if e!=nil{return zero,"",e}
  for _,spec:=range adopted {
   if !strings.EqualFold(spec.ModelRef,req.ModelRef){continue}
   for _,q:=range spec.Quantizations {if strings.EqualFold(q,quant){
    hasQuant=true
    // Preserve the base model's actual capabilities, including embedding,
    // but bind the source to the verified adopted artifact.
    selected.SourceRef=spec.SourceRef
    break
   }}
   if hasQuant{break}
  }
 }
 if !hasQuant{return zero,"",fmt.Errorf("quantization %s has no verified, registered artifact for %s",quant,selected.ModelRef)}
 selected.Quantizations=[]string{quant}
 useCase:=req.UseCase
 supported:=supportsUseCase(selected,useCase)
 if !supported {
  if len(selected.UseCases)==0{return zero,"",errors.New("model is missing a valid use-case classification")}
  useCase=selected.UseCases[0]
 }
 catalogRec,cat,err:=s.catalog.Active(ctx);_ = catalogRec
 if err!=nil{return zero,"",fmt.Errorf("active trusted catalogue unavailable: %w",err)}
 detected=filterHardwareForCatalog(detected,cat)
 contextTokens:=req.ContextTokens
 if contextTokens>selected.ContextLength{contextTokens=selected.ContextLength}
 if contextTokens<=0{return zero,"",errors.New("model does not advertise a valid context length")}
 recommendReq:=RecommendRequest{UseCase:useCase,ContextTokens:contextTokens,Limit:1,MinimumFit:FitMarginal,StorageHeadroomPct:0,PreferGPU:preferGPU,PlacementPreference:placement}
 recs,err:=Recommend(detected,[]ModelSpec{selected},recommendReq)
 if err!=nil{return zero,"",err}
 var rec Recommendation
 if len(recs)>0 {rec=recs[0]} else {
  if !req.AllowResourceOverride {
   return zero,"",errors.New("this quantization exceeds OnePane's estimated RAM/VRAM budget; choose a different compute placement or enable the experimental resource override")
  }
  required:=weightBytes(selected.ParamsB,quant)+contextOverhead(selected.ParamsB,contextTokens)
  pp,mode,available,fit:=bestPlacement(detected,required,placement)
  if pp.Mode=="" {
   // Estimated memory can be exceeded only by explicit operator override.
   // Missing hardware/backend capabilities remain hard failures.
   var overrideErr error
   pp,mode,available,overrideErr=experimentalPlacement(detected,required,placement)
   if overrideErr!=nil{return zero,"",overrideErr}
   fit=FitTooTight
  }
  pp.Experimental=true
  pp.Notes=append(pp.Notes,"explicit operator override of estimated resource fit")
  rec=Recommendation{Model:selected,Quantization:quant,ContextTokens:contextTokens,FitLevel:fit,RunMode:mode,EstimateSource:"onepane_native",EstimateConfidence:"estimated",MemoryRequired:required,MemoryAvailable:available,Placement:pp,Notes:[]string{"User override: runtime may fail to load due to physical memory limits"}}
 }
 raw,_:=json.Marshal(rec)
 probe:=InstallPlan{NodeID:detected.NodeID,HardwareProfileID:detected.ID,ModelRef:selected.ModelRef,Quantization:quant,SourceRef:selected.SourceRef,RuntimeName:selected.Runtime,PlanJSON:raw}
 _,runtimeArtifact,modelArtifact,err:=s.catalog.ResolvePlan(ctx,probe)
 if err!=nil{return zero,"",fmt.Errorf("verified artifact/runtime resolution failed: %w",err)}
 if modelArtifact.SizeBytes<=0{return zero,"",errors.New("model artifact size is invalid")}
 // A performance override cannot bypass physical storage or integrity limits.
 modelDisk:=currentStorage(storageProbePath(s.modelRoot))
 runtimeDisk:=currentStorage(storageProbePath(s.dataDir))
 modelNeed:=safeAddBytes(modelArtifact.SizeBytes,modelArtifact.SizeBytes)
 var runtimeDownload,runtimeReserve int64
 for _,rt:=range cat.Runtimes {
  if strings.EqualFold(rt.Name,runtimeArtifact.Name)&&strings.EqualFold(rt.Backend,runtimeArtifact.Backend)&&strings.EqualFold(rt.OS,detected.OSName)&&strings.EqualFold(rt.Architecture,detected.Architecture) {
   if !s.trustedRuntimeAlreadyInstalled(ctx,detected.NodeID,rt) {
    runtimeDownload=runtimeDownloadBudget(rt)
    runtimeReserve=runtimeInstallReserve(runtimeDownload)
   }
   break
  }
 }
 runtimeNeed:=safeAddBytes(runtimeDownload,runtimeReserve)
 if pathWithin(s.dataDir,s.modelRoot) {
  if modelDisk.AvailableBytes<safeAddBytes(modelNeed,runtimeNeed) {
   return zero,"",fmt.Errorf("insufficient physical disk space for verified downloads and runtime staging: need %.1f GiB, available %.1f GiB",float64(safeAddBytes(modelNeed,runtimeNeed))/float64(1<<30),float64(modelDisk.AvailableBytes)/float64(1<<30))
  }
 } else if modelDisk.AvailableBytes<modelNeed || runtimeDisk.AvailableBytes<runtimeNeed {
  return zero,"",errors.New("insufficient physical disk space for the model and managed runtime installation")
 }
 rec.ModelArtifactBytes=modelArtifact.SizeBytes
 rec.RuntimeDownloadBytes=runtimeDownload
 rec.RuntimeInstallReserve=runtimeReserve
 rec.DiskRequired=safeAddBytes(modelArtifact.SizeBytes,runtimeReserve)
 rec.DownloadScratch=safeAddBytes(modelArtifact.SizeBytes,runtimeDownload)
 if !supported{rec.Notes=append(rec.Notes,fmt.Sprintf("Model is classified as %s; installation is permitted despite requested %s use case",useCase,req.UseCase))}
 rec.Notes=append(rec.Notes,"Selected exact quantization resolved against a digest-pinned artifact")
 return rec,useCase,nil
}

// experimentalPlacement is allowed ONLY for explicitly overridden memory
// estimates. It does not create missing GPUs, cross incompatible backends or
// override verified artifact and physical disk requirements.
func experimentalPlacement(p HardwareProfile, required int64, preference PlacementMode) (PlacementPlan, RunMode, int64, error) {
    ram := p.Memory.AvailableBytes
    backend, accelerators := chooseGroup(groupByBackend(p))
    var gpu GPU
    hasGPU := false
    for _, g := range accelerators {
        if !hasGPU || g.VRAMBytes > gpu.VRAMBytes {gpu=g;hasGPU=true}
    }
    if preference==PlacementSingleDevice {
        if !hasGPU {return PlacementPlan{},"",0,errors.New("GPU-only placement requested but no compatible GPU backend is available; override cannot invent hardware")}
        return PlacementPlan{Mode:PlacementSingleDevice,Backend:backend,Experimental:true,Devices:[]PlacementDevice{accelDevice(gpu,backend,required)}},RunGPU,gpu.VRAMBytes,nil
    }
    if preference==PlacementCPUOffload || (preference=="" && hasGPU && ram>0) {
        if !hasGPU || ram<=0 {return PlacementPlan{},"",0,errors.New("hybrid placement requires compatible GPU and CPU memory; override cannot invent hardware")}
        gpuBytes:=required
        if gpuBytes>gpu.VRAMBytes {gpuBytes=gpu.VRAMBytes}
        if gpuBytes<0 {gpuBytes=0}
        return PlacementPlan{Mode:PlacementCPUOffload,Backend:backend,Experimental:true,
            Devices:[]PlacementDevice{accelDevice(gpu,backend,gpuBytes),
                {Kind:"cpu",Name:p.CPU.Name,Backend:"cpu",CapacityBytes:ram,AllocatedBytes:required-gpuBytes}}},
            RunCPUGPU,gpu.VRAMBytes+ram,nil
    }
    if preference!="" && preference!=PlacementCPUOnly {
        return PlacementPlan{},"",0,errors.New("requested accelerator placement is unsupported for an experimental override")
    }
    if ram<=0{return PlacementPlan{},"",0,errors.New("CPU memory unavailable for experimental placement")}
    return PlacementPlan{Mode:PlacementCPUOnly,Backend:"cpu",Experimental:true,
        Devices:[]PlacementDevice{{Kind:"cpu",Name:p.CPU.Name,Backend:"cpu",CapacityBytes:ram,AllocatedBytes:required}}},RunCPU,ram,nil
}
