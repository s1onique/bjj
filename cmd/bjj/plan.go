package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/s1onique/bjj/internal/jjadapter"
	"github.com/s1onique/bjj/internal/plan"
)

// runPlan implements `bjj plan --remote <R> --bookmark <B> [--json]`.
//
// The command is strictly read-only. It MUST NOT push, fetch, track,
// move, create, or describe any state.
func runPlan(argv []string, stdout, stderr io.Writer) int {
	remote, bookmark, jsonOut, err := parsePlanArgs(argv)
	if err != nil {
		fmt.Fprintf(stderr, "bjj plan: %v\n", err)
		fmt.Fprintln(stderr, "")
		fmt.Fprintln(stderr, "usage: bjj plan --remote <remote> --bookmark <bookmark> [--json]")
		return exitInvalidArgs
	}

	dir, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(stderr, "bjj plan: cannot get working directory: %v\n", err)
		return exitInternalError
	}

	src := plan.NewJJSource(jjadapter.New())
	obs, err := plan.ResolveObserved(context.Background(), src, plan.ResolveOptions{
		Dir:      dir,
		Remote:   remote,
		Bookmark: bookmark,
	})
	if err != nil {
		var pe *plan.Error
		if errors.As(err, &pe) {
			if jsonOut {
				b, jerr := plan.RenderErrorJSON(pe)
				if jerr != nil {
					fmt.Fprintf(stderr, "bjj plan: %v\n", jerr)
					return exitInternalError
				}
				stdout.Write(b)
				stdout.Write([]byte("\n"))
			} else {
				fmt.Fprintln(stderr, plan.RenderErrorText(pe))
			}
			// Map typed errors to the CLI exit contract.
			switch pe.Code {
			case plan.CodeLocalBookmarkNotFound, plan.CodeRemoteNotFound:
				return exitInvalidArgs
			default:
				return exitInternalError
			}
		}
		fmt.Fprintf(stderr, "bjj plan: %v\n", err)
		return exitInternalError
	}

	// The CLI emits ONLY the canonical PublishPlan in both
	// text and JSON output modes. The PlanObservation envelope
	// (opID, repository path) is diagnostic-only and is
	// available programmatically via plan.ResolveObserved, but
	// it does not appear in `bjj plan` output. This matches
	// ACT-BJJ-PLAN01-CORRECTION02 §4 (CLI_COMMENT_MATCHES_OUTPUT):
	// the canonical subject must be the only thing emitted.
	if jsonOut {
		body, err := plan.RenderJSON(obs.Plan)
		if err != nil {
			fmt.Fprintf(stderr, "bjj plan: render: %v\n", err)
			return exitInternalError
		}
		stdout.Write(body)
		if len(body) > 0 && body[len(body)-1] != '\n' {
			stdout.Write([]byte("\n"))
		}
		return exitOK
	}

	if err := plan.RenderText(obs.Plan, stdout); err != nil {
		fmt.Fprintf(stderr, "bjj plan: render: %v\n", err)
		return exitInternalError
	}
	return exitOK
}

// parsePlanArgs extracts --remote, --bookmark, and --json from the
// argv. Fails closed on missing, duplicated, or unknown flags.
//
// Supported forms:
//
//	--remote R --bookmark B
//	--remote=R --bookmark=B
//	--json
//
// PLAN01 does not accept positional arguments or repeated flags.
func parsePlanArgs(argv []string) (remote, bookmark string, jsonOut bool, err error) {
	consumeValue := func(flag string, idx int) (string, int, error) {
		if idx+1 >= len(argv) {
			return "", idx, fmt.Errorf("%s requires a value", flag)
		}
		return argv[idx+1], idx + 1, nil
	}

	remoteSeen := false
	bookmarkSeen := false

	for i := 0; i < len(argv); i++ {
		a := argv[i]
		switch {
		case a == "--json":
			if jsonOut {
				return "", "", false, errors.New("--json may not be repeated")
			}
			jsonOut = true
		case a == "-h" || a == "--help":
			return "", "", false, errors.New("help requested")
		case a == "--remote":
			if remoteSeen {
				return "", "", false, errors.New("--remote may not be repeated")
			}
			remoteSeen = true
			val, next, err := consumeValue("--remote", i)
			if err != nil {
				return "", "", false, err
			}
			remote = val
			i = next
		case len(a) > 9 && a[:9] == "--remote=":
			if remoteSeen {
				return "", "", false, errors.New("--remote may not be repeated")
			}
			remoteSeen = true
			remote = a[9:]
		case a == "--bookmark":
			if bookmarkSeen {
				return "", "", false, errors.New("--bookmark may not be repeated")
			}
			bookmarkSeen = true
			val, next, err := consumeValue("--bookmark", i)
			if err != nil {
				return "", "", false, err
			}
			bookmark = val
			i = next
		case len(a) > 11 && a[:11] == "--bookmark=":
			if bookmarkSeen {
				return "", "", false, errors.New("--bookmark may not be repeated")
			}
			bookmarkSeen = true
			bookmark = a[11:]
		default:
			return "", "", false, fmt.Errorf("unknown argument %q", a)
		}
	}
	if !remoteSeen {
		return "", "", false, errors.New("--remote is required")
	}
	if remote == "" {
		return "", "", false, errors.New("--remote may not be empty")
	}
	if !bookmarkSeen {
		return "", "", false, errors.New("--bookmark is required")
	}
	if bookmark == "" {
		return "", "", false, errors.New("--bookmark may not be empty")
	}
	return remote, bookmark, jsonOut, nil
}
