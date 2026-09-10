package lab

import (
	"context"
	"errors"
	"fmt"
)

// ControlRawJJPush demonstrates that an unrestricted `jj git push` can
// mutate the disposable remote. It is a CONTROL observation, not
// product success.
//
// Semantically:
//
//	before:
//	  remote refs/heads/feature absent or != candidate
//	jj git push --bookmark feature --remote lab
//	after:
//	  remote refs/heads/feature == candidate commit
//
// Returns the SHA the remote now points at for `feature`.
func (l *Lab) ControlRawJJPush(ctx context.Context) (string, error) {
	if l == nil || l.JJClient == "" {
		return "", errors.New("lab: ControlRawJJPush: lab not set up")
	}

	// Sanity: confirm the candidate commit we intend to push.
	candidateChangeID, err := l.jjOutput(ctx, l.JJClient, []string{
		"log", "--no-graph", "-r", "feature", "-T", "commit_id",
	})
	if err != nil {
		return "", fmt.Errorf("resolve candidate commit: %w", err)
	}
	if candidateChangeID == "" {
		return "", errors.New("lab: candidate change for `feature` not found")
	}

	// Confirm remote feature is absent or different before push.
	before, _ := l.gitOutput(ctx, l.Remote, []string{"rev-parse", "refs/heads/feature"})
	if before == candidateChangeID {
		return "", fmt.Errorf("lab: pre-condition failed: remote already at %s", candidateChangeID)
	}

	// Push the bookmark explicitly via the remote name `lab`.
	if err := l.runJJ(ctx, l.JJClient, []string{
		"git", "push",
		"--remote", "lab",
		"--bookmark", "feature",
		"--allow-empty-description",
	}); err != nil {
		return "", fmt.Errorf("jj git push: %w", err)
	}

	// Verify independently by reading the bare remote.
	after, err := l.gitOutput(ctx, l.Remote, []string{"rev-parse", "refs/heads/feature"})
	if err != nil {
		return "", fmt.Errorf("verify remote feature: %w", err)
	}
	if after != candidateChangeID {
		return "", fmt.Errorf("lab: post-condition failed: remote=%s candidate=%s", after, candidateChangeID)
	}
	return after, nil
}
