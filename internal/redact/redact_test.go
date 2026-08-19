package redact

import "testing"

func TestIsSensitiveHeader(t *testing.T) {
	cases := []struct {
		name string
		want bool
	}{
		{"Authorization", true},
		{"authorization", true},
		{"Cookie", true},
		{"X-Api-Key", true},
		{"X-Auth-Token", true},
		{"X-Secret", true},
		{"X-Password", true},
		{"Content-Type", false},
		{"Accept", false},
		{"User-Agent", false},
	}
	for _, tc := range cases {
		if got := IsSensitiveHeader(tc.name); got != tc.want {
			t.Errorf("IsSensitiveHeader(%q) = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestHeadersRedacts(t *testing.T) {
	in := map[string]string{
		"Authorization": "Bearer secret-token",
		"Content-Type":  "application/json",
	}
	out := Headers(in)
	if out["Authorization"] != Placeholder {
		t.Errorf("expected Authorization to be redacted, got %q", out["Authorization"])
	}
	if out["Content-Type"] != "application/json" {
		t.Errorf("expected Content-Type to pass through unchanged, got %q", out["Content-Type"])
	}
	// Original map must not be mutated.
	if in["Authorization"] != "Bearer secret-token" {
		t.Errorf("Headers must not mutate its input, got %q", in["Authorization"])
	}
}

func TestHeadersNil(t *testing.T) {
	if got := Headers(nil); got != nil {
		t.Errorf("expected nil in -> nil out, got %v", got)
	}
}
