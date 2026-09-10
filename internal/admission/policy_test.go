package admission_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/s1onique/bjj/internal/admission"
)

// TestPolicy_DefaultPolicyMatchesDoc is the bounded check that
// DefaultPolicy() matches the documented defaults in
// ACT-BJJ-ADMISSION01 §24.
func TestPolicy_DefaultPolicyMatchesDoc(t *testing.T) {
	p := admission.DefaultPolicy()
	if !p.AllowNewBookmarks {
		t.Fatalf("AllowNewBookmarks=false, want true")
	}
	if p.AllowNonFastForward {
		t.Fatalf("AllowNonFastForward=true, want false")
	}
	if !p.RequireDescription {
		t.Fatalf("RequireDescription=false, want true")
	}
	if p.AllowConflictedCommits {
		t.Fatalf("AllowConflictedCommits=true, want false")
	}
	if p.PrivateCommits != "none()" {
		t.Fatalf("PrivateCommits=%q, want none()", p.PrivateCommits)
	}
}

// TestPolicy_LoadFromRepo_Absent verifies §23: missing file
// yields DefaultPolicy() with PolicyOutcomeDefault.
func TestPolicy_LoadFromRepo_Absent(t *testing.T) {
	tmp := t.TempDir()
	p, src, err := admission.LoadPolicyFromRepo(tmp)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if src.Outcome != admission.PolicyOutcomeDefault {
		t.Fatalf("outcome=%s, want DEFAULT_POLICY", src.Outcome)
	}
	if p.PrivateCommits != "none()" {
		t.Fatalf("PrivateCommits=%q, want none()", p.PrivateCommits)
	}
	if !p.AllowNewBookmarks {
		t.Fatalf("AllowNewBookmarks=false on default policy")
	}
}

