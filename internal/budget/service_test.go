package budget

import "testing"

func TestAccountAvailable(t *testing.T) {
	a := Account{LimitAmount: 100, CommittedAmount: 40, ReservedAmount: 25}
	if got := a.Available(); got != 35 {
		t.Fatalf("available=%d", got)
	}
}
func TestStatuses(t *testing.T) {
	if ReservationReserved == ReservationCommitted {
		t.Fatal("statuses must differ")
	}
}
