package plan_test

import (
	"os/exec"
	"testing"
)

// This file holds test-only prerequisite machinery, mirroring the
// convention from ACT-BJJ-LAB01.

func requireToolTB(tb testing.TB, name string) {
	tb.Helper()
	if _, err := exec.LookPath(name); err != nil {
		tb.Fatalf("required tool %q missing: %v -- install %s to run BJJ plan tests", name, err, name)
	}
}

func requirePlanPrereqs(tb testing.TB) {
	tb.Helper()
	requireToolTB(tb, "jj")
	requireToolTB(tb, "git")
}

// TestRequiredPlanToolsAvailable is the canonical fail-closed gate.
func TestRequiredPlanToolsAvailable(t *testing.T) {
	requirePlanPrereqs(t)
}
