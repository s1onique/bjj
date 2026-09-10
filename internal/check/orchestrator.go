package check

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// CheckSubject is a self-contained subject binding for the
// orchestrator. It carries every field the CheckResult.Subject
// MUST carry, plus the planned revision (== admitted NewCommitID)
// the materializer should export.
//
// The check package itself does NOT import internal/admission —
// callers in cmd/bjj translate an admission.AdmissionResult
// into a CheckSubject via a tiny adapter. That decoupling is
// what makes the pure aggregator testable without an admission
// run.
type CheckSubject struct {
	// SourceOperationID is the operation id the materializer
	// pins to. MUST equal AdmissionResult.SourceOperationID
	// (which is the same opID that produced PublishPlan).
	SourceOperationID string

	// Remote / Bookmark / Old / New mirror the admitted
	// subject. NewCommitID is the materializer target.
	Remote      string
	Bookmark    string
	OldCommitID string
	NewCommitID string
}

// AsSubjectIdentity returns the typed SubjectIdentity used in
// CheckResult.Subject. The result has exactly four fields
// (Remote, Bookmark, OldCommitID, NewCommitID), making it
// structurally equal to admission.SubjectIdentity per
// CHECK01-CORRECTION01 §5.
//
// SourceOperationID is intentionally NOT included here; it is
// observation provenance, not canonical subject identity. It
// lives in CheckObservation instead.
func (s CheckSubject) AsSubjectIdentity() SubjectIdentity {
	return SubjectIdentity{
		Remote:      s.Remote,
		Bookmark:    s.Bookmark,
		OldCommitID: s.OldCommitID,
		NewCommitID: s.NewCommitID,
	}
}

// Options configures one Check invocation.
type Options struct {
	// RepoDir is the absolute path of the source Jujutsu
	// repository. It is passed to the FileSource for
	// `jj file list` / `jj file show` invocations.
	RepoDir string

	// WorkspaceParent is the absolute path of the directory
	// under which the disposable verification workspace will
	// be created. A fresh sub-directory is allocated per run.
	WorkspaceParent string

	// ProfileName selects the check profile. Empty means "v1".
	ProfileName string

	// MaxOutputBytes bounds each check's stdout and stderr.
	// Zero means use the runner default.
	MaxOutputBytes int
}

// Orchestrator wires together admission-gated materialization,
// pre-exec integrity verification, profile execution, and pure
// aggregation.
//
// Orchestrator performs NO subprocess invocation directly; it
// composes the injected Materializer and Runner. This keeps the
// orchestrator itself a pure Go function (apart from filesystem
// mkdir/rmdir for the disposable workspace), which is essential
// for adversarial testing.
type Orchestrator struct {
	Materializer Materializer
	Runner       Runner
	// ReMaterializeForVerify, when non-nil, is called BEFORE
	// check execution to re-list the materialized workspace and
	// compare it against the post-materialization manifest.
	// The default is to walk the workspace and SHA-256 each
	// file (cheaper than re-invoking jj, and proves
	// CHECK_WORKSPACE_PREEXEC_INTEGRITY). Tests can replace
	// this with a fake that mutates the workspace.
	ReMaterializeForVerify ReVerifier
}

// ReVerifier is invoked between materialization and check
// execution. It re-enumerates the workspace and returns a fresh
// Manifest. The orchestrator compares the new Manifest against
// the materializer's output; mismatch -> CodeWorkspaceChanged.
//
// The default ReVerifier walks the workspace and SHA-256s each
// regular file; tests may inject a fake that drops a file to
// prove the orchestrator catches tampering.
type ReVerifier func(ctx context.Context, workspaceDir string) (*Manifest, error)

