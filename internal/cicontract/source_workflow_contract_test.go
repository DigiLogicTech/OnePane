// Package cicontract checks the release source workflow itself. It does not
// establish installed Windows, physical inference or release acceptance.
package cicontract

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

// Reviewed command inventory from c68693a8 before Windows partitioning. Tests
// never regenerate it: every original selector, package and timeout is retained.
//
//go:embed native_windows_commands_v1.json
var nativeCommandsJSON []byte

type workflowStep struct {
	Name, Shell, Run string
	If               string `yaml:"if"`
	Env              map[string]string
	ContinueOnError  bool `yaml:"continue-on-error"`
}
type workflowJob struct {
	Name            string         `yaml:"name"`
	RunsOn          string         `yaml:"runs-on"`
	Timeout         int            `yaml:"timeout-minutes"`
	If              string         `yaml:"if"`
	Needs           []string       `yaml:"needs"`
	Steps           []workflowStep `yaml:"steps"`
	ContinueOnError bool           `yaml:"continue-on-error"`
}
type sourceWorkflow struct {
	Jobs map[string]workflowJob `yaml:"jobs"`
	On   struct {
		PullRequest struct {
			Paths []string `yaml:"paths"`
		} `yaml:"pull_request"`
	} `yaml:"on"`
}

func readSourceWorkflow(t *testing.T) sourceWorkflow {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", ".github", "workflows", "project-workspace-development-checks.yml"))
	if err != nil {
		t.Fatal(err)
	}
	var w sourceWorkflow
	if err = yaml.Unmarshal(data, &w); err != nil {
		t.Fatal(err)
	}
	return w
}

var windowsJobs = []string{"offline-msi-evidence", "windows-library-team", "windows-worker-recovery", "windows-team-dispatch"}
var gateFamilies = map[string]string{
	"development-source-contract": "SOURCE_RESULT", "worker-recovery-race": "WORKER_RESULT", "library-team-node-race": "TEAM_RESULT", "managed-inference-race": "MANAGED_RESULT", "full-regression": "FULL_RESULT",
	"library-document-race": "DOCUMENT_RESULT",
	"recovery-schema-race":  "RECOVERY_RESULT",
	"offline-msi-evidence":  "WINDOWS_CORE_RESULT", "windows-library-team": "WINDOWS_LIBRARY_RESULT", "windows-worker-recovery": "WINDOWS_WORKER_RESULT", "windows-team-dispatch": "WINDOWS_DISPATCH_RESULT",
}

const nativeGoGuard = "if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE }"

func TestNativeWindowsPartitionPreservesReviewedCommandsAndFailFast(t *testing.T) {
	w := readSourceWorkflow(t)
	var want []string
	if err := json.Unmarshal(nativeCommandsJSON, &want); err != nil {
		t.Fatal(err)
	}
	if len(want) != 65 {
		t.Fatalf("reviewed baseline changed: %d", len(want))
	}
	want = append(want, "go test -count=1 -timeout 3m ./internal/cicontract")
	want = append(want, "go test -tags integration -count=1 -timeout 3m -run '^TestLibraryDocument' ./internal/projectworkspace ./internal/api")
	var got []string
	msi := 0
	for _, id := range windowsJobs {
		job, ok := w.Jobs[id]
		if !ok || job.RunsOn != "windows-latest" || job.Timeout != 12 || job.If != "" || job.ContinueOnError {
			t.Fatalf("mandatory native family absent/weakened %s %+v", id, job)
		}
		commands := 0
		for _, step := range job.Steps {
			if step.Run != "" && (step.If != "" || step.ContinueOnError) {
				t.Fatalf("mandatory native step can skip/fail softly: %s / %s", id, step.Name)
			}
			lines := strings.Split(step.Run, "\n")
			for i, line := range lines {
				command := strings.TrimSpace(line)
				if command == `.\scripts\test_offline_msi_evidence.ps1` {
					msi++
					if step.Shell != "powershell" {
						t.Fatal("MSI evidence shell changed")
					}
				}
				if !strings.HasPrefix(command, "go test ") {
					continue
				}
				if step.Shell != "pwsh" || step.ContinueOnError || i+1 >= len(lines) || strings.TrimSpace(lines[i+1]) != nativeGoGuard {
					t.Fatalf("native exit may be overwritten: %s / %s", id, command)
				}
				got = append(got, command)
				commands++
			}
		}
		if commands == 0 {
			t.Fatalf("empty native family %s", id)
		}
	}
	if msi != 1 {
		t.Fatalf("MSI script missing/duplicated %d", msi)
	}
	sort.Strings(got)
	sort.Strings(want)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("native source command coverage changed:\nwant=%q\ngot=%q", want, got)
	}
	for _, path := range []string{"internal/cicontract/**", "internal/artifact/**", "internal/recoveryinventory/**", "internal/budget/**", "go.mod", "go.sum"} {
		found := false
		for _, actual := range w.On.PullRequest.Paths {
			found = found || path == actual
		}
		if !found {
			t.Fatalf("source change cannot trigger authoritative checks: %s", path)
		}
	}
}

