// Package admission implements ACT-BJJ-ADMISSION01 — BJJ's deterministic
// publication-subject admission boundary.
//
// Admission answers one question:
//
//	Given an already-resolved publication subject and facts observed
//	from the EXACT SAME Jujutsu operation view, is this subject
//	structurally eligible to proceed to expensive verification?
//
// Admission does NOT prove correctness. It does NOT execute Factory
// gates. It does NOT grant publication authority. It exists to
// reject obviously ineligible subjects BEFORE expensive checks run.
//
// The result is one of:
//
//	ADMIT              -- the subject is structurally eligible
//	DENY(<reasons>)    -- the subject is prohibited by policy
//	NOT_ADMITTED(...)  -- no publication is necessary
//
// Any failure to determine eligibility (query failure, malformed
// output, invalid policy) is a hard typed *Error, NOT a DENY.
//
// Admission is layered above internal/plan and internal/jjadapter:
//
//	internal/jjadapter  bounded jj adapter (typed records only)
//	        ↓
//	internal/plan       canonical PublishPlan + PlanObservation
//	        ↓
//	internal/admission  facts + policy + evaluator
//	        ↓
//	cmd/bjj             CLI orchestration (read-only)
//
// The package itself performs no subprocess calls. Fact gathering
// is the ONLY place that talks to the adapter; the evaluator is
// pure Go. The CLI is orchestration only.
package admission

// SchemaVersion is the version of the JSON contract produced by this
// package's renderers. It is incremented on incompatible changes.
const SchemaVersion = 1

// MoveRelation classifies a single bookmark move for admission
// purposes.
//
// It is the typed answer to the question "what shape is this move?"
// and MUST be derived from explicit ancestry / presence queries
// against the pinned operation view, never inferred from
// PublishPlan.Commits.
type MoveRelation string

const (
	// MoveCreate means the remote-tracking bookmark is absent and
	// the local bookmark has a target. Potentially admissible.
	MoveCreate MoveRelation = "CREATE"

	// MoveFastForward means the remote-tracking bookmark target is
	// an ancestor of the local bookmark target. Potentially
	// admissible.
	MoveFastForward MoveRelation = "FAST_FORWARD"

	// MoveNonFastForward means the remote-tracking bookmark target
	// is NOT an ancestor of the local bookmark target. DENY by
	// default; admission allows it only when the loaded Policy
	// explicitly enables it.
	MoveNonFastForward MoveRelation = "NON_FAST_FORWARD"

	// MoveNoChange means the remote-tracking bookmark target is
	// equal to the local bookmark target (or both are absent).
	// Not a publication transaction; admission returns
	// DecisionNotNeeded.
	MoveNoChange MoveRelation = "NO_CHANGE"
)

// Decision is the discrete outcome of an admission evaluation.
type Decision string

const (
	// DecisionAdmit means the subject is structurally eligible to
	// proceed to expensive verification.
	DecisionAdmit Decision = "admit"

	// DecisionDeny means the subject is prohibited by at least one
	// policy rule. Reasons are populated in deterministic order.
	DecisionDeny Decision = "deny"

	// DecisionNotNeeded means publication would be a no-op
	// (MoveNoChange) so no further verification should run. This is
	// a positive, machine-readable outcome, NOT an error.
	DecisionNotNeeded Decision = "not_needed"
)

// ReasonCode is the typed reason identifier carried by a Reason.
//
// Names are stable and machine-comparable. Callers MUST switch on
// Code, never on Message.
type ReasonCode string

const (
	// ReasonNoRemoteChange is reported on DecisionNotNeeded when the
	// resolved move relation is NO_CHANGE.
	ReasonNoRemoteChange ReasonCode = "NO_REMOTE_CHANGE"

	// ReasonNonFastForward is reported when the move relation is
	// NON_FAST_FORWARD and policy does not allow it.
	ReasonNonFastForward ReasonCode = "NON_FAST_FORWARD"

	// ReasonNewBookmarkNotAllowed is reported when the move relation
	// is CREATE and policy.AllowNewBookmarks is false.
	ReasonNewBookmarkNotAllowed ReasonCode = "NEW_BOOKMARK_NOT_ALLOWED"

	// ReasonPrivateCommit is reported when one or more outgoing
	// commits match the policy's private revset (per §9 this also
	// covers private ancestors that would expose the private commit
	// through publication).
	ReasonPrivateCommit ReasonCode = "PRIVATE_COMMIT"

	// ReasonConflictedCommit is reported when one or more outgoing
	// commits are conflicted (have unresolved content conflicts in
	// the pinned view).
	ReasonConflictedCommit ReasonCode = "CONFLICTED_COMMIT"

	// ReasonEmptyDescription is reported when one or more outgoing
	// commits have an empty description.
	ReasonEmptyDescription ReasonCode = "EMPTY_DESCRIPTION"

	// ReasonFactsInconsistent is reported when admission facts are
	// structurally inconsistent (e.g. SourceOperationID does not
	// match the plan observation's opID). This is treated as a
	// policy denial of the strongest kind, NOT a hard error,
	// because the caller has all the information required to
	// produce the typed decision.
	ReasonFactsInconsistent ReasonCode = "FACTS_INCONSISTENT"
)

