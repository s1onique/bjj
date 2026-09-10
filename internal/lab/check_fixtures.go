package lab

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// CheckCandidateHandle identifies a seeded candidate tree in
// the lab's JJClient. ACT-BJJ-CHECK01 lab tests obtain a
// handle via one of the SeedCleanGoCandidate /
// SeedGofmtBadCandidate / etc helpers and then resolve it
// through the candidate's commit id.
type CheckCandidateHandle struct {
	// Description is the commit description used during
	// seeding. Useful for diagnostics in test failures.
	Description string
	// CommitID is the full jj commit id of the seeded
	// candidate change.
	CommitID string
	// ChangeID is the full jj change id of the seeded
	// candidate change.
	ChangeID string
	// WorkspacePath is the absolute path of the jj client's
	// live working directory at the time of seeding.
	WorkspacePath string
}

// SeedCleanGoCandidate creates a "clean Go candidate": a new
// commit on top of `main` whose tree contains a minimal
// go.mod and a single Go source file that compiles cleanly and
// passes `go vet`. The candidate commit is returned as a
// CheckCandidateHandle.
//
// The returned commit has a non-empty description
// (admission.RequireDescription is true by default, so
// admission would deny an empty-description commit).
func (l *Lab) SeedCleanGoCandidate(ctx context.Context) (*CheckCandidateHandle, error) {
	return l.seedGoCandidate(ctx, "clean", func(dir string) error {
		return writeMinimalGoModule(dir, "clean")
	})
}

// SeedGofmtBadCandidate creates a candidate whose Go source is
// NOT gofmt-clean. The expected check outcome is
// gofmt FAIL while build/vet/test still PASS.
//
// `gofmt -l .` reports any file that is not gofmt-clean, so we
// deliberately write a file with bad indentation while keeping
// the file syntactically valid (so `go build` etc still pass).
func (l *Lab) SeedGofmtBadCandidate(ctx context.Context) (*CheckCandidateHandle, error) {
	return l.seedGoCandidate(ctx, "gofmt-bad", func(dir string) error {
		if err := writeMinimalGoModule(dir, "gofmt-bad"); err != nil {
			return err
		}
		// Overwrite main.go with spaces-only indentation
		// so gofmt -l . will report it, while keeping the
		// file syntactically valid so build/vet/test pass.
		bad := "package main\n\nfunc Greet() string {\n    return \"hi\"\n}\n\nfunc main() {\n    println(Greet())\n}\n"
		return os.WriteFile(filepath.Join(dir, "main.go"), []byte(bad), 0o644)
	})
}

// SeedGoTestFailingCandidate creates a candidate whose Go test
// suite fails an assertion. Expected outcomes: build PASS,
// vet PASS, test FAIL.
func (l *Lab) SeedGoTestFailingCandidate(ctx context.Context) (*CheckCandidateHandle, error) {
	return l.seedGoCandidate(ctx, "go-test-failing", func(dir string) error {
		if err := writeMinimalGoModule(dir, "go-test-failing"); err != nil {
			return err
		}
		test := "package main\n\nimport \"testing\"\n\nfunc TestAlwaysFails(t *testing.T) {\n\tt.Fatal(\"intentional failure for check fixture\")\n}\n"
		return os.WriteFile(filepath.Join(dir, "main_test.go"), []byte(test), 0o644)
	})
}

// SeedGoBuildFailingCandidate creates a candidate whose Go
// source does not compile. Expected outcomes: build FAIL.
func (l *Lab) SeedGoBuildFailingCandidate(ctx context.Context) (*CheckCandidateHandle, error) {
	return l.seedGoCandidate(ctx, "go-build-failing", func(dir string) error {
		if err := writeMinimalGoModule(dir, "go-build-failing"); err != nil {
			return err
		}
		bad := "package main\n\nfunc Greet() string {\n\treturn \"hi\n}\n"
		return os.WriteFile(filepath.Join(dir, "main.go"), []byte(bad), 0o644)
	})
}

// seedGoCandidate is the shared scaffolding for the four
// lab check fixtures. It:
//
//  1. Writes a candidate tree to the jj client working copy
//     using the supplied builder callback.
//  2. Describes the change.
//  3. Returns a typed handle carrying the new commit id.
func (l *Lab) seedGoCandidate(ctx context.Context, description string, build func(dir string) error) (*CheckCandidateHandle, error) {
	if l == nil || l.JJClient == "" {
		return nil, errors.New("lab: seedGoCandidate: lab not set up")
	}
	if err := build(l.JJClient); err != nil {
		return nil, fmt.Errorf("lab: seedGoCandidate(%s): %w", description, err)
	}
	if err := l.runJJ(ctx, l.JJClient, []string{"describe", "-m", "bjj-check: " + description}); err != nil {
		return nil, fmt.Errorf("lab: seedGoCandidate: describe: %w", err)
	}
	commitID, err := l.jjOutput(ctx, l.JJClient, []string{
		"log", "--no-graph", "-r", "@", "-T", "commit_id",
	})
	if err != nil {
		return nil, fmt.Errorf("lab: seedGoCandidate: resolve commit id: %w", err)
	}
	changeID, err := l.jjOutput(ctx, l.JJClient, []string{
		"log", "--no-graph", "-r", "@", "-T", "change_id",
	})
	if err != nil {
		return nil, fmt.Errorf("lab: seedGoCandidate: resolve change id: %w", err)
	}
	return &CheckCandidateHandle{
		Description:   "bjj-check: " + description,
		CommitID:      commitID,
		ChangeID:      changeID,
		WorkspacePath: l.JJClient,
	}, nil
}

// writeMinimalGoModule writes a minimal compilable Go module
// into dir. name is used as the module path and the package
// name. The module has no external dependencies so `go build`,
// `go vet`, and `go test -count=1 ./...` all run without
// network access (assuming a populated local module cache).
func writeMinimalGoModule(dir, name string) error {
	gomod := "module " + name + "\n\ngo 1.22\n"
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gomod), 0o644); err != nil {
		return fmt.Errorf("write go.mod: %w", err)
	}
	src := "package main\n\nfunc Greet() string { return \"hi\" }\n\nfunc main() { _ = Greet() }\n"
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(src), 0o644); err != nil {
		return fmt.Errorf("write main.go: %w", err)
	}
	return nil
}
