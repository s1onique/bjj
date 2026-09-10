package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/s1onique/bjj/internal/version"
)

func TestRunNoArgs(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run(nil, &stdout, &stderr); code != exitInvalidArgs {
		t.Fatalf("expected exit %d, got %d", exitInvalidArgs, code)
	}
	if !strings.Contains(stderr.String(), "usage: bjj") {
		t.Fatalf("expected usage on stderr; got %q", stderr.String())
	}
}

func TestRunUnknownCommand(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"publish"}, &stdout, &stderr); code != exitInvalidArgs {
		t.Fatalf("expected exit %d, got %d", exitInvalidArgs, code)
	}
	if !strings.Contains(stderr.String(), "unknown command") {
		t.Fatalf("expected 'unknown command' on stderr; got %q", stderr.String())
	}
}

func TestRunHelp(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"help"}, &stdout, &stderr); code != exitOK {
		t.Fatalf("expected exit 0, got %d", code)
	}
	if !strings.Contains(stdout.String(), "usage: bjj") {
		t.Fatalf("expected usage on stdout; got %q", stdout.String())
	}
}

func TestRunVersionText(t *testing.T) {
	// Snapshot/restore version globals.
	origV, origC, origB := version.Version, version.Commit, version.BuildTime
	t.Cleanup(func() {
		version.Version, version.Commit, version.BuildTime = origV, origC, origB
	})
	version.Version = "0.1.0"
	version.Commit = "deadbeef"
	version.BuildTime = "2026-09-10T00:00:00Z"

	var stdout, stderr bytes.Buffer
	if code := run([]string{"version"}, &stdout, &stderr); code != exitOK {
		t.Fatalf("expected exit 0, got %d (stderr=%s)", code, stderr.String())
	}
	s := stdout.String()
	if !strings.HasPrefix(s, "bjj 0.1.0\n") {
		t.Fatalf("expected banner starting with 'bjj 0.1.0'; got %q", s)
	}
	if !strings.Contains(s, "commit:     deadbeef") {
		t.Fatalf("banner missing commit line; got %q", s)
	}
	if !strings.Contains(s, "build time: 2026-09-10T00:00:00Z") {
		t.Fatalf("banner missing build time; got %q", s)
	}
	if stderr.Len() != 0 {
		t.Fatalf("expected empty stderr; got %q", stderr.String())
	}
}

func TestRunVersionJSONStrict(t *testing.T) {
	origV, origC, origB := version.Version, version.Commit, version.BuildTime
	t.Cleanup(func() {
		version.Version, version.Commit, version.BuildTime = origV, origC, origB
	})
	version.Version = "0.1.0"
	version.Commit = "cafebabe"
	version.BuildTime = "2026-09-10T01:02:03Z"

	var stdout, stderr bytes.Buffer
	if code := run([]string{"version", "--json"}, &stdout, &stderr); code != exitOK {
		t.Fatalf("expected exit 0, got %d (stderr=%s)", code, stderr.String())
	}
	// Must be valid strict JSON.
	var got version.Info
	if err := json.Unmarshal(stdout.Bytes(), &got); err != nil {
		t.Fatalf("invalid strict JSON: %v\nbody: %s", err, stdout.String())
	}
	if got.Version != "0.1.0" || got.Commit != "cafebabe" || got.BuildTime != "2026-09-10T01:02:03Z" {
		t.Fatalf("unexpected decoded info: %+v", got)
	}
	// Output must end with a newline.
	if !bytes.HasSuffix(stdout.Bytes(), []byte("\n")) {
		t.Fatalf("expected trailing newline; got %q", stdout.String())
	}
}

func TestRunVersionUnknownArg(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"version", "--bogus"}, &stdout, &stderr); code != exitInvalidArgs {
		t.Fatalf("expected exit %d, got %d", exitInvalidArgs, code)
	}
	if !strings.Contains(stderr.String(), "unknown argument") {
		t.Fatalf("expected 'unknown argument' on stderr; got %q", stderr.String())
	}
}

func TestRunShortVersionFlags(t *testing.T) {
	// `bjj -v` and `bjj --version` should behave like `bjj version`.
	var stdout, stderr bytes.Buffer
	if code := run([]string{"-v"}, &stdout, &stderr); code != exitOK {
		t.Fatalf("expected exit 0, got %d", code)
	}
	if !strings.HasPrefix(stdout.String(), "bjj ") {
		t.Fatalf("expected banner; got %q", stdout.String())
	}
}
