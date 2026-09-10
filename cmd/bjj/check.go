package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/s1onique/bjj/internal/admission"
	"github.com/s1onique/bjj/internal/check"
	"github.com/s1onique/bjj/internal/jjadapter"
	"github.com/s1onique/bjj/internal/plan"
)

// runCheck implements `bjj check --remote <R> --bookmark <B> [--json]`.
//
// Layered behaviour:
//
//  1. Resolve the canonical plan via plan.ResolveObserved (which
//     pins the operation view).
//  2. Load the BJJ admission policy from the FROZEN candidate
//     (CORRECTION03).
//  3. Gather admission facts at the pinned opID.
//  4. Evaluate the decision with the frozen policy.
//  5. If decision != admit, render a typed non-check result
//     and exit ExitCheckAdmission (3). The check path is NEVER
//     entered for non-admitted subjects.
//  6. Otherwise, hand off to the check orchestrator, which
//     materializes the frozen NEW tree at opID, runs the v1
//     check profile, and aggregates the result.
//  7. Render the result.
//
// `bjj check` performs NO push, NO fetch, NO transport.
func runCheck(argv []string, stdout, stderr io.Writer) int {
	remote, bookmark, jsonOut, err := parseCheckArgs(argv)
	if err != nil {
		fmt.Fprintf(stderr, "bjj check: %v\n", err)
		fmt.Fprintln(stderr, "")
		fmt.Fprintln(stderr, "usage: bjj check --remote <remote> --bookmark <bookmark> [--json]")
		return check.ExitCheckInvalidArgs
	}

	dir, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(stderr, "bjj check: cannot get working directory: %v\n", err)
		return check.ExitCheckInfraError
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
		return renderPlanErrorAsCheck(err, jsonOut, stdout, stderr)
	}
	if obs == nil || obs.Plan == nil {
		fmt.Fprintf(stderr, "bjj check: plan resolver returned empty observation\n")
		return check.ExitCheckInfraError
	}
	if obs.SourceOperationID == "" {
		fmt.Fprintf(stderr, "bjj check: plan observation has empty SourceOperationID\n")
		return check.ExitCheckInfraError
	}

	// 2. Wrap plan -> PlanView; load policy.
	pv, werr := wrapPlan(obs.Plan)
	if werr != nil {
		return renderCheckAdmissionError(werr, jsonOut, stdout, stderr)
	}

	jj := jjadapter.New()
	policyReader := &jjPolicyReader{a: jj}
	policy, _, perr := admission.LoadPolicyAt(ctx, policyReader, pv, dir, obs.SourceOperationID)
	if perr != nil {
		return renderCheckAdmissionError(perr, jsonOut, stdout, stderr)
	}

	// 3. Gather admission facts.
	src := &adapterSource{a: jj}
	g := admission.NewGatherer(src)
	facts, gerr := g.Gather(ctx, pv, policy, admission.GatherOptions{
		Dir:  dir,
		OpID: obs.SourceOperationID,
	})
	if gerr != nil {
		return renderCheckAdmissionError(gerr, jsonOut, stdout, stderr)
	}

	// 4. Evaluate.
	result, eerr := admission.Evaluate(admission.AdmissionInput{
		Plan:   pv,
		Facts:  facts,
		Policy: policy,
	})
	if eerr != nil {
		return renderCheckAdmissionError(eerr, jsonOut, stdout, stderr)
	}

	// 5. Gate: only ADMIT enters the check path. The verdict
	//    is rendered to stdout (text or JSON) before exit.
	if result.Decision != admission.DecisionAdmit {
		switch result.Decision {
		case admission.DecisionDeny:
			renderCheckNonAdmit(result, jsonOut, stdout)
			return check.ExitCheckAdmission
		case admission.DecisionNotNeeded:
			renderCheckNonAdmit(result, jsonOut, stdout)
			return check.ExitCheckAdmission
		default:
			fmt.Fprintf(stderr, "bjj check: unknown admission decision %q\n", result.Decision)
			return check.ExitCheckInfraError
		}
	}

	// 6. Build the check subject from the admitted identity.
	subject := check.CheckSubject{
		SourceOperationID: obs.SourceOperationID,
		Remote:            result.Subject.Remote,
		Bookmark:          result.Subject.Bookmark,
		OldCommitID:       result.Subject.OldCommitID,
		NewCommitID:       result.Subject.NewCommitID,
	}

	// 7. Allocate a parent dir for the disposable workspace.
	parent, err := os.MkdirTemp("", "bjj-check-parent-")
	if err != nil {
		fmt.Fprintf(stderr, "bjj check: mktemp parent: %v\n", err)
		return check.ExitCheckInfraError
	}
	defer func() { _ = os.RemoveAll(parent) }()

	// 8. Materialize + run profile + aggregate.
	orch := &check.Orchestrator{
		Materializer: check.NewJJMaterializer(check.NewJJFileSource(jj)),
		Runner:       check.NewExecRunner(),
	}
	checkObs, cerr := orch.Run(ctx, subject, check.Options{
		RepoDir:         dir,
		WorkspaceParent: parent,
	})
	if cerr != nil {
		return renderCheckError(cerr, jsonOut, stdout, stderr)
	}
	checkResult := checkObs.Result

	// 9. Render + exit.
	if jsonOut {
		body, rerr := check.RenderObservationJSON(checkObs)
		if rerr != nil {
			fmt.Fprintf(stderr, "bjj check: render: %v\n", rerr)
			return check.ExitCheckInfraError
		}
		stdout.Write(body)
		if len(body) > 0 && body[len(body)-1] != '\n' {
			stdout.Write([]byte("\n"))
		}
	} else {
		if rerr := check.RenderObservationText(checkObs, stdout); rerr != nil {
			fmt.Fprintf(stderr, "bjj check: render: %v\n", rerr)
			return check.ExitCheckInfraError
		}
	}

	switch checkResult.Status {
	case check.StatusPass:
		return check.ExitCheckOK
	case check.StatusFail:
		return check.ExitCheckFailed
	case check.StatusError:
		return check.ExitCheckInfraError
	default:
		fmt.Fprintf(stderr, "bjj check: unknown aggregate status %q\n", checkResult.Status)
		return check.ExitCheckInfraError
	}
}

