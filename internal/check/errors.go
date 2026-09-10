package check

import (
	"errors"
	"fmt"
)

// ErrorCode is the typed failure code surfaced by the check
// package for hard infrastructure failures.
//
// Per ACT-BJJ-CHECK01 §25, infrastructure failures MUST be
// distinguished from check failures:
//
//	check failure         -> CheckOutcome.Status == "fail"
//	check could not run   -> *Error with one of the codes below
//
// A normal failed test is NOT an infrastructure error; it produces
// a CheckOutcome with Status == fail, not an *Error.
type ErrorCode string

const (
	// CodeSubjectMismatch: the materializer target (commit id)
	// does not match the admitted subject's NewCommitID.
	// Hard failure; the result MUST NOT be reported as the
	// frozen subject's check.
	CodeSubjectMismatch ErrorCode = "CHECK_SUBJECT_MISMATCH"

	// CodeMaterializationFailed: the materializer could not
	// produce the disposable workspace (jj invocation failure,
	// filesystem error, etc.).
	CodeMaterializationFailed ErrorCode = "CHECK_MATERIALIZATION_FAILED"

	// CodeMaterializationMismatch: a post-materialization
	// re-listing of the workspace does not match the expected
	// manifest (in this ACT, the file list emitted at materialization
	// time). Detected at pre-exec integrity verification.
	CodeMaterializationMismatch ErrorCode = "CHECK_MATERIALIZATION_MISMATCH"

	// CodeWorkspaceChanged: between materialization and the
	// start of check execution, the workspace contents changed.
	// Hard failure; refusing to run against tampered material.
	CodeWorkspaceChanged ErrorCode = "CHECK_WORKSPACE_CHANGED"

	// CodeExecFailed: a check process could not be started
	// (executable missing) or terminated abnormally.
	CodeExecFailed ErrorCode = "CHECK_EXEC_FAILED"

	// CodeTimeout: a check exceeded its configured timeout.
	CodeTimeout ErrorCode = "CHECK_TIMEOUT"

	// CodeAdmissionNotAdmitted: the upstream admission decision
	// was not "admit" (deny or not_needed). The check path is
	// not entered.
	CodeAdmissionNotAdmitted ErrorCode = "ADMISSION_NOT_ADMITTED"

	// CodeUnsupportedTreeEntry: the materializer encountered a
	// tree entry whose kind is not supported by CHECK01
	// (symlink, git-submodule, conflict, tree). The v1
	// profile refuses to silently coerce these to regular
	// files because doing so falsifies the durable claim
	// "materializes the exact frozen candidate tree".
	CodeUnsupportedTreeEntry ErrorCode = "CHECK_UNSUPPORTED_TREE_ENTRY"
)

// Error is the typed check-infrastructure failure.
//
// Callers MUST switch on Code via errors.As; never on the string
// message.
type Error struct {
	Code    ErrorCode
	Message string
	Cause   error
}

func (e *Error) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("check: %s: %s: %v", e.Code, e.Message, e.Cause)
	}
	return fmt.Sprintf("check: %s: %s", e.Code, e.Message)
}

func (e *Error) Unwrap() error { return e.Cause }

// NewError constructs an *Error with the given code, message, and
// optional cause.
func NewError(code ErrorCode, msg string, cause error) *Error {
	return &Error{Code: code, Message: msg, Cause: cause}
}

// AsError returns the *Error wrapped in err, if any.
func AsError(err error) (*Error, bool) {
	var ce *Error
	if errors.As(err, &ce) {
		return ce, true
	}
	return nil, false
}
