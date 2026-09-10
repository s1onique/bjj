package check

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// FileEntry is one materialized file. Path is relative to the
// workspace root and uses forward-slash separators in canonical
// (sorted) order.
//
// Kind + Executable are the semantic tree metadata Jujutsu
// carries about the entry; CHECK01-CORRECTION01 makes them
// first-class so chmod tampering and post-materialize mutation
// can be detected at pre-exec re-verify time.
//
// Mode is the file mode the materializer actually wrote
// (0o644 for non-executable regular files, 0o755 for executable
// regular files). It is derived from (Kind, Executable), not
// independently authorable.
//
// ContentSHA256 is the lowercase hex SHA-256 of the file's raw
// bytes at materialization time. This is NOT a SubjectDigest; it
// is a local proof that the materialized workspace matches the
// re-listing at pre-exec integrity verification time.
//
// Symlink / git-submodule / conflict entries are NOT enumerated
// in the manifest; the materializer fails closed with
// CodeUnsupportedTreeEntry when it encounters one.
type FileEntry struct {
	Path          string    `json:"path"`
	Kind          EntryKind `json:"kind"`
	Executable    bool      `json:"executable"`
	Mode          uint32    `json:"mode"`
	Size          int64     `json:"size"`
	ContentSHA256 string    `json:"sha256"`
}

// Manifest is the canonical enumeration of a materialized
// workspace.
//
// Entries are sorted by Path in ascending lexicographic order.
// The same Files slice always produces the same Manifest SHA.
type Manifest struct {
	Files []FileEntry
}