// renderCheckNonAdmit renders the admission decision (deny or
// not_needed) for callers that asked for "check" but never got
// past the admission gate. In JSON mode the admission result is
// echoed verbatim so callers can decode it; in text mode the
// admission text rendering is used.
func renderCheckNonAdmit(result admission.AdmissionResult, jsonOut bool, stdout io.Writer) {
	if jsonOut {
		body, err := admission.RenderJSON(result)
		if err != nil {
			fmt.Fprintf(stdout, `{"error":%q}`+"\n", err.Error())
			return
		}
		stdout.Write(body)
		if len(body) > 0 && body[len(body)-1] != '\n' {
			stdout.Write([]byte("\n"))
		}
		return
	}
	_ = admission.RenderText(result, stdout)
}

// renderCheckAdmissionError maps admission typed errors to the
// check CLI exit contract.
func renderCheckAdmissionError(err error, jsonOut bool, stdout, stderr io.Writer) int {
	var ae *admission.Error
	if errors.As(err, &ae) {
		if jsonOut {
			b, jerr := admission.RenderErrorJSON(ae)
			if jerr != nil {
				fmt.Fprintf(stderr, "bjj check: %v\n", jerr)
				return check.ExitCheckInfraError
			}
			stdout.Write(b)
			stdout.Write([]byte("\n"))
		} else {
			fmt.Fprintln(stderr, admission.RenderErrorText(ae))
		}
		return check.ExitCheckInfraError
	}
	fmt.Fprintf(stderr, "bjj check: %v\n", err)
	return check.ExitCheckInfraError
}

// renderPlanErrorAsCheck maps PLAN01 typed errors to the check
// CLI exit contract.
func renderPlanErrorAsCheck(err error, jsonOut bool, stdout, stderr io.Writer) int {
	var pe *plan.Error
	if errors.As(err, &pe) {
		if jsonOut {
			b, jerr := plan.RenderErrorJSON(pe)
			if jerr != nil {
				fmt.Fprintf(stderr, "bjj check: %v\n", jerr)
				return check.ExitCheckInfraError
			}
			stdout.Write(b)
			stdout.Write([]byte("\n"))
		} else {
			fmt.Fprintln(stderr, plan.RenderErrorText(pe))
		}
		switch pe.Code {
		case plan.CodeLocalBookmarkNotFound, plan.CodeRemoteNotFound, plan.CodeNoRemoteChange:
			return check.ExitCheckInvalidArgs
		default:
			return check.ExitCheckInfraError
		}
	}
	fmt.Fprintf(stderr, "bjj check: %v\n", err)
	return check.ExitCheckInfraError
}

// renderCheckError maps check typed errors to the check CLI exit
// contract.
func renderCheckError(err error, jsonOut bool, stdout, stderr io.Writer) int {
	var ce *check.Error
	if errors.As(err, &ce) {
		if jsonOut {
			body, rerr := check.RenderErrorJSON(ce)
			if rerr != nil {
				fmt.Fprintf(stderr, "bjj check: %v\n", rerr)
				return check.ExitCheckInfraError
			}
			stdout.Write(body)
			if len(body) > 0 && body[len(body)-1] != '\n' {
				stdout.Write([]byte("\n"))
			}
		} else {
			fmt.Fprintln(stderr, check.RenderErrorText(ce))
		}
		return check.ExitCheckInfraError
	}
	fmt.Fprintf(stderr, "bjj check: %v\n", err)
	return check.ExitCheckInfraError
}

// parseCheckArgs extracts --remote, --bookmark, and --json.
// Same shape as parsePlanArgs and parseAdmitArgs; CHECK01 does
// not accept --revision or --current (those would weaken
// subject binding and are explicitly rejected per §15).
func parseCheckArgs(argv []string) (remote, bookmark string, jsonOut bool, err error) {
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
