// Package check implements ACT-BJJ-CHECK01 — BJJ's bound Factory
// verification layer.
//
// It answers one question:
//
//	Given an ADMITTED publication subject, did the sanctioned
//	verifier actually execute the required checks against the exact
//	frozen candidate tree that admission just approved?
//
// The contract is intentionally narrow:
//
//   - The check target is the NEW commit of the admitted
//     PublishPlan.BookmarkMoves[0], pinned at the SAME
//     PlanObservation.SourceOperationID used by admission.
//   - The materializer reads files from Jujutsu's frozen operation
//     view into a disposable directory; the live working copy is
//     NEVER consulted.
//   - The aggregator is pure Go: same inputs => byte-identical
//     CheckResult JSON, regardless of execution order or
//     map iteration.
//   - Checks cannot invoke transport (git push/fetch, jj git
//     push/fetch) or mutating jj subcommands. Static guards in
//     internal/plan/safety_test.go enforce this.
//
// The package performs no subprocess invocation directly. It
// consumes an injected Materializer and an injected Runner so the
// domain logic (subject binding, pure aggregation) can be tested
// without spinning up real processes.
package check

// SchemaVersion is the version of the CheckResult JSON contract.
// It is incremented on incompatible changes.
//
// CHECK01-CORRECTION01 bumped this to 2 to reflect:
//
//   - Subject identity now has FOUR fields, matching
//     admission.SubjectIdentity (SourceOperationID moved out of
//     the canonical body).
//   - FileEntry gained Kind and Executable; Mode is still
//     included for backward compatibility but is now derived
//     from (Kind, Executable) per the new materialization rules.
//   - CheckObservation introduced as the envelope for opID,
//     resolved absolute program paths, workspace paths,
//     durations, and bounded stdout/stderr. None of those fields
//     live in the canonical CheckResult any more.
const SchemaVersion = 2

// CheckStatus is the discrete per-check outcome.
//
// Semantics:
//
//	PASS   - verifier executed and reported success (exit 0 where
//	         success is the canonical signal).
//	FAIL   - verifier executed successfully as a process but the
//	         check itself failed (e.g. go test exits 1 because a
//	         test assertion failed).
//	ERROR  - verifier could not be executed reliably (missing
//	         executable, timeout, materialization failure,
//		manifest drift).
type CheckStatus string

const (
	// StatusPass means the check executed and reported success.
	StatusPass CheckStatus = "pass"

	// StatusFail means the check executed but reported failure.
	// This is a property of the subject tree, not an
	// infrastructure error.
	StatusFail CheckStatus = "fail"

	// StatusError means the check could not be executed reliably.
	// This is an infrastructure / process-level failure distinct
	// from the subject failing the check.
	StatusError CheckStatus = "error"
)

// CheckID is the stable identifier of a CheckSpec. The aggregator
// orders outcomes by ID, so IDs must be unique within a profile and
// stable across runs.
type CheckID string

const (
	// CheckIDGofmt is the `gofmt -l .` tree-local format check.
	CheckIDGofmt CheckID = "gofmt"

	// CheckIDGoVet is the `go vet ./...` tree-local static-analysis
	// check.
	CheckIDGoVet CheckID = "go_vet"

	// CheckIDGoTest is the `go test -count=1 ./...` tree-local
	// test execution check.
	CheckIDGoTest CheckID = "go_test"

	// CheckIDGoBuild is the `go build ./...` tree-local
	// compilation check.
	CheckIDGoBuild CheckID = "go_build"
)

// EntryKind enumerates the semantic kinds of tree entries the
// materializer recognises. The CHECK01-CORRECTION01 manifest
// preserves these so reverify can detect chmod tampering and
// other post-materialize edits.
//
// KIND values are intentionally the same string the underlying
// Jujutsu `file_type()` template method emits, except that we
// add KIND_TREE for directory entries (jj doesn't list those
// from `file list` but the kind field is reserved so future
// expansion is wire-compatible).
type EntryKind string

const (
	// KindFile is a regular file. The Executable flag decides
	// whether the materializer writes 0o644 or 0o755.
	KindFile EntryKind = "file"

	// KindSymlink is a symbolic link. CHECK01 does NOT support
	// materializing symlinks; the materializer fails closed
	// with CodeUnsupportedTreeEntry.
	KindSymlink EntryKind = "symlink"

	// KindGitSubmodule is a Git submodule pointer. CHECK01 does
	// NOT support materializing submodules; the materializer
	// fails closed with CodeUnsupportedTreeEntry.
	KindGitSubmodule EntryKind = "git-submodule"

	// KindConflict is an unresolved merge conflict. CHECK01
	// refuses to verify a conflicted tree.
	KindConflict EntryKind = "conflict"

	// KindTree is a directory placeholder (reserved for future
	// use; jj's `file list` does not currently emit these).
	KindTree EntryKind = "tree"
)

