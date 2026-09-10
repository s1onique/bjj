package lab

import (
	"os/exec"
	"testing"
)

// This file holds test-only prerequisite machinery. It is intentionally
// NOT part of the production `internal/lab` package surface.
//
// Why: test assertion helpers belong in `_test.go` files so they
// cannot leak into production binaries or be invoked by callers who
// should be using the regular error-returning `requireTool` instead.

// requireToolTB is the test-side helper used by every central lab test.
// It fails the test (not skips) when the named executable is missing,
// so a `go test` run cannot report PASS merely because the central
// publication laboratory was unavailable.
//
// This is the canonical fail-closed prerequisite gate required by the
// ACT-BJJ-LAB01 contract.
func requireToolTB(tb testing.TB, name string) {
	tb.Helper()
	if _, err := exec.LookPath(name); err != nil {
		tb.Fatalf("required lab tool %q unavailable: %v -- "+
			"install %s before running the BJJ publication laboratory",
			name, err, name)
	}
}

// RequireGitAndJJ is the canonical central-gate prerequisite check.
// It MUST be called at the top of every test in the central
// acceptance path. A test that calls this helper cannot silently skip
// when its required tools are missing.
//
// Required property:
//
//	make all
//	  without git -> FAIL
//	  without jj  -> FAIL
//
// Production code must NOT depend on this symbol. Use the
// regular-error API `requireTool(name)` for runtime prerequisite
// checks in non-test code.
func RequireGitAndJJ(tb testing.TB) {
	tb.Helper()
	requireToolTB(tb, "git")
	requireToolTB(tb, "jj")
}
