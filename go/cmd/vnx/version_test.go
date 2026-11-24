package main

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/dave/vnx/go/pkg/vnx"
)

// TestVersionExitsZero pins the check a script runs to ask "is a working vnx
// here?": both spellings print the specification version alone and exit 0.
func TestVersionExitsZero(t *testing.T) {
	bin := build(t)
	for _, arg := range []string{"version", "--version"} {
		out, err := exec.Command(bin, arg).Output()
		if err != nil {
			t.Fatalf("vnx %s: %v", arg, err)
		}
		if got := strings.TrimSpace(string(out)); got != vnx.Version {
			t.Errorf("vnx %s printed %q, want %q", arg, got, vnx.Version)
		}
	}
}

// TestUsageListsVersion keeps the command discoverable from the help text.
func TestUsageListsVersion(t *testing.T) {
	if !strings.Contains(usage, "vnx version") {
		t.Error("usage does not mention vnx version")
	}
}
