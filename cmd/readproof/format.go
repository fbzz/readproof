package main

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/fbzz/readproof/internal/policy"
	"github.com/fbzz/readproof/internal/resolver"
)

func newTabWriter() *tabwriter.Writer {
	return tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
}

func freshnessLabel(result resolver.ResolveResult) string {
	switch result.Decision {
	case policy.DecisionFetch:
		return fmt.Sprintf("fresh (observed %s, policy %s)", result.Snapshot.ObservedAt.Format(time.RFC3339), result.Resource.Policy.Strategy)
	case policy.DecisionUseCurrent:
		age := time.Since(result.Snapshot.ObservedAt).Round(time.Second)
		return fmt.Sprintf("cached (age %s, policy %s)", age, result.Resource.Policy.Strategy)
	case policy.DecisionUsePinned:
		return fmt.Sprintf("pinned (snapshot %s)", result.Snapshot.SnapshotID)
	case policy.DecisionUseTag:
		return fmt.Sprintf("tagged (@%s -> snapshot %s, observed %s, policy not consulted)",
			result.Ref, result.Snapshot.SnapshotID, result.Snapshot.ObservedAt.Format(time.RFC3339))
	default:
		return "unknown"
	}
}

func formatProvenance(p map[string]string) string {
	keys := make([]string, 0, len(p))
	for k := range p {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%s", k, p[k]))
	}
	return strings.Join(parts, " ")
}

func pluralEntries(n int) string {
	if n == 1 {
		return "y"
	}
	return "ies"
}

func parseHeaders(raw []string) (map[string]string, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	headers := make(map[string]string, len(raw))
	for _, h := range raw {
		key, value, ok := strings.Cut(h, ":")
		if !ok || strings.TrimSpace(key) == "" {
			return nil, fmt.Errorf("invalid --header %q, want Key:Value", h)
		}
		headers[strings.TrimSpace(key)] = strings.TrimSpace(value)
	}
	return headers, nil
}

func indent(text, prefix string) string {
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	var b strings.Builder
	for _, l := range lines {
		b.WriteString(prefix)
		b.WriteString(l)
		b.WriteString("\n")
	}
	return b.String()
}
