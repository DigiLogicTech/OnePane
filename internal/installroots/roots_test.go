package installroots

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func ntfs(drive string, free uint64) VolumeFact {
	return VolumeFact{Drive: drive, FileSystem: "NTFS", FreeBytes: free, Present: true}
}

func TestFreshInstallRequiresExplicitRootsNTFSAndSpace(t *testing.T) {
	got := Decide(Request{
		Data: `C:\ProgramData\OnePane`, Model: `D:\OnePane\models`, Project: `D:\OnePane\Projects`,
		ProgramVolume: `C:`,
		Volumes:       []VolumeFact{ntfs("C:", 4<<30), ntfs("D:", 20<<30)},
	})
	if !got.OK || got.Upgrade {
		t.Fatalf("fresh layout rejected: %+v", got)
	}
	if !strings.EqualFold(got.Data, `C:\ProgramData\OnePane`) || !strings.EqualFold(got.Model, `D:\OnePane\models`) {
		t.Fatalf("roots changed: %+v", got)
	}
}

func TestFreshInstallDoesNotInventModelRootOnC(t *testing.T) {
	got := Decide(Request{Data: `C:\ProgramData\OnePane`, Volumes: []VolumeFact{ntfs("C:", 40<<30)}})
	if got.OK || len(got.Errors) == 0 {
		t.Fatal("empty model and project roots were accepted")
	}
}

func TestFreshInstallRejectsFATAndTightDisk(t *testing.T) {
	got := Decide(Request{
		Data: `C:\ProgramData\OnePane`, Model: `D:\OnePane\models`, Project: `E:\OnePane\Projects`,
		Volumes: []VolumeFact{
			{Drive: "C:", FileSystem: "NTFS", FreeBytes: 40 << 30, Present: true},
			{Drive: "D:", FileSystem: "FAT32", FreeBytes: 40 << 30, Present: true},
			{Drive: "E:", FileSystem: "NTFS", FreeBytes: 1 << 20, Present: true},
		},
	})
	if got.OK {
		t.Fatal("unsafe volumes accepted")
	}
	text := strings.Join(got.Errors, "\n")
	if !strings.Contains(text, "FAT32") || !strings.Contains(text, "E:") {
		t.Fatalf("errors = %s", text)
	}
}

func TestUpgradeKeepsCombinedDRootAndDoesNotMoveIt(t *testing.T) {
	installed := `D:\OnePaneData`
	got := Decide(Request{
		Data: installed, Model: installed + `\models`, Project: installed + `\Projects`,
		InstalledData: installed, InstalledModel: installed + `\models`, InstalledProject: installed + `\Projects`,
		Volumes: []VolumeFact{ntfs("D:", 2<<30)},
	})
	if !got.OK || !got.Upgrade {
		t.Fatalf("existing QA7 layout was not kept: %+v", got)
	}
	moved := Decide(Request{
		Data: `C:\ProgramData\OnePane`, Model: `D:\OnePane\models`, Project: `D:\OnePane\Projects`,
		InstalledData: installed, InstalledModel: installed + `\models`, InstalledProject: installed + `\Projects`,
		Volumes: []VolumeFact{ntfs("C:", 40<<30), ntfs("D:", 40<<30)},
	})
	if moved.OK {
		t.Fatal("upgrade silently accepted a different data root")
	}
}

func TestLegacyUpgradeDerivesModelAndProjectUnderRecordedDataRoot(t *testing.T) {
	got := Decide(Request{
		Data: `D:\OnePaneData`, Model: `D:\OnePaneData\models`, Project: `D:\OnePaneData\Projects`,
		InstalledData: `D:\OnePaneData`,
		Volumes:       []VolumeFact{ntfs("D:", UpgradeFreeBytes)},
	})
	if !got.OK {
		t.Fatalf("legacy derived roots rejected: %+v", got.Errors)
	}
	changed := Decide(Request{
		Data: `D:\OnePaneData`, Model: `E:\models`, Project: `D:\OnePaneData\Projects`,
		InstalledData: `D:\OnePaneData`,
		Volumes:       []VolumeFact{ntfs("D:", 40<<30), ntfs("E:", 40<<30)},
	})
	if changed.OK {
		t.Fatal("legacy upgrade accepted a new model drive")
	}
}

