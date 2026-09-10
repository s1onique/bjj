package admission_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/s1onique/bjj/internal/admission"
	"github.com/s1onique/bjj/internal/jjadapter"
	"github.com/s1onique/bjj/internal/lab"
	"github.com/s1onique/bjj/internal/plan"
)

// CORRECTION03 §6-§7 adversarial tests.
//
// Each test resolves a publication plan, reads the policy via
// `jj file show -r NEW --at-op=<opID>` (via admission.LoadPolicyAt),
// and then mutates the live working copy / repository BEFORE the
// admission evaluation is finished. The admission decision MUST
// still reflect the policy bytes that lived in the frozen
// candidate tree at OP_A, never the live working copy.
//
// Each test names a single invariant from §6-§7 in its Go
// identifier; the doc comment quotes the spec section it
// enforces.

func correction03Reader(t *testing.T, l *lab.Lab, obs *plan.PlanObservation) (admission.Policy, admission.PolicySource) {
	t.Helper()
	a := jjadapter.New()
	pol, src, err := admission.LoadPolicyAt(
		context.Background(),
		&adapterPolicyReaderForTest{a: a},
		mustWrapPlanTest(obs.Plan),
		l.JJClient,
		obs.SourceOperationID,
	)
	if err != nil {
		t.Fatalf("LoadPolicyAt: %v", err)
	}
	return pol, src
}

