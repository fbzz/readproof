package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/fbzz/readproof/internal/version"
)

// execRoot runs the real command tree the way `readproof` does, with
// cobra's output captured. It returns stdout (where cobra writes usage),
// stderr, and the error main would turn into a non-zero exit.
func execRoot(t *testing.T, args ...string) (stdout, stderr string, err error) {
	t.Helper()

	// The root command reads these at construction time; pin them so a
	// developer's exported READPROOF_SERVER_URL can't send the test to a
	// server.
	t.Setenv("READPROOF_SERVER_URL", "")
	t.Setenv("READPROOF_API_KEY", "")

	root := newRootCmd()
	var out, errOut bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errOut)
	root.SetArgs(args)
	err = root.Execute()
	return out.String(), errOut.String(), err
}

// A resolution failure or an unknown run is a runtime error, not CLI
// misuse: the user typed a valid command. Printing the usage block there
// buries the one line that says what went wrong. The returned error is
// what makes `readproof` exit non-zero.
func TestRuntimeErrorPrintsNoUsage(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"unregistered uri", []string{"get", "readproof://demo/nope"}, "not found"},
		{"unknown run", []string{"run", "commit", "run-never-started"}, "run: not found: run-never-started"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			args := append(tc.args, "--data-dir", t.TempDir())
			stdout, stderr, err := execRoot(t, args...)
			if err == nil {
				t.Fatalf("expected an error (non-zero exit) for %v", tc.args)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %v, want it to mention %q", err, tc.want)
			}
			if strings.Contains(stdout, "Usage:") {
				t.Errorf("runtime error printed the usage block:\n%s", stdout)
			}
			// main is the single place the error is printed; cobra must
			// not also print it, or every failure shows up twice.
			if stderr != "" {
				t.Errorf("cobra printed to stderr as well as returning the error:\n%s", stderr)
			}
		})
	}
}

// The other half of the same contract: genuine misuse still gets usage,
// because there the usage block is the answer.
func TestUsageErrorsStillPrintUsage(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"missing argument", []string{"get"}, "accepts 1 arg"},
		{"unknown flag", []string{"get", "--nope", "readproof://demo/x"}, "unknown flag"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stdout, _, err := execRoot(t, tc.args...)
			if err == nil {
				t.Fatalf("expected an error for %v", tc.args)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %v, want it to mention %q", err, tc.want)
			}
			if !strings.Contains(stdout, "Usage:") {
				t.Errorf("no usage block printed for %v:\n%s", tc.args, stdout)
			}
		})
	}
}

// `readproof version` and `readproof --version` are the same line: a bug
// report quoting either has to identify the same build.
func TestVersionCommandAndFlagAgree(t *testing.T) {
	want := "readproof " + version.String() + "\n"

	stdout, _, err := execRoot(t, "version")
	if err != nil {
		t.Fatalf("readproof version: %v", err)
	}
	if stdout != want {
		t.Errorf("readproof version printed %q, want %q", stdout, want)
	}

	stdout, _, err = execRoot(t, "--version")
	if err != nil {
		t.Fatalf("readproof --version: %v", err)
	}
	if stdout != want {
		t.Errorf("readproof --version printed %q, want %q", stdout, want)
	}
	if !strings.Contains(want, version.Version) {
		t.Errorf("version line %q does not contain %q", want, version.Version)
	}
}
