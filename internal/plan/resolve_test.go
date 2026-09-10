package plan_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/s1onique/bjj/internal/jjadapter"
	"github.com/s1onique/bjj/internal/lab"
	"github.com/s1onique/bjj/internal/plan"
)

// planFromLab resolves a plan against a freshly-built lab.
func planFromLab(t *testing.T, l *lab.Lab, bookmark string) (*plan.PublishPlan, error) {
	t.Helper()
	src := plan.NewJJSource(jjadapter.New())
	return plan.Resolve(context.Background(), src, plan.ResolveOptions{
		Dir:      l.JJClient,
		Remote:   "lab",
		Bookmark: bookmark,
	})
}

// planObservedFromLab resolves a plan with its observation envelope.
func planObservedFromLab(t *testing.T, l *lab.Lab, bookmark string) (*plan.PlanObservation, error) {
	t.Helper()
	src := plan.NewJJSource(jjadapter.New())
	return plan.ResolveObserved(context.Background(), src, plan.ResolveOptions{
		Dir:      l.JJClient,
		Remote:   "lab",
		Bookmark: bookmark,
	})
}

// planCode returns the typed plan error code, or "" if err is nil.
func planCode(err error) plan.ErrorCode {
	if err == nil {
		return ""
	}
	if pe, ok := plan.AsError(err); ok {
		return pe.Code
	}
	return ""
}

// CASE A — new remote bookmark.

func TestPlan_NewBookmark_Resolves_Absent_To_New(t *testing.T) {
	requirePlanPrereqs(t)
	ctx := context.Background()
	l, err := lab.Setup(ctx)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	defer l.Cleanup()

	if _, err := l.GitOutput(ctx, l.Remote, []string{"rev-parse", "refs/heads/feature"}); err == nil {
		t.Fatalf("expected refs/heads/feature absent before plan")
	}

	p, err := planFromLab(t, l, "feature")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if p.Status != plan.StatusPlanned {
		t.Fatalf("status=%s, want planned", p.Status)
	}
	if p.Remote.Name != "lab" {
		t.Fatalf("remote.name=%q, want lab", p.Remote.Name)
	}
	if len(p.BookmarkMoves) != 1 {
		t.Fatalf("expected 1 bookmark move, got %d", len(p.BookmarkMoves))
	}
	mv := p.BookmarkMoves[0]
	if mv.Name != "feature" {
		t.Fatalf("move.name=%q, want feature", mv.Name)
	}
	if mv.Old != nil {
		t.Fatalf("expected move.old == nil for new bookmark, got %+v", mv.Old)
	}
	if mv.New == nil {
		t.Fatalf("expected move.new non-nil")
	}
	if mv.New.CommitID == "" || mv.New.ChangeID == "" {
		t.Fatalf("move.new missing ids: %+v", mv.New)
	}
	if len(p.Commits) == 0 {
		t.Fatalf("expected at least one outgoing commit")
	}
	for _, c := range p.Commits {
		if c.CommitID == "" || c.ChangeID == "" {
			t.Fatalf("commit missing ids: %+v", c)
		}
	}
}

// CASE B — fast-forward candidate.

