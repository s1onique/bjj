package check

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/s1onique/bjj/internal/execx"
)

// Default timeouts per check (milliseconds).
//
// Per ACT-BJJ-CHECK01 §24 these are the compile-time defaults. A
// future ACT could expose configuration; CHECK01 keeps them static
// because authoritative verification-policy provenance is not yet
// solved.
const (
	DefaultGofmtTimeoutMillis   = 30_000
	DefaultGoVetTimeoutMillis   = 60_000
	DefaultGoTestTimeoutMillis  = 120_000
	DefaultGoBuildTimeoutMillis = 60_000
)

// Default output caps (bytes) per stream.
//
// Per ACT-BJJ-CHECK01 §23 each check uses bounded capture with a
// fixed constant. execx.DefaultMaxOutputBytes is 1 MiB; for
// go_test that is more than enough, but gofmt and go build emit
// essentially nothing, so we keep the same default for uniformity.
const (
	DefaultGofmtOutputBytes   = execx.DefaultMaxOutputBytes
	DefaultGoVetOutputBytes   = execx.DefaultMaxOutputBytes
	DefaultGoTestOutputBytes  = execx.DefaultMaxOutputBytes
	DefaultGoBuildOutputBytes = execx.DefaultMaxOutputBytes
)

// Runner abstracts how CheckSpec execution happens.
//
// The default Runner is ExecRunner; tests inject fake runners that
// return deterministic CheckOutcome values without spawning real
// processes.
//
// CHECK01-CORRECTION01: the Runner returns a CheckOutcome that
// contains BOTH canonical (ID/Status/ExitCode/ErrorCode/Message)
// AND diagnostic (Program/Argv/Stdout/Stderr/WorkspacePath/
// DurationMillis) fields. The orchestrator is responsible for
// stripping the diagnostic fields before placing the value into
// the canonical CheckResult; they live on in the
// CheckObservation.Diagnostics slice.
type Runner interface {
	// Run executes spec in workspaceDir and returns the typed
	// outcome. Implementations MUST:
	//
	//   - honour spec.TimeoutMillis (or a sensible default)
	//   - bound stdout and stderr to at most maxOutputBytes each
	//   - report executable-missing as ERROR + CodeExecFailed,
	//     never as PASS or FAIL
	//   - report timeout as ERROR + CodeTimeout, never as FAIL
	Run(ctx context.Context, spec CheckSpec, workspaceDir string, maxOutputBytes int) CheckOutcome
}

// ExecRunner is the production Runner. It invokes the check
// process via internal/execx, which already enforces:
//
//   - absolute working directory
//   - argv passed structurally
//   - bounded stdout/stderr
//   - credential-bearing environment stripped
//
// ExecRunner adds:
//
//   - per-check timeout (context.WithTimeout)
//   - classification of context.DeadlineExceeded -> ERROR + CodeTimeout
//   - classification of executable-missing (-1 exit + exec.ErrNotFound)
//     -> ERROR + CodeExecFailed
//   - diagnostic capture of wall-clock duration and workspace path
//     (DIAGNOSTIC ONLY; never echoed in canonical JSON)
type ExecRunner struct{}

// NewExecRunner returns a production Runner.
func NewExecRunner() ExecRunner { return ExecRunner{} }

// resolvedProgram returns the absolute path of spec.Program,
// falling back to spec.Program itself if resolution fails.
// Used to populate CheckDiagnostic.ResolvedProgram so the
// diagnostic reflects what was actually invoked.
func resolvedProgram(spec CheckSpec) string {
	if spec.Program == "" {
		return ""
	}
	if p, err := exec.LookPath(spec.Program); err == nil {
		return p
	}
	return spec.Program
}

