package admission_test

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/s1onique/bjj/internal/admission"
	"github.com/s1onique/bjj/internal/jjadapter"
	"github.com/s1onique/bjj/internal/lab"
	"github.com/s1onique/bjj/internal/plan"
)

// requireAdmissionPrereqs is the central gate: every test in
// this file requires the same tooling as the plan tests.
func requireAdmissionPrereqs(t *testing.T) {
	t.Helper()
	for _, tool := range []string{"git", "jj"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Fatalf("required tool %q missing: %v", tool, err)
		}
	}
}

// adapterSourceForTest wraps the production adapter for the
// gatherer. It exists in tests so production admission does not
// gain a dependency on the cmd/bjj package.
type adapterSourceForTest struct {
	a *jjadapter.Adapter
}

func (s *adapterSourceForTest) ListCommits(ctx context.Context, dir, opID, revset string) ([]jjadapter.CommitRef, error) {
	return s.a.ListCommits(ctx, dir, opID, revset)
}

func (s *adapterSourceForTest) ListCommitObs(ctx context.Context, dir, opID, revset string) ([]jjadapter.CommitObs, error) {
	return s.a.ListCommitObs(ctx, dir, opID, revset)
}

// planViewForTest is a test-only adapter from *plan.PublishPlan
// to admission.PlanView.
//
// CORRECTION01 (P1): the production concrete adapter moved to
// cmd/bjj/plan_adapter.go so admission no longer imports
// internal/plan. This test file lives in package admission_test
// and IS allowed to import internal/plan (only production
// admission code is constrained), so it can construct the
// adapter locally.
type planViewForTest struct {
	p *plan.PublishPlan
}

func (a *planViewForTest) RemoteName() string { return a.p.Remote.Name }
func (a *planViewForTest) BookmarkName() string {
	if len(a.p.BookmarkMoves) == 0 {
		return ""
	}
	return a.p.BookmarkMoves[0].Name
}
func (a *planViewForTest) OldCommitID() string {
	mv := a.p.BookmarkMoves[0]
	if mv.Old == nil {
		return ""
	}
	return mv.Old.CommitID
}
func (a *planViewForTest) NewCommitID() string {
	mv := a.p.BookmarkMoves[0]
	if mv.New == nil {
		return ""
	}
	return mv.New.CommitID
}
func (a *planViewForTest) StatusName() string { return string(a.p.Status) }
func (a *planViewForTest) OutgoingCommits() []admission.CommitRef {
	out := make([]admission.CommitRef, 0, len(a.p.Commits))
	for _, c := range a.p.Commits {
		out = append(out, admission.CommitRef{
			CommitID: c.CommitID,
			ChangeID: c.ChangeID,
		})
	}
	return out
}

func mustWrapPlanTest(p *plan.PublishPlan) admission.PlanView {
	if p == nil || len(p.BookmarkMoves) == 0 {
		panic("mustWrapPlanTest: empty plan")
	}
	return &planViewForTest{p: p}
}

// admitLabFixture constructs a fresh lab with the standard
// candidate (feature bookmark on a fresh C1 commit).
func admitLabFixture(t *testing.T) (*lab.Lab, *plan.PlanObservation) {
	t.Helper()
	requireAdmissionPrereqs(t)
	ctx := context.Background()
	l, err := lab.Setup(ctx)
	if err != nil {
		t.Fatalf("lab.Setup: %v", err)
	}
	t.Cleanup(l.Cleanup)
	src := plan.NewJJSource(jjadapter.New())
	obs, err := plan.ResolveObserved(ctx, src, plan.ResolveOptions{
		Dir:      l.JJClient,
		Remote:   "lab",
		Bookmark: "feature",
	})
	if err != nil {
		t.Fatalf("ResolveObserved: %v", err)
	}
	return l, obs
}

