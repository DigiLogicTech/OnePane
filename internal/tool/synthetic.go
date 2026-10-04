package tool

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/DigiLogicTech/OnePane/internal/authority"
	"github.com/DigiLogicTech/OnePane/internal/policy"
)

const (
	SyntheticAdapterID       = "builtin.synthetic"
	SyntheticAdapterVersion  = "1"
	SyntheticEchoToolID      = "synthetic.echo"
	SyntheticEchoToolVersion = "1"
	SyntheticEchoCapability  = "synthetic.echo"
)

type SyntheticAdapter struct{}

func (SyntheticAdapter) ID() string      { return SyntheticAdapterID }
func (SyntheticAdapter) Version() string { return SyntheticAdapterVersion }

func (SyntheticAdapter) Invoke(ctx context.Context, req AdapterRequest) (AdapterResult, error) {
	if err := ctx.Err(); err != nil {
		return AdapterResult{}, err
	}
	if req.ToolID != SyntheticEchoToolID || req.ToolVersion != SyntheticEchoToolVersion {
		return AdapterResult{}, fmt.Errorf("synthetic adapter does not implement %s@%s", req.ToolID, req.ToolVersion)
	}
	var value any
	if err := json.Unmarshal(req.Input, &value); err != nil {
		return AdapterResult{}, err
	}
	out, err := json.Marshal(map[string]any{
		"echo":         value,
		"resource_ref": req.ResourceRef,
	})
	if err != nil {
		return AdapterResult{}, err
	}
	return AdapterResult{Summary: "synthetic echo completed", Result: out}, nil
}

func RegisterSynthetic(reg *Registry) error {
	return reg.Register(Definition{
		ID: SyntheticEchoToolID, Version: SyntheticEchoToolVersion,
		CapabilityID: SyntheticEchoCapability,
		Mode:         authority.ActionRead,
		AdapterID:    SyntheticAdapterID, AdapterVersion: SyntheticAdapterVersion,
		Risk: policy.RiskLow, MinimumVerification: policy.VerificationV1, MinimumApproval: policy.ApprovalNone,
	}, SyntheticAdapter{})
}
