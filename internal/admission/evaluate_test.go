package admission_test

import (
	"testing"

	"github.com/s1onique/bjj/internal/admission"
)

// stubPlanView is a hand-rolled PlanView for testing the pure
// evaluator in isolation.
type stubPlanView struct {
	remote, bookmark, oldID, newID, status string
	outgoing                               []admission.CommitRef
}

func (s stubPlanView) RemoteName() string   { return s.remote }
func (s stubPlanView) BookmarkName() string { return s.bookmark }
func (s stubPlanView) OldCommitID() string  { return s.oldID }
func (s stubPlanView) NewCommitID() string  { return s.newID }
func (s stubPlanView) StatusName() string   { return s.status }
func (s stubPlanView) OutgoingCommits() []admission.CommitRef {
	return s.outgoing
}

// TestEvaluate_CreateAdmit is the create-bookmark happy path.
func TestEvaluate_CreateAdmit(t *testing.T) {
	in := admission.AdmissionInput{
		Plan: stubPlanView{
			remote:   "lab",
			bookmark: "feature",
			oldID:    "",
			newID:    "new0000000000000000000000000000000000aaaa",
			status:   "planned",
		},
		Facts: admission.AdmissionFacts{
			SourceOperationID: "op_test",
			MoveRelation:      admission.MoveCreate,
			OutgoingCommits: []admission.CommitRef{
				{CommitID: "new0000000000000000000000000000000000aaaa", ChangeID: "ch1"},
			},
		},
		Policy: admission.DefaultPolicy(),
	}
	res, err := admission.Evaluate(in)
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if res.Decision != admission.DecisionAdmit {
		t.Fatalf("decision=%s, want admit", res.Decision)
	}
	if len(res.Reasons) != 0 {
		t.Fatalf("expected zero reasons, got %+v", res.Reasons)
	}
}

// TestEvaluate_CreateDisabled_Deny verifies that CREATE is
// denied when policy.AllowNewBookmarks is false.
func TestEvaluate_CreateDisabled_Deny(t *testing.T) {
	pol := admission.DefaultPolicy()
	pol.AllowNewBookmarks = false
	in := admission.AdmissionInput{
		Plan: stubPlanView{
			remote: "lab", bookmark: "feature",
			oldID: "", newID: "new0000000000000000000000000000000000aaaa",
			status: "planned",
		},
		Facts: admission.AdmissionFacts{
			SourceOperationID: "op_test",
			MoveRelation:      admission.MoveCreate,
			OutgoingCommits:   []admission.CommitRef{{CommitID: "new0000000000000000000000000000000000aaaa", ChangeID: "ch1"}},
		},
		Policy: pol,
	}
	res, err := admission.Evaluate(in)
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if res.Decision != admission.DecisionDeny {
		t.Fatalf("decision=%s, want deny", res.Decision)
	}
	if len(res.Reasons) != 1 || res.Reasons[0].Code != admission.ReasonNewBookmarkNotAllowed {
		t.Fatalf("reasons=%+v, want one NEW_BOOKMARK_NOT_ALLOWED", res.Reasons)
	}
}

// TestEvaluate_FastForwardAdmit verifies FF moves are admitted
// under the default policy.
func TestEvaluate_FastForwardAdmit(t *testing.T) {
	in := admission.AdmissionInput{
		Plan: stubPlanView{
			remote: "lab", bookmark: "feature",
			oldID:  "old0000000000000000000000000000000000aaaa",
			newID:  "new0000000000000000000000000000000000bbbb",
			status: "planned",
		},
		Facts: admission.AdmissionFacts{
			SourceOperationID: "op_test",
			MoveRelation:      admission.MoveFastForward,
			OutgoingCommits:   []admission.CommitRef{{CommitID: "new0000000000000000000000000000000000bbbb", ChangeID: "ch1"}},
		},
		Policy: admission.DefaultPolicy(),
	}
	res, err := admission.Evaluate(in)
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if res.Decision != admission.DecisionAdmit {
		t.Fatalf("decision=%s, want admit", res.Decision)
	}
}