// gatherInLab gathers admission facts for a real lab observation.
//
// CORRECTION03: the policy is read from the frozen candidate
// tree via `jj file show -r NEW --at-op=<opID>`, NOT from the
// live filesystem. Tests that leave .bjj/policy.toml in the
// working copy without committing must either commit it via
// `jj describe` or use `lab.SeedPolicyInCandidate` to roll the
// policy into the candidate's tree before resolving the plan.
func gatherInLab(t *testing.T, l *lab.Lab, obs *plan.PlanObservation) admission.AdmissionFacts {
	t.Helper()
	a := jjadapter.New()
	pol, _, err := admission.LoadPolicyAt(
		context.Background(),
		&adapterPolicyReaderForTest{a: a},
		mustWrapPlanTest(obs.Plan),
		l.JJClient,
		obs.SourceOperationID,
	)
	if err != nil {
		t.Fatalf("LoadPolicyAt: %v", err)
	}
	g := admission.NewGatherer(&adapterSourceForTest{a: a})
	pv := mustWrapPlanTest(obs.Plan)
	facts, err := g.Gather(context.Background(), pv, pol, admission.GatherOptions{
		Dir:  l.JJClient,
		OpID: obs.SourceOperationID,
	})
	if err != nil {
		t.Fatalf("Gather: %v", err)
	}
	return facts
}

// adapterPolicyReaderForTest satisfies admission.PolicyReader on
// top of *jjadapter.Adapter for tests.
type adapterPolicyReaderForTest struct {
	a *jjadapter.Adapter
}

func (r *adapterPolicyReaderForTest) ReadPolicyFile(ctx context.Context, dir, opID, revision, path string) ([]byte, admission.PolicyFilePresence, error) {
	body, presence, err := r.a.FileShowAtOp(ctx, dir, opID, revision, path)
	if err != nil {
		return nil, "", err
	}
	switch presence {
	case jjadapter.FilePresencePresent:
		return body, admission.PolicyFilePresencePresent, nil
	case jjadapter.FilePresenceAbsent:
		return nil, admission.PolicyFilePresenceAbsent, nil
	default:
		return nil, "", &jjadapter.ErrJJFailed{
			Program:  r.a.JJ,
			Argv:     []string{"file", "show", "-r", revision, "--at-op=" + opID, path},
			ExitCode: -1,
			Stderr:   "unknown presence",
		}
	}
}

// TestAdmission_CreateDefaultAdmit verifies §25: a new-bookmark
// subject is admitted by default under the no-policy case.
func TestAdmission_CreateDefaultAdmit(t *testing.T) {
	l, obs := admitLabFixture(t)
	if obs.Plan.Status != plan.StatusPlanned {
		t.Fatalf("plan status=%s, want planned", obs.Plan.Status)
	}
	facts := gatherInLab(t, l, obs)
	if facts.MoveRelation != admission.MoveCreate {
		t.Fatalf("MoveRelation=%s, want CREATE", facts.MoveRelation)
	}
	pol, _, _ := admission.LoadPolicyFromRepo(l.JJClient)
	pv := mustWrapPlanTest(obs.Plan)
	res, err := admission.Evaluate(admission.AdmissionInput{
		Plan: pv, Facts: facts, Policy: pol,
	})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if res.Decision != admission.DecisionAdmit {
		t.Fatalf("Decision=%s, want admit; reasons=%+v", res.Decision, res.Reasons)
	}
}

