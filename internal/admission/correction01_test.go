package admission_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/s1onique/bjj/internal/admission"
	"github.com/s1onique/bjj/internal/jjadapter"
	"github.com/s1onique/bjj/internal/lab"
	"github.com/s1onique/bjj/internal/plan"
)

// TestAdmission_AlreadyPublishedPrivateAncestor_DoesNotDeny is the
// CORRECTION01 §3 regression fixture.
//
// History:
//
//	A (lab/feature@A, "private: ancestor A")
//	|
//	B ("public: descendant B")    <-- feature (local)
//
// B is the new tip of feature; the publication subject is {B}
// because the remote-tracking bookmark for feature already points
// at A, which is the effectiveOld. The remote side already
// carries A; admission MUST NOT deny because A matches
// description('private:*').
func TestAdmission_AlreadyPublishedPrivateAncestor_DoesNotDeny(t *testing.T) {
	requireAdmissionPrereqs(t)
	ctx := context.Background()
	l, err := lab.Setup(ctx)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	t.Cleanup(l.Cleanup)

	body := strings.Join([]string{
		`schema_version = 1`,
		`private_commits = "description('private:*')"`,
		``,
	}, "\n")
	if err := l.SeedPolicyInCandidate(ctx, body); err != nil {
		t.Fatalf("SeedPolicyInCandidate: %v", err)
	}

	// 1. Describe feature@A as private: ancestor A.
	if err := l.RunJJ(ctx, []string{"describe", "feature", "-m", "private: ancestor A"}); err != nil {
		t.Fatalf("jj describe A: %v", err)
	}

	// 2. Push feature@A to the lab remote.
	if err := l.SeedBookmarkPush(ctx, "feature"); err != nil {
		t.Fatalf("SeedBookmarkPush: %v", err)
	}

	// 3. Add a new descendant B (public-looking) on top of A and
	//    move feature to it.
	if err := l.RunJJ(ctx, []string{"new", "feature", "-m", "public: descendant B"}); err != nil {
		t.Fatalf("jj new B: %v", err)
	}
	if err := os.WriteFile(filepath.Join(l.JJClient, "b.txt"), []byte("b\n"), 0o644); err != nil {
		t.Fatalf("write b: %v", err)
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

	// Sanity: the plan's subject is just B; A is the effectiveOld.
	if len(obs.Plan.Commits) != 1 {
		t.Fatalf("plan.Commits=%v, want exactly one (B); A must be excluded", commitIDs(obs.Plan.Commits))
	}
	if obs.Plan.BookmarkMoves[0].Old == nil {
		t.Fatalf("expected non-nil Old; should reference the remote-tracking tip")
	}

	a := jjadapter.New()
	pol, _, err := admission.LoadPolicyAt(
		context.Background(),
		&correction01PolicyReader{a: a},
		correction01WrapPlan(obs.Plan),
		l.JJClient,
		obs.SourceOperationID,
	)
	if err != nil {
		t.Fatalf("LoadPolicyAt: %v", err)
	}

	facts := gatherInLab(t, l, obs)

	// The CORRECTION01 invariant: A must NOT appear in any fact
	// category. It is not in the subject set; it is already on
	// the remote.
	if len(facts.PrivateCommits) != 0 {
		t.Fatalf("facts.PrivateCommits=%v, want empty; A is already-published and out of subject", facts.PrivateCommits)
	}
	for _, c := range facts.OutgoingCommits {
		if c.CommitID == obs.Plan.BookmarkMoves[0].Old.CommitID {
			t.Fatalf("facts.OutgoingCommits must not contain effectiveOld A=%s", c.CommitID)
		}
	}

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

// TestAdmission_AlreadyPublishedBadDescription_DoesNotDeny is the
// CORRECTION01 §3 counterpart for the empty-description predicate.
//
// History:
//
//	A (lab/feature@A, description "")
//	|
//	B ("public: descendant B")    <-- feature (local)
//
// A is on the remote. The subject is {B}. B has a real
// description. Admission MUST admit.
func TestAdmission_AlreadyPublishedBadDescription_DoesNotDeny(t *testing.T) {
	requireAdmissionPrereqs(t)
	ctx := context.Background()
	l, err := lab.Setup(ctx)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	t.Cleanup(l.Cleanup)

	if err := l.RunJJ(ctx, []string{"describe", "feature", "-m", ""}); err != nil {
		t.Fatalf("jj describe A empty: %v", err)
	}
	if err := l.SeedBookmarkPush(ctx, "feature"); err != nil {
		t.Fatalf("SeedBookmarkPush: %v", err)
	}

	if err := l.RunJJ(ctx, []string{"new", "feature", "-m", "public: descendant B"}); err != nil {
		t.Fatalf("jj new B: %v", err)
	}
	if err := os.WriteFile(filepath.Join(l.JJClient, "b.txt"), []byte("b\n"), 0o644); err != nil {
		t.Fatalf("write b: %v", err)
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

	if len(obs.Plan.Commits) != 1 {
		t.Fatalf("plan.Commits=%v, want exactly one (B); A must be excluded", commitIDs(obs.Plan.Commits))
	}

	a := jjadapter.New()
	pol, _, err := admission.LoadPolicyAt(
		context.Background(),
		&correction01PolicyReader{a: a},
		correction01WrapPlan(obs.Plan),
		l.JJClient,
		obs.SourceOperationID,
	)
	if err != nil {
		t.Fatalf("LoadPolicyAt: %v", err)
	}
	facts := gatherInLab(t, l, obs)

	if len(facts.EmptyDescriptionCommits) != 0 {
		t.Fatalf("facts.EmptyDescriptionCommits=%v, want empty; A is already-published", facts.EmptyDescriptionCommits)
	}

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

// TestAdmission_SubjectSetMatchesPlan asserts the CORRECTION01 §1
// invariant directly: AdmissionFacts.OutgoingCommits is the SAME
// set as PublishPlan.Commits (after canonical sorting). Any
// divergence is a structural bug.
func TestAdmission_SubjectSetMatchesPlan(t *testing.T) {
	l, obs := admitLabFixture(t)
	facts := gatherInLab(t, l, obs)

	planIDs := commitIDs(obs.Plan.Commits)
	admissionIDs := admissionCommitIDs(facts.OutgoingCommits)
	if !sameStringSet(planIDs, admissionIDs) {
		t.Fatalf("subject set mismatch:\n  plan    = %v\n  admission = %v", planIDs, admissionIDs)
	}
}

// commitIDs returns the commit_ids from a slice of
// plan.CommitRef as a string slice.
func commitIDs(cs []plan.CommitRef) []string {
	out := make([]string, 0, len(cs))
	for _, c := range cs {
		out = append(out, c.CommitID)
	}
	return out
}

// admissionCommitIDs returns the commit_ids from a slice of
// admission.CommitRef as a string slice.
func admissionCommitIDs(cs []admission.CommitRef) []string {
	out := make([]string, 0, len(cs))
	for _, c := range cs {
		out = append(out, c.CommitID)
	}
	return out
}

// sameStringSet returns true iff a and b contain exactly the
// same strings, ignoring order.
func sameStringSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	seen := map[string]int{}
	for _, s := range a {
		seen[s]++
	}
	for _, s := range b {
		seen[s]--
		if seen[s] < 0 {
			return false
		}
	}
	return true
}

// correction01WrapPlan adapts a *plan.PublishPlan into the
// admission.PlanView interface for tests in this file. A copy
// lives in integration_test.go as mustWrapPlanTest; tests in
// this file have a dedicated alias so the dependency direction
// stays local.
func correction01WrapPlan(p *plan.PublishPlan) admission.PlanView {
	if p == nil || len(p.BookmarkMoves) == 0 {
		panic("correction01WrapPlan: empty plan")
	}
	return &correction01PlanView{p: p}
}

type correction01PlanView struct {
	p *plan.PublishPlan
}

func (a *correction01PlanView) RemoteName() string   { return a.p.Remote.Name }
func (a *correction01PlanView) BookmarkName() string { return a.p.BookmarkMoves[0].Name }
func (a *correction01PlanView) OldCommitID() string {
	mv := a.p.BookmarkMoves[0]
	if mv.Old == nil {
		return ""
	}
	return mv.Old.CommitID
}
func (a *correction01PlanView) NewCommitID() string {
	mv := a.p.BookmarkMoves[0]
	if mv.New == nil {
		return ""
	}
	return mv.New.CommitID
}
func (a *correction01PlanView) StatusName() string { return string(a.p.Status) }
func (a *correction01PlanView) OutgoingCommits() []admission.CommitRef {
	out := make([]admission.CommitRef, 0, len(a.p.Commits))
	for _, c := range a.p.Commits {
		out = append(out, admission.CommitRef{
			CommitID: c.CommitID,
			ChangeID: c.ChangeID,
		})
	}
	return out
}

// correction01PolicyReader satisfies admission.PolicyReader via
// the real jj adapter.
type correction01PolicyReader struct {
	a *jjadapter.Adapter
}

func (r *correction01PolicyReader) ReadPolicyFile(ctx context.Context, dir, opID, revision, path string) ([]byte, admission.PolicyFilePresence, error) {
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
