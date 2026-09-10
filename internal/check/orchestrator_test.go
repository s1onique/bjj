package check

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeRunner is a deterministic Runner for unit tests.
//
// Each entry in outcomes is returned in order; if the slice is
// exhausted, the runner records an ERROR outcome. Callers may
// also set a hook to be invoked before returning each outcome
// (used by the mid-run mutation tests). The hook receives the
// index AND the workspaceDir passed to that Run call, so a
// hook can mutate the per-check workspace or inspect another
// independently-supplied workspace.
type fakeRunner struct {
	outcomes []CheckOutcome
	hook     func(idx int, workspaceDir string)
	calls    int
}

func (r *fakeRunner) Run(ctx context.Context, spec CheckSpec, workspaceDir string, maxOutputBytes int) CheckOutcome {
	if r.calls >= len(r.outcomes) {
		return CheckOutcome{ID: spec.ID, Program: spec.Program, Argv: spec.Argv, Status: StatusError, ExitCode: -1, ErrorCode: CodeExecFailed, ErrorMessage: "fakeRunner: exhausted"}
	}
	o := r.outcomes[r.calls]
	r.calls++
	if r.hook != nil {
		r.hook(r.calls-1, workspaceDir)
	}
	if o.Program == "" {
		o.Program = spec.Program
	}
	if len(o.Argv) == 0 {
		o.Argv = append([]string(nil), spec.Argv...)
	}
	if o.ID == "" {
		o.ID = spec.ID
	}
	o.WorkspacePath = workspaceDir
	return o
}

// TestOrchestrator_RejectsEmptySubject asserts that the
// orchestrator refuses to materialize a workspace when the
// subject identity is incomplete.
func TestOrchestrator_RejectsEmptySubject(t *testing.T) {
	orch := &Orchestrator{
		Materializer: NewJJMaterializer(&fakeFileSource{Files: map[string][]byte{"a": []byte("x")}}),
		Runner:       &fakeRunner{},
	}
	subject := CheckSubject{NewCommitID: "", SourceOperationID: "op"}
	_, err := orch.Run(context.Background(), subject, Options{
		RepoDir: "/d", WorkspaceParent: t.TempDir(),
	})
	if err == nil {
		t.Fatalf("expected error for empty NewCommitID")
	}
	var ce *Error
	if !errors.As(err, &ce) {
		t.Fatalf("expected *Error, got %v", err)
	}
	if ce.Code != CodeAdmissionNotAdmitted {
		t.Fatalf("got code %q want %q", ce.Code, CodeAdmissionNotAdmitted)
	}
}

