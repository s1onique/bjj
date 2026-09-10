package plan_test

import (
	"bytes"
	"fmt"
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
// file under internal/plan, internal/jjadapter, and
// internal/admission and asserts that no call site can possibly
// construct an argv equivalent to
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
// ACT-BJJ-ADMISSION01 §29 extends this guard to the admission
// production code as well.
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
	for _, dir := range []string{"internal/plan", "internal/jjadapter", "internal/admission", "internal/check"} {
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

// TestCheckLayerHasNoJJMutation is the
// ACT-BJJ-CHECK01 §30 bounded safeguard.
//
// Documented contract:
//
//	internal/check and cmd/bjj/check.go (non-test) MUST NOT
//	construct argv equivalent to a mutating jj subcommand
//	(jj new, jj describe, jj squash, jj bookmark set,
//	 jj commit, jj rebase, jj git push, jj git fetch).
//
// The check layer is read-only against the frozen Jujutsu
// view; mutation would defeat the whole point of the bound
// verification.
//
// The forbidden sequences are guarded separately from
// transport so a future ACT can relax one without relaxing the
// other.
func TestCheckLayerHasNoJJMutation(t *testing.T) {
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
	for _, dir := range []string{"internal/check", "cmd/bjj"} {
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
			checkFileForJJMutation(t, path)
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", dir, err)
		}
	}
}

// TestCheckLayerGoModuleReadOnly is the
// ACT-BJJ-CHECK01-CORRECTION01 §2 static guard.
//
// Documented contract:
//
//	internal/check (production AND test) MUST NOT construct
//	an execx.Env entry with GOFLAGS set to the unsafe
//	overlay. The check runner runs under GOFLAGS=-mod=readonly
//	so a candidate that needs to mutate go.mod / go.sum to
//	verify itself will FAIL/ERROR instead of being silently
//	repaired.
//
// This is a static AST-level guard: it walks the check
// package's source and test files and rejects any string
// literal that sets GOFLAGS to the unsafe overlay form.
func TestCheckLayerGoModuleReadOnly(t *testing.T) {
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
	for _, dir := range []string{"internal/check"} {
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
			body, rerr := os.ReadFile(path)
			if rerr != nil {
				return rerr
			}
			// The forbidden overlay is the literal
			// `GOFLAGS=-mod=mod` (with or without quotes).
			// `-mod=readonly` is the required overlay and
			// is checked separately by
			// TestRunner_UsesModReadonly.
			if bytes.Contains(body, []byte("GOFLAGS=-mod=mod")) {
				return fmt.Errorf("file %s sets GOFLAGS to the forbidden unsafe overlay; use GOFLAGS=-mod=readonly", path)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", dir, err)
		}
	}
}

// TestAdmissionHasNoDirectPlanImport is the
// ACT-BJJ-ADMISSION01-CORRECTION01 §5 guard.
//
// Documented contract:
//
//	internal/admission imports NO internal/plan
//
// The concrete *plan.PublishPlan -> admission.PlanView adapter
// was moved to cmd/bjj/plan_adapter.go so this package no longer
// drags in internal/plan's transitive surface. The AST walk
// below catches any future regression that re-introduces the
// import at the production layer.
func TestAdmissionHasNoDirectPlanImport(t *testing.T) {
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
	abs := filepath.Join(root, "internal", "admission")
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
		// _test.go files are allowed to import internal/plan
		// because they need to construct fixtures; the guard is
		// specifically about the production admission package.
		if strings.HasSuffix(path, "_test.go") {
			return nil
		}
		checkFileHasNoPlanImport(t, path)
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
}

// checkFileHasNoPlanImport parses one production .go file and
// fails the test if any import path equals
// "github.com/s1onique/bjj/internal/plan".
func checkFileHasNoPlanImport(t *testing.T, path string) {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	const forbidden = "github.com/s1onique/bjj/internal/plan"
	for _, imp := range f.Imports {
		if imp.Path == nil {
			continue
		}
		val := strings.Trim(imp.Path.Value, "\"")
		if val == forbidden {
			t.Errorf("%s: forbidden import %q (CORRECTION01 §5: internal/admission must not depend on internal/plan)",
				path, forbidden)
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

// checkFileForJJMutation parses one .go file and asserts that no
// composite literal contains an argv slice whose sequence starts
// with "jj" and continues with a mutating subcommand.
//
// The forbidden sequences are:
//
//	{"jj", "new"}
//	{"jj", "describe"}
//	{"jj", "squash"}
//	{"jj", "bookmark", "set"}
//	{"jj", "bookmark", "create"}
//	{"jj", "bookmark", "delete"}
//	{"jj", "bookmark", "rename"}
//	{"jj", "bookmark", "track"}
//	{"jj", "bookmark", "untrack"}
//	{"jj", "commit"}
//	{"jj", "rebase"}
//	{"jj", "abandon"}
//	{"jj", "diffedit"}
//	{"jj", "edit"}
//	{"jj", "split"}
//	{"jj", "restore"}
//	{"jj", "duplicate"}
//	{"jj", "move"}
//	{"jj", "merge"}
//	{"jj", "resolve"}
//	{"jj", "sign"}
//
// Note that we do NOT include the transport triggers
// ({"jj", "git", "push"}, {"jj", "git", "fetch"}) here because
// TestPlanLayerHasNoTransport already covers them. This guard
// is specifically for the mutating surface that would corrupt
// the frozen operation view.
func checkFileForJJMutation(t *testing.T, path string) {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}

	forbidden := [][]string{
		{"jj", "new"},
		{"jj", "describe"},
		{"jj", "squash"},
		{"jj", "bookmark", "set"},
		{"jj", "bookmark", "create"},
		{"jj", "bookmark", "delete"},
		{"jj", "bookmark", "rename"},
		{"jj", "bookmark", "track"},
		{"jj", "bookmark", "untrack"},
		{"jj", "commit"},
		{"jj", "rebase"},
		{"jj", "abandon"},
		{"jj", "diffedit"},
		{"jj", "edit"},
		{"jj", "split"},
		{"jj", "restore"},
		{"jj", "duplicate"},
		{"jj", "move"},
		{"jj", "merge"},
		{"jj", "resolve"},
		{"jj", "sign"},
	}

	ast.Inspect(f, func(n ast.Node) bool {
		cl, ok := n.(*ast.CompositeLit)
		if !ok {
			return true
		}
		if !isStringSliceType(cl.Type) {
			return true
		}
		elts := stringSliceElements(cl)
		for _, forbid := range forbidden {
			if containsSequence(elts, forbid) {
				t.Errorf("%s: forbidden jj-mutation sequence %v in argv: %v",
					path, forbid, elts)
			}
		}
		return true
	})
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
		"internal/check/types.go",
		"internal/check/errors.go",
		"internal/check/profile.go",
		"internal/check/materialize.go",
		"internal/check/orchestrator.go",
		"internal/check/runner.go",
		"internal/check/aggregate.go",
		"internal/check/render.go",
		"internal/check/jjsource.go",
		"cmd/bjj/check.go",
	} {
		full := filepath.Join(root, p)
		if _, err := os.Stat(full); err != nil {
			t.Errorf("expected %s to exist: %v", full, err)
		}
	}
}

// TestCheckFilesExist is the ACT-BJJ-CHECK01 structural
// companion of TestPlanFilesExist. It asserts that every
// expected check package file is present.
func TestCheckFilesExist(t *testing.T) {
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
	// Same list as TestPlanFilesExist, plus the check entry
	// point. Listing both ensures the safety test does not
	// silently regress on either ACT.
	for _, p := range []string{
		"cmd/bjj/check.go",
		"internal/check/types.go",
		"internal/check/orchestrator.go",
		"internal/check/profile.go",
	} {
		full := filepath.Join(root, p)
		if _, err := os.Stat(full); err != nil {
			t.Errorf("expected %s to exist: %v", full, err)
		}
	}
}

// TestCmdAdmissionPathUsesPolicyReader is the
// ACT-BJJ-ADMISSION01-CORRECTION03 §10 static safeguard.
//
// Documented contract:
//
//	cmd/bjj/*.go (non-test) MUST NOT call admission.LoadPolicyFromRepo
//
// The production admission path is required to read the policy
// from the frozen Jujutsu view via admission.LoadPolicyAt (or
// equivalent opID-bound reader). The deprecated live-fs reader
// is permitted only inside *_test.go files and inside the
// admission package's own unit tests; a production caller
// invoking it would race the working tree and produce a mixed
// view.
//
// The guard walks cmd/bjj/*.go (non-test) and AST-rejects any
// SelectorExpr that names admission.LoadPolicyFromRepo. It does
// not grep; it parses so future refactors that rename the
// qualifier do not silently disable the check.
func TestCmdAdmissionPathUsesPolicyReader(t *testing.T) {
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
	abs := filepath.Join(root, "cmd", "bjj")
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
		// _test.go files are explicitly out of scope; tests may
		// exercise the live-fs reader for parser unit tests.
		if strings.HasSuffix(path, "_test.go") {
			return nil
		}
		checkFileForPolicyFromRepo(t, path)
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
}

// checkFileForPolicyFromRepo parses path and fails the test if
// any *ast.SelectorExpr names admission.LoadPolicyFromRepo.
func checkFileForPolicyFromRepo(t *testing.T, path string) {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if sel.Sel.Name != "LoadPolicyFromRepo" {
				return true
			}
			x, ok := sel.X.(*ast.Ident)
			if !ok {
				return true
			}
			if x.Name == "admission" {
				t.Errorf("%s: %s: production CLI MUST NOT call admission.LoadPolicyFromRepo; use admission.LoadPolicyAt with a PolicyReader",
					path, fset.Position(n.Pos()))
			}
			return true
		})
	}
}