// DefaultReVerifier walks workspaceDir, SHA-256s each regular
// file, and returns a Manifest in canonical (sorted) order.
//
// CHECK01-CORRECTION01: the verifier uses os.Lstat so it does
// NOT follow symlinks; every entry's (kind, executable, mode)
// is read from the lstat result and recorded in the manifest.
// chmod +x between materialization and re-verify will be
// detected because the materializer wrote Mode=0o644 while
// the post-chmod file shows Mode=0o755.
func DefaultReVerifier(ctx context.Context, workspaceDir string) (*Manifest, error) {
	if workspaceDir == "" {
		return nil, fmt.Errorf("check: DefaultReVerifier: empty workspaceDir")
	}
	out := &Manifest{Files: []FileEntry{}}
	err := filepath.WalkDir(workspaceDir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		// Lstat (not Stat) so symlinks are NOT followed;
		// any unexpected symlink is captured as KindSymlink
		// so manifestEqual can flag a post-materialize
		// substitution.
		info, ierr := os.Lstat(path)
		if ierr != nil {
			return ierr
		}
		if info.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(workspaceDir, path)
		if err != nil {
			return err
		}
		kind := KindFile
		if info.Mode()&os.ModeSymlink != 0 {
			// The materializer refuses symlinks, so a
			// symlink that appears here means something
			// tampered with the workspace after
			// materialization. We still emit it as
			// KindSymlink so manifestEqual can detect the
			// drift; we do NOT recurse into it.
			kind = KindSymlink
			out.Files = append(out.Files, FileEntry{
				Path:       filepath.ToSlash(rel),
				Kind:       kind,
				Executable: false,
				Mode:       uint32(info.Mode().Perm()),
				Size:       info.Size(),
			})
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(body)
		executable := info.Mode().Perm()&0o111 != 0
		out.Files = append(out.Files, FileEntry{
			Path:          filepath.ToSlash(rel),
			Kind:          kind,
			Executable:    executable,
			Mode:          uint32(info.Mode().Perm()),
			Size:          int64(len(body)),
			ContentSHA256: hex.EncodeToString(sum[:]),
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(out.Files, func(i, j int) bool { return out.Files[i].Path < out.Files[j].Path })
	return out, nil
}

// manifestEqual compares two Manifests by per-file
// (path, kind, executable, mode, sha256) tuple. It is
// order-insensitive because the default ReVerifier is documented
// to return sorted output; the materializer also sorts. We
// defensively normalise here too.
//
// CHECK01-CORRECTION01 §3/§4 requires that chmod tampering
// between materialization and pre-exec verification be
// detected. Comparing only (path, sha256) would silently allow
// `chmod +x` (which doesn't change bytes). Comparing
// (path, kind, executable, mode, sha256) closes that gap.
func manifestEqual(a, b *Manifest) bool {
	if a == nil || b == nil {
		return a == b
	}
	if len(a.Files) != len(b.Files) {
		return false
	}
	acp := make([]FileEntry, len(a.Files))
	copy(acp, a.Files)
	bcp := make([]FileEntry, len(b.Files))
	copy(bcp, b.Files)
	sort.Slice(acp, func(i, j int) bool { return acp[i].Path < acp[j].Path })
	sort.Slice(bcp, func(i, j int) bool { return bcp[i].Path < bcp[j].Path })
	for i := range acp {
		if acp[i].Path != bcp[i].Path {
			return false
		}
		if acp[i].Kind != bcp[i].Kind {
			return false
		}
		if acp[i].Executable != bcp[i].Executable {
			return false
		}
		if acp[i].Mode != bcp[i].Mode {
			return false
		}
		if acp[i].ContentSHA256 != bcp[i].ContentSHA256 {
			return false
		}
	}
	return true
}

// Run executes the check pipeline for subject against the
// injected collaborators.
//
// CHECK01-CORRECTION01 §1 invariant:
//
//	INPUT(gofmt)   == NEW @ OP_A
//	INPUT(go_vet)  == NEW @ OP_A
//	INPUT(go_test) == NEW @ OP_A
//	INPUT(go_build)== NEW @ OP_A
//
// Each CheckSpec runs in a FRESH disposable workspace so a
// malicious or buggy check cannot contaminate the input of
// subsequent checks. The flow is:
//
//  1. Pre-flight: validate subject identity has a non-empty
//     NewCommitID and SourceOperationID. Refuse with
//     CodeAdmissionNotAdmitted otherwise.
//  2. Load the profile (compile-time defaults; no I/O).
//  3. For each CheckSpec:
//     a. Materialize a fresh workspace from (opID, NEW).
//     b. Pre-exec reverify: walk the workspace, compare
//     against the fresh manifest. Mismatch ->
//     CodeWorkspaceChanged + StatusError.
//     c. Run the check via the Runner.
//     d. Capture the diagnostic (workspace path, resolved
//     program, bounded stdout/stderr, duration).
//     e. Cleanup the workspace.
//  4. Aggregate: produce the canonical CheckResult.
//  5. Wrap canonical + diagnostics into a CheckObservation.
//
// Run cleans up every per-check workspace before returning.
// The returned CheckObservation carries the per-check
// WorkspacePath inside Diagnostics so callers can introspect
// post-hoc (subject to disk cleanup having happened).
func (o *Orchestrator) Run(ctx context.Context, subject CheckSubject, opts Options) (CheckObservation, error) {
	if o == nil || o.Materializer == nil || o.Runner == nil {
		return CheckObservation{}, NewError(CodeMaterializationFailed,
			"check: orchestrator not initialised", nil)
	}
	if subject.NewCommitID == "" {
		return CheckObservation{}, NewError(CodeAdmissionNotAdmitted,
			"check: subject has empty NewCommitID; cannot bind materializer target", nil)
	}
	if subject.SourceOperationID == "" {
		return CheckObservation{}, NewError(CodeAdmissionNotAdmitted,
			"check: subject has empty SourceOperationID; cannot pin materializer", nil)
	}
	if opts.RepoDir == "" {
		return CheckObservation{}, NewError(CodeMaterializationFailed,
			"check: empty RepoDir", nil)
	}
	if opts.WorkspaceParent == "" {
		return CheckObservation{}, NewError(CodeMaterializationFailed,
			"check: empty WorkspaceParent", nil)
	}

	prof, perr := Lookup(opts.ProfileName)
	if perr != nil {
		return CheckObservation{}, NewError(CodeExecFailed,
			fmt.Sprintf("check: profile: %v", perr), perr)
	}

	reverify := o.ReMaterializeForVerify
	if reverify == nil {
		reverify = DefaultReVerifier
	}

	outcomes := make([]CheckOutcome, 0, len(prof.Specs))
	diagnostics := make([]CheckDiagnostic, 0, len(prof.Specs))

	// Track the manifest of the FIRST check so the aggregator
	// can populate FileCount from a stable source. The
	// manifests from later checks are equal by construction
	// (same opID + revision) so any of them would work.
	var firstManifest *Manifest

	for i, spec := range prof.Specs {
		if err := ctx.Err(); err != nil {
			return CheckObservation{}, NewError(CodeMaterializationFailed,
				fmt.Sprintf("check: orchestrator: context cancelled: %v", err), err)
		}

		// Step 3a: materialize a fresh workspace for this spec.
		manifest, workspaceDir, merr := o.Materializer.Materialize(ctx, opts.WorkspaceParent, opts.RepoDir, subject.SourceOperationID, subject.NewCommitID)
		if merr != nil {
			return CheckObservation{}, merr
		}
		if firstManifest == nil {
			firstManifest = manifest
		}

		// Step 3b: pre-exec reverify against the fresh
		// manifest. We verify the SAME workspace the check
		// is about to run in (no TOCTOU window).
		after, verr := reverify(ctx, workspaceDir)
		if verr != nil {
			_ = os.RemoveAll(workspaceDir)
			return CheckObservation{}, NewError(CodeMaterializationMismatch,
				fmt.Sprintf("check: pre-exec re-verify: %v", verr), verr)
		}
		if !manifestEqual(manifest, after) {
			_ = os.RemoveAll(workspaceDir)
			return CheckObservation{}, NewError(CodeWorkspaceChanged,
				"check: workspace changed between materialization and pre-exec verification", nil)
		}

		// Step 3c: run the check.
		out := o.Runner.Run(ctx, spec, workspaceDir, opts.MaxOutputBytes)

		// Step 3d: capture diagnostic (DIAGNOSTIC ONLY; not
		// part of canonical CheckOutcome).
		diagnostics = append(diagnostics, CheckDiagnostic{
			ID:              spec.ID,
			ResolvedProgram: out.ResolvedProgram,
			Argv:            append([]string(nil), out.Argv...),
			WorkspacePath:   workspaceDir,
			Stdout:          append([]byte(nil), out.Stdout...),
			Stderr:          append([]byte(nil), out.Stderr...),
			StdoutTruncated: out.StdoutTruncated,
			StderrTruncated: out.StderrTruncated,
			DurationMillis:  out.DurationMillis,
		})

		// Step 3e: canonical outcome (strip diagnostic-only
		// fields). The orchestrator is the boundary that
		// prevents host-dependent / per-run data (resolved
		// program path, stdout/stderr, durations, workspace
		// path) from leaking into CheckResult.Checks.
		outcomes = append(outcomes, CheckOutcome{
			ID:           spec.ID,
			Status:       out.Status,
			ExitCode:     out.ExitCode,
			ErrorCode:    out.ErrorCode,
			ErrorMessage: out.ErrorMessage,
			Program:      spec.Program,
			Argv:         append([]string(nil), out.Argv...),
		})

		// Step 3f: cleanup. We do NOT propagate cleanup
		// errors; the canonical outcome has already been
		// recorded.
		_ = os.RemoveAll(workspaceDir)
		_ = i // index reserved for future per-spec ordering
	}

	// Step 4 + 5: aggregate canonical, wrap into observation.
	result := Aggregate(subject.AsSubjectIdentity(), firstManifest, outcomes)
	obs := CheckObservation{
		Result:                    result,
		SourceOperationID:         subject.SourceOperationID,
		ResolvedAbsoluteWorkspace: opts.WorkspaceParent,
		Diagnostics:               diagnostics,
	}
	return obs, nil
}

// trimPath is a tiny helper used by tests / CLI to print
// diagnostic workspace paths without leaking the absolute path
// into canonical JSON. Not used by the orchestrator itself.
func trimPath(p string) string {
	if p == "" {
		return ""
	}
	parts := strings.Split(p, string(filepath.Separator))
	if len(parts) <= 2 {
		return p
	}
	return ".../" + strings.Join(parts[len(parts)-2:], string(filepath.Separator))
}