// ManifestSHA returns the SHA-256 over the canonical
// representation of m. Entries are processed in lexicographic
// Path order; missing orderings are detected by the comparator.
//
// The hash covers (Path, Kind, Executable, Mode, ContentSHA256)
// for every entry so two manifests with the same paths and
// contents but different kinds or executable bits produce
// distinct hashes (per CHECK01-CORRECTION01 §3 / §4).
func (m Manifest) ManifestSHA() string {
	cp := make([]FileEntry, len(m.Files))
	copy(cp, m.Files)
	sort.Slice(cp, func(i, j int) bool { return cp[i].Path < cp[j].Path })

	h := sha256.New()
	for _, e := range cp {
		h.Write([]byte(e.Path))
		h.Write([]byte{0})
		h.Write([]byte(e.Kind))
		h.Write([]byte{0})
		var flagBuf [1]byte
		if e.Executable {
			flagBuf[0] = 1
		}
		h.Write(flagBuf[:])
		h.Write([]byte{0})
		h.Write([]byte(e.ContentSHA256))
		h.Write([]byte{0})
		var modeBuf [4]byte
		modeBuf[0] = byte(e.Mode >> 24)
		modeBuf[1] = byte(e.Mode >> 16)
		modeBuf[2] = byte(e.Mode >> 8)
		modeBuf[3] = byte(e.Mode)
		h.Write(modeBuf[:])
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

// Materializer produces a disposable, verifiable workspace that
// contains the frozen candidate tree.
//
// Implementations MUST:
//
//   - read source bytes from the operation-pinned view at opID;
//     the live working copy is FORBIDDEN
//   - return a Manifest enumerating every materialized file
//   - reject empty revisions (a workspace with zero files is not
//     a candidate tree)
//   - return CodeMaterializationFailed on any error
//
// The default implementation is JJMaterializer.
type Materializer interface {
	// Materialize produces a disposable workspace under
	// parentDir (a fresh sub-directory is created) containing
	// the tree of revision as visible at opID. The returned
	// Manifest describes every file actually written.
	//
	// parentDir MUST be the absolute path of an existing
	// directory; it is the caller's responsibility to clean up
	// the workspace after the check run.
	Materialize(ctx context.Context, parentDir, dir, opID, revision string) (*Manifest, string, error)
}

// JJMaterializer is the production Materializer. It uses
// `jj --at-op=<opID> file list -r <rev>` to enumerate files and
// `jj --at-op=<opID> file show -r <rev> <path>` to fetch each
// file's bytes — both via the supplied FileSource.
//
// Per ACT-BJJ-CHECK01 §6 strategy #2 is preferred in jj 0.41.0
// because `jj archive` is not part of the 0.41.0 CLI surface.
//
// JJMaterializer has no opinion on transport. It is a tree
// reader, period. The injected FileSource encapsulates the actual
// jj invocation; a fake FileSource can be plugged in for tests.
type JJMaterializer struct {
	// FS is the file-source used for `file list` and
	// `file show` calls. Required.
	FS FileSource
}

// FileSource abstracts the bounded jj file queries the
// materializer performs.
//
// JJSource is the production implementation; tests use
// FakeFileSource to inject deterministic in-memory trees.
//
// CHECK01-CORRECTION01: the list-side method now returns
// TreeEntry values (path, kind, executable) instead of bare
// paths, so the materializer can preserve the executable bit
// and refuse symlink / git-submodule / conflict entries.
type FileSource interface {
	// ListTreeEntries returns the canonical list of tree
	// entries in revision as visible at opID. The slice MUST be
	// sorted in lexicographic Path order; the materializer
	// relies on this for deterministic Manifest hashing.
	//
	// Implementations MUST error (not return empty) when the
	// jj invocation fails or produces unparseable output.
	ListTreeEntries(ctx context.Context, dir, opID, revision string) ([]TreeEntry, error)

	// ShowFile returns the bytes of path in revision as visible
	// at opID. Returns an error (NOT a "file absent" return) on
	// any jj invocation or filesystem failure.
	ShowFile(ctx context.Context, dir, opID, revision, path string) ([]byte, error)
}

// TreeEntry is the typed view of one entry returned by
// `jj file list`. It mirrors the underlying Jujutsu TreeEntry
// template: a path, a kind (file | symlink | git-submodule |
// conflict | tree), and an executable bit (only meaningful for
// KindFile).
//
// TreeEntry values are pure data and are immutable after
// construction.
type TreeEntry struct {
	// Path is the repo-relative path (forward-slash separated).
	Path string

	// Kind is the semantic kind of the entry.
	Kind EntryKind

	// Executable is true when the entry is a regular file with
	// the executable bit set. False for non-file kinds.
	Executable bool
}

// NewJJMaterializer returns a JJMaterializer backed by fs.
func NewJJMaterializer(fs FileSource) *JJMaterializer {
	return &JJMaterializer{FS: fs}
}

// Materialize implements Materializer.
func (m *JJMaterializer) Materialize(ctx context.Context, parentDir, dir, opID, revision string) (*Manifest, string, error) {
	if m == nil || m.FS == nil {
		return nil, "", NewError(CodeMaterializationFailed,
			"check: materializer not initialised (no FileSource)", nil)
	}
	if parentDir == "" {
		return nil, "", NewError(CodeMaterializationFailed,
			"check: materializer: empty parentDir", nil)
	}
	if dir == "" {
		return nil, "", NewError(CodeMaterializationFailed,
			"check: materializer: empty repo dir", nil)
	}
	if opID == "" {
		return nil, "", NewError(CodeMaterializationFailed,
			"check: materializer: empty opID; refusing to read from the live operation", nil)
	}
	if revision == "" {
		return nil, "", NewError(CodeMaterializationFailed,
			"check: materializer: empty revision; refusing to read from the live workspace", nil)
	}

	files, err := m.FS.ListTreeEntries(ctx, dir, opID, revision)
	if err != nil {
		return nil, "", NewError(CodeMaterializationFailed,
			fmt.Sprintf("check: materializer: list tree entries: %v", err), err)
	}
	if len(files) == 0 {
		return nil, "", NewError(CodeMaterializationFailed,
			"check: materializer: empty file list; refusing to materialize empty tree", nil)
	}

	// Per ACT-BJJ-CHECK01-CORRECTION01 §1 (per-check fresh
	// workspace) we MUST allocate a fresh sub-directory on
	// every call. We previously used a deterministic
	// "bjj-check-<revision>" name which silently collapsed
	// repeated materializations of the same revision onto
	// the same directory — the orchestrator removes that
	// directory between checks, but if the orchestrator
	// ever fails to clean up (or if a check exits before
	// the orchestrator's RemoveAll), subsequent checks
	// would re-materialize INTO a directory that already
	// contains poisoned bytes from a previous run.
	//
	// MkdirTemp guarantees a fresh empty directory per
	// call. The revision is still present in the suffix
	// for diagnosability, but uniqueness is sourced from
	// the kernel's random number generator.
	wsName := "bjj-check-" + sanitizeForPath(revision) + "-"
	workspaceDir, err := os.MkdirTemp(parentDir, wsName)
	if err != nil {
		return nil, "", NewError(CodeMaterializationFailed,
			fmt.Sprintf("check: materializer: mkdir workspace: %v", err), err)
	}

	manifest := &Manifest{Files: make([]FileEntry, 0, len(files))}
	for _, entry := range files {
		if err := ctx.Err(); err != nil {
			_ = os.RemoveAll(workspaceDir)
			return nil, "", NewError(CodeMaterializationFailed,
				fmt.Sprintf("check: materializer: context cancelled: %v", err), err)
		}

		// Fail closed on tree kinds CHECK01 does not support.
		// Silently coercing a symlink or submodule into a
		// regular 0644 file would falsify the durable claim
		// that the workspace is an exact materialization of
		// the frozen tree.
		switch entry.Kind {
		case KindFile:
			// supported
		case KindSymlink, KindGitSubmodule, KindConflict, KindTree:
			_ = os.RemoveAll(workspaceDir)
			return nil, "", NewError(CodeUnsupportedTreeEntry,
				fmt.Sprintf("check: materializer: entry %q has unsupported kind %q; refusing to coerce",
					entry.Path, entry.Kind), nil)
		default:
			_ = os.RemoveAll(workspaceDir)
			return nil, "", NewError(CodeUnsupportedTreeEntry,
				fmt.Sprintf("check: materializer: entry %q has unknown kind %q",
					entry.Path, entry.Kind), nil)
		}

		cleanRel := filepath.Clean(entry.Path)
		if strings.HasPrefix(cleanRel, "..") || strings.Contains(cleanRel, "\x00") {
			_ = os.RemoveAll(workspaceDir)
			return nil, "", NewError(CodeMaterializationFailed,
				fmt.Sprintf("check: materializer: unsafe path %q", entry.Path), nil)
		}
		abs := filepath.Join(workspaceDir, cleanRel)
		if !strings.HasPrefix(abs, workspaceDir+string(filepath.Separator)) && abs != workspaceDir {
			_ = os.RemoveAll(workspaceDir)
			return nil, "", NewError(CodeMaterializationFailed,
				fmt.Sprintf("check: materializer: path escape %q", entry.Path), nil)
		}
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			_ = os.RemoveAll(workspaceDir)
			return nil, "", NewError(CodeMaterializationFailed,
				fmt.Sprintf("check: materializer: mkdir parent for %q: %v", entry.Path, err), err)
		}
		body, err := m.FS.ShowFile(ctx, dir, opID, revision, entry.Path)
		if err != nil {
			_ = os.RemoveAll(workspaceDir)
			return nil, "", NewError(CodeMaterializationFailed,
				fmt.Sprintf("check: materializer: show %q: %v", entry.Path, err), err)
		}

		// Derive Mode from (Kind, Executable). For CHECK01-v1
		// the only recognised kind is "file"; executable bit
		// toggles 0o644 <-> 0o755.
		mode := uint32(0o644)
		if entry.Executable {
			mode = 0o755
		}

		sum := sha256.Sum256(body)
		manifest.Files = append(manifest.Files, FileEntry{
			Path:          filepath.ToSlash(cleanRel),
			Kind:          entry.Kind,
			Executable:    entry.Executable,
			Mode:          mode,
			Size:          int64(len(body)),
			ContentSHA256: hex.EncodeToString(sum[:]),
		})
		if err := os.WriteFile(abs, body, os.FileMode(mode)); err != nil {
			_ = os.RemoveAll(workspaceDir)
			return nil, "", NewError(CodeMaterializationFailed,
				fmt.Sprintf("check: materializer: write %q: %v", entry.Path, err), err)
		}
	}
	sort.Slice(manifest.Files, func(i, j int) bool { return manifest.Files[i].Path < manifest.Files[j].Path })
	return manifest, workspaceDir, nil
}

// sanitizeForPath strips characters that are unsafe in a
// filesystem path component. The result is meant for
// human-debuggability, not security; the directory is created
// 0700 and removed after the run.
func sanitizeForPath(s string) string {
	if s == "" {
		return "empty"
	}
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z',
			r >= 'A' && r <= 'Z',
			r >= '0' && r <= '9',
			r == '-' || r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
		if b.Len() > 32 {
			break
		}
	}
	out := b.String()
	if out == "" {
		return "empty"
	}
	return out
}
