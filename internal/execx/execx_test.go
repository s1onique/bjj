package execx

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// useTmpDir creates a fresh temp directory rooted under t.TempDir() and
// returns its absolute path.
func useTmpDir(t *testing.T) string {
	t.Helper()
	d := t.TempDir()
	abs, err := filepath.Abs(d)
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	return abs
}

func TestRunRequiresProgram(t *testing.T) {
	res := Run(context.Background(), Request{Dir: useTmpDir(t)})
	if res.Err == nil {
		t.Fatal("expected error for empty Program")
	}
}

func TestRunRequiresAbsoluteDir(t *testing.T) {
	res := Run(context.Background(), Request{
		Program: "/bin/echo",
		Dir:     "relative",
	})
	if res.Err == nil {
		t.Fatal("expected error for relative Dir")
	}
}

func TestRunSuccess(t *testing.T) {
	res := Run(context.Background(), Request{
		Program: "/bin/echo",
		Args:    []string{"hello", "world"},
		Dir:     useTmpDir(t),
	})
	if res.Err != nil {
		t.Fatalf("unexpected error: %v", res.Err)
	}
	if res.ExitCode != 0 {
		t.Fatalf("expected exit 0, got %d", res.ExitCode)
	}
	if got := strings.TrimSpace(string(res.Stdout)); got != "hello world" {
		t.Fatalf("unexpected stdout: %q", got)
	}
}

func TestRunNonZeroExit(t *testing.T) {
	res := Run(context.Background(), Request{
		Program: "/bin/sh",
		Args:    []string{"-c", "echo oops 1>&2; exit 7"},
		Dir:     useTmpDir(t),
	})
	if res.Err == nil {
		t.Fatal("expected error for non-zero exit")
	}
	if res.ExitCode != 7 {
		t.Fatalf("expected exit 7, got %d", res.ExitCode)
	}
	if !bytes.Contains(res.Stderr, []byte("oops")) {
		t.Fatalf("expected stderr to contain oops; got %q", res.Stderr)
	}
}

func TestRunStdoutStderrCapture(t *testing.T) {
	res := Run(context.Background(), Request{
		Program: "/bin/sh",
		Args:    []string{"-c", "echo out; echo err 1>&2"},
		Dir:     useTmpDir(t),
	})
	if res.Err != nil {
		t.Fatalf("unexpected error: %v", res.Err)
	}
	if !bytes.Contains(res.Stdout, []byte("out")) {
		t.Fatalf("stdout missing: %q", res.Stdout)
	}
	if !bytes.Contains(res.Stderr, []byte("err")) {
		t.Fatalf("stderr missing: %q", res.Stderr)
	}
}

func TestRunMissingExecutableReturnsTypedError(t *testing.T) {
	res := Run(context.Background(), Request{
		Program: "/no/such/binary/exists/anywhere",
		Args:    []string{},
		Dir:     useTmpDir(t),
	})
	if res.Err == nil {
		t.Fatal("expected error for missing program")
	}
	if res.ExitCode != -1 {
		t.Fatalf("expected ExitCode=-1 for missing program, got %d", res.ExitCode)
	}
}

func TestRunOutputTruncation(t *testing.T) {
	res := Run(context.Background(), Request{
		Program:        "/bin/sh",
		Args:           []string{"-c", "yes A | head -c 200000"},
		Dir:            useTmpDir(t),
		MaxOutputBytes: 1024,
	})
	if res.Err != nil {
		t.Fatalf("unexpected error: %v", res.Err)
	}
	if len(res.Stdout) > 2*1024 {
		t.Fatalf("stdout not bounded; got %d bytes", len(res.Stdout))
	}
	if !bytes.Contains(res.Stdout, []byte(TruncationMarker)) {
		t.Fatalf("expected truncation marker in stdout; got tail %q", tail(res.Stdout))
	}
}

func TestRunEnvIsExplicitOverlay(t *testing.T) {
	// Set a credential-like variable in the test process env; NewBaseEnv
	// must strip it. Then ensure an explicit overlay re-adds it.
	t.Setenv("GITHUB_TOKEN", "secret-should-be-stripped")
	t.Setenv("BJJ_TEST_OVERLAY", "1")

	base := NewBaseEnv()
	for _, kv := range base {
		if strings.HasPrefix(kv, "GITHUB_TOKEN=") {
			t.Fatalf("GITHUB_TOKEN must not leak through NewBaseEnv: %s", kv)
		}
	}

	dir := useTmpDir(t)
	res := Run(context.Background(), Request{
		Program: "/bin/sh",
		Args:    []string{"-c", "echo token=${GITHUB_TOKEN:-absent}; echo overlay=${BJJ_TEST_OVERLAY:-absent}"},
		Dir:     dir,
		Env: []string{
			"GITHUB_TOKEN=" + os.Getenv("GITHUB_TOKEN"),
			"BJJ_TEST_OVERLAY=" + os.Getenv("BJJ_TEST_OVERLAY"),
		},
	})
	if res.Err != nil {
		t.Fatalf("unexpected error: %v", res.Err)
	}
	out := string(res.Stdout)
	if !strings.Contains(out, "token=secret-should-be-stripped") {
		t.Fatalf("explicit overlay did not re-add GITHUB_TOKEN; got: %s", out)
	}
	if !strings.Contains(out, "overlay=1") {
		t.Fatalf("explicit overlay did not pass BJJ_TEST_OVERLAY; got: %s", out)
	}
}

func TestRunEnvStripsSensitivePrefixes(t *testing.T) {
	t.Setenv("GITHUB_TOKEN", "ghs_test")
	t.Setenv("SSH_AUTH_SOCK", "/tmp/sock")
	t.Setenv("JUJUTSU_CONFIG", "/dev/null")
	t.Setenv("HOME", "/Users/leaky")
	t.Setenv("LANG", "en_US.UTF-8")

	base := NewBaseEnv()
	joined := strings.Join(base, "\n")
	for _, forbidden := range []string{
		"GITHUB_TOKEN=",
		"SSH_AUTH_SOCK=",
		"JUJUTSU_CONFIG=",
		"HOME=",
	} {
		if strings.Contains(joined, forbidden) {
			t.Fatalf("sensitive var %q leaked into base env; got:\n%s", forbidden, joined)
		}
	}
	if !strings.Contains(joined, "LANG=") {
		t.Fatalf("LANG should have been preserved; got:\n%s", joined)
	}
}

func tail(b []byte) string {
	if len(b) > 80 {
		return string(b[len(b)-80:])
	}
	return string(b)
}
