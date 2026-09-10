// outgoingCommits returns the set of commits reachable from newRef
// that are not reachable from oldRef.
//
//   - When oldRef is non-nil: revset is `OLD..NEW`, which jj defines
//     as `::NEW ~ ::OLD` (ancestors of NEW, excluding those of OLD).
//
//   - When oldRef is nil (creating a new remote bookmark): we
//     resolve an "effective OLD" as the nearest ancestor of NEW
//     that is currently a remote-tracked bookmark target. If no
//     such ancestor exists (truly orphaned branch), we fall back
//     to `::NEW ~ root()` so the entire chain is described.
//
// The synthetic root() is always excluded because it is not a real
// commit and carries no publication-relevant identity.
//
// Per ACT §7: "Do NOT accidentally include the entire repository
// history if that is not useful for BJJ's future verification
// model." The effective-OLD heuristic implements this by anchoring
// the chain at the nearest remote-tracked commit, which is exactly
// the boundary that publication would actually cross.
package plan

import (
	"context"
	"fmt"

	"github.com/s1onique/bjj/internal/jjadapter"
)

// Source is the bounded repository observation interface the
// resolver depends on. The concrete implementation is jjadapter;
// tests may substitute an in-memory implementation.
//
// Single-view consistency contract:
//
// A single Resolve consumes exactly ONE operation view. Snapshot
// captures that view's operation id; every subsequent CommitsAt call
// MUST be made against that same operation id. The resolver never
// re-pins; if a caller wants to observe a fresh view, they must call
// Snapshot again (and Resolve again).
type Source interface {
	// Snapshot returns a pinned, consistent observation of the
	// repository state. The returned operation id MUST be reused
	// for every subsequent CommitsAt invocation within the same
	// plan.
	Snapshot(ctx context.Context, dir string) (jjadapter.Snapshot, error)

	// CommitsAt returns the commits selected by a revset, observed
	// against the EXACT operation id supplied. Implementations MUST
	// NOT re-pin or otherwise select a different operation view;
	// they MUST pass --at-op=<opID> to every underlying jj call.
	CommitsAt(ctx context.Context, dir, opID, revset string) ([]jjadapter.CommitRef, error)
}

func outgoingCommits(ctx context.Context, src Source, dir, opID string, oldRef, newRef *CommitRef) ([]jjadapter.CommitRef, error) {
	var revset string
	if oldRef != nil {
		revset = fmt.Sprintf("%s..%s ~ root()", oldRef.CommitID, newRef.CommitID)
	} else {
		// New bookmark: nearest remote-tracked ancestor is the
		// effective baseline. If none, fall back to the whole
		// chain (minus root).
		effOld, err := nearestRemoteAncestor(ctx, src, dir, opID, newRef.CommitID)
		if err != nil {
			return nil, err
		}
		if effOld == "" {
			revset = fmt.Sprintf("::%s ~ root()", newRef.CommitID)
		} else {
			revset = fmt.Sprintf("%s..%s ~ root()", effOld, newRef.CommitID)
		}
	}
	raw, err := src.CommitsAt(ctx, dir, opID, revset)
	if err != nil {
		return nil, NewError(CodeJJQueryFailed, "outgoing revset failed", err)
	}
	return raw, nil
}

// nearestRemoteAncestor returns the commit id of the nearest
// ancestor of commitID that is currently a remote-tracked bookmark
// target, or "" if no such ancestor exists.
//
// Implementation: use jj's revset
//
//	heads(::commitID & remote_bookmarks())
//
// which selects the most-recent commits among the intersection
// of NEW's ancestors and the current remote-tracked bookmarks.
//
// Fail-closed: if the underlying jj query fails, the error is
// propagated as CodeJJQueryFailed. We MUST NOT swallow it and
// fall back to "no effective OLD"; that would silently widen the
// outgoing set to the entire ::NEW ~ root() chain and produce a
// PublishPlan that does not reflect what the caller asked for.
// See ACT-BJJ-PLAN01-CORRECTION02 §2.
func nearestRemoteAncestor(ctx context.Context, src Source, dir, opID, commitID string) (string, error) {
	revset := fmt.Sprintf("heads(::%s & remote_bookmarks())", commitID)
	raw, err := src.CommitsAt(ctx, dir, opID, revset)
	if err != nil {
		// Fail closed: a query failure MUST surface as a typed
		// error. Returning ("", nil) here would cause the
		// resolver to fall through to ::NEW ~ root(), which
		// silently widens the publication subject. Callers are
		// expected to treat CodeJJQueryFailed as a hard stop.
		return "", NewError(CodeJJQueryFailed, "effective-old lookup failed", err)
	}
	if len(raw) == 0 {
		return "", nil
	}
	return raw[0].CommitID, nil
}

