package lab

import (
	"context"
	"testing"
)

func TestNegativeTransportLeavesRemoteUnchanged(t *testing.T) {
	RequireGitAndJJ(t)

	ctx := context.Background()
	l, err := Setup(ctx)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	defer l.Cleanup()

	// Pre-seed a known refs/heads/feature on the bare remote so we can
	// observe that the negative push does not mutate it.
	featureSHA, err := l.ControlRawGitPush(ctx)
	if err != nil {
		t.Fatalf("seed raw git push: %v", err)
	}

	// Run the negative transport.
	failure, err := l.NegativeTransport(ctx)
	if err != nil {
		t.Fatalf("negative transport: %v", err)
	}
	if !failure.Attempted {
		t.Fatal("expected failure.Attempted to be true")
	}
	if failure.Err == nil {
		t.Fatal("expected failure.Err to be non-nil")
	}

	// Remote state must be unchanged.
	after, err := l.gitOutput(ctx, l.Remote, []string{"rev-parse", "refs/heads/feature"})
	if err != nil {
		t.Fatalf("verify remote feature: %v", err)
	}
	if after != featureSHA {
		t.Fatalf("remote feature mutated despite failed transport: before=%s after=%s", featureSHA, after)
	}
}
