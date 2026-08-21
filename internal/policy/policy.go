package policy

import "time"

// Strategy is one of the freshness strategies Readproof v0.1 supports.
type Strategy string

const (
	StrategyRequireFresh Strategy = "require_fresh"
	StrategyAllowStale   Strategy = "allow_stale"
	StrategyPinned       Strategy = "pinned"
)

// Policy controls how a Resource's freshness is enforced during resolution.
type Policy struct {
	Strategy Strategy
	// MaxAge applies only to StrategyAllowStale. Zero means no TTL (never
	// refresh once a snapshot exists).
	MaxAge time.Duration
	// PinnedSnapshotID applies only to StrategyPinned.
	PinnedSnapshotID string
}

// Decision is the outcome of evaluating a Policy against current cache state.
type Decision int

const (
	DecisionFetch Decision = iota
	DecisionUseCurrent
	DecisionUsePinned
	// DecisionUseTag is the outcome of resolving readproof://ns/path@<tag>. It
	// is never produced by Evaluate: a tag ref bypasses policy entirely (the
	// caller asked for one exact snapshot), so the resolver sets it directly.
	DecisionUseTag
)

func (d Decision) String() string {
	switch d {
	case DecisionFetch:
		return "fetch"
	case DecisionUseCurrent:
		return "use_current"
	case DecisionUsePinned:
		return "use_pinned"
	case DecisionUseTag:
		return "use_tag"
	default:
		return "unknown"
	}
}

// Evaluate decides whether a resolve should re-fetch from the source, reuse
// the current cached snapshot, or resolve to a pinned snapshot.
func Evaluate(p Policy, hasCurrent bool, currentObservedAt, now time.Time) Decision {
	switch p.Strategy {
	case StrategyPinned:
		return DecisionUsePinned
	case StrategyRequireFresh:
		return DecisionFetch
	case StrategyAllowStale:
		if !hasCurrent {
			return DecisionFetch
		}
		if p.MaxAge > 0 && now.Sub(currentObservedAt) > p.MaxAge {
			return DecisionFetch
		}
		return DecisionUseCurrent
	default:
		return DecisionFetch
	}
}