func TestPlan_FastForward_Resolves_Old_To_New(t *testing.T) {
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

	if err := l.RunJJ(ctx, []string{"new", "feature", "-m", "ff C2"}); err != nil {
		t.Fatalf("jj new: %v", err)
	}
	if err := os.WriteFile(filepath.Join(l.JJClient, "feature2.txt"), []byte("ff\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := l.RunJJ(ctx, []string{"bookmark", "set", "feature", "-r", "@"}); err != nil {
		t.Fatalf("jj bookmark set: %v", err)
	}

	p, err := planFromLab(t, l, "feature")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if p.Status != plan.StatusPlanned {
		t.Fatalf("status=%s, want planned", p.Status)
	}
	mv := p.BookmarkMoves[0]
	if mv.Old == nil {
		t.Fatalf("expected move.old non-nil for fast-forward")
	}
	if mv.New == nil || mv.New.CommitID == mv.Old.CommitID {
		t.Fatalf("expected different new commit: old=%s new=%v", mv.Old.CommitID, mv.New)
	}
	if len(p.Commits) == 0 {
		t.Fatalf("expected outgoing commits for fast-forward")
	}
}

// CASE C — no-op.

func TestPlan_NoOp_Detected(t *testing.T) {
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

	p, err := planFromLab(t, l, "feature")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if p.Status != plan.StatusNoRemoteChange {
		t.Fatalf("status=%s, want no_remote_change", p.Status)
	}
	mv := p.BookmarkMoves[0]
	if mv.Old == nil || mv.New == nil {
		t.Fatalf("expected both old and new for no-op")
	}
	if mv.Old.CommitID != mv.New.CommitID {
		t.Fatalf("no-op must have equal ids: old=%s new=%s", mv.Old.CommitID, mv.New.CommitID)
	}
}

// CASE D — missing local bookmark.

func TestPlan_Missing_Local_Bookmark_Typed_Failure(t *testing.T) {
	requirePlanPrereqs(t)
	ctx := context.Background()
	l, err := lab.Setup(ctx)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	defer l.Cleanup()

	_, err = planFromLab(t, l, "nonexistent")
	if err == nil {
		t.Fatalf("expected error for missing local bookmark")
	}
	if planCode(err) != plan.CodeLocalBookmarkNotFound {
		t.Fatalf("code=%s, want LOCAL_BOOKMARK_NOT_FOUND; err=%v", planCode(err), err)
	}
}

// CASE E — missing remote.

func TestPlan_Missing_Remote_Typed_Failure(t *testing.T) {
	requirePlanPrereqs(t)
	ctx := context.Background()
	l, err := lab.Setup(ctx)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	defer l.Cleanup()

	src := plan.NewJJSource(jjadapter.New())
	_, err = plan.Resolve(context.Background(), src, plan.ResolveOptions{
		Dir: l.JJClient, Remote: "wat", Bookmark: "feature",
	})
	if err == nil {
		t.Fatalf("expected error for missing remote")
	}
	if planCode(err) != plan.CodeRemoteNotFound {
		t.Fatalf("code=%s, want REMOTE_NOT_FOUND; err=%v", planCode(err), err)
	}
}

// CASE F — conflicted local bookmark.

func TestPlan_Local_Bookmark_Conflicted_Typed_Failure(t *testing.T) {
	requirePlanPrereqs(t)
	ctx := context.Background()
	l, err := lab.Setup(ctx)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	defer l.Cleanup()

	if err := l.SeedConflictedBookmark(ctx, "conflict-test"); err != nil {
		t.Fatalf("seed conflict: %v", err)
	}

	_, err = planFromLab(t, l, "conflict-test")
	if err == nil {
		t.Skipf("local bookmark not observed as conflicted on 0.41.0; ACT §6 CASE F")
		return
	}
	if planCode(err) != plan.CodeLocalBookmarkConflicted {
		t.Fatalf("code=%s, want LOCAL_BOOKMARK_CONFLICTED; err=%v", planCode(err), err)
	}
}

// CASE H — rewritten local change separates identities.

func TestPlan_Rewritten_Local_Change_Separates_Identities(t *testing.T) {
	requirePlanPrereqs(t)
	ctx := context.Background()
	l, err := lab.Setup(ctx)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	defer l.Cleanup()

	changeID, oldCommitID, newCommitID, err := l.SeedRewrittenFeature(ctx, "main")
	if err != nil {
		t.Fatalf("seed rewrite: %v", err)
	}
	if changeID == "" || oldCommitID == "" || newCommitID == "" {
		t.Fatalf("seed returned empty ids: %s %s %s", changeID, oldCommitID, newCommitID)
	}
	if oldCommitID == newCommitID {
		t.Fatalf("seed did not rewrite commit id (both %s)", oldCommitID)
	}

	// Move the existing `feature` bookmark to the rewritten commit.
	if err := l.RunJJ(ctx, []string{"bookmark", "set", "feature", "-r", "@", "--allow-backwards"}); err != nil {
		t.Fatalf("set feature: %v", err)
	}

	p, err := planFromLab(t, l, "feature")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if p.BookmarkMoves[0].New.ChangeID != changeID {
		t.Fatalf("plan change_id=%s, want %s",
			p.BookmarkMoves[0].New.ChangeID, changeID)
	}
	if p.BookmarkMoves[0].New.CommitID != newCommitID {
		t.Fatalf("plan commit_id=%s, want %s (post-rewrite)",
			p.BookmarkMoves[0].New.CommitID, newCommitID)
	}
}

// Determinism.

func TestPlan_Same_State_Resolves_Identical_Plan(t *testing.T) {
	requirePlanPrereqs(t)
	ctx := context.Background()
	l, err := lab.Setup(ctx)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	defer l.Cleanup()

	p1, err := planFromLab(t, l, "feature")
	if err != nil {
		t.Fatalf("resolve #1: %v", err)
	}
	p2, err := planFromLab(t, l, "feature")
	if err != nil {
		t.Fatalf("resolve #2: %v", err)
	}

	b1, err := plan.RenderJSON(p1)
	if err != nil {
		t.Fatalf("render #1: %v", err)
	}
	b2, err := plan.RenderJSON(p2)
	if err != nil {
		t.Fatalf("render #2: %v", err)
	}
	// PublishPlan body is the canonical subject and contains NO
	// observation metadata. Two identical observations of the same
	// state MUST produce byte-identical JSON.
	if string(b1) != string(b2) {
		t.Fatalf("plans diverge:\n---\n%s\n---\n%s\n", b1, b2)
	}
}

// Multiple outgoing commits, canonical order.

func TestPlan_Multiple_Commits_Canonical_Order(t *testing.T) {
	requirePlanPrereqs(t)
	ctx := context.Background()
	l, err := lab.Setup(ctx)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	defer l.Cleanup()

	if _, _, err := l.SeedCreateStackedCommits(ctx, "main", "stack", 3); err != nil {
		t.Fatalf("seed stack: %v", err)
	}

	p, err := planFromLab(t, l, "stack")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	// Expect exactly the 3 new stack commits (main excluded as
	// nearest remote-tracked ancestor).
	if len(p.Commits) != 3 {
		t.Fatalf("expected 3 outgoing commits, got %d (%v)", len(p.Commits), p.Commits)
	}
	for i := 1; i < len(p.Commits); i++ {
		if !(p.Commits[i-1].CommitID < p.Commits[i].CommitID) {
			t.Fatalf("commits not in canonical lex order: %v", p.Commits)
		}
	}
}

// Safety: plan must not mutate the remote.

func TestPlan_DoesNotMutateRemote(t *testing.T) {
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

	beforeRefs, err := l.GitOutput(ctx, l.Remote, []string{"for-each-ref", "--format=%(refname) %(objectname)"})
	if err != nil {
		t.Fatalf("read refs before: %v", err)
	}

	if _, err := planFromLab(t, l, "feature"); err != nil {
		t.Fatalf("resolve: %v", err)
	}

	afterRefs, err := l.GitOutput(ctx, l.Remote, []string{"for-each-ref", "--format=%(refname) %(objectname)"})
	if err != nil {
		t.Fatalf("read refs after: %v", err)
	}

	if beforeRefs != afterRefs {
		t.Fatalf("plan mutated remote:\nbefore:\n%s\nafter:\n%s", beforeRefs, afterRefs)
	}
}

// Single-view consistency: mutating local between resolves produces
// distinct plans that reflect the *current* state.

func TestPlan_SingleViewConsistency_MutateBetweenResolves(t *testing.T) {
	requirePlanPrereqs(t)
	ctx := context.Background()
	l, err := lab.Setup(ctx)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	defer l.Cleanup()

	p1, err := planFromLab(t, l, "feature")
	if err != nil {
		t.Fatalf("resolve #1: %v", err)
	}
	if p1.Status != plan.StatusPlanned || p1.BookmarkMoves[0].Old != nil {
		t.Fatalf("plan #1 wrong: %+v", p1)
	}
	firstNew := p1.BookmarkMoves[0].New.CommitID

	if err := l.RunJJ(ctx, []string{"new", "feature", "-m", "ff C2"}); err != nil {
		t.Fatalf("jj new: %v", err)
	}
	if err := os.WriteFile(filepath.Join(l.JJClient, "ff2.txt"), []byte("ff2\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := l.RunJJ(ctx, []string{"bookmark", "set", "feature", "-r", "@"}); err != nil {
		t.Fatalf("jj bookmark set: %v", err)
	}

	p2, err := planFromLab(t, l, "feature")
	if err != nil {
		t.Fatalf("resolve #2: %v", err)
	}
	if p2.BookmarkMoves[0].New.CommitID == firstNew {
		t.Fatalf("plan #2 should reflect new commit; got same %s", firstNew)
	}
	if p2.BookmarkMoves[0].Old != nil {
		t.Fatalf("plan #2 old should still be nil (no push yet); got %+v", p2.BookmarkMoves[0].Old)
	}

	// Operation ids are observation metadata, not part of the
	// canonical subject. Use ResolveObserved for both resolves
	// and verify the observation ids differ across mutations.
	obs1, err := planObservedFromLab(t, l, "feature")
	if err != nil {
		t.Fatalf("observe #1: %v", err)
	}
	// Capture the first observation's id, then mutate and observe
	// again to ensure mutation is reflected in the observation id.
	firstOp := obs1.SourceOperationID

	if err := l.RunJJ(ctx, []string{"new", "feature", "-m", "obs mutation"}); err != nil {
		t.Fatalf("jj new: %v", err)
	}

	obs2, err := planObservedFromLab(t, l, "feature")
	if err != nil {
		t.Fatalf("observe #2: %v", err)
	}
	if obs1.SourceOperationID == obs2.SourceOperationID {
		_ = firstOp
		t.Fatalf("operation ids should differ across mutations; both %s", obs1.SourceOperationID)
	}
}

// Stale-known-remote-state by design:
// PLAN_REMOTE_BASELINE = LOCAL_KNOWN_REMOTE_STATE.

func TestPlan_StaleKnownRemoteState_Describes_LocalKnownState(t *testing.T) {
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
	oldRemoteSHA, err := l.GitOutput(ctx, l.Remote, []string{"rev-parse", "refs/heads/feature"})
	if err != nil {
		t.Fatalf("read remote feature: %v", err)
	}

	mainSHA, err := l.GitOutput(ctx, l.Remote, []string{"rev-parse", "refs/heads/main"})
	if err != nil {
		t.Fatalf("read remote main: %v", err)
	}
	if mainSHA == oldRemoteSHA {
		t.Fatalf("remote main and feature are the same; cannot fabricate divergence: %s", mainSHA)
	}
	if err := l.SeedExternalRemoteMove(ctx, "feature", mainSHA); err != nil {
		t.Fatalf("external move: %v", err)
	}
	newRemoteSHA, _ := l.GitOutput(ctx, l.Remote, []string{"rev-parse", "refs/heads/feature"})
	if newRemoteSHA == oldRemoteSHA {
		t.Fatalf("external move did not change remote refs/heads/feature (still %s)", newRemoteSHA)
	}

	p, err := planFromLab(t, l, "feature")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if p.BookmarkMoves[0].Old == nil {
		t.Fatalf("expected old non-nil (planning client knows pre-mutation state)")
	}
	if p.BookmarkMoves[0].Old.CommitID != oldRemoteSHA {
		t.Fatalf("plan old=%s, want %s (local-known remote state)",
			p.BookmarkMoves[0].Old.CommitID, oldRemoteSHA)
	}
}

// JSON round-trip is stable.

func TestPlan_JSON_RoundTrip_Stable(t *testing.T) {
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
	var got plan.PublishPlan
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v\nbody:\n%s", err, b)
	}
	if got.SchemaVersion != plan.SchemaVersion {
		t.Fatalf("schema=%d, want %d", got.SchemaVersion, plan.SchemaVersion)
	}
	if got.Status != p.Status {
		t.Fatalf("status mismatch: %s vs %s", got.Status, p.Status)
	}
	if len(got.BookmarkMoves) != 1 || got.BookmarkMoves[0].Name != p.BookmarkMoves[0].Name {
		t.Fatalf("bookmark moves mismatch")
	}
}

// Text rendering does not include absolute paths.

func TestPlan_TextRendering_NoPaths(t *testing.T) {
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
	var sb strings.Builder
	if err := plan.RenderText(p, &sb); err != nil {
		t.Fatalf("render text: %v", err)
	}
	out := sb.String()
	if strings.Contains(out, l.JJClient) {
		t.Fatalf("text rendering leaked absolute path: %s", out)
	}
	if strings.Contains(out, l.Remote) {
		t.Fatalf("text rendering leaked remote path: %s", out)
	}
}

// Typed error: AsError unwrapping.

func TestPlan_AsError_UnwrapsTypedError(t *testing.T) {
	requirePlanPrereqs(t)
	ctx := context.Background()
	l, err := lab.Setup(ctx)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	defer l.Cleanup()

	_, err = planFromLab(t, l, "no-such-bookmark")
	if err == nil {
		t.Fatalf("expected error")
	}
	pe, ok := plan.AsError(err)
	if !ok {
		t.Fatalf("expected *plan.Error, got %T (%v)", err, err)
	}
	if pe.Code != plan.CodeLocalBookmarkNotFound {
		t.Fatalf("code=%s, want LOCAL_BOOKMARK_NOT_FOUND", pe.Code)
	}
}

// JSON contract: schema_version and top-level keys are present.

func TestPlan_JSON_Schema_Keys(t *testing.T) {
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
	for _, want := range []string{
		`"schema_version": 1`,
		`"status"`,
		`"remote"`,
		`"bookmark_moves"`,
		`"commits"`,
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("JSON missing %s; got:\n%s", want, s)
		}
	}
}

// IDs are full length (40 hex for git commit, 32 hex for jj change).

func TestPlan_IDs_AreFullLength(t *testing.T) {
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
	for _, c := range p.Commits {
		if len(c.CommitID) != 40 {
			t.Fatalf("commit_id not full length: %q (len=%d)", c.CommitID, len(c.CommitID))
		}
		if len(c.ChangeID) != 32 {
			t.Fatalf("change_id not full length: %q (len=%d)", c.ChangeID, len(c.ChangeID))
		}
	}
	if p.BookmarkMoves[0].New != nil {
		if len(p.BookmarkMoves[0].New.CommitID) != 40 {
			t.Fatalf("move.new.commit_id not full length: %q", p.BookmarkMoves[0].New.CommitID)
		}
		if len(p.BookmarkMoves[0].New.ChangeID) != 32 {
			t.Fatalf("move.new.change_id not full length: %q", p.BookmarkMoves[0].New.ChangeID)
		}
	}
}
