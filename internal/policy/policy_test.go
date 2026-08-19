package policy

import (
	"testing"
	"time"
)

func TestEvaluate(t *testing.T) {
	now := time.Date(2026, 8, 19, 12, 0, 0, 0, time.UTC)

	cases := []struct {
		name              string
		policy            Policy
		hasCurrent        bool
		currentObservedAt time.Time
		want              Decision
	}{
		{name: "require_fresh always fetches, no current", policy: Policy{Strategy: StrategyRequireFresh}, hasCurrent: false, want: DecisionFetch},
		{name: "require_fresh always fetches, has current", policy: Policy{Strategy: StrategyRequireFresh}, hasCurrent: true, currentObservedAt: now, want: DecisionFetch},
		{name: "allow_stale fetches when no current", policy: Policy{Strategy: StrategyAllowStale, MaxAge: time.Hour}, hasCurrent: false, want: DecisionFetch},
		{name: "allow_stale uses current within max age", policy: Policy{Strategy: StrategyAllowStale, MaxAge: time.Hour}, hasCurrent: true, currentObservedAt: now.Add(-10 * time.Minute), want: DecisionUseCurrent},
		{name: "allow_stale fetches when expired", policy: Policy{Strategy: StrategyAllowStale, MaxAge: time.Hour}, hasCurrent: true, currentObservedAt: now.Add(-2 * time.Hour), want: DecisionFetch},
		{name: "allow_stale with no max age always uses current", policy: Policy{Strategy: StrategyAllowStale}, hasCurrent: true, currentObservedAt: now.Add(-100 * time.Hour), want: DecisionUseCurrent},
		{name: "pinned always uses pinned, with current", policy: Policy{Strategy: StrategyPinned, PinnedSnapshotID: "snap_1"}, hasCurrent: true, currentObservedAt: now, want: DecisionUsePinned},
		{name: "pinned always uses pinned, without current", policy: Policy{Strategy: StrategyPinned, PinnedSnapshotID: "snap_1"}, hasCurrent: false, want: DecisionUsePinned},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Evaluate(tc.policy, tc.hasCurrent, tc.currentObservedAt, now)
			if got != tc.want {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}
