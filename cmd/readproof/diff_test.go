package main

import (
	"testing"
	"time"

	"github.com/fbzz/readproof/internal/diff"
)

func TestWhyLine(t *testing.T) {
	a := time.Date(2026, 8, 19, 16, 5, 30, 0, time.UTC)
	b := time.Date(2026, 8, 20, 9, 0, 0, 0, time.UTC)

	base := diff.EntryDiff{
		URI:             "readproof://demo/policies/refunds",
		Status:          diff.StatusChanged,
		SourceRevisionA: "8af92d1",
		SourceRevisionB: "c31be07",
		ObservedAtA:     a,
		ObservedAtB:     b,
	}

	want := "  why: source revision 8af92d1 → c31be07; observed 2026-08-19T16:05:30Z → 2026-08-20T09:00:00Z"
	if got := whyLine(base); got != want {
		t.Fatalf("whyLine =\n%q\nwant\n%q", got, want)
	}

	// Refs are appended only when at least one side was mounted by tag.
	tagged := base
	tagged.RefB = "prod"
	want += "; ref - → prod"
	if got := whyLine(tagged); got != want {
		t.Fatalf("whyLine (tagged) =\n%q\nwant\n%q", got, want)
	}
}
