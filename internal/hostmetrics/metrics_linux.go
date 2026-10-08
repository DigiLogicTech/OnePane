//go:build linux
package hostmetrics

import (
 "os"
 "strings"
 "strconv"
 "fmt"
 "syscall"
)

func readCPU()(idle,total uint64,err error){
 raw,e:=os.ReadFile("/proc/stat");if e!=nil{return 0,0,e}
 line:=strings.SplitN(string(raw),"\n",2)[0]
 fields:=strings.Fields(line)
 if len(fields)<5||fields[0]!="cpu"{return 0,0,fmt.Errorf("CPU counters unavailable")}
 for i:=1;i<len(fields);i++{v,e:=strconv.ParseUint(fields[i],10,64);if e!=nil{return 0,0,e};total+=v;if i==4||i==5{idle+=v}}
 return idle,total,nil
}
func readMemory()(memStats,error){
 raw,e:=os.ReadFile("/proc/meminfo");if e!=nil{return memStats{},e}
 var m memStats
 for _,line:=range strings.Split(string(raw),"\n"){
  f:=strings.Fields(line);if len(f)<2{continue}
  v,e:=strconv.ParseUint(f[1],10,64);if e!=nil{continue}
  switch f[0]{case "MemTotal:":m.total=v*1024;case "MemAvailable:":m.available=v*1024}
 }
 if m.total==0{return m,fmt.Errorf("memory counters unavailable")}
 return m,nil
}
func readDisk()(diskStats,error){
 var s syscall.Statfs_t
 if e:=syscall.Statfs("/",&s);e!=nil{return diskStats{},e}
 return diskStats{path:"/",total:s.Blocks*uint64(s.Bsize),free:s.Bavail*uint64(s.Bsize)},nil
}
func readNetwork()(rx,tx uint64,err error){
 raw,e:=os.ReadFile("/proc/net/dev");if e!=nil{return 0,0,e}
 for _,line:=range strings.Split(string(raw),"\n"){
  split:=strings.SplitN(line,":",2);if len(split)!=2||strings.TrimSpace(split[0])=="lo"{continue}
  f:=strings.Fields(split[1]);if len(f)<9{continue}
  a,e1:=strconv.ParseUint(f[0],10,64);b,e2:=strconv.ParseUint(f[8],10,64)
  if e1==nil&&e2==nil{rx+=a;tx+=b}
 }
 return rx,tx,nil
}
func readProcessRSS()(uint64,error){
 raw,e:=os.ReadFile("/proc/self/status");if e!=nil{return 0,e}
 for _,line:=range strings.Split(string(raw),"\n"){if strings.HasPrefix(line,"VmRSS:"){
  f:=strings.Fields(line);if len(f)>1 {v,e:=strconv.ParseUint(f[1],10,64);return v*1024,e}
 }}
 return 0,fmt.Errorf("process RSS unavailable")
}
