package lab

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/s1onique/bjj/internal/execx"
)

// TransportFailure reports the outcome of a deliberately failing
// transport attempt.
type TransportFailure struct {
	// Attempted is true when the subprocess was actually invoked.
	Attempted bool
	// ExitCode is the captured exit status, or -1 if the program could
	// not even be started.
	ExitCode int
	// Stderr contains the bounded stderr from the attempt.
	Stderr string
	// Err is the underlying error from execx (non-nil on transport
	// failure).
	Err error
}

// NegativeTransport exercises the lab's ability to distinguish
// "command attempted" from "publication actually happened" by attempting
// a push to a non-existent local remote from the raw Git client.
//
// It verifies:
//
//   - the subprocess was attempted (TRANSPORT_FAILURE_OBSERVED);
//   - the actual remote `refs/heads/feature` is unchanged
//     (FAILED_TRANSPORT_REMOTE_UNCHANGED).
func (l *Lab) NegativeTransport(ctx context.Context) (TransportFailure, error) {
	if l == nil || l.GitClient == "" {
		return TransportFailure{}, errors.New("lab: NegativeTransport: lab not set up")
	}

	// Record pre-state.
	before, err := l.gitOutput(ctx, l.Remote, []string{"rev-parse", "refs/heads/feature"})
	if err != nil {
		// refs/heads/feature may legitimately not exist yet (no
		// control push run). Treat empty as "absent".
		before = ""
	}

	nonexistent := l.Root + "/this-remote-does-not-exist.git"

	// Invoke the push directly via execx so we can capture exit/stderr
	// with the same env policy the rest of the lab uses.
	res := execx.Run(ctx, execx.Request{
		Program: "git",
		Args:    []string{"push", nonexistent, "main"},
		Dir:     l.GitClient,
		Env:     gitEnv(l.GitClient, l.labHome()),
	})

	failure := TransportFailure{
		Attempted: true,
		ExitCode:  res.ExitCode,
		Stderr:    strings.TrimSpace(string(res.Stderr)),
		Err:       res.Err,
	}
	if res.Err == nil {
		// Unexpected: push to nonexistent remote succeeded.
		failure.Err = fmt.Errorf("lab: negative transport unexpectedly succeeded")
		return failure, failure.Err
	}

	// Verify post-state matches pre-state.
	after, _ := l.gitOutput(ctx, l.Remote, []string{"rev-parse", "refs/heads/feature"})
	if after != before {
		return failure, fmt.Errorf("lab: remote feature changed despite failed transport: before=%q after=%q", before, after)
	}
	return failure, nil
}