// CheckSpec describes a single verifiable check.
//
// For CHECK01-CORRECTION01 Program is the LOGICAL program name
// (e.g. "gofmt", "go"). The Runner resolves the absolute path
// at run time and records it in CheckObservation.ResolvedProgram,
// which is NOT part of the canonical CheckOutcome.
//
// This split keeps the canonical body reproducible across
// machines (the logical name is stable; the absolute path
// depends on the host's toolchain location).
//
// CheckSpec carries no environment data; environment is curated
// by the Runner (which inherits from internal/execx's base env
// and overlays GOFLAGS=-mod=readonly). Offline Go execution
// (GOPROXY=off) is a separate ACT and is NOT part of the
// CHECK01 Runner contract.
type CheckSpec struct {
	// ID is the stable identifier; used for ordering and JSON.
	ID CheckID

	// Program is the LOGICAL program name (e.g. "gofmt", "go").
	// Resolution to an absolute path happens in the Runner; the
	// resolved path is recorded on CheckDiagnostic.ResolvedProgram,
	// not in the canonical outcome. This keeps canonical
	// CheckResult bytes host-independent.
	Program string

	// Argv is the argument vector passed to Program.
	Argv []string

	// TimeoutMillis is the per-check wall-clock budget in
	// milliseconds. Zero means use the runner default.
	TimeoutMillis int
}

// CheckOutcome is the typed result of executing one CheckSpec.
// It is the runner-internal envelope; the canonical, JSON-stable
// subset is:
//
//   - ID + Status + ExitCode + ErrorCode/ErrorMessage
//
// CheckOutcome ALSO carries observation-only fields
// (Program, Argv, Stdout, Stderr, WorkspacePath,
// ResolvedProgram, DurationMillis, Truncated flags). The
// orchestrator is responsible for splitting them: the canonical
// subset goes into CheckResult.Checks; the observation subset
// goes into CheckObservation.Diagnostics.
//
// The split mirrors PLAN01's separation of canonical
// PublishPlan from PlanObservation and is the precondition for
// CHECK_REPEATED_EXECUTION_CANONICAL_RESULT_IDENTICAL.
type CheckOutcome struct {
	// ID mirrors CheckSpec.ID for ordering stability.
	ID CheckID `json:"id"`

	// Status is one of pass | fail | error.
	Status CheckStatus `json:"status"`

	// ExitCode is the captured process exit code. -1 means the
	// process could not be started at all (e.g. executable
	// missing); in that case Status is ERROR and ErrorCode is
	// populated.
	ExitCode int `json:"exit_code"`

	// ErrorCode is the typed infrastructure-error code when
	// Status is ERROR. Empty when Status is PASS or FAIL.
	ErrorCode ErrorCode `json:"error_code,omitempty"`

	// ErrorMessage is a short human-readable diagnostic suitable
	// for CLI rendering. Empty when Status is PASS or FAIL.
	// MUST NOT contain captured stdout/stderr (those live in
	// CheckObservation).
	ErrorMessage string `json:"error_message,omitempty"`

	// Program is the logical program name from CheckSpec. Echoed
	// in canonical JSON for transparency. The ABSOLUTE path is
	// NOT echoed here (it lives in ResolvedProgram, which is
	// observation-only).
	Program string `json:"program"`

	// Argv is the argument vector actually invoked. Echoed in
	// canonical JSON for reproducibility.
	Argv []string `json:"argv"`

	// ----- observation-only fields (NOT in canonical JSON) -----

	// ResolvedProgram is the absolute path of the executable
	// that was actually invoked (host-dependent).
	ResolvedProgram string `json:"-"`

	// Stdout / Stderr are the bounded captures.
	Stdout []byte `json:"-"`
	Stderr []byte `json:"-"`

	// StdoutTruncated / StderrTruncated are true when the
	// underlying buffer hit its cap.
	StdoutTruncated bool `json:"-"`
	StderrTruncated bool `json:"-"`

	// DurationMillis is the wall-clock duration.
	DurationMillis int64 `json:"-"`

	// WorkspacePath is the disposable directory the check ran in.
	WorkspacePath string `json:"-"`
}

// CheckDiagnostic is one bounded chunk of process I/O captured
// during a check. Diagnostics live in CheckObservation only;
// they never appear in the canonical CheckResult.
type CheckDiagnostic struct {
	// ID mirrors CheckOutcome.ID.
	ID CheckID `json:"id"`

	// ResolvedProgram is the absolute path of the executable
	// actually invoked (host-dependent; e.g.
	// /usr/local/go/bin/go).
	ResolvedProgram string `json:"resolved_program"`

	// Argv is the argument vector actually invoked.
	Argv []string `json:"argv"`

	// WorkspacePath is the disposable directory the check ran in.
	WorkspacePath string `json:"workspace_path"`

	// Stdout / Stderr are the bounded captures (up to 1 MiB each
	// per verdict §23).
	Stdout []byte `json:"stdout,omitempty"`
	Stderr []byte `json:"stderr,omitempty"`

	// StdoutTruncated / StderrTruncated are true when the
	// underlying buffer hit its cap.
	StdoutTruncated bool `json:"stdout_truncated,omitempty"`
	StderrTruncated bool `json:"stderr_truncated,omitempty"`

	// DurationMillis is the wall-clock duration.
	DurationMillis int64 `json:"duration_millis"`
}