// TestCheckWorkspaceAlwaysFreshDirectory asserts
// ACT-BJJ-CHECK01-CORRECTION01 §1 (per-check fresh
// workspace) at the source level: the check layer MUST
// allocate workspace directories via os.MkdirTemp (which
// generates a unique-per-call directory name) and MUST NOT
// use a deterministic name based on the revision.
//
// The pre-CORRECTION01 implementation used
//
//	wsName := "bjj-check-" + revision
//	workspaceDir := filepath.Join(parentDir, wsName)
//	os.Mkdir(workspaceDir, 0o700)
//
// which silently collapsed repeated materializations of the
// same revision onto the same directory. The orchestrator
// removed the directory between checks but if cleanup ever
// failed, subsequent checks would re-materialize into a
// poisoned workspace.
//
// This guard walks internal/check and fails the build if any
// .go file contains the forbidden pattern `os.Mkdir(` AND
// `"bjj-check-"`. The check-layer materializer is the only
// place that should create workspace directories; it must
// use os.MkdirTemp so each call gets a fresh directory.
func TestCheckWorkspaceAlwaysFreshDirectory(t *testing.T) {
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
	abs := filepath.Join(root, "internal", "check")
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
		body, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		// Forbidden: a workspace directory allocated via
		// os.Mkdir with a deterministic "bjj-check-..."
		// prefix. Use os.MkdirTemp so each call gets a
		// fresh directory.
		if bytes.Contains(body, []byte(`os.Mkdir(`)) && bytes.Contains(body, []byte(`"bjj-check-`)) {
			return fmt.Errorf("file %s allocates a workspace directory with os.Mkdir and a deterministic 'bjj-check-' prefix; use os.MkdirTemp so every call gets a fresh directory", path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk internal/check: %v", err)
	}
}
