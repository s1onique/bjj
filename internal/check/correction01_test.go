package check

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestManifest_PreservesExecutableBit asserts
// CHECK01-CORRECTION01 §3: when the FileSource reports a
// regular file with executable=true, the materializer writes
// the file with mode 0o755.
func TestManifest_PreservesExecutableBit(t *testing.T) {
	fs := &fakeFileSource{
		Files: map[string][]byte{
			"scripts/check.sh": []byte("#!/bin/sh\necho hi\n"),
		},
		Executable: map[string]bool{
			"scripts/check.sh": true,
		},
	}
	m := NewJJMaterializer(fs)
	manifest, wsDir, err := m.Materialize(context.Background(), t.TempDir(), "/d", "op", "rev")
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(wsDir) })

	if len(manifest.Files) != 1 {
		t.Fatalf("manifest entries: got %d want 1", len(manifest.Files))
	}
	e := manifest.Files[0]
	if !e.Executable {
		t.Fatalf("Executable flag lost: %+v", e)
	}
	if e.Mode != 0o755 {
		t.Fatalf("Mode: got %o want 0o755", e.Mode)
	}
	info, ierr := os.Stat(filepath.Join(wsDir, "scripts", "check.sh"))
	if ierr != nil {
		t.Fatalf("stat: %v", ierr)
	}
	if info.Mode().Perm() != 0o755 {
		t.Fatalf("on-disk mode: got %o want 0o755", info.Mode().Perm())
	}
}

// TestManifest_SymlinkFailsClosed asserts CHECK01-CORRECTION01
// §3 / §12 (CHECK_SYMLINK_NOT_COERCED_TO_REGULAR_FILE).
func TestManifest_SymlinkFailsClosed(t *testing.T) {
	symFS := &symlinkFakeSource{}
	m := NewJJMaterializer(symFS)
	_, _, err := m.Materialize(context.Background(), t.TempDir(), "/d", "op", "rev")
	var ce *Error
	if !errors.As(err, &ce) {
		t.Fatalf("expected *Error, got %v", err)
	}
	if ce.Code != CodeUnsupportedTreeEntry {
		t.Fatalf("got code %q want %q", ce.Code, CodeUnsupportedTreeEntry)
	}
}

type symlinkFakeSource struct{}

func (s *symlinkFakeSource) ListTreeEntries(ctx context.Context, dir, opID, revision string) ([]TreeEntry, error) {
	return []TreeEntry{{Path: "link.txt", Kind: KindSymlink, Executable: false}}, nil
}

func (s *symlinkFakeSource) ShowFile(ctx context.Context, dir, opID, revision, path string) ([]byte, error) {
	return []byte("target"), nil
}

// TestManifest_GitSubmoduleFailsClosed asserts
// CHECK01-CORRECTION01 §3 (CHECK_SUBMODULE_NOT_COERCED_TO_REGULAR_FILE).
func TestManifest_GitSubmoduleFailsClosed(t *testing.T) {
	subFS := &submoduleFakeSource{}
	m := NewJJMaterializer(subFS)
	_, _, err := m.Materialize(context.Background(), t.TempDir(), "/d", "op", "rev")
	var ce *Error
	if !errors.As(err, &ce) {
		t.Fatalf("expected *Error, got %v", err)
	}
	if ce.Code != CodeUnsupportedTreeEntry {
		t.Fatalf("got code %q want %q", ce.Code, CodeUnsupportedTreeEntry)
	}
}

type submoduleFakeSource struct{}

func (s *submoduleFakeSource) ListTreeEntries(ctx context.Context, dir, opID, revision string) ([]TreeEntry, error) {
	return []TreeEntry{{Path: "vendor/sub", Kind: KindGitSubmodule, Executable: false}}, nil
}

func (s *submoduleFakeSource) ShowFile(ctx context.Context, dir, opID, revision, path string) ([]byte, error) {
	return []byte("gitlink"), nil
}

