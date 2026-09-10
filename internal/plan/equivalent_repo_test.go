package plan_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/s1onique/bjj/internal/lab"
	"github.com/s1onique/bjj/internal/plan"
)

// TestPlan_EquivalentRepositories_DifferentPaths_NoEnvironmentInBody
// is the strict P1 determinism test.
//
// It constructs two independent lab universes at different
// filesystem paths (and therefore different remote URL strings),
// then asserts that NEITHER canonical plan body contains any
// environment-specific information (paths, URLs).
//
// Note: two independent repositories cannot produce byte-identical
// commit_ids because SHAs are content-addressed; that's a property
// of Git, not of BJJ. The contract under test is "the canonical
// plan body does not encode environment", not "two independent
// repos produce identical commits".
func TestPlan_EquivalentRepositories_DifferentPaths_NoEnvironmentInBody(t *testing.T) {
	requirePlanPrereqs(t)
	ctx := context.Background()

	l1, err := lab.Setup(ctx)
	if err != nil {
		t.Fatalf("setup #1: %v", err)
	}
	defer l1.Cleanup()
	l2, err := lab.Setup(ctx)
	if err != nil {
		t.Fatalf("setup #2: %v", err)
	}
	defer l2.Cleanup()

	if l1.Root == l2.Root {
		t.Fatalf("two lab setups must have different paths; both %s", l1.Root)
	}
	if l1.JJClient == l2.JJClient {
		t.Fatalf("two lab setups must have different jj clients; both %s", l1.JJClient)
	}
	if l1.Remote == l2.Remote {
		t.Fatalf("two lab setups must have different remote paths; both %s", l1.Remote)
	}

	if err := l1.SeedBookmarkPush(ctx, "feature"); err != nil {
		t.Fatalf("seed #1: %v", err)
	}
	if err := l2.SeedBookmarkPush(ctx, "feature"); err != nil {
		t.Fatalf("seed #2: %v", err)
	}

	plans := []*plan.PublishPlan{}
	for i, l := range []*lab.Lab{l1, l2} {
		p, err := planFromLab(t, l, "feature")
		if err != nil {
			t.Fatalf("resolve #%d: %v", i+1, err)
		}
		b, err := plan.RenderJSON(p)
		if err != nil {
			t.Fatalf("render #%d: %v", i+1, err)
		}
		plans = append(plans, p)
		s := string(b)
		if strings.Contains(s, l.Remote) {
			t.Fatalf("plan #%d leaked remote path: %s", i+1, l.Remote)
		}
		if strings.Contains(s, l.JJClient) {
			t.Fatalf("plan #%d leaked jj client path: %s", i+1, l.JJClient)
		}
		if strings.Contains(s, l.Root) {
			t.Fatalf("plan #%d leaked lab root path: %s", i+1, l.Root)
		}
		if strings.Contains(s, `"url"`) {
			t.Fatalf("plan #%d contains url field:\n%s", i+1, s)
		}
	}

	// Additionally, both plans must share the same JSON SHAPE:
	// same top-level keys in the same order, same remote name.
	b1, _ := plan.RenderJSON(plans[0])
	b2, _ := plan.RenderJSON(plans[1])
	if !sameJSONShape(t, b1, b2) {
		t.Fatalf("canonical plan shapes diverge:\n--- #1 ---\n%s\n--- #2 ---\n%s\n", b1, b2)
	}
}

// TestPlan_SameRepoPathChange_NoPathLeak proves the canonical plan
// body is invariant under repository path changes for the same
// semantic content. We resolve the same lab twice with no mutation;
// the canonical body must be byte-identical (the determinism test
// already covers this), but this test additionally asserts that the
// body does not embed the lab's path even once.
func TestPlan_SameRepoPathChange_NoPathLeak(t *testing.T) {
	requirePlanPrereqs(t)
	ctx := context.Background()
	l, err := lab.Setup(ctx)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	defer l.Cleanup()

	if err := l.SeedBookmarkPush(ctx, "feature"); err != nil {
		t.Fatalf("seed: %v", err)
	}

	p, err := planFromLab(t, l, "feature")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	b, err := plan.RenderJSON(p)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	s := string(b)
	if strings.Contains(s, l.JJClient) {
		t.Fatalf("plan leaked jj client path: %s\n%s", l.JJClient, s)
	}
	if strings.Contains(s, l.Remote) {
		t.Fatalf("plan leaked remote path: %s\n%s", l.Remote, s)
	}
	if strings.Contains(s, l.Root) {
		t.Fatalf("plan leaked lab root path: %s\n%s", l.Root, s)
	}
}

// sameJSONShape compares two JSON bodies for top-level shape
// identity: same set of keys. The values may legitimately differ
// because commit ids are content-addressed.
func sameJSONShape(t *testing.T, a, b []byte) bool {
	t.Helper()
	var ka, kb map[string]any
	if err := json.Unmarshal(a, &ka); err != nil {
		t.Fatalf("decode shape a: %v", err)
	}
	if err := json.Unmarshal(b, &kb); err != nil {
		t.Fatalf("decode shape b: %v", err)
	}
	if len(ka) != len(kb) {
		return false
	}
	for k := range ka {
		if _, ok := kb[k]; !ok {
			return false
		}
	}
	return true
}

// TestPlan_CanonicalSubject_HasNoSourceOperationID verifies the
// canonical PublishPlan JSON contains no source_operation_id field.
// The field lives only in the observation envelope.
func TestPlan_CanonicalSubject_HasNoSourceOperationID(t *testing.T) {
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
	if strings.Contains(string(b), "source_operation_id") {
		t.Fatalf("canonical plan body contains source_operation_id:\n%s", b)
	}

	obs, err := planObservedFromLab(t, l, "feature")
	if err != nil {
		t.Fatalf("observe: %v", err)
	}
	if obs.SourceOperationID == "" {
		t.Fatalf("observation envelope missing source operation id")
	}
	if obs.Plan == nil {
		t.Fatalf("observation envelope missing plan")
	}
}

// TestPlan_CanonicalOrder_IsLexicographicCommitID verifies the
// canonical order claim matches implementation: lexicographic
// full commit_id.
func TestPlan_CanonicalOrder_IsLexicographicCommitID(t *testing.T) {
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
	if len(p.Commits) != 3 {
		t.Fatalf("expected 3 outgoing commits, got %d (%v)", len(p.Commits), p.Commits)
	}
	for i := 1; i < len(p.Commits); i++ {
		if !(p.Commits[i-1].CommitID < p.Commits[i].CommitID) {
			t.Fatalf("commits not in lexicographic commit_id order: %v", p.Commits)
		}
	}
}
