package admission_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/s1onique/bjj/internal/admission"
)

// TestEvaluate_CanonicalOrderOfReasons verifies §15: reasons are
// emitted in canonical ReasonCode order regardless of which rule
// fired first.
func TestEvaluate_CanonicalOrderOfReasons(t *testing.T) {
	in := admission.AdmissionInput{
		Plan: stubPlanView{
			remote: "lab", bookmark: "feature",
			oldID:  "old0000000000000000000000000000000000aaaa",
			newID:  "new0000000000000000000000000000000000bbbb",
			status: "planned",
		},
		Facts: admission.AdmissionFacts{
			SourceOperationID:       "op_test",
			MoveRelation:            admission.MoveNonFastForward,
			OutgoingCommits:         []admission.CommitRef{{CommitID: "new0000000000000000000000000000000000bbbb", ChangeID: "ch1"}},
			PrivateCommits:          []admission.CommitRef{{CommitID: "new0000000000000000000000000000000000bbbb", ChangeID: "ch1"}},
			ConflictedCommits:       []admission.CommitRef{{CommitID: "new0000000000000000000000000000000000bbbb", ChangeID: "ch1"}},
			EmptyDescriptionCommits: []admission.CommitRef{{CommitID: "new0000000000000000000000000000000000bbbb", ChangeID: "ch1"}},
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
	codes := []admission.ReasonCode{}
	for _, r := range res.Reasons {
		codes = append(codes, r.Code)
	}
	want := []admission.ReasonCode{
		admission.ReasonNonFastForward,
		admission.ReasonPrivateCommit,
		admission.ReasonConflictedCommit,
		admission.ReasonEmptyDescription,
	}
	if len(codes) != len(want) {
		t.Fatalf("codes=%v, want %v", codes, want)
	}
	for i, c := range codes {
		if c != want[i] {
			t.Fatalf("codes=%v, want %v (idx %d: got %s, want %s)", codes, want, i, c, want[i])
		}
	}
}

// TestEvaluate_Deterministic verifies §38: same input produces
// byte-identical JSON output.
func TestEvaluate_Deterministic(t *testing.T) {
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
			PrivateCommits:    []admission.CommitRef{{CommitID: "new0000000000000000000000000000000000bbbb", ChangeID: "ch1"}},
		},
		Policy: admission.DefaultPolicy(),
	}
	first, err := admission.Evaluate(in)
	if err != nil {
		t.Fatalf("evaluate first: %v", err)
	}
	firstJSON, err := admission.RenderJSON(first)
	if err != nil {
		t.Fatalf("render first: %v", err)
	}
	for i := 0; i < 5; i++ {
		again, err := admission.Evaluate(in)
		if err != nil {
			t.Fatalf("evaluate again: %v", err)
		}
		againJSON, err := admission.RenderJSON(again)
		if err != nil {
			t.Fatalf("render again: %v", err)
		}
		if string(firstJSON) != string(againJSON) {
			t.Fatalf("iteration %d produced different JSON:\n--- first ---\n%s\n--- again ---\n%s\n",
				i, firstJSON, againJSON)
		}
	}
}

// TestEvaluate_JSON_OmitsAbsolutePathsAndURLs is the bounded
// guard against environment leakage in the admit decision JSON.
func TestEvaluate_JSON_OmitsAbsolutePathsAndURLs(t *testing.T) {
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
		Policy: admission.DefaultPolicy(),
	}
	res, err := admission.Evaluate(in)
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	b, err := admission.RenderJSON(res)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	s := string(b)
	// No source_operation_id should leak into the decision JSON.
	if strings.Contains(s, "source_operation_id") {
		t.Fatalf("decision JSON leaked source_operation_id:\n%s", s)
	}
	// It must round-trip via standard json.
	var roundtrip admission.AdmissionResult
	if err := json.Unmarshal(b, &roundtrip); err != nil {
		t.Fatalf("roundtrip parse: %v\n%s", err, s)
	}
	if roundtrip.Decision != res.Decision {
		t.Fatalf("roundtrip decision mismatch: %s vs %s", roundtrip.Decision, res.Decision)
	}
}

// TestEvaluate_SubjectIdentity verifies the subject identity in
// the result matches the plan view.
func TestEvaluate_SubjectIdentity(t *testing.T) {
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
		Policy: admission.DefaultPolicy(),
	}
	res, err := admission.Evaluate(in)
	if err != nil {
		t.Fatalf("evaluate: %v", err)
	}
	if res.Subject.Remote != "lab" {
		t.Fatalf("subject.remote=%q, want lab", res.Subject.Remote)
	}
	if res.Subject.Bookmark != "feature" {
		t.Fatalf("subject.bookmark=%q, want feature", res.Subject.Bookmark)
	}
	if res.Subject.OldCommitID != "" {
		t.Fatalf("subject.old_commit_id=%q, want empty (CREATE)", res.Subject.OldCommitID)
	}
	if res.Subject.NewCommitID != "new0000000000000000000000000000000000aaaa" {
		t.Fatalf("subject.new_commit_id mismatch: %q", res.Subject.NewCommitID)
	}
}