// Reason is one typed denial / not-needed reason carried by a
// AdmissionResult.
//
// Code is stable; Message is diagnostic-only and MUST NOT be
// parsed by callers.
type Reason struct {
	Code    ReasonCode `json:"code"`
	Message string     `json:"message,omitempty"`
}

// SubjectIdentity is the typed subject identity carried inside a
// AdmissionResult so callers can cross-check which subject the
// decision evaluated.
//
// This is intentionally NOT a cryptographic digest (SubjectDigest
// belongs to a later evidence-binding ACT). It is just enough
// structure to verify the decision belongs to the same subject as
// the caller's plan.
type SubjectIdentity struct {
	// Remote is the logical remote name from the source plan.
	Remote string `json:"remote"`

	// Bookmark is the bookmark name from the source plan.
	Bookmark string `json:"bookmark"`

	// OldCommitID is the OLD target commit id, or empty if the
	// remote-tracking bookmark is absent (creation).
	OldCommitID string `json:"old_commit_id"`

	// NewCommitID is the NEW target commit id (always present for
	// admission-evaluable subjects).
	NewCommitID string `json:"new_commit_id"`
}

// Policy is the parsed, immutable admission policy for one
// admission run.
//
// Admission loads policy ONCE (per §27) and freezes it. Subsequent
// rules use the frozen snapshot; concurrent policy-file mutations
// only affect the NEXT admission run.
type Policy struct {
	// SchemaVersion is the parsed schema_version of the policy
	// file (always 1 today). Per CORRECTION01 §4 the version is
	// the deliberate forward-compatibility boundary: unknown
	// keys are FAIL CLOSED at any version we recognise, and
	// extending the schema requires bumping this value and
	// teaching the parser about the new key.
	SchemaVersion int `json:"schema_version"`

	// AllowNewBookmarks governs CREATE moves.
	AllowNewBookmarks bool `json:"allow_new_bookmarks"`

	// AllowNonFastForward governs NON_FAST_FORWARD moves.
	AllowNonFastForward bool `json:"allow_non_fast_forward"`

	// RequireDescription rejects outgoing commits with empty
	// descriptions. Always treated as a deny policy.
	RequireDescription bool `json:"require_description"`

	// AllowConflictedCommits governs CONFLICTED_COMMIT rejection.
	// When false (the default), conflicted outgoing commits are
	// denied; when true, the conflict check is skipped.
	AllowConflictedCommits bool `json:"allow_conflicted_commits"`

	// PrivateCommits is the BJJ-owned revset expression that
	// selects private commits in the frozen view. Default "none()".
	// The expression is opaque to admission; it is forwarded to
	// the adapter verbatim.
	PrivateCommits string `json:"private_commits"`
}

// DefaultPolicy returns the documented initial BJJ admission
// policy. It is the source of truth for ACT-BJJ-ADMISSION01 §24.
//
//	allow_new_bookmarks      = true
//	allow_non_fast_forward   = false
//	require_description      = true
//	allow_conflicted_commits = false
//	private_commits          = "none()"
func DefaultPolicy() Policy {
	return Policy{
		SchemaVersion:          1,
		AllowNewBookmarks:      true,
		AllowNonFastForward:    false,
		RequireDescription:     true,
		AllowConflictedCommits: false,
		PrivateCommits:         "none()",
	}
}

// AdmissionFacts is the smallest sufficient set of facts about a
// pinned publication subject that the admission evaluator needs.
//
// Facts MUST be derived from the SAME operation view that produced
// the canonical PublishPlan. They MUST be deterministic, typed,
// and machine-comparable. They MUST NOT contain wall-clock time,
// temp paths, remote credentials, command duration, or human jj
// prose.
type AdmissionFacts struct {
	// SourceOperationID is the operation view the facts were
	// gathered at. MUST equal PlanObservation.SourceOperationID;
	// mismatch is treated as ReasonFactsInconsistent.
	SourceOperationID string `json:"source_operation_id"`

	// MoveRelation classifies the resolved move shape.
	MoveRelation MoveRelation `json:"move_relation"`

	// OutgoingCommits lists every commit material to publication
	// (the same set carried by PublishPlan.Commits, but typed here
	// for admission-only consumption).
	OutgoingCommits []CommitRef `json:"outgoing_commits"`

	// PrivateCommits lists every outgoing commit that matched the
	// policy's private revset (per §9 this also covers private
	// ancestors that would expose the private commit through
	// publication).
	PrivateCommits []CommitRef `json:"private_commits"`

	// ConflictedCommits lists every outgoing commit whose pinned
	// view had an unresolved content conflict. Empty when jj 0.41.0
	// cannot express the conflict predicate; see §11 and the
	// admission package documentation.
	ConflictedCommits []CommitRef `json:"conflicted_commits"`

	// EmptyDescriptionCommits lists every outgoing commit whose
	// description is empty in the pinned view.
	EmptyDescriptionCommits []CommitRef `json:"empty_description_commits"`
}

