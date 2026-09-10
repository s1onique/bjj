package lab

import (
	"context"
	"strings"
	"testing"
)

func TestControlRawJJPushMutatesRemote(t *testing.T) {
	RequireGitAndJJ(t)

	ctx := context.Background()
	l, err := Setup(ctx)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	defer l.Cleanup()

	got, err := l.ControlRawJJPush(ctx)
	if err != nil {
		t.Fatalf("control raw jj push: %v", err)
	}
	if got == "" {
		t.Fatal("expected non-empty SHA after raw jj push")
	}

	// Verify the SHA matches the candidate commit recorded by jj.
	candidate, err := l.jjOutput(ctx, l.JJClient, []string{
		"log", "--no-graph", "-r", "feature", "-T", "commit_id",
	})
	if err != nil {
		t.Fatalf("resolve candidate: %v", err)
	}
	candidate = strings.TrimSpace(candidate)
	if candidate != got {
		t.Fatalf("candidate mismatch: control=%s jj=%s", got, candidate)
	}

	// Cross-verify against the bare remote.
	remote, err := l.gitOutput(ctx, l.Remote, []string{"rev-parse", "refs/heads/feature"})
	if err != nil {
		t.Fatalf("verify remote feature: %v", err)
	}
	if remote != got {
		t.Fatalf("remote SHA mismatch: control=%s verify=%s", got, remote)
	}
}

func TestControlRawJJPushUsesLabRemoteName(t *testing.T) {
	RequireGitAndJJ(t)

	ctx := context.Background()
	l, err := Setup(ctx)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	defer l.Cleanup()

	// Confirm the jj client only knows the `lab` remote name.
	out, err := l.jjOutput(ctx, l.JJClient, []string{"git", "remote", "list"})
	if err != nil {
		t.Fatalf("jj git remote list: %v", err)
	}
	if !strings.Contains(out, "lab") {
		t.Fatalf("expected `lab` in jj git remote list; got: %s", out)
	}
	if strings.Contains(out, "origin") {
		t.Fatalf("jj git remote list unexpectedly mentions origin: %s", out)
	}
}
