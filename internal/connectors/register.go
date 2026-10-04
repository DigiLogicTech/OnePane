package connectors

import (
	"fmt"

	"github.com/DigiLogicTech/OnePane/internal/authority"
	"github.com/DigiLogicTech/OnePane/internal/policy"
	"github.com/DigiLogicTech/OnePane/internal/tool"
)

func RegisterBuiltins(r *tool.Registry, secrets SecretBroker) error {
	for _, p := range Builtins() {
		if err := registerMode(r, secrets, p, "read", authority.ActionRead, policy.RiskLow, policy.VerificationV1); err != nil {
			return err
		}
		if err := registerMode(r, secrets, p, "mutate", authority.ActionMutate, policy.RiskMedium, policy.VerificationV2); err != nil {
			return err
		}
		if p.SupportsSend {
			if err := registerMode(r, secrets, p, "send", authority.ActionExternalSend, policy.RiskHigh, policy.VerificationV3); err != nil {
				return err
			}
		}
	}
	return nil
}

func registerMode(r *tool.Registry, secrets SecretBroker, p Preset, name string, mode authority.ActionMode, risk policy.RiskLevel, verify policy.VerificationLevel) error {
	a := NewHTTPAdapter(p, name, secrets)
	def := tool.Definition{ID: "plugin." + p.ID + "." + name, Version: "1", CapabilityID: "plugin." + p.ID + "." + name, Mode: mode, AdapterID: a.ID(), AdapterVersion: a.Version(), Risk: risk, MinimumVerification: verify, MinimumApproval: policy.ApprovalNone}
	if err := r.Register(def, a); err != nil {
		return fmt.Errorf("register %s: %w", def.ID, err)
	}
	return nil
}
