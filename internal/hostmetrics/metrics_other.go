//go:build !windows && !linux
package hostmetrics
import "errors"
var unavailable=errors.New("host metric unsupported")
func readCPU()(idle,total uint64,err error){return 0,0,unavailable}
func readMemory()(memStats,error){return memStats{},unavailable}
func readDisk()(diskStats,error){return diskStats{},unavailable}
func readNetwork()(rx,tx uint64,err error){return 0,0,unavailable}
func readProcessRSS()(uint64,error){return 0,unavailable}
