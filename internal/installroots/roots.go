// Package installroots decides Windows install and upgrade storage roots.
// It never creates, moves, deletes, or rewrites operator data. Callers probe
// volumes and identity files, then apply Decide.
package installroots

import (
	"fmt"
	"strings"
)

const (
	// Fresh-install minimums. They are sums per volume: data on C: and models
	// plus projects on D: are not each compared with the same free-space figure.
	FreshDataFreeBytes    uint64 = 1 << 30
	FreshModelFreeBytes   uint64 = 8 << 30
	FreshProjectFreeBytes uint64 = 1 << 30
	// Program Files payload margin, added to the system volume on a fresh install.
	FreshPayloadFreeBytes uint64 = 256 << 20
	// Upgrades of an existing layout only need room to replace program files
	// and write a log. They do not demand a fresh model download.
	UpgradeFreeBytes uint64 = 200 << 20
)

// VolumeFact is a read-only probe of one local drive.
type VolumeFact struct {
	Drive      string
	FileSystem string
	FreeBytes  uint64
	Present    bool
}

// PartialKind classifies an identity artifact without reading its bytes.
type PartialKind string

const (
	PartialEmpty      PartialKind = "empty"
	PartialTruncated  PartialKind = "truncated"
	PartialReparse    PartialKind = "reparse"
	PartialDirectory  PartialKind = "directory"
	PartialUnreadable PartialKind = "unreadable"
)

// PartialFinding is an incomplete data root. Partial roots are never repaired.
type PartialFinding struct {
	Root     string
	Artifact string
	Size     int64
	Kind     PartialKind
	Partial  bool
}

func (f PartialFinding) Error() string {
	if !f.Partial {
		return ""
	}
	detail := fmt.Sprintf("%d bytes", f.Size)
	switch f.Kind {
	case PartialEmpty:
		detail = "empty (0 bytes)"
	case PartialReparse:
		detail = "a reparse point"
	case PartialDirectory:
		detail = "a directory"
	case PartialUnreadable:
		detail = "unreadable"
	case PartialTruncated:
		detail = fmt.Sprintf("truncated (%d bytes)", f.Size)
	}
	return fmt.Sprintf("%s contains an incomplete OnePane identity (%s is %s). OnePane will not regenerate, repair, or delete it. Choose a different application data directory, or cancel and leave this folder unchanged.", f.Root, f.Artifact, detail)
}

// Request is the installer decision input. Paths may use Windows separators
// even when the decision runs on another OS.
type Request struct {
	Data, Model, Project                            string
	InstalledData, InstalledModel, InstalledProject string
	ProgramVolume                                   string
	Volumes                                         []VolumeFact
	SelectedPartial                                 PartialFinding
	UnselectedPartial                               []PartialFinding
}

// Result is the only storage layout the installer may record.
type Result struct {
	OK      bool
	Upgrade bool
	Data    string
	Model   string
	Project string
	Errors  []string
	Notices []string
}

// Decide accepts a fresh layout or the exact installed layout. It never
// substitutes a different root for an existing one.
func Decide(in Request) Result {
	var out Result
	data := cleanWin(in.Data)
	model := cleanWin(in.Model)
	project := cleanWin(in.Project)
	installedData := cleanWin(in.InstalledData)
	installedModel := cleanWin(in.InstalledModel)
	installedProject := cleanWin(in.InstalledProject)
	out.Data, out.Model, out.Project = data, model, project
	out.Upgrade = installedData != "" || installedModel != "" || installedProject != ""

	for _, partial := range in.UnselectedPartial {
		if partial.Partial && !sameWin(partial.Root, data) {
			out.Notices = append(out.Notices, partial.Error()+" This folder is not the selected data directory and will be left untouched.")
		}
	}
	if in.SelectedPartial.Partial {
		out.Errors = append(out.Errors, in.SelectedPartial.Error())
	}

	if out.Upgrade {
		decideUpgrade(&out, in.Volumes, data, model, project, installedData, installedModel, installedProject)
	} else {
		decideFresh(&out, in.Volumes, data, model, project, cleanWin(in.ProgramVolume))
	}
	if len(out.Errors) > 0 {
		out.OK = false
		return out
	}
	out.OK = true
	return out
}

func decideFresh(out *Result, volumes []VolumeFact, data, model, project, programVolume string) {
	if data == "" || model == "" || project == "" {
		out.Errors = append(out.Errors, "Choose a local NTFS directory for application data, model weights, and projects. Model and project folders are not created under ProgramData automatically.")
		return
	}
	drives := map[string]uint64{}
	for _, item := range []struct {
		path  string
		bytes uint64
		name  string
	}{
		{data, FreshDataFreeBytes, "application data"},
		{model, FreshModelFreeBytes, "model weights"},
		{project, FreshProjectFreeBytes, "projects"},
	} {
		drive, err := requireDrive(item.path, item.name)
		if err != nil {
			out.Errors = append(out.Errors, err.Error())
			continue
		}
		drives[drive] += item.bytes
	}
	if programVolume == "" {
		programVolume = `C:`
	}
	if drive, ok := volumeLetter(programVolume); ok {
		drives[drive] += FreshPayloadFreeBytes
	}
	checkVolumes(out, volumes, drives, true)
}