// TestAdmission_FastForwardAdmit verifies §7: a FF subject is
// admitted by default.
func TestAdmission_FastForwardAdmit(t *testing.T) {
	requireAdmissionPrereqs(t)
	ctx := context.Background()
	l, err := lab.Setup(ctx)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	defer l.Cleanup()
	if err := l.SeedBookmarkPush(ctx, "feature"); err != nil {
		t.Fatalf("SeedBookmarkPush: %v", err)
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
	src := plan.NewJJSource(jjadapter.New())
	obs, err := plan.ResolveObserved(ctx, src, plan.ResolveOptions{
		Dir: l.JJClient, Remote: "lab", Bookmark: "feature",
	})
	if err != nil {
		t.Fatalf("ResolveObserved: %v", err)
	}
	facts := gatherInLab(t, l, obs)
	if facts.MoveRelation != admission.MoveFastForward {
		t.Fatalf("MoveRelation=%s, want FAST_FORWARD", facts.MoveRelation)
	}
	pol, _, _ := admission.LoadPolicyFromRepo(l.JJClient)
	pv := mustWrapPlanTest(obs.Plan)
	res, err := admission.Evaluate(admission.AdmissionInput{
		Plan: pv, Facts: facts, Policy: pol,
	})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if res.Decision != admission.DecisionAdmit {
		t.Fatalf("Decision=%s, want admit; reasons=%+v", res.Decision, res.Reasons)
	}
}

// TestAdmission_NoRemoteChange_NotNeeded verifies §13: a no-op
// subject is short-circuited to DecisionNotNeeded.
func TestAdmission_NoRemoteChange_NotNeeded(t *testing.T) {
	requireAdmissionPrereqs(t)
	ctx := context.Background()
	l, err := lab.Setup(ctx)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	defer l.Cleanup()
	if err := l.SeedBookmarkPush(ctx, "feature"); err != nil {
		t.Fatalf("SeedBookmarkPush: %v", err)
	}
	src := plan.NewJJSource(jjadapter.New())
	obs, err := plan.ResolveObserved(ctx, src, plan.ResolveOptions{
		Dir: l.JJClient, Remote: "lab", Bookmark: "feature",
	})
	if err != nil {
		t.Fatalf("ResolveObserved: %v", err)
	}
	if obs.Plan.Status != plan.StatusNoRemoteChange {
		t.Fatalf("plan status=%s, want no_remote_change", obs.Plan.Status)
	}
	facts := gatherInLab(t, l, obs)
	if facts.MoveRelation != admission.MoveNoChange {
		t.Fatalf("MoveRelation=%s, want NO_CHANGE", facts.MoveRelation)
	}
	pol, _, _ := admission.LoadPolicyFromRepo(l.JJClient)
	pv := mustWrapPlanTest(obs.Plan)
	res, err := admission.Evaluate(admission.AdmissionInput{
		Plan: pv, Facts: facts, Policy: pol,
	})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if res.Decision != admission.DecisionNotNeeded {
		t.Fatalf("Decision=%s, want not_needed", res.Decision)
	}
	if len(res.Reasons) != 1 || res.Reasons[0].Code != admission.ReasonNoRemoteChange {
		t.Fatalf("reasons=%+v, want NO_REMOTE_CHANGE", res.Reasons)
	}
}

// TestAdmission_PrivateCommit_Deny is the §32 evidence gate.
func TestAdmission_PrivateCommit_Deny(t *testing.T) {
	requireAdmissionPrereqs(t)
	ctx := context.Background()
	l, err := lab.Setup(ctx)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	defer l.Cleanup()

	// Seed the policy file directly into the candidate's tree
	// (CORRECTION03 §5: the policy must be part of NEW's tree,
	// not the live working copy, so admission can read it back
	// via `jj file show -r NEW --at-op=<opID>`).
	body := strings.Join([]string{
		`schema_version = 1`,
		`private_commits = "description('private:*')"`,
		"",
	}, "\n")
	if err := l.SeedPolicyInCandidate(ctx, body); err != nil {
		t.Fatalf("SeedPolicyInCandidate: %v", err)
	}

	if err := l.RunJJ(ctx, []string{"describe", "feature", "-m", "private: secret work"}); err != nil {
		t.Fatalf("jj describe: %v", err)
	}
	src := plan.NewJJSource(jjadapter.New())
	obs, err := plan.ResolveObserved(ctx, src, plan.ResolveOptions{
		Dir: l.JJClient, Remote: "lab", Bookmark: "feature",
	})
	if err != nil {
		t.Fatalf("ResolveObserved: %v", err)
	}
	a := jjadapter.New()
	pol, _, err := admission.LoadPolicyAt(
		context.Background(),
		&adapterPolicyReaderForTest{a: a},
		mustWrapPlanTest(obs.Plan),
		l.JJClient,
		obs.SourceOperationID,
	)
	if err != nil {
		t.Fatalf("LoadPolicyAt: %v", err)
	}
	if pol.PrivateCommits == admission.DefaultPolicy().PrivateCommits {
		t.Fatalf("BJJ policy private_commits did not load; got %q", pol.PrivateCommits)
	}
	facts := gatherInLab(t, l, obs)
	if len(facts.PrivateCommits) == 0 {
		t.Fatalf("expected private commits detected; got %+v", facts)
	}
	pv := mustWrapPlanTest(obs.Plan)
	res, err := admission.Evaluate(admission.AdmissionInput{
		Plan: pv, Facts: facts, Policy: pol,
	})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if res.Decision != admission.DecisionDeny {
		t.Fatalf("Decision=%s, want deny; reasons=%+v", res.Decision, res.Reasons)
	}
	foundPrivate := false
	for _, r := range res.Reasons {
		if r.Code == admission.ReasonPrivateCommit {
			foundPrivate = true
		}
	}
	if !foundPrivate {
		t.Fatalf("reasons=%+v, want PRIVATE_COMMIT", res.Reasons)
	}
}

// TestAdmission_PrivateAncestor_Deny verifies that a private
// commit that IS material to the publication subject (i.e. the
// remote-tracking bookmark is absent so the whole stack is
// published fresh) denies.
//
// Per CORRECTION01 §1, "private ancestor" no longer means
// "any private ancestor of NEW" — that class is excluded by
// the subject-set restriction. This test now exercises the
// narrow case where the private commit is itself part of
// PublishPlan.Commits.
func TestAdmission_PrivateAncestor_Deny(t *testing.T) {
	requireAdmissionPrereqs(t)
	ctx := context.Background()
	l, err := lab.Setup(ctx)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	defer l.Cleanup()

	// Seed the policy file directly into the candidate's tree
	// (CORRECTION03 §5).
	body := strings.Join([]string{
		`schema_version = 1`,
		`private_commits = "description('private:*')"`,
		"",
	}, "\n")
	if err := l.SeedPolicyInCandidate(ctx, body); err != nil {
		t.Fatalf("SeedPolicyInCandidate: %v", err)
	}

	if err := l.RunJJ(ctx, []string{"describe", "feature", "-m", "private: ancestor"}); err != nil {
		t.Fatalf("jj describe private: %v", err)
	}
	if err := l.RunJJ(ctx, []string{"new", "feature", "-m", "public: descendant"}); err != nil {
		t.Fatalf("jj new: %v", err)
	}
	if err := os.WriteFile(filepath.Join(l.JJClient, "public.txt"), []byte("public\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := l.RunJJ(ctx, []string{"bookmark", "set", "feature", "-r", "@"}); err != nil {
		t.Fatalf("jj bookmark set: %v", err)
	}

	src := plan.NewJJSource(jjadapter.New())
	obs, err := plan.ResolveObserved(ctx, src, plan.ResolveOptions{
		Dir: l.JJClient, Remote: "lab", Bookmark: "feature",
	})
	if err != nil {
		t.Fatalf("ResolveObserved: %v", err)
	}
	a := jjadapter.New()
	pol, _, err := admission.LoadPolicyAt(
		context.Background(),
		&adapterPolicyReaderForTest{a: a},
		mustWrapPlanTest(obs.Plan),
		l.JJClient,
		obs.SourceOperationID,
	)
	if err != nil {
		t.Fatalf("LoadPolicyAt: %v", err)
	}
	facts := gatherInLab(t, l, obs)
	if len(facts.PrivateCommits) == 0 {
		t.Fatalf("expected at least one private commit detected; got %+v", facts)
	}
	pv := mustWrapPlanTest(obs.Plan)
	res, err := admission.Evaluate(admission.AdmissionInput{
		Plan: pv, Facts: facts, Policy: pol,
	})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if res.Decision != admission.DecisionDeny {
		t.Fatalf("Decision=%s, want deny", res.Decision)
	}
}

// TestAdmission_EmptyDescription_Deny is the §33 evidence gate.
func TestAdmission_EmptyDescription_Deny(t *testing.T) {
	requireAdmissionPrereqs(t)
	ctx := context.Background()
	l, err := lab.Setup(ctx)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	defer l.Cleanup()

	if err := l.RunJJ(ctx, []string{"describe", "feature", "-m", ""}); err != nil {
		t.Fatalf("jj describe empty: %v", err)
	}
	src := plan.NewJJSource(jjadapter.New())
	obs, err := plan.ResolveObserved(ctx, src, plan.ResolveOptions{
		Dir: l.JJClient, Remote: "lab", Bookmark: "feature",
	})
	if err != nil {
		t.Fatalf("ResolveObserved: %v", err)
	}
	pol, _, _ := admission.LoadPolicyFromRepo(l.JJClient)
	facts := gatherInLab(t, l, obs)
	if len(facts.EmptyDescriptionCommits) == 0 {
		t.Fatalf("expected empty-description detection; got facts=%+v", facts)
	}
	pv := mustWrapPlanTest(obs.Plan)
	res, err := admission.Evaluate(admission.AdmissionInput{
		Plan: pv, Facts: facts, Policy: pol,
	})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if res.Decision != admission.DecisionDeny {
		t.Fatalf("Decision=%s, want deny; reasons=%+v", res.Decision, res.Reasons)
	}
	foundEmpty := false
	for _, r := range res.Reasons {
		if r.Code == admission.ReasonEmptyDescription {
			foundEmpty = true
		}
	}
	if !foundEmpty {
		t.Fatalf("reasons=%+v, want EMPTY_DESCRIPTION", res.Reasons)
	}
}

// TestAdmission_ConflictedCommit_Deny is the §31 evidence gate.
// A pinned subject whose tip has unresolved content conflicts
// must be denied regardless of policy allow_nff flag.
func TestAdmission_ConflictedCommit_Deny(t *testing.T) {
	requireAdmissionPrereqs(t)
	ctx := context.Background()
	l, err := lab.Setup(ctx)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	defer l.Cleanup()

	// Push feature so the remote-tracking bookmark exists.
	if err := l.SeedBookmarkPush(ctx, "feature"); err != nil {
		t.Fatalf("SeedBookmarkPush: %v", err)
	}

	if _, _, err := l.SeedConflictedCommit(ctx); err != nil {
		t.Fatalf("SeedConflictedCommit: %v", err)
	}

	// Move feature onto the conflicted commit. This is a
	// backward FF (since the new tip is not a descendant of
	// the previous feature), so --allow-backwards is needed.
	if err := l.RunJJ(ctx, []string{"bookmark", "set", "feature", "--allow-backwards", "-r", "@"}); err != nil {
		t.Fatalf("jj bookmark set: %v", err)
	}

	src := plan.NewJJSource(jjadapter.New())
	obs, err := plan.ResolveObserved(ctx, src, plan.ResolveOptions{
		Dir: l.JJClient, Remote: "lab", Bookmark: "feature",
	})
	if err != nil {
		t.Fatalf("ResolveObserved: %v", err)
	}
	pol, _, _ := admission.LoadPolicyFromRepo(l.JJClient)
	facts := gatherInLab(t, l, obs)
	if len(facts.ConflictedCommits) == 0 {
		t.Fatalf("expected ConflictedCommits to be non-empty; facts=%+v", facts)
	}
	pv := mustWrapPlanTest(obs.Plan)
	res, err := admission.Evaluate(admission.AdmissionInput{
		Plan: pv, Facts: facts, Policy: pol,
	})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if res.Decision != admission.DecisionDeny {
		t.Fatalf("Decision=%s, want deny; reasons=%+v", res.Decision, res.Reasons)
	}
	foundConflict := false
	for _, r := range res.Reasons {
		if r.Code == admission.ReasonConflictedCommit {
			foundConflict = true
		}
	}
	if !foundConflict {
		t.Fatalf("reasons=%+v, want CONFLICTED_COMMIT", res.Reasons)
	}
}

// TestAdmission_NonFastForward_DenyDefault verifies §26: a NFF
// subject is denied under default policy.
func TestAdmission_NonFastForward_DenyDefault(t *testing.T) {
	requireAdmissionPrereqs(t)
	ctx := context.Background()
	l, err := lab.Setup(ctx)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	defer l.Cleanup()
	// Push feature so the remote-tracking bookmark exists.
	if err := l.SeedBookmarkPush(ctx, "feature"); err != nil {
		t.Fatalf("SeedBookmarkPush: %v", err)
	}
	// Now build a divergent local commit on top of main (NOT on
	// top of feature), then move the feature bookmark there.
	// This produces a non-fast-forward because the remote
	// tracking commit is not an ancestor of the new local target.
	if err := l.RunJJ(ctx, []string{"new", "main", "-m", "divergent C2"}); err != nil {
		t.Fatalf("jj new: %v", err)
	}
	if err := os.WriteFile(filepath.Join(l.JJClient, "divergent.txt"), []byte("divergent\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := l.RunJJ(ctx, []string{"bookmark", "set", "feature", "--allow-backwards", "-r", "@"}); err != nil {
		t.Fatalf("jj bookmark set: %v", err)
	}
	src := plan.NewJJSource(jjadapter.New())
	obs, err := plan.ResolveObserved(ctx, src, plan.ResolveOptions{
		Dir: l.JJClient, Remote: "lab", Bookmark: "feature",
	})
	if err != nil {
		t.Fatalf("ResolveObserved: %v", err)
	}
	facts := gatherInLab(t, l, obs)
	if facts.MoveRelation != admission.MoveNonFastForward {
		t.Fatalf("MoveRelation=%s, want NON_FAST_FORWARD", facts.MoveRelation)
	}
	pol, _, _ := admission.LoadPolicyFromRepo(l.JJClient)
	pv := mustWrapPlanTest(obs.Plan)
	res, err := admission.Evaluate(admission.AdmissionInput{
		Plan: pv, Facts: facts, Policy: pol,
	})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if res.Decision != admission.DecisionDeny {
		t.Fatalf("Decision=%s, want deny", res.Decision)
	}
	foundNFF := false
	for _, r := range res.Reasons {
		if r.Code == admission.ReasonNonFastForward {
			foundNFF = true
		}
	}
	if !foundNFF {
		t.Fatalf("reasons=%+v, want NON_FAST_FORWARD", res.Reasons)
	}
}

// TestAdmission_NonFastForward_AllowedByPolicy verifies §26.
func TestAdmission_NonFastForward_AllowedByPolicy(t *testing.T) {
	requireAdmissionPrereqs(t)
	ctx := context.Background()
	l, err := lab.Setup(ctx)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	defer l.Cleanup()

	// Seed everything FIRST, then write the policy. This avoids
	// any potential jj-workspace interaction that could erase an
	// untracked .bjj/ directory during seeding.
	if err := l.SeedBookmarkPush(ctx, "feature"); err != nil {
		t.Fatalf("SeedBookmarkPush: %v", err)
	}
	if err := l.RunJJ(ctx, []string{"new", "main", "-m", "divergent C2"}); err != nil {
		t.Fatalf("jj new: %v", err)
	}
	if err := os.WriteFile(filepath.Join(l.JJClient, "divergent.txt"), []byte("divergent\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := l.RunJJ(ctx, []string{"bookmark", "set", "feature", "--allow-backwards", "-r", "@"}); err != nil {
		t.Fatalf("jj bookmark set: %v", err)
	}

	// Seed the policy file directly into the candidate's tree
	// (CORRECTION03 §5).
	body := strings.Join([]string{
		`schema_version = 1`,
		`allow_non_fast_forward = true`,
		`require_description = false`,
		"",
	}, "\n")
	if err := l.SeedPolicyInCandidate(ctx, body); err != nil {
		t.Fatalf("SeedPolicyInCandidate: %v", err)
	}

	src := plan.NewJJSource(jjadapter.New())
	obs, err := plan.ResolveObserved(ctx, src, plan.ResolveOptions{
		Dir: l.JJClient, Remote: "lab", Bookmark: "feature",
	})
	if err != nil {
		t.Fatalf("ResolveObserved: %v", err)
	}
	a := jjadapter.New()
	pol, _, err := admission.LoadPolicyAt(
		context.Background(),
		&adapterPolicyReaderForTest{a: a},
		mustWrapPlanTest(obs.Plan),
		l.JJClient,
		obs.SourceOperationID,
	)
	if err != nil {
		t.Fatalf("LoadPolicyAt: %v", err)
	}
	if !pol.AllowNonFastForward {
		t.Fatalf("policy did not load allow_non_fast_forward=true; got %+v", pol)
	}
	g := admission.NewGatherer(&adapterSourceForTest{a: a})
	pv := mustWrapPlanTest(obs.Plan)
	facts, err := g.Gather(ctx, pv, pol, admission.GatherOptions{
		Dir: l.JJClient, OpID: obs.SourceOperationID,
	})
	if err != nil {
		t.Fatalf("Gather: %v", err)
	}
	if facts.MoveRelation != admission.MoveNonFastForward {
		t.Fatalf("MoveRelation=%s, want NON_FAST_FORWARD", facts.MoveRelation)
	}
	res, err := admission.Evaluate(admission.AdmissionInput{
		Plan: pv, Facts: facts, Policy: pol,
	})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if res.Decision != admission.DecisionAdmit {
		t.Fatalf("Decision=%s, want admit (policy allows NFF); reasons=%+v",
			res.Decision, res.Reasons)
	}
}

// TestAdmission_NoTransport verifies §29.
func TestAdmission_NoTransport(t *testing.T) {
	requireAdmissionPrereqs(t)
	ctx := context.Background()
	l, err := lab.Setup(ctx)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	defer l.Cleanup()
	beforeFeature, _ := l.GitOutput(ctx, l.Remote, []string{"rev-parse", "refs/heads/feature"})

	src := plan.NewJJSource(jjadapter.New())
	obs, err := plan.ResolveObserved(ctx, src, plan.ResolveOptions{
		Dir: l.JJClient, Remote: "lab", Bookmark: "feature",
	})
	if err != nil {
		t.Fatalf("ResolveObserved: %v", err)
	}
	facts := gatherInLab(t, l, obs)
	pol, _, _ := admission.LoadPolicyFromRepo(l.JJClient)
	pv := mustWrapPlanTest(obs.Plan)
	if _, err := admission.Evaluate(admission.AdmissionInput{
		Plan: pv, Facts: facts, Policy: pol,
	}); err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	afterFeature, _ := l.GitOutput(ctx, l.Remote, []string{"rev-parse", "refs/heads/feature"})
	if beforeFeature != afterFeature {
		t.Fatalf("remote refs/heads/feature changed during admit: before=%q after=%q",
			beforeFeature, afterFeature)
	}
}

// TestAdmission_JSON_NoEnvironment verifies the canonical JSON
// does not encode environment.
func TestAdmission_JSON_NoEnvironment(t *testing.T) {
	requireAdmissionPrereqs(t)
	ctx := context.Background()
	l, err := lab.Setup(ctx)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	defer l.Cleanup()
	src := plan.NewJJSource(jjadapter.New())
	obs, err := plan.ResolveObserved(ctx, src, plan.ResolveOptions{
		Dir: l.JJClient, Remote: "lab", Bookmark: "feature",
	})
	if err != nil {
		t.Fatalf("ResolveObserved: %v", err)
	}
	facts := gatherInLab(t, l, obs)
	pol, _, _ := admission.LoadPolicyFromRepo(l.JJClient)
	pv := mustWrapPlanTest(obs.Plan)
	res, err := admission.Evaluate(admission.AdmissionInput{
		Plan: pv, Facts: facts, Policy: pol,
	})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	b, err := admission.RenderJSON(res)
	if err != nil {
		t.Fatalf("RenderJSON: %v", err)
	}
	s := string(b)
	if strings.Contains(s, l.Remote) {
		t.Fatalf("JSON leaked remote path: %s\n%s", l.Remote, s)
	}
	if strings.Contains(s, l.JJClient) {
		t.Fatalf("JSON leaked jj client path: %s\n%s", l.JJClient, s)
	}
	var any map[string]any
	if err := json.Unmarshal(b, &any); err != nil {
		t.Fatalf("invalid strict JSON: %v\n%s", err, s)
	}
}

// TestAdmission_Gather_UsesFrozenOpID is the §4 + §37 guard: the
// gatherer issues every query against the EXACT opID it was
// passed, even if the underlying repository mutates between
// queries.
func TestAdmission_Gather_UsesFrozenOpID(t *testing.T) {
	requireAdmissionPrereqs(t)
	ctx := context.Background()
	l, err := lab.Setup(ctx)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	defer l.Cleanup()

	src := plan.NewJJSource(jjadapter.New())
	obs, err := plan.ResolveObserved(ctx, src, plan.ResolveOptions{
		Dir: l.JJClient, Remote: "lab", Bookmark: "feature",
	})
	if err != nil {
		t.Fatalf("ResolveObserved: %v", err)
	}
	snapshotOp := obs.SourceOperationID

	adapter := &mutatingAdapter{
		inner:  jjadapter.New(),
		mutate: l,
		opIDs:  []string{},
	}
	pol, _, _ := admission.LoadPolicyFromRepo(l.JJClient)
	g := admission.NewGatherer(adapter)
	pv := mustWrapPlanTest(obs.Plan)
	if _, err := g.Gather(ctx, pv, pol, admission.GatherOptions{
		Dir: l.JJClient, OpID: snapshotOp,
	}); err != nil {
		t.Fatalf("Gather: %v", err)
	}
	if !adapter.mutated {
		t.Fatalf("mutatingAdapter never mutated; cannot prove single-view")
	}
	currentOp, err := probeCurrentOpID(ctx, l)
	if err != nil {
		t.Fatalf("probe op: %v", err)
	}
	if snapshotOp == currentOp {
		t.Fatalf("mutation did not advance op; snap=%s current=%s", snapshotOp, currentOp)
	}
	for i, op := range adapter.opIDs {
		if op != snapshotOp {
			t.Fatalf("admission query #%d used opID %s; expected %s", i, op, snapshotOp)
		}
	}
}

// mutatingAdapter wraps the production jjadapter.Adapter and
// records every opID it sees. After the first call to either
// ListCommits or ListCommitObs it mutates the underlying
// repository, advancing the operation id.
type mutatingAdapter struct {
	inner   *jjadapter.Adapter
	mutate  *lab.Lab
	mutated bool
	opIDs   []string
}

func (m *mutatingAdapter) ListCommits(ctx context.Context, dir, opID, revset string) ([]jjadapter.CommitRef, error) {
	m.opIDs = append(m.opIDs, opID)
	if !m.mutated {
		m.mutated = true
		if err := m.mutate.RunJJ(ctx, []string{"new", "-m", "mid-gather mutation"}); err != nil {
			return nil, err
		}
		if err := os.WriteFile(filepath.Join(m.mutate.JJClient, "mid.txt"), []byte("mid\n"), 0o644); err != nil {
			return nil, err
		}
	}
	return m.inner.ListCommits(ctx, dir, opID, revset)
}

func (m *mutatingAdapter) ListCommitObs(ctx context.Context, dir, opID, revset string) ([]jjadapter.CommitObs, error) {
	m.opIDs = append(m.opIDs, opID)
	if !m.mutated {
		m.mutated = true
		if err := m.mutate.RunJJ(ctx, []string{"new", "-m", "mid-gather mutation"}); err != nil {
			return nil, err
		}
	}
	return m.inner.ListCommitObs(ctx, dir, opID, revset)
}

// probeCurrentOpID returns the current jj operation id of the
// lab's jj client (without going through the production
// adapter's env overlay).
func probeCurrentOpID(ctx context.Context, l *lab.Lab) (string, error) {
	out, err := l.JJOutput(ctx, l.JJClient, []string{"op", "log", "--no-graph", "--limit", "1", "-T", "id"})
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}
