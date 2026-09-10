package plan

import (
	"errors"
	"fmt"
)

// ErrorCode is the typed failure code surfaced by the resolver and
// carried by *Error. Callers MUST switch on Code, never on Error's
// string representation.
type ErrorCode string

const (
	// CodeRemoteNotFound: the requested remote name is unknown to
	// Jujutsu at the observed operation view.
	CodeRemoteNotFound ErrorCode = "REMOTE_NOT_FOUND"

	// CodeLocalBookmarkNotFound: no local bookmark with the given
	// name exists in the observed repository view. A deleted-but-
	// remembered local bookmark (Present==false) is reported here.
	CodeLocalBookmarkNotFound ErrorCode = "LOCAL_BOOKMARK_NOT_FOUND"

	// CodeLocalBookmarkConflicted: the local bookmark is in a
	// conflicted state with multiple competing targets.
	CodeLocalBookmarkConflicted ErrorCode = "LOCAL_BOOKMARK_CONFLICTED"

	// CodeRemoteBookmarkConflicted: the remote-tracking bookmark has
	// multiple competing targets recorded.
	CodeRemoteBookmarkConflicted ErrorCode = "REMOTE_BOOKMARK_CONFLICTED"

	// CodeNoRemoteChange is a positive typed signal that publication
	// would be a no-op.
	CodeNoRemoteChange ErrorCode = "NO_REMOTE_CHANGE"

	// CodeJJQueryFailed: an underlying `jj` invocation failed or
	// returned unexpected output.
	CodeJJQueryFailed ErrorCode = "JJ_QUERY_FAILED"

	// CodeInconsistentView: the resolver detected that the
	// repository view changed between observations within the same
	// plan.
	CodeInconsistentView ErrorCode = "INCONSISTENT_REPOSITORY_VIEW"
)

// Error is the typed plan-resolution failure. Callers MUST switch on
// Code via errors.As; never on the string message.
type Error struct {
	Code    ErrorCode
	Message string
	Cause   error
}

func (e *Error) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("plan: %s: %s: %v", e.Code, e.Message, e.Cause)
	}
	return fmt.Sprintf("plan: %s: %s", e.Code, e.Message)
}

func (e *Error) Unwrap() error { return e.Cause }

// NewError constructs an *Error with the given code, message, and
// optional cause.
func NewError(code ErrorCode, msg string, cause error) *Error {
	return &Error{Code: code, Message: msg, Cause: cause}
}

// AsError returns the *Error wrapped in err, if any.
func AsError(err error) (*Error, bool) {
	var pe *Error
	if errors.As(err, &pe) {
		return pe, true
	}
	return nil, false
}
