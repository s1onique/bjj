package jjadapter

import (
	"errors"
	"testing"
)

// TestParseCommitObsLines_AcceptsValidInput verifies the parser
// accepts the canonical CommitObsTemplate output.
func TestParseCommitObsLines_AcceptsValidInput(t *testing.T) {
	body := "abcdef0123456789abcdef0123456789abcdef0123|abcdef0123456789abcdef0123456789abcdef0123|false|first line\n"
	out, err := ParseCommitObsLines("jj", []string{"log"}, []byte(body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("expected 1 row, got %d", len(out))
	}
	o := out[0]
	if o.CommitID != "abcdef0123456789abcdef0123456789abcdef0123" {
		t.Fatalf("CommitID=%q", o.CommitID)
	}
	if o.ChangeID != "abcdef0123456789abcdef0123456789abcdef0123" {
		t.Fatalf("ChangeID=%q", o.ChangeID)
	}
	if o.Conflict {
		t.Fatalf("Conflict=true, want false")
	}
	if o.FirstLine != "first line" {
		t.Fatalf("FirstLine=%q", o.FirstLine)
	}
}

// TestParseCommitObsLines_AcceptsConflict verifies the parser
// recognises "true" as Conflict.
func TestParseCommitObsLines_AcceptsConflict(t *testing.T) {
	body := "abcdef0123456789abcdef0123456789abcdef0123|abcdef0123456789abcdef0123456789abcdef0123|true|conflict commit\n"
	out, err := ParseCommitObsLines("jj", []string{"log"}, []byte(body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !out[0].Conflict {
		t.Fatalf("Conflict=false, want true")
	}
}

// TestParseCommitObsLines_AcceptsEmptyFirstLine verifies the
// parser accepts an empty first-line description (which is the
// signal admission uses to flag EMPTY_DESCRIPTION).
func TestParseCommitObsLines_AcceptsEmptyFirstLine(t *testing.T) {
	body := "abcdef0123456789abcdef0123456789abcdef0123|abcdef0123456789abcdef0123456789abcdef0123|false|\n"
	out, err := ParseCommitObsLines("jj", []string{"log"}, []byte(body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out[0].FirstLine != "" {
		t.Fatalf("FirstLine=%q, want empty", out[0].FirstLine)
	}
}

// TestParseCommitObsLines_FailsClosedOnMalformedInput verifies
// ACT-BJJ-ADMISSION01 §35: any malformed row yields
// *ErrJJParseFailed (the parser NEVER silently drops a row).
func TestParseCommitObsLines_FailsClosedOnMalformedInput(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"wrong-field-count", "abcdef|abcdef|false\n"},
		{"non-boolean-conflict", "abcdef|abcdef|maybe|first\n"},
		{"empty-commit-id", "|abcdef|false|first\n"},
		{"empty-change-id", "abcdef||false|first\n"},
		{"empty-line-ok", "\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseCommitObsLines("jj", []string{"log"}, []byte(tc.body))
			if tc.name == "empty-line-ok" {
				if err != nil {
					t.Fatalf("empty line must be skipped without error; got %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected ErrJJParseFailed, got nil")
			}
			var p *ErrJJParseFailed
			if !errors.As(err, &p) {
				t.Fatalf("errors.As did not unwrap ErrJJParseFailed; got %T", err)
			}
			if p.Kind != "commit-obs" {
				t.Fatalf("Kind=%q, want commit-obs", p.Kind)
			}
		})
	}
}
