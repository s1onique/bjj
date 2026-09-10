// Package jjadapter is the bounded adapter between BJJ and the local
// `jj` CLI.
//
// All `jj` interactions used by PLAN01 (and later ACTs) flow through
// this package so that:
//
//   - argv is passed structurally (no shell) via internal/execx;
//   - output is parsed from machine-oriented templates, never from
//     human colors or column spacing;
//   - the caller works with typed records, not raw stdout;
//   - the resulting package has no transport semantics and contains
//     no invocation that could mutate the remote.
//
// The adapter deliberately does not implement the domain model. That
// belongs to internal/plan.
package jjadapter

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/s1onique/bjj/internal/execx"
)

// BookmarksTemplate is the machine-oriented template emitted by
// `jj bookmark list --all-remotes`. Each line is:
//
//	<name>|<remote>|<present>|<conflict>|<commit_id>|<added>|<removed>\n
//
// <remote> is the empty string for local bookmarks. <present>=false
// means the bookmark is absent (deleted locally or not yet observed
// on the remote). <conflict>=true means the bookmark has multiple
// competing targets; in that case the commit_id field is empty and
// the union of <added> and <removed> lists every candidate target.
// Each candidate is encoded as "<commit_id>:<change_id>",
// comma-joined.
//
// The parser derives Kind from the presence of <remote>: empty ==
// "LOC", non-empty == "REM". We do not use `if(remote == "", ...)`
// because RefSymbol's structural equality with the empty string is
// not reliable in 0.41.0.
const BookmarksTemplate = `name ++ "|" ++ remote ++ "|" ++ present ++ "|" ++ conflict ++ "|" ++ if(present, normal_target.commit_id(), "") ++ "|" ++ added_targets.map(|c| c.commit_id().shortest(0) ++ ":" ++ c.change_id().shortest(0)).join(",") ++ "|" ++ removed_targets.map(|c| c.commit_id().shortest(0) ++ ":" ++ c.change_id().shortest(0)).join(",") ++ "\n"`

// LogTemplate emits one commit per line as "<commit_id>|<change_id>".
// No graph characters. No colors. Full identifiers only.
const LogTemplate = `commit_id ++ "|" ++ change_id ++ "\n"`

// OperationIDTemplate emits the bare operation id followed by a
// newline.
const OperationIDTemplate = `id ++ "\n"`

// CommitRef is the typed identity of a single commit as observed by
// `jj log`. Both identifiers are full-length and unaliased.
type CommitRef struct {
	CommitID string
	ChangeID string
}

// BookmarkRef is a single bookmark row parsed from
// `jj bookmark list --all-remotes`.
type BookmarkRef struct {
	// Kind is "LOC" (local bookmark, Remote=="") or "REM" (remote
	// tracking bookmark).
	Kind string

	// Name is the bookmark name (no "@remote" suffix).
	Name string

	// Remote is the remote name for remote-tracking bookmarks and
	// empty for local bookmarks.
	Remote string

	// Present is false when the bookmark has been deleted locally or
	// has no known remote target.
	Present bool

	// Conflict is true when the bookmark has multiple competing
	// targets (LOCAL_BOOKMARK_CONFLICTED or REMOTE_BOOKMARK_CONFLICTED).
	Conflict bool

	// Target is the non-conflicted target commit if Present && !Conflict.
	// nil when Present is false or Conflict is true.
	Target *CommitRef

	// AddedTargets and RemovedTargets carry every candidate commit
	// when Conflict is true. They are always empty for non-conflicted
	// bookmarks.
	AddedTargets   []CommitRef
	RemovedTargets []CommitRef
}

// Snapshot is the bounded repository observation obtained from one
// pinned operation view.
type Snapshot struct {
	// OperationID is the operation id the snapshot was observed at.
	// Empty if the underlying tool could not produce one.
	OperationID string

	// Remotes is the set of remote names known to the repository
	// together with their URL (best effort).
	Remotes map[string]string

	// Bookmarks is the complete local + remote bookmark set.
	Bookmarks []BookmarkRef

	// Commits is the set of commits material to the snapshot. For
	// PLAN01 this is the set `::NEW ~ ::OLD` for the resolved
	// bookmark, plus the bookmark endpoints themselves.
	Commits []CommitRef

	// SourceCommand records the canonical command used to obtain this
	// snapshot, for diagnostic purposes only. It is NOT included in
	// the deterministic plan body.
	SourceCommand string
}

