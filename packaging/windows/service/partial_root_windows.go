//go:build windows

package main

import (
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/DigiLogicTech/OnePane/internal/installroots"
)

func rejectPartialDataRoot(root string) error {
	finding := installroots.InspectInstallerDataRoot(root)
	if !finding.Partial {
		return nil
	}
	return fmt.Errorf("%s", finding.Error())
}

func noteUnselectedPartialRoot(w io.Writer, selected, programData string) {
	if w == nil {
		return
	}
	if strings.TrimSpace(programData) == "" {
		programData = `C:\ProgramData`
	}
	other := filepath.Join(programData, "OnePane")
	if strings.EqualFold(filepath.Clean(other), filepath.Clean(selected)) {
		return
	}
	finding := installroots.InspectInstallerDataRoot(other)
	if !finding.Partial {
		return
	}
	fmt.Fprintf(w, "%s partial data root left unchanged: %s\n", time.Now().Format(time.RFC3339Nano), finding.Error())
}
