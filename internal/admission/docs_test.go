// docs_test.go gates the durable documentation contracts introduced
// in ACT-BJJ-ADMISSION01-CORRECTION02 §3.
//
// The previous review identified stale prose passages that
// described the pre-CORRECTION01 implementation. These tests fail
// if any of the known fossil phrases reappear in the durable
// documents, so future drift has a chance of being caught before
// code review.
//
// Each test scans a specific document and asserts that:
//
//   - one or more known-fossil phrases are NO LONGER present, AND
//   - the corresponding replacement phrase IS present.
//
// The tests do NOT perform whole-document snapshots; they only
// check the targeted semantic fossils, in line with the
// CORRECTION02 §4 directive.
package admission_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// repoRoot walks up from the current test file's directory until
// it finds go.mod. That directory is the repo root and the base
// for every docs/ path used by these tests.
func repoRoot(t *testing.T) string {
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

// TestDocs_NoUnknownKeyIgnoredClaim guards ACT-BJJ-ADMISSION01's
// malformed-policy section. CORRECTION01 made unknown keys fail
// closed; CORRECTION02 §3.1 requires the durable text to say so.
func TestDocs_NoUnknownKeyIgnoredClaim(t *testing.T) {
	body := readDoc(t, filepath.Join(repoRoot(t), "docs/acts/ACT-BJJ-ADMISSION01.md"))
	fossil := "Unknown top-level keys are ignored for forward-compatibility"
	if strings.Contains(body, fossil) {
		t.Fatalf("ACT-BJJ-ADMISSION01.md still claims %q\n"+
			"which contradicts the CORRECTION01 fail-closed policy "+
			"schema. Replace with the current text.", fossil)
	}
	if !strings.Contains(body, "Unknown top-level keys FAIL CLOSED under") &&
		!strings.Contains(body, "Unknown keys fail closed") &&
		!strings.Contains(body, "Unknown top-level keys FAIL") {
		t.Fatalf("ACT-BJJ-ADMISSION01.md is missing the CORRECTION01/02 " +
			"replacement claim. Add the fail-closed phrasing.")
	}
}

// TestDocs_ArchitectureNoFourQueryGatherer guards architecture.md
// against the pre-CORRECTION01 description that listed a fourth
// `::NEW ~ root()` query to reconstruct the subject set.
func TestDocs_ArchitectureNoFourQueryGatherer(t *testing.T) {
	body := readDoc(t, filepath.Join(repoRoot(t), "docs/architecture.md"))
	fossil := "outgoing set ::NEW ~ root()"
	if strings.Contains(body, fossil) {
		t.Fatalf("docs/architecture.md still describes a four-query "+
			"gatherer with %q, which is the pre-CORRECTION01 "+
			"contract. Replace with the three-query gatherer that "+
			"takes the subject verbatim from PublishPlan.Commits.", fossil)
	}
	if !strings.Contains(body, "subject & policy.PrivateCommits") &&
		!strings.Contains(body, "subject &  policy.PrivateCommits") {
		t.Fatalf("docs/architecture.md is missing the " +
			"`subject & policy.PrivateCommits` intersection form " +
			"from the fact-gathering contract.")
	}
}

// TestDocs_ArchitecturePlanBridgeLocation guards architecture.md
// against the stale WrapPlan() claim. The concrete
// `*plan.PublishPlan -> admission.PlanView` bridge lives in
// `cmd/bjj/plan_adapter.go`.
func TestDocs_ArchitecturePlanBridgeLocation(t *testing.T) {
	body := readDoc(t, filepath.Join(repoRoot(t), "docs/architecture.md"))
	fossilPatterns := []string{
		"`WrapPlan()` adapter implementation",
		"provides a\n   `WrapPlan()` adapter",
	}
	for _, p := range fossilPatterns {
		if strings.Contains(body, p) {
			t.Fatalf("docs/architecture.md still claims the concrete "+
				"plan -> PlanView adapter lives in internal/plan "+
				"(phrase %q). Per CORRECTION02 the bridge lives in "+
				"cmd/bjj/plan_adapter.go.", p)
		}
	}
	if !strings.Contains(body, "cmd/bjj/plan_adapter.go") {
		t.Fatalf("docs/architecture.md does not mention " +
			"cmd/bjj/plan_adapter.go, the CORRECTION01/02 " +
			"location of the concrete plan bridge.")
	}
}

// TestDocs_AdmissionPlanBridgeLocation applies the same fossil
// check to ACT-BJJ-ADMISSION01.md. We only flag the canonical
// stale phrasing (with backticks, in a present-tense context);
// historical quotes inside CORRECTION02 SUMMARY are allowed.
func TestDocs_AdmissionPlanBridgeLocation(t *testing.T) {
	body := readDoc(t, filepath.Join(repoRoot(t), "docs/acts/ACT-BJJ-ADMISSION01.md"))
	fossilPatterns := []string{
		"`WrapPlan()` adapter implementation",
		"`internal/plan` provides a WrapPlan",
	}
	for _, p := range fossilPatterns {
		if strings.Contains(body, p) {
			t.Fatalf("ACT-BJJ-ADMISSION01.md still claims the concrete "+
				"plan -> PlanView adapter is provided by internal/plan "+
				"(phrase %q). Per CORRECTION02 the bridge lives in "+
				"cmd/bjj/plan_adapter.go.", p)
		}
	}
	if !strings.Contains(body, "cmd/bjj/plan_adapter.go") {
		t.Fatalf("ACT-BJJ-ADMISSION01.md does not mention " +
			"cmd/bjj/plan_adapter.go.")
	}
}

// TestDocs_PrivatePredicateIntersection guards the private-predicate
// semantic. CORRECTION02 §3.2 (the small wording bug) requires
// the durable text to use the intersection form, not the union.
//
// To avoid false positives on historical references inside
// CORRECTION02 SUMMARY prose, we only flag the fossil pattern
// when it appears inside a code-fenced block, i.e. when a line
// containing the pattern is wrapped in triple backticks (the
// fact-gathering contract is always a fenced code block).
func TestDocs_PrivatePredicateIntersection(t *testing.T) {
	unionPatterns := []string{
		"subject | policy.PrivateCommits",
		"subject  |  policy.PrivateCommits",
		"subject|policy.PrivateCommits",
	}
	for _, path := range []string{
		"docs/acts/ACT-BJJ-ADMISSION01.md",
		"docs/architecture.md",
	} {
		body := readDoc(t, filepath.Join(repoRoot(t), path))
		if lineHasFencedUnion(body, unionPatterns) {
			t.Fatalf("%s still claims the private predicate is a "+
				"union inside a code-fenced block. Production code "+
				"uses intersection: subject & policy.PrivateCommits.",
				path)
		}
	}
}

// lineHasFencedUnion scans body and returns true if any
// fossil pattern appears inside a triple-backtick fenced
// block. Historical references in plain prose are ignored.
func lineHasFencedUnion(body string, patterns []string) bool {
	inFence := false
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			inFence = !inFence
			continue
		}
		if !inFence {
			continue
		}
		for _, p := range patterns {
			if strings.Contains(line, p) {
				return true
			}
		}
	}
	return false
}

