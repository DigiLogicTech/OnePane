package tool

import "strings"

// scopedFileAction maps only the file operations represented by the
// feature-policy vocabulary. Other tools retain all existing authority,
// workspace sandbox and operation-coordinator checks.
func scopedFileAction(capabilityID, toolID string) string {
	capabilityID = strings.ToLower(strings.TrimSpace(capabilityID))
	toolID = strings.ToLower(strings.TrimSpace(toolID))
	switch {
	case capabilityID == "filesystem.read", capabilityID == "file.read",
		toolID == "project.app.files.inspect", toolID == "file.read":
		return "files.read"
	case capabilityID == "filesystem.write", capabilityID == "file.write",
		toolID == "project.app.files.edit",toolID == "project.app.files.publish",toolID == "file.write":
		return "files.write"
	default:
		return ""
	}
}
