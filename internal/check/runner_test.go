package check

import (
	"context"
	"os/exec"
	"strings"
	"testing"
)

// TestExecRunner_PassingCommand is the happy-path coverage:
// `true` exits 0 and the runner reports StatusPass.
//
// On macOS /usr/bin/true exists; on Linux /bin/true. We use
// exec.LookPath so the test is portable.
func TestExecRunner_PassingCommand(t *testing.T) {
	prog, err := exec.LookPath("true")
	if err != nil {
		t.Skip("`true` not found on PATH")
	}
	spec := CheckSpec{
		ID:      CheckIDGofmt,
		Program: prog,
		Argv:    []string{},
	}
	out := ExecRunner{}.Run(context.Background(), spec, t.TempDir(), 1024)
	if out.Status != StatusPass {
		t.Fatalf("status: got %q want %q (err=%s)", out.Status, StatusPass, out.ErrorMessage)
	}
	if out.ExitCode != 0 {
		t.Fatalf("exit code: got %d want 0", out.ExitCode)
	}
	if out.ID != CheckIDGofmt || out.Program != prog {
		t.Fatalf("outcome echo wrong: %+v", out)
	}
	if out.WorkspacePath == "" {
		t.Fatalf("WorkspacePath should be set (diagnostic only)")
	}
}

// TestExecRunner_FailingCommand asserts that a process exit of
// non-zero is reported as StatusFail, NOT StatusError.
func TestExecRunner_FailingCommand(t *testing.T) {
	prog, err := exec.LookPath("false")
	if err != nil {
		t.Skip("`false` not found on PATH")
	}
	spec := CheckSpec{
		ID:      CheckIDGoTest,
		Program: prog,
		Argv:    []string{},
	}
	out := ExecRunner{}.Run(context.Background(), spec, t.TempDir(), 1024)
	if out.Status != StatusFail {
		t.Fatalf("status: got %q want %q", out.Status, StatusFail)
	}
	if out.ExitCode == 0 {
		t.Fatalf("expected non-zero exit code, got 0")
	}
}

// TestExecRunner_ExecutableMissing asserts that a missing
// executable is reported as StatusError + CodeExecFailed.
func TestExecRunner_ExecutableMissing(t *testing.T) {
	spec := CheckSpec{
		ID:      CheckIDGofmt,
		Program: "/no/such/executable/bjj-test-xyz",
		Argv:    []string{},
	}
	out := ExecRunner{}.Run(context.Background(), spec, t.TempDir(), 1024)
	if out.Status != StatusError {
		t.Fatalf("status: got %q want %q", out.Status, StatusError)
	}
	if out.ErrorCode != CodeExecFailed {
		t.Fatalf("error code: got %q want %q", out.ErrorCode, CodeExecFailed)
	}
	if out.ExitCode != -1 {
		t.Fatalf("exit code: got %d want -1", out.ExitCode)
	}
}

// TestExecRunner_EmptyProgramIsHardError asserts the empty-
// program guard.
func TestExecRunner_EmptyProgramIsHardError(t *testing.T) {
	spec := CheckSpec{ID: CheckIDGofmt, Program: "", Argv: nil}
	out := ExecRunner{}.Run(context.Background(), spec, t.TempDir(), 1024)
	if out.Status != StatusError || out.ErrorCode != CodeExecFailed {
		t.Fatalf("got status=%q code=%q want StatusError/CodeExecFailed", out.Status, out.ErrorCode)
	}
}

// TestExecRunner_EmptyWorkspaceIsHardError asserts the empty-
// workspace guard.
func TestExecRunner_EmptyWorkspaceIsHardError(t *testing.T) {
	trueProg, err := exec.LookPath("true")
	if err != nil {
		t.Skip("`true` not found on PATH")
	}
	spec := CheckSpec{ID: CheckIDGofmt, Program: trueProg, Argv: nil}
	out := ExecRunner{}.Run(context.Background(), spec, "", 1024)
	if out.Status != StatusError || out.ErrorCode != CodeExecFailed {
		t.Fatalf("got status=%q code=%q want StatusError/CodeExecFailed", out.Status, out.ErrorCode)
	}
}

