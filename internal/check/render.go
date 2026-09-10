package check

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// RenderJSON emits the strict, deterministic JSON representation
// of a CheckResult.
//
// Two equivalent runs produce byte-identical output because the
// Aggregator pre-sorts outcomes by CheckID and the package never
// iterates maps for canonical output.
//
// Diagnostic-only fields on CheckOutcome (DurationMillis,
// WorkspacePath, ResolvedProgram) are tagged `json:"-"` and
// never appear here.
func RenderJSON(r CheckResult) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	if err := enc.Encode(r); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// RenderObservationJSON emits the strict, deterministic JSON
// representation of a CheckObservation.
//
// The shape is:
//
//	{
//	  "result": { ... canonical CheckResult ... },
//	  "source_operation_id": "...",
//	  "resolved_workspace_parent": "...",
//	  "diagnostics": [ { ... CheckDiagnostic ... }, ... ]
//	}
//
// Two successful runs of the same frozen subject at the SAME
// opID produce byte-identical canonical bodies AND diagnostic
// bodies (the latter because diagnostics have no time-varying
// fields; only stdout/stderr are bounded but capture is
// deterministic given identical inputs).
//
// Two observations of the same semantic subject at DIFFERENT
// opIDs may differ in source_operation_id AND in
// resolved_workspace_parent; the embedded canonical CheckResult
// MUST be byte-identical.
func RenderObservationJSON(obs CheckObservation) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetIndent("", "  ")
	if err := enc.Encode(obs); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// RenderText writes a human-readable rendering of a CheckResult
// to w.
func RenderText(r CheckResult, w io.Writer) error {
	if w == nil {
		return fmt.Errorf("check: nil writer")
	}
	if _, err := fmt.Fprintln(w, "bjj check"); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "schema_version: %d\n", r.SchemaVersion); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "status:         %s\n", r.Status); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, "subject:"); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "  remote:             %s\n", r.Subject.Remote); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "  bookmark:           %s\n", r.Subject.Bookmark); err != nil {
		return err
	}
	if r.Subject.OldCommitID == "" {
		if _, err := fmt.Fprintln(w, "  old_commit_id:      <absent>"); err != nil {
			return err
		}
	} else {
		if _, err := fmt.Fprintf(w, "  old_commit_id:      %s\n", r.Subject.OldCommitID); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintf(w, "  new_commit_id:      %s\n", r.Subject.NewCommitID); err != nil {
		return err
	}
	// source_operation_id is observation provenance and
	// therefore NOT part of the canonical subject identity
	// (CHECK01-CORRECTION01 §5). It is rendered by
	// RenderObservationText when the caller wants it.
	if _, err := fmt.Fprintf(w, "  materialized_files: %d\n", r.FileCount); err != nil {
		return err
	}
	if len(r.Checks) > 0 {
		if _, err := fmt.Fprintln(w, "checks:"); err != nil {
			return err
		}
		for _, c := range r.Checks {
			if _, err := fmt.Fprintf(w, "  - %s: %s (exit %d)\n", c.ID, c.Status, c.ExitCode); err != nil {
				return err
			}
		}
	}
	return nil
}

// RenderObservationText writes a human-readable rendering of
// a CheckObservation (canonical + diagnostics) to w. The
// observation-only fields (source_operation_id,
// resolved_workspace_parent, per-check diagnostics) are
// rendered here so they never leak into the canonical
// RenderText output.
func RenderObservationText(obs CheckObservation, w io.Writer) error {
	if w == nil {
		return fmt.Errorf("check: nil writer")
	}
	if err := RenderText(obs.Result, w); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, "observation:"); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "  source_operation_id:  %s\n", obs.SourceOperationID); err != nil {
		return err
	}
	if obs.ResolvedAbsoluteWorkspace != "" {
		if _, err := fmt.Fprintf(w, "  workspace_parent:     %s\n", obs.ResolvedAbsoluteWorkspace); err != nil {
			return err
		}
	}
	if len(obs.Diagnostics) > 0 {
		if _, err := fmt.Fprintln(w, "diagnostics:"); err != nil {
			return err
		}
		for _, d := range obs.Diagnostics {
			if _, err := fmt.Fprintf(w, "  - %s: program=%s argv=%v workspace=%s duration_ms=%d\n",
				d.ID, d.ResolvedProgram, d.Argv, trimPathForRender(d.WorkspacePath), d.DurationMillis); err != nil {
				return err
			}
		}
	}
	return nil
}

// trimPathForRender reduces an absolute path to its trailing
// two components for compact display. Used only by the
// observation text renderer.
func trimPathForRender(p string) string {
	if p == "" {
		return ""
	}
	parts := strings.Split(p, "/")
	if len(parts) <= 2 {
		return p
	}
	return ".../" + strings.Join(parts[len(parts)-2:], "/")
}

// RenderErrorText emits a single-line error message for a typed
// check *Error.
func RenderErrorText(e *Error) string {
	if e == nil {
		return "check: <nil error>"
	}
	if e.Cause != nil {
		return fmt.Sprintf("check: %s: %s: %s",
			e.Code, e.Message, strings.TrimSpace(e.Cause.Error()))
	}
	return fmt.Sprintf("check: %s: %s", e.Code, e.Message)
}

// ErrorJSON is the stable machine-readable structure of a typed
// check error.
type ErrorJSON struct {
	SchemaVersion int    `json:"schema_version"`
	Code          string `json:"code"`
	Message       string `json:"message"`
	Cause         string `json:"cause,omitempty"`
}

// RenderErrorJSON renders a typed check error as strict JSON.
func RenderErrorJSON(e *Error) ([]byte, error) {
	if e == nil {
		return nil, fmt.Errorf("check: nil error")
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