// CheckObservation is the observation envelope around a
// CheckResult. It carries every piece of volatile, host-dependent,
// or non-semantic data the canonical body must NOT carry:
//
//   - SourceOperationID: the pinned Jujutsu operation id
//   - ResolvedAbsoluteWorkspace: the (parental) directory under
//     which the per-check workspaces were created
//   - Diagnostics: per-check bounded stdout/stderr, resolved
//     program paths, per-check workspace paths, durations
//
// The shape mirrors PLAN01's `PlanObservation`:
//
//	PlanObservation = { plan, sourceOperationID }
//	CheckObservation = { result, sourceOperationID, diagnostics }
//
// Two observations of the same semantic subject at DIFFERENT
// Jujutsu operations are allowed to differ in CheckObservation;
// they MUST agree on CheckResult (subject + checks).
type CheckObservation struct {
	// Result is the canonical CheckResult.
	Result CheckResult `json:"result"`

	// SourceOperationID is the Jujutsu operation id the
	// materializer was pinned to. Observation provenance, not
	// semantic subject identity.
	SourceOperationID string `json:"source_operation_id"`

	// ResolvedAbsoluteWorkspace is the parent directory under
	// which per-check workspaces were created (the orchestrator's
	// Options.WorkspaceParent, usually a temp dir created by the
	// CLI). Diagnostic only.
	ResolvedAbsoluteWorkspace string `json:"resolved_workspace_parent,omitempty"`

	// Diagnostics is the per-check observation slice, in
	// profile-declaration order (NOT canonical ID order; that
	// ordering is preserved in Result.Checks).
	Diagnostics []CheckDiagnostic `json:"diagnostics,omitempty"`
}

// SubjectIdentity is the typed subject identity carried inside a
// CheckResult so callers can cross-check which subject the checks
// were bound to.
//
// CHECK01-CORRECTION01 makes this structurally equal to
// admission.SubjectIdentity (Remote, Bookmark, OldCommitID,
// NewCommitID). The fifth field that CHECK01 originally
// carried (SourceOperationID) has moved to CheckObservation,
// restoring the canonical-vs-observation boundary that PLAN01
// established.
//
// (SubjectDigest, which would make this binding cryptographic,
// belongs to a later ACT.)
type SubjectIdentity struct {
	// Remote is the logical remote name from the source plan.
	Remote string `json:"remote"`

	// Bookmark is the bookmark name from the source plan.
	Bookmark string `json:"bookmark"`

	// OldCommitID is the OLD target commit id, or empty if the
	// remote-tracking bookmark is absent (creation).
	OldCommitID string `json:"old_commit_id"`

	// NewCommitID is the NEW target commit id — the exact commit
	// the materializer must export.
	NewCommitID string `json:"new_commit_id"`
}

// CheckResult is the typed, canonical output of the check phase.
//
// Two equivalent runs produce byte-identical JSON: aggregation is
// pure, ordering is by CheckID (stable), and no execution-time
// data (wall-clock duration, workspace path, PID) leaks into the
// canonical body.
type CheckResult struct {
	// SchemaVersion identifies the JSON contract.
	SchemaVersion int `json:"schema_version"`

	// Status is the aggregate status:
	//   any ERROR in Checks -> "error"
	//   else any FAIL       -> "fail"
	//   else                -> "pass"
	Status CheckStatus `json:"status"`

	// Subject is the typed subject identity the checks ran
	// against. The caller MUST compare this against the matching
	// AdmissionResult.Subject to prove CHECK_SUBJECT ==
	// ADMITTED_SUBJECT.
	Subject SubjectIdentity `json:"subject"`

	// Checks is the list of per-check outcomes in canonical
	// (CheckID-sorted) order.
	Checks []CheckOutcome `json:"checks"`

	// FileCount is the number of materialized files in the
	// disposable verification workspace. Diagnostic; not part
	// of any cryptographic subject binding.
	FileCount int `json:"file_count"`
}

// Exit codes for `bjj check`.
//
// Per ACT-BJJ-CHECK01 §27:
//
//	0 = all checks PASS
//	1 = invalid arguments
//	2 = infrastructure ERROR (materialization, manifest, exec)
//	3 = admission deny/not_needed
//	4 = checks executed and at least one FAIL
const (
	// ExitCheckOK = all checks PASS.
	ExitCheckOK = 0
	// ExitCheckInvalidArgs = invalid CLI invocation.
	ExitCheckInvalidArgs = 1
	// ExitCheckInfraError = infrastructure ERROR (materialization,
	// manifest drift, exec failure, timeout, etc.).
	ExitCheckInfraError = 2
	// ExitCheckAdmission = admission did not admit (deny or
	// not_needed). The check path is skipped entirely.
	ExitCheckAdmission = 3
	// ExitCheckFailed = checks executed and at least one FAILed.
	ExitCheckFailed = 4
)
