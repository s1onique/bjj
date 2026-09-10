package plan

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// RenderJSON emits the strict, deterministic JSON representation of
// the plan.
//
// The encoding/json package produces a canonical ordering for
// struct fields (declaration order) and a stable representation of
// maps and slices.
func RenderJSON(p *PublishPlan) ([]byte, error) {
	if p == nil {
		return nil, fmt.Errorf("plan: nil plan")
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	if err := enc.Encode(p); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// RenderText emits a concise, publication-oriented human-readable
// rendering of the plan. The JSON form is the authoritative
// representation; the text form is for humans only.
func RenderText(p *PublishPlan, w io.Writer) error {
	if p == nil {
		return fmt.Errorf("plan: nil plan")
	}
	if _, err := fmt.Fprintln(w, "bjj plan"); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "schema_version: %d\n", p.SchemaVersion); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "status:         %s\n", p.Status); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "remote:         %s\n", p.Remote.Name); err != nil {
		return err
	}
	for i, m := range p.BookmarkMoves {
		if _, err := fmt.Fprintf(w, "bookmark[%d]:   %s\n", i, m.Name); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "  old: %s\n", renderMoveSide(m.Old)); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "  new: %s\n", renderMoveSide(m.New)); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(w, "commits:        %d\n", len(p.Commits)); err != nil {
		return err
	}
	for i, c := range p.Commits {
		if _, err := fmt.Fprintf(w, "  [%d] commit=%s change=%s\n", i, c.CommitID, c.ChangeID); err != nil {
			return err
		}
	}
	return nil
}

func renderMoveSide(c *CommitRef) string {
	if c == nil {
		return "<absent>"
	}
	return fmt.Sprintf("commit=%s change=%s", c.CommitID, c.ChangeID)
}

// RenderErrorText emits an actionable, single-line error message.
func RenderErrorText(e *Error) string {
	if e == nil {
		return "plan: <nil error>"
	}
	if e.Cause != nil {
		return fmt.Sprintf("plan: %s: %s: %s",
			e.Code, e.Message, strings.TrimSpace(e.Cause.Error()))
	}
	return fmt.Sprintf("plan: %s: %s", e.Code, e.Message)
}

// ErrorJSON is the stable machine-readable structure of a typed
// plan error.
type ErrorJSON struct {
	SchemaVersion int      `json:"schema_version"`
	Code          string   `json:"code"`
	Message       string   `json:"message"`
	Cause         string   `json:"cause,omitempty"`
	Argv          []string `json:"argv,omitempty"`
	ExitCode      int      `json:"exit_code,omitempty"`
}

// RenderErrorJSON renders a typed plan error as strict JSON.
func RenderErrorJSON(e *Error) ([]byte, error) {
	if e == nil {
		return nil, fmt.Errorf("plan: nil error")
	}
	out := ErrorJSON{
		SchemaVersion: SchemaVersion,
		Code:          string(e.Code),
		Message:       e.Message,
	}
	if e.Cause != nil {
		out.Cause = strings.TrimSpace(e.Cause.Error())
		if jjf := AsJJFailure(e.Cause); jjf != nil {
			out.Argv = append([]string(nil), jjf.JJArgv()...)
			out.ExitCode = jjf.JJExitCode()
		}
	}
	return json.MarshalIndent(out, "", "  ")
}

// JJFailure is the narrow contract satisfied by *jjadapter.ErrJJFailed
// so the plan package can surface argv/exit without depending on the
// adapter package's concrete type.
type JJFailure interface {
	error
	JJArgv() []string
	JJExitCode() int
}

// AsJJFailure returns the JJFailure view of err, or nil.
func AsJJFailure(err error) JJFailure {
	if jjf, ok := err.(JJFailure); ok {
		return jjf
	}
	return nil
}
