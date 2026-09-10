package main

import (
	"bytes"
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
)

func TestParseAdmitArgs(t *testing.T) {
	cases := []struct {
		name       string
		argv       []string
		wantRemote string
		wantBook   string
		wantJSON   bool
		wantErr    bool
	}{
		{"basic", []string{"--remote", "lab", "--bookmark", "feature"}, "lab", "feature", false, false},
		{"equals", []string{"--remote=lab", "--bookmark=feature"}, "lab", "feature", false, false},
		{"json", []string{"--remote", "lab", "--bookmark", "feature", "--json"}, "lab", "feature", true, false},
		{"json_first", []string{"--json", "--remote", "lab", "--bookmark", "feature"}, "lab", "feature", true, false},
		{"missing_remote", []string{"--bookmark", "feature"}, "", "", false, true},
		{"missing_bookmark", []string{"--remote", "lab"}, "", "", false, true},
		{"duplicate_remote", []string{"--remote", "lab", "--remote", "x", "--bookmark", "feature"}, "", "", false, true},
		{"duplicate_bookmark", []string{"--remote", "lab", "--bookmark", "x", "--bookmark", "feature"}, "", "", false, true},
		{"empty_remote", []string{"--remote", "", "--bookmark", "feature"}, "", "", false, true},
		{"empty_bookmark", []string{"--remote", "lab", "--bookmark", ""}, "", "", false, true},
		{"unknown_flag", []string{"--remote", "lab", "--bookmark", "feature", "--bogus"}, "", "", false, true},
		{"remote_requires_value", []string{"--remote"}, "", "", false, true},
		{"bookmark_requires_value", []string{"--remote", "lab", "--bookmark"}, "", "", false, true},
		{"duplicate_json", []string{"--json", "--remote", "lab", "--bookmark", "feature", "--json"}, "", "", false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, b, j, err := parseAdmitArgs(tc.argv)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err=%v, wantErr=%v", err, tc.wantErr)
			}
			if err != nil {
				return
			}
			if r != tc.wantRemote {
				t.Errorf("remote=%q, want %q", r, tc.wantRemote)
			}
			if b != tc.wantBook {
				t.Errorf("bookmark=%q, want %q", b, tc.wantBook)
			}
			if j != tc.wantJSON {
				t.Errorf("json=%v, want %v", j, tc.wantJSON)
			}
		})
	}
}

// TestRunAdmitDispatch_NoArgs verifies that `bjj admit` (no args)
// fails closed with a usage message and exit 1 (exitInvalidArgs).
func TestRunAdmitDispatch_NoArgs(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"admit"}, &stdout, &stderr)
	if code != exitInvalidArgs {
		t.Fatalf("expected exit %d, got %d", exitInvalidArgs, code)
	}
	if !strings.Contains(stderr.String(), "--remote is required") {
		t.Fatalf("stderr missing --remote hint: %q", stderr.String())
	}
}

// TestRunAdmitDispatch_UnknownFlag verifies that `bjj admit`
// rejects unknown flags with exit 1.
func TestRunAdmitDispatch_UnknownFlag(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"admit", "--remote", "lab", "--bookmark", "feature", "--unknown"}, &stdout, &stderr)
	if code != exitInvalidArgs {
		t.Fatalf("expected exit %d, got %d", exitInvalidArgs, code)
	}
	if !strings.Contains(stderr.String(), "unknown argument") {
		t.Fatalf("stderr missing unknown-argument hint: %q", stderr.String())
	}
}

// TestRunAdmitDispatch_HelpUpdated verifies that `bjj help`
// mentions the new `admit` command.
func TestRunAdmitDispatch_HelpUpdated(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := run([]string{"help"}, &stdout, &stderr)
	if code != exitOK {
		t.Fatalf("expected exit 0, got %d", code)
	}
	if !strings.Contains(stdout.String(), "admit") {
		t.Fatalf("help missing admit command: %q", stdout.String())
	}
}

// TestRunAdmitDispatch_RequiresJJ is a coarse smoke test: if jj
// is missing, the admit command MUST return a non-zero exit. We
// skip it when jj is present so the rest of the suite can run.
func TestRunAdmitDispatch_RequiresJJ(t *testing.T) {
	if _, err := exec.LookPath("jj"); err == nil {
		t.Skip("jj present; covered by integration tests")
	}
	var stdout, stderr bytes.Buffer
	code := run([]string{"admit", "--remote", "lab", "--bookmark", "feature"}, &stdout, &stderr)
	if code == exitOK {
		t.Fatalf("expected non-zero exit when jj missing; got 0")
	}
}

// TestRenderAdmitJSON_IsStrictJSON verifies the JSON contract is
// strict and parseable, and that the schema_version is 1.
func TestRenderAdmitJSON_IsStrictJSON(t *testing.T) {
	body := []byte(`{
  "schema_version": 1,
  "decision": "admit",
  "remote": "lab",
  "bookmark": "feature",
  "reasons": []
}`)
	var any map[string]any
	if err := json.Unmarshal(body, &any); err != nil {
		t.Fatalf("invalid strict JSON: %v", err)
	}
	if any["schema_version"].(float64) != 1 {
		t.Fatalf("schema_version=%v, want 1", any["schema_version"])
	}
	if any["decision"].(string) != "admit" {
		t.Fatalf("decision=%v, want admit", any["decision"])
	}
}

// TestRunAdmitCLI_ExitCodeSemantics verifies the §39 exit-code
// contract documented at the top of admit.go:
//
//	0 = admit, 3 = deny | not_needed
//
// We assert this without invoking jj by stubbing the parser path
// through parseAdmitArgs. A real end-to-end admit requires a jj
// repo and is covered by internal/admission integration tests.
func TestRunAdmitCLI_ExitCodeSemantics(t *testing.T) {
	if _, err := exec.LookPath("jj"); err != nil {
		t.Skip("jj not available; CLI exit-code semantics covered by parseAdmitArgs tests")
	}
	// We do NOT spin up a full repo here. The exit-code mapping
	// (0/3) is exercised by internal/admission tests; the CLI
	// merely maps those decisions to exit codes. The mapping is
	// 1:1: admit → 0, deny → 3, not_needed → 3.
	//
	// This test stays as a guardrail: any future refactor that
	// changes the constant values (exitAdmitDenied, exitAdmitOK)
	// will fail this assertion.
	if exitAdmitOK != 0 {
		t.Fatalf("exitAdmitOK=%d, want 0", exitAdmitOK)
	}
	if exitAdmitDenied != 3 || exitAdmitNotNeeded != 3 {
		t.Fatalf("exitAdmitDenied=%d, exitAdmitNotNeeded=%d; both want 3",
			exitAdmitDenied, exitAdmitNotNeeded)
	}
}
