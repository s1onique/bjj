package check

import (
	"fmt"
	"sort"
	"strings"
)

// Profile is a named, ordered, deterministic collection of
// CheckSpecs that the Aggregator runs against the materialized
// workspace.
//
// For ACT-BJJ-CHECK01 the only profile is the compiled BJJ v1
// (see DefaultV1Profile). User-editable arbitrary command policy
// is explicitly out of scope per §9 — authoritative
// verification-policy provenance is not yet solved, and a
// per-repository file would force us to either (a) read it from
// the live filesystem (TOCTOU) or (b) bind it to the operation
// view (which is fine but still requires a policy schema
// admission ACT of its own).
type Profile struct {
	// Name is the stable identifier of the profile.
	Name string

	// Specs is the canonical ordered list of check specs the
	// runner executes. Specs are returned in declaration
	// order; the Aggregator re-sorts outcomes by CheckID, so
	// this order affects only which check runs first when they
	// share a parent process, not the canonical JSON.
	Specs []CheckSpec
}

// Lookup returns the named profile, or an error if the name is
// not recognised.
//
// CHECK01 only knows about "v1".
func Lookup(name string) (*Profile, error) {
	switch name {
	case "", "v1":
		return DefaultV1Profile(), nil
	default:
		return nil, fmt.Errorf("check: unknown profile %q (only \"v1\" is supported)", name)
	}
}

// DefaultV1Profile returns the compiled BJJ v1 check profile.
//
// Per ACT-BJJ-CHECK01-CORRECTION01 §1 (canonical program
// identity) the profile embeds LOGICAL program names
// (`"go"`, `"gofmt"`) only. The Runner resolves the actual
// executable path via PATH lookup at run time and records
// that absolute path in `CheckObservation.Diagnostics[].ResolvedProgram`,
// not in the canonical `CheckResult`.
//
// This keeps canonical `CheckResult` JSON host-independent:
// the same frozen subject + the same profile produce the
// same canonical bytes on every host, regardless of where
// `go` happens to live on disk.
//
// Per ACT-BJJ-CHECK01 §8 the profile contains only tree-local
// checks whose input semantics are actually bound correctly to
// the materialized workspace:
//
//	gofmt   -l .
//	go vet  ./...
//	go test -count=1 ./...
//	go build ./...
//
// PATCH_HYGIENE is intentionally NOT included: `git diff --check`
// requires Git history the materializer does not provide. A
// future ACT may add a subject-delta check that materializes
// both OLD and NEW trees and diffs them; CHECK01 does not fake
// it.
//
// Specs are returned in canonical (CheckID-ascending) order so
// that two callers that ask for the default profile see the
// same order regardless of any internal helper reorderings.
func DefaultV1Profile() *Profile {
	specs := []CheckSpec{
		{
			ID:      CheckIDGoBuild,
			Program: "go",
			Argv:    []string{"build", "./..."},
			// Compile-time default is DefaultGoBuildTimeoutMillis;
			// leaving TimeoutMillis zero triggers default in Runner.
		},
		{
			ID:      CheckIDGofmt,
			Program: "gofmt",
			Argv:    []string{"-l", "."},
		},
		{
			ID:      CheckIDGoTest,
			Program: "go",
			Argv:    []string{"test", "-count=1", "./..."},
		},
		{
			ID:      CheckIDGoVet,
			Program: "go",
			Argv:    []string{"vet", "./..."},
		},
	}
	sort.SliceStable(specs, func(i, j int) bool { return specs[i].ID < specs[j].ID })
	return &Profile{Name: "v1", Specs: specs}
}

// RunProfile is REMOVED in CHECK01-CORRECTION01. The
// orchestrator now drives the per-check loop directly so each
// check runs in its own fresh workspace (see Orchestrator.Run).
// The function is kept out of the public surface deliberately
// to prevent callers from re-introducing shared-workspace
// checks by mistake.

// SpecSummary returns a human-readable one-line summary of a
// CheckSpec. Used by the CLI text renderer; not part of the
// canonical JSON contract.
func SpecSummary(s CheckSpec) string {
	var b strings.Builder
	b.WriteString(string(s.ID))
	b.WriteString(" (")
	b.WriteString(s.Program)
	for _, a := range s.Argv {
		b.WriteByte(' ')
		b.WriteString(a)
	}
	b.WriteByte(')')
	return b.String()
}
