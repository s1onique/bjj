// docs_test.go gates the durable documentation contracts introduced
// in ACT-BJJ-CHECK01's freeze cleanup. The freeze reviewer
// identified documentation/comment correctness defects (fossil
// phrases that contradict the corrected CORRECTION01
// implementation) and demanded that future drift be caught
// before review.
//
// Each test scans a specific document (or the canonical types.go
// source comment) and asserts that:
//
//   - one or more known-fossil phrases are NO LONGER present, AND
//   - the corresponding replacement phrase IS present
//     (where applicable).
//
// The tests do NOT perform whole-document snapshots; they only
// check the targeted semantic fossils, mirroring the
// admission/docs_test.go style.
package check_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// checkRepoRoot walks up from the current test file's directory
// until it finds go.mod. That directory is the repo root and the
// base for every docs/ path used by these tests.
func checkRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for i := 0; i < 16; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	t.Fatalf("could not locate repo root from cwd=%q", dir)
	return ""
}

// readCheckDoc reads path relative to the repo root and fails the
// test if the file is missing.
func readCheckDoc(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

// TestDocs_CheckSubjectIsFourFields guards
// ACT-BJJ-CHECK01's documentation against the fossil 5-field
// subject claim. CORRECTION01 restored SubjectIdentity to the
// 4-field shape (remote, bookmark, old_commit_id, new_commit_id)
// and removed source_operation_id from the canonical subject.
// The freeze-threshold subject paragraph and the
// subject-identity binding paragraph must both state the 4-field
// contract. The fossil pattern is allowed only inside the
// CORRECTION01 historical-narrative addendum (which is clearly
// marked as historical prose).
//
// This gate is required as DOC_CHECK_CANONICAL_SUBJECT_IS_FOUR_FIELDS.
func TestDocs_CheckSubjectIsFourFields(t *testing.T) {
	body := readCheckDoc(t, filepath.Join(checkRepoRoot(t), "docs/acts/ACT-BJJ-CHECK01.md"))

	// Fossil: any line stating the pre-CORRECTION01 5-field
	// tuple (with the bare "old, new" form) anywhere in the file.
	fossilForms := []string{
		"(remote, bookmark, old, new, opID)",
		"(remote, bookmark, old, new, opID) tuple",
	}
	for _, p := range fossilForms {
		if strings.Contains(body, p) {
			t.Fatalf("ACT-BJJ-CHECK01.md still contains the fossil 5-field "+
				"subject tuple %q. The canonical SubjectIdentity has exactly "+
				"4 fields: (remote, bookmark, old_commit_id, new_commit_id). "+
				"source_operation_id is observation-only provenance.", p)
		}
	}

	// Fossil: any prose claiming CheckResult.Subject has "five fields".
	if strings.Contains(body, "The five fields") ||
		strings.Contains(body, "the five fields") {
		t.Fatalf("ACT-BJJ-CHECK01.md still claims CheckResult.Subject has " +
			"five fields. SubjectIdentity has exactly four fields.")
	}

	// Required: the freeze-threshold subject line must reference
	// the 4-field contract.
	if !strings.Contains(body, "old_commit_id, new_commit_id") {
		t.Fatalf("ACT-BJJ-CHECK01.md is missing the 4-field contract " +
			"old_commit_id, new_commit_id. Add the CORRECTION01 phrasing.")
	}
}

// TestDocs_CheckOpIDObservationOnly guards the boundary between
// canonical SubjectIdentity (4 fields) and observation-only
// source_operation_id. The durable prose must explicitly state
// that source_operation_id is observation provenance and is NOT
// part of the canonical SubjectIdentity.
//
// This gate is required as DOC_CHECK_OPERATION_ID_OBSERVATION_ONLY.
func TestDocs_CheckOpIDObservationOnly(t *testing.T) {
	body := readCheckDoc(t, filepath.Join(checkRepoRoot(t), "docs/acts/ACT-BJJ-CHECK01.md"))

	lower := strings.ToLower(body)
	if !strings.Contains(lower, "source_operation_id") {
		t.Fatalf("ACT-BJJ-CHECK01.md must mention source_operation_id " +
			"explicitly so reviewers can see the boundary.")
	}
	if !strings.Contains(lower, "observation provenance") {
		t.Fatalf("ACT-BJJ-CHECK01.md must state that source_operation_id " +
			"is observation provenance (not canonical).")
	}
}

// TestDocs_CheckTimeoutsMatchCode guards the v1-profile timeout
// table in the durable ACT against the actual CHECK01 timeouts.
// The freeze reviewer identified a fossil:
//
//	go_vet   120s   (correct: 60s)
//	go_test  600s   (correct: 120s)
//	go_build 120s   (correct: 60s)
//
// Any reintroduction of the pre-CORRECTION01 timeout values for
// those three checks in ACT-BJJ-CHECK01.md fails this test.
// The gofmt default (30s) is unchanged.
//
// This gate is required as DOC_CHECK_TIMEOUTS_MATCH_CODE.
func TestDocs_CheckTimeoutsMatchCode(t *testing.T) {
	body := readCheckDoc(t, filepath.Join(checkRepoRoot(t), "docs/acts/ACT-BJJ-CHECK01.md"))

	for _, fossil := range []string{
		"go_vet       go         vet ./...                       120s",
		"go_test      go         test -count=1 ./...             600s",
		"go_build     go         build ./...                     120s",
	} {
		if strings.Contains(body, fossil) {
			t.Fatalf("ACT-BJJ-CHECK01.md still contains the fossil "+
				"timeout row %q. The corrected v1 profile uses "+
				"go_vet=60s, go_test=120s, go_build=60s, gofmt=30s.",
				fossil)
		}
	}

	for _, required := range []string{
		"go_vet       go         vet ./...                       60s",
		"go_test      go         test -count=1 ./...             120s",
		"go_build     go         build ./...                     60s",
	} {
		if !strings.Contains(body, required) {
			t.Fatalf("ACT-BJJ-CHECK01.md is missing the corrected "+
				"timeout row %q.", required)
		}
	}
}

// TestDocs_CheckProgramIdentityMatchesCode guards the durable
// documentation and the source-comment contract for
// CheckSpec.Program / CheckOutcome.Program. After CORRECTION01:
//
//   - Program carries the LOGICAL program name, not an absolute
//     or PATH-resolved path;
//   - ResolvedProgram is recorded only on CheckDiagnostic (which
//     is observation-only).
//
// The fossil pattern "Program is the absolute or PATH-resolved
// executable name" must not appear in any durable document or in
// the CheckSpec / CheckOutcome source comment.
//
// This gate is required as DOC_CHECK_PROGRAM_IDENTITY_MATCHES_CODE.
func TestDocs_CheckProgramIdentityMatchesCode(t *testing.T) {
	root := checkRepoRoot(t)

	fossil := "Program is the absolute or PATH-resolved executable name"

	for _, path := range []string{
		"docs/acts/ACT-BJJ-CHECK01.md",
		"docs/architecture.md",
		"README.md",
	} {
		body := readCheckDoc(t, filepath.Join(root, path))
		if strings.Contains(body, fossil) {
			t.Fatalf("%s still contains the fossil %q. "+
				"Program is the LOGICAL program name; resolved "+
				"executable identity belongs only to "+
				"CheckDiagnostic.ResolvedProgram.",
				path, fossil)
		}
	}

	src := readCheckDoc(t, filepath.Join(root, "internal/check/types.go"))
	if strings.Contains(src, fossil) {
		t.Fatalf("internal/check/types.go still contains the fossil "+
			"%q in a comment. Program is the LOGICAL program name; "+
			"resolved executable identity belongs only to "+
			"CheckDiagnostic.ResolvedProgram.", fossil)
	}

	for _, required := range []string{
		"LOGICAL program name",
		"ResolvedProgram",
	} {
		if !strings.Contains(src, required) {
			t.Fatalf("internal/check/types.go is missing the phrase %q. "+
				"The CheckSpec / CheckOutcome contract must state "+
				"Program is the LOGICAL program name and that resolved "+
				"identity lives on ResolvedProgram.", required)
		}
	}
}

// TestDocs_CheckNoMutableTestTotal guards ACT-BJJ-CHECK01
// against re-introducing a mutable aggregate test count
// (e.g. "195 pass, 0 fail") into durable ACT prose.
//
// The freeze reviewer (LAB01 doctrine) established that
// durable contract + frozen subject ⇒ generated evidence,
// not mutable statistics transcribed into prose. ACT
// documentation is part of the frozen publication contract;
// an integer that drifts on every additional test will
// silently desynchronize from reality.
//
// The fossil phrases are:
//
//   - "Total tests" as a label for a numeric count
//   - "tests pass" / "tests fail" as a bare numeric claim
//     (e.g. "195 pass", "0 fail", "199 tests pass")
//
// Allow-list: this test is itself allowed to mention
// the labels because it documents them. The substring
// matches run against the ACT file only and are bounded
// to a small set of fossil patterns.
//
// This gate is required as DOC_CHECK_HAS_NO_MUTABLE_TEST_TOTAL.
func TestDocs_CheckNoMutableTestTotal(t *testing.T) {
	body := readCheckDoc(t, filepath.Join(checkRepoRoot(t), "docs/acts/ACT-BJJ-CHECK01.md"))

	fossils := []string{
		"Total tests",
		"tests pass",
		"tests fail",
	}
	for _, fossil := range fossils {
		if strings.Contains(body, fossil) {
			t.Fatalf("ACT-BJJ-CHECK01.md still contains the mutable-test-count "+
				"fossil %q. Aggregate test counts are emitted by `go test` on "+
				"each verification run and must NOT be transcribed into "+
				"durable ACT prose. The durable gates are the structural "+
				"guards and the property tests listed below.", fossil)
		}
	}

	for _, required := range []string{
		"Aggregate test counts are reported by `go test`",
		"NOT transcribed into durable prose",
	} {
		if !strings.Contains(body, required) {
			t.Fatalf("ACT-BJJ-CHECK01.md is missing the non-drift phrase %q. "+
				"The durable ACT must explicitly state that aggregate test "+
				"counts are reported by `go test` and are NOT transcribed "+
				"into durable prose.", required)
		}
	}
}