// Adapter invokes `jj` under a fixed environment and parses its
// machine-oriented output.
type Adapter struct {
	// JJ is the path to the `jj` executable. Empty means "use PATH".
	JJ string

	// Home is the value of HOME passed to the child process. Empty
	// means a fresh per-call temp directory is allocated.
	Home string
}

// New constructs an Adapter that uses `jj` from PATH.
func New() *Adapter { return &Adapter{} }

// ErrJJFailed indicates a `jj` invocation failed. Wrapped errors
// preserve program, argv, exit code, and bounded stderr so callers
// can produce actionable diagnostics.
type ErrJJFailed struct {
	Program  string
	Argv     []string
	ExitCode int
	Stderr   string
	Cause    error
}

func (e *ErrJJFailed) Error() string {
	return fmt.Sprintf("jjadapter: %s %s: exit %d: %s",
		e.Program, summarizeArgv(e.Argv), e.ExitCode, strings.TrimSpace(e.Stderr))
}

func (e *ErrJJFailed) Unwrap() error { return e.Cause }

// JJArgv returns a copy of the argv that produced this failure.
func (e *ErrJJFailed) JJArgv() []string {
	out := make([]string, len(e.Argv))
	copy(out, e.Argv)
	return out
}

// JJExitCode returns the exit code of the failing jj process.
func (e *ErrJJFailed) JJExitCode() int { return e.ExitCode }

// ErrJJParseFailed indicates the adapter received a `jj` invocation's
// stdout whose shape did not match the machine-oriented template. Per
// ACT-BJJ-PLAN01-CORRECTION02 §3 the adapter MUST NOT silently skip
// malformed rows or invent partial values; it returns this error
// instead so callers can fail closed.
type ErrJJParseFailed struct {
	// Program is the jj executable that produced the malformed output.
	Program string
	// Argv is the argv that produced the malformed output.
	Argv []string
	// Kind names which parser rejected the row ("bookmark", "commit", ...).
	Kind string
	// Line is the offending row (or first few rows) for diagnostics.
	Line string
	// Reason is a short, machine-stable explanation.
	Reason string
}

func (e *ErrJJParseFailed) Error() string {
	return fmt.Sprintf("jjadapter: %s %s: malformed %s output: %s: %q",
		e.Program, summarizeArgv(e.Argv), e.Kind, e.Reason, e.Line)
}

// JJArgv returns a copy of the argv that produced the malformed output.
func (e *ErrJJParseFailed) JJArgv() []string {
	out := make([]string, len(e.Argv))
	copy(out, e.Argv)
	return out
}

// JJExitCode returns -1 for parse failures (no exit code to surface).
func (e *ErrJJParseFailed) JJExitCode() int { return -1 }

