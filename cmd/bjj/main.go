// Command bjj is the Bounded Jujutsu CLI.
//
// During ACT-BJJ-CHECK01 the surface is:
//
//	bjj version [--json]
//	bjj plan    --remote <R> --bookmark <B> [--json]
//	bjj admit   --remote <R> --bookmark <B> [--json]
//	bjj check   --remote <R> --bookmark <B> [--json]
//
// All other planned commands (publish, status, receipt) are
// explicitly deferred to subsequent ACTs.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/s1onique/bjj/internal/version"
)

// Exit codes used by the CLI:
//
//	0  success
//	1  invalid invocation
//	2  internal error
const (
	exitOK            = 0
	exitInvalidArgs   = 1
	exitInternalError = 2
)

func main() {
	code := run(os.Args[1:], os.Stdout, os.Stderr)
	os.Exit(code)
}

// run is the testable entry point. It dispatches to the requested
// subcommand and returns a process exit code.
func run(argv []string, stdout, stderr io.Writer) int {
	if len(argv) == 0 {
		fmt.Fprintln(stderr, "usage: bjj <command> [args]")
		fmt.Fprintln(stderr, "")
		fmt.Fprintln(stderr, "commands:")
		fmt.Fprintln(stderr, "  version          print BJJ build identity (text)")
		fmt.Fprintln(stderr, "  version --json   print BJJ build identity (strict JSON)")
		fmt.Fprintln(stderr, "  plan             describe the publication subject for (remote, bookmark)")
		fmt.Fprintln(stderr, "  admit            determine whether a publication subject is admission-eligible")
		fmt.Fprintln(stderr, "  check            run the v1 check profile against an admitted subject")
		return exitInvalidArgs
	}
	switch argv[0] {
	case "version", "--version", "-v":
		return cmdVersion(argv[1:], stdout, stderr)
	case "plan":
		return runPlan(argv[1:], stdout, stderr)
	case "admit":
		return runAdmit(argv[1:], stdout, stderr)
	case "check":
		return runCheck(argv[1:], stdout, stderr)
	case "help", "--help", "-h":
		fmt.Fprintln(stdout, "usage: bjj <command> [args]")
		fmt.Fprintln(stdout, "")
		fmt.Fprintln(stdout, "commands:")
		fmt.Fprintln(stdout, "  version          print BJJ build identity (text)")
		fmt.Fprintln(stdout, "  version --json   print BJJ build identity (strict JSON)")
		fmt.Fprintln(stdout, "  plan    --remote <R> --bookmark <B> [--json]")
		fmt.Fprintln(stdout, "                  describe the publication subject for (R, B)")
		fmt.Fprintln(stdout, "  admit   --remote <R> --bookmark <B> [--json]")
		fmt.Fprintln(stdout, "                  determine whether the publication subject is admission-eligible")
		fmt.Fprintln(stdout, "  check   --remote <R> --bookmark <B> [--json]")
		fmt.Fprintln(stdout, "                  run the v1 check profile against an admitted subject")
		return exitOK
	default:
		fmt.Fprintf(stderr, "bjj: unknown command %q\n", argv[0])
		fmt.Fprintln(stderr, "run `bjj help` for usage")
		return exitInvalidArgs
	}
}

// cmdVersion prints build identity. With --json it emits strict JSON;
// otherwise it emits the canonical human-readable form.
func cmdVersion(argv []string, stdout, stderr io.Writer) int {
	for _, a := range argv {
		switch a {
		case "--json":
			return writeVersionJSON(stdout)
		case "-h", "--help":
			fmt.Fprintln(stdout, "usage: bjj version [--json]")
			return exitOK
		default:
			fmt.Fprintf(stderr, "bjj version: unknown argument %q\n", a)
			return exitInvalidArgs
		}
	}
	writeVersionText(stdout)
	return exitOK
}

func writeVersionText(w io.Writer) {
	fmt.Fprintln(w, versionBanner(version.Current()))
}

func writeVersionJSON(w io.Writer) int {
	info := version.Current()
	// Use encoding/json for strict-JSON emission. The resulting output
	// is intentionally minimal and shape-stable.
	b, err := json.Marshal(info)
	if err != nil {
		fmt.Fprintf(w, `{"error":%q}`+"\n", err.Error())
		return exitInternalError
	}
	// Always end with a newline for human-friendliness while remaining
	// valid JSON.
	if _, err := w.Write(b); err != nil {
		return exitInternalError
	}
	if _, err := io.WriteString(w, "\n"); err != nil {
		return exitInternalError
	}
	return exitOK
}

// versionBanner renders the canonical text form used by `bjj version`.
//
// Format is intentionally simple and stable:
//
//	bjj <version>
//	commit:    <commit>
//	build time: <build_time>
func versionBanner(info version.Info) string {
	var b strings.Builder
	b.WriteString("bjj ")
	b.WriteString(info.Version)
	b.WriteByte('\n')
	b.WriteString("commit:     ")
	b.WriteString(info.Commit)
	b.WriteByte('\n')
	b.WriteString("build time: ")
	b.WriteString(info.BuildTime)
	b.WriteByte('\n')
	return b.String()
}

// itoa is a tiny strconv-free helper for the few places the CLI needs
// to print small integers; kept here so cmd/bjj stays free of new
// imports.
func itoa(n int) string { return strconv.Itoa(n) }
