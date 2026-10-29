package team

import (
	"bytes"
	"encoding/json"
	"strings"

	"github.com/DigiLogicTech/OnePane/internal/assurancecriteria"
	"github.com/DigiLogicTech/OnePane/internal/policy"
	"github.com/DigiLogicTech/OnePane/internal/verification"
)

// Assignment checks are additive. They cannot delete inherited checks, lower
// the parent's assurance floor or alter its routing/toolchain authority.
func assignmentCompletionCriteria(base map[string]json.RawMessage, spec AssignmentSpec) (map[string]json.RawMessage, error) {
	level := policy.VerificationV0
	if raw, ok := base["required_verification"]; ok {
		var text string
		if json.Unmarshal(raw, &text) != nil {
			return nil, ErrInvalid
		}
		level = policy.VerificationLevel(strings.ToUpper(strings.TrimSpace(text)))
		if !verification.ValidLevel(level) {
			return nil, ErrInvalid
		}
	}
	if spec.RequiredVerification != "" {
		requested := policy.VerificationLevel(spec.RequiredVerification)
		if !verification.ValidLevel(requested) || verification.LevelRank(requested) < verification.LevelRank(level) {
			return nil, ErrInvalid
		}
		level = requested
	}
	if len(spec.VerificationSpec) > 0 {
		decode := func(raw json.RawMessage) (assurancecriteria.Criteria, error) {
			var value assurancecriteria.Criteria
			if !strings.HasPrefix(strings.TrimSpace(string(raw)), "{") {
				return value, ErrInvalid
			}
			d := json.NewDecoder(bytes.NewReader(raw))
			d.DisallowUnknownFields()
			if len(raw) > 16<<10 || d.Decode(&value) != nil {
				return value, ErrInvalid
			}
			return value, nil
		}
		added, err := decode(spec.VerificationSpec)
		if err != nil {
			return nil, err
		}
		var inherited assurancecriteria.Criteria
		if raw, ok := base["verification_spec"]; ok {
			inherited, err = decode(raw)
			if err != nil {
				return nil, err
			}
		}
		inherited.ResultChecks = append(inherited.ResultChecks, added.ResultChecks...)
		inherited.Artifacts = append(inherited.Artifacts, added.Artifacts...)
		inherited.Observations = append(inherited.Observations, added.Observations...)
		if len(inherited.ResultChecks) > 32 || len(inherited.Artifacts) > 32 || len(inherited.Observations) > 32 {
			return nil, ErrInvalid
		}
		base["verification_spec"], _ = json.Marshal(inherited)
		if level == policy.VerificationV0 {
			level = policy.VerificationV1
		}
	}
	if spec.RequiredVerification != "" || len(spec.VerificationSpec) > 0 {
		base["required_verification"], _ = json.Marshal(level)
	}
	return base, nil
}