// TestExecRunner_TimeoutIsDistinctError asserts that a
// context-cancelled timeout is reported as StatusError +
// CodeTimeout, not StatusFail.
func TestExecRunner_TimeoutIsDistinctError(t *testing.T) {
	sleepProg, err := exec.LookPath("sleep")
	if err != nil {
		t.Skip("`sleep` not found on PATH")
	}
	spec := CheckSpec{
		ID:            CheckIDGoTest,
		Program:       sleepProg,
		Argv:          []string{"5"},
		TimeoutMillis: 100,
	}
	out := ExecRunner{}.Run(context.Background(), spec, t.TempDir(), 1024)
	if out.Status != StatusError {
		t.Fatalf("status: got %q want %q", out.Status, StatusError)
	}
	if out.ErrorCode != CodeTimeout {
		t.Fatalf("error code: got %q want %q", out.ErrorCode, CodeTimeout)
	}
}

// TestExecRunner_ParentContextCancelled propagates cancellation.
//
// We use `false` (which exits immediately) so the runner is
// never waiting on a long-running process; the parent context is
// pre-cancelled and we assert the runner does not produce
// StatusPass.
func TestExecRunner_ParentContextCancelled(t *testing.T) {
	falseProg, err := exec.LookPath("false")
	if err != nil {
		t.Skip("`false` not found on PATH")
	}
	spec := CheckSpec{
		ID:      CheckIDGoTest,
		Program: falseProg,
		Argv:    []string{},
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	out := ExecRunner{}.Run(ctx, spec, t.TempDir(), 1024)
	// Either StatusError (context cancelled before exec) or
	// StatusFail (process exited non-zero) is acceptable; the
	// critical assertion is that we do NOT get StatusPass.
	if out.Status == StatusPass {
		t.Fatalf("cancelled parent context should not produce StatusPass")
	}
}

// TestExecRunner_TruncationFlagged covers verdict §23.
//
// We use `printf` with a long argument (well under 1 MiB but
// larger than the cap) so the test is portable and does not
// depend on `yes` or process-pipe buffering semantics that
// vary by platform.
func TestExecRunner_TruncationFlagged(t *testing.T) {
	printfProg, err := exec.LookPath("printf")
	if err != nil {
		t.Skip("`printf` not found on PATH")
	}
	// 1 KiB of "x"; cap at 64 bytes -> truncation MUST occur.
	big := strings.Repeat("x", 1024)
	spec := CheckSpec{
		ID:      CheckIDGofmt,
		Program: printfProg,
		Argv:    []string{"%s", big},
	}
	out := ExecRunner{}.Run(context.Background(), spec, t.TempDir(), 64)
	if !out.StdoutTruncated {
		t.Fatalf("expected StdoutTruncated=true, got false (stdout=%d bytes)", len(out.Stdout))
	}
}

// TestDefaultTimeoutFor confirms compiled defaults.
func TestDefaultTimeoutFor(t *testing.T) {
	cases := map[CheckID]int{
		CheckIDGofmt:   DefaultGofmtTimeoutMillis,
		CheckIDGoVet:   DefaultGoVetTimeoutMillis,
		CheckIDGoTest:  DefaultGoTestTimeoutMillis,
		CheckIDGoBuild: DefaultGoBuildTimeoutMillis,
	}
	for id, want := range cases {
		if got := defaultTimeoutFor(id); got != want {
			t.Fatalf("defaultTimeoutFor(%s) = %d, want %d", id, got, want)
		}
	}
	if got := defaultTimeoutFor("unknown"); got != 60_000 {
		t.Fatalf("unknown id should fall back to 60s; got %d", got)
	}
}

// TestExecRunner_DurationIsDiagnostic.
func TestExecRunner_DurationIsDiagnostic(t *testing.T) {
	trueProg, err := exec.LookPath("true")
	if err != nil {
		t.Skip("`true` not found on PATH")
	}
	spec := CheckSpec{ID: CheckIDGofmt, Program: trueProg, Argv: nil}
	out := ExecRunner{}.Run(context.Background(), spec, t.TempDir(), 1024)
	if out.DurationMillis < 0 {
		t.Fatalf("DurationMillis should be >= 0, got %d", out.DurationMillis)
	}
	if out.DurationMillis > 5000 {
		t.Fatalf("DurationMillis suspiciously large: %d", out.DurationMillis)
	}
}