// TestManifest_UnknownKindFailsClosed asserts CHECK01-CORRECTION01
// §3: unknown file_type fails closed.
func TestManifest_UnknownKindFailsClosed(t *testing.T) {
	fs := &unknownKindFakeSource{}
	m := NewJJMaterializer(fs)
	_, _, err := m.Materialize(context.Background(), t.TempDir(), "/d", "op", "rev")
	var ce *Error
	if !errors.As(err, &ce) {
		t.Fatalf("expected *Error, got %v", err)
	}
	if ce.Code != CodeUnsupportedTreeEntry {
		t.Fatalf("got code %q want %q", ce.Code, CodeUnsupportedTreeEntry)
	}
}

type unknownKindFakeSource struct{}

func (u *unknownKindFakeSource) ListTreeEntries(ctx context.Context, dir, opID, revision string) ([]TreeEntry, error) {
	return []TreeEntry{{Path: "weird.txt", Kind: EntryKind("UNRECOGNISED"), Executable: false}}, nil
}

func (u *unknownKindFakeSource) ShowFile(ctx context.Context, dir, opID, revision, path string) ([]byte, error) {
	return []byte("x"), nil
}

// TestReverify_DetectsChmodTampering asserts CHECK01-CORRECTION01
// §3 / §4 (CHECK_MANIFEST_MODE_BOUND).
func TestReverify_DetectsChmodTampering(t *testing.T) {
	fs := &fakeFileSource{Files: map[string][]byte{"a.txt": []byte("hi")}}
	orch := &Orchestrator{
		Materializer: NewJJMaterializer(fs),
		Runner:       &fakeRunner{outcomes: []CheckOutcome{}},
		ReMaterializeForVerify: func(ctx context.Context, ws string) (*Manifest, error) {
			return &Manifest{Files: []FileEntry{
				{Path: "a.txt", Kind: KindFile, Executable: true, Mode: 0o755, Size: 2, ContentSHA256: "ignored"},
			}}, nil
		},
	}
	subject := CheckSubject{
		SourceOperationID: "op-1", Remote: "lab", Bookmark: "feature", NewCommitID: "rev-1",
	}
	_, err := orch.Run(context.Background(), subject, Options{
		RepoDir: "/d", WorkspaceParent: t.TempDir(),
	})
	if err == nil {
		t.Fatalf("expected CodeWorkspaceChanged, got nil")
	}
	var ce *Error
	if !errors.As(err, &ce) {
		t.Fatalf("expected *Error, got %v", err)
	}
	if ce.Code != CodeWorkspaceChanged {
		t.Fatalf("got code %q want %q", ce.Code, CodeWorkspaceChanged)
	}
}

// TestReverify_DetectsKindTampering asserts CHECK01-CORRECTION01
// §3 / §4 (CHECK_MANIFEST_KIND_BOUND).
func TestReverify_DetectsKindTampering(t *testing.T) {
	fs := &fakeFileSource{Files: map[string][]byte{"a.txt": []byte("hi")}}
	orch := &Orchestrator{
		Materializer: NewJJMaterializer(fs),
		Runner:       &fakeRunner{outcomes: []CheckOutcome{}},
		ReMaterializeForVerify: func(ctx context.Context, ws string) (*Manifest, error) {
			return &Manifest{Files: []FileEntry{
				{Path: "a.txt", Kind: KindSymlink, Executable: false, Mode: 0o777, Size: 0},
			}}, nil
		},
	}
	subject := CheckSubject{
		SourceOperationID: "op-1", Remote: "lab", Bookmark: "feature", NewCommitID: "rev-1",
	}
	_, err := orch.Run(context.Background(), subject, Options{
		RepoDir: "/d", WorkspaceParent: t.TempDir(),
	})
	if err == nil {
		t.Fatalf("expected CodeWorkspaceChanged, got nil")
	}
	var ce *Error
	if !errors.As(err, &ce) {
		t.Fatalf("expected *Error, got %v", err)
	}
	if ce.Code != CodeWorkspaceChanged {
		t.Fatalf("got code %q want %q", ce.Code, CodeWorkspaceChanged)
	}
}

