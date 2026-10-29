//go:build windows

// OnePane.Preflight is a read-only MSI storage check. It does not create,
// move, delete, or rewrite data, model, or project roots.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unsafe"

	"github.com/DigiLogicTech/OnePane/internal/installroots"
	"golang.org/x/sys/windows"
)

func main() {
	data := flag.String("data", "", "application data root")
	model := flag.String("model", "", "model root")
	project := flag.String("project", "", "project root")
	installedData := flag.String("installed-data", "", "existing data root")
	installedModel := flag.String("installed-model", "", "existing model root")
	installedProject := flag.String("installed-project", "", "existing project root")
	programData := flag.String("program-data", "", "ProgramData directory")
	programVolume := flag.String("program-volume", `C:\`, "program files directory")
	uiLevel := flag.Int("ui-level", 2, "Windows Installer UILevel")
	flag.Parse()

	req := installroots.Request{
		Data: *data, Model: *model, Project: *project,
		InstalledData: *installedData, InstalledModel: *installedModel, InstalledProject: *installedProject,
		ProgramVolume: *programVolume,
	}
	req.SelectedPartial = installroots.InspectInstallerDataRoot(*data)
	other := defaultPartialRoot(*programData)
	if other != "" && !strings.EqualFold(filepath.Clean(other), filepath.Clean(strings.TrimSpace(*data))) {
		if finding := installroots.InspectInstallerDataRoot(other); finding.Partial {
			req.UnselectedPartial = []installroots.PartialFinding{finding}
		}
	}
	seen := map[string]bool{}
	for _, path := range []string{*data, *model, *project, *programVolume} {
		drive, ok := driveOf(path)
		if !ok || seen[drive] {
			continue
		}
		seen[drive] = true
		fact, err := installroots.ProbeVolume(path)
		if err != nil {
			fact = installroots.VolumeFact{Drive: drive, Present: false}
		}
		req.Volumes = append(req.Volumes, fact)
	}
	result := installroots.Decide(req)
	for _, notice := range result.Notices {
		fmt.Println("OnePane storage check:", notice)
	}
	if result.OK {
		fmt.Printf("OnePane storage check: data %s; models %s; projects %s\n", result.Data, result.Model, result.Project)
		return
	}
	var lines []string
	for _, item := range result.Errors {
		line := "OnePane storage check: " + item
		fmt.Println(line)
		lines = append(lines, item)
	}
	if *uiLevel >= 5 {
		notifyOperator(strings.Join(lines, "\n\n"))
	}
	os.Exit(1)
}

func defaultPartialRoot(programData string) string {
	programData = strings.TrimSpace(programData)
	if programData == "" {
		programData = os.Getenv("ProgramData")
	}
	if programData == "" {
		programData = `C:\ProgramData`
	}
	return filepath.Join(programData, "OnePane")
}

func driveOf(path string) (string, bool) {
	path = strings.TrimSpace(path)
	if len(path) < 2 || path[1] != ':' {
		return "", false
	}
	return strings.ToUpper(path[:1]) + ":", true
}

func notifyOperator(message string) {
	if strings.TrimSpace(message) == "" {
		return
	}
	title, _ := windows.UTF16FromString("OnePane setup")
	body, err := windows.UTF16FromString(message)
	if err != nil || len(title) == 0 || len(body) == 0 {
		return
	}
	kernel := windows.NewLazySystemDLL("kernel32.dll")
	session, _, _ := kernel.NewProc("WTSGetActiveConsoleSessionId").Call()
	var response uint32
	// WTS_CURRENT_SERVER_HANDLE is 0. Wait is bounded so a full UI install
	// cannot hang, and silent installs never reach this call.
	_, _, _ = windows.NewLazySystemDLL("wtsapi32.dll").NewProc("WTSSendMessageW").Call(
		0, session,
		uintptr(unsafe.Pointer(&title[0])), uintptr(len(title)*2),
		uintptr(unsafe.Pointer(&body[0])), uintptr(len(body)*2),
		0x30, 30, uintptr(unsafe.Pointer(&response)), 1,
	)
}
