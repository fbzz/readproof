package resource

import "testing"

func TestParseURI(t *testing.T) {
	cases := []struct {
		name     string
		raw      string
		wantErr  bool
		wantNS   string
		wantPath string
	}{
		{name: "valid", raw: "ctx://acme/policies/refunds", wantNS: "acme", wantPath: "policies/refunds"},
		{name: "valid nested path", raw: "ctx://demo/a/b/c", wantNS: "demo", wantPath: "a/b/c"},
		{name: "missing scheme", raw: "acme/policies/refunds", wantErr: true},
		{name: "missing path", raw: "ctx://acme", wantErr: true},
		{name: "empty namespace", raw: "ctx:///policies/refunds", wantErr: true},
		{name: "empty", raw: "", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			u, err := ParseURI(tc.raw)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error for %q, got none", tc.raw)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error for %q: %v", tc.raw, err)
			}
			if u.Namespace != tc.wantNS || u.Path != tc.wantPath {
				t.Fatalf("got {%s %s}, want {%s %s}", u.Namespace, u.Path, tc.wantNS, tc.wantPath)
			}
			if u.String() != tc.raw {
				t.Fatalf("String() round-trip: got %q, want %q", u.String(), tc.raw)
			}
		})
	}
}
