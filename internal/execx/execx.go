// Package execx is a bounded subprocess seam used by BJJ to invoke
// external VCS tools (git, jj).
//
// Design constraints (enforced by this package, not by convention):
//
//   - argv is passed structurally; no shell interpolation.
//   - working directory is required; no implicit CWD inheritance.
//   - the environment is an explicit overlay on a curated base. By default
//     credential-related variables (GITHUB_TOKEN, GIT_ASKPASS, SSH_*, ...)
//     are stripped unless explicitly permitted.
//   - stdout and stderr are bounded; oversized output is truncated with
//     a deterministic sentinel rather than allowed to OOM the process.
//   - errors carry program, argv, exit status, and bounded output so
//     callers can produce actionable diagnostics without losing fidelity.
//   - environment values are never echoed by this package.
package execx

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// DefaultMaxOutputBytes bounds the amount of stdout/stderr a single
// subprocess call may produce before truncation kicks in.
const DefaultMaxOutputBytes = 1 << 20 // 1 MiB

// SensitiveEnvPrefixes enumerates variable-name prefixes that are stripped
// from the inherited environment by NewBaseEnv() unless re-allowed.
//
// The list is intentionally conservative: it targets well-known credential
// surfaces, not every conceivable secret. Callers may still opt in to
// specific names via AllowEnv.
var SensitiveEnvPrefixes = []string{
	"GITHUB_",
	"GH_",
	"GL_", // GitLab CLI tokens
	"BB_", // Bitbucket
	"GIT_ASKPASS",
	"GIT_CONFIG_GLOBAL",
	"GIT_CONFIG_SYSTEM",
	"SSH_",
	"GNUPG_",
}

// SensitiveEnvExact enumerates variable names (not prefixes) stripped
// unconditionally because they are commonly credential-bearing.
var SensitiveEnvExact = []string{
	"HOME",        // BJJ tests should not inherit dev's home/credential helper
	"XDG_CONFIG_HOME",
	"XDG_CACHE_HOME",
	"JUJUTSU_CONFIG",
	"JJ_CONFIG",
}

// Request describes a single subprocess invocation.
type Request struct {
	// Program is the absolute or PATH-resolved executable name.
	Program string

	// Args are passed structurally; no shell parsing is performed.
	Args []string

	// Dir is the working directory. Required.
	Dir string

	// Env is appended on top of the curated base produced by NewBaseEnv.
	// If Env is nil, the base alone is used.
	Env []string

	// Stdin, if non-nil, is piped to the child.
	Stdin io.Reader

	// MaxOutputBytes bounds captured stdout and stderr. Zero means
	// DefaultMaxOutputBytes.
	MaxOutputBytes int
}

// Result captures the bounded outcome of a subprocess invocation.
type Result struct {
	Stdout   []byte
	Stderr   []byte
	ExitCode int
	Err      error
}

// TruncationMarker is appended to truncated stdout/stderr buffers so callers
// can detect that the captured output was bounded.
const TruncationMarker = "\n[execx: output truncated]\n"

// NewBaseEnv returns a minimal environment suitable for executing VCS
// tooling inside the BJJ lab. It deliberately strips credential-bearing
// variables and forces a deterministic PATH.
func NewBaseEnv() []string {
	allow := map[string]struct{}{
		"PATH":   {},
		"LANG":   {},
		"LC_ALL": {},
		"TZ":     {},
		"TMPDIR": {},
		"TERM":   {},
	}
	exactSensitive := make(map[string]struct{}, len(SensitiveEnvExact))
	for _, n := range SensitiveEnvExact {
		exactSensitive[n] = struct{}{}
	}
	base := make([]string, 0, len(allow)+4)
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		if _, sensitive := exactSensitive[name]; sensitive {
			continue
		}
		if hasPrefixAny(name, SensitiveEnvPrefixes) {
			continue
		}
		if _, ok := allow[name]; ok {
			base = append(base, kv)
		}
	}
	if !hasKey(base, "PATH") {
		base = append(base, "PATH=/usr/bin:/bin:/usr/sbin:/sbin:/usr/local/bin")
	}
	return base
}

func hasPrefixAny(name string, prefixes []string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}

// AllowEnv re-adds a specific sensitive variable name to the base env
// by reading its current value from the process environment.
func AllowEnv(name string) string {
	v, _ := os.LookupEnv(name)
	return name + "=" + v
}

func hasKey(env []string, name string) bool {
	prefix := name + "="
	for _, kv := range env {
		if strings.HasPrefix(kv, prefix) {
			return true
		}
	}
	return false
}

// Run executes req and returns the bounded Result.
//
// Run never panics on a missing executable; instead it returns a Result
// with Err set so callers can produce actionable prerequisite errors.
func Run(ctx context.Context, req Request) Result {
	if req.Program == "" {
		return Result{Err: errors.New("execx: empty Program")}
	}
	if req.Dir == "" {
		return Result{Err: errors.New("execx: empty Dir")}
	}
	if !filepath.IsAbs(req.Dir) {
		return Result{Err: fmt.Errorf("execx: Dir must be absolute, got %q", req.Dir)}
	}
	if req.MaxOutputBytes <= 0 {
		req.MaxOutputBytes = DefaultMaxOutputBytes
	}

	cmd := exec.CommandContext(ctx, req.Program, req.Args...)
	cmd.Dir = req.Dir
	cmd.Env = append(NewBaseEnv(), req.Env...)
	cmd.Stdin = req.Stdin

	var stdoutBuf, stderrBuf boundedBuffer
	stdoutBuf.limit = req.MaxOutputBytes
	stderrBuf.limit = req.MaxOutputBytes
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	err := cmd.Run()
	res := Result{
		Stdout: stdoutBuf.Bytes(),
		Stderr: stderrBuf.Bytes(),
	}
	var ee *exec.ExitError
	switch {
	case err == nil:
		res.ExitCode = 0
	case errors.As(err, &ee):
		res.ExitCode = ee.ExitCode()
		res.Err = fmt.Errorf("execx: %s %s: exit %d: %s",
			req.Program, summarizeArgv(req.Args), res.ExitCode, stderrSnippet(res.Stderr))
	default:
		res.ExitCode = -1
		res.Err = fmt.Errorf("execx: %s %s: %w",
			req.Program, summarizeArgv(req.Args), err)
	}
	return res
}

// summarizeArgv returns a short printable representation of argv.
func summarizeArgv(argv []string) string {
	const max = 8
	if len(argv) > max {
		return strings.Join(argv[:max], " ") + " ...(" +
			fmt.Sprintf("%d more", len(argv)-max) + ")"
	}
	return strings.Join(argv, " ")
}

func stderrSnippet(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 200 {
		return s[:200] + "..."
	}
	return s
}

// boundedBuffer is an io.Writer that caps memory growth and records
// whether truncation occurred.
type boundedBuffer struct {
	buf       bytes.Buffer
	limit     int
	truncated bool
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	if b.truncated {
		return len(p), nil
	}
	remaining := b.limit - b.buf.Len()
	if remaining <= 0 {
		b.markTruncated()
		return len(p), nil
	}
	if len(p) <= remaining {
		_, _ = b.buf.Write(p)
		return len(p), nil
	}
	_, _ = b.buf.Write(p[:remaining])
	b.markTruncated()
	return len(p), nil
}

func (b *boundedBuffer) markTruncated() {
	if b.truncated {
		return
	}
	b.truncated = true
	_, _ = b.buf.WriteString(TruncationMarker)
}

func (b *boundedBuffer) Bytes() []byte {
	out := b.buf.Bytes()
	cp := make([]byte, len(out))
	copy(cp, out)
	return cp
}