// CORRECTION03 §6: "OP_A restrictive, OP_B permissive" —
// mid-admission mutation from restrictive to permissive must
// NOT widen the policy.
//
// The mutation in this test is exclusively a LIVE WORKING COPY
// mutation, NOT a `jj describe`-style committed mutation.
// We deliberately do NOT call SeedPolicyInCandidate for the
// mutation; that would advance the operation view and create
// a different commit for NEW, defeating the purpose. The whole
// point of CORRECTION03 is to defeat the unsafe scenario where
// the live working copy changes between plan resolution and
// policy load.
//
// Setup order matters: the policy must be part of the candidate
// tree at the moment `ResolveObserved` runs. We do that by
// seeding it into the @ change, pushing feature, then creating
// a divergent C2 on top of MAIN. C2 does NOT inherit C1's
// policy, so we commit the policy into C2's tree too via
// `SeedPolicyInCandidate` after creating C2. The whole stack is
// then [main, C1, C2-with-policy] and feature moves from C1 to
// C2.
func TestAdmission_MidPolicyMutation_RestrictiveToPermissive_CannotAdmitNFF(t *testing.T) {
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
	if err := l.RunJJ(ctx, []string{"new", "main", "-m", "divergent C2"}); err != nil {
		t.Fatalf("jj new: %v", err)
	}
	if err := os.WriteFile(filepath.Join(l.JJClient, "div.txt"), []byte("d\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	// Seed restrictive policy into C2 (=NEW @ OP_A).
	if err := l.SeedPolicyInCandidate(ctx, strings.Join([]string{
		`schema_version = 1`,
		`allow_non_fast_forward = false`,
		`require_description = true`,
		"",
	}, "\n")); err != nil {
		t.Fatalf("SeedPolicyInCandidate: %v", err)
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

	polOP_A, srcOP_A := correction03Reader(t, l, obs)
	if polOP_A.AllowNonFastForward {
		t.Fatalf("OP_A policy must disallow NFF; got %+v", polOP_A)
	}
	if srcOP_A.OperationID != obs.SourceOperationID {
		t.Fatalf("OP_A source opID mismatch: %s vs %s",
			srcOP_A.OperationID, obs.SourceOperationID)
	}

	// OP_B mutation: overwrite LIVE WORKING COPY only. The pinned
	// (opID, NEW) view still sees the restrictive policy.
	if err := os.WriteFile(filepath.Join(l.JJClient, ".bjj", "policy.toml"),
		[]byte("schema_version = 1\nallow_non_fast_forward = true\nrequire_description = false\n"),
		0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	polAtEval, _ := correction03Reader(t, l, obs)
	if polAtEval.AllowNonFastForward {
		t.Fatalf("policy leaked the OP_B mutation; got AllowNonFastForward=true")
	}

	a := jjadapter.New()
	g := admission.NewGatherer(&adapterSourceForTest{a: a})
	pv := mustWrapPlanTest(obs.Plan)
	facts, err := g.Gather(ctx, pv, polAtEval, admission.GatherOptions{
		Dir: l.JJClient, OpID: obs.SourceOperationID,
	})
	if err != nil {
		t.Fatalf("Gather: %v", err)
	}
	if facts.MoveRelation != admission.MoveNonFastForward {
		t.Fatalf("MoveRelation=%s, want NON_FAST_FORWARD", facts.MoveRelation)
	}
	res, err := admission.Evaluate(admission.AdmissionInput{
		Plan: pv, Facts: facts, Policy: polAtEval,
	})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if res.Decision != admission.DecisionDeny {
		t.Fatalf("Decision=%s, want deny (OP_A restrictive policy must dominate); reasons=%+v",
			res.Decision, res.Reasons)
	}
}

// CORRECTION03 §6 inverse: "OP_A permissive, OP_B restrictive" —
// mid-admission mutation from permissive to restrictive must
// NOT deny the candidate.
func TestAdmission_MidPolicyMutation_PermissiveToRestrictive_CannotDenyFF(t *testing.T) {
	requireAdmissionPrereqs(t)
	ctx := context.Background()
	l, err := lab.Setup(ctx)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	defer l.Cleanup()

	if err := l.SeedPolicyInCandidate(ctx, strings.Join([]string{
		`schema_version = 1`,
		`allow_non_fast_forward = true`,
		`require_description = false`,
		"",
	}, "\n")); err != nil {
		t.Fatalf("SeedPolicyInCandidate: %v", err)
	}
	if err := l.SeedBookmarkPush(ctx, "feature"); err != nil {
		t.Fatalf("SeedBookmarkPush: %v", err)
	}
	if err := l.RunJJ(ctx, []string{"new", "feature", "-m", "ff candidate"}); err != nil {
		t.Fatalf("jj new: %v", err)
	}
	if err := os.WriteFile(filepath.Join(l.JJClient, "f.txt"), []byte("f\n"), 0o644); err != nil {
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
	polOP_A, _ := correction03Reader(t, l, obs)
	if !polOP_A.AllowNonFastForward {
		t.Fatalf("OP_A policy must allow NFF; got %+v", polOP_A)
	}

	// OP_B mutation: overwrite LIVE WORKING COPY only.
	if err := os.WriteFile(filepath.Join(l.JJClient, ".bjj", "policy.toml"),
		[]byte("schema_version = 1\nallow_non_fast_forward = false\nrequire_description = true\n"),
		0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	polAtEval, _ := correction03Reader(t, l, obs)
	if !polAtEval.AllowNonFastForward {
		t.Fatalf("OP_A permissive policy leaked the OP_B restrictive mutation")
	}

	a := jjadapter.New()
	g := admission.NewGatherer(&adapterSourceForTest{a: a})
	pv := mustWrapPlanTest(obs.Plan)
	facts, err := g.Gather(ctx, pv, polAtEval, admission.GatherOptions{
		Dir: l.JJClient, OpID: obs.SourceOperationID,
	})
	if err != nil {
		t.Fatalf("Gather: %v", err)
	}
	res, err := admission.Evaluate(admission.AdmissionInput{
		Plan: pv, Facts: facts, Policy: polAtEval,
	})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if res.Decision != admission.DecisionAdmit {
		t.Fatalf("Decision=%s, want admit (OP_A permissive policy must dominate); reasons=%+v",
			res.Decision, res.Reasons)
	}
}

// CORRECTION03 §7: policy exists @ OP_A, removed @ OP_B.
func TestAdmission_MidPolicyMutation_PolicyLiveRemoveIgnored(t *testing.T) {
	requireAdmissionPrereqs(t)
	ctx := context.Background()
	l, err := lab.Setup(ctx)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	defer l.Cleanup()

	if err := l.SeedPolicyInCandidate(ctx, strings.Join([]string{
		`schema_version = 1`,
		`allow_non_fast_forward = true`,
		`require_description = false`,
		"",
	}, "\n")); err != nil {
		t.Fatalf("SeedPolicyInCandidate: %v", err)
	}
	if err := l.SeedBookmarkPush(ctx, "feature"); err != nil {
		t.Fatalf("SeedBookmarkPush: %v", err)
	}
	if err := l.RunJJ(ctx, []string{"new", "feature", "-m", "ff candidate"}); err != nil {
		t.Fatalf("jj new: %v", err)
	}
	if err := os.WriteFile(filepath.Join(l.JJClient, "f.txt"), []byte("f\n"), 0o644); err != nil {
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
	polOP_A, _ := correction03Reader(t, l, obs)
	if !polOP_A.AllowNonFastForward {
		t.Fatalf("OP_A policy must be LOADED; got %+v", polOP_A)
	}

	if err := os.Remove(filepath.Join(l.JJClient, ".bjj", "policy.toml")); err != nil {
		t.Fatalf("remove: %v", err)
	}

	polAtEval, _ := correction03Reader(t, l, obs)
	if !polAtEval.AllowNonFastForward {
		t.Fatalf("OP_A policy was lost after live removal; got %+v", polAtEval)
	}
}

// CORRECTION03 §7: policy absent @ OP_A, added @ OP_B.
func TestAdmission_MidPolicyMutation_PolicyLiveAddIgnored(t *testing.T) {
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
	if err := l.RunJJ(ctx, []string{"new", "main", "-m", "divergent C2"}); err != nil {
		t.Fatalf("jj new: %v", err)
	}
	if err := os.WriteFile(filepath.Join(l.JJClient, "div.txt"), []byte("d\n"), 0o644); err != nil {
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

	polOP_A, srcOP_A := correction03Reader(t, l, obs)
	if srcOP_A.Outcome != admission.PolicyOutcomeDefault {
		t.Fatalf("OP_A outcome=%s, want DEFAULT_POLICY", srcOP_A.Outcome)
	}
	if polOP_A.AllowNonFastForward {
		t.Fatalf("DefaultPolicy must NOT allow NFF; got %+v", polOP_A)
	}

	if err := os.MkdirAll(filepath.Join(l.JJClient, ".bjj"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(l.JJClient, ".bjj", "policy.toml"),
		[]byte("schema_version = 1\nallow_non_fast_forward = true\nrequire_description = false\n"),
		0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	polAtEval, srcAtEval := correction03Reader(t, l, obs)
	if srcAtEval.Outcome != admission.PolicyOutcomeDefault {
		t.Fatalf("OP_B added policy leaked into admission; got outcome=%s", srcAtEval.Outcome)
	}
	if polAtEval.AllowNonFastForward {
		t.Fatalf("DefaultPolicy must still be in effect; got %+v", polAtEval)
	}
}

// CORRECTION03 §7: policy valid @ OP_A, malformed @ OP_B.
func TestAdmission_MidPolicyMutation_PolicyLiveMalformingIgnored(t *testing.T) {
	requireAdmissionPrereqs(t)
	ctx := context.Background()
	l, err := lab.Setup(ctx)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	defer l.Cleanup()

	if err := l.SeedPolicyInCandidate(ctx, strings.Join([]string{
		`schema_version = 1`,
		`allow_non_fast_forward = true`,
		`require_description = false`,
		"",
	}, "\n")); err != nil {
		t.Fatalf("SeedPolicyInCandidate: %v", err)
	}
	if err := l.SeedBookmarkPush(ctx, "feature"); err != nil {
		t.Fatalf("SeedBookmarkPush: %v", err)
	}
	if err := l.RunJJ(ctx, []string{"new", "feature", "-m", "ff candidate"}); err != nil {
		t.Fatalf("jj new: %v", err)
	}
	if err := os.WriteFile(filepath.Join(l.JJClient, "f.txt"), []byte("f\n"), 0o644); err != nil {
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
	polOP_A, _ := correction03Reader(t, l, obs)
	if !polOP_A.AllowNonFastForward {
		t.Fatalf("OP_A policy must be LOADED; got %+v", polOP_A)
	}

	if err := os.WriteFile(filepath.Join(l.JJClient, ".bjj", "policy.toml"),
		[]byte("this is = not valid @@@ toml !!!\n"),
		0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	polAtEval, _ := correction03Reader(t, l, obs)
	if !polAtEval.AllowNonFastForward {
		t.Fatalf("OP_B malforming leaked; got %+v", polAtEval)
	}
}

// CORRECTION03 §5: policy absent from frozen candidate @ OP_A
// resolves to DefaultPolicy.
func TestAdmission_PolicyAbsentInFrozenCandidate_UsesDefault(t *testing.T) {
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
	if err := l.RunJJ(ctx, []string{"new", "main", "-m", "divergent C2"}); err != nil {
		t.Fatalf("jj new: %v", err)
	}
	if err := os.WriteFile(filepath.Join(l.JJClient, "div.txt"), []byte("d\n"), 0o644); err != nil {
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
	pol, srcPol := correction03Reader(t, l, obs)
	if srcPol.Outcome != admission.PolicyOutcomeDefault {
		t.Fatalf("outcome=%s, want DEFAULT_POLICY", srcPol.Outcome)
	}
	if pol.AllowNonFastForward {
		t.Fatalf("DefaultPolicy must not allow NFF; got %+v", pol)
	}
}

// CORRECTION03 §10: the PolicySource carries the (opID, NEW)
// pair, so audit trails can prove the bytes came from the
// frozen candidate, not the live filesystem.
func TestAdmission_PolicySource_BoundToFrozenView(t *testing.T) {
	requireAdmissionPrereqs(t)
	ctx := context.Background()
	l, err := lab.Setup(ctx)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	defer l.Cleanup()

	if err := l.SeedPolicyInCandidate(ctx, strings.Join([]string{
		`schema_version = 1`,
		`allow_non_fast_forward = false`,
		`require_description = true`,
		"",
	}, "\n")); err != nil {
		t.Fatalf("SeedPolicyInCandidate: %v", err)
	}

	src := plan.NewJJSource(jjadapter.New())
	obs, err := plan.ResolveObserved(ctx, src, plan.ResolveOptions{
		Dir: l.JJClient, Remote: "lab", Bookmark: "feature",
	})
	if err != nil {
		t.Fatalf("ResolveObserved: %v", err)
	}
	_, srcPol := correction03Reader(t, l, obs)
	if srcPol.OperationID != obs.SourceOperationID {
		t.Fatalf("policy source opID=%q, want %q",
			srcPol.OperationID, obs.SourceOperationID)
	}
	wantRev := obs.Plan.BookmarkMoves[0].New.CommitID
	if srcPol.Revision != wantRev {
		t.Fatalf("policy source revision=%q, want %q",
			srcPol.Revision, wantRev)
	}
}

// CORRECTION03 §3: FileShowAtOp refuses to read with empty opID
// or empty revision so callers cannot accidentally consult the
// live operation / workspace.
func TestAdapter_FileShowAtOp_RefusesEmptyInputs(t *testing.T) {
	a := jjadapter.New()
	_, _, err := a.FileShowAtOp(context.Background(), "/tmp", "", "abc", "f.txt")
	if err == nil {
		t.Fatal("FileShowAtOp with empty opID must error")
	}
	if !strings.Contains(err.Error(), "opID") {
		t.Fatalf("expected opID error, got %v", err)
	}
	_, _, err = a.FileShowAtOp(context.Background(), "/tmp", "op", "", "f.txt")
	if err == nil {
		t.Fatal("FileShowAtOp with empty revision must error")
	}
	if !strings.Contains(err.Error(), "revision") {
		t.Fatalf("expected revision error, got %v", err)
	}
	_, _, err = a.FileShowAtOp(context.Background(), "/tmp", "op", "rev", "")
	if err == nil {
		t.Fatal("FileShowAtOp with empty path must error")
	}
	if !strings.Contains(err.Error(), "path") {
		t.Fatalf("expected path error, got %v", err)
	}
}

// CORRECTION03 §5: a policy present in the frozen candidate @
// OP_A but malformed in the SAME revision's tree is reported
// as MALFORMED, not DEFAULT_POLICY. This is the "fail closed"
// half of CORRECTION02.
func TestAdmission_PolicyMalformedInFrozenCandidate_FailsClosed(t *testing.T) {
	requireAdmissionPrereqs(t)
	ctx := context.Background()
	l, err := lab.Setup(ctx)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	defer l.Cleanup()

	if err := l.SeedPolicyInCandidate(ctx, strings.Join([]string{
		`schema_version = 1`,
		`allow_non_fast_forward = true`,
		`unknown_key = "value"`,
		"",
	}, "\n")); err != nil {
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
	_, _, err = admission.LoadPolicyAt(
		context.Background(),
		&adapterPolicyReaderForTest{a: a},
		mustWrapPlanTest(obs.Plan),
		l.JJClient,
		obs.SourceOperationID,
	)
	if err == nil {
		t.Fatal("LoadPolicyAt with malformed policy must error")
	}
	var ae *admission.Error
	if !errors.As(err, &ae) {
		t.Fatalf("expected *admission.Error, got %v", err)
	}
	if ae.Code != admission.CodePolicyInvalid {
		t.Fatalf("Code=%s, want %s", ae.Code, admission.CodePolicyInvalid)
	}
}
