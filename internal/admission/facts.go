// Package admission — fact gatherer.
//
// The fact gatherer is the ONLY place in the admission package
// that talks to the jj adapter. Its job is to populate an
// AdmissionFacts struct by issuing a small number of bounded,
// pinned, fail-closed queries against the operation view that
// produced the canonical plan.
//
// The gatherer is NOT the evaluator. The evaluator (evaluate.go)
// is pure Go and runs only over the typed facts produced here.
//
// CORRECTION01 invariant (P0):
//
//	AdmissionFacts.OutgoingCommits == PublishPlan.Commits
//
// The gatherer MUST NOT independently reconstruct the publication
// subject set. It reads the canonical subject from planView and
// restricts every subsequent predicate query to exactly that set.
// This eliminates the subject-widening class of bugs that PLAN01
// spent two corrections removing.
package admission

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/s1onique/bjj/internal/jjadapter"
)

// Source is the bounded repository observation interface the
// admission fact gatherer depends on. The concrete implementation
// is *jjadapter.Adapter; tests may substitute an in-memory
// implementation.
//
// Single-view consistency contract: every method accepts an
// explicit opID. The Source MUST pass --at-op=<opID> to every
// underlying jj invocation. If opID is empty, the gatherer
// refuses to proceed with a typed CodeInconsistentView error
// (per ACT §4: facts MUST come from the same operation view as
// the plan).
type Source interface {
	// ListCommits returns the commits selected by revset at the
	// given operation view.
	ListCommits(ctx context.Context, dir, opID, revset string) ([]jjadapter.CommitRef, error)

	// ListCommitObs returns commits + per-commit predicates
	// (conflict, first-line description) at the given operation
	// view.
	ListCommitObs(ctx context.Context, dir, opID, revset string) ([]jjadapter.CommitObs, error)
}

// GatherOptions parameterises a single admission fact-gathering
// call.
//
// Dir is the on-disk repository path (absolute). OpID is the
// pinned operation id; it MUST equal the opID the source plan was
// observed at.
type GatherOptions struct {
	// Dir is the absolute path to the repository working tree.
	Dir string

	// OpID is the operation view the facts MUST be gathered at.
	// MUST be non-empty; an empty OpID triggers
	// CodeInconsistentView.
	OpID string
}

// Gatherer wires a Source to the admission package's fact
// gathering logic. Production callers use NewGatherer with the
// real adapter; tests may construct one directly with a fake
// Source.
type Gatherer struct {
	src Source
}

// NewGatherer constructs a Gatherer over src.
func NewGatherer(src Source) *Gatherer {
	return &Gatherer{src: src}
}

// Gather produces the AdmissionFacts for a publication subject,
// pinned to opts.OpID.
//
// CORRECTION01 behaviour:
//
//   - OutgoingCommits is taken VERBATIM from planView.OutgoingCommits
//     (= PublishPlan.Commits). The gatherer NEVER issues a second
//     `::New ~ root()` query to reconstruct the subject set; doing
//     so would silently widen admission to ancestors that are
//     already on the remote.
//   - Every predicate query (private, conflict, empty-description)
//     is restricted to exactly that subject set via
//     `subject & predicate`. An already-published ancestor of the
//     subject does NOT participate in any predicate evaluation.
//   - The move-relation probe (OLD ∈ ::NEW) is the ONE exception:
//     it operates on the plan's NEW + OLD pair directly. This is
//     structural, not subject-widening.
//
// Query budget:
//
//  1. move-relation probe: (OLD & ::NEW) (or skipped for CREATE)
//  2. private predicate:   (subject & policy.PrivateCommits)
//  3. per-commit obs:      (subject)
//
// All queries use --at-op=<OpID>. Query failures propagate as
// typed *Error with CodeJJQueryFailed or CodeJJParseFailed; per
// §22 admission NEVER interprets a query failure as
// "predicate = false".
func (g *Gatherer) Gather(ctx context.Context, planView PlanView, policy Policy, opts GatherOptions) (AdmissionFacts, error) {
	if planView == nil {
		return AdmissionFacts{}, NewError(CodeNoBookmarkMove,
			"admission: nil plan view", nil)
	}
	if opts.OpID == "" {
		return AdmissionFacts{}, NewError(CodeInconsistentView,
			"admission: empty OpID; refusing to mix views", nil)
	}

	out := AdmissionFacts{
		SourceOperationID: opts.OpID,
	}

	// Short-circuit: a no-op plan carries zero outgoing commits
	// and produces DecisionNotNeeded without any jj query.
	if planView.StatusName() == PlanStatusNoRemoteChange {
		out.MoveRelation = MoveNoChange
		out.OutgoingCommits = []CommitRef{}
		return out, nil
	}

	newID := planView.NewCommitID()
	oldID := planView.OldCommitID()

	// Canonical subject: read directly from the plan view. The
	// plan resolver (PLAN01) already enumerated this set with the
	// effective-old semantics, in lexicographic commit_id order;
	// we copy it verbatim and trust it as the source of truth.
	planOutgoing := planView.OutgoingCommits()
	out.OutgoingCommits = append([]CommitRef(nil), planOutgoing...)
	sortCommitRefs(out.OutgoingCommits)

	// Move relation. The gatherer queries ancestry explicitly
	// rather than inferring from PublishPlan.Commits (§7).
	var err error
	out.MoveRelation, err = g.classifyMove(ctx, opts, oldID, newID)
	if err != nil {
		return AdmissionFacts{}, err
	}

	// Build the subject-set revset we will intersect every
	// predicate against. This is a disjunction of explicit
	// commit_ids, NOT `::New`, so it cannot widen.
	subjectRevset, err := buildSubjectRevset(out.OutgoingCommits)
	if err != nil {
		return AdmissionFacts{}, err
	}
	if subjectRevset == "" {
		// Empty subject set (only possible if OutgoingCommits
		// was empty for a non-no-op plan). Skip predicate
		// queries; facts stay empty.
		return out, nil
	}

	// Private predicate. We compute
	//   subject & policy.PrivateCommits
	// at the frozen operation view. This is restricted to the
	// exact canonical subject set; an already-published ancestor
	// of the subject does NOT participate, even if it would match
	// the policy's revset.
	if strings.TrimSpace(policy.PrivateCommits) == "" {
		return AdmissionFacts{}, NewError(CodePolicyInvalid,
			"admission: policy.PrivateCommits is empty; refusing to evaluate", nil)
	}
	privateRevset := fmt.Sprintf("(%s & (%s))", subjectRevset, policy.PrivateCommits)
	rawPrivate, err := g.src.ListCommits(ctx, opts.Dir, opts.OpID, privateRevset)
	if err != nil {
		return AdmissionFacts{}, errJJToAdmission("gather private predicate", err)
	}
	out.PrivateCommits = make([]CommitRef, 0, len(rawPrivate))
	for _, c := range rawPrivate {
		out.PrivateCommits = append(out.PrivateCommits, commitRefFromJJ(c))
	}
	sortCommitRefs(out.PrivateCommits)

	// Per-commit observations: conflict + first-line description,
	// restricted to exactly the canonical subject set.
	rawObs, err := g.src.ListCommitObs(ctx, opts.Dir, opts.OpID, subjectRevset)
	if err != nil {
		return AdmissionFacts{}, errJJToAdmission("gather per-commit observations", err)
	}
	for _, ob := range rawObs {
		if ob.Conflict {
			out.ConflictedCommits = append(out.ConflictedCommits, CommitRef{
				CommitID: ob.CommitID, ChangeID: ob.ChangeID,
			})
		}
		if ob.FirstLine == "" {
			out.EmptyDescriptionCommits = append(out.EmptyDescriptionCommits, CommitRef{
				CommitID: ob.CommitID, ChangeID: ob.ChangeID,
			})
		}
	}
	sortCommitRefs(out.ConflictedCommits)
	sortCommitRefs(out.EmptyDescriptionCommits)

	return out, nil
}

