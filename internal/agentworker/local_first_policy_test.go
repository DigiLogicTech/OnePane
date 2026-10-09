package agentworker

import "testing"

// Ordinary autonomous work should make progress on modest hardware using a
// verified L1-capable limited model rather than insisting on high GPU capacity.
// This does NOT relax tool authority, cost authorization or Research mode pins.
func TestDefaultRoutePolicyAllowsQualifiedLimitedModels(t *testing.T) {
    p := defaultRoutePolicy()
    if !p.AllowLimited {
        t.Fatal("qualified, admitted limited models should remain schedulable for ordinary tasks")
    }
    if p.AllowUntested {
        t.Fatal("allowing limited models must never auto-admit untested models")
    }
    if p.AllowSubscriptionUsage || p.AllowPotentialMonetarySpend {
        t.Fatal("limited hardware must not silently authorize subscriptions or paid clouds")
    }
    if !p.PreferZeroIncrementalCost {
        t.Fatal("ordinary tasks must continue to prefer local/zero-incremental-cost resources")
    }
    if !p.RoutingEnabled {
        t.Fatal("ordinary tasks should allow bounded operator-permitted route escalation")
    }
}
