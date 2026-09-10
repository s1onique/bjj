package check

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestAggregate_OrderingIsDeterministic asserts that Aggregate
// produces identical canonical JSON regardless of the order in
// which outcomes are passed in.
//
// Required by ACT-BJJ-CHECK01 §12: CHECK_RESULT_DETERMINISTIC.
func TestAggregate_OrderingIsDeterministic(t *testing.T) {
	subject := SubjectIdentity{
		Remote:      "lab",
		Bookmark:    "feature",
		OldCommitID: "aaaa",
		NewCommitID: "bbbb",
	}
	manifest := &Manifest{
		Files: []FileEntry{
			{Path: "a.go", ContentSHA256: "x", Mode: 0o644, Size: 1},
			{Path: "b.go", ContentSHA256: "y", Mode: 0o644, Size: 1},
		},
	}

	mkOutcome := func(id CheckID, s CheckStatus) CheckOutcome {
		return CheckOutcome{
			ID:      id,
			Program: "/bin/echo",
			Argv:    []string{string(id)},
			Status:  s,
		}
	}

	o1 := mkOutcome(CheckIDGofmt, StatusPass)
	o2 := mkOutcome(CheckIDGoBuild, StatusPass)
	o3 := mkOutcome(CheckIDGoVet, StatusPass)
	o4 := mkOutcome(CheckIDGoTest, StatusPass)

	// Forward order
	r1 := Aggregate(subject, manifest, []CheckOutcome{o1, o2, o3, o4})
	// Reverse order
	r2 := Aggregate(subject, manifest, []CheckOutcome{o4, o3, o2, o1})
	// Mixed order
	r3 := Aggregate(subject, manifest, []CheckOutcome{o3, o1, o4, o2})

	b1, err := RenderJSON(r1)
	if err != nil {
		t.Fatalf("render r1: %v", err)
	}
	b2, err := RenderJSON(r2)
	if err != nil {
		t.Fatalf("render r2: %v", err)
	}
	b3, err := RenderJSON(r3)
	if err != nil {
		t.Fatalf("render r3: %v", err)
	}

	if string(b1) != string(b2) {
		t.Fatalf("r1 != r2:\n%s\nvs\n%s", b1, b2)
	}
	if string(b1) != string(b3) {
		t.Fatalf("r1 != r3:\n%s\nvs\n%s", b1, b3)
	}

	// Confirm the canonical order is by CheckID ascending
	// lexicographic order (the four built-in IDs sort as:
	// "go_build" < "go_test" < "go_vet" < "gofmt").
	want := []CheckID{CheckIDGoBuild, CheckIDGoTest, CheckIDGoVet, CheckIDGofmt}
	for i, w := range want {
		if r1.Checks[i].ID != w {
			t.Fatalf("canonical order wrong at index %d: got %q want %q", i, r1.Checks[i].ID, w)
		}
	}
}

// TestAggregate_StatusFolding confirms the verdict §12 rules:
// any ERROR -> error; else any FAIL -> fail; else pass.
func TestAggregate_StatusFolding(t *testing.T) {
	subject := SubjectIdentity{Remote: "lab", Bookmark: "f", NewCommitID: "n"}
	cases := []struct {
		name string
		in   []CheckOutcome
		want CheckStatus
	}{
		{
			name: "all pass",
			in:   []CheckOutcome{{ID: "a", Status: StatusPass}, {ID: "b", Status: StatusPass}},
			want: StatusPass,
		},
		{
			name: "one fail",
			in:   []CheckOutcome{{ID: "a", Status: StatusPass}, {ID: "b", Status: StatusFail}},
			want: StatusFail,
		},
		{
			name: "one error outranks fail",
			in:   []CheckOutcome{{ID: "a", Status: StatusFail}, {ID: "b", Status: StatusError}},
			want: StatusError,
		},
		{
			name: "errors and passes",
			in:   []CheckOutcome{{ID: "a", Status: StatusPass}, {ID: "b", Status: StatusError}},
			want: StatusError,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Aggregate(subject, nil, c.in).Status
			if got != c.want {
				t.Fatalf("got %q want %q", got, c.want)
			}
		})
	}
}

// TestAggregate_DoesNotLeakDiagnosticFields verifies that
// DurationMillis and WorkspacePath are NOT present in the
// canonical JSON (per ACT-BJJ-CHECK01 §10 "do not include ...
// in canonical JSON").
func TestAggregate_DoesNotLeakDiagnosticFields(t *testing.T) {
	o := CheckOutcome{
		ID:             CheckIDGofmt,
		Program:        "gofmt",
		Argv:           []string{"-l", "."},
		Status:         StatusPass,
		ExitCode:       0,
		DurationMillis: 1234,
		WorkspacePath:  "/tmp/leaked/path",
	}
	r := Aggregate(SubjectIdentity{Remote: "lab", Bookmark: "f", NewCommitID: "n"}, nil, []CheckOutcome{o})
	body, err := RenderJSON(r)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if strings.Contains(string(body), "/tmp/leaked/path") {
		t.Fatalf("WorkspacePath leaked into canonical JSON: %s", body)
	}
	if strings.Contains(string(body), "1234") {
		// "1234" is also a substring of the JSON schema_version
		// field if it happened to be set to that. Our SchemaVersion
		// is 1, so this is safe.
		t.Fatalf("DurationMillis leaked into canonical JSON: %s", body)
	}
}

// TestRenderJSON_StrictShape confirms that RenderJSON produces
// strict, decoder-friendly JSON (no trailing junk, no NaN).
func TestRenderJSON_StrictShape(t *testing.T) {
	r := CheckResult{
		SchemaVersion: SchemaVersion,
		Status:        StatusPass,
		Subject: SubjectIdentity{
			Remote: "lab", Bookmark: "f", NewCommitID: "n",
		},
		Checks: []CheckOutcome{
			{ID: CheckIDGofmt, Program: "gofmt", Argv: []string{"-l", "."}, Status: StatusPass, ExitCode: 0},
		},
		FileCount: 1,
	}
	body, err := RenderJSON(r)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	var back CheckResult
	if err := json.Unmarshal(body, &back); err != nil {
		t.Fatalf("unmarshal: %v\nbody: %s", err, body)
	}
	if back.SchemaVersion != SchemaVersion || back.Status != StatusPass {
		t.Fatalf("roundtrip mismatch: %+v", back)
	}
	if len(back.Checks) != 1 || back.Checks[0].ID != CheckIDGofmt {
		t.Fatalf("roundtrip lost checks: %+v", back.Checks)
	}
}