// ResolveOptions parameterises a single Resolve call.
//
// Only one (Remote, Bookmark) pair is supported in PLAN01.
type ResolveOptions struct {
	Dir      string
	Remote   string
	Bookmark string
}

// Resolve is the heart of PLAN01.
//
// It answers the single question: "exactly what shared remote state
// would a future publication transaction attempt to create or move?"
//
// Resolve is pure (no mutation), read-only (no remote contact), and
// single-view consistent (every underlying jj invocation is pinned
// to the SAME operation id captured at Snapshot time; the resolver
// never re-pins).
//
// Resolution order:
//
//  1. Pin the operation view (Snapshot) and obtain remotes +
//     bookmarks at that view. Capture snap.OperationID as opID.
//  2. Validate the remote exists.
//  3. Validate the local bookmark is non-conflicted and present.
//  4. Read the remote-tracking bookmark target.
//  5. Resolve OLD/NEW ChangeIDs by querying against opID.
//  6. Materialise the outgoing commit set against opID.
//  7. Sort the commit set in canonical lexicographic commit_id order.
//  8. Compare Old and New commit ids to determine Status.
//
// All failure modes return a *Error with a typed Code.
func Resolve(ctx context.Context, src Source, opts ResolveOptions) (*PublishPlan, error) {
	p, _, err := resolveInternal(ctx, src, opts)
	return p, err
}

// ResolveObserved is the same as Resolve but also returns the
// observation envelope (which carries the source operation id).
//
// The plan returned in obs.Plan is the canonical subject; the
// surrounding fields are observation diagnostics and MUST NOT
// participate in canonical subject comparison.
func ResolveObserved(ctx context.Context, src Source, opts ResolveOptions) (*PlanObservation, error) {
	p, snap, err := resolveInternal(ctx, src, opts)
	if err != nil {
		return nil, err
	}
	return Observe(p, snap, opts.Dir), nil
}

