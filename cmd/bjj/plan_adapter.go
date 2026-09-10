package main

import (
	"github.com/s1onique/bjj/internal/admission"
	"github.com/s1onique/bjj/internal/plan"
)

// planAdapter implements admission.PlanView over a
// *plan.PublishPlan.
//
// CORRECTION01 (P1): the concrete adapter used to live in
// internal/admission/planview.go and forced the admission package
// to import internal/plan. To restore the documented decoupling
// (internal/admission imports no internal/plan), the adapter now
// lives in the CLI orchestration package, where the dependency
// direction is natural: cmd/bjj -> internal/plan and cmd/bjj ->
// internal/admission. The admission package itself depends only
// on the PlanView interface.
type planAdapter struct {
	p *plan.PublishPlan
}

// wrapPlan returns an admission.PlanView over p.
//
// Returns an admission *Error with CodeNoBookmarkMove when p is
// nil or carries zero bookmark moves.
func wrapPlan(p *plan.PublishPlan) (admission.PlanView, error) {
	if p == nil {
		return nil, admission.NewError(admission.CodeNoBookmarkMove,
			"admission: nil plan", nil)
	}
	if len(p.BookmarkMoves) == 0 {
		return nil, admission.NewError(admission.CodeNoBookmarkMove,
			"admission: plan has zero bookmark moves", nil)
	}
	return &planAdapter{p: p}, nil
}

// mustWrapPlan is a test-only convenience that panics on error.
func mustWrapPlan(p *plan.PublishPlan) admission.PlanView {
	v, err := wrapPlan(p)
	if err != nil {
		panic(err)
	}
	return v
}

func (a *planAdapter) RemoteName() string { return a.p.Remote.Name }

func (a *planAdapter) BookmarkName() string {
	if len(a.p.BookmarkMoves) == 0 {
		return ""
	}
	return a.p.BookmarkMoves[0].Name
}

func (a *planAdapter) OldCommitID() string {
	mv := a.p.BookmarkMoves[0]
	if mv.Old == nil {
		return ""
	}
	return mv.Old.CommitID
}

func (a *planAdapter) NewCommitID() string {
	mv := a.p.BookmarkMoves[0]
	if mv.New == nil {
		return ""
	}
	return mv.New.CommitID
}

func (a *planAdapter) StatusName() string { return string(a.p.Status) }

// OutgoingCommits returns the canonical publication subject set,
// verbatim from PublishPlan.Commits, in admission.CommitRef form.
//
// This is the source of truth that admission's fact gatherer now
// relies on (CORRECTION01 §1): admission MUST NOT independently
// reconstruct the subject set via "::New ~ root()".
func (a *planAdapter) OutgoingCommits() []admission.CommitRef {
	out := make([]admission.CommitRef, 0, len(a.p.Commits))
	for _, c := range a.p.Commits {
		out = append(out, admission.CommitRef{
			CommitID: c.CommitID,
			ChangeID: c.ChangeID,
		})
	}
	return out
}
