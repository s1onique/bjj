package jjadapter

import (
	"errors"
	"testing"
)

// TestParseBookmarkLines_FailsClosedOnMalformedInput is the
// adapter-local guard for ACT-BJJ-PLAN01-CORRECTION02 §3. Any
// non-empty, non-trailer row whose shape does not match the
// machine-oriented template (7 pipe-separated fields) MUST yield
// *ErrJJParseFailed.
func TestParseBookmarkLines_FailsClosedOnMalformedInput(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"wrong-field-count-few", "feature|lab|true|false\n"},
		{"wrong-field-count-many", "feature|lab|true|false|abcdef||extra|more\n"},
		{"non-boolean-present", "feature|lab|yes|false|abcdef||\n"},
		{"non-boolean-conflict", "feature|lab|true|maybe|abcdef||\n"},
		{"present-nonconflict-empty-commit", "feature|lab|true|false|||\n"},
		{"conflict-with-nonempty-commit", "feature|lab|false|true|abcdef|a:b|c:d\n"},
	}
	prog := "jj"
	argv := []string{"bookmark", "list", "--all-remotes"}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseBookmarkLines(prog, argv, []byte(tc.body))
			if err == nil {
				t.Fatalf("expected ErrJJParseFailed, got nil")
			}
			var p *ErrJJParseFailed
			if !errors.As(err, &p) {
				t.Fatalf("errors.As did not unwrap ErrJJParseFailed; got %T", err)
			}
			if p.Kind != "bookmark" {
				t.Fatalf("Kind=%q, want bookmark", p.Kind)
			}
		})
	}
}

// TestParseBookmarkLines_AcceptsValidInput asserts the parser
// accepts a well-formed row.
func TestParseBookmarkLines_AcceptsValidInput(t *testing.T) {
	body := "feature||true|false|abcdef0123456789abcdef0123456789abcdef0123||\n"
	out, err := ParseBookmarkLines("jj", []string{"bookmark", "list"}, []byte(body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("expected 1 row, got %d", len(out))
	}
	br := out[0]
	if br.Name != "feature" {
		t.Fatalf("Name=%q", br.Name)
	}
	if br.Kind != "LOC" {
		t.Fatalf("Kind=%q, want LOC", br.Kind)
	}
	if !br.Present || br.Conflict {
		t.Fatalf("Present/Conflict wrong: %+v", br)
	}
	if br.Target == nil || br.Target.CommitID != "abcdef0123456789abcdef0123456789abcdef0123" {
		t.Fatalf("Target wrong: %+v", br.Target)
	}
}

// TestParseCommitLines_FailsClosedOnMalformedInput is the
// adapter-local guard for ACT-BJJ-PLAN01-CORRECTION02 §3.
// Malformed commit rows MUST yield *ErrJJParseFailed.
func TestParseCommitLines_FailsClosedOnMalformedInput(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"no-pipe", "garbage line with no pipe\n"},
		{"empty-commit-id", "|abcdef\n"},
		{"empty-change-id", "abcdef|\n"},
		{"leading-pipe", "|abcdef\n"},
		{"empty-line-ok", "\n"}, // empty lines are skipped, not errors
	}
	prog := "jj"
	argv := []string{"log", "--no-graph"}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, err := ParseCommitLines(prog, argv, []byte(tc.body))
			if tc.name == "empty-line-ok" {
				if err != nil {
					t.Fatalf("empty line must be skipped without error; got %v", err)
				}
				if len(out) != 0 {
					t.Fatalf("empty line produced %d rows", len(out))
				}
				return
			}
			if err == nil {
				t.Fatalf("expected ErrJJParseFailed, got nil")
			}
			var p *ErrJJParseFailed
			if !errors.As(err, &p) {
				t.Fatalf("errors.As did not unwrap ErrJJParseFailed")
			}
			if p.Kind != "commit" {
				t.Fatalf("Kind=%q, want commit", p.Kind)
			}
		})
	}
}

// TestParseCommitLines_AcceptsValidInput asserts the parser
// accepts a well-formed row.
func TestParseCommitLines_AcceptsValidInput(t *testing.T) {
	body := "abcdef0123456789abcdef0123456789abcdef0123|abcdef0123456789abcdef0123456789abcdef0123\n"
	out, err := ParseCommitLines("jj", []string{"log"}, []byte(body))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("expected 1 row, got %d", len(out))
	}
	c := out[0]
	if c.CommitID != "abcdef0123456789abcdef0123456789abcdef0123" {
		t.Fatalf("CommitID=%q", c.CommitID)
	}
	if c.ChangeID != "abcdef0123456789abcdef0123456789abcdef0123" {
		t.Fatalf("ChangeID=%q", c.ChangeID)
	}
}