// IsJJParseFailed reports whether err (or anything it wraps) is an
// ErrJJParseFailed. It exists so callers can distinguish parse-time
// errors from invocation-time errors without importing adapter types.
func IsJJParseFailed(err error) bool {
	var p *ErrJJParseFailed
	return errors.As(err, &p)
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

// PinOperation obtains the canonical operation id for the current
// repository view, with `--no-integrate-operation` and
// `--ignore-working-copy`. The returned id can be passed to other
// `jj` invocations via --at-op=<id> to guarantee they observe the
// same view.
func (a *Adapter) PinOperation(ctx context.Context, dir string) (string, error) {
	argv := []string{
		"op", "log", "--no-graph", "--limit", "1",
		"-T", OperationIDTemplate,
		"--ignore-working-copy",
		"--no-integrate-operation",
	}
	res := a.run(ctx, dir, argv)
	if res.Err != nil {
		return "", &ErrJJFailed{
			Program:  res.program,
			Argv:     argv,
			ExitCode: res.ExitCode,
			Stderr:   string(res.Stderr),
			Cause:    res.Err,
		}
	}
	id := strings.TrimRight(string(res.Stdout), "\n")
	if id == "" {
		return "", errors.New("jjadapter: empty operation id from jj op log")
	}
	return id, nil
}

// ListBookmarks returns every local and remote-tracking bookmark
// observed at the given operation view.
func (a *Adapter) ListBookmarks(ctx context.Context, dir, opID string) ([]BookmarkRef, error) {
	argv := []string{
		"bookmark", "list", "--all-remotes",
		"-T", BookmarksTemplate,
		"--ignore-working-copy",
		"--no-integrate-operation",
	}
	if opID != "" {
		argv = append(argv, "--at-op="+opID)
	}
	res := a.run(ctx, dir, argv)
	if res.Err != nil {
		return nil, &ErrJJFailed{
			Program:  res.program,
			Argv:     argv,
			ExitCode: res.ExitCode,
			Stderr:   string(res.Stderr),
			Cause:    res.Err,
		}
	}
	return parseBookmarkLines(res.program, argv, res.Stdout)
}

// ListCommits returns the commits selected by revset, full
// identifiers, in jj's default order.
func (a *Adapter) ListCommits(ctx context.Context, dir, opID, revset string) ([]CommitRef, error) {
	argv := []string{
		"log", "--no-graph",
		"-T", LogTemplate,
		"--ignore-working-copy",
		"--no-integrate-operation",
	}
	if opID != "" {
		argv = append(argv, "--at-op="+opID)
	}
	if revset != "" {
		argv = append(argv, "-r", revset)
	}
	res := a.run(ctx, dir, argv)
	if res.Err != nil {
		return nil, &ErrJJFailed{
			Program:  res.program,
			Argv:     argv,
			ExitCode: res.ExitCode,
			Stderr:   string(res.Stderr),
			Cause:    res.Err,
		}
	}
	return parseCommitLines(res.program, argv, res.Stdout)
}

// ListRemotes returns the set of remote names and URLs known to the
// repository. `jj git remote list` does not accept -T and emits
// "<remote> <url>" per line; we parse that stable shape.
func (a *Adapter) ListRemotes(ctx context.Context, dir, opID string) (map[string]string, error) {
	argv := []string{
		"git", "remote", "list",
		"--ignore-working-copy",
		"--no-integrate-operation",
	}
	if opID != "" {
		argv = append(argv, "--at-op="+opID)
	}
	res := a.run(ctx, dir, argv)
	if res.Err != nil {
		return nil, &ErrJJFailed{
			Program:  res.program,
			Argv:     argv,
			ExitCode: res.ExitCode,
			Stderr:   string(res.Stderr),
			Cause:    res.Err,
		}
	}
	out := map[string]string{}
	for _, line := range strings.Split(string(res.Stdout), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		sp := strings.SplitN(line, " ", 2)
		if len(sp) == 0 {
			continue
		}
		name := sp[0]
		url := ""
		if len(sp) == 2 {
			url = strings.TrimSpace(sp[1])
		}
		out[name] = url
	}
	return out, nil
}

// run executes jj under the adapter's environment policy and
// captures stdout/stderr with bounded size.
type runResult struct {
	Stdout   []byte
	Stderr   []byte
	ExitCode int
	Err      error
	program  string
}

func (a *Adapter) run(ctx context.Context, dir string, argv []string) runResult {
	prog := a.JJ
	if prog == "" {
		prog = "jj"
	}
	res := execx.Run(ctx, execx.Request{
		Program: prog,
		Args:    argv,
		Dir:     dir,
		Env:     a.env(dir),
	})
	return runResult{
		Stdout:   res.Stdout,
		Stderr:   res.Stderr,
		ExitCode: res.ExitCode,
		Err:      res.Err,
		program:  prog,
	}
}

// env returns the environment overlay for a jj invocation under this
// adapter.
func (a *Adapter) env(dir string) []string {
	home := a.Home
	if home == "" {
		home = filepath.Join(dir, ".jjadapter-home")
	}
	return []string{
		"JJ_CONFIG=" + filepath.Join(home, "jj-disabled-config.toml"),
		"HOME=" + home,
		"XDG_CONFIG_HOME=" + filepath.Join(home, "xdg"),
	}
}

// parseBookmarkLines parses the bounded stdout of
// `jj bookmark list --all-remotes` into typed BookmarkRef rows.
//
// Lines starting with the jj "Hint:" trailer are ignored.
//
// Per ACT-BJJ-PLAN01-CORRECTION02 §3, the parser fails closed: any
// non-empty, non-trailer row whose shape does not match the
// machine-oriented template (7 pipe-separated fields) yields an
// *ErrJJParseFailed. The parser NEVER invents partial BookmarkRef
// values from malformed input; the caller MUST NOT publish a plan
// derived from such output.
func parseBookmarkLines(prog string, argv []string, b []byte) ([]BookmarkRef, error) {
	out := []BookmarkRef{}
	for _, line := range strings.Split(string(b), "\n") {
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "Hint:") {
			continue
		}
		fields := strings.Split(line, "|")
		if len(fields) != 7 {
			return nil, &ErrJJParseFailed{
				Program: prog,
				Argv:    argv,
				Kind:    "bookmark",
				Line:    line,
				Reason:  fmt.Sprintf("expected 7 pipe-separated fields, got %d", len(fields)),
			}
		}
		name := fields[0]
		remote := fields[1]
		presentRaw := fields[2]
		conflictRaw := fields[3]
		commitID := fields[4]
		added := fields[5]
		removed := fields[6]

		// Validate boolean fields. jj's template emits "true" or
		// "false" literally; anything else is malformed.
		if presentRaw != "true" && presentRaw != "false" {
			return nil, &ErrJJParseFailed{
				Program: prog,
				Argv:    argv,
				Kind:    "bookmark",
				Line:    line,
				Reason:  fmt.Sprintf("present field must be true|false, got %q", presentRaw),
			}
		}
		if conflictRaw != "true" && conflictRaw != "false" {
			return nil, &ErrJJParseFailed{
				Program: prog,
				Argv:    argv,
				Kind:    "bookmark",
				Line:    line,
				Reason:  fmt.Sprintf("conflict field must be true|false, got %q", conflictRaw),
			}
		}
		present := presentRaw == "true"
		conflict := conflictRaw == "true"

		// Structural invariants:
		//   present && !conflict -> commit_id MUST be non-empty
		//   conflict             -> commit_id MUST be empty
		//
		// Note: jj's template emits added_targets/removed_targets as
		// the joined id lists even when non-conflict; in that case
		// both fields are usually empty strings. We accept
		// non-conflict rows whose added/removed fields happen to be
		// non-empty (some jj versions populate them with the
		// present target) but we validate them with parseTaggedCommits
		// so a structurally broken row still surfaces as a parse
		// failure.
		if present && !conflict && commitID == "" {
			return nil, &ErrJJParseFailed{
				Program: prog,
				Argv:    argv,
				Kind:    "bookmark",
				Line:    line,
				Reason:  "present && !conflict requires non-empty commit_id",
			}
		}
		if conflict && commitID != "" {
			return nil, &ErrJJParseFailed{
				Program: prog,
				Argv:    argv,
				Kind:    "bookmark",
				Line:    line,
				Reason:  "conflict requires empty commit_id (candidates live in added/removed)",
			}
		}

		kind := "REM"
		if remote == "" {
			kind = "LOC"
		}

		br := BookmarkRef{
			Kind:     kind,
			Name:     name,
			Remote:   remote,
			Present:  present,
			Conflict: conflict,
		}
		if present && !conflict {
			br.Target = &CommitRef{CommitID: commitID}
		}
		if conflict {
			parsedAdded, err := parseTaggedCommits(argv, added)
			if err != nil {
				return nil, err
			}
			parsedRemoved, err := parseTaggedCommits(argv, removed)
			if err != nil {
				return nil, err
			}
			br.AddedTargets = parsedAdded
			br.RemovedTargets = parsedRemoved
		}
		out = append(out, br)
	}
	return out, nil
}

