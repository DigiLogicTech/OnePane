package serviceinstall

import (
	"strings"
	"testing"

	"github.com/DigiLogicTech/OnePane/internal/config"
)

func TestSystemdStoragePlanPreservesSelectedRoots(t *testing.T) {
	cfg := config.Default()
	cfg.Storage.DataDir = "/srv/onepane data"
	cfg.Storage.ProjectRoot = "/mnt/projects/onepane"
	cfg.LocalAI.ModelPoolPath = "/mnt/models/100% models"
	p, err := PlanStorage(cfg, "/var/lib/onepane")
	if err != nil {
		t.Fatal(err)
	}
	p.ConfigSHA256 = strings.Repeat("a", 64)
	got := p.DropIn()
	for _, want := range []string{`ReadWritePaths=`, `"/srv/onepane data"`, `"/mnt/projects/onepane"`, `"/mnt/models/100%% models"`, `"/var/lib/onepane"`, `"ONEPANE_STARTUP_EVIDENCE_ROOT=/srv/onepane data"`} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %s in %s", want, got)
		}
	}
	for _, forbidden := range []string{"ProtectHome=", "ProtectSystem=", "User=", "ExecStart=", "ReadWritePaths=/", "ReadWritePaths=\"/\""} {
		if strings.Contains(got, forbidden) {
			t.Fatalf("broadened service boundary: %s", got)
		}
	}
	if cfg.Storage.DataDir != "/srv/onepane data" || cfg.LocalAI.ModelPoolPath != "/mnt/models/100% models" {
		t.Fatal("planning changed configuration")
	}
}

func TestSystemdStoragePlanRejectsMaskedAndUnsafeLocations(t *testing.T) {
	for _, value := range []string{"", "relative", "/", "/srv", "/var/lib", "/home/test/onepane", "/root/onepane", "/tmp/onepane", "/run/onepane/data", "/usr/share/onepane", "/etc/onepane/data", "/srv/../other", "/srv/a\nb", "/srv/onepane\\data", " /srv/data"} {
		t.Run(value, func(t *testing.T) {
			cfg := config.Default()
			cfg.Storage.DataDir = value
			if _, err := PlanStorage(cfg, "/var/lib/onepane"); err == nil {
				t.Fatal("unsafe storage path accepted")
			}
		})
	}
	cfg := config.Default()
	cfg.Storage.DataDir = "/srv/onepane"
	p, err := PlanStorage(cfg, "/var/lib/onepane")
	if err != nil {
		t.Fatal(err)
	}
	if p.Models != "/srv/onepane/models/managed" || p.Projects != "/srv/onepane/projects" {
		t.Fatalf("runtime defaults differ: %+v", p)
	}
	if got := strings.Count(p.DropIn(), `"/srv/onepane"`); got != 2 {
		t.Fatalf("nested roots should be covered only by selected parent: %s", p.DropIn())
	}
}
