package system

import "testing"

func TestCanTransition(t *testing.T) {
	states := []Mode{
		ModeUninitialized, ModeBootstrap, ModeCommissioning, ModeRecovery,
		ModeNormal, ModeSafe, ModeReadOnly, ModeMaintenance, ModeDegraded,
	}

	allowed := map[[2]Mode]bool{}
	add := func(from Mode, tos ...Mode) {
		for _, to := range tos {
			allowed[[2]Mode{from, to}] = true
		}
	}

	add(ModeUninitialized, ModeBootstrap)
	add(ModeBootstrap, ModeCommissioning, ModeRecovery, ModeDegraded, ModeSafe)
	add(ModeCommissioning, ModeNormal, ModeDegraded, ModeRecovery, ModeSafe)
	add(ModeRecovery, ModeCommissioning, ModeNormal, ModeDegraded, ModeReadOnly, ModeSafe)
	add(ModeNormal, ModeDegraded, ModeReadOnly, ModeMaintenance, ModeRecovery, ModeSafe)
	add(ModeDegraded, ModeNormal, ModeReadOnly, ModeMaintenance, ModeRecovery, ModeSafe)
	add(ModeReadOnly, ModeNormal, ModeDegraded, ModeMaintenance, ModeRecovery, ModeSafe)
	add(ModeMaintenance, ModeCommissioning, ModeNormal, ModeDegraded, ModeRecovery, ModeSafe)
	add(ModeSafe, ModeRecovery, ModeCommissioning)

	for _, from := range states {
		for _, to := range states {
			got := CanTransition(from, to)
			want := allowed[[2]Mode{from, to}]
			if got != want {
				t.Fatalf("CanTransition(%q,%q)=%v want %v", from, to, got, want)
			}
		}
	}
}
