package admission

import (
	"errors"
	"fmt"
)

// ErrorCode is the typed failure code surfaced by the admission
// package for hard infrastructure failures (NOT for policy
// denials; those are reported via AdmissionResult.Decision).
//
// Per §16 and §22, infrastructure failures MUST be distinguished
// from policy denials:
//
//	policy denial         -> AdmissionResult.Decision == "deny"
//	no-op                 -> AdmissionResult.Decision == "not_needed"
//	jj query failed       -> *Error with CodeJJQueryFailed
//	malformed jj output   -> *Error with CodeJJParseFailed
//	policy absent         -> use DefaultPolicy (NOT an error)
//	policy malformed      -> *Error with CodePolicyInvalid
//	operation id mismatch -> *Error with CodeInconsistentView
type ErrorCode string

const (
	// CodeJJQueryFailed: an underlying jj invocation failed.
	CodeJJQueryFailed ErrorCode = "JJ_QUERY_FAILED"

	// CodeJJParseFailed: jj produced structurally malformed output.
	CodeJJParseFailed ErrorCode = "JJ_PARSE_FAILED"

	// CodePolicyInvalid: repository BJJ policy exists but is
	// malformed; admission refuses to fall back to defaults.
	CodePolicyInvalid ErrorCode = "POLICY_INVALID"

	// CodeInconsistentView: facts' SourceOperationID does not
	// match the plan observation's SourceOperationID. This is
	// raised BEFORE evaluation as a hard infrastructure error
	// (the entire admission run is unsafe).
	CodeInconsistentView ErrorCode = "INCONSISTENT_REPOSITORY_VIEW"

	// CodeNoBookmarkMove: the canonical plan has zero bookmark
	// moves. PLAN01 always produces one move; this is a defensive
	// guard for callers that bypass the resolver.
	CodeNoBookmarkMove ErrorCode = "NO_BOOKMARK_MOVE"
)

// Error is the typed admission-infrastructure failure.
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
		return fmt.Sprintf("admission: %s: %s: %v", e.Code, e.Message, e.Cause)
	}
	return fmt.Sprintf("admission: %s: %s", e.Code, e.Message)
}

func (e *Error) Unwrap() error { return e.Cause }

// NewError constructs an *Error with the given code, message, and
// optional cause.
func NewError(code ErrorCode, msg string, cause error) *Error {
	return &Error{Code: code, Message: msg, Cause: cause}
}

// AsError returns the *Error wrapped in err, if any.
func AsError(err error) (*Error, bool) {
	var ae *Error
	if errors.As(err, &ae) {
		return ae, true
	}
	return nil, false
}
