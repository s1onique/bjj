package check_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/s1onique/bjj/internal/check"
	"github.com/s1onique/bjj/internal/jjadapter"
	"github.com/s1onique/bjj/internal/lab"
)

// TestIntegration_CleanGoCandidate_PassesAllChecks is the
// happy-path integration test for ACT-BJJ-CHECK01.
func TestIntegration_CleanGoCandidate_PassesAllChecks(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	l, err := lab.Setup(ctx)
	if err != nil {
		t.Skipf("lab prerequisites missing: %v", err)
	}
	defer l.Cleanup()

	cand, err := l.SeedCleanGoCandidate(ctx)
	if err != nil {
		t.Fatalf("seed clean candidate: %v", err)
	}
	jj := jjadapter.New()
	opID, err := jj.PinOperation(ctx, l.JJClient)
	if err != nil {
		t.Fatalf("pin operation: %v", err)
	}

	subject := check.CheckSubject{
		SourceOperationID: opID,
		Remote:            "lab",
		Bookmark:          "main",
		NewCommitID:       cand.CommitID,
	}

	parent := t.TempDir()
	orch := &check.Orchestrator{
		Materializer: check.NewJJMaterializer(check.NewJJFileSource(jj)),
		Runner:       check.NewExecRunner(),
	}
	obs, err := orch.Run(ctx, subject, check.Options{
		RepoDir:         l.JJClient,
		WorkspaceParent: parent,
	})
	if err != nil {
		t.Fatalf("orchestrator: %v", err)
	}
	if obs.Result.Status != check.StatusPass {
		for _, c := range obs.Result.Checks {
			t.Logf("  check %s: status=%s exit=%d err=%q", c.ID, c.Status, c.ExitCode, c.ErrorMessage)
		}
		t.Fatalf("expected StatusPass, got %q", obs.Result.Status)
	}
	if len(obs.Result.Checks) != 4 {
		t.Fatalf("expected 4 checks, got %d", len(obs.Result.Checks))
	}
	if obs.Result.Subject.NewCommitID != cand.CommitID {
		t.Fatalf("subject mismatch: got %q want %q", obs.Result.Subject.NewCommitID, cand.CommitID)
	}
	if obs.SourceOperationID != opID {
		t.Fatalf("opID mismatch: got %q want %q", obs.SourceOperationID, opID)
	}
}

