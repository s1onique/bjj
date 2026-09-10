package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/s1onique/bjj/internal/admission"
	"github.com/s1onique/bjj/internal/jjadapter"
	"github.com/s1onique/bjj/internal/plan"
)

// Exit codes for `bjj admit`.
//
// Per ACT-BJJ-ADMISSION01 §39:
//
//	0 = admitted
//	1 = invalid CLI arguments
//	2 = infrastructure / internal failure
//	3 = valid decision but denied or not_needed
const (
	exitAdmitOK        = 0
	exitAdmitDenied    = 3
	exitAdmitNotNeeded = 3
)

// runAdmit implements `bjj admit --remote <R> --bookmark <B> [--json]`.
//
// Layered behaviour:
//
//  1. Resolve the canonical plan via plan.ResolveObserved (which
//     pins the operation view).
//  2. Load the BJJ admission policy from the repository
//     (.bjj/policy.toml, falling back to DefaultPolicy when
//     absent).
//  3. Verify the plan's SourceOperationID is non-empty (already
//     enforced by the plan resolver, but re-checked here so admit
//     cannot accidentally evaluate at a fresh opID).
//  4. Gather admission facts at the pinned opID.
//  5. Evaluate the decision with the frozen policy.
//  6. Render the result.
//
// `bjj admit` performs NO push, NO fetch, NO transport.
func runAdmit(argv []string, stdout, stderr io.Writer) int {
	remote, bookmark, jsonOut, err := parseAdmitArgs(argv)
	if err != nil {
		fmt.Fprintf(stderr, "bjj admit: %v\n", err)
		fmt.Fprintln(stderr, "")
		fmt.Fprintln(stderr, "usage: bjj admit --remote <remote> --bookmark <bookmark> [--json]")
		return exitInvalidArgs
	}

	dir, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(stderr, "bjj admit: cannot get working directory: %v\n", err)
		return exitInternalError
	}

	ctx := context.Background()

	// 1. Resolve the canonical plan via PLAN01.
	planSrc := plan.NewJJSource(jjadapter.New())
	obs, err := plan.ResolveObserved(ctx, planSrc, plan.ResolveOptions{
		Dir:      dir,
		Remote:   remote,
		Bookmark: bookmark,
	})
	if err != nil {
		return renderPlanErrorAsAdmit(err, jsonOut, stdout, stderr)
	}
	if obs == nil || obs.Plan == nil {
		fmt.Fprintf(stderr, "bjj admit: plan resolver returned empty observation\n")
		return exitInternalError
	}
	if obs.SourceOperationID == "" {
		fmt.Fprintf(stderr, "bjj admit: plan observation has empty SourceOperationID\n")
		return exitInternalError
	}

	// 2. Wrap the plan as a PlanView FIRST so the policy load is
	//    bound to the publication NEW (CORRECTION03 §4). The
	//    concrete *plan.PublishPlan -> admission.PlanView bridge
	//    lives in plan_adapter.go (CORRECTION01 P1: admission no
	//    longer imports internal/plan).
	pv, werr := wrapPlan(obs.Plan)
	if werr != nil {
		return renderAdmissionError(werr, jsonOut, stdout, stderr)
	}

	// 3. Load the admission policy from the FROZEN CANDIDATE.
	//
	// CORRECTION03 §1 closed the live-filesystem policy read:
	// .bjj/policy.toml is now read via `jj --at-op=<OP_A> file
	// show -r <NEW>`, so a working-copy mutation between plan
	// resolution and policy load CANNOT influence admission.
	//
	// We share one Adapter between the policy reader and the
	// fact-gatherer source so both observe the same opID-bound
	// env overlay (per-repository JJ_CONFIG override).
	jj := jjadapter.New()
	policyReader := &jjPolicyReader{a: jj}
	policy, _, perr := admission.LoadPolicyAt(ctx, policyReader, pv, dir, obs.SourceOperationID)
	if perr != nil {
		return renderAdmissionError(perr, jsonOut, stdout, stderr)
	}

	// 4. Gather admission facts at the pinned opID.
	src := &adapterSource{a: jj}
	g := admission.NewGatherer(src)
	facts, gerr := g.Gather(ctx, pv, policy, admission.GatherOptions{
		Dir:  dir,
		OpID: obs.SourceOperationID,
	})
	if gerr != nil {
		return renderAdmissionError(gerr, jsonOut, stdout, stderr)
	}

	// 5. Evaluate.
	result, eerr := admission.Evaluate(admission.AdmissionInput{
		Plan:   pv,
		Facts:  facts,
		Policy: policy,
	})
	if eerr != nil {
		return renderAdmissionError(eerr, jsonOut, stdout, stderr)
	}

	// 6. Render.
	if jsonOut {
		body, rerr := admission.RenderJSON(result)
		if rerr != nil {
			fmt.Fprintf(stderr, "bjj admit: render: %v\n", rerr)
			return exitInternalError
		}
		stdout.Write(body)
		if len(body) > 0 && body[len(body)-1] != '\n' {
			stdout.Write([]byte("\n"))
		}
	} else {
		if err := admission.RenderText(result, stdout); err != nil {
			fmt.Fprintf(stderr, "bjj admit: render: %v\n", err)
			return exitInternalError
		}
	}

	switch result.Decision {
	case admission.DecisionAdmit:
		return exitAdmitOK
	case admission.DecisionDeny:
		return exitAdmitDenied
	case admission.DecisionNotNeeded:
		return exitAdmitNotNeeded
	default:
		fmt.Fprintf(stderr, "bjj admit: unknown decision %q\n", result.Decision)
		return exitInternalError
	}
}

