package lab

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

// Setup constructs a fresh lab under a temporary directory and seeds
// initial state.
//
// The seed establishes a remote with a single commit C0 on `main`.
// Setup additionally clones the bare remote into GitClient and JJClient
// (the latter as a jj colocated workspace) and creates a C1 candidate
// commit with a `feature` bookmark ready to push.
func Setup(ctx context.Context) (*Lab, error) {
	if err := requireTool("git"); err != nil {
		return nil, fmt.Errorf("lab: prerequisite: %w", err)
	}
	if err := requireTool("jj"); err != nil {
		return nil, fmt.Errorf("lab: prerequisite: %w", err)
	}
	root, err := os.MkdirTemp("", "bjj-lab-")
	if err != nil {
		return nil, fmt.Errorf("mkdtemp: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "home"), 0o700); err != nil {
		os.RemoveAll(root)
		return nil, fmt.Errorf("mkhome: %w", err)
	}
	l := &Lab{
		Root:      root,
		Remote:    filepath.Join(root, "remote.git"),
		Seed:      filepath.Join(root, "seed"),
		GitClient: filepath.Join(root, "git-client"),
		JJClient:  filepath.Join(root, "jj-client"),
		Home:      filepath.Join(root, "home"),
	}

	// Capture the developer's real origin (if any) so we can prove at
	// the end of the run that we never touched it.
	if v, err := captureRealOriginFromProject(); err == nil {
		l.realOrigin = v
	}

	// 1. Create bare remote.
	if err := l.runGit(ctx, "", []string{"init", "--bare", "--initial-branch=main", l.Remote}); err != nil {
		l.Cleanup()
		return nil, fmt.Errorf("init bare remote: %w", err)
	}

	// 2. Create seed repo with deterministic C0 and push to bare remote.
	if err := l.seedBare(ctx, l.Seed, l.Remote); err != nil {
		l.Cleanup()
		return nil, fmt.Errorf("seed: %w", err)
	}

	// 3. Clone bare remote into raw Git client.
	if err := l.runGit(ctx, "", []string{"clone", l.Remote, l.GitClient}); err != nil {
		l.Cleanup()
		return nil, fmt.Errorf("git clone: %w", err)
	}
	if err := l.configureTestIdentity(ctx, l.GitClient); err != nil {
		l.Cleanup()
		return nil, fmt.Errorf("configure git identity: %w", err)
	}

	// 4. Create jj client as a colocated clone of the same remote.
	if err := l.runJJ(ctx, "", []string{"git", "clone", l.Remote, l.JJClient}); err != nil {
		l.Cleanup()
		return nil, fmt.Errorf("jj git clone: %w", err)
	}
	if err := l.configureTestIdentity(ctx, l.JJClient); err != nil {
		l.Cleanup()
		return nil, fmt.Errorf("configure jj git identity: %w", err)
	}

	// Rename the jj remote to `lab` so pushes are unambiguous and not
	// derived from a default name.
	if err := l.runJJ(ctx, l.JJClient, []string{"git", "remote", "rename", "origin", "lab"}); err != nil {
		l.Cleanup()
		return nil, fmt.Errorf("jj git remote rename: %w", err)
	}

	// Configure jj's per-repo user identity BEFORE creating any new
	// change, so the candidate commit carries an author/committer and
	// `jj git push` does not reject it.
	if err := l.runJJ(ctx, l.JJClient, []string{
		"config", "set", "--repo", "user.name", "bjj-lab",
	}); err != nil {
		l.Cleanup()
		return nil, fmt.Errorf("jj config user.name: %w", err)
	}
	if err := l.runJJ(ctx, l.JJClient, []string{
		"config", "set", "--repo", "user.email", "bjj-lab@example.invalid",
	}); err != nil {
		l.Cleanup()
		return nil, fmt.Errorf("jj config user.email: %w", err)
	}

	// 5. Create candidate change C1 with `feature` bookmark on the jj side.
	if err := l.labSeedCandidate(ctx, l.JJClient); err != nil {
		l.Cleanup()
		return nil, fmt.Errorf("jj seed candidate: %w", err)
	}
	return l, nil
}

// seedBare creates the seed repository at seedDir with a deterministic
// C0 commit and pushes it to the bare remote.
func (l *Lab) seedBare(ctx context.Context, seedDir, remote string) error {
	if err := l.runGit(ctx, "", []string{"init", "--initial-branch=main", seedDir}); err != nil {
		return err
	}
	if err := l.configureTestIdentity(ctx, seedDir); err != nil {
		return err
	}
	readme := filepath.Join(seedDir, "README.txt")
	if err := os.WriteFile(readme, []byte("bjj lab seed\n"), 0o644); err != nil {
		return err
	}
	for _, args := range [][]string{
		{"add", "README.txt"},
		{"commit", "-m", "seed: initial commit"},
		{"remote", "add", "origin", remote},
		{"push", "-u", "origin", "main"},
	} {
		if err := l.runGit(ctx, seedDir, args); err != nil {
			return err
		}
	}
	return nil
}

// labSeedCandidate creates the C1 candidate change on top of C0 in the
// jj client workspace, and places a `feature` bookmark on it.
func (l *Lab) labSeedCandidate(ctx context.Context, jjDir string) error {
	// Create a new change on top of main.
	if err := l.runJJ(ctx, jjDir, []string{"new", "main", "-m", "lab: candidate C1"}); err != nil {
		return err
	}
	// Write a deterministic file.
	candidate := filepath.Join(jjDir, "feature.txt")
	if err := os.WriteFile(candidate, []byte("candidate content\n"), 0o644); err != nil {
		return err
	}
	// Create the feature bookmark at the working copy.
	if err := l.runJJ(ctx, jjDir, []string{"bookmark", "create", "feature", "-r", "@"}); err != nil {
		return err
	}
	return nil
}
