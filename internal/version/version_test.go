package version

import "testing"

// Commit is stamped in with -ldflags at release time and empty otherwise;
// String has to read well both ways, since it ends up in `readproof
// version` output and in MCP server info.
func TestStringAppendsCommitOnlyWhenStamped(t *testing.T) {
	if got := String(); got != Version {
		t.Fatalf("String() with no stamped commit = %q, want %q", got, Version)
	}

	prev := Commit
	t.Cleanup(func() { Commit = prev })
	Commit = "a1b2c3d"
	if got, want := String(), Version+"+a1b2c3d"; got != want {
		t.Fatalf("String() with a stamped commit = %q, want %q", got, want)
	}
}
