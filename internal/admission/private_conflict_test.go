package admission_test

import (
	"testing"

	"github.com/s1onique/bjj/internal/admission"
)

// TestEvaluate_PrivateCommit_Deny verifies §9: any private
// outgoing commit denies.
func TestEvaluate_PrivateCommit_Deny(t *testing.T) {
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
			PrivateCommits:    []admission.CommitRef{{CommitID: "new0000000000000000000000000000000000aaaa", ChangeID: "ch1"}},
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
	found := false
	for _, r := range res.Reasons {
		if r.Code == admission.ReasonPrivateCommit {
			found = true
		}
	}
	if !found {
		t.Fatalf("reasons=%+v, want PRIVATE_COMMIT", res.Reasons)
	}
}

// TestEvaluate_EmptyDescription_Deny verifies §12.
func TestEvaluate_EmptyDescription_Deny(t *testing.T) {
	in := admission.AdmissionInput{
		Plan: stubPlanView{
			remote: "lab", bookmark: "feature",
			oldID: "", newID: "new0000000000000000000000000000000000aaaa",
			status: "planned",
		},
		Facts: admission.AdmissionFacts{
			SourceOperationID:       "op_test",
			MoveRelation:            admission.MoveCreate,
			OutgoingCommits:         []admission.CommitRef{{CommitID: "new0000000000000000000000000000000000aaaa", ChangeID: "ch1"}},
			EmptyDescriptionCommits: []admission.CommitRef{{CommitID: "new0000000000000000000000000000000000aaaa", ChangeID: "ch1"}},
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
	found := false
	for _, r := range res.Reasons {
		if r.Code == admission.ReasonEmptyDescription {
			found = true
		}
	}
	if !found {
		t.Fatalf("reasons=%+v, want EMPTY_DESCRIPTION", res.Reasons)
	}
}

// TestEvaluate_ConflictedCommit_Deny verifies §11.
func TestEvaluate_ConflictedCommit_Deny(t *testing.T) {
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
			ConflictedCommits: []admission.CommitRef{{CommitID: "new0000000000000000000000000000000000aaaa", ChangeID: "ch1"}},
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
	found := false
	for _, r := range res.Reasons {
		if r.Code == admission.ReasonConflictedCommit {
			found = true
		}
	}
	if !found {
		t.Fatalf("reasons=%+v, want CONFLICTED_COMMIT", res.Reasons)
	}
}

// TestEvaluate_ConflictedCommit_AllowedByPolicy verifies the
// AllowConflictedCommits escape hatch.
func TestEvaluate_ConflictedCommit_AllowedByPolicy(t *testing.T) {
	pol := admission.DefaultPolicy()
	pol.AllowConflictedCommits = true
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
			ConflictedCommits: []admission.CommitRef{{CommitID: "new0000000000000000000000000000000000aaaa", ChangeID: "ch1"}},
		},
		Policy: pol,
	}
	res, err := admission.Evaluate(in)
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if res.Decision != admission.DecisionAdmit {
		t.Fatalf("decision=%s, want admit", res.Decision)
	}
}
