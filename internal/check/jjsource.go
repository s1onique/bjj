package check

import (
	"context"

	"github.com/s1onique/bjj/internal/jjadapter"
)

// JJFileSource is the production FileSource. It delegates to the
// bounded jjadapter.Adapter for both `file list` and `file show`
// invocations.
//
// JJFileSource deliberately does NOT define an opinion on
// transport. It is a tree reader, period.
//
// CHECK01-CORRECTION01: the list-side method now returns
// TreeEntry values so the materializer can preserve the
// executable bit and reject unsupported kinds.
type JJFileSource struct {
	Adapter *jjadapter.Adapter
}

// NewJJFileSource returns a JJFileSource backed by a.
func NewJJFileSource(a *jjadapter.Adapter) *JJFileSource {
	return &JJFileSource{Adapter: a}
}

// ListTreeEntries implements FileSource. It adapts the
// adapter's typed TreeEntry values into the check package's
// own (structurally compatible) TreeEntry.
func (s *JJFileSource) ListTreeEntries(ctx context.Context, dir, opID, revision string) ([]TreeEntry, error) {
	if s == nil || s.Adapter == nil {
		return nil, errJJAdapterUnset
	}
	raw, err := s.Adapter.ListTreeEntries(ctx, dir, opID, revision)
	if err != nil {
		return nil, err
	}
	out := make([]TreeEntry, len(raw))
	for i, e := range raw {
		out[i] = TreeEntry{
			Path:       e.Path,
			Kind:       EntryKind(e.Kind),
			Executable: e.Executable,
		}
	}
	return out, nil
}

// ShowFile implements FileSource.
func (s *JJFileSource) ShowFile(ctx context.Context, dir, opID, revision, path string) ([]byte, error) {
	if s == nil || s.Adapter == nil {
		return nil, errJJAdapterUnset
	}
	return s.Adapter.ShowFile(ctx, dir, opID, revision, path)
}

// errJJAdapterUnset is the package-private error returned by
// JJFileSource methods when the underlying adapter is nil. It is
// never surfaced to callers because every JJFileSource is
// constructed via NewJJFileSource, which sets the adapter; this
// branch is purely defensive.
var errJJAdapterUnset = &jjAdapterUnsetError{}

type jjAdapterUnsetError struct{}

func (e *jjAdapterUnsetError) Error() string {
	return "check: JJFileSource has nil adapter"
}