func TestRequiredSourceGateIncludesEveryNativeAndLinuxFamily(t *testing.T) {
	w := readSourceWorkflow(t)
	gate := w.Jobs["development-contract"]
	if gate.If != "${{ always() }}" || gate.Name != "Validate honest Workspace runtime boundaries" || gate.ContinueOnError || len(gate.Needs) != len(gateFamilies) || len(gate.Steps) != 1 || gate.Steps[0].ContinueOnError {
		t.Fatalf("required aggregate can skip a mandatory family: %+v", gate)
	}
	seen := map[string]bool{}
	for _, family := range gate.Needs {
		key, ok := gateFamilies[family]
		if !ok || seen[family] {
			t.Fatalf("unexpected or duplicate family %s", family)
		}
		seen[family] = true
		job, exists := w.Jobs[family]
		if !exists || job.If != "" || job.ContinueOnError {
			t.Fatalf("mandatory family allowed to fail: %s", family)
		}
		if gate.Steps[0].Env[key] != "${{ needs."+family+".result }}" || !strings.Contains(gate.Steps[0].Run, `"$`+key+`"`) {
			t.Fatalf("aggregate ignores %s", family)
		}
	}
}

func TestRequiredSourceGateRejectsEachNonSuccessResult(t *testing.T) {
	// Execute the actual Bash aggregate on hosted Linux. Windows checks its
	// complete needs/env contract above and exercises actual PowerShell below.
	if runtime.GOOS == "windows" {
		t.Skip("Bash aggregate runs on Linux; native PowerShell has a separate execution check")
	}
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Fatal("required Linux gate shell unavailable")
	}
	step := readSourceWorkflow(t).Jobs["development-contract"].Steps[0]
	run := func(badKey, badResult string) error {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, bash, "-e", "-c", step.Run)
		cmd.Env = os.Environ()
		for _, key := range gateFamilies {
			value := "success"
			if key == badKey {
				value = badResult
			}
			cmd.Env = append(cmd.Env, key+"="+value)
		}
		return cmd.Run()
	}
	if err = run("", ""); err != nil {
		t.Fatal("all-success source gate denied", err)
	}
	for family, key := range gateFamilies {
		for _, result := range []string{"failure", "cancelled", "skipped", ""} {
			if err = run(key, result); err == nil {
				t.Fatalf("aggregate accepted %s=%q", family, result)
			}
		}
	}
}

func TestNativePowerShellStopsBeforeLaterSuccessfulGo(t *testing.T) {
	pwsh, err := exec.LookPath("pwsh")
	if err != nil {
		if runtime.GOOS == "windows" {
			t.Fatal(err)
		}
		t.Skip("PowerShell execution is mandatory in the native Windows source family")
	}
	goPath, err := exec.LookPath("go")
	if err != nil {
		t.Fatal(err)
	}
	for _, success := range []bool{false, true} {
		t.Run(fmt.Sprint(success), func(t *testing.T) {
			marker := filepath.Join(t.TempDir(), "later-command.txt")
			argument := "onepane-invalid-native-command"
			if success {
				argument = "version"
			}
			// Use the guard read from the real workflow, not a separate test-only
			// implementation. A failing native process must prevent the next Go
			// success from overwriting LASTEXITCODE and creating the marker.
			guard := ""
			for _, step := range readSourceWorkflow(t).Jobs["windows-worker-recovery"].Steps {
				lines := strings.Split(step.Run, "\n")
				for i, line := range lines {
					if strings.HasPrefix(strings.TrimSpace(line), "go test ") {
						guard = strings.TrimSpace(lines[i+1])
						break
					}
				}
			}
			if guard == "" {
				t.Fatal("no actual native guard found")
			}
			code := "$ErrorActionPreference='Stop'; $PSNativeCommandUseErrorActionPreference=$false; & $env:ONEPANE_CI_TEST_GO " + argument + "\n" + guard + "\n& $env:ONEPANE_CI_TEST_GO version\nSet-Content -LiteralPath $env:ONEPANE_CI_TEST_MARKER -Value 'later success'"
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, pwsh, "-NoProfile", "-NonInteractive", "-Command", code)
			cmd.Env = append(os.Environ(), "ONEPANE_CI_TEST_GO="+goPath, "ONEPANE_CI_TEST_MARKER="+marker)
			output, err := cmd.CombinedOutput()
			_, statErr := os.Stat(marker)
			if success {
				if err != nil || statErr != nil {
					t.Fatalf("valid native command denied: %v / %v / %s", err, statErr, output)
				}
			} else if err == nil || !os.IsNotExist(statErr) {
				t.Fatalf("early native failure hidden by later success: %v / %v / %s", err, statErr, output)
			}
		})
	}
}
