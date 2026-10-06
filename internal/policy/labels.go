package policy

import (
	"fmt"
	"strings"
)

// ValidateDataLabel validates the independent workspace, confidentiality,
// residency and trust dimensions used by artifacts, observations and context.
func ValidateDataLabel(label DataLabel) error {
	if !validLabel(label) {
		return fmt.Errorf("invalid data label")
	}
	return nil
}

// StorageLabel converts protocol-facing label values to the lowercase values
// frozen in the v0.1 SQLite schema.
func StorageLabel(label DataLabel) (confidentiality, residency, trust string, err error) {
	if err := ValidateDataLabel(label); err != nil {
		return "", "", "", err
	}
	return strings.ToLower(string(label.Confidentiality)), strings.ToLower(string(label.Residency)), strings.ToLower(string(label.Trust)), nil
}

// DataLabelFromStorage restores a typed label from the frozen SQLite values.
func DataLabelFromStorage(workspaceID, confidentiality, residency, trust string, originNodeID *string) (DataLabel, error) {
	label := DataLabel{
		WorkspaceID:     workspaceID,
		Confidentiality: Confidentiality(strings.ToUpper(confidentiality)),
		Residency:       Residency(strings.ToUpper(residency)),
		Trust:           TrustClass(strings.ToUpper(trust)),
	}
	if originNodeID != nil {
		label.OriginNodeID = *originNodeID
	}
	if err := ValidateDataLabel(label); err != nil {
		return DataLabel{}, err
	}
	return label, nil
}
