package lab

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestRunEvidenceAllObservationsPass(t *testing.T) {
	RequireGitAndJJ(t)

	_, ev, err := Run(context.Background())
	if err != nil {
		t.Fatalf("lab Run: %v (failure_reason=%q)", err, ev.FailureReason)
	}

	if ev.SchemaVersion != SchemaVersion {
		t.Fatalf("schema_version=%d, want %d", ev.SchemaVersion, SchemaVersion)
	}
	if !strings.HasPrefix(ev.JJVersion, "jj ") {
		t.Fatalf("unexpected jj version line: %q", ev.JJVersion)
	}
	if !strings.HasPrefix(ev.GitVersion, "git version ") {
		t.Fatalf("unexpected git version line: %q", ev.GitVersion)
	}

	required := []struct {
		name string
		val  bool
	}{
		{"ControlRawGitPush", ev.ControlRawGitPush},
		{"ControlRawJJPush", ev.ControlRawJJPush},
		{"LabRemoteIsLocal", ev.LabRemoteIsLocal},
		{"RealOriginNotReferenced", ev.RealOriginNotReferenced},
		{"HomeCredentialNotNeeded", ev.HomeCredentialNotNeeded},
		{"TransportFailureObserved", ev.TransportFailureObserved},
		{"FailedTransportRemoteUnchanged", ev.FailedTransportRemoteUnchanged},
		{"RemoteGitStateVerified", ev.RemoteGitStateVerified},
		{"RemoteJJStateVerified", ev.RemoteJJStateVerified},
	}
	for _, r := range required {
		if !r.val {
			t.Errorf("evidence.%s = false", r.name)
		}
	}
}

func TestEvidenceJSONStrict(t *testing.T) {
	ev := Evidence{
		SchemaVersion:                  SchemaVersion,
		JJVersion:                      "jj 0.41.0",
		GitVersion:                     "git version 2.54.0",
		GoVersion:                      "go1.26.6",
		OS:                             "darwin",
		Arch:                           "arm64",
		ControlRawGitPush:              true,
		ControlRawJJPush:               true,
		LabRemoteIsLocal:               true,
		RealOriginNotReferenced:        true,
		HomeCredentialNotNeeded:        true,
		TransportFailureObserved:       true,
		FailedTransportRemoteUnchanged: true,
		RemoteGitStateVerified:         true,
		RemoteJJStateVerified:          true,
	}
	b, err := EvidenceJSON(ev)
	if err != nil {
		t.Fatalf("EvidenceJSON: %v", err)
	}
	// Must be valid strict JSON.
	var round Evidence
	if err := json.Unmarshal(b, &round); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if round != ev {
		t.Fatalf("round-trip mismatch: %+v vs %+v", round, ev)
	}
	s := string(b)
	for _, want := range []string{
		`"schema_version": 1`,
		`"control_raw_git_push": true`,
		`"control_raw_jj_push": true`,
		`"lab_remote_is_local": true`,
		`"real_origin_not_referenced": true`,
		`"home_credential_not_needed": true`,
		`"transport_failure_observed": true`,
		`"failed_transport_remote_unchanged": true`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("JSON missing %s; got:\n%s", want, s)
		}
	}
}

func TestLabDoesNotTouchRealOrigin(t *testing.T) {
	RequireGitAndJJ(t)

	ctx := context.Background()
	l, err := Setup(ctx)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	defer l.Cleanup()

	realOrigin := l.RealOrigin()
	if realOrigin == "" {
		t.Skip("no real origin configured in the project; cannot assert non-reference")
	}

	// Run both control pushes on independent labs so each observes a
	// pristine remote (the second push would otherwise fail with
	// "stale info" because the first one moved refs/heads/feature).
	labA, err := Setup(ctx)
	if err != nil {
		t.Fatalf("setup A: %v", err)
	}
	defer labA.Cleanup()
	if _, err := labA.ControlRawGitPush(ctx); err != nil {
		t.Fatalf("control raw git push: %v", err)
	}

	labB, err := Setup(ctx)
	if err != nil {
		t.Fatalf("setup B: %v", err)
	}
	defer labB.Cleanup()
	if _, err := labB.ControlRawJJPush(ctx); err != nil {
		t.Fatalf("control raw jj push: %v", err)
	}

	// Every git client and jj client inside the lab must reference only
	// local paths in their remotes.
	for _, dir := range []string{labA.GitClient, labB.GitClient, labB.JJClient} {
		out, err := l.gitOutput(ctx, dir, []string{"remote", "-v"})
		if err != nil {
			continue
		}
		if strings.Contains(out, realOrigin) {
			t.Fatalf("lab repository %s references real origin %s: %s", dir, realOrigin, out)
		}
	}
}
