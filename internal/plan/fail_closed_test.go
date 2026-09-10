package plan_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/s1onique/bjj/internal/jjadapter"
	"github.com/s1onique/bjj/internal/lab"
	"github.com/s1onique/bjj/internal/plan"
)

// TestPlan_EffectiveOldQueryFailsClosed is the bounded test for
// ACT-BJJ-PLAN01-CORRECTION02 §2: when the effective-old
// CommitsAt query fails, the resolver MUST propagate the failure
// as JJ_QUERY_FAILED. It MUST NOT swallow the error and fall
// back to "no effective OLD" (which would silently widen the
// publication subject to ::NEW ~ root()).
func TestPlan_EffectiveOldQueryFailsClosed(t *testing.T) {
	requirePlanPrereqs(t)
	ctx := context.Background()
	l, err := lab.Setup(ctx)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	defer l.Cleanup()

	// Use a brand-new bookmark that has never been pushed to the
	// remote. This forces the resolver into the
	// nearestRemoteAncestor path: it must query
	// heads(::NEW & remote_bookmarks()), which is the call we
	// inject a failure for.
	if err := l.RunJJ(ctx, []string{"new", "main", "-m", "effective-old test"}); err != nil {
		t.Fatalf("jj new: %v", err)
	}
	if err := l.RunJJ(ctx, []string{"bookmark", "create", "orphan-feature", "-r", "@"}); err != nil {
		t.Fatalf("jj bookmark create: %v", err)
	}

	src := plan.NewJJSource(jjadapter.New())
	wrapped := &effectiveOldFailingSource{inner: src}

	p, err := plan.Resolve(ctx, wrapped, plan.ResolveOptions{
		Dir: l.JJClient, Remote: "lab", Bookmark: "orphan-feature",
	})
	if err == nil {
		t.Fatalf("expected effective-old failure; got plan=%+v", p)
	}
	pe, ok := plan.AsError(err)
	if !ok {
		t.Fatalf("expected *plan.Error, got %T: %v", err, err)
	}
	if pe.Code != plan.CodeJJQueryFailed {
		t.Fatalf("code=%s, want JJ_QUERY_FAILED", pe.Code)
	}
	if !strings.Contains(strings.ToLower(pe.Message), "effective-old") {
		t.Fatalf("message %q does not identify the effective-old query", pe.Message)
	}
	if p != nil {
		t.Fatalf("plan must be nil when query fails; got %+v", p)
	}
}

// TestPlan_BookmarkOutputMalformed_FailsClosed is the bounded test
// for ACT-BJJ-PLAN01-CORRECTION02 §3: when `jj bookmark list`
// emits a malformed row, the adapter MUST surface an
// *ErrJJParseFailed (which the resolver maps to JJ_QUERY_FAILED)
// rather than silently dropping the row or inventing partial
// values.
func TestPlan_BookmarkOutputMalformed_FailsClosed(t *testing.T) {
	prog := "jj"
	argv := []string{"bookmark", "list", "--all-remotes"}

	cases := []struct {
		name string
		body string
	}{
		{name: "wrong-field-count", body: "feature|lab|true|false\n"},
		{name: "non-boolean-present", body: "feature|lab|yes|false|abcdef||\n"},
		{name: "non-boolean-conflict", body: "feature|lab|true|maybe|abcdef||\n"},
		{name: "present-nonconflict-empty-commit", body: "feature|lab|true|false|||\n"},
		{name: "conflict-with-nonempty-commit", body: "feature|lab|false|true|abcdef|a:b|c:d\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseBookmarkForTest(prog, argv, []byte(tc.body))
			if err == nil {
				t.Fatalf("expected ErrJJParseFailed, got nil")
			}
			if !jjadapter.IsJJParseFailed(err) {
				t.Fatalf("expected ErrJJParseFailed, got %T: %v", err, err)
			}
			var p *jjadapter.ErrJJParseFailed
			if !errors.As(err, &p) {
				t.Fatalf("errors.As did not unwrap ErrJJParseFailed")
			}
			if p.Kind != "bookmark" {
				t.Fatalf("Kind=%q, want bookmark", p.Kind)
			}
		})
	}
}

// TestPlan_CommitOutputMalformed_FailsClosed is the bounded test
// for ACT-BJJ-PLAN01-CORRECTION02 §3: when `jj log` emits a
// malformed row, the adapter MUST surface *ErrJJParseFailed.
// Silently skipping rows is forbidden because losing a row can
// alter the semantic state BJJ thinks it observed.
func TestPlan_CommitOutputMalformed_FailsClosed(t *testing.T) {
	prog := "jj"
	argv := []string{"log", "--no-graph"}

	cases := []struct {
		name string
		body string
	}{
		{name: "no-pipe", body: "garbage line with no pipe\n"},
		{name: "empty-commit-id", body: "|abcdef\n"},
		{name: "empty-change-id", body: "abcdef|\n"},
		{name: "leading-pipe", body: "|abcdef\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseCommitForTest(prog, argv, []byte(tc.body))
			if err == nil {
				t.Fatalf("expected ErrJJParseFailed, got nil")
			}
			if !jjadapter.IsJJParseFailed(err) {
				t.Fatalf("expected ErrJJParseFailed, got %T: %v", err, err)
			}
		})
	}
}

