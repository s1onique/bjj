// Package plan is BJJ's first publication-domain primitive.
//
// It owns the typed description of what publication would mean for a
// given (remote, bookmark) pair, observed against the local Jujutsu
// repository state.
//
// A PublishPlan is NOT:
//
//   - working tree status;
//   - current @ commit;
//   - git diff;
//   - a shell command;
//   - human-formatted jj output.
//
// It is the answer to the question "what would publication mean right
// now?" and nothing else. PLAN01 deliberately does not hash, bind
// evidence to, verify, or transport the plan.
//
// Future ACTs (admission, check, evidence, publish) will consume this
// type. PLAN01 only constructs and renders it.
package plan

import (
	"github.com/s1onique/bjj/internal/jjadapter"
)

// SchemaVersion is the version of the JSON contract produced by this
// package. It is incremented on incompatible changes.
const SchemaVersion = 1

// Status is the discrete state of a resolved plan.
//
// Status is intentionally small and machine-comparable. The error
// model below carries the typed failure codes; Status here covers the
// positive outcomes only.
type Status string

const (
	// StatusPlanned means the plan describes a real mutation that
	// publication would perform: at least one bookmark move exists
	// with Old != New.
	StatusPlanned Status = "planned"

	// StatusNoRemoteChange means publication would be a no-op:
	// every resolved bookmark move has Old == New (or Old and New
	// are both absent). It is a positive, machine-readable result,
	// not an error.
	StatusNoRemoteChange Status = "no_remote_change"
)

// Remote identifies the logical remote name participating in the
// plan.
//
// The remote URL is intentionally NOT a field of the canonical
// plan body. Two repositories with identical semantic Jujutsu
// state but different filesystem locations (or different URL
// forms for the same remote) MUST produce identical canonical
// plan JSON. URL diagnostics belong to a future observation
// envelope, not the publication subject.
type Remote struct {
	// Name is the logical Jujutsu remote name (e.g. "lab", "origin").
	Name string `json:"name"`
}

// CommitRef is the typed identity of a commit in a plan. Both
// identifiers are full-length.
type CommitRef struct {
	// CommitID is the Git-compatible commit id (publication-relevant).
	CommitID string `json:"commit_id"`

	// ChangeID is the Jujutsu change id (reasoning-relevant).
	ChangeID string `json:"change_id"`
}

// BookmarkMove describes the publication mutation that would happen
// for a single (remote, bookmark) pair.
//
// Old is nil when the remote-tracking bookmark is absent (i.e. this
// would CREATE a new remote bookmark).
type BookmarkMove struct {
	// Name is the bookmark name.
	Name string `json:"name"`

	// Old is the current remote target, or nil if absent.
	Old *CommitRef `json:"old"`

	// New is the current local target. Always non-nil when Status is
	// StatusPlanned or StatusNoRemoteChange.
	New *CommitRef `json:"new"`

	// Conflict is true when the local or remote bookmark is in a
	// conflicted state. The resolver surfaces such cases as typed
	// errors; this field is reserved for future ACTs that may
	// reify conflict candidates.
	Conflict bool `json:"conflict,omitempty"`
}

// PublishPlan is the typed, deterministic, read-only description of
// what publication would do, observed against a single pinned
// repository operation view.
//
// The PublishPlan body is the canonical publication SUBJECT. It
// contains NO environment-specific or operation-specific data:
// no remote URL, no source operation id. Two repositories with
// the same semantic Jujutsu state MUST produce identical PublishPlan
// JSON regardless of:
//
//   - the filesystem location they live in;
//   - the form of their remote URLs;
//   - the specific operation id observed.
//
// Observation metadata (e.g. which operation id was pinned) is
// carried separately in PlanObservation, which is a non-canonical
// diagnostic envelope layered around PublishPlan.
type PublishPlan struct {
	// SchemaVersion identifies the JSON contract. Consumers MUST
	// refuse to parse a plan whose schema_version differs.
	SchemaVersion int `json:"schema_version"`

	// Status is StatusPlanned or StatusNoRemoteChange.
	Status Status `json:"status"`

	// Remote identifies the logical remote participating in the plan.
	Remote Remote `json:"remote"`

	// BookmarkMoves lists the resolved moves in deterministic order.
	// For PLAN01 the list always contains exactly one element.
	BookmarkMoves []BookmarkMove `json:"bookmark_moves"`

	// Commits lists every commit material to the plan in canonical
	// LEXICOGRAPHIC commit_id order (NOT ancestor-before-descendant).
	// For a fast-forward move this is the set `::New ~ ::Old`. For a
	// new-bookmark move this is `::New` anchored at the nearest
	// remote-tracked ancestor.
	Commits []CommitRef `json:"commits"`
}

// PlanObservation wraps a PublishPlan with the observation metadata
// that produced it. The observation envelope is diagnostic-only:
// it MUST NOT participate in canonical subject comparison.
//
// Two observations of the same repository at different operation ids
// yield the same PublishPlan subject but different PlanObservation
// envelopes. This separation is enforced by ACT-BJJ-PLAN01
// CORRECTION01 §6.
type PlanObservation struct {
	// Plan is the canonical publication subject.
	Plan *PublishPlan `json:"plan"`

	// SourceOperationID is the operation id the plan was observed at.
	// Observation metadata only; NOT part of the canonical subject.
	SourceOperationID string `json:"source_operation_id"`

	// RepositoryPath is the absolute filesystem path of the
	// repository at observation time. Observation metadata only;
	// NOT part of the canonical subject.
	RepositoryPath string `json:"repository_path,omitempty"`
}

// Observe produces a PlanObservation by pairing a freshly-resolved
// plan with the snapshot metadata it was built from. The plan is
// the canonical subject; the surrounding fields are observation
// diagnostics.
func Observe(p *PublishPlan, snap jjadapter.Snapshot, repoPath string) *PlanObservation {
	if p == nil {
		return nil
	}
	return &PlanObservation{
		Plan:              p,
		SourceOperationID: snap.OperationID,
		RepositoryPath:    repoPath,
	}
}

// fromJJCommit converts a jjadapter CommitRef to the plan's
// CommitRef.
func fromJJCommit(c jjadapter.CommitRef) CommitRef {
	return CommitRef{CommitID: c.CommitID, ChangeID: c.ChangeID}
}
