package version

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestInfoDefaultsAreSentinels(t *testing.T) {
	if Version != "dev" {
		t.Fatalf("expected default Version=dev, got %q", Version)
	}
	if Commit != "unknown" {
		t.Fatalf("expected default Commit=unknown, got %q", Commit)
	}
	if BuildTime != "unknown" {
		t.Fatalf("expected default BuildTime=unknown, got %q", BuildTime)
	}
}

func TestInfoProducesStrictJSON(t *testing.T) {
	// Save and restore to keep test isolation even if other tests mutate globals.
	origV, origC, origB := Version, Commit, BuildTime
	t.Cleanup(func() { Version, Commit, BuildTime = origV, origC, origB })

	Version = "0.1.0"
	Commit = "deadbeef"
	BuildTime = "2026-09-10T00:00:00Z"

	info := Info{
		Version:   Version,
		Commit:    Commit,
		BuildTime: BuildTime,
	}
	b, err := json.Marshal(info)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	got := string(b)
	for _, want := range []string{
		`"version":"0.1.0"`,
		`"commit":"deadbeef"`,
		`"build_time":"2026-09-10T00:00:00Z"`,
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("JSON missing %s; got %s", want, got)
		}
	}
	// Round-trip strict decode to guarantee it is valid strict JSON.
	var round Info
	if err := json.Unmarshal(b, &round); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if round != info {
		t.Fatalf("round-trip mismatch: %+v vs %+v", round, info)
	}
}
