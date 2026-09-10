package lab

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestWriteEvidenceToIgnoredArtifact is an optional convenience that
// writes the lab's evidence record to ./build/bjj-evidence.json. It is
// gated behind the BJJ_WRITE_EVIDENCE env var so a regular `go test`
// run does not create ephemeral files in the repository tree.
//
// The artifact location is excluded by .gitignore.
func TestWriteEvidenceToIgnoredArtifact(t *testing.T) {
	if os.Getenv("BJJ_WRITE_EVIDENCE") == "" {
		t.Skip("set BJJ_WRITE_EVIDENCE=1 to persist evidence to ./build/bjj-evidence.json")
	}
	// Once the feature flag is on, missing required tools is a hard
	// failure -- not a silent skip.
	RequireGitAndJJ(t)

	_, ev, err := Run(context.Background())
	if err != nil {
		t.Fatalf("lab Run: %v (failure_reason=%q)", err, ev.FailureReason)
	}

	b, err := EvidenceJSON(ev)
	if err != nil {
		t.Fatalf("EvidenceJSON: %v", err)
	}

	// Locate the project root: the parent directory of the current
	// working directory's "go.mod" ancestor. Since the test runs from
	// the package directory, we walk up looking for go.mod.
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	root := cwd
	for {
		if _, err := os.Stat(filepath.Join(root, "go.mod")); err == nil {
			break
		}
		parent := filepath.Dir(root)
		if parent == root {
			t.Fatalf("could not locate project root (no go.mod above %s)", cwd)
		}
		root = parent
	}
	out := filepath.Join(root, "build", "bjj-evidence.json")
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		t.Fatalf("mkdir build: %v", err)
	}
	if err := os.WriteFile(out, b, 0o644); err != nil {
		t.Fatalf("write evidence: %v", err)
	}
	t.Logf("wrote evidence to %s", out)

	// Sanity-check the persisted file is strict JSON.
	if !strings.HasPrefix(string(b), "{") {
		if len(b) < 64 {
			t.Fatalf("evidence does not start with `{`: %q", string(b))
		}
		t.Fatalf("evidence does not start with `{`: %q", string(b)[:64])
	}
}
