package gateway

import (
	"database/sql"
	"fmt"

	"github.com/DigiLogicTech/OnePane/internal/authority"
	"github.com/DigiLogicTech/OnePane/internal/policy"
	"github.com/DigiLogicTech/OnePane/internal/tool"
)

func Register(r *tool.Registry, db *sql.DB, secrets SecretBroker) error {
	a := NewSendAdapter(db, secrets)
	def := tool.Definition{ID: "gateway.send", Version: "1", CapabilityID: "gateway.send", Mode: authority.ActionExternalSend, AdapterID: a.ID(), AdapterVersion: a.Version(), Risk: policy.RiskLow, MinimumVerification: policy.VerificationV1, MinimumApproval: policy.ApprovalNone}
	if err := r.Register(def, a); err != nil {
		return fmt.Errorf("register gateway.send: %w", err)
	}
	return nil
}
