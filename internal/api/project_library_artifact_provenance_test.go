package api

import (
	"strings"
	"testing"

	"github.com/DigiLogicTech/OnePane/internal/artifact"
	"github.com/DigiLogicTech/OnePane/internal/projectworkspace"
)

func TestLibraryArtifactProvenanceRequiresActiveExactProjectTenantAndHash(t *testing.T) {
	project := "project-one"
	version := projectworkspace.LibraryVersion{
		AssetID: "asset-one", Version: 1, ContentHash: "sha256:" + strings.Repeat("ab", 32), SizeBytes: 42,
	}
	live := artifact.Artifact{
		ID: "art-one", ProjectID: &project, WorkspaceID: "tenant",
		ContentHash: version.ContentHash, SizeBytes: 42, Status: artifact.StatusActive,
	}
	if !libraryArtifactMatchesProject(version, live, project, "tenant") {
		t.Fatal("known valid Project Library provenance rejected")
	}
	cases := []struct {
		name   string
		mutate func(*projectworkspace.LibraryVersion, *artifact.Artifact)
	}{
		{"quarantined", func(_ *projectworkspace.LibraryVersion, a *artifact.Artifact) { a.Status = artifact.StatusQuarantined }},
		{"archived", func(_ *projectworkspace.LibraryVersion, a *artifact.Artifact) { a.Status = artifact.StatusArchived }},
		{"corrupted", func(_ *projectworkspace.LibraryVersion, a *artifact.Artifact) { a.Status = artifact.StatusCorrupted }},
		{"missing_project", func(_ *projectworkspace.LibraryVersion, a *artifact.Artifact) { a.ProjectID = nil }},
		{"foreign_project", func(_ *projectworkspace.LibraryVersion, a *artifact.Artifact) { v := "other"; a.ProjectID = &v }},
		{"foreign_tenant", func(_ *projectworkspace.LibraryVersion, a *artifact.Artifact) { a.WorkspaceID = "other-tenant" }},
		{"wrong_content_hash", func(_ *projectworkspace.LibraryVersion, a *artifact.Artifact) { a.ContentHash = "sha256:tampered" }},
		{"wrong_size", func(_ *projectworkspace.LibraryVersion, a *artifact.Artifact) { a.SizeBytes = 999 }},
		{"empty_recorded_hash", func(v *projectworkspace.LibraryVersion, _ *artifact.Artifact) { v.ContentHash = "" }},
		{"negative_version_size", func(v *projectworkspace.LibraryVersion, _ *artifact.Artifact) { v.SizeBytes = -1 }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v, a := version, live
			tc.mutate(&v, &a)
			if libraryArtifactMatchesProject(v, a, project, "tenant") {
				t.Fatal("unsafe managed artifact provenance accepted")
			}
		})
	}
	if libraryArtifactMatchesProject(version, live, "", "tenant") ||
		libraryArtifactMatchesProject(version, live, project, "") {
		t.Fatal("empty scope must not authorise Library download")
	}
}

func TestLibraryArtifactHashFormsRequireExactSHA256(t *testing.T) {
	project := "project"
	digest := strings.Repeat("ab", 32)
	raw := artifact.Artifact{ProjectID: &project, WorkspaceID: "tenant", ContentHash: "sha256:" + digest, SizeBytes: 3, Status: artifact.StatusActive}
	for _, hash := range []string{digest, "sha256:" + digest} {
		version := projectworkspace.LibraryVersion{ContentHash: hash, SizeBytes: 3}
		if !libraryArtifactMatchesProject(version, raw, project, "tenant") {
			t.Fatalf("supported persisted form rejected: %q", hash)
		}
	}
	for _, hash := range []string{"", "abc", "sha256:abc", strings.Repeat("a", 63), strings.Repeat("a", 65), "sha256:" + strings.Repeat("a", 63), "SHA256:" + digest, "sha512:" + digest, "sha256:sha256:" + digest, strings.ToUpper(digest), "sha256:" + strings.ToUpper(digest), " " + digest, digest + "\n", strings.Repeat("g", 64), "sha256:" + strings.Repeat("ac", 32)} {
		version := projectworkspace.LibraryVersion{ContentHash: hash, SizeBytes: 3}
		if libraryArtifactMatchesProject(version, raw, project, "tenant") {
			t.Fatalf("invalid/mismatched digest accepted: %q", hash)
		}
	}
	version := projectworkspace.LibraryVersion{ContentHash: digest, SizeBytes: 3}
	for _, hash := range []string{digest, "SHA256:" + digest, "sha512:" + digest, "sha256:" + strings.ToUpper(digest), "sha256:" + strings.Repeat("ac", 32)} {
		changed := raw
		changed.ContentHash = hash
		if libraryArtifactMatchesProject(version, changed, project, "tenant") {
			t.Fatalf("noncanonical/mismatched managed hash accepted: %q", hash)
		}
	}
}