// TestEvaluate_NonFastForward_DenyDefault verifies non-FF is
// denied by default.
func TestEvaluate_NonFastForward_DenyDefault(t *testing.T) {
	in := admission.AdmissionInput{
		Plan: stubPlanView{
			remote: "lab", bookmark: "feature",
			oldID:  "old0000000000000000000000000000000000aaaa",
			newID:  "new0000000000000000000000000000000000bbbb",
			status: "planned",
		},
		Facts: admission.AdmissionFacts{
			SourceOperationID: "op_test",
			MoveRelation:      admission.MoveNonFastForward,
			OutgoingCommits:   []admission.CommitRef{{CommitID: "new0000000000000000000000000000000000bbbb", ChangeID: "ch1"}},
		},
		Policy: admission.DefaultPolicy(),
	}
	res, err := admission.Evaluate(in)
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if res.Decision != admission.DecisionDeny {
		t.Fatalf("decision=%s, want deny", res.Decision)
	}
	if len(res.Reasons) != 1 || res.Reasons[0].Code != admission.ReasonNonFastForward {
		t.Fatalf("reasons=%+v, want one NON_FAST_FORWARD", res.Reasons)
	}
}

// TestEvaluate_NonFastForward_AllowedByPolicy verifies the
// emergency/rewrite escape hatch.
func TestEvaluate_NonFastForward_AllowedByPolicy(t *testing.T) {
	pol := admission.DefaultPolicy()
	pol.AllowNonFastForward = true
	in := admission.AdmissionInput{
		Plan: stubPlanView{
			remote: "lab", bookmark: "feature",
			oldID:  "old0000000000000000000000000000000000aaaa",
			newID:  "new0000000000000000000000000000000000bbbb",
			status: "planned",
		},
		Facts: admission.AdmissionFacts{
			SourceOperationID: "op_test",
			MoveRelation:      admission.MoveNonFastForward,
			OutgoingCommits:   []admission.CommitRef{{CommitID: "new0000000000000000000000000000000000bbbb", ChangeID: "ch1"}},
		},
		Policy: pol,
	}
	res, err := admission.Evaluate(in)
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if res.Decision != admission.DecisionAdmit {
		t.Fatalf("decision=%s, want admit (policy allows NFF)", res.Decision)
	}
}

// TestEvaluate_NoChange_NotNeeded verifies no-op short-circuits
// to DecisionNotNeeded regardless of policy.
func TestEvaluate_NoChange_NotNeeded(t *testing.T) {
	pol := admission.DefaultPolicy()
	pol.AllowNewBookmarks = false
	pol.AllowNonFastForward = true
	pol.PrivateCommits = "all()"
	pol.RequireDescription = false

	in := admission.AdmissionInput{
		Plan: stubPlanView{
			remote: "lab", bookmark: "feature",
			oldID:  "old0000000000000000000000000000000000aaaa",
			newID:  "old0000000000000000000000000000000000aaaa",
			status: "no_remote_change",
		},
		Facts: admission.AdmissionFacts{
			SourceOperationID: "op_test",
			MoveRelation:      admission.MoveNoChange,
			PrivateCommits:    []admission.CommitRef{{CommitID: "old0000000000000000000000000000000000aaaa", ChangeID: "ch1"}},
		},
		Policy: pol,
	}
	res, err := admission.Evaluate(in)
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if res.Decision != admission.DecisionNotNeeded {
		t.Fatalf("decision=%s, want not_needed", res.Decision)
	}
	if len(res.Reasons) != 1 || res.Reasons[0].Code != admission.ReasonNoRemoteChange {
		t.Fatalf("reasons=%+v, want NO_REMOTE_CHANGE", res.Reasons)
	}
}
