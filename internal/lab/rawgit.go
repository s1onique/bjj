package lab

import (
	"context"
	"errors"
	"fmt"
	"os"
)

// ControlRawGitPush demonstrates that an unrestricted raw Git client can
// mutate the disposable remote. It is a CONTROL observation, not product
// success.
//
// Semantically:
//
//	before:
//	  remote refs/heads/feature absent or != C1
//	raw git push origin feature
//	after:
//	  remote refs/heads/feature == C1
//
// Returns the SHA the remote now points at for `feature`.
func (l *Lab) ControlRawGitPush(ctx context.Context) (string, error) {
	if l == nil || l.GitClient == "" {
		return "", errors.New("lab: ControlRawGitPush: lab not set up")
	}

	// In the raw Git client, create the same C1 commit and feature
	// branch that the jj client carries, so the raw push is meaningful.
	if err := l.runGit(ctx, l.GitClient, []string{"checkout", "-b", "feature"}); err != nil {
		return "", fmt.Errorf("checkout feature: %w", err)
	}
	featureFile := l.GitClient + "/feature.txt"
	if err := writeFileAtomic(featureFile, []byte("candidate content\n")); err != nil {
		return "", fmt.Errorf("write feature: %w", err)
	}
	for _, args := range [][]string{
		{"add", "feature.txt"},
		{"commit", "-m", "lab: candidate C1"},
	} {
		if err := l.runGit(ctx, l.GitClient, args); err != nil {
			return "", fmt.Errorf("git %v: %w", args, err)
		}
	}

	// Sanity: confirm local feature branch is at the new commit.
	localSHA, err := l.gitOutput(ctx, l.GitClient, []string{"rev-parse", "feature"})
	if err != nil {
		return "", fmt.Errorf("rev-parse local feature: %w", err)
	}

	// Confirm remote feature is absent or different before push.
	before, _ := l.gitOutput(ctx, l.Remote, []string{"rev-parse", "refs/heads/feature"})
	if before == localSHA {
		return "", fmt.Errorf("lab: pre-condition failed: remote already at %s", localSHA)
	}

	// Push raw, using remote name `origin` (the clone default).
	if err := l.runGit(ctx, l.GitClient, []string{"push", "origin", "feature"}); err != nil {
		return "", fmt.Errorf("raw git push: %w", err)
	}

	// Verify independently by reading the bare remote.
	after, err := l.gitOutput(ctx, l.Remote, []string{"rev-parse", "refs/heads/feature"})
	if err != nil {
		return "", fmt.Errorf("verify remote feature: %w", err)
	}
	if after != localSHA {
		return "", fmt.Errorf("lab: post-condition failed: remote=%s local=%s", after, localSHA)
	}
	return after, nil
}

// writeFileAtomic writes data to path by creating a sibling temp file
// and renaming into place. Used so partially-written files cannot be
// mistaken for real commits.
func writeFileAtomic(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
