// Package admission — pure evaluator.
//
// Evaluate is the single admission decision function. It performs
// no subprocess calls and has no observable side effects, so it
// can be unit-tested with hand-constructed AdmissionInput values
// and asserted for byte-identical JSON output (§38).
package admission

import (
	"sort"
)

// Evaluate runs the deterministic admission decision over the
// provided input.
//
// Evaluate is pure: it does not call into the filesystem, the
// adapter, the plan resolver, or any other I/O surface. All
// admission facts MUST already be gathered by the caller.
//
// Three terminal outcomes:
//
//	DecisionAdmit       - subject is structurally eligible
//	DecisionDeny        - subject is prohibited by policy
//	DecisionNotNeeded   - publication would be a no-op
//
// Errors returned from Evaluate are reserved for true
// infrastructure problems with the input itself (e.g. an
// unparseable plan status); policy violations are reported via
// the Decision/Reasons fields, NOT as errors.
func Evaluate(in AdmissionInput) (AdmissionResult, error) {
	if in.Plan == nil {
		return AdmissionResult{}, NewError(CodeNoBookmarkMove,
			"admission: nil plan view", nil)
	}

	subject := SubjectIdentity{
		Remote:      in.Plan.RemoteName(),
		Bookmark:    in.Plan.BookmarkName(),
		OldCommitID: in.Plan.OldCommitID(),
		NewCommitID: in.Plan.NewCommitID(),
	}

	// Early structural check: facts' SourceOperationID must equal
	// the plan observation's SourceOperationID. We do not have the
	// plan observation here; the CLI layer is responsible for
	// verifying this BEFORE calling Evaluate and passing the
	// matched-opID facts. We DO, however, defensively reject an
	// empty facts opID as FACTS_INCONSISTENT because it indicates
	// the gatherer failed to bind to the operation view.
	if in.Facts.SourceOperationID == "" {
		reasons := []Reason{{
			Code:    ReasonFactsInconsistent,
			Message: "admission facts missing SourceOperationID; refusing to evaluate",
		}}
		return AdmissionResult{
			SchemaVersion: SchemaVersion,
			Decision:      DecisionDeny,
			Reasons:       reasons,
			Subject:       subject,
		}, nil
	}

	reasons := []Reason{}

	// 1. No-op short-circuit. DecisionNotNeeded trumps every other
	//    rule; if publication would not change the remote, we
	//    MUST NOT spend any further verification budget.
	if in.Facts.MoveRelation == MoveNoChange {
		return AdmissionResult{
			SchemaVersion: SchemaVersion,
			Decision:      DecisionNotNeeded,
			Reasons: []Reason{{
				Code:    ReasonNoRemoteChange,
				Message: "no remote change; publication would be a no-op",
			}},
			Subject: subject,
		}, nil
	}

	// 2. Move-shape rules.
	switch in.Facts.MoveRelation {
	case MoveCreate:
		if !in.Policy.AllowNewBookmarks {
			reasons = append(reasons, Reason{
				Code:    ReasonNewBookmarkNotAllowed,
				Message: "policy forbids creating new remote bookmarks",
			})
		}
	case MoveNonFastForward:
		if !in.Policy.AllowNonFastForward {
			reasons = append(reasons, Reason{
				Code:    ReasonNonFastForward,
				Message: "policy forbids non-fast-forward moves",
			})
		}
	case MoveFastForward:
		// Always admissible on shape grounds.
	default:
		// Defensive: an unknown MoveRelation is a structural
		// admission failure; deny with the strongest typed
		// reason so the caller does not silently ADMIT.
		reasons = append(reasons, Reason{
			Code:    ReasonFactsInconsistent,
			Message: "admission facts carry an unknown MoveRelation; refusing to evaluate",
		})
	}

	// 3. Private-commit rule.
	if len(in.Facts.PrivateCommits) > 0 {
		reasons = append(reasons, Reason{
			Code:    ReasonPrivateCommit,
			Message: "outgoing commit set contains a private commit (or a private ancestor)",
		})
	}

	// 4. Conflicted-commit rule.
	if !in.Policy.AllowConflictedCommits && len(in.Facts.ConflictedCommits) > 0 {
		reasons = append(reasons, Reason{
			Code:    ReasonConflictedCommit,
			Message: "outgoing commit set contains an unresolved conflict",
		})
	}

	// 5. Empty-description rule.
	if in.Policy.RequireDescription && len(in.Facts.EmptyDescriptionCommits) > 0 {
		reasons = append(reasons, Reason{
			Code:    ReasonEmptyDescription,
			Message: "outgoing commit set contains a commit with an empty description",
		})
	}

	// Sort reasons in canonical order so equivalent denials
	// produce byte-identical JSON regardless of the order in
	// which the rules above fired.
	sortReasons(reasons)

	if len(reasons) > 0 {
		return AdmissionResult{
			SchemaVersion: SchemaVersion,
			Decision:      DecisionDeny,
			Reasons:       reasons,
			Subject:       subject,
		}, nil
	}
	return AdmissionResult{
		SchemaVersion: SchemaVersion,
		Decision:      DecisionAdmit,
		Reasons:       []Reason{},
		Subject:       subject,
	}, nil
}

// sortReasons sorts reasons in canonical ReasonCode order
// (stable). Equal codes keep their relative order.
func sortReasons(rs []Reason) {
	sort.SliceStable(rs, func(i, j int) bool {
		return reasonRank(rs[i].Code) < reasonRank(rs[j].Code)
	})
}
