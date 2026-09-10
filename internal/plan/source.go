package plan

import (
	"context"

	"github.com/s1onique/bjj/internal/jjadapter"
)

// JJSource is the production Source implementation.
//
// It honors the single-view consistency contract: Snapshot captures
// an operation id; every subsequent CommitsAt call uses that EXACT
// operation id, never a fresh pin. The resolver owns the opID and
// threads it through.
type JJSource struct {
	a *jjadapter.Adapter
}

// NewJJSource wraps the given jjadapter.Adapter as a plan.Source.
func NewJJSource(a *jjadapter.Adapter) *JJSource {
	if a == nil {
		a = jjadapter.New()
	}
	return &JJSource{a: a}
}

// Snapshot implements Source. It pins the operation view exactly
// once, then fetches bookmarks and remotes at that view.
func (s *JJSource) Snapshot(ctx context.Context, dir string) (jjadapter.Snapshot, error) {
	opID, err := s.a.PinOperation(ctx, dir)
	if err != nil {
		return jjadapter.Snapshot{}, err
	}

	remotes, err := s.a.ListRemotes(ctx, dir, opID)
	if err != nil {
		return jjadapter.Snapshot{}, err
	}

	bookmarks, err := s.a.ListBookmarks(ctx, dir, opID)
	if err != nil {
		return jjadapter.Snapshot{}, err
	}

	return jjadapter.Snapshot{
		OperationID: opID,
		Remotes:     remotes,
		Bookmarks:   bookmarks,
	}, nil
}

// CommitsAt implements Source. It runs `jj log --no-graph -r <revset>`
// pinned to the EXACT opID supplied by the resolver. It MUST NOT
// re-pin; doing so would silently violate single-view consistency.
//
// The opID parameter is required (non-empty); the adapter is told
// --at-op=<opID> for every invocation. If the supplied opID becomes
// stale between observations (because the underlying repository
// changed), the resolver is responsible for catching that at
// snapshot time and re-Snapshotting.
func (s *JJSource) CommitsAt(ctx context.Context, dir, opID, revset string) ([]jjadapter.CommitRef, error) {
	if opID == "" {
		return nil, &Error{
			Code:    CodeInconsistentView,
			Message: "CommitsAt called with empty opID; refusing to re-pin",
		}
	}
	return s.a.ListCommits(ctx, dir, opID, revset)
}
