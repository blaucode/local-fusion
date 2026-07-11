package version

import (
	"strings"
	"testing"
)

func TestStringContainsVersionAndCommit(t *testing.T) {
	s := String()
	if !strings.Contains(s, Version) {
		t.Errorf("String() = %q, want it to contain Version %q", s, Version)
	}
	if !strings.Contains(s, Commit) {
		t.Errorf("String() = %q, want it to contain Commit %q", s, Commit)
	}
}
