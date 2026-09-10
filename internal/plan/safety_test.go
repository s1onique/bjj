package plan_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPlanLayerHasNoTransport is the bounded static safeguard
// required by ACT-BJJ-PLAN01 §26. It walks the AST of every .go
// file under internal/plan and internal/jjadapter and asserts that
// no call site can possibly construct an argv equivalent to
//
//	"git", "push"
//	"git", "fetch"
//	"jj", "git", "push"
//	"jj", "git", "fetch"
//
// The check operates on the parsed Go AST rather than raw string
// matching so it cannot be defeated by trivial reformatting or
// concatenation.
//
// If a future ACT legitimately needs one of these invocations, this
// test is the gate to update.
func TestPlanLayerHasNoTransport(t *testing.T) {
	root, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(root, "go.mod")); err == nil {
			break
		}
		parent := filepath.Dir(root)
		if parent == root {
			t.Fatalf("could not find project root above %s", root)
		}
		root = parent
	}
	for _, dir := range []string{"internal/plan", "internal/jjadapter"} {
		abs := filepath.Join(root, dir)
		err = filepath.WalkDir(abs, func(path string, d fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if d.IsDir() {
				return nil
			}
			if !strings.HasSuffix(path, ".go") {
				return nil
			}
			if strings.HasSuffix(path, "_test.go") {
				return nil
			}
			checkFileForTransport(t, path)
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", dir, err)
		}
	}
}

// checkFileForTransport parses one .go file and asserts that no
// composite literal or call expression contains an argv slice whose
// sequence contains a transport-triggering token pair.
func checkFileForTransport(t *testing.T, path string) {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}

	// Transport triggers we forbid.
	forbidden := [][]string{
		{"git", "push"},
		{"git", "fetch"},
		{"jj", "git", "push"},
		{"jj", "git", "fetch"},
	}

	ast.Inspect(f, func(n ast.Node) bool {
		cl, ok := n.(*ast.CompositeLit)
		if !ok {
			return true
		}
		// Only inspect []string literals (or untyped string slices).
		if !isStringSliceType(cl.Type) {
			return true
		}
		elts := stringSliceElements(cl)
		for _, forbid := range forbidden {
			if containsSequence(elts, forbid) {
				t.Errorf("%s: forbidden transport sequence %v in argv: %v",
					path, forbid, elts)
			}
		}
		return true
	})
}

func isStringSliceType(e ast.Expr) bool {
	at, ok := e.(*ast.ArrayType)
	if !ok {
		return false
	}
	if at.Len != nil {
		return false
	}
	// Element type should be "string" (possibly via *ast.Ident).
	id, ok := at.Elt.(*ast.Ident)
	if !ok {
		return false
	}
	return id.Name == "string"
}

func stringSliceElements(cl *ast.CompositeLit) []string {
	out := []string{}
	for _, e := range cl.Elts {
		bl, ok := e.(*ast.BasicLit)
		if !ok || bl.Kind != token.STRING {
			return nil
		}
		// Strip surrounding quotes.
		s := bl.Value
		if len(s) >= 2 && (s[0] == '"' || s[0] == '`') {
			s = s[1 : len(s)-1]
		}
		out = append(out, s)
	}
	return out
}

func containsSequence(haystack, needle []string) bool {
	if len(needle) == 0 {
		return true
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		match := true
		for j, n := range needle {
			if haystack[i+j] != n {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

// TestPlanFilesExist is a tiny structural assertion: the PLAN01
// packages must exist on disk.
func TestPlanFilesExist(t *testing.T) {
	root, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(root, "go.mod")); err == nil {
			break
		}
		parent := filepath.Dir(root)
		if parent == root {
			t.Fatalf("could not find project root above %s", root)
		}
		root = parent
	}
	for _, p := range []string{
		"internal/plan/types.go",
		"internal/plan/errors.go",
		"internal/plan/resolve.go",
		"internal/plan/render.go",
		"internal/plan/source.go",
		"internal/jjadapter/jjadapter.go",
	} {
		full := filepath.Join(root, p)
		if _, err := os.Stat(full); err != nil {
			t.Errorf("expected %s to exist: %v", full, err)
		}
	}
}
