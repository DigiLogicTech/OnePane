package msi

import (
	"os"
	"strings"
	"testing"
)

func TestMSIAuthoringHasNoStaleVersionFallback(t *testing.T) {
	text, err := os.ReadFile("OnePane.wxs")
	if err != nil {
		t.Fatal(err)
	}
	src := string(text)
	if strings.Contains(src, `ProductVersion = "3.3.10"`) || strings.Contains(src, `ProductVersion="3.3.10"`) {
		t.Fatal("MSI authoring still falls back to 3.3.10")
	}
	if !strings.Contains(src, "Missing ProductVersion") || !strings.Contains(src, "OnePane.Preflight.exe") {
		t.Fatal("MSI authoring does not require an explicit version and storage preflight")
	}
	if !strings.Contains(src, "NOT ONEPANE_MODEL_ROOT AND ONEPANE_INSTALLED_DATA_ROOT") {
		t.Fatal("fresh installs can still invent a model root")
	}
	if !strings.Contains(src, "ONEPANE_INSTALLED_DATA_ROOT") || !strings.Contains(src, "WixQuietExec") {
		t.Fatal("upgrade root guard or preflight custom action is missing")
	}
}
