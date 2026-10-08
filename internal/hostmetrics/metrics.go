package hostmetrics

import (
 "context"
 "os"
 "os/exec"
 "strconv"
 "strings"
 "sync"
 "time"
 "runtime"
 "path/filepath"
 "sort"
)

type GPU struct {
 Name string `json:"name"`
 UsagePercent *float64 `json:"usage_percent,omitempty"`
 VRAMUsedBytes uint64 `json:"vram_used_bytes"`
 VRAMTotalBytes uint64 `json:"vram_total_bytes"`
 TemperatureC *float64 `json:"temperature_c,omitempty"`
}
type StorageVolume struct {
 Path string `json:"path"`
 Roles []string `json:"roles"`
 TotalBytes uint64 `json:"total_bytes"`
 FreeBytes uint64 `json:"free_bytes"`
}
type Snapshot struct {
 Timestamp int64 `json:"timestamp"`
 Hostname string `json:"hostname"`
 CPUPercent *float64 `json:"cpu_percent,omitempty"`
 MemoryTotalBytes uint64 `json:"memory_total_bytes,omitempty"`
 MemoryAvailableBytes uint64 `json:"memory_available_bytes,omitempty"`
 DiskTotalBytes uint64 `json:"disk_total_bytes,omitempty"`
 DiskFreeBytes uint64 `json:"disk_free_bytes,omitempty"`
 DiskPath string `json:"disk_path"`
 StorageVolumes []StorageVolume `json:"storage_volumes,omitempty"`
 NetworkRXBytesPerSec *float64 `json:"network_rx_bytes_per_sec,omitempty"`
 NetworkTXBytesPerSec *float64 `json:"network_tx_bytes_per_sec,omitempty"`
 ProcessRSSBytes uint64 `json:"process_rss_bytes,omitempty"`
 Goroutines int `json:"goroutines"`
 GPUs []GPU `json:"gpus"`
}
type Sampler struct {
 mu sync.Mutex
 cpuIdle,cpuTotal uint64
 rx,tx uint64
 previous time.Time
}
var hostSampler=&Sampler{}
func Collect() Snapshot {return hostSampler.CollectWithPaths(nil)}
func CollectWithPaths(paths map[string]string) Snapshot {return hostSampler.CollectWithPaths(paths)}
func (s *Sampler) Collect() Snapshot {return s.CollectWithPaths(nil)}
func (s *Sampler) CollectWithPaths(paths map[string]string) Snapshot {
 now:=time.Now()
 name,_:=os.Hostname()
 out:=Snapshot{Timestamp:now.UnixMilli(),Hostname:name,Goroutines:runtime.NumGoroutine(),GPUs:[]GPU{}}
 if v,err:=readMemory();err==nil {out.MemoryTotalBytes=v.total;out.MemoryAvailableBytes=v.available}
 if v,err:=readDisk();err==nil {out.DiskTotalBytes=v.total;out.DiskFreeBytes=v.free;out.DiskPath=v.path}
 if rss,err:=readProcessRSS();err==nil {out.ProcessRSSBytes=rss}
 idle,total,cpuErr:=readCPU()
 rx,tx,netErr:=readNetwork()
 s.mu.Lock()
 if cpuErr==nil && total>s.cpuTotal && s.cpuTotal>0 {
  elapsed:=total-s.cpuTotal
  idleDelta:=uint64(0);if idle>=s.cpuIdle {idleDelta=idle-s.cpuIdle}
  if idleDelta>elapsed {idleDelta=elapsed}
  percent:=100*float64(elapsed-idleDelta)/float64(elapsed)
  out.CPUPercent=&percent
 }
 if cpuErr==nil {s.cpuIdle=idle;s.cpuTotal=total}
 if netErr==nil&&!s.previous.IsZero()&&now.After(s.previous){
  seconds:=now.Sub(s.previous).Seconds()
  if seconds>0&&rx>=s.rx&&tx>=s.tx{
   receive:=float64(rx-s.rx)/seconds;transmit:=float64(tx-s.tx)/seconds
   out.NetworkRXBytesPerSec=&receive;out.NetworkTXBytesPerSec=&transmit
  }
 }
 if netErr==nil {s.rx=rx;s.tx=tx;s.previous=now}
 s.mu.Unlock()
 out.GPUs=readGPUs()
 out.StorageVolumes=collectStorageVolumes(paths)
 return out
}
type memStats struct{total,available uint64}
type diskStats struct{path string;total,free uint64}

func readGPUs() []GPU {
 executable,err:=exec.LookPath("nvidia-smi")
 if err!=nil&&runtime.GOOS=="windows" {
  candidate:=filepath.Join(os.Getenv("ProgramFiles"),"NVIDIA Corporation","NVSMI","nvidia-smi.exe")
  if st,e:=os.Stat(candidate);e==nil&&!st.IsDir(){executable=candidate;err=nil}
 }
 if err!=nil{return []GPU{}}
 ctx,cancel:=context.WithTimeout(context.Background(),2*time.Second);defer cancel()
 output,err:=exec.CommandContext(ctx,executable,"--query-gpu=name,utilization.gpu,memory.used,memory.total,temperature.gpu","--format=csv,noheader,nounits").Output()
 if err!=nil{return []GPU{}}
 var rows []GPU
 for _,line:=range strings.Split(strings.TrimSpace(string(output)),"\n"){
  fields:=strings.Split(line,",");if len(fields)<5 {continue}
  name:=strings.TrimSpace(fields[0]);if name=="" {continue}
  item:=GPU{Name:name}
  if v,e:=strconv.ParseFloat(strings.TrimSpace(fields[1]),64);e==nil&&v>=0&&v<=100 {item.UsagePercent=&v}
  if v,e:=strconv.ParseUint(strings.TrimSpace(fields[2]),10,64);e==nil{item.VRAMUsedBytes=v*1024*1024}
  if v,e:=strconv.ParseUint(strings.TrimSpace(fields[3]),10,64);e==nil{item.VRAMTotalBytes=v*1024*1024}
  if v,e:=strconv.ParseFloat(strings.TrimSpace(fields[4]),64);e==nil {item.TemperatureC=&v}
  rows=append(rows,item)
 }
 return rows
}


func collectStorageVolumes(paths map[string]string) []StorageVolume {
 // Coalesce by resolved volume, not by configured folder. A model pool and
 // project root on D:\ must be one physical-space tile.
 volumes:=map[string]*StorageVolume{}
 for _,role:=range []string{"Models","Projects"} {
  p:=strings.TrimSpace(paths[role]);if p==""{continue}
  stat,e:=readDiskAt(p);if e!=nil||stat.total==0{continue}
  key:=strings.ToLower(filepath.Clean(stat.path))
  if v,ok:=volumes[key];ok{v.Roles=append(v.Roles,role)}else{
   volumes[key]=&StorageVolume{Path:stat.path,Roles:[]string{role},TotalBytes:stat.total,FreeBytes:stat.free}
  }
 }
 var out []StorageVolume
 for _,v:=range volumes {out=append(out,*v)}
 // Stable ordering keeps tiles from moving around between five-second samples.
 sort.Slice(out,func(i,j int)bool{return out[i].Path<out[j].Path})
 return out
}