// Run implements Runner.
func (ExecRunner) Run(ctx context.Context, spec CheckSpec, workspaceDir string, maxOutputBytes int) CheckOutcome {
	start := time.Now()
	out := CheckOutcome{
		ID:            spec.ID,
		Program:       spec.Program,
		Argv:          append([]string(nil), spec.Argv...),
		WorkspacePath: workspaceDir,
	}
	out.ResolvedProgram = resolvedProgram(spec)

	if spec.Program == "" {
		out.Status = StatusError
		out.ExitCode = -1
		out.ErrorCode = CodeExecFailed
		out.ErrorMessage = "check: empty program"
		out.DurationMillis = time.Since(start).Milliseconds()
		return out
	}
	if workspaceDir == "" {
		out.Status = StatusError
		out.ExitCode = -1
		out.ErrorCode = CodeExecFailed
		out.ErrorMessage = "check: empty workspace dir"
		out.DurationMillis = time.Since(start).Milliseconds()
		return out
	}
	if maxOutputBytes <= 0 {
		maxOutputBytes = execx.DefaultMaxOutputBytes
	}

	timeout := spec.TimeoutMillis
	if timeout <= 0 {
		timeout = defaultTimeoutFor(spec.ID)
	}

	cctx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Millisecond)
	defer cancel()

	// The Go toolchain needs GOCACHE to be defined; execx
	// strips HOME (per the security policy) so we must set a
	// per-workspace GOCACHE explicitly. We do this in the
	// Env overlay rather than the base env so it scopes to
	// the check process alone.
	gocache := filepath.Join(workspaceDir, ".bjj-go-cache")
	_ = os.MkdirAll(gocache, 0o700)
	// CHECK01-CORRECTION01 §2: GOFLAGS=-mod=readonly. A
	// candidate that requires modifying go.mod / go.sum to
	// verify must FAIL/ERROR rather than have BJJ silently
	// repair the module metadata. -mod=readonly makes the Go
	// toolchain reject any attempt to update those files.
	env := []string{"GOCACHE=" + gocache, "GOFLAGS=-mod=readonly"}

	res := execx.Run(cctx, execx.Request{
		Program:        spec.Program,
		Args:           spec.Argv,
		Dir:            workspaceDir,
		Env:            env,
		MaxOutputBytes: maxOutputBytes,
	})

	out.DurationMillis = time.Since(start).Milliseconds()
	out.Stdout = res.Stdout
	out.Stderr = res.Stderr
	out.ExitCode = res.ExitCode

	// Truncation detection: execx inserts a marker when the
	// buffer hit its cap. We do not parse the marker; we trust
	// the byte lengths against the cap.
	out.StdoutTruncated = len(res.Stdout) >= maxOutputBytes
	out.StderrTruncated = len(res.Stderr) >= maxOutputBytes

	if res.Err != nil {
		// Classify the error.
		if errors.Is(cctx.Err(), context.DeadlineExceeded) {
			out.Status = StatusError
			out.ErrorCode = CodeTimeout
			out.ErrorMessage = fmt.Sprintf("check %s: timeout after %dms", spec.ID, timeout)
			return out
		}
		// Missing executable is the most common "could not
		// start" case; classify it BEFORE the ExitCode != 0
		// branch because on some platforms os/exec sets a
		// non-zero exit code when the binary is missing.
		if isExecNotFound(res.Err) {
			out.Status = StatusError
			out.ErrorCode = CodeExecFailed
			out.ErrorMessage = fmt.Sprintf("check %s: executable not found: %s", spec.ID, spec.Program)
			return out
		}
		// Non-zero exit but still a process-level error wrapper
		// (execx sets ExitCode from exec.ExitError when available).
		// That is a check FAIL, not an ERROR.
		if res.ExitCode != 0 {
			out.Status = StatusFail
			return out
		}
		// Process returned 0 but res.Err is set — surface as
		// infrastructure error to avoid silently masking a
		// problem.
		out.Status = StatusError
		out.ErrorCode = CodeExecFailed
		out.ErrorMessage = fmt.Sprintf("check %s: exec error: %v", spec.ID, res.Err)
		return out
	}
	if res.ExitCode == 0 {
		// Special case: `gofmt -l .` exits 0 even when it
		// finds files needing formatting; the failing
		// signal is non-empty stdout. We classify that as
		// FAIL rather than PASS so the check profile
		// actually proves what it claims.
		if spec.ID == CheckIDGofmt && len(trimTrailingNewlines(res.Stdout)) > 0 {
			out.Status = StatusFail
			return out
		}
		out.Status = StatusPass
		return out
	}
	out.Status = StatusFail
	return out
}

// trimTrailingNewlines strips trailing whitespace from a byte
// slice, used to ignore the trailing newline gofmt emits even
// when the result list is otherwise empty.
func trimTrailingNewlines(b []byte) []byte {
	for len(b) > 0 && (b[len(b)-1] == '\n' || b[len(b)-1] == '\r' || b[len(b)-1] == ' ' || b[len(b)-1] == '\t') {
		b = b[:len(b)-1]
	}
	return b
}

// isExecNotFound reports whether err originates from
// exec.LookPath failing inside os/exec, or from a process-spawn
// failure because the binary does not exist on the platform.
//
// os/exec returns *exec.Error (which wraps ErrNotFound) when
// the binary cannot be found, but on some platforms it can also
// surface as fs.ErrNotExist via a different wrapping. We accept
// both so the classification is robust across Linux, macOS,
// and Windows.
func isExecNotFound(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, exec.ErrNotFound) {
		return true
	}
	// os/exec wraps the *exec.Error{Name, Err} pair; Err is
	// fs.ErrNotExist on some platforms when the file does not
	// exist but PATH resolution succeeded to a missing file.
	if errors.Is(err, fs.ErrNotExist) {
		return true
	}
	return false
}

// defaultTimeoutFor returns the compiled default timeout for the
// given check id. Unknown IDs fall back to 60 seconds.
func defaultTimeoutFor(id CheckID) int {
	switch id {
	case CheckIDGofmt:
		return DefaultGofmtTimeoutMillis
	case CheckIDGoVet:
		return DefaultGoVetTimeoutMillis
	case CheckIDGoTest:
		return DefaultGoTestTimeoutMillis
	case CheckIDGoBuild:
		return DefaultGoBuildTimeoutMillis
	default:
		return 60_000
	}
}

// defaultOutputBytesFor returns the compiled default output cap for
// the given check id. Unknown IDs fall back to execx default.
func defaultOutputBytesFor(id CheckID) int {
	switch id {
	case CheckIDGofmt:
		return DefaultGofmtOutputBytes
	case CheckIDGoVet:
		return DefaultGoVetOutputBytes
	case CheckIDGoTest:
		return DefaultGoTestOutputBytes
	case CheckIDGoBuild:
		return DefaultGoBuildOutputBytes
	default:
		return execx.DefaultMaxOutputBytes
	}
}