func TestSelectedPartialRootIsAChoiceAndUnselectedPartialIsPreserved(t *testing.T) {
	partial := PartialFinding{Root: `C:\ProgramData\OnePane`, Artifact: `data\keys\node.identity`, Kind: PartialEmpty, Partial: true}
	selected := Decide(Request{
		Data: partial.Root, Model: `D:\OnePane\models`, Project: `D:\OnePane\Projects`,
		Volumes:         []VolumeFact{ntfs("C:", 40<<30), ntfs("D:", 40<<30)},
		SelectedPartial: partial,
	})
	if selected.OK || !strings.Contains(strings.Join(selected.Errors, " "), "will not regenerate") {
		t.Fatalf("partial selection was not refused: %+v", selected)
	}
	kept := Decide(Request{
		Data: `D:\OnePaneData`, Model: `D:\OnePaneData\models`, Project: `D:\OnePaneData\Projects`,
		InstalledData: `D:\OnePaneData`, InstalledModel: `D:\OnePaneData\models`, InstalledProject: `D:\OnePaneData\Projects`,
		Volumes:           []VolumeFact{ntfs("D:", 2<<30)},
		UnselectedPartial: []PartialFinding{partial},
	})
	if !kept.OK {
		t.Fatalf("healthy upgrade blocked by an unselected partial root: %+v", kept.Errors)
	}
	if !strings.Contains(strings.Join(kept.Notices, " "), `C:\ProgramData\OnePane`) {
		t.Fatalf("unselected partial root was not reported: %+v", kept.Notices)
	}
}

func TestInspectPartialIdentityNeverNeedsTheKeyBytes(t *testing.T) {
	root := t.TempDir()
	data := filepath.Join(root, "data")
	keys := filepath.Join(data, "keys")
	if err := os.MkdirAll(keys, 0o700); err != nil {
		t.Fatal(err)
	}
	identity := filepath.Join(keys, "node.identity")
	if err := os.WriteFile(identity, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	got := InspectInstallerDataRoot(root)
	if !got.Partial || got.Kind != PartialEmpty {
		t.Fatalf("empty identity not detected: %+v", got)
	}
	info, err := os.Stat(identity)
	if err != nil || info.Size() != 0 {
		t.Fatalf("inspection changed the empty identity: %v %+v", err, info)
	}
	if err := os.WriteFile(identity, make([]byte, 32), 0o600); err != nil {
		t.Fatal(err)
	}
	if InspectInstallerDataRoot(root).Partial {
		t.Fatal("32-byte identity treated as partial")
	}
	if err := os.WriteFile(identity, []byte{1, 2, 3}, 0o600); err != nil {
		t.Fatal(err)
	}
	trunc := InspectInstallerDataRoot(root)
	if !trunc.Partial || trunc.Kind != PartialTruncated {
		t.Fatalf("truncated identity: %+v", trunc)
	}
	if err := os.Remove(identity); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(identity, 0o700); err != nil {
		t.Fatal(err)
	}
	dir := InspectIdentityDir(data)
	if !dir.Partial || dir.Kind != PartialDirectory {
		t.Fatalf("directory identity: %+v", dir)
	}
	if err := os.Remove(identity); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(data, "elsewhere"), identity); err != nil {
		return
	}
	reparse := InspectIdentityDir(data)
	if !reparse.Partial || reparse.Kind != PartialReparse {
		t.Fatalf("symlink identity: %+v", reparse)
	}
}

func TestMissingIdentityIsNotPartial(t *testing.T) {
	if InspectInstallerDataRoot(t.TempDir()).Partial {
		t.Fatal("absent root treated as partial")
	}
}