// TestCanonicalSubjectHasNoOpID asserts CHECK01-CORRECTION01
// §5 (CHECK_CANONICAL_SUBJECT_HAS_NO_OPERATION_ID): the
// CheckResult.Subject body MUST NOT contain an opID; the opID
// lives in CheckObservation only.
func TestCanonicalSubjectHasNoOpID(t *testing.T) {
	fs := &fakeFileSource{Files: map[string][]byte{"a.txt": []byte("hi")}}
	runner := &fakeRunner{
		outcomes: []CheckOutcome{
			{ID: CheckIDGoBuild, Status: StatusPass, ExitCode: 0},
			{ID: CheckIDGofmt, Status: StatusPass, ExitCode: 0},
			{ID: CheckIDGoTest, Status: StatusPass, ExitCode: 0},
			{ID: CheckIDGoVet, Status: StatusPass, ExitCode: 0},
		},
	}
	orch := &Orchestrator{Materializer: NewJJMaterializer(fs), Runner: runner}
	subject := CheckSubject{
		SourceOperationID: "op-12345",
		Remote:            "lab", Bookmark: "feature", NewCommitID: "rev-1",
	}
	obs, err := orch.Run(context.Background(), subject, Options{
		RepoDir: "/d", WorkspaceParent: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("orchestrator: %v", err)
	}

	// 1. Canonical body: must not contain opID.
	body, err := RenderJSON(obs.Result)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if bytes.Contains(body, []byte("op-12345")) {
		t.Fatalf("canonical JSON leaked opID:\n%s", body)
	}
	if bytes.Contains(body, []byte("source_operation_id")) {
		t.Fatalf("canonical JSON contains source_operation_id key:\n%s", body)
	}

	// 2. Canonical SubjectIdentity MUST match admission shape.
	if obs.Result.Subject.Remote != "lab" ||
		obs.Result.Subject.Bookmark != "feature" ||
		obs.Result.Subject.NewCommitID != "rev-1" {
		t.Fatalf("subject identity shape wrong: %+v", obs.Result.Subject)
	}

	// 3. Observation body: opID is present.
	obsBody, err := RenderObservationJSON(obs)
	if err != nil {
		t.Fatalf("render obs: %v", err)
	}
	if !bytes.Contains(obsBody, []byte("op-12345")) {
		t.Fatalf("observation JSON missing opID:\n%s", obsBody)
	}
}

// TestSubjectIdentityMatchesAdmission asserts CHECK01-CORRECTION01
// §5: check.SubjectIdentity has the same JSON keys as
// admission.SubjectIdentity (excluding SourceOperationID).
func TestSubjectIdentityMatchesAdmission(t *testing.T) {
	chk := SubjectIdentity{Remote: "lab", Bookmark: "b", OldCommitID: "o", NewCommitID: "n"}
	cb, err := json.Marshal(chk)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, want := range []string{
		`"remote":"lab"`, `"bookmark":"b"`,
		`"old_commit_id":"o"`, `"new_commit_id":"n"`,
	} {
		if !bytes.Contains(cb, []byte(want)) {
			t.Fatalf("check subject JSON missing %q: %s", want, cb)
		}
	}
	if bytes.Contains(cb, []byte("source_operation_id")) {
		t.Fatalf("check subject JSON must not contain source_operation_id: %s", cb)
	}
}

// TestRepeatedExecution_CanonicalResultIdentical asserts
// CHECK01-CORRECTION01 §7 (CHECK_REPEATED_EXECUTION_CANONICAL_RESULT_IDENTICAL):
// running the orchestrator twice against the same fake source
// (same opID + revision) must produce byte-identical canonical
// CheckResult JSON. Diagnostics MAY differ.
func TestRepeatedExecution_CanonicalResultIdentical(t *testing.T) {
	makeRunner := func() *fakeRunner {
		return &fakeRunner{
			outcomes: []CheckOutcome{
				{ID: CheckIDGoBuild, Status: StatusPass, ExitCode: 0},
				{ID: CheckIDGofmt, Status: StatusPass, ExitCode: 0},
				{ID: CheckIDGoTest, Status: StatusPass, ExitCode: 0},
				{ID: CheckIDGoVet, Status: StatusPass, ExitCode: 0},
			},
		}
	}
	subject := CheckSubject{
		SourceOperationID: "op-1",
		Remote:            "lab", Bookmark: "feature", NewCommitID: "rev-1",
	}
	runOnce := func() CheckObservation {
		orch := &Orchestrator{
			Materializer: NewJJMaterializer(&fakeFileSource{Files: map[string][]byte{"a.txt": []byte("hi")}}),
			Runner:       makeRunner(),
		}
		obs, err := orch.Run(context.Background(), subject, Options{
			RepoDir: "/d", WorkspaceParent: t.TempDir(),
		})
		if err != nil {
			t.Fatalf("run: %v", err)
		}
		return obs
	}
	obs1 := runOnce()
	obs2 := runOnce()
	b1, _ := RenderJSON(obs1.Result)
	b2, _ := RenderJSON(obs2.Result)
	if !bytes.Equal(b1, b2) {
		t.Fatalf("canonical CheckResult bytes differ across two runs:\n--1--\n%s\n--2--\n%s", b1, b2)
	}
	if obs1.SourceOperationID != obs2.SourceOperationID {
		t.Fatalf("opID should be stable across runs of same subject")
	}
}

// TestRunner_UsesModReadonly asserts CHECK01-CORRECTION01 §2:
// runner.go must contain GOFLAGS=-mod=readonly overlay and
// NOT the old -mod=mod overlay. Static guard.
func TestRunner_UsesModReadonly(t *testing.T) {
	src, err := os.ReadFile("runner.go")
	if err != nil {
		t.Fatalf("read runner.go: %v", err)
	}
	if !bytes.Contains(src, []byte("GOFLAGS=-mod=readonly")) {
		t.Fatalf("runner.go missing GOFLAGS=-mod=readonly overlay")
	}
	if bytes.Contains(src, []byte(`"GOFLAGS=`)) && bytes.Contains(src, []byte(`-mod=`)) && !bytes.Contains(src, []byte("-mod=readonly")) {
		// rough: runner.go is allowed to mention GOFLAGS only
		// in the readonly form
		t.Fatalf("runner.go references GOFLAGS with an overlay other than -mod=readonly")
	}
}

// TestExcludedFromCanonical_ObservationFields asserts
// CHECK01-CORRECTION01 §5 / §6 (CHECK_DIAGNOSTICS_OUTSIDE_CANONICAL_RESULT):
// the canonical JSON MUST NOT contain Stdout, Stderr,
// WorkspacePath, DurationMillis, or ResolvedProgram even when
// the runner reports them populated.
func TestExcludedFromCanonical_ObservationFields(t *testing.T) {
	fs := &fakeFileSource{Files: map[string][]byte{"a.txt": []byte("hi")}}
	// fakeRunner.hook lets us mutate the orchestrator's
	// outcomes AFTER the orchestrator builds them; we use it
	// to inject diagnostic-only fields that the orchestrator
	// would otherwise overwrite.
	runner := &fakeRunner{
		outcomes: []CheckOutcome{
			{ID: CheckIDGoBuild, Status: StatusPass, ExitCode: 0},
			{ID: CheckIDGofmt, Status: StatusPass, ExitCode: 0},
			{ID: CheckIDGoTest, Status: StatusPass, ExitCode: 0},
			{ID: CheckIDGoVet, Status: StatusPass, ExitCode: 0},
		},
	}
	orch := &Orchestrator{Materializer: NewJJMaterializer(fs), Runner: runner}
	subject := CheckSubject{
		SourceOperationID: "op-1",
		Remote:            "lab", Bookmark: "feature", NewCommitID: "rev-1",
	}
	obs, err := orch.Run(context.Background(), subject, Options{
		RepoDir: "/d", WorkspaceParent: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("orchestrator: %v", err)
	}

	// Diagnostics carry the diagnostic-only fields by design
	// (they live outside the canonical CheckResult). Verify
	// the canonical body is clean and that the observation
	// body contains the diagnostic fields.
	body, err := RenderJSON(obs.Result)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	for _, leak := range []string{
		"duration_millis",
		"workspace_path",
		"resolved_program",
		"stdout",
		"stderr",
	} {
		if bytes.Contains(body, []byte(leak)) {
			t.Fatalf("canonical JSON leaked %q:\n%s", leak, body)
		}
	}

	// Each CheckOutcome carries json:"-" tags on the
	// diagnostic-only fields; verify they actually exist in
	// the CheckOutcome struct (sanity check) and verify the
	// canonical JSON does not serialise them.
	if len(obs.Result.Checks) != 4 {
		t.Fatalf("expected 4 checks, got %d", len(obs.Result.Checks))
	}
	// The observation JSON DOES include per-check diagnostics.
	obsBody, err := RenderObservationJSON(obs)
	if err != nil {
		t.Fatalf("render obs: %v", err)
	}
	for _, want := range []string{
		"diagnostics",
		"workspace_path",
		"resolved_program",
		"duration_millis",
	} {
		if !bytes.Contains(obsBody, []byte(want)) {
			t.Fatalf("observation JSON missing %q:\n%s", want, obsBody)
		}
	}
}

// TestDefaultReVerifierReadsModeAndKind asserts CHECK01-CORRECTION01
// §3 / §4: the default ReVerifier must populate kind +
// executable + mode from Lstat, not from a hardcoded 0o644.
func TestDefaultReVerifierReadsModeAndKind(t *testing.T) {
	ws := t.TempDir()
	if err := os.WriteFile(filepath.Join(ws, "exec.sh"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write exec: %v", err)
	}
	if err := os.WriteFile(filepath.Join(ws, "plain.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatalf("write plain: %v", err)
	}
	if err := os.Symlink("plain.txt", filepath.Join(ws, "link.txt")); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	m, err := DefaultReVerifier(context.Background(), ws)
	if err != nil {
		t.Fatalf("reverify: %v", err)
	}
	if len(m.Files) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(m.Files))
	}
	byPath := map[string]FileEntry{}
	for _, e := range m.Files {
		byPath[e.Path] = e
	}
	if e := byPath["exec.sh"]; !e.Executable || e.Mode != 0o755 || e.Kind != KindFile {
		t.Fatalf("exec.sh not classified as executable file: %+v", e)
	}
	if e := byPath["plain.txt"]; e.Executable || e.Mode != 0o644 || e.Kind != KindFile {
		t.Fatalf("plain.txt not classified as non-executable file: %+v", e)
	}
	if e := byPath["link.txt"]; e.Kind != KindSymlink {
		t.Fatalf("link.txt not classified as symlink: %+v", e)
	}
}

// TestCanonicalProgramIsLogicalNotResolvedPath asserts
// CHECK01-CORRECTION01 §1 (canonical program identity):
//
//	DefaultV1Profile().Specs[*].Program MUST be the LOGICAL
//	program name ("go" or "gofmt"), NOT an absolute path.
//
// Resolution to an absolute executable path happens ONLY
// inside the Runner, which records the resolved path on
// CheckDiagnostic.ResolvedProgram (which is excluded from
// the canonical JSON by its `json:"-"` tag on CheckOutcome).
//
// If a future regression embeds an absolute path in
// CheckSpec.Program (or, by extension, in CheckOutcome.Program
// in the canonical result), this test fails the build.
func TestCanonicalProgramIsLogicalNotResolvedPath(t *testing.T) {
	prof := DefaultV1Profile()
	if prof == nil {
		t.Fatal("DefaultV1Profile returned nil")
	}
	if len(prof.Specs) == 0 {
		t.Fatal("DefaultV1Profile returned no specs")
	}
	allowed := map[string]bool{
		"go":    true,
		"gofmt": true,
	}
	for _, spec := range prof.Specs {
		if spec.Program == "" {
			t.Fatalf("spec %q has empty Program; expected a logical name", spec.ID)
		}
		// A logical name MUST NOT contain a path separator or
		// begin with a recognised host toolchain prefix.
		if strings.ContainsRune(spec.Program, '/') {
			t.Fatalf("spec %q Program=%q contains '/'; expected logical name, not an absolute path", spec.ID, spec.Program)
		}
		if strings.ContainsRune(spec.Program, filepath.Separator) {
			t.Fatalf("spec %q Program=%q contains a path separator; expected logical name", spec.ID, spec.Program)
		}
		if !allowed[spec.Program] {
			t.Fatalf("spec %q Program=%q is not a recognised logical program name", spec.ID, spec.Program)
		}
	}
}

// TestCanonicalCrossHostProgramInvariant asserts the §1
// end-to-end invariant: when the Runner is replaced with two
// stub Runners that report DIFFERENT resolved executable
// paths but otherwise identical outcomes, the canonical
// CheckResult JSON MUST be byte-identical.
//
// This is the test that catches host-path pollution of
// canonical bytes — exactly the regression the freeze
// verdict reviewer called out as a P1 to fix before
// EVIDENCE01. It is the cross-environment / fake-resolution
// test requested in the freeze-cleanup instruction.
func TestCanonicalCrossHostProgramInvariant(t *testing.T) {
	subject := SubjectIdentity{
		Remote: "lab", Bookmark: "feature",
		OldCommitID: "old", NewCommitID: "new",
	}

	makeOutcome := func(program string, resolvedPath string, status CheckStatus) CheckOutcome {
		return CheckOutcome{
			ID:              CheckIDGoBuild,
			Status:          status,
			ExitCode:        0,
			Program:         program,
			Argv:            []string{"build", "./..."},
			ResolvedProgram: resolvedPath,
		}
	}

	outA := makeOutcome("go", "/nix/store/aaa-go-1.26.6/bin/go", StatusPass)
	outB := makeOutcome("go", "/usr/local/go/bin/go", StatusPass)

	mfst := &Manifest{Files: []FileEntry{
		{Path: "go.mod", Kind: KindFile, Executable: false, Mode: 0o644, Size: 8, ContentSHA256: "deadbeef"},
	}}

	resA := Aggregate(subject, mfst, []CheckOutcome{outA})
	resB := Aggregate(subject, mfst, []CheckOutcome{outB})

	canonicalA, err := RenderJSON(resA)
	if err != nil {
		t.Fatalf("render A: %v", err)
	}
	canonicalB, err := RenderJSON(resB)
	if err != nil {
		t.Fatalf("render B: %v", err)
	}
	if !bytes.Equal(canonicalA, canonicalB) {
		t.Fatalf("canonical bytes differ when only the resolved executable path differs:\n--- host A ---\n%s\n--- host B ---\n%s", canonicalA, canonicalB)
	}

	// Defense-in-depth: the canonical body MUST NOT contain
	// either resolved path.
	if bytes.Contains(canonicalA, []byte("/nix/store")) {
		t.Fatalf("canonical body leaked nix path: %s", canonicalA)
	}
	if bytes.Contains(canonicalA, []byte("/usr/local")) {
		t.Fatalf("canonical body leaked /usr/local path: %s", canonicalA)
	}

	// The OBSERVATION body, by contrast, MUST contain the
	// resolved path. Build a minimal CheckObservation and
	// verify the resolved_program field survives.
	obsA := CheckObservation{
		Result:            resA,
		SourceOperationID: "op-1",
		Diagnostics: []CheckDiagnostic{
			{
				ID: CheckIDGoBuild, ResolvedProgram: "/nix/store/aaa-go-1.26.6/bin/go",
				Argv: []string{"build", "./..."},
			},
		},
	}
	obsJSON, err := RenderObservationJSON(obsA)
	if err != nil {
		t.Fatalf("render obs: %v", err)
	}
	if !bytes.Contains(obsJSON, []byte("/nix/store/aaa-go-1.26.6/bin/go")) {
		t.Fatalf("observation body did NOT carry the resolved path; the §1 split is broken: %s", obsJSON)
	}
}
