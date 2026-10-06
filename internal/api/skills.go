package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/DigiLogicTech/OnePane/internal/skillcatalog"
)

type skillCatalogService interface {
	Packages(context.Context) ([]skillcatalog.Package, error)
	Upload(context.Context, []byte, string) (skillcatalog.Package, error)
	Install(context.Context, string, string) (skillcatalog.Package, error)
	SetStatus(context.Context, string, string) (skillcatalog.Package, error)
	ToolBundles(context.Context) ([]skillcatalog.ToolBundle, error)
	Assignments(context.Context, string) ([]skillcatalog.Assignment, error)
	Assign(context.Context, string, string, string, string, string, json.RawMessage) (skillcatalog.Assignment, error)
	TeamPresets(context.Context) ([]skillcatalog.TeamPreset, error)
}

func (s *Server) skillWorkspace(w http.ResponseWriter, r *http.Request, write bool) (Identity, string, bool) {
	i, ok := s.authenticate(w, r)
	if !ok { return Identity{}, "", false }
	ws := strings.TrimSpace(r.URL.Query().Get("workspace_id"))
	if ws == "" {
		if err := r.ParseMultipartForm(skillcatalog.MaxPackageBytes + (1 << 20)); err == nil {
			ws = strings.TrimSpace(r.FormValue("workspace_id"))
		}
	}
	if ws == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return Identity{}, "", false
	}
	capability := "project.read"
	if write { capability = "project.write" }
	if !s.authorize(w, r, i, ws, capability) { return Identity{}, "", false }
	return i, ws, true
}

func (s *Server) listSkillPackages(w http.ResponseWriter, r *http.Request) {
	_, _, ok := s.skillWorkspace(w, r, false); if !ok { return }
	rows, err := s.skills.Packages(r.Context())
	respondDomain(w, rows, err, http.StatusOK)
}
func (s *Server) uploadSkillPackage(w http.ResponseWriter, r *http.Request) {
	i, _, ok := s.skillWorkspace(w, r, true); if !ok { return }
	if err := r.ParseMultipartForm(skillcatalog.MaxPackageBytes + (1 << 20)); err != nil {
		writeError(w, http.StatusBadRequest, "invalid skill upload"); return
	}
	f, _, err := r.FormFile("package")
	if err != nil { writeError(w, http.StatusBadRequest, "package file is required"); return }
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, skillcatalog.MaxPackageBytes+1))
	if err != nil || len(raw) > skillcatalog.MaxPackageBytes {
		writeError(w, http.StatusBadRequest, "skill package exceeds 25 MiB"); return
	}
	out, err := s.skills.Upload(r.Context(), raw, i.PrincipalID)
	respondDomain(w, out, err, http.StatusCreated)
}
func (s *Server) installSkillPackage(w http.ResponseWriter, r *http.Request) {
	i, _, ok := s.skillWorkspace(w, r, true); if !ok { return }
	out, err := s.skills.Install(r.Context(), r.PathValue("packageID"), i.PrincipalID)
	respondDomain(w, out, err, http.StatusOK)
}
func (s *Server) setSkillPackageStatus(w http.ResponseWriter, r *http.Request) {
	_, _, ok := s.skillWorkspace(w, r, true); if !ok { return }
	var in struct{ Status string `json:"status"` }
	if !decodeJSON(w, r, &in) { return }
	out, err := s.skills.SetStatus(r.Context(), r.PathValue("packageID"), in.Status)
	respondDomain(w, out, err, http.StatusOK)
}
func (s *Server) listToolBundles(w http.ResponseWriter, r *http.Request) {
	_, _, ok := s.skillWorkspace(w, r, false); if !ok { return }
	rows, err := s.skills.ToolBundles(r.Context())
	respondDomain(w, rows, err, http.StatusOK)
}
func (s *Server) listSkillAssignments(w http.ResponseWriter, r *http.Request) {
	_, ws, ok := s.skillWorkspace(w, r, false); if !ok { return }
	rows, err := s.skills.Assignments(r.Context(), ws)
	respondDomain(w, rows, err, http.StatusOK)
}
func (s *Server) assignSkill(w http.ResponseWriter, r *http.Request) {
	i, ws, ok := s.skillWorkspace(w, r, true); if !ok { return }
	var in struct {
		PackageID     string          `json:"package_id"`
		SubjectKind   string          `json:"subject_kind"`
		SubjectID     string          `json:"subject_id"`
		Configuration json.RawMessage `json:"configuration"`
	}
	if !decodeJSON(w, r, &in) { return }
	out, err := s.skills.Assign(r.Context(), ws, in.PackageID, in.SubjectKind, in.SubjectID, i.PrincipalID, in.Configuration)
	respondDomain(w, out, err, http.StatusCreated)
}
func (s *Server) listTeamPresets(w http.ResponseWriter, r *http.Request) {
	_, _, ok := s.skillWorkspace(w, r, false); if !ok { return }
	rows, err := s.skills.TeamPresets(r.Context())
	respondDomain(w, rows, err, http.StatusOK)
}