// resolveInternal does the work shared by Resolve and ResolveObserved.
func resolveInternal(ctx context.Context, src Source, opts ResolveOptions) (*PublishPlan, jjadapter.Snapshot, error) {
	if src == nil {
		return nil, jjadapter.Snapshot{}, NewError(CodeJJQueryFailed, "nil source", nil)
	}
	if opts.Dir == "" {
		return nil, jjadapter.Snapshot{}, NewError(CodeJJQueryFailed, "empty Dir", nil)
	}
	if opts.Remote == "" {
		return nil, jjadapter.Snapshot{}, NewError(CodeJJQueryFailed, "empty Remote", nil)
	}
	if opts.Bookmark == "" {
		return nil, jjadapter.Snapshot{}, NewError(CodeJJQueryFailed, "empty Bookmark", nil)
	}

	snap, err := src.Snapshot(ctx, opts.Dir)
	if err != nil {
		if _, ok := AsError(err); ok {
			return nil, jjadapter.Snapshot{}, err
		}
		return nil, jjadapter.Snapshot{}, NewError(CodeJJQueryFailed, "snapshot failed", err)
	}

	opID := snap.OperationID
	if opID == "" {
		return nil, jjadapter.Snapshot{}, NewError(CodeInconsistentView,
			"snapshot returned empty operation id; cannot guarantee single-view consistency", nil)
	}

	if _, ok := snap.Remotes[opts.Remote]; !ok {
		return nil, jjadapter.Snapshot{}, NewError(CodeRemoteNotFound,
			fmt.Sprintf("remote %q is not known to the repository", opts.Remote), nil)
	}

	loc, ok := jjadapter.FindLocalBookmark(snap, opts.Bookmark)
	if !ok || !loc.Present {
		return nil, jjadapter.Snapshot{}, NewError(CodeLocalBookmarkNotFound,
			fmt.Sprintf("local bookmark %q not found", opts.Bookmark), nil)
	}
	if loc.Conflict {
		return nil, jjadapter.Snapshot{}, NewError(CodeLocalBookmarkConflicted,
			fmt.Sprintf("local bookmark %q has multiple competing targets", opts.Bookmark), nil)
	}
	if loc.Target == nil {
		return nil, jjadapter.Snapshot{}, NewError(CodeJJQueryFailed,
			fmt.Sprintf("local bookmark %q present but target unknown", opts.Bookmark), nil)
	}

	rem, hasRem := jjadapter.FindRemoteBookmark(snap, opts.Bookmark, opts.Remote)
	if hasRem && rem.Conflict {
		return nil, jjadapter.Snapshot{}, NewError(CodeRemoteBookmarkConflicted,
			fmt.Sprintf("remote bookmark %q@%q has multiple competing targets",
				opts.Bookmark, opts.Remote), nil)
	}

	var oldRef *CommitRef
	if hasRem && rem.Present && rem.Target != nil {
		// Resolve the change id for the OLD target too, so the
		// plan carries both identifiers symmetrically. This
		// gives future ACTs a clean identity for the OLD side.
		oldChangeID, err := resolveChangeID(ctx, src, opts.Dir, opID, rem.Target.CommitID)
		if err != nil {
			return nil, jjadapter.Snapshot{}, err
		}
		oldRef = &CommitRef{
			CommitID: rem.Target.CommitID,
			ChangeID: oldChangeID,
		}
	}

	newChangeID, err := resolveChangeID(ctx, src, opts.Dir, opID, loc.Target.CommitID)
	if err != nil {
		return nil, jjadapter.Snapshot{}, err
	}
	newRef := &CommitRef{
		CommitID: loc.Target.CommitID,
		ChangeID: newChangeID,
	}

	outgoing, err := outgoingCommits(ctx, src, opts.Dir, opID, oldRef, newRef)
	if err != nil {
		return nil, jjadapter.Snapshot{}, err
	}

	ordered := canonicalise(outgoing)

	status := StatusPlanned
	if oldRef != nil && oldRef.CommitID == newRef.CommitID {
		status = StatusNoRemoteChange
	}

	return &PublishPlan{
		SchemaVersion: SchemaVersion,
		Status:        status,
		Remote: Remote{
			Name: opts.Remote,
		},
		BookmarkMoves: []BookmarkMove{{
			Name: opts.Bookmark,
			Old:  oldRef,
			New:  newRef,
		}},
		Commits: ordered,
	}, snap, nil
}

// resolveChangeID returns the ChangeID for a given commit id by
// querying the local jj view pinned at opID.
func resolveChangeID(ctx context.Context, src Source, dir, opID, commitID string) (string, error) {
	commits, err := src.CommitsAt(ctx, dir, opID, commitID)
	if err != nil {
		return "", NewError(CodeJJQueryFailed, "change-id lookup failed", err)
	}
	for _, c := range commits {
		if c.CommitID == commitID {
			return c.ChangeID, nil
		}
	}
	return "", NewError(CodeJJQueryFailed,
		fmt.Sprintf("commit %s not materialised at op %s", commitID, opID), nil)
}

// canonicalise sorts the outgoing commit set in canonical order.
// We use lexicographic full-commit_id sort, which is a canonical
// total order on full hex strings and is deterministic. This is
// NOT ancestor-before-descendant ordering; see ACT-BJJ-PLAN01 §6.
func canonicalise(raw []jjadapter.CommitRef) []CommitRef {
	if len(raw) == 0 {
		return []CommitRef{}
	}

	seen := make(map[string]bool, len(raw))
	out := make([]jjadapter.CommitRef, 0, len(raw))
	for _, c := range raw {
		if seen[c.CommitID] {
			continue
		}
		seen[c.CommitID] = true
		out = append(out, c)
	}

	// Simple insertion sort for small slices.
	for i := 1; i < len(out); i++ {
		j := i
		for j > 0 && out[j-1].CommitID > out[j].CommitID {
			out[j-1], out[j] = out[j], out[j-1]
			j--
		}
	}

	res := make([]CommitRef, len(out))
	for i, c := range out {
		res[i] = fromJJCommit(c)
	}
	return res
}