// readDoc reads path relative to the repo root and fails the test
// if the file is missing.
func readDoc(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

// TestDocs_NoStricterThanTOMLForDuplicates guards
// ACT-BJJ-ADMISSION01-CORRECTION03's documentation-truth
// follow-on. The durable prose used to say the bounded parser
// is "stricter than TOML itself" with respect to duplicate
// keys, but TOML already bans duplicate keys ("Defining a key
// multiple times is invalid", TOML v1.1.0). The correct
// wording is "preserves the fail-closed duplicate-key rule
// that the TOML spec already mandates".
//
// This test only fires on the precise fossil phrase; the
// replacement text ("preserves ... the TOML spec") is allowed
// to appear anywhere, including inside fenced blocks.
func TestDocs_NoStricterThanTOMLForDuplicates(t *testing.T) {
	patterns := []string{
		"stricter than TOML itself",
		"is stricter than TOML",
		"stricter than TOML",
	}
	for _, path := range []string{
		"docs/acts/ACT-BJJ-ADMISSION01.md",
		"docs/architecture.md",
	} {
		body := readDoc(t, filepath.Join(repoRoot(t), path))
		for _, p := range patterns {
			if strings.Contains(body, p) {
				t.Fatalf("%s still contains the fossil %q. "+
					"Duplicate-key detection is required by TOML itself; "+
					"replace with \"preserves the fail-closed duplicate-key "+
					"rule that the TOML spec already mandates\".",
					path, p)
			}
		}
	}
}