// CommitRef is the typed identity of a commit as observed by the
// admission fact gatherer. It mirrors plan.CommitRef but does not
// import internal/plan (to keep admission decoupled from plan's
// canonical rendering).
type CommitRef struct {
	CommitID string `json:"commit_id"`
	ChangeID string `json:"change_id"`
}

// PlanView is the tiny read-only interface admission needs from a
// publication plan. *plan.PublishPlan satisfies it via a thin
// adapter in this package, so admission does not need to import
// internal/plan directly.
//
// Implementing this interface in tests lets the evaluator be
// exercised in pure isolation.
type PlanView interface {
	// RemoteName returns the logical remote name from the plan.
	RemoteName() string

	// BookmarkName returns the bookmark name from the plan.
	BookmarkName() string

	// OldCommitID returns the OLD target commit id, or empty if
	// the remote-tracking bookmark is absent.
	OldCommitID() string

	// NewCommitID returns the NEW target commit id.
	NewCommitID() string

	// StatusName returns the plan status as a string ("planned"
	// or "no_remote_change"). This is the canonical signal that
	// triggers DecisionNotNeeded when the bookmark move is a
	// no-op.
	StatusName() string

	// OutgoingCommits returns the canonical publication subject
	// set, in lexicographic commit_id order.
	//
	// This is the SAME set carried by PublishPlan.Commits: the
	// set of commits material to publication. For a fast-forward
	// move it is "::New ~ ::Old"; for a new-bookmark move it is
	// the same set anchored at the nearest remote-tracked
	// ancestor.
	//
	// Per CORRECTION01 §1, admission MUST evaluate predicates
	// over this exact set, never a separately reconstructed
	// "::New ~ root()" superset. Implementations MUST return the
	// canonical PLAN01 set; admission treats any divergence as
	// a structural bug.
	OutgoingCommits() []CommitRef
}

// AdmissionInput is the conceptual input bundle the admission
// evaluator consumes.
//
// Plan is a small view over the canonical plan (kept as an
// interface to avoid an import cycle with internal/plan in
// tests; production callers pass *plan.PublishPlan wrapped by
// the PlanView adapter in this package). Facts and Policy are
// typed.
type AdmissionInput struct {
	// Plan is the canonical publication subject wrapped as a
	// PlanView.
	Plan PlanView

	// Facts are the predicates observed about Plan at the same
	// operation view.
	Facts AdmissionFacts

	// Policy is the loaded, frozen admission policy.
	Policy Policy
}

// AdmissionResult is the typed output of an admission evaluation.
//
// Result is canonical: same Input => byte-identical JSON. The
// SubjectIdentity is included so callers can verify which subject
// the decision belongs to without comparing the surrounding
// PlanView.
type AdmissionResult struct {
	// SchemaVersion identifies the JSON contract.
	SchemaVersion int `json:"schema_version"`

	// Decision is admit | deny | not_needed.
	Decision Decision `json:"decision"`

	// Reasons is non-empty exactly when Decision is deny or
	// not_needed. Reasons are emitted in canonical (sorted)
	// ReasonCode order so two equivalent denials produce
	// byte-identical JSON.
	Reasons []Reason `json:"reasons"`

	// Subject is the typed subject identity the decision
	// evaluated. It is intentionally limited to non-environmental
	// fields; the path / opID live elsewhere.
	Subject SubjectIdentity `json:"subject"`
}

// ReasonCodes returns the canonical total order of ReasonCode
// values used to sort Reasons in deterministic JSON output.
//
// The order is:
//
//	NO_REMOTE_CHANGE
//	NON_FAST_FORWARD
//	NEW_BOOKMARK_NOT_ALLOWED
//	PRIVATE_COMMIT
//	CONFLICTED_COMMIT
//	EMPTY_DESCRIPTION
//	FACTS_INCONSISTENT
//
// Adding a new ReasonCode requires extending this list AND the
// sortReasons helper used by the evaluator.
func ReasonCodes() []ReasonCode {
	return []ReasonCode{
		ReasonNoRemoteChange,
		ReasonNonFastForward,
		ReasonNewBookmarkNotAllowed,
		ReasonPrivateCommit,
		ReasonConflictedCommit,
		ReasonEmptyDescription,
		ReasonFactsInconsistent,
	}
}

// reasonRank assigns a stable rank for sorting. Unknown codes
// sort after all known codes (still deterministically, by name).
func reasonRank(c ReasonCode) int {
	for i, known := range ReasonCodes() {
		if c == known {
			return i
		}
	}
	// Unknown codes: large constant + name offset is not stable
	// across runs, so we just append them at the end. Since
	// admission never emits unknown codes, this branch is
	// defensive only.
	return len(ReasonCodes()) + 1
}
