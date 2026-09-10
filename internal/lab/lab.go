// Package lab is the BJJ publication laboratory.
//
// The laboratory constructs an entire disposable universe inside a
// temporary directory:
//
//	tmp/
//	├── remote.git/   # bare Git repository (the "remote")
//	├── seed/         # the initial seed repository creator
//	├── git-client/   # a raw Git clone used for control experiment A
//	└── jj-client/    # a Jujutsu workspace used for control experiment B
//
// Every operation is performed via internal/execx so that:
//
//   - argv is passed structurally (no shell),
//   - the environment is an explicit overlay (no credential leakage),
//   - working directories are absolute,
//   - stdout/stderr are bounded,
//   - the real `origin` remote is never referenced.
//
// This package exposes typed Lab / Evidence structures so the control
// observations are machine-checkable, not prose.
package lab

import (
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// SchemaVersion is the version of the Evidence JSON schema produced by
// this package.
const SchemaVersion = 1

// Evidence is the typed result of running the publication laboratory.
//
// All booleans are observations of the lab, not claims about product
// security. CONTROL_RAW_* records the unbounded baseline leak that
// later ACTs must remove.
type Evidence struct {
	SchemaVersion                  int    `json:"schema_version"`
	JJVersion                      string `json:"jj_version"`
	GitVersion                     string `json:"git_version"`
	GoVersion                      string `json:"go_version"`
	OS                             string `json:"os"`
	Arch                           string `json:"arch"`
	ControlRawGitPush              bool   `json:"control_raw_git_push"`
	ControlRawJJPush               bool   `json:"control_raw_jj_push"`
	LabRemoteIsLocal               bool   `json:"lab_remote_is_local"`
	RealOriginNotReferenced        bool   `json:"real_origin_not_referenced"`
	HomeCredentialNotNeeded        bool   `json:"home_credential_not_needed"`
	TransportFailureObserved       bool   `json:"transport_failure_observed"`
	FailedTransportRemoteUnchanged bool   `json:"failed_transport_remote_unchanged"`
	RemoteGitStateVerified         bool   `json:"remote_git_state_verified"`
	RemoteJJStateVerified          bool   `json:"remote_jj_state_verified"`
	FailureReason                  string `json:"failure_reason,omitempty"`
}

// Lab is a constructed publication laboratory.
//
// It owns a temporary directory and the paths to its fixture components.
// Use Setup to construct, defer Cleanup to remove.
type Lab struct {
	Root      string
	Remote    string // bare git remote
	Seed      string // seed repository
	GitClient string // raw git client clone
	JJClient  string // jj colocated workspace
	Home      string // scratch HOME for child processes (no credentials)

	realOrigin string // captured at Setup time so we can prove it is untouched
}

// Cleanup removes the lab root. Safe to call multiple times.
func (l *Lab) Cleanup() {
	if l == nil || l.Root == "" {
		return
	}
	_ = os.RemoveAll(l.Root)
	l.Root = ""
}

// RealOrigin returns the developer's real GitHub origin URL captured at
// Setup time, or empty string if it could not be determined.
func (l *Lab) RealOrigin() string { return l.realOrigin }

// IsLocalRemoteURL reports whether the given URL refers to a local
// filesystem path (which is what the lab uses). It accepts absolute
// paths and file:// URLs.
func IsLocalRemoteURL(u string) bool {
	if u == "" {
		return false
	}
	if strings.HasPrefix(u, "file://") {
		return true
	}
	if strings.HasPrefix(u, "/") {
		return true
	}
	return false
}

// GoVersion is the Go runtime version running this lab.
func GoVersion() string { return runtime.Version() }

// requireTool returns an actionable prerequisite error if the named
// executable is not available on PATH. Production callers may use it
// to fail a workflow before invoking subprocess tooling.
//
// Test-side fail-closed machinery (requireToolTB / RequireGitAndJJ)
// lives in internal/lab/prereq_test.go and is not part of this
// package's public surface.
func requireTool(name string) error {
	if _, err := exec.LookPath(name); err != nil {
		return err
	}
	return nil
}
