package api

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/DigiLogicTech/OnePane/internal/team"
)

func (s *Server) listTeams(w http.ResponseWriter, r *http.Request) {
	i, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	if s.team == nil {
		writeError(w, 503, "team mode unavailable")
		return
	}
	ws := r.URL.Query().Get("workspace_id")
	if !s.authorize(w, r, i, ws, "team.read") {
		return
	}
	out, err := s.team.ListTeams(r.Context(), ws)
	respondDomain(w, out, err, 200)
}
func (s *Server) createTeam(w http.ResponseWriter, r *http.Request) {
	i, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	var in struct{ WorkspaceID, Name, Purpose string }
	if !decodeJSON(w, r, &in) {
		return
	}
	if !s.authorize(w, r, i, in.WorkspaceID, "team.write") {
		return
	}
	out, err := s.team.CreateTeam(r.Context(), team.CreateTeamCommand{WorkspaceID: in.WorkspaceID, Name: in.Name, Purpose: in.Purpose, CreatedBy: i.PrincipalID})
	respondDomain(w, out, err, 201)
}
func (s *Server) teamFor(w http.ResponseWriter, r *http.Request, cap string) (Identity, team.Team, bool) {
	i, ok := s.authenticate(w, r)
	if !ok {
		return Identity{}, team.Team{}, false
	}
	t, err := s.team.Team(r.Context(), r.PathValue("teamID"))
	if err != nil {
		respondDomain(w, nil, err, 0)
		return Identity{}, team.Team{}, false
	}
	if !s.authorize(w, r, i, t.WorkspaceID, cap) {
		return Identity{}, team.Team{}, false
	}
	return i, t, true
}
func (s *Server) listTeamMembers(w http.ResponseWriter, r *http.Request) {
	_, t, ok := s.teamFor(w, r, "team.read")
	if !ok {
		return
	}
	out, err := s.team.ListMembers(r.Context(), t.ID)
	respondDomain(w, out, err, 200)
}
func (s *Server) addTeamMember(w http.ResponseWriter, r *http.Request) {
	i, t, ok := s.teamFor(w, r, "team.write")
	if !ok {
		return
	}
	var in struct {
		PrincipalID   *string         `json:"principal_id"`
		MemberKind    string          `json:"member_kind"`
		DisplayName   string          `json:"display_name"`
		RoleName      string          `json:"role_name"`
		CapabilityID  string          `json:"capability_id"`
		ProtocolLevel string          `json:"protocol_level"`
		RoutePolicy   json.RawMessage `json:"route_policy"`
		Config        json.RawMessage `json:"config"`
		Ordinal       int             `json:"ordinal"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	out, err := s.team.AddMember(r.Context(), team.AddMemberCommand{TeamID: t.ID, PrincipalID: in.PrincipalID, MemberKind: in.MemberKind, DisplayName: in.DisplayName, RoleName: in.RoleName, CapabilityID: in.CapabilityID, ProtocolLevel: in.ProtocolLevel, RoutePolicy: in.RoutePolicy, Config: in.Config, Ordinal: in.Ordinal, CreatedBy: i.PrincipalID})
	respondDomain(w, out, err, 201)
}
func (s *Server) startTeamSession(w http.ResponseWriter, r *http.Request) {
	i, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	var in struct {
		TeamID          string          `json:"team_id"`
		GatewayTargetID *string         `json:"gateway_target_id"`
		Config          json.RawMessage `json:"config"`
		ExecutionMode   string          `json:"execution_mode"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	t, err := s.team.Team(r.Context(), in.TeamID)
	if err != nil {
		respondDomain(w, nil, err, 0)
		return
	}
	if !s.authorize(w, r, i, t.WorkspaceID, "team.write") {
		return
	}
	out, err := s.team.StartSession(r.Context(), team.StartSessionCommand{TaskID: r.PathValue("taskID"), TeamID: in.TeamID, CreatedBy: i.PrincipalID, GatewayTargetID: in.GatewayTargetID, Config: in.Config, ExecutionMode: in.ExecutionMode})
	respondDomain(w, out, err, 201)
}
func (s *Server) getTaskTeamSession(w http.ResponseWriter, r *http.Request) {
	i, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	out, err := s.team.SessionByTask(r.Context(), r.PathValue("taskID"))
	if err != nil {
		respondDomain(w, nil, err, 0)
		return
	}
	if !s.authorize(w, r, i, out.WorkspaceID, "team.read") {
		return
	}
	writeJSON(w, 200, out)
}
func (s *Server) sessionFor(w http.ResponseWriter, r *http.Request, cap string) (Identity, team.Session, bool) {
	i, ok := s.authenticate(w, r)
	if !ok {
		return Identity{}, team.Session{}, false
	}
	ss, err := s.team.Session(r.Context(), r.PathValue("sessionID"))
	if err != nil {
		respondDomain(w, nil, err, 0)
		return Identity{}, team.Session{}, false
	}
	if !s.authorize(w, r, i, ss.WorkspaceID, cap) {
		return Identity{}, team.Session{}, false
	}
	return i, ss, true
}
func (s *Server) getTeamSession(w http.ResponseWriter, r *http.Request) {
	_, ss, ok := s.sessionFor(w, r, "team.read")
	if ok {
		writeJSON(w, 200, ss)
	}
}
func (s *Server) getTeamSessionManifest(w http.ResponseWriter, r *http.Request) {
	_, ss, ok := s.sessionFor(w, r, "team.read")
	if !ok {
		return
	}
	manifest, err := s.team.SessionManifest(r.Context(), ss.ID)
	if err != nil {
		respondDomain(w, nil, err, 0)
		return
	}
	bindings, err := s.team.ListSeatBindings(r.Context(), ss.ID)
	if err != nil {
		respondDomain(w, nil, err, 0)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"manifest": manifest, "seat_bindings": bindings})
}
func (s *Server) listTeamMessages(w http.ResponseWriter, r *http.Request) {
	_, ss, ok := s.sessionFor(w, r, "team.read")
	if !ok {
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	out, err := s.team.ListMessages(r.Context(), ss.ID, limit)
	respondDomain(w, out, err, 200)
}
func (s *Server) postTeamMessage(w http.ResponseWriter, r *http.Request) {
	i, ss, ok := s.sessionFor(w, r, "team.write")
	if !ok {
		return
	}
	var in struct {
		AuthorMemberID   *string         `json:"author_member_id"`
		ReplyToMessageID *string         `json:"reply_to_message_id"`
		Content          json.RawMessage `json:"content"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	out, err := s.team.PostMessage(r.Context(), team.PostMessageCommand{SessionID: ss.ID, AuthorPrincipalID: i.PrincipalID, AuthorMemberID: in.AuthorMemberID, ReplyToMessageID: in.ReplyToMessageID, Kind: "human", Content: in.Content})
	respondDomain(w, out, err, 201)
}
func (s *Server) requestTeamRound(w http.ResponseWriter, r *http.Request) {
	i, ss, ok := s.sessionFor(w, r, "team.write")
	if !ok {
		return
	}
	var in struct {
		TriggerMessageID *string  `json:"trigger_message_id"`
		MemberIDs        []string `json:"member_ids"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	out, err := s.team.RequestRound(r.Context(), team.RequestRoundCommand{SessionID: ss.ID, RequestedByPrincipalID: i.PrincipalID, TriggerMessageID: in.TriggerMessageID, MemberIDs: in.MemberIDs})
	respondDomain(w, out, err, 202)
}
func (s *Server) listTeamTurns(w http.ResponseWriter, r *http.Request) {
	_, ss, ok := s.sessionFor(w, r, "team.read")
	if !ok {
		return
	}
	out, err := s.team.ListTurns(r.Context(), ss.ID)
	respondDomain(w, out, err, 200)
}
func (s *Server) listTeamPlans(w http.ResponseWriter, r *http.Request) {
	_, ss, ok := s.sessionFor(w, r, "team.read")
	if !ok {
		return
	}
	out, err := s.team.ListPlans(r.Context(), ss.ID)
	respondDomain(w, out, err, 200)
}
func (s *Server) proposeTeamPlan(w http.ResponseWriter, r *http.Request) {
	i, ss, ok := s.sessionFor(w, r, "team.write")
	if !ok {
		return
	}
	var in struct {
		ProposedByMemberID *string         `json:"proposed_by_member_id"`
		Plan               json.RawMessage `json:"plan"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	out, err := s.team.ProposePlan(r.Context(), team.ProposePlanCommand{SessionID: ss.ID, ProposedByPrincipalID: i.PrincipalID, ProposedByMemberID: in.ProposedByMemberID, Plan: in.Plan})
	respondDomain(w, out, err, 201)
}
func (s *Server) acceptTeamPlan(w http.ResponseWriter, r *http.Request) {
	i, ss, ok := s.sessionFor(w, r, "team.write")
	if !ok {
		return
	}
	var in struct {
		AcceptOpenRisk bool            `json:"accept_open_risk"`
		Decision       json.RawMessage `json:"decision"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	out, err := s.team.AcceptPlan(r.Context(), team.AcceptPlanCommand{SessionID: ss.ID, PlanID: r.PathValue("planID"), AcceptedByPrincipalID: i.PrincipalID, AcceptOpenRisk: in.AcceptOpenRisk, Decision: in.Decision})
	respondDomain(w, out, err, 200)
}
func (s *Server) listTeamObjections(w http.ResponseWriter, r *http.Request) {
	_, ss, ok := s.sessionFor(w, r, "team.read")
	if !ok {
		return
	}
	out, err := s.team.ListObjections(r.Context(), ss.ID)
	respondDomain(w, out, err, 200)
}
func (s *Server) raiseTeamObjection(w http.ResponseWriter, r *http.Request) {
	i, ss, ok := s.sessionFor(w, r, "team.write")
	if !ok {
		return
	}
	var in struct {
		PlanID           *string         `json:"plan_id"`
		RaisedByMemberID *string         `json:"raised_by_member_id"`
		Severity         string          `json:"severity"`
		Summary          string          `json:"summary"`
		Detail           json.RawMessage `json:"detail"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	out, err := s.team.RaiseObjection(r.Context(), team.RaiseObjectionCommand{SessionID: ss.ID, PlanID: in.PlanID, RaisedByMemberID: in.RaisedByMemberID, Severity: in.Severity, Summary: in.Summary, Detail: in.Detail, ActorPrincipalID: i.PrincipalID})
	respondDomain(w, out, err, 201)
}
func (s *Server) resolveTeamObjection(w http.ResponseWriter, r *http.Request) {
	i, ok := s.authenticate(w, r)
	if !ok {
		return
	}
	ob, err := s.team.Objection(r.Context(), r.PathValue("objectionID"))
	if err != nil {
		respondDomain(w, nil, err, 0)
		return
	}
	if !s.authorize(w, r, i, ob.WorkspaceID, "team.write") {
		return
	}
	var in struct {
		Status     string          `json:"status"`
		Resolution json.RawMessage `json:"resolution"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	out, err := s.team.ResolveObjection(r.Context(), team.ResolveObjectionCommand{ObjectionID: r.PathValue("objectionID"), Status: in.Status, ResolvedByPrincipalID: i.PrincipalID, Resolution: in.Resolution})
	respondDomain(w, out, err, 200)
}
func (s *Server) listTeamDecisions(w http.ResponseWriter, r *http.Request) {
	_, ss, ok := s.sessionFor(w, r, "team.read")
	if !ok {
		return
	}
	out, err := s.team.ListDecisions(r.Context(), ss.ID)
	respondDomain(w, out, err, 200)
}
func (s *Server) reopenTeamDeliberation(w http.ResponseWriter, r *http.Request) {
	i, ss, ok := s.sessionFor(w, r, "team.write")
	if !ok {
		return
	}
	var in struct {
		Reason string `json:"reason"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	out, err := s.team.ReopenDeliberation(r.Context(), team.ReopenCommand{SessionID: ss.ID, RequestedByPrincipalID: i.PrincipalID, Reason: in.Reason})
	respondDomain(w, out, err, 200)
}

func (s *Server) updateTeamConfiguration(w http.ResponseWriter, r *http.Request) {
	i, t, ok := s.teamFor(w, r, "team.write")
	if !ok { return }
	var in struct {
		ExpectedRevision int64           `json:"expected_revision"`
		Configuration    json.RawMessage `json:"configuration"`
	}
	if !decodeJSON(w, r, &in) { return }
	out, err := s.team.UpdateConfiguration(r.Context(), team.UpdateConfigurationCommand{
		TeamID: t.ID, ActorPrincipalID: i.PrincipalID, ExpectedRevision: in.ExpectedRevision, Configuration: in.Configuration,
	})
	respondDomain(w, out, err, http.StatusOK)
}