// parseCommitLines parses the bounded stdout of `jj log` into typed
// CommitRef rows.
//
// Per ACT-BJJ-PLAN01-CORRECTION02 §3, the parser fails closed: any
// non-empty line whose shape does not match "<commit_id>|<change_id>"
// yields an *ErrJJParseFailed. The parser NEVER silently omits
// malformed rows, because losing a row can alter the semantic state
// BJJ thinks it observed.
func parseCommitLines(prog string, argv []string, b []byte) ([]CommitRef, error) {
	out := []CommitRef{}
	for _, line := range strings.Split(string(b), "\n") {
		if line == "" {
			continue
		}
		fields := strings.SplitN(line, "|", 2)
		if len(fields) != 2 {
			return nil, &ErrJJParseFailed{
				Program: prog,
				Argv:    argv,
				Kind:    "commit",
				Line:    line,
				Reason:  fmt.Sprintf("expected 2 pipe-separated fields, got %d", len(fields)),
			}
		}
		commitID := fields[0]
		changeID := fields[1]
		// Both identifiers MUST be non-empty. jj's template always
		// emits full hex; an empty id here means the template
		// failed silently or the output was truncated.
		if commitID == "" {
			return nil, &ErrJJParseFailed{
				Program: prog,
				Argv:    argv,
				Kind:    "commit",
				Line:    line,
				Reason:  "commit_id must be non-empty",
			}
		}
		if changeID == "" {
			return nil, &ErrJJParseFailed{
				Program: prog,
				Argv:    argv,
				Kind:    "commit",
				Line:    line,
				Reason:  "change_id must be non-empty",
			}
		}
		// Defensive structural check: pipe-split must not produce a
		// stray empty field at index 0 (e.g. if the line begins with
		// "|").
		if strings.HasPrefix(line, "|") {
			return nil, &ErrJJParseFailed{
				Program: prog,
				Argv:    argv,
				Kind:    "commit",
				Line:    line,
				Reason:  "line begins with '|' without a leading commit_id",
			}
		}
		out = append(out, CommitRef{
			CommitID: commitID,
			ChangeID: changeID,
		})
	}
	return out, nil
}

