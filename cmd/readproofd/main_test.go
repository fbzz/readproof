package main

import (
	"os"
	"strings"
	"testing"
)

const testDSN = "postgres://readproof:sup3r-s3cret%21@db.internal:5432/readproof?sslmode=disable"

// RP-12: a startup failure used to reach the log through log.Fatalf("%v", err)
// wrapping a pgx error, and a pgx connection error can quote the DSN — which
// carries the password.
func TestScrubDSNRemovesTheConnectionStringAndPassword(t *testing.T) {
	cases := []string{
		"open postgres backend: failed to connect to `" + testDSN + "`: connection refused",
		`password authentication failed for user "readproof" (password sup3r-s3cret!)`,
		"dial error: sup3r-s3cret%21@db.internal:5432",
	}
	for _, text := range cases {
		got := scrubDSN(text, testDSN)
		for _, secret := range []string{"sup3r-s3cret!", "sup3r-s3cret%21", testDSN} {
			if strings.Contains(got, secret) {
				t.Errorf("scrubDSN(%q) left %q in %q", text, secret, got)
			}
		}
	}

	// Nothing to scrub, nothing changed.
	if got := scrubDSN("some unrelated error", ""); got != "some unrelated error" {
		t.Errorf("scrubDSN with no DSN rewrote the message: %q", got)
	}
}

// What may be logged: where the server is pointed, never who it authenticates
// as or with what.
func TestDescribeDSNLogsOnlyHostAndDatabase(t *testing.T) {
	got := describeDSN(testDSN)
	if got != "host=db.internal:5432 db=readproof" {
		t.Fatalf("describeDSN = %q", got)
	}
	for _, secret := range []string{"sup3r-s3cret", "readproof:"} {
		if strings.Contains(got, secret) {
			t.Fatalf("describeDSN leaked %q: %q", secret, got)
		}
	}
	if got := describeDSN("not a url at all"); strings.Contains(got, "not a url") {
		t.Fatalf("describeDSN echoed an unparseable DSN: %q", got)
	}
}

func TestSplitPathListSplitsOnCommasAndThePathSeparator(t *testing.T) {
	sep := string(os.PathListSeparator)
	got := splitPathList(" /srv/policies , /srv/runbooks" + sep + "/srv/specs" + sep + " ")
	want := []string{"/srv/policies", "/srv/runbooks", "/srv/specs"}
	if len(got) != len(want) {
		t.Fatalf("splitPathList = %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("splitPathList = %q, want %q", got, want)
		}
	}
	if len(splitPathList("")) != 0 {
		t.Fatalf("an empty value should produce no roots")
	}
}

// A repeatable flag appends to whatever the environment seeded, and one
// occurrence may itself carry a comma-separated list.
func TestStringListFlagAppends(t *testing.T) {
	list := stringList{"FROM_ENV"}
	if err := list.Set("FIRST"); err != nil {
		t.Fatalf("set: %v", err)
	}
	if err := list.Set("SECOND, THIRD"); err != nil {
		t.Fatalf("set: %v", err)
	}
	if got := list.String(); got != "FROM_ENV,FIRST,SECOND,THIRD" {
		t.Fatalf("stringList = %q", got)
	}
}
