package tag

import (
	"errors"
	"strings"
	"testing"
)

func TestValidateName(t *testing.T) {
	valid := []string{
		"prod",
		"v3",
		"1",
		"release-2026.08",
		"a_b.c-d",
		strings.Repeat("a", 64),
	}
	for _, name := range valid {
		if err := ValidateName(name); err != nil {
			t.Fatalf("ValidateName(%q) = %v, want nil", name, err)
		}
	}

	invalid := []string{
		"",
		"-leading-dash",
		".leading-dot",
		"_leading-underscore",
		"has space",
		"has/slash",
		"has@at", // would make ctx://ns/p@a@b ambiguous
		"héllo",  // ASCII only
		strings.Repeat("a", 65),
	}
	for _, name := range invalid {
		err := ValidateName(name)
		if !errors.Is(err, ErrInvalidName) {
			t.Fatalf("ValidateName(%q) = %v, want ErrInvalidName", name, err)
		}
	}
}
