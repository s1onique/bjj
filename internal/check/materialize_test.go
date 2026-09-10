package check

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// fakeFileSource is a deterministic FileSource for unit tests.
type fakeFileSource struct {
	Files      map[string][]byte
	Executable map[string]bool
	ListErr    error
	ShowErr    error
	Calls      []string
}

func (f *fakeFileSource) ListTreeEntries(ctx context.Context, dir, opID, revision string) ([]TreeEntry, error) {
	f.Calls = append(f.Calls, "list:"+opID+":"+revision)
	if f.ListErr != nil {
		return nil, f.ListErr
	}
	out := make([]TreeEntry, 0, len(f.Files))
	for k := range f.Files {
		exec := false
		if f.Executable != nil {
			exec = f.Executable[k]
		}
		out = append(out, TreeEntry{Path: k, Kind: KindFile, Executable: exec})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

func (f *fakeFileSource) ShowFile(ctx context.Context, dir, opID, revision, path string) ([]byte, error) {
	f.Calls = append(f.Calls, "show:"+opID+":"+revision+":"+path)
	if f.ShowErr != nil {
		return nil, f.ShowErr
	}
	body, ok := f.Files[path]
	if !ok {
		return nil, errors.New("fake: file not found: " + path)
	}
	return body, nil
}

// TestMaterialize_RejectsEmptyInputs asserts the
// pre-flight guards of the materializer (verdict §5 / §6).
func TestMaterialize_RejectsEmptyInputs(t *testing.T) {
	fs := &fakeFileSource{}
	m := NewJJMaterializer(fs)
	ctx := context.Background()
	parent := t.TempDir()

	cases := []struct {
		name string
		dir  string
		op   string
		rev  string
		want ErrorCode
	}{
		{"empty dir", "", "op", "rev", CodeMaterializationFailed},
		{"empty opID", "/some/dir", "", "rev", CodeMaterializationFailed},
		{"empty revision", "/some/dir", "op", "", CodeMaterializationFailed},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, _, err := m.Materialize(ctx, parent, c.dir, c.op, c.rev)
			var ce *Error
			if !errors.As(err, &ce) {
				t.Fatalf("expected *Error, got %v", err)
			}
			if ce.Code != c.want {
				t.Fatalf("got code %q want %q", ce.Code, c.want)
			}
		})
	}
}

// TestMaterialize_RejectsEmptyFileList asserts that an empty
// revision tree is rejected (not silently allowed).
func TestMaterialize_RejectsEmptyFileList(t *testing.T) {
	fs := &fakeFileSource{Files: map[string][]byte{}}
	m := NewJJMaterializer(fs)
	parent := t.TempDir()
	_, _, err := m.Materialize(context.Background(), parent, "/d", "op", "rev")
	var ce *Error
	if !errors.As(err, &ce) {
		t.Fatalf("expected *Error, got %v", err)
	}
	if ce.Code != CodeMaterializationFailed {
		t.Fatalf("got code %q want %q", ce.Code, CodeMaterializationFailed)
	}
}

// TestMaterialize_WritesAllFilesAndManifest asserts the happy
// path: every file in the fake source ends up on disk with the
// same bytes and is recorded in the manifest.
func TestMaterialize_WritesAllFilesAndManifest(t *testing.T) {
	fs := &fakeFileSource{
		Files: map[string][]byte{
			"a.txt":     []byte("hello"),
			"b/c.txt":   []byte("world"),
			"b/d/e.txt": []byte("!"),
		},
	}
	m := NewJJMaterializer(fs)
	parent := t.TempDir()
	manifest, wsDir, err := m.Materialize(context.Background(), parent, "/d", "op", "rev")
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(wsDir) })

	if got := len(manifest.Files); got != 3 {
		t.Fatalf("manifest entries: got %d want 3", got)
	}
	for _, e := range manifest.Files {
		body, err := os.ReadFile(filepath.Join(wsDir, filepath.FromSlash(e.Path)))
		if err != nil {
			t.Fatalf("read %s: %v", e.Path, err)
		}
		if string(body) != string(fs.Files[e.Path]) {
			t.Fatalf("body mismatch for %s: got %q want %q", e.Path, body, fs.Files[e.Path])
		}
	}
	for i := 1; i < len(manifest.Files); i++ {
		if manifest.Files[i-1].Path > manifest.Files[i].Path {
			t.Fatalf("manifest not sorted at %d: %v", i, []string{manifest.Files[i-1].Path, manifest.Files[i].Path})
		}
	}
}

// TestMaterialize_PathEscapeIsHardRejected asserts that a path
// starting with ".." is hard-rejected by the materializer.
func TestMaterialize_PathEscapeIsHardRejected(t *testing.T) {
	fs := &fakeFileSource{
		Files: map[string][]byte{
			"../escape.txt": []byte("nope"),
		},
	}
	m := NewJJMaterializer(fs)
	parent := t.TempDir()
	_, _, err := m.Materialize(context.Background(), parent, "/d", "op", "rev")
	if err == nil {
		t.Fatalf("expected error for path escape, got nil")
	}
	var ce *Error
	if !errors.As(err, &ce) {
		t.Fatalf("expected *Error, got %v", err)
	}
	if ce.Code != CodeMaterializationFailed {
		t.Fatalf("got code %q want %q", ce.Code, CodeMaterializationFailed)
	}
}

// TestManifest_ManifestSHAIsDeterministic asserts that two
// manifests with the same contents produce the same SHA even
// when their entries are in different orders.
func TestManifest_ManifestSHAIsDeterministic(t *testing.T) {
	mk := func() *Manifest {
		return &Manifest{
			Files: []FileEntry{
				{Path: "b", Kind: KindFile, ContentSHA256: "B", Mode: 0o644, Size: 1},
				{Path: "a", Kind: KindFile, ContentSHA256: "A", Mode: 0o644, Size: 1},
				{Path: "c", Kind: KindFile, ContentSHA256: "C", Mode: 0o644, Size: 1},
			},
		}
	}
	m1 := mk()
	m2 := mk()
	for i, j := 0, len(m2.Files)-1; i < j; i, j = i+1, j-1 {
		m2.Files[i], m2.Files[j] = m2.Files[j], m2.Files[i]
	}
	if m1.ManifestSHA() != m2.ManifestSHA() {
		t.Fatalf("manifest SHA differs across orderings:\n%s\nvs\n%s", m1.ManifestSHA(), m2.ManifestSHA())
	}
}

// TestSanitizeForPath_DefensiveDefaults covers the edge cases
// of sanitizeForPath.
func TestSanitizeForPath_DefensiveDefaults(t *testing.T) {
	if sanitizeForPath("") != "empty" {
		t.Fatalf("empty input should yield 'empty'")
	}
	if strings.ContainsAny(sanitizeForPath("../etc/passwd"), "/") {
		t.Fatalf("sanitize should strip slashes: %q", sanitizeForPath("../etc/passwd"))
	}
	s := sanitizeForPath("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	if len(s) > 33 {
		t.Fatalf("sanitize should truncate, got len=%d", len(s))
	}
}