// adapterSource wraps *jjadapter.Adapter to satisfy admission.Source.
type adapterSource struct {
	a *jjadapter.Adapter
}

func (s *adapterSource) ListCommits(ctx context.Context, dir, opID, revset string) ([]jjadapter.CommitRef, error) {
	return s.a.ListCommits(ctx, dir, opID, revset)
}

func (s *adapterSource) ListCommitObs(ctx context.Context, dir, opID, revset string) ([]jjadapter.CommitObs, error) {
	return s.a.ListCommitObs(ctx, dir, opID, revset)
}

// jjPolicyReader satisfies admission.PolicyReader on top of the
// real jj adapter. It translates the adapter's
// FilePresencePresent/FilePresenceAbsent dichotomy into the
// admission package's own PolicyFilePresence enum and surfaces
// every infrastructure failure as a non-nil error so the policy
// loader never silently falls back to DefaultPolicy on a broken
// repository.
//
// CORRECTION03 §3 mandates this exact seam: the production
// admission path uses `jj --at-op=<opID> file show -r <revision>
// <path>`, never os.ReadFile(<dir>/...).
type jjPolicyReader struct {
	a *jjadapter.Adapter
}

func (r *jjPolicyReader) ReadPolicyFile(ctx context.Context, dir, opID, revision, path string) ([]byte, admission.PolicyFilePresence, error) {
	body, presence, err := r.a.FileShowAtOp(ctx, dir, opID, revision, path)
	if err != nil {
		return nil, "", err
	}
	switch presence {
	case jjadapter.FilePresencePresent:
		return body, admission.PolicyFilePresencePresent, nil
	case jjadapter.FilePresenceAbsent:
		return nil, admission.PolicyFilePresenceAbsent, nil
	default:
		return nil, "", fmt.Errorf("admission: unknown jj file presence %q", presence)
	}
}

// renderPlanErrorAsAdmit maps PLAN01 typed errors to the admit
// CLI exit contract.
func renderPlanErrorAsAdmit(err error, jsonOut bool, stdout, stderr io.Writer) int {
	var pe *plan.Error
	if errors.As(err, &pe) {
		if jsonOut {
			b, jerr := plan.RenderErrorJSON(pe)
			if jerr != nil {
				fmt.Fprintf(stderr, "bjj admit: %v\n", jerr)
				return exitInternalError
			}
			stdout.Write(b)
			stdout.Write([]byte("\n"))
		} else {
			fmt.Fprintln(stderr, plan.RenderErrorText(pe))
		}
		switch pe.Code {
		case plan.CodeLocalBookmarkNotFound, plan.CodeRemoteNotFound, plan.CodeNoRemoteChange:
			return exitInvalidArgs
		default:
			return exitInternalError
		}
	}
	fmt.Fprintf(stderr, "bjj admit: %v\n", err)
	return exitInternalError
}

// renderAdmissionError maps admission typed errors to the admit
// CLI exit contract.
func renderAdmissionError(err error, jsonOut bool, stdout, stderr io.Writer) int {
	var ae *admission.Error
	if errors.As(err, &ae) {
		if jsonOut {
			b, jerr := admission.RenderErrorJSON(ae)
			if jerr != nil {
				fmt.Fprintf(stderr, "bjj admit: %v\n", jerr)
				return exitInternalError
			}
			stdout.Write(b)
			stdout.Write([]byte("\n"))
		} else {
			fmt.Fprintln(stderr, admission.RenderErrorText(ae))
		}
		return exitInternalError
	}
	fmt.Fprintf(stderr, "bjj admit: %v\n", err)
	return exitInternalError
}

// parseAdmitArgs extracts --remote, --bookmark, and --json. The
// parsing rules are identical to parsePlanArgs but live in a
// dedicated helper so future admit-only flags can be added
// without touching plan.
func parseAdmitArgs(argv []string) (remote, bookmark string, jsonOut bool, err error) {
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