// TestPlan_MalformedJJOutput_SurfacesAsJJQueryFailed is the
// end-to-end proof that a malformed-jj-output path surfaces as
// CodeJJQueryFailed at the resolver boundary, satisfying
// PLAN_MALFORMED_JJ_OUTPUT = JJ_QUERY_FAILED.
func TestPlan_MalformedJJOutput_SurfacesAsJJQueryFailed(t *testing.T) {
	requirePlanPrereqs(t)
	ctx := context.Background()
	l, err := lab.Setup(ctx)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	defer l.Cleanup()

	if err := l.SeedBookmarkPush(ctx, "feature"); err != nil {
		t.Fatalf("seed: %v", err)
	}

	src := plan.NewJJSource(jjadapter.New())
	wrapped := &malformedBookmarkSource{inner: src}

	_, err = plan.Resolve(ctx, wrapped, plan.ResolveOptions{
		Dir: l.JJClient, Remote: "lab", Bookmark: "feature",
	})
	if err == nil {
		t.Fatalf("expected error from malformed bookmark output; got nil")
	}
	pe, ok := plan.AsError(err)
	if !ok {
		t.Fatalf("expected *plan.Error, got %T: %v", err, err)
	}
	if pe.Code != plan.CodeJJQueryFailed {
		t.Fatalf("code=%s, want JJ_QUERY_FAILED (PLAN_MALFORMED_JJ_OUTPUT)", pe.Code)
	}
}

// effectiveOldFailingSource wraps a real Source and forces the
// effective-old CommitsAt query to return an error. The wrapping
// detects the revset `heads(... & remote_bookmarks())` and
// substitutes a hard failure for that single call; all other
// CommitsAt calls delegate to the real source.
type effectiveOldFailingSource struct {
	inner plan.Source
}

func (s *effectiveOldFailingSource) Snapshot(ctx context.Context, dir string) (jjadapter.Snapshot, error) {
	return s.inner.Snapshot(ctx, dir)
}

func (s *effectiveOldFailingSource) CommitsAt(ctx context.Context, dir, opID, revset string) ([]jjadapter.CommitRef, error) {
	if strings.Contains(revset, "remote_bookmarks()") {
		return nil, errors.New("injected: effective-old query failed")
	}
	return s.inner.CommitsAt(ctx, dir, opID, revset)
}

// malformedBookmarkSource simulates a parse failure from the
// adapter by returning a typed *jjadapter.ErrJJParseFailed from
// Snapshot. The resolver's snapshot-error path maps any
// non-plan-typed error to CodeJJQueryFailed, satisfying
// PLAN_MALFORMED_JJ_OUTPUT = JJ_QUERY_FAILED.
type malformedBookmarkSource struct {
	inner plan.Source
}

func (s *malformedBookmarkSource) Snapshot(ctx context.Context, dir string) (jjadapter.Snapshot, error) {
	return jjadapter.Snapshot{}, &jjadapter.ErrJJParseFailed{
		Program: "jj",
		Argv:    []string{"bookmark", "list", "--all-remotes"},
		Kind:    "bookmark",
		Line:    "broken|row",
		Reason:  "injected: malformed bookmark row in test",
	}
}

func (s *malformedBookmarkSource) CommitsAt(ctx context.Context, dir, opID, revset string) ([]jjadapter.CommitRef, error) {
	return s.inner.CommitsAt(ctx, dir, opID, revset)
}

// parseBookmarkForTest is a thin test shim that calls the
// adapter's exported ParseBookmarkLines.
func parseBookmarkForTest(prog string, argv []string, body []byte) ([]jjadapter.BookmarkRef, error) {
	return jjadapter.ParseBookmarkLines(prog, argv, body)
}

// parseCommitForTest calls the adapter's exported ParseCommitLines.
func parseCommitForTest(prog string, argv []string, body []byte) ([]jjadapter.CommitRef, error) {
	return jjadapter.ParseCommitLines(prog, argv, body)
}

// TestPlan_DocSingleViewContractMatchesCode is the bounded check
// for ACT-BJJ-PLAN01-CORRECTION02 §4: the production Source
// contract and the JJSource implementation MUST both express
// the same single-view story (pinned-once, never re-pin, refuse
// empty opID). This test inspects the contract through the type
// system rather than re-parsing documentation.
func TestPlan_DocSingleViewContractMatchesCode(t *testing.T) {
	// A nil source is rejected — guards against accidentally
	// introducing a "use a default source" path.
	_, err := plan.Resolve(context.Background(), nil, plan.ResolveOptions{
		Dir:      "/tmp",
		Remote:   "lab",
		Bookmark: "feature",
	})
	if err == nil {
		t.Fatalf("nil source must fail closed")
	}
	pe, ok := plan.AsError(err)
	if !ok {
		t.Fatalf("expected *plan.Error, got %T", err)
	}
	if pe.Code != plan.CodeJJQueryFailed {
		t.Fatalf("nil source code=%s, want JJ_QUERY_FAILED", pe.Code)
	}

	// Empty opID is rejected at the adapter boundary.
	src := plan.NewJJSource(jjadapter.New())
	_, err = src.CommitsAt(context.Background(), "/tmp", "", "all()")
	pe, ok = plan.AsError(err)
	if !ok {
		t.Fatalf("empty opID must produce *plan.Error")
	}
	if pe.Code != plan.CodeInconsistentView {
		t.Fatalf("empty opID code=%s, want INCONSISTENT_REPOSITORY_VIEW", pe.Code)
	}
}