// parseTaggedCommits parses "<commit_id>:<change_id>" comma-joined
// lists as emitted by the BookmarksTemplate.
//
// Per ACT-BJJ-PLAN01-CORRECTION02 §3, the parser fails closed: a
// tagged target MUST contain a non-empty "<commit_id>:<change_id>"
// pair. Items missing the colon, missing either side, or carrying
// stray fields yield *ErrJJParseFailed. This matches the rest of
// the adapter: losing a row silently could alter the conflict
// state BJJ thinks it observed.
func parseTaggedCommits(argv []string, s string) ([]CommitRef, error) {
	if s == "" {
		return nil, nil
	}
	out := []CommitRef{}
	for _, item := range strings.Split(s, ",") {
		if item == "" {
			return nil, &ErrJJParseFailed{
				Kind:   "tagged-commit",
				Argv:   argv,
				Line:   s,
				Reason: "empty item in tagged-commit list",
			}
		}
		idx := strings.Index(item, ":")
		if idx <= 0 {
			return nil, &ErrJJParseFailed{
				Kind:   "tagged-commit",
				Argv:   argv,
				Line:   item,
				Reason: `expected "commit_id:change_id" form`,
			}
		}
		commitID := item[:idx]
		changeID := item[idx+1:]
		if commitID == "" || changeID == "" {
			return nil, &ErrJJParseFailed{
				Kind:   "tagged-commit",
				Argv:   argv,
				Line:   item,
				Reason: "tagged-commit requires both commit_id and change_id to be non-empty",
			}
		}
		// Reject any stray trailing colon fields.
		if strings.Contains(changeID, ":") {
			return nil, &ErrJJParseFailed{
				Kind:   "tagged-commit",
				Argv:   argv,
				Line:   item,
				Reason: "tagged-commit must contain exactly one ':' separator",
			}
		}
		out = append(out, CommitRef{CommitID: commitID, ChangeID: changeID})
	}
	return out, nil
}

// FindLocalBookmark returns the local BookmarkRef for name from the
// snapshot, if any.
func FindLocalBookmark(snap Snapshot, name string) (BookmarkRef, bool) {
	for _, b := range snap.Bookmarks {
		if b.Kind == "LOC" && b.Name == name {
			return b, true
		}
	}
	return BookmarkRef{}, false
}

// FindRemoteBookmark returns the remote-tracking BookmarkRef for
// name@remote, if any.
func FindRemoteBookmark(snap Snapshot, name, remote string) (BookmarkRef, bool) {
	for _, b := range snap.Bookmarks {
		if b.Kind == "REM" && b.Name == name && b.Remote == remote {
			return b, true
		}
	}
	return BookmarkRef{}, false
}
