package lab

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

// SeedBookmarkPush pushes the named bookmark to the lab remote so a
// PLAN01 fixture has a known non-absent remote-target.
func (l *Lab) SeedBookmarkPush(ctx context.Context, bookmark string) error {
	if l == nil || l.JJClient == "" {
		return errors.New("lab: SeedBookmarkPush: lab not set up")
	}
	return l.runJJ(ctx, l.JJClient, []string{
		"git", "push",
		"--remote", "lab",
		"--bookmark", bookmark,
		"--allow-empty-description",
	})
}

// SeedRemoteMoveTo moves the remote-tracking bookmark on the lab
// remote by pushing from the raw git client.
func (l *Lab) SeedRemoteMoveTo(ctx context.Context, bookmark, seedBranch string) error {
	if l == nil || l.GitClient == "" {
		return errors.New("lab: SeedRemoteMoveTo: lab not set up")
	}
	return l.runGit(ctx, l.GitClient, []string{
		"push", "-f", "origin",
		seedBranch + ":" + bookmark,
	})
}

// SeedExternalRemoteMove mutates the bare remote's refs/heads/<bookmark>
// directly via `git update-ref`.
func (l *Lab) SeedExternalRemoteMove(ctx context.Context, bookmark, commitID string) error {
	if l == nil || l.Remote == "" {
		return errors.New("lab: SeedExternalRemoteMove: lab not set up")
	}
	return l.runGit(ctx, l.Remote, []string{
		"update-ref", "refs/heads/" + bookmark, commitID,
	})
}

// SeedCreateStackedCommits creates N new stacked commits on top of
// `base` in the jj client, placing the `stack` bookmark on the
// topmost commit.
func (l *Lab) SeedCreateStackedCommits(ctx context.Context, base, descriptionPrefix string, n int) (deepest, topmost string, err error) {
	if l == nil || l.JJClient == "" {
		return "", "", errors.New("lab: SeedCreateStackedCommits: lab not set up")
	}
	if n <= 0 {
		return "", "", errors.New("lab: SeedCreateStackedCommits: n must be > 0")
	}

	if err := l.runJJ(ctx, l.JJClient, []string{"new", base, "-m", descriptionPrefix + " 1"}); err != nil {
		return "", "", err
	}
	if err := l.writeStubFile("stack-1.txt"); err != nil {
		return "", "", err
	}
	d, err := l.jjOutput(ctx, l.JJClient, []string{"log", "--no-graph", "-r", "@", "-T", "commit_id"})
	if err != nil {
		return "", "", err
	}
	deepest = strings.TrimSpace(d)
	if err := l.runJJ(ctx, l.JJClient, []string{"bookmark", "create", "stack", "-r", "@"}); err != nil {
		return "", "", err
	}

	prev := "@"
	for i := 2; i <= n; i++ {
		if err := l.runJJ(ctx, l.JJClient, []string{"new", prev, "-m", fmt.Sprintf("%s %d", descriptionPrefix, i)}); err != nil {
			return "", "", err
		}
		if err := l.writeStubFile(fmt.Sprintf("stack-%d.txt", i)); err != nil {
			return "", "", err
		}
		prev = "@"
	}
	if err := l.runJJ(ctx, l.JJClient, []string{"bookmark", "set", "stack", "-r", "@"}); err != nil {
		return "", "", err
	}
	t, err := l.jjOutput(ctx, l.JJClient, []string{"log", "--no-graph", "-r", "@", "-T", "commit_id"})
	if err != nil {
		return "", "", err
	}
	topmost = strings.TrimSpace(t)
	return deepest, topmost, nil
}

// writeStubFile writes a deterministic stub file to the jj client.
func (l *Lab) writeStubFile(name string) error {
	if l == nil || l.JJClient == "" {
		return errors.New("lab: writeStubFile: lab not set up")
	}
	p := filepath.Join(l.JJClient, name)
	return os.WriteFile(p, []byte(name+"\n"), 0o644)
}

// writeStubFileGit writes a deterministic stub file to the git client.
func (l *Lab) writeStubFileGit(name string) error {
	if l == nil || l.GitClient == "" {
		return errors.New("lab: writeStubFileGit: lab not set up")
	}
	p := filepath.Join(l.GitClient, name)
	return os.WriteFile(p, []byte(name+"\n"), 0o644)
}

// SeedRewrittenFeature builds a single commit on top of `base`,
// records its change id and commit id, then rewrites the commit.
func (l *Lab) SeedRewrittenFeature(ctx context.Context, base string) (changeID, oldCommitID, newCommitID string, err error) {
	if l == nil || l.JJClient == "" {
		return "", "", "", errors.New("lab: SeedRewrittenFeature: lab not set up")
	}
	if err := l.runJJ(ctx, l.JJClient, []string{"new", base, "-m", "rewrite original"}); err != nil {
		return "", "", "", err
	}
	if err := l.writeStubFile("rewrite-1.txt"); err != nil {
		return "", "", "", err
	}
	cid, err := l.jjOutput(ctx, l.JJClient, []string{"log", "--no-graph", "-r", "@", "-T", "change_id"})
	if err != nil {
		return "", "", "", err
	}
	changeID = strings.TrimSpace(cid)
	old, err := l.jjOutput(ctx, l.JJClient, []string{"log", "--no-graph", "-r", "@", "-T", "commit_id"})
	if err != nil {
		return "", "", "", err
	}
	oldCommitID = strings.TrimSpace(old)
	if err := l.runJJ(ctx, l.JJClient, []string{"describe", "-m", "rewrite amended"}); err != nil {
		return "", "", "", err
	}
	nc, err := l.jjOutput(ctx, l.JJClient, []string{"log", "--no-graph", "-r", "@", "-T", "commit_id"})
	if err != nil {
		return "", "", "", err
	}
	newCommitID = strings.TrimSpace(nc)
	return changeID, oldCommitID, newCommitID, nil
}

// SeedConflictedBookmark creates a locally-conflicted bookmark by
// advancing both a raw-git clone and the jj client to diverge from
// the same starting point, then fetching.
func (l *Lab) SeedConflictedBookmark(ctx context.Context, bookmark string) error {
	if l == nil || l.GitClient == "" || l.JJClient == "" {
		return errors.New("lab: SeedConflictedBookmark: lab not set up")
	}
	if err := l.runGit(ctx, l.GitClient, []string{"checkout", "-b", bookmark}); err != nil {
		return err
	}
	if err := l.writeStubFileGit(bookmark + "-via-git.txt"); err != nil {
		return err
	}
	if err := l.runGit(ctx, l.GitClient, []string{"add", bookmark + "-via-git.txt"}); err != nil {
		return err
	}
	if err := l.runGit(ctx, l.GitClient, []string{"commit", "-m", "git-side divergence"}); err != nil {
		return err
	}
	if err := l.runGit(ctx, l.GitClient, []string{"push", "-f", "origin", bookmark}); err != nil {
		return err
	}
	if err := l.runJJ(ctx, l.JJClient, []string{"new", "main", "-m", "jj-side divergence"}); err != nil {
		return err
	}
	if err := l.writeStubFile(bookmark + "-via-jj.txt"); err != nil {
		return err
	}
	if err := l.runJJ(ctx, l.JJClient, []string{"bookmark", "create", bookmark, "-r", "@"}); err != nil {
		return err
	}
	return l.runJJ(ctx, l.JJClient, []string{"git", "fetch", "--remote", "lab"})
}
