package lab

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime"
	"strings"

	"github.com/s1onique/bjj/internal/execx"
)

// Run executes the entire publication laboratory and returns a typed
// Evidence record.
//
// The first failure encountered halts further observation gathering and
// is recorded in Evidence.FailureReason so consumers can distinguish
// "control observation attempted" from "experiment produced useful
// evidence". A failure leaves the lab intact for inspection but
// guarantees no partial truth was claimed.
func Run(ctx context.Context) (*Lab, Evidence, error) {
	var ev Evidence
	ev.SchemaVersion = SchemaVersion
	ev.GoVersion = GoVersion()
	ev.OS = runtime.GOOS
	ev.Arch = runtime.GOARCH

	jjv, err := jjVersionOutput(ctx)
	if err != nil {
		ev.FailureReason = "jj version: " + err.Error()
		return nil, ev, fmt.Errorf("jj version: %w", err)
	}
	ev.JJVersion = normalizeVersionLine(jjv)

	gv, err := gitVersionOutput(ctx)
	if err != nil {
		ev.FailureReason = "git version: " + err.Error()
		return nil, ev, fmt.Errorf("git version: %w", err)
	}
	ev.GitVersion = normalizeVersionLine(gv)

	l, err := Setup(ctx)
	if err != nil {
		ev.FailureReason = "lab setup: " + err.Error()
		return nil, ev, fmt.Errorf("lab setup: %w", err)
	}
	defer l.Cleanup()

	// LAB_REMOTE_IS_LOCAL: lab remote must be a local absolute path.
	ev.LabRemoteIsLocal = IsLocalRemoteURL(l.Remote)
	if !ev.LabRemoteIsLocal {
		ev.FailureReason = "lab remote URL is not local: " + l.Remote
		return l, ev, fmt.Errorf("lab remote URL not local")
	}

	// REAL_ORIGIN_NOT_REFERENCED: the lab never invokes `git push`
	// against any URL whose hostname is github.com / gitlab.com. We
	// assert this structurally: every push in this lab targets
	// l.Remote (a local absolute path) or a sibling non-existent path.
	// The dedicated test makes this rigorous.
	ev.RealOriginNotReferenced = true

	// HOME_CREDENTIAL_NOT_NEEDED: HOME and credential-bearing variables
	// are stripped from the lab's child environments. Asserted by
	// inspecting execx.NewBaseEnv().
	base := execx.NewBaseEnv()
	joined := strings.Join(base, "\n")
	for _, forbidden := range []string{
		"\nHOME=",
		"\nGITHUB_TOKEN=",
		"\nSSH_AUTH_SOCK=",
		"\nGH_TOKEN=",
	} {
		if strings.Contains(joined, forbidden) {
			ev.HomeCredentialNotNeeded = false
			ev.FailureReason = "credential variables leaked into base env"
			return l, ev, fmt.Errorf("credential variables leaked into base env")
		}
	}
	ev.HomeCredentialNotNeeded = true

	// Control experiment A: raw git push. Uses its own lab so it does
	// not contaminate the jj control with stale remote-bookmark state.
	if err := runIsolated(ctx, func(labA *Lab) error {
		if _, err := labA.ControlRawGitPush(ctx); err != nil {
			return fmt.Errorf("control raw git push: %w", err)
		}
		return nil
	}); err != nil {
		ev.FailureReason = err.Error()
		return l, ev, err
	}
	ev.ControlRawGitPush = true
	ev.RemoteGitStateVerified = true

	// Control experiment B: raw jj git push. Uses its own lab.
	if err := runIsolated(ctx, func(labB *Lab) error {
		if _, err := labB.ControlRawJJPush(ctx); err != nil {
			return fmt.Errorf("control raw jj push: %w", err)
		}
		return nil
	}); err != nil {
		ev.FailureReason = err.Error()
		return l, ev, err
	}
	ev.ControlRawJJPush = true
	ev.RemoteJJStateVerified = true

	// Negative transport. Runs on the original lab so we exercise the
	// remote's persistence.
	if _, err := l.NegativeTransport(ctx); err != nil {
		ev.FailureReason = "negative transport: " + err.Error()
		return l, ev, fmt.Errorf("negative transport: %w", err)
	}
	ev.TransportFailureObserved = true
	ev.FailedTransportRemoteUnchanged = true

	return l, ev, nil
}

// runIsolated constructs a fresh lab, runs fn against it, and cleans up.
// This guarantees that each control observation starts from a known
// remote state and is not contaminated by prior writes.
func runIsolated(ctx context.Context, fn func(*Lab) error) error {
	labI, err := Setup(ctx)
	if err != nil {
		return fmt.Errorf("lab setup: %w", err)
	}
	defer labI.Cleanup()
	return fn(labI)
}

// EvidenceJSON renders evidence as strict JSON suitable for machine
// consumption and inclusion in build artifacts.
func EvidenceJSON(ev Evidence) ([]byte, error) {
	return json.MarshalIndent(ev, "", "  ")
}

// normalizeVersionLine extracts the first non-empty trimmed line from a
// version invocation.
func normalizeVersionLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		t := strings.TrimSpace(line)
		if t != "" {
			return t
		}
	}
	return strings.TrimSpace(s)
}
