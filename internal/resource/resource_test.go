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
		// "@" is reserved for tag refs — ParseURI must never fold one into
		// the path. Callers split first with SplitRef.
		{name: "ref-bearing", raw: "ctx://acme/policies/refunds@prod", wantErr: true},
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

func TestSplitRef(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		wantErr bool
		wantURI string
		wantRef string
	}{
		{name: "bare uri", raw: "ctx://acme/policies/refunds", wantURI: "ctx://acme/policies/refunds"},
		{name: "tagged", raw: "ctx://acme/policies/refunds@prod", wantURI: "ctx://acme/policies/refunds", wantRef: "prod"},
		{name: "tag with dots and dashes", raw: "ctx://acme/p/x@v1.2-rc", wantURI: "ctx://acme/p/x", wantRef: "v1.2-rc"},
		{name: "empty ref", raw: "ctx://acme/policies/refunds@", wantErr: true},
		{name: "double at", raw: "ctx://acme/policies/refunds@a@b", wantErr: true},
		{name: "at in namespace", raw: "ctx://acme@x/policies", wantErr: true},
		{name: "missing scheme", raw: "acme/policies@prod", wantErr: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			uri, ref, err := SplitRef(tc.raw)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error for %q, got uri=%q ref=%q", tc.raw, uri, ref)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error for %q: %v", tc.raw, err)
			}
			if uri != tc.wantURI || ref != tc.wantRef {
				t.Fatalf("got (%q, %q), want (%q, %q)", uri, ref, tc.wantURI, tc.wantRef)
			}
		})
	}
}
