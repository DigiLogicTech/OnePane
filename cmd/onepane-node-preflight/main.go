package main

import (
 "context"
 "encoding/json"
 "flag"
 "fmt"
 "os"
 "runtime"

 "github.com/DigiLogicTech/OnePane/internal/nodepreflight"
)

func main(){
 var required string
 var strict bool
 flag.StringVar(&required,"require","","require one approved OCI image variable (default: report all)")
 flag.BoolVar(&strict,"strict",false,"exit nonzero unless all requested prerequisites pass")
 flag.Parse()
 report,err:=nodepreflight.Check(context.Background(),nodepreflight.LocalPodman{},
  os.Getenv,runtime.GOOS,required)
 if err!=nil{
  fmt.Fprintln(os.Stderr,"invalid Node preflight arguments")
  os.Exit(2)
 }
 if json.NewEncoder(os.Stdout).Encode(report)!=nil{os.Exit(2)}
 if strict&&!report.ReadyForPhysicalTest{os.Exit(1)}
}
