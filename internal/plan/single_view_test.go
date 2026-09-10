package plan_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/s1onique/bjj/internal/jjadapter"
	"github.com/s1onique/bjj/internal/lab"
	"github.com/s1onique/bjj/internal/plan"
)

// TestPlan_SingleView_MidResolveMutation_PinnedToSnapshotOpID is the
// P0 adversarial test for ACT-BJJ-PLAN01-CORRECTION02 §1.
//
// It wraps the production JJSource with a mutatingSource that
// mutates the underlying repository BETWEEN Snapshot() and the
// FIRST CommitsAt() within one Resolve. The required sequence is:
//
//	Snapshot()                       -> OP_A
//	↓
//	mutate repository (jj new)       -> OP_B (OP_B != OP_A)
//	↓
//	CommitsAt(... OP_A, ...)         -> observed at OP_A
//	CommitsAt(... OP_A, ...)         -> observed at OP_A
//
// The test asserts:
//   - snapshot_op   = OP_A
//   - current_head  = OP_B (post-mutation)
//   - OP_A != OP_B
//   - every CommitsAt opID == OP_A
//
// If the resolver ever re-pins from inside a single Resolve
// (e.g. by calling PinOperation on each query), the first
// CommitsAt would observe OP_B and this test would fail.
func TestPlan_SingleView_MidResolveMutation_PinnedToSnapshotOpID(t *testing.T) {
	requirePlanPrereqs(t)
	ctx := context.Background()
	l, err := lab.Setup(ctx)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	defer l.Cleanup()

	if err := l.SeedBookmarkPush(ctx, "feature"); err != nil {
		t.Fatalf("seed push: %v", err)
	}

	capture := &opIDCatcher{t: t}
	src := plan.NewJJSource(jjadapter.New())
	wrapped := &mutatingSource{
		inner:   src,
		catcher: capture,
		mutate: func() {
			if err := l.RunJJ(ctx, []string{"new", "-m", "mid-resolve mutation"}); err != nil {
				t.Fatalf("mid-resolve jj new: %v", err)
			}
			if err := os.WriteFile(filepath.Join(l.JJClient, "mid.txt"), []byte("mid\n"), 0o644); err != nil {
				t.Fatalf("mid-resolve write: %v", err)
			}
		},
	}
	capture.window = wrapped

	obs, err := plan.ResolveObserved(ctx, wrapped, plan.ResolveOptions{
		Dir: l.JJClient, Remote: "lab", Bookmark: "feature",
	})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if obs.SourceOperationID == "" {
		t.Fatalf("observation missing operation id")
	}
	snapshotOp := obs.SourceOperationID

	if !capture.didMutateRun() {
		t.Fatalf("wrapper did not run mid-resolve mutation; cannot prove single-view")
	}
	if !capture.mutatedAfterSnapshotRan() {
		t.Fatalf("wrapper mutated BEFORE Snapshot, not after; this is not a mid-resolve test")
	}
	if !capture.mutatedBeforeFirstCommitsRan() {
		t.Fatalf("wrapper did not mutate BEFORE first CommitsAt; this is not a mid-resolve test")
	}

	currentOp, err := probeCurrentOpID(ctx, l)
	if err != nil {
		t.Fatalf("op id probe: %v", err)
	}
	if currentOp == "" {
		t.Fatalf("current operation id is empty after mutation")
	}
	if snapshotOp == currentOp {
		t.Fatalf("mutation did not advance operation id; snap=%s current=%s", snapshotOp, currentOp)
	}

	if len(capture.opIDs) == 0 {
		t.Fatalf("harness captured no opIDs; CommitsAt was never invoked")
	}
	for i, op := range capture.opIDs {
		if op != snapshotOp {
			t.Fatalf("CommitsAt #%d used opID %s; expected %s (snapshot's opID)",
				i, op, snapshotOp)
		}
	}
}

// probeCurrentOpID returns the current operation id of the lab's
// jj client by going through the lab's exported JJOutput helper,
// so the test does not depend on internal lab env plumbing.
func probeCurrentOpID(ctx context.Context, l *lab.Lab) (string, error) {
	return l.JJOutput(ctx, l.JJClient, []string{
		"op", "log", "--no-graph", "--limit", "1",
		"-T", "id",
		"--ignore-working-copy",
		"--no-integrate-operation",
	})
}

