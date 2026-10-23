package team

import (
	"encoding/json"
	"testing"
)

func TestTeamAssignmentCriteriaAreAdditiveAndCannotWeakenTaskAssurance(t *testing.T) {
	s, db, ss, specs := assignmentFixture(t)
	specs[0].RequiredVerification = "V2"
	specs[0].VerificationSpec = json.RawMessage(`{"result_checks":[{"pointer":"/summary","operator":"not_empty"}]}`)
	ids := acceptAssignments(t, s, ss, specs)
	var first, second string
	if err := db.SQL().QueryRow(`SELECT completion_json FROM tasks WHERE id=?`, ids["build"]).Scan(&first); err != nil {
		t.Fatal(err)
	}
	if err := db.SQL().QueryRow(`SELECT completion_json FROM tasks WHERE id=?`, ids["review"]).Scan(&second); err != nil {
		t.Fatal(err)
	}
	var one, two map[string]json.RawMessage
	json.Unmarshal([]byte(first), &one)
	json.Unmarshal([]byte(second), &two)
	if string(one["required_verification"]) != `"V2"` || string(two["required_verification"]) != `"V1"` || len(one["verification_spec"]) == 0 {
		t.Fatalf("distinct reviewed criteria lost: %s / %s", first, second)
	}
	if string(one["toolchain_manifest_sha256"]) != `"reviewed-toolchain"` {
		t.Fatal("criteria editing removed inherited toolchain pin")
	}
}

func TestTeamAssignmentCriteriaRejectWeakenedAndUnrecognizedRequirements(t *testing.T) {
	for _, spec := range []AssignmentSpec{
		{RequiredVerification: "V0"},
		{RequiredVerification: "V99"},
		{VerificationSpec: json.RawMessage(`{"ignore_parent":true}`)},
		{VerificationSpec: json.RawMessage(`null`)},
		{VerificationSpec: json.RawMessage(`{"result_checks":[{"pointer":"/x","operator":"exists","unknown":true}]}`)},
	} {
		base := map[string]json.RawMessage{"required_verification": json.RawMessage(`"V1"`)}
		if _, err := assignmentCompletionCriteria(base, spec); err == nil {
			t.Fatalf("unsafe assurance edit accepted: %+v", spec)
		}
	}
	base := map[string]json.RawMessage{"verification_spec": json.RawMessage(`{"result_checks":[{"pointer":"/parent","operator":"exists"}]}`)}
	result, err := assignmentCompletionCriteria(base, AssignmentSpec{VerificationSpec: json.RawMessage(`{"result_checks":[{"pointer":"/child","operator":"exists"}]}`)})
	if err != nil {
		t.Fatal(err)
	}
	var checks struct {
		Checks []json.RawMessage `json:"result_checks"`
	}
	json.Unmarshal(result["verification_spec"], &checks)
	if len(checks.Checks) != 2 || string(result["required_verification"]) != `"V1"` {
		t.Fatalf("parent check lost or added checks left at declaration-only: %s", result)
	}
}