// TestPolicy_LoadFromRepo_Malformed verifies §23: a malformed
// policy file fails closed (CodePolicyInvalid), NOT a fallback
// to default.
func TestPolicy_LoadFromRepo_Malformed(t *testing.T) {
	tmp := t.TempDir()
	body := "this is not a valid policy line\nallow_new_bookmarks = maybe\n"
	if err := os.WriteFile(filepath.Join(tmp, ".bjj", "policy.toml"), []byte(body), 0o644); err != nil {
		// .bjj may not exist; create it.
		if err := os.MkdirAll(filepath.Join(tmp, ".bjj"), 0o755); err != nil {
			t.Fatalf("mkdir .bjj: %v", err)
		}
		if err := os.WriteFile(filepath.Join(tmp, ".bjj", "policy.toml"), []byte(body), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	_, src, err := admission.LoadPolicyFromRepo(tmp)
	if err == nil {
		t.Fatalf("expected error from malformed policy")
	}
	if src.Outcome != admission.PolicyOutcomeMalformed {
		t.Fatalf("outcome=%s, want MALFORMED", src.Outcome)
	}
	var ae *admission.Error
	if !asErr(err, &ae) {
		t.Fatalf("expected *admission.Error, got %T", err)
	}
	if ae.Code != admission.CodePolicyInvalid {
		t.Fatalf("code=%s, want POLICY_INVALID", ae.Code)
	}
}

// TestPolicy_LoadFromRepo_Loaded verifies a well-formed file
// produces the expected policy.
func TestPolicy_LoadFromRepo_Loaded(t *testing.T) {
	tmp := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmp, ".bjj"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	body := strings.Join([]string{
		`schema_version = 1`,
		`allow_new_bookmarks = false`,
		`allow_non_fast_forward = true`,
		`require_description = false`,
		`allow_conflicted_commits = true`,
		`private_commits = "description('private:*')"`,
		"",
	}, "\n")
	if err := os.WriteFile(filepath.Join(tmp, ".bjj", "policy.toml"), []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	p, src, err := admission.LoadPolicyFromRepo(tmp)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if src.Outcome != admission.PolicyOutcomeLoaded {
		t.Fatalf("outcome=%s, want LOADED", src.Outcome)
	}
	if p.AllowNewBookmarks {
		t.Fatalf("AllowNewBookmarks=true, want false")
	}
	if !p.AllowNonFastForward {
		t.Fatalf("AllowNonFastForward=false, want true")
	}
	if p.RequireDescription {
		t.Fatalf("RequireDescription=true, want false")
	}
	if !p.AllowConflictedCommits {
		t.Fatalf("AllowConflictedCommits=false, want true")
	}
	if p.PrivateCommits != `description('private:*')` {
		t.Fatalf("PrivateCommits=%q", p.PrivateCommits)
	}
}

// TestPolicy_AmbientJJPolicyIndependent is the bounded check
// for §10: ambient jujutsu config (e.g. a global
// git.private-commits setting) MUST NOT influence BJJ policy.
//
// BJJ's policy file lives at .bjj/policy.toml inside the repo
// (NOT under ~/.jjconfig). This test asserts that loading with
// no ambient jj config available (the test runner does not
// touch the developer's $HOME) produces the same result as
// loading with any ambient setting.
func TestPolicy_AmbientJJPolicyIndependent(t *testing.T) {
	tmp := t.TempDir()
	// No .bjj/policy.toml present.
	p1, _, err := admission.LoadPolicyFromRepo(tmp)
	if err != nil {
		t.Fatalf("load #1: %v", err)
	}
	// Setting a global env var that looks like a jj config
	// variable must NOT alter the BJJ policy load.
	t.Setenv("JJ_CONFIG", "/tmp/whatever.toml")
	p2, _, err := admission.LoadPolicyFromRepo(tmp)
	if err != nil {
		t.Fatalf("load #2: %v", err)
	}
	if p1.PrivateCommits != p2.PrivateCommits {
		t.Fatalf("PrivateCommits drifted under JJ_CONFIG: %q vs %q", p1.PrivateCommits, p2.PrivateCommits)
	}
	if p1.AllowNewBookmarks != p2.AllowNewBookmarks {
		t.Fatalf("AllowNewBookmarks drifted under JJ_CONFIG")
	}
}

// TestPolicy_UnknownKey_FailsClosed is the CORRECTION01 §4 gate.
// Under schema_version = 1 the parser MUST reject any key it does
// not recognise. Silently ignoring unknown keys would let a typo
// like "private_commmits" leave private_commits at its default
// (none()), silently weakening the policy.
func TestPolicy_UnknownKey_FailsClosed(t *testing.T) {
	tmp := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmp, ".bjj"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	body := strings.Join([]string{
		`schema_version = 1`,
		`allow_new_bookmarks = false`,
		`some_future_field = "ignored"`,
		"",
	}, "\n")
	if err := os.WriteFile(filepath.Join(tmp, ".bjj", "policy.toml"), []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, src, err := admission.LoadPolicyFromRepo(tmp)
	if err == nil {
		t.Fatalf("expected error from unknown policy key")
	}
	if src.Outcome != admission.PolicyOutcomeMalformed {
		t.Fatalf("outcome=%s, want MALFORMED", src.Outcome)
	}
	var ae *admission.Error
	if !asErr(err, &ae) {
		t.Fatalf("expected *admission.Error, got %T", err)
	}
	if ae.Code != admission.CodePolicyInvalid {
		t.Fatalf("code=%s, want POLICY_INVALID", ae.Code)
	}
}

// TestPolicy_TypoCannotWeakenPolicy is the practical corollary:
// a misspelled "private_commmits" (three m's) MUST be rejected.
// Without fail-closed parsing this typo would silently leave
// private_commits at its default "none()", making admission more
// permissive than the policy intends.
func TestPolicy_TypoCannotWeakenPolicy(t *testing.T) {
	tmp := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmp, ".bjj"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	body := strings.Join([]string{
		`schema_version = 1`,
		`private_commmits = "description('private:*')"`, // typo
		"",
	}, "\n")
	if err := os.WriteFile(filepath.Join(tmp, ".bjj", "policy.toml"), []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, src, err := admission.LoadPolicyFromRepo(tmp)
	if err == nil {
		t.Fatalf("expected error from misspelled private_commmits")
	}
	if src.Outcome != admission.PolicyOutcomeMalformed {
		t.Fatalf("outcome=%s, want MALFORMED", src.Outcome)
	}
	var ae *admission.Error
	if !asErr(err, &ae) {
		t.Fatalf("expected *admission.Error, got %T", err)
	}
	if ae.Code != admission.CodePolicyInvalid {
		t.Fatalf("code=%s, want POLICY_INVALID", ae.Code)
	}
}

// asErr is a small wrapper around errors.As to avoid the import
// in every test (we only need it once).
func asErr(err error, target **admission.Error) bool {
	ae, ok := admission.AsError(err)
	if !ok {
		return false
	}
	*target = ae
	return true
}

// writePolicy writes body to .bjj/policy.toml under tmp and
// fails the test on error.
func writePolicy(t *testing.T, tmp, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(tmp, ".bjj"), 0o755); err != nil {
		t.Fatalf("mkdir .bjj: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmp, ".bjj", "policy.toml"), []byte(body), 0o644); err != nil {
		t.Fatalf("write policy: %v", err)
	}
}

// requirePolicyInvalid asserts err is a non-nil
// *admission.Error{CodePolicyInvalid}.
func requirePolicyInvalid(t *testing.T, err error, src admission.PolicySource) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error from policy load; outcome=%s", src.Outcome)
	}
	if src.Outcome != admission.PolicyOutcomeMalformed {
		t.Fatalf("outcome=%s, want MALFORMED", src.Outcome)
	}
	var ae *admission.Error
	if !asErr(err, &ae) {
		t.Fatalf("expected *admission.Error, got %T", err)
	}
	if ae.Code != admission.CodePolicyInvalid {
		t.Fatalf("code=%s, want POLICY_INVALID", ae.Code)
	}
}

// TestPolicy_DuplicateKey_FailsClosed is the CORRECTION02 §1 gate.
// The bounded parser preserves TOML's fail-closed duplicate-key
// rule (per the TOML v1.1.0 spec: "Defining a key multiple times
// is invalid"). Every key, recognised or not, may appear at most
// once in the policy file; a second occurrence produces
// POLICY_INVALID rather than silently picking one value.
func TestPolicy_DuplicateKey_FailsClosed(t *testing.T) {
	tmp := t.TempDir()
	writePolicy(t, tmp, strings.Join([]string{
		`schema_version = 1`,
		`allow_non_fast_forward = false`,
		`allow_non_fast_forward = true`,
		"",
	}, "\n"))
	_, src, err := admission.LoadPolicyFromRepo(tmp)
	requirePolicyInvalid(t, err, src)
	if !strings.Contains(err.Error(), "duplicate policy key") {
		t.Fatalf("error message should mention duplicate; got: %v", err)
	}
	if !strings.Contains(err.Error(), "allow_non_fast_forward") {
		t.Fatalf("error message should name the duplicated key; got: %v", err)
	}
}

// TestPolicy_DuplicateSchemaVersion_FailsClosed exercises the
// duplicate-key path specifically for schema_version, which is
// otherwise a single-line policy declaration.
func TestPolicy_DuplicateSchemaVersion_FailsClosed(t *testing.T) {
	tmp := t.TempDir()
	writePolicy(t, tmp, strings.Join([]string{
		`schema_version = 1`,
		`schema_version = 1`,
		`private_commits = "none()"`,
		"",
	}, "\n"))
	_, src, err := admission.LoadPolicyFromRepo(tmp)
	requirePolicyInvalid(t, err, src)
}

// TestPolicy_DuplicateCannotWeakenPolicy is the practical
// corollary: a duplicate of a security-relevant key MUST NOT
// silently win. The point of duplicate detection is that
// generated/merged policy files cannot quietly weaken
// private_commits, allow_non_fast_forward, etc.
func TestPolicy_DuplicateCannotWeakenPolicy(t *testing.T) {
	tmp := t.TempDir()
	writePolicy(t, tmp, strings.Join([]string{
		`schema_version = 1`,
		`private_commits = "description('private:*')"`,
		`private_commits = "none()"`,
		"",
	}, "\n"))
	_, src, err := admission.LoadPolicyFromRepo(tmp)
	requirePolicyInvalid(t, err, src)
	if !strings.Contains(err.Error(), "duplicate policy key") ||
		!strings.Contains(err.Error(), "private_commits") {
		t.Fatalf("error should name the duplicated private_commits key; got: %v", err)
	}
}

// TestPolicy_SchemaVersionRequired_PresentFileMissingSchema
// is the CORRECTION02 §2 gate. A file that is present MUST
// declare exactly one schema_version. Otherwise it is
// POLICY_INVALID, even if every other key is valid.
func TestPolicy_SchemaVersionRequired_PresentFileMissingSchema(t *testing.T) {
	tmp := t.TempDir()
	writePolicy(t, tmp, strings.Join([]string{
		`allow_non_fast_forward = true`,
		`require_description = false`,
		``,
	}, "\n"))
	_, src, err := admission.LoadPolicyFromRepo(tmp)
	requirePolicyInvalid(t, err, src)
	if !strings.Contains(err.Error(), "schema_version") {
		t.Fatalf("error should mention missing schema_version; got: %v", err)
	}
}

// TestPolicy_SchemaVersionRequired_DuplicateFails verifies the
// "exactly one schema_version" half of the §2 contract.
func TestPolicy_SchemaVersionRequired_DuplicateFails(t *testing.T) {
	tmp := t.TempDir()
	writePolicy(t, tmp, strings.Join([]string{
		`schema_version = 1`,
		`schema_version = 1`,
		`private_commits = "none()"`,
		``,
	}, "\n"))
	_, src, err := admission.LoadPolicyFromRepo(tmp)
	requirePolicyInvalid(t, err, src)
}

// TestPolicy_SchemaVersionRequired_UnsupportedFails verifies the
// "must be a supported schema" half of §2. Today only "1" is
// recognised; an unsupported value is POLICY_INVALID.
func TestPolicy_SchemaVersionRequired_UnsupportedFails(t *testing.T) {
	tmp := t.TempDir()
	writePolicy(t, tmp, strings.Join([]string{
		`schema_version = 99`,
		`private_commits = "none()"`,
		``,
	}, "\n"))
	_, src, err := admission.LoadPolicyFromRepo(tmp)
	requirePolicyInvalid(t, err, src)
	if !strings.Contains(err.Error(), "schema_version") {
		t.Fatalf("error should mention schema_version; got: %v", err)
	}
}

// TestPolicy_AbsentFileStillUsesDefault protects the §2 carve-out:
// "file absent" must still return DefaultPolicy with
// PolicyOutcomeDefault. The new schema_version requirement is
// strictly for present files.
func TestPolicy_AbsentFileStillUsesDefault(t *testing.T) {
	tmp := t.TempDir()
	p, src, err := admission.LoadPolicyFromRepo(tmp)
	if err != nil {
		t.Fatalf("absent-file load: %v", err)
	}
	if src.Outcome != admission.PolicyOutcomeDefault {
		t.Fatalf("outcome=%s, want DEFAULT_POLICY", src.Outcome)
	}
	if p.SchemaVersion != 1 {
		t.Fatalf("SchemaVersion=%d on default policy, want 1", p.SchemaVersion)
	}
	if p.PrivateCommits != "none()" {
		t.Fatalf("PrivateCommits=%q on default policy, want none()", p.PrivateCommits)
	}
}