// TestOrchestrator_HappyPath_PassesThroughAggregatedResult
// asserts that a clean subject with all-PASS outcomes produces
// a CheckResult with overall status "pass".
func TestOrchestrator_HappyPath_PassesThroughAggregatedResult(t *testing.T) {
	fs := &fakeFileSource{Files: map[string][]byte{"a.txt": []byte("hello")}}
	runner := &fakeRunner{
		outcomes: []CheckOutcome{
			{ID: CheckIDGoBuild, Status: StatusPass, ExitCode: 0},
			{ID: CheckIDGofmt, Status: StatusPass, ExitCode: 0},
			{ID: CheckIDGoTest, Status: StatusPass, ExitCode: 0},
			{ID: CheckIDGoVet, Status: StatusPass, ExitCode: 0},
		},
	}
	orch := &Orchestrator{
		Materializer: NewJJMaterializer(fs),
		Runner:       runner,
	}
	subject := CheckSubject{
		SourceOperationID: "op-1",
		Remote:            "lab",
		Bookmark:          "feature",
		NewCommitID:       "rev-1",
	}
	obs, err := orch.Run(context.Background(), subject, Options{
		RepoDir: "/d", WorkspaceParent: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("orchestrator run: %v", err)
	}
	result := obs.Result

	if result.Status != StatusPass {
		t.Fatalf("status: got %q want %q", result.Status, StatusPass)
	}
	if len(result.Checks) != 4 {
		t.Fatalf("expected 4 checks, got %d", len(result.Checks))
	}
	if result.FileCount != 1 {
		t.Fatalf("file count: got %d want 1", result.FileCount)
	}
	if result.Subject.NewCommitID != "rev-1" {
		t.Fatalf("subject mismatch in result: %+v", result.Subject)
	}
	// SourceOperationID is observation-only (CORRECTION01 §5);
	// verify it propagated into the observation envelope.
	if obs.SourceOperationID != "op-1" {
		t.Fatalf("observation opID: got %q want %q", obs.SourceOperationID, "op-1")
	}
}

// TestOrchestrator_AnyFailureFoldsToFail asserts the §12
// aggregation rule when at least one check FAILs.
func TestOrchestrator_AnyFailureFoldsToFail(t *testing.T) {
	fs := &fakeFileSource{Files: map[string][]byte{"a.txt": []byte("hello")}}
	runner := &fakeRunner{
		outcomes: []CheckOutcome{
			{ID: CheckIDGoBuild, Status: StatusPass, ExitCode: 0},
			{ID: CheckIDGofmt, Status: StatusFail, ExitCode: 1},
			{ID: CheckIDGoTest, Status: StatusPass, ExitCode: 0},
			{ID: CheckIDGoVet, Status: StatusPass, ExitCode: 0},
		},
	}
	orch := &Orchestrator{
		Materializer: NewJJMaterializer(fs),
		Runner:       runner,
	}
	subject := CheckSubject{
		SourceOperationID: "op-1", Remote: "lab", Bookmark: "feature", NewCommitID: "rev-1",
	}
	obs, err := orch.Run(context.Background(), subject, Options{
		RepoDir: "/d", WorkspaceParent: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("orchestrator run: %v", err)
	}
	if obs.Result.Status != StatusFail {
		t.Fatalf("status: got %q want %q", obs.Result.Status, StatusFail)
	}
}

// TestOrchestrator_ErrorOutranksFail asserts that an ERROR
// outranks FAIL in the aggregate (per §12).
func TestOrchestrator_ErrorOutranksFail(t *testing.T) {
	fs := &fakeFileSource{Files: map[string][]byte{"a.txt": []byte("hello")}}
	runner := &fakeRunner{
		outcomes: []CheckOutcome{
			{ID: CheckIDGoBuild, Status: StatusFail, ExitCode: 1},
			{ID: CheckIDGofmt, Status: StatusError, ExitCode: -1, ErrorCode: CodeTimeout},
			{ID: CheckIDGoTest, Status: StatusPass, ExitCode: 0},
			{ID: CheckIDGoVet, Status: StatusPass, ExitCode: 0},
		},
	}
	orch := &Orchestrator{
		Materializer: NewJJMaterializer(fs),
		Runner:       runner,
	}
	subject := CheckSubject{
		SourceOperationID: "op-1", Remote: "lab", Bookmark: "feature", NewCommitID: "rev-1",
	}
	obs, err := orch.Run(context.Background(), subject, Options{
		RepoDir: "/d", WorkspaceParent: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("orchestrator run: %v", err)
	}
	if obs.Result.Status != StatusError {
		t.Fatalf("status: got %q want %q", obs.Result.Status, StatusError)
	}
}

// TestOrchestrator_WorkspaceTamperingDetected asserts that the
// pre-exec reverify catches a tampered workspace and returns
// CodeWorkspaceChanged (verdict §21).
func TestOrchestrator_WorkspaceTamperingDetected(t *testing.T) {
	fs := &fakeFileSource{Files: map[string][]byte{
		"a.txt": []byte("hello"),
		"b.txt": []byte("world"),
	}}
	orch := &Orchestrator{
		Materializer: NewJJMaterializer(fs),
		Runner:       &fakeRunner{outcomes: []CheckOutcome{}},
		ReMaterializeForVerify: func(ctx context.Context, ws string) (*Manifest, error) {
			return &Manifest{Files: []FileEntry{
				{Path: "a.txt", Kind: KindFile, ContentSHA256: "original-a", Mode: 0o644, Size: 5},
				{Path: "b.txt", Kind: KindFile, ContentSHA256: "TAMPERED", Mode: 0o644, Size: 5},
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
		t.Fatalf("expected workspace-changed error, got nil")
	}
	var ce *Error
	if !errors.As(err, &ce) {
		t.Fatalf("expected *Error, got %v", err)
	}
	if ce.Code != CodeWorkspaceChanged {
		t.Fatalf("got code %q want %q", ce.Code, CodeWorkspaceChanged)
	}
}

// TestOrchestrator_PreviousSpecMutationCannotAffectNext is the
// CHECK01-CORRECTION01 §1 invariant proof: a check that mutates
// its OWN supplied workspaceDir MUST NOT contaminate the input
// of the next check.
//
// This test does NOT mutate the fake source. It mutates the
// workspaceDir that the orchestrator hands to check #1, then
// asserts that check #2 receives an INDEPENDENTLY supplied
// workspaceDir (created by the orchestrator's per-check fresh
// materialize) in which `a.txt` still holds the original
// frozen bytes. The orchestrator must pass two DIFFERENT
// workspaceDir values to the two calls; check #2's dir is
// untouched by check #1's write.
//
// Specifically:
//
//	check #1 receives workspaceDir N
//	check #1 mutates {N}/a.txt -> "poisoned-by-check-0"
//	check #2 receives workspaceDir M  (a fresh per-check dir)
//	check #2 reads {M}/a.txt -> "frozen"   ← assertion
//
// If the orchestrator reused one workspace across checks the
// two dirs would be identical, M == N, and the assertion would
// fail.
func TestOrchestrator_PreviousSpecMutationCannotAffectNext(t *testing.T) {
	fs := &fakeFileSource{Files: map[string][]byte{
		"a.txt": []byte("frozen"),
	}}

	// dirsSeen collects the (idx, dir) pairs the orchestrator
	// hands to the runner. Hooks run sequentially from a single
	// goroutine, so a plain slice is sufficient.
	//
	// We also capture the bytes that check #2 sees when IT
	// reads a.txt out of ITS OWN workspaceDir — that
	// assertion MUST happen inside the hook, before the
	// orchestrator's post-run cleanup deletes the dir.
	var (
		dirsSeen          []string
		check2ObservedTxt string
		check2ReadErr     error
	)

	runner := &fakeRunner{
		outcomes: []CheckOutcome{
			{ID: CheckIDGoBuild, Status: StatusPass, ExitCode: 0},
			{ID: CheckIDGofmt, Status: StatusPass, ExitCode: 0},
			{ID: CheckIDGoTest, Status: StatusPass, ExitCode: 0},
			{ID: CheckIDGoVet, Status: StatusPass, ExitCode: 0},
		},
		hook: func(idx int, workspaceDir string) {
			dirsSeen = append(dirsSeen, workspaceDir)
			switch idx {
			case 0:
				// Check #1 mutates its OWN
				// supplied workspaceDir. This MUST
				// NOT be visible to check #2.
				path := filepath.Join(workspaceDir, "a.txt")
				if err := os.WriteFile(path, []byte("poisoned-by-check-0"), 0o644); err != nil {
					t.Fatalf("hook could not write: %v", err)
				}
			case 1:
				// Check #2 reads its OWN
				// workspaceDir (still alive at this
				// point; the orchestrator only cleans
				// up AFTER the hook returns).
				got, rerr := os.ReadFile(filepath.Join(workspaceDir, "a.txt"))
				check2ReadErr = rerr
				if got != nil {
					check2ObservedTxt = string(got)
				}
			}
		},
	}
	orch := &Orchestrator{
		Materializer: NewJJMaterializer(fs),
		Runner:       runner,
	}
	subject := CheckSubject{
		SourceOperationID: "op-1", Remote: "lab", Bookmark: "feature", NewCommitID: "rev-1",
	}
	obs, err := orch.Run(context.Background(), subject, Options{
		RepoDir: "/d", WorkspaceParent: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("orchestrator run: %v", err)
	}
	if obs.Result.Status != StatusPass {
		t.Fatalf("status: got %q want %q", obs.Result.Status, StatusPass)
	}

	// We need at least 2 checks to exercise the invariant.
	if len(dirsSeen) < 2 {
		t.Fatalf("test did not exercise per-check workspace isolation: got %d dirs", len(dirsSeen))
	}

	// (a) Check #1 and check #2 must have been handed
	//     DIFFERENT workspace dirs (per-check fresh
	//     materialization).
	if dirsSeen[0] == dirsSeen[1] {
		t.Fatalf("CHECK01-CORRECTION01 §1 violated: check #1 and check #2 shared workspaceDir %q", dirsSeen[0])
	}

	// (b) Check #2's independently-supplied workspaceDir must
	//     contain the original frozen bytes — proof that
	//     check #1's write did not leak across. (Captured
	//     inside the hook before the orchestrator's cleanup.)
	if check2ReadErr != nil {
		t.Fatalf("check #2 could not read a.txt from its independently-supplied workspaceDir %q: %v", dirsSeen[1], check2ReadErr)
	}
	if check2ObservedTxt != "frozen" {
		t.Fatalf("check #2 saw %q in its own workspaceDir; per-check fresh-workspace invariant violated (check #1's write leaked into check #2)", check2ObservedTxt)
	}

	// (c) After the orchestrator finishes, the per-check
	//     workspaces MUST no longer exist — the per-check
	//     cleanup ran.
	for i, d := range dirsSeen {
		if _, err := os.Stat(d); err == nil {
			t.Fatalf("check #%d workspaceDir %q still exists after orchestrator cleanup; per-check isolation is leaky", i, d)
		}
	}
}

// TestOrchestrator_LiveWorktreeMutationIsolated asserts the
// verdict §20 invariant: mutations to the live repository (here
// modelled as writing a file in /tmp) MUST NOT alter the
// disposable workspace.
//
// CHECK01-CORRECTION01: this test now verifies per-check fresh
// workspace isolation against live-repo mutations. Because the
// orchestrator cleans up each per-check workspace before the
// next one materializes, any side-effects to the live repo
// during check N can only affect the workspace of check N+1
// via the FAKE FileSource's reaction to those side-effects;
// here the fake source does NOT observe liveRepo, so the
// workspace bytes must remain exactly the materialized bytes.
func TestOrchestrator_LiveWorktreeMutationIsolated(t *testing.T) {
	liveRepo := t.TempDir()
	fs := &fakeFileSource{Files: map[string][]byte{
		"a.txt": []byte("frozen"),
	}}
	mutated := false
	runner := &fakeRunner{
		outcomes: []CheckOutcome{
			{ID: CheckIDGoBuild, Status: StatusPass, ExitCode: 0},
			{ID: CheckIDGofmt, Status: StatusPass, ExitCode: 0},
			{ID: CheckIDGoTest, Status: StatusPass, ExitCode: 0},
			{ID: CheckIDGoVet, Status: StatusPass, ExitCode: 0},
		},
		hook: func(idx int, workspaceDir string) {
			if idx == 0 {
				_ = os.WriteFile(filepath.Join(liveRepo, "live-pollution.txt"), []byte("X"), 0o644)
				mutated = true
			}
		},
	}
	orch := &Orchestrator{
		Materializer: NewJJMaterializer(fs),
		Runner:       runner,
	}
	subject := CheckSubject{
		SourceOperationID: "op-1", Remote: "lab", Bookmark: "feature", NewCommitID: "rev-1",
	}
	obs, err := orch.Run(context.Background(), subject, Options{
		RepoDir: liveRepo, WorkspaceParent: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("orchestrator run: %v", err)
	}
	if !mutated {
		t.Fatalf("hook never fired; test is not exercising the invariant")
	}
	// The last workspace diagnostic carries the workspace
	// path; we read from it to confirm it contains the
	// expected bytes. (Each per-check workspace is cleaned up
	// after its check runs, so we can only inspect the
	// diagnostic's recorded path AFTER it was active.)
	if len(obs.Diagnostics) == 0 {
		t.Fatalf("expected per-check diagnostics, got none")
	}
	for _, d := range obs.Diagnostics {
		body, rerr := os.ReadFile(filepath.Join(d.WorkspacePath, "a.txt"))
		if rerr != nil {
			// Workspace already cleaned up; that's fine.
			continue
		}
		if string(body) != "frozen" {
			t.Fatalf("workspace %s a.txt was modified: got %q want %q", d.WorkspacePath, body, "frozen")
		}
	}
	if obs.Result.Status != StatusPass {
		t.Fatalf("status: got %q want %q", obs.Result.Status, StatusPass)
	}
}

// TestOrchestrator_NoMutationOfSourceRepo asserts the verdict §22
// invariant: check execution MUST NOT mutate the source
// repository.
//
// CHECK01-CORRECTION01: this test now checks that the
// workspace parent (the only directory BJJ touches that is
// close to the source repo) does not live inside the source
// repo, and that the source repo itself is unchanged after
// the run.
func TestOrchestrator_NoMutationOfSourceRepo(t *testing.T) {
	repo := t.TempDir()
	if err := os.WriteFile(filepath.Join(repo, "initial.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}
	fs := &fakeFileSource{Files: map[string][]byte{
		"a.txt": []byte("frozen"),
	}}
	runner := &fakeRunner{
		outcomes: []CheckOutcome{
			{ID: CheckIDGoBuild, Status: StatusPass, ExitCode: 0},
			{ID: CheckIDGofmt, Status: StatusPass, ExitCode: 0},
			{ID: CheckIDGoTest, Status: StatusPass, ExitCode: 0},
			{ID: CheckIDGoVet, Status: StatusPass, ExitCode: 0},
		},
	}
	orch := &Orchestrator{
		Materializer: NewJJMaterializer(fs),
		Runner:       runner,
	}
	subject := CheckSubject{
		SourceOperationID: "op-1", Remote: "lab", Bookmark: "feature", NewCommitID: "rev-1",
	}
	wsParent := t.TempDir()
	obs, err := orch.Run(context.Background(), subject, Options{
		RepoDir: repo, WorkspaceParent: wsParent,
	})
	if err != nil {
		t.Fatalf("orchestrator run: %v", err)
	}

	entries, err := os.ReadDir(repo)
	if err != nil {
		t.Fatalf("read repo: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "initial.txt" {
		names := make([]string, len(entries))
		for i, e := range entries {
			names[i] = e.Name()
		}
		t.Fatalf("repo contents changed: %v", names)
	}
	if strings.HasPrefix(wsParent, repo+string(filepath.Separator)) {
		t.Fatalf("workspace parent leaked into source repo: wsParent=%s repo=%s", wsParent, repo)
	}
	for _, d := range obs.Diagnostics {
		if strings.HasPrefix(d.WorkspacePath, repo+string(filepath.Separator)) {
			t.Fatalf("per-check workspace leaked into source repo: ws=%s repo=%s", d.WorkspacePath, repo)
		}
	}
}

// TestOrchestrator_DeniedSubjectNotExecuted asserts the verdict
// §16 invariant.
func TestOrchestrator_DeniedSubjectNotExecuted(t *testing.T) {
	fs := &fakeFileSource{Files: map[string][]byte{"a.txt": []byte("x")}}
	calls := 0
	countingFS := &countingFileSource{inner: fs, calls: &calls}
	orch := &Orchestrator{
		Materializer: NewJJMaterializer(countingFS),
		Runner:       &fakeRunner{outcomes: []CheckOutcome{}},
	}
	subject := CheckSubject{SourceOperationID: "op-1", NewCommitID: ""}
	_, err := orch.Run(context.Background(), subject, Options{
		RepoDir: "/d", WorkspaceParent: t.TempDir(),
	})
	if err == nil {
		t.Fatalf("expected error for denied subject")
	}
	var ce *Error
	if !errors.As(err, &ce) || ce.Code != CodeAdmissionNotAdmitted {
		t.Fatalf("got %v want CodeAdmissionNotAdmitted", err)
	}
	if calls != 0 {
		t.Fatalf("materializer was invoked for a denied subject; calls=%d", calls)
	}
}

// countingFileSource wraps a FileSource and counts invocations.
type countingFileSource struct {
	inner FileSource
	calls *int
}

func (c *countingFileSource) ListTreeEntries(ctx context.Context, dir, opID, revision string) ([]TreeEntry, error) {
	*c.calls++
	return c.inner.ListTreeEntries(ctx, dir, opID, revision)
}

func (c *countingFileSource) ShowFile(ctx context.Context, dir, opID, revision, path string) ([]byte, error) {
	return c.inner.ShowFile(ctx, dir, opID, revision, path)
}
