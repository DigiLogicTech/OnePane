package recoveryinventory

import "testing"

func TestRecoveryWorkerJournalContractIdentityIgnoresOnlyJSONFormatting(t *testing.T) {
	first, err := workerJournalContractHash([]byte("[\n{\"column\":\"task_id\"}\n]"))
	if err != nil {
		t.Fatal(err)
	}
	again, err := workerJournalContractHash([]byte("[\r\n { \"column\" : \"task_id\" }\r\n]"))
	if err != nil || first != again {
		t.Fatal("contract identity depends on checkout line endings")
	}
	changed, err := workerJournalContractHash([]byte(`[{"column":"attempt_id"}]`))
	if err != nil || changed == first {
		t.Fatal("changed structural contract retained old identity")
	}
	if _, err = workerJournalContractHash([]byte(`[{`)); err == nil {
		t.Fatal("malformed contract acquired an identity")
	}
}