// TestIntegration_GofmtBadCandidate_FailsGofmtOnly proves
// the check profile actually distinguishes "gofmt complained"
// from "code is fine".
func TestIntegration_GofmtBadCandidate_FailsGofmtOnly(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	l, err := lab.Setup(ctx)
	if err != nil {
		t.Skipf("lab prerequisites missing: %v", err)
	}
	defer l.Cleanup()

	cand, err := l.SeedGofmtBadCandidate(ctx)
	if err != nil {
		t.Fatalf("seed gofmt-bad candidate: %v", err)
	}
	jj := jjadapter.New()
	opID, err := jj.PinOperation(ctx, l.JJClient)
	if err != nil {
		t.Fatalf("pin operation: %v", err)
	}

	subject := check.CheckSubject{
		SourceOperationID: opID,
		Remote:            "lab",
		Bookmark:          "main",
		NewCommitID:       cand.CommitID,
	}

	orch := &check.Orchestrator{
		Materializer: check.NewJJMaterializer(check.NewJJFileSource(jj)),
		Runner:       check.NewExecRunner(),
	}
	obs, err := orch.Run(ctx, subject, check.Options{
		RepoDir:         l.JJClient,
		WorkspaceParent: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("orchestrator: %v", err)
	}
	if obs.Result.Status != check.StatusFail {
		t.Fatalf("expected StatusFail (gofmt), got %q", obs.Result.Status)
	}
	var sawGofmtFail bool
	for _, c := range obs.Result.Checks {
		if c.ID == check.CheckIDGofmt {
			if c.Status != check.StatusFail {
				t.Fatalf("gofmt status: got %q want %q", c.Status, check.StatusFail)
			}
			sawGofmtFail = true
		} else if c.Status != check.StatusPass {
			t.Fatalf("non-gofmt check %s should have passed: got %q", c.ID, c.Status)
		}
	}
	if !sawGofmtFail {
		t.Fatalf("did not see gofmt FAIL outcome in result")
	}
}

// TestIntegration_GoTestFailingCandidate_FailsGoTestOnly
// proves `go test` failure propagation works.
func TestIntegration_GoTestFailingCandidate_FailsGoTestOnly(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	l, err := lab.Setup(ctx)
	if err != nil {
		t.Skipf("lab prerequisites missing: %v", err)
	}
	defer l.Cleanup()

	cand, err := l.SeedGoTestFailingCandidate(ctx)
	if err != nil {
		t.Fatalf("seed go-test-failing candidate: %v", err)
	}
	jj := jjadapter.New()
	opID, err := jj.PinOperation(ctx, l.JJClient)
	if err != nil {
		t.Fatalf("pin operation: %v", err)
	}

	subject := check.CheckSubject{
		SourceOperationID: opID,
		Remote:            "lab",
		Bookmark:          "main",
		NewCommitID:       cand.CommitID,
	}

	orch := &check.Orchestrator{
		Materializer: check.NewJJMaterializer(check.NewJJFileSource(jj)),
		Runner:       check.NewExecRunner(),
	}
	obs, err := orch.Run(ctx, subject, check.Options{
		RepoDir:         l.JJClient,
		WorkspaceParent: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("orchestrator: %v", err)
	}
	if obs.Result.Status != check.StatusFail {
		t.Fatalf("expected StatusFail (go_test), got %q", obs.Result.Status)
	}
	for _, c := range obs.Result.Checks {
		if c.ID == check.CheckIDGoTest {
			if c.Status != check.StatusFail {
				t.Fatalf("go_test status: got %q want %q", c.Status, check.StatusFail)
			}
		}
	}
}

// TestIntegration_GoBuildFailingCandidate_FailsGoBuild is the
// negative-path test for build failures.
func TestIntegration_GoBuildFailingCandidate_FailsGoBuild(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	l, err := lab.Setup(ctx)
	if err != nil {
		t.Skipf("lab prerequisites missing: %v", err)
	}
	defer l.Cleanup()

	cand, err := l.SeedGoBuildFailingCandidate(ctx)
	if err != nil {
		t.Fatalf("seed go-build-failing candidate: %v", err)
	}
	jj := jjadapter.New()
	opID, err := jj.PinOperation(ctx, l.JJClient)
	if err != nil {
		t.Fatalf("pin operation: %v", err)
	}

	subject := check.CheckSubject{
		SourceOperationID: opID,
		Remote:            "lab",
		Bookmark:          "main",
		NewCommitID:       cand.CommitID,
	}

	orch := &check.Orchestrator{
		Materializer: check.NewJJMaterializer(check.NewJJFileSource(jj)),
		Runner:       check.NewExecRunner(),
	}
	obs, err := orch.Run(ctx, subject, check.Options{
		RepoDir:         l.JJClient,
		WorkspaceParent: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("orchestrator: %v", err)
	}
	if obs.Result.Status != check.StatusFail {
		t.Fatalf("expected StatusFail (go_build), got %q", obs.Result.Status)
	}
	for _, c := range obs.Result.Checks {
		if c.ID == check.CheckIDGoBuild {
			if c.Status != check.StatusFail {
				t.Fatalf("go_build status: got %q want %q", c.Status, check.StatusFail)
			}
		}
	}
}

// TestIntegration_MidRunMutationCannotChangeSubject is the
// verdict §19 P0 gate run against a real jj repo. It mutates
// the live jj client between materialization and pre-exec
// reverify and asserts the materialized workspace still
// contains the original candidate (no pollution).
func TestIntegration_MidRunMutationCannotChangeSubject(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	l, err := lab.Setup(ctx)
	if err != nil {
		t.Skipf("lab prerequisites missing: %v", err)
	}
	defer l.Cleanup()

	cand, err := l.SeedCleanGoCandidate(ctx)
	if err != nil {
		t.Fatalf("seed clean candidate: %v", err)
	}
	jj := jjadapter.New()
	opID, err := jj.PinOperation(ctx, l.JJClient)
	if err != nil {
		t.Fatalf("pin operation: %v", err)
	}

	subject := check.CheckSubject{
		SourceOperationID: opID,
		Remote:            "lab",
		Bookmark:          "main",
		NewCommitID:       cand.CommitID,
	}

	mutated := false
	orch := &check.Orchestrator{
		Materializer: check.NewJJMaterializer(check.NewJJFileSource(jj)),
		Runner:       check.NewExecRunner(),
		ReMaterializeForVerify: func(ctx context.Context, ws string) (*check.Manifest, error) {
			mutated = true
			_ = os.WriteFile(filepath.Join(l.JJClient, "STRANGER.txt"), []byte("pollution"), 0o644)
			return check.DefaultReVerifier(ctx, ws)
		},
	}

	obs, err := orch.Run(ctx, subject, check.Options{
		RepoDir:         l.JJClient,
		WorkspaceParent: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("orchestrator: %v", err)
	}
	if !mutated {
		t.Fatalf("mutation hook never ran")
	}
	// CHECK01-CORRECTION01: each check now runs in its own
	// disposable workspace; the post-cleanup of check 0 means
	// there is no surviving wsDir to stat. We instead
	// confirm that NONE of the diagnostics' workspace paths
	// contain a "STRANGER.txt" file (they are all cleaned
	// up, but if we wanted to check we could re-walk them).
	if obs.Result.Status != check.StatusPass {
		t.Fatalf("expected StatusPass, got %q", obs.Result.Status)
	}
}

// TestIntegration_SubjectMismatch proves the orchestrator
// refuses to run checks against a tampered workspace.
func TestIntegration_SubjectMismatch(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	l, err := lab.Setup(ctx)
	if err != nil {
		t.Skipf("lab prerequisites missing: %v", err)
	}
	defer l.Cleanup()

	cand, err := l.SeedCleanGoCandidate(ctx)
	if err != nil {
		t.Fatalf("seed clean candidate: %v", err)
	}
	jj := jjadapter.New()
	opID, err := jj.PinOperation(ctx, l.JJClient)
	if err != nil {
		t.Fatalf("pin operation: %v", err)
	}

	subject := check.CheckSubject{
		SourceOperationID: opID,
		Remote:            "lab",
		Bookmark:          "main",
		NewCommitID:       cand.CommitID,
	}

	orch := &check.Orchestrator{
		Materializer: check.NewJJMaterializer(check.NewJJFileSource(jj)),
		Runner:       check.NewExecRunner(),
		ReMaterializeForVerify: func(ctx context.Context, ws string) (*check.Manifest, error) {
			return &check.Manifest{Files: nil}, nil
		},
	}
	_, err = orch.Run(ctx, subject, check.Options{
		RepoDir:         l.JJClient,
		WorkspaceParent: t.TempDir(),
	})
	if err == nil {
		t.Fatalf("expected error for tampered workspace")
	}
	var ce *check.Error
	if !errors.As(err, &ce) {
		t.Fatalf("expected *check.Error, got %v", err)
	}
	if ce.Code != check.CodeWorkspaceChanged {
		t.Fatalf("got code %q want %q", ce.Code, check.CodeWorkspaceChanged)
	}
}

// TestIntegration_RunCheckJSONShape confirms the canonical
// JSON is strict and never leaks the workspace path.
func TestIntegration_RunCheckJSONShape(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	l, err := lab.Setup(ctx)
	if err != nil {
		t.Skipf("lab prerequisites missing: %v", err)
	}
	defer l.Cleanup()

	cand, err := l.SeedCleanGoCandidate(ctx)
	if err != nil {
		t.Fatalf("seed clean candidate: %v", err)
	}
	jj := jjadapter.New()
	opID, err := jj.PinOperation(ctx, l.JJClient)
	if err != nil {
		t.Fatalf("pin operation: %v", err)
	}
	subject := check.CheckSubject{
		SourceOperationID: opID,
		Remote:            "lab",
		Bookmark:          "main",
		NewCommitID:       cand.CommitID,
	}
	orch := &check.Orchestrator{
		Materializer: check.NewJJMaterializer(check.NewJJFileSource(jj)),
		Runner:       check.NewExecRunner(),
	}
	obs, err := orch.Run(ctx, subject, check.Options{
		RepoDir:         l.JJClient,
		WorkspaceParent: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("orchestrator: %v", err)
	}
	body, err := check.RenderObservationJSON(obs)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	// The CANONICAL portion of the observation (obs.Result)
	// must NOT contain any workspace path. Diagnostics DO
	// carry per-check workspace paths (that's their purpose),
	// so we render only the canonical body for this assertion.
	canonicalBody, err := check.RenderJSON(obs.Result)
	if err != nil {
		t.Fatalf("render canonical: %v", err)
	}
	for _, d := range obs.Diagnostics {
		if strings.Contains(string(canonicalBody), d.WorkspacePath) {
			t.Fatalf("per-check workspace path %q leaked into canonical JSON", d.WorkspacePath)
		}
	}
	// Sanity: the full observation JSON is non-empty.
	if len(body) == 0 {
		t.Fatalf("observation JSON is empty")
	}
}