// buildSubjectRevset returns a Jujutsu revset expression that
// selects exactly the given commits, used to restrict predicate
// queries to the canonical publication subject set.
//
// The expression is a disjunction of explicit commit_id atoms:
//
//	commit_id_1 | commit_id_2 | ...
//
// Jujutsu accepts any commit_id in a revset as a single-commit
// selector, so this expression is exact: it cannot widen to
// ancestors of the subject, and it does not depend on the
// `::` (ancestors) operator.
//
// Returns ("", nil) when cs is empty.
func buildSubjectRevset(cs []CommitRef) (string, error) {
	if len(cs) == 0 {
		return "", nil
	}
	parts := make([]string, 0, len(cs))
	for _, c := range cs {
		id := strings.TrimSpace(c.CommitID)
		if id == "" {
			return "", NewError(CodeInconsistentView,
				"admission: outgoing subject contains a commit with empty commit_id", nil)
		}
		parts = append(parts, id)
	}
	return strings.Join(parts, " | "), nil
}

// classifyMove returns the MoveRelation for the (OLD, NEW) pair
// at the pinned operation view.
//
// It is implemented as a single bounded query:
//
//	(OLD == "")             -> CREATE  (no query needed)
//	OLD == NEW              -> NO_CHANGE
//	OLD & ::NEW  non-empty  -> FAST_FORWARD
//	otherwise               -> NON_FAST_FORWARD
//
// Note: jj 0.41.0 does not expose a `conflict()` revset function,
// but `conflict` is a per-commit template field, so conflict
// detection happens in Gather() via ListCommitObs, not here.
func (g *Gatherer) classifyMove(ctx context.Context, opts GatherOptions, oldID, newID string) (MoveRelation, error) {
	if oldID == "" {
		return MoveCreate, nil
	}
	if oldID == newID {
		return MoveNoChange, nil
	}
	// Ancestry probe: does ::NEW contain OLD?
	revset := fmt.Sprintf("(%s & ::%s)", oldID, newID)
	hits, err := g.src.ListCommits(ctx, opts.Dir, opts.OpID, revset)
	if err != nil {
		return "", errJJToAdmission("classify move (ancestry probe)", err)
	}
	if len(hits) > 0 {
		return MoveFastForward, nil
	}
	return MoveNonFastForward, nil
}

// sortCommitRefs sorts commit refs in lexicographic commit_id
// order. PLAN01 sorts the same way; admission preserves the
// convention so result JSON is byte-deterministic.
func sortCommitRefs(cs []CommitRef) {
	sort.Slice(cs, func(i, j int) bool {
		return cs[i].CommitID < cs[j].CommitID
	})
}
