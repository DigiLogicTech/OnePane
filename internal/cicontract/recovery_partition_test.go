package cicontract

import (
	"go/ast"
	"go/build"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

func TestRecoveryRacePartitionCoversEveryIntegrationTestExactlyOnce(t *testing.T) {
	w := readSourceWorkflow(t)
	job := w.Jobs["recovery-schema-race"]
	if job.RunsOn != "ubuntu-latest" || job.Timeout != 15 || job.If != "" || job.ContinueOnError {
		t.Fatal("recovery race family lost its runner, deadline or mandatory execution")
	}
	patterns := []string{
		"^TestRecoveryIndirect",
		"^Test(Recovery(Schema|TaskAttempt)|KnownTaskProvenance|ModelRequest)",
	}
	want := make([]string, len(patterns))
	for i, pattern := range patterns {
		want[i] = "go test -race -tags integration -count=1 -timeout 3m -run '" + pattern + "' ./internal/recoveryinventory"
	}
	var actual []string
	for _, step := range job.Steps {
		if step.If != "" || step.ContinueOnError {
			t.Fatal("recovery race step allowed to skip or ignore failure")
		}
		for _, line := range strings.Split(step.Run, "\n") {
			if line = strings.TrimSpace(line); line != "" {
				actual = append(actual, line)
			}
		}
	}
	if !reflect.DeepEqual(actual, want) {
		t.Fatalf("recovery commands/deadlines changed: %q", actual)
	}
	for _, step := range w.Jobs["worker-recovery-race"].Steps {
		if strings.Contains(step.Run, "./internal/recoveryinventory") {
			t.Fatal("recovery package duplicated in the serial Worker family")
		}
	}
	// Inspect actual Linux+integration test declarations, not a fixture that
	// could silently omit a new test. This executes on Windows too, without
	// treating a native non-race pass as Linux race execution evidence.
	dir := filepath.Join("..", "recoveryinventory")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	buildContext := build.Default
	buildContext.GOOS, buildContext.GOARCH = "linux", "amd64"
	buildContext.CgoEnabled = true
	buildContext.BuildTags = []string{"integration", "race"}
	counts := make([]int, len(patterns))
	total := 0
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		match, err := buildContext.MatchFile(dir, entry.Name())
		if err != nil {
			t.Fatal(err)
		}
		if !match {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), filepath.Join(dir, entry.Name()), nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, declaration := range file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Recv != nil || !strings.HasPrefix(function.Name.Name, "Test") || function.Type.Params == nil || len(function.Type.Params.List) != 1 {
				continue
			}
			covered := 0
			for i, pattern := range patterns {
				if regexp.MustCompile(pattern).MatchString(function.Name.Name) {
					counts[i]++
					covered++
				}
			}
			if covered != 1 {
				t.Fatalf("Linux integration test %s assigned to %d recovery partitions", function.Name.Name, covered)
			}
			total++
		}
	}
	if total < 12 || counts[0] == 0 || counts[1] == 0 {
		t.Fatalf("recovery test inventory disappeared: %d %v", total, counts)
	}
}
