// Package admission — rendering.
//
// RenderJSON emits the strict, deterministic JSON representation
// of an AdmissionResult.
//
// RenderText emits a concise, publication-oriented human-readable
// rendering for `bjj admit` (non-JSON mode). The JSON form is the
// authoritative representation; the text form is for humans only.
package admission

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
)

// RenderJSON emits the strict, deterministic JSON representation
// of the admission result.
//
// The encoding/json package produces a canonical ordering for
// struct fields (declaration order) and a stable representation
// of maps and slices. Reasons are already sorted in canonical
// ReasonCode order by the evaluator, so two equivalent denials
// produce byte-identical output.
func RenderJSON(r AdmissionResult) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	if err := enc.Encode(r); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// RenderText writes a human-readable rendering of the admission
// decision to w.
func RenderText(r AdmissionResult, w io.Writer) error {
	if w == nil {
		return fmt.Errorf("admission: nil writer")
	}
	if _, err := fmt.Fprintln(w, "bjj admit"); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "schema_version: %d\n", r.SchemaVersion); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "decision:       %s\n", r.Decision); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "subject:\n"); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "  remote:     %s\n", r.Subject.Remote); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "  bookmark:   %s\n", r.Subject.Bookmark); err != nil {
		return err
	}
	if r.Subject.OldCommitID == "" {
		if _, err := fmt.Fprintln(w, "  old_commit: <absent>"); err != nil {
			return err
		}
	} else {
		if _, err := fmt.Fprintf(w, "  old_commit: %s\n", r.Subject.OldCommitID); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(w, "  new_commit: %s\n", r.Subject.NewCommitID); err != nil {
		return err
	}
	if len(r.Reasons) > 0 {
		if _, err := fmt.Fprintln(w, "reasons:"); err != nil {
			return err
		}
		for _, rs := range r.Reasons {
			if rs.Message != "" {
				if _, err := fmt.Fprintf(w, "  - %s: %s\n", rs.Code, rs.Message); err != nil {
					return err
				}
			} else {
				if _, err := fmt.Fprintf(w, "  - %s\n", rs.Code); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// RenderErrorText emits a single-line error message for a typed
// admission *Error.
func RenderErrorText(e *Error) string {
	if e == nil {
		return "admission: <nil error>"
	}
	if e.Cause != nil {
		return fmt.Sprintf("admission: %s: %s: %s",
			e.Code, e.Message, strings.TrimSpace(e.Cause.Error()))
	}
	return fmt.Sprintf("admission: %s: %s", e.Code, e.Message)
}

// ErrorJSON is the stable machine-readable structure of a typed
// admission error.
type ErrorJSON struct {
	SchemaVersion int    `json:"schema_version"`
	Code          string `json:"code"`
	Message       string `json:"message"`
	Cause         string `json:"cause,omitempty"`
}

// RenderErrorJSON renders a typed admission error as strict JSON.
func RenderErrorJSON(e *Error) ([]byte, error) {
	if e == nil {
		return nil, fmt.Errorf("admission: nil error")
	}
	out := ErrorJSON{
		SchemaVersion: SchemaVersion,
		Code:          string(e.Code),
		Message:       e.Message,
	}
	if e.Cause != nil {
		out.Cause = strings.TrimSpace(e.Cause.Error())
	}
	return json.MarshalIndent(out, "", "  ")
}

// sortReasonsPublic is the externally visible wrapper used by
// callers that want the same canonical reason ordering as the
// evaluator. It exists primarily for testing.
func SortReasons(rs []Reason) {
	sort.SliceStable(rs, func(i, j int) bool {
		return reasonRank(rs[i].Code) < reasonRank(rs[j].Code)
	})
}
