// Package admission — PlanView bridge helpers.
//
// CORRECTION01 (P1): the concrete *plan.PublishPlan -> admission.PlanView
// adapter previously lived in this file and imported
// internal/plan. That violated the documented decoupling claim.
// The adapter has been moved to cmd/bjj/plan_adapter.go so the
// admission package does not import internal/plan at all.
//
// This file now ONLY holds:
//
//   - the planStatusNoRemoteChange constant (string form of the
//     plan-side StatusNoRemoteChange). It is duplicated here to
//     avoid leaking the plan package's unexported constants while
//     still letting admission recognise a no-op plan.
//   - small conversion helpers used by the gatherer.
//
// The PlanView interface itself is declared in types.go.
package admission

import (
	"errors"
	"fmt"

	"github.com/s1onique/bjj/internal/jjadapter"
)

// PlanStatusNoRemoteChange is the string form of
// plan.StatusNoRemoteChange. It is duplicated here to avoid
// importing the plan package's unexported constants; admission
// only needs to recognise this one status value.
//
// Callers that have a *plan.PublishPlan must convert its Status
// field via the same string cast; the value is part of PLAN01's
// canonical wire contract.
const PlanStatusNoRemoteChange = "no_remote_change"

// commitRefFromJJ converts a jjadapter CommitRef to admission.CommitRef.
func commitRefFromJJ(c jjadapter.CommitRef) CommitRef {
	return CommitRef{CommitID: c.CommitID, ChangeID: c.ChangeID}
}

// errJJToAdmission converts a jjadapter error into the
// corresponding admission infrastructure error. This keeps the
// fact gatherer's error surface uniform.
//
// Mapping:
//
//	ErrJJFailed      -> admission.CodeJJQueryFailed
//	ErrJJParseFailed -> admission.CodeJJParseFailed
//	default          -> admission.CodeJJQueryFailed (defensive)
func errJJToAdmission(op string, err error) error {
	if err == nil {
		return nil
	}
	var fjp *jjadapter.ErrJJParseFailed
	if errors.As(err, &fjp) {
		return NewError(CodeJJParseFailed,
			fmt.Sprintf("%s: malformed jj output: %s", op, fjp.Reason), err)
	}
	var fjf *jjadapter.ErrJJFailed
	if errors.As(err, &fjf) {
		return NewError(CodeJJQueryFailed,
			fmt.Sprintf("%s: jj invocation failed (exit %d)", op, fjf.ExitCode), err)
	}
	return NewError(CodeJJQueryFailed,
		fmt.Sprintf("%s: jj error", op), err)
}
