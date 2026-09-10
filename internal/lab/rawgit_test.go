package lab

import (
	"context"
	"testing"
)

func TestControlRawGitPushMutatesRemote(t *testing.T) {
	RequireGitAndJJ(t)

	ctx := context.Background()
	l, err := Setup(ctx)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	defer l.Cleanup()

	// Pre-condition: remote refs/heads/feature is absent.
	if _, err := l.gitOutput(ctx, l.Remote, []string{"rev-parse", "refs/heads/feature"}); err == nil {
		t.Fatalf("expected refs/heads/feature to be absent before control push")
	}

	got, err := l.ControlRawGitPush(ctx)
	if err != nil {
		t.Fatalf("control raw git push: %v", err)
	}
	if got == "" {
		t.Fatal("expected non-empty SHA after raw git push")
	}

	// Verify remote independently via rev-parse.
	remote, err := l.gitOutput(ctx, l.Remote, []string{"rev-parse", "refs/heads/feature"})
	if err != nil {
		t.Fatalf("verify remote feature: %v", err)
	}
	if remote != got {
		t.Fatalf("remote SHA mismatch: control=%s verify=%s", got, remote)
	}
}
