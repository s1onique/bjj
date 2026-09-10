package lab

import (
	"context"
	"os"
	"strings"
	"testing"
)

// TestRequiredLabToolsAvailable is the canonical fail-closed
// prerequisite gate for the publication laboratory. It MUST be the
// first test to run in this package; if either required executable
// is missing, the entire acceptance run is considered to have FAILED,
// not skipped.
//
// Required property:
//
//	make all
//	  without git -> FAIL
//	  without jj  -> FAIL
//
// Individual tests MAY also call RequireGitAndJJ for fail-closed
// behavior, but this central gate ensures the test binary itself
// refuses to report success in a degraded environment.
func TestRequiredLabToolsAvailable(t *testing.T) {
	RequireGitAndJJ(t)
}

// TestRequireTool still verifies the helper-level requireTool returns
// a typed error for a missing tool.
func TestRequireTool(t *testing.T) {
	if err := requireTool("git"); err != nil {
		t.Fatalf("git should be available: %v", err)
	}
	if err := requireTool("this-tool-does-not-exist-xyz"); err == nil {
		t.Fatal("expected error for missing tool")
	}
}

func TestIsLocalRemoteURL(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"/tmp/remote.git", true},
		{"file:///tmp/remote.git", true},
		{"https://github.com/foo/bar.git", false},
		{"git@github.com:foo/bar.git", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := IsLocalRemoteURL(tc.in); got != tc.want {
			t.Errorf("IsLocalRemoteURL(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestSetupCreatesDisposableLab(t *testing.T) {
	RequireGitAndJJ(t)

	ctx := context.Background()
	l, err := Setup(ctx)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	defer l.Cleanup()

	if !strings.HasPrefix(l.Root, os.TempDir()) {
		t.Fatalf("lab root not under os.TempDir: %s", l.Root)
	}
	if !IsLocalRemoteURL(l.Remote) {
		t.Fatalf("lab remote URL not local: %s", l.Remote)
	}
	if _, err := os.Stat(l.Remote + "/HEAD"); err != nil {
		t.Fatalf("bare remote HEAD missing: %v", err)
	}
	if _, err := os.Stat(l.JJClient); err != nil {
		t.Fatalf("jj client missing: %v", err)
	}
	if _, err := os.Stat(l.GitClient); err != nil {
		t.Fatalf("git client missing: %v", err)
	}
}

// TestSeedConflictedCommit_MaterialisesConflict verifies that
// SeedConflictedCommit produces a commit whose `conflict` flag
// is true (per jj 0.41.0's commit-level conflict detection,
// exposed through `conflict` in `jj log -T`).
func TestSeedConflictedCommit_MaterialisesConflict(t *testing.T) {
	RequireGitAndJJ(t)
	ctx := context.Background()
	l, err := Setup(ctx)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	defer l.Cleanup()

	_, _, err = l.SeedConflictedCommit(ctx)
	if err != nil {
		t.Fatalf("SeedConflictedCommit: %v", err)
	}
	out, err := l.JJOutput(ctx, l.JJClient, []string{
		"log", "--no-graph", "-r", "@", "-T", "conflict",
	})
	if err != nil {
		t.Fatalf("jj log: %v", err)
	}
	if !strings.Contains(out, "true") {
		t.Fatalf("expected conflict=true on @; got %q", out)
	}
}