func decideUpgrade(out *Result, volumes []VolumeFact, data, model, project, installedData, installedModel, installedProject string) {
	if installedData == "" || data == "" || !sameWin(data, installedData) {
		out.Errors = append(out.Errors, mismatch("application data", installedData, data))
	}
	wantModel := installedModel
	if wantModel == "" && installedData != "" {
		wantModel = cleanWin(installedData + `\models`)
	}
	wantProject := installedProject
	if wantProject == "" && installedData != "" {
		wantProject = cleanWin(installedData + `\Projects`)
	}
	if model == "" || !sameWin(model, wantModel) {
		out.Errors = append(out.Errors, mismatch("model weights", wantModel, model))
	}
	if project == "" || !sameWin(project, wantProject) {
		out.Errors = append(out.Errors, mismatch("projects", wantProject, project))
	}
	drives := map[string]uint64{}
	for _, path := range []string{data, model, project} {
		drive, ok := volumeLetter(path)
		if !ok {
			continue
		}
		drives[drive] = UpgradeFreeBytes
	}
	checkVolumes(out, volumes, drives, false)
	if len(out.Errors) == 0 {
		out.Notices = append(out.Notices, "Existing data, model, and project roots are unchanged.")
	}
}

func mismatch(name, installed, requested string) string {
	if installed == "" {
		installed = "(none recorded)"
	}
	if requested == "" {
		requested = "(empty)"
	}
	return fmt.Sprintf("Upgrades do not move or replace the %s root. Installed location: %s. Requested location: %s. Re-run setup without changing the existing roots.", name, installed, requested)
}

func requireDrive(path, name string) (string, error) {
	drive, ok := volumeLetter(path)
	if !ok {
		return "", fmt.Errorf("%s must be a local drive-letter directory, not %q", name, path)
	}
	cleaned := cleanWin(path)
	if strings.EqualFold(cleaned, drive) || strings.EqualFold(cleaned, drive+`\`) {
		return "", fmt.Errorf("%s must be a folder on %s, not the drive itself", name, drive)
	}
	return drive, nil
}

func checkVolumes(out *Result, volumes []VolumeFact, need map[string]uint64, fresh bool) {
	byDrive := map[string]VolumeFact{}
	for _, vol := range volumes {
		drive, ok := volumeLetter(vol.Drive)
		if !ok {
			drive = strings.ToUpper(strings.TrimSpace(vol.Drive))
		}
		byDrive[drive] = vol
	}
	for drive, bytes := range need {
		vol, ok := byDrive[drive]
		if !ok || !vol.Present {
			out.Errors = append(out.Errors, fmt.Sprintf("Drive %s is not available for OnePane storage.", drive))
			continue
		}
		fs := strings.ToUpper(strings.TrimSpace(vol.FileSystem))
		if fs != "NTFS" {
			out.Errors = append(out.Errors, fmt.Sprintf("Drive %s is %s. OnePane storage requires NTFS.", drive, displayFS(vol.FileSystem)))
			continue
		}
		if vol.FreeBytes < bytes {
			if fresh {
				out.Errors = append(out.Errors, fmt.Sprintf("Drive %s has %d bytes free; this install needs %d bytes.", drive, vol.FreeBytes, bytes))
			} else {
				out.Errors = append(out.Errors, fmt.Sprintf("Drive %s has %d bytes free; an upgrade needs %d bytes free and will not move existing roots.", drive, vol.FreeBytes, bytes))
			}
		}
	}
}

func displayFS(fs string) string {
	if strings.TrimSpace(fs) == "" {
		return "an unknown filesystem"
	}
	return fs
}

func volumeLetter(path string) (string, bool) {
	path = cleanWin(path)
	if len(path) < 2 || path[1] != ':' || !isLetter(path[0]) {
		return "", false
	}
	return strings.ToUpper(path[:1]) + ":", true
}

func isLetter(b byte) bool {
	return (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z')
}

func sameWin(a, b string) bool {
	return strings.EqualFold(cleanWin(a), cleanWin(b))
}

func cleanWin(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	path = strings.ReplaceAll(path, "/", `\`)
	for strings.Contains(path, `\\`) {
		// Keep a UNC prefix rejected by volumeLetter; collapse only after a drive.
		if len(path) >= 2 && path[1] == ':' {
			path = path[:2] + strings.ReplaceAll(path[2:], `\\`, `\`)
			break
		}
		break
	}
	for len(path) > 3 && strings.HasSuffix(path, `\`) {
		path = strings.TrimSuffix(path, `\`)
	}
	return path
}