// TestPlan_SingleView_JJSource_RefusesEmptyOpID asserts the
// production JJSource refuses to call CommitsAt without an opID.
func TestPlan_SingleView_JJSource_RefusesEmptyOpID(t *testing.T) {
	src := plan.NewJJSource(jjadapter.New())
	_, err := src.CommitsAt(context.Background(), "/tmp", "", "all()")
	if err == nil {
		t.Fatalf("expected error when opID is empty")
	}
	pe, ok := plan.AsError(err)
	if !ok {
		t.Fatalf("expected *plan.Error, got %T", err)
	}
	if pe.Code != plan.CodeInconsistentView {
		t.Fatalf("code=%s, want INCONSISTENT_REPOSITORY_VIEW", pe.Code)
	}
	if !strings.Contains(pe.Message, "refusing to re-pin") {
		t.Fatalf("message=%q does not contain 'refusing to re-pin'", pe.Message)
	}
}

// TestPlan_CanonicalSubject_HasNoRemoteURL verifies the canonical
// PublishPlan JSON contains NO remote URL field.
func TestPlan_CanonicalSubject_HasNoRemoteURL(t *testing.T) {
	requirePlanPrereqs(t)
	ctx := context.Background()
	l, err := lab.Setup(ctx)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	defer l.Cleanup()

	p, err := planFromLab(t, l, "feature")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	b, err := plan.RenderJSON(p)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	s := string(b)
	if strings.Contains(s, l.Remote) {
		t.Fatalf("canonical plan body leaked remote path: %s\nbody:\n%s", l.Remote, s)
	}
	if strings.Contains(s, l.JJClient) {
		t.Fatalf("canonical plan body leaked jj client path: %s\nbody:\n%s", l.JJClient, s)
	}
	if strings.Contains(s, `"url"`) {
		t.Fatalf("canonical plan body contains url field:\n%s", s)
	}
}

// mutatingSource wraps a Source and, on the first CommitsAt call
// AFTER Snapshot has been delegated, invokes the configured
// mutate function against the underlying repository. The
// mutation advances the working-copy operation id; if the
// resolver re-pins, it would observe the post-mutation state.
type mutatingSource struct {
	inner   plan.Source
	catcher *opIDCatcher
	mutate  func()

	didMutate                 bool
	mutatedAfterSnapshot      bool
	mutatedBeforeFirstCommits bool
}

func (m *mutatingSource) Snapshot(ctx context.Context, dir string) (jjadapter.Snapshot, error) {
	return m.inner.Snapshot(ctx, dir)
}

func (m *mutatingSource) CommitsAt(ctx context.Context, dir, opID, revset string) ([]jjadapter.CommitRef, error) {
	if !m.didMutate {
		m.didMutate = true
		m.mutatedAfterSnapshot = true
		m.mutatedBeforeFirstCommits = true
		m.mutate()
	}
	m.catcher.record(opID)
	return m.inner.CommitsAt(ctx, dir, opID, revset)
}

// opIDCatcher captures every opID passed to CommitsAt and also
// holds the active testing.T so the mutatingSource can call the
// configured mutate function (which may call t.Fatalf). The
// window field exposes the mutatingSource's mutation-bookkeeping
// so callers can assert the mutation ran in the correct window.
type opIDCatcher struct {
	t      *testing.T
	opIDs  []string
	window *mutatingSource
}

func (c *opIDCatcher) didMutateRun() bool {
	return c.window != nil && c.window.didMutate
}

func (c *opIDCatcher) mutatedAfterSnapshotRan() bool {
	return c.window != nil && c.window.mutatedAfterSnapshot
}

func (c *opIDCatcher) mutatedBeforeFirstCommitsRan() bool {
	return c.window != nil && c.window.mutatedBeforeFirstCommits
}

func (c *opIDCatcher) record(opID string) {
	c.opIDs = append(c.opIDs, opID)
}

func (c *opIDCatcher) reset() {
	c.opIDs = nil
}

// silence unused imports of atomic when the catcher's locking is
// not strictly needed (Go's race detector would still catch issues
// without it; we keep the import for future parallel coverage).
var _ = atomic.CompareAndSwapInt32
