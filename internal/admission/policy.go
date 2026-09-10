package admission

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

// PolicyFileName is the canonical repository-local BJJ policy
// file name. ACT-BJJ-ADMISSION01 §8 documents ".bjj/policy.toml"
// as a possible location; we standardise on .bjj/policy.toml.
//
// The file MUST live inside the repository so that per-repository
// JJ_CONFIG and other environmental settings do not silently
// affect admission decisions (§10).
const PolicyFileName = ".bjj/policy.toml"

// PolicyLoadOutcome describes how a Policy was loaded.
//
// Per §23:
//
//	POLICY_ABSENT    -> DEFAULT_POLICY
//	POLICY_MALFORMED -> FAIL_CLOSED
type PolicyLoadOutcome string

const (
	// PolicyOutcomeDefault means no BJJ policy file exists; the
	// built-in DefaultPolicy was used.
	PolicyOutcomeDefault PolicyLoadOutcome = "DEFAULT_POLICY"

	// PolicyOutcomeLoaded means a BJJ policy file was found and
	// parsed successfully.
	PolicyOutcomeLoaded PolicyLoadOutcome = "LOADED"

	// PolicyOutcomeMalformed means a BJJ policy file was found
	// but could not be parsed; admission refuses to fall back to
	// defaults.
	PolicyOutcomeMalformed PolicyLoadOutcome = "MALFORMED"
)

// PolicySource describes the origin of a loaded Policy. It is
// returned by LoadPolicyAtWithOpID / LoadPolicyFromRepo so
// callers can assert ADMISSION_AMBIENT_JJ_POLICY_INDEPENDENT.
//
// Per §28, the BJJ policy file is CANDIDATE-LOCAL. It does NOT
// constitute authoritative policy; future ACTs bind authoritative
// policy outside the candidate's unilateral control.
//
// CORRECTION03 §4: the Source carries the (operation, revision)
// pair the bytes were actually read from, so diagnostic output
// and audit trails can prove the policy matched the publication
// subject rather than the live working copy.
type PolicySource struct {
	// Outcome is DEFAULT_POLICY | LOADED | MALFORMED.
	Outcome PolicyLoadOutcome `json:"outcome"`

	// Path is the policy file path (".bjj/policy.toml"
	// relative to the candidate tree), or empty when Outcome is
	// DEFAULT_POLICY.
	Path string `json:"path,omitempty"`

	// OperationID is the pinned Jujutsu operation view the
	// policy bytes were read from. Empty for the DEFAULT_POLICY
	// case.
	OperationID string `json:"operation_id,omitempty"`

	// Revision is the pinned Jujutsu revision the policy bytes
	// were read from. Per CORRECTION03 §4 this is the
	// publication NEW (PublishPlan.BookmarkMoves[0].New.CommitID).
	// Empty for the DEFAULT_POLICY case.
	Revision string `json:"revision,omitempty"`
}

// PolicyFilePresence is a typed discriminator for "file is in
// the candidate tree" vs "file is missing from the candidate
// tree" vs "infrastructure failure reading the file".
//
// CORRECTION03 requires this distinction be explicit: the
// DEFAULT_POLICY case applies only when the file is genuinely
// absent from the frozen candidate, not when the live
// filesystem cannot be read.
type PolicyFilePresence string

const (
	// PolicyFilePresencePresent means the policy file exists in
	// the candidate tree and the bytes are returned.
	PolicyFilePresencePresent PolicyFilePresence = "PRESENT"

	// PolicyFilePresenceAbsent means the policy file does not
	// exist in the candidate tree at the pinned view. NOT an
	// error; resolves to DefaultPolicy.
	PolicyFilePresenceAbsent PolicyFilePresence = "ABSENT"
)

// PolicyReader is the bounded dependency admission uses to read
// repository-local files (currently just the policy) from a
// pinned Jujutsu operation view.
//
// CORRECTION03 §2 mandates this interface: the production
// admission path MUST NOT consult the live filesystem. Reading
// .bjj/policy.toml from os.ReadFile(<dir>/...) would race any
// other process or agent that mutates the working tree, and
// could produce a false admit (or false deny) when the policy on
// disk diverges from the policy in the frozen candidate.
//
// Implementations MUST:
//
//   - return ABSENT with nil bytes and nil error when the file
//     does not exist in the pinned view;
//   - return PRESENT with the file bytes when the file does
//     exist in the pinned view;
//   - return a non-nil error for any infrastructure failure
//     (corrupt repo, adapter error, parser anomaly). Returning
//     ABSENT in that case would silently weaken admission.
//
// The opID and revision arguments are explicit by design: every
// policy read is bound to the same Jujutsu view that produced
// the publication subject, so the policy cannot quietly diverge
// from the candidate.
type PolicyReader interface {
	// ReadPolicyFile returns the contents of path as visible at
	// (opID, revision), together with a typed presence
	// indicator.
	//
	// opID MUST be the operation view the PublishPlan was
	// resolved at; revision MUST be the publication NEW
	// (PublishPlan.BookmarkMoves[0].New.CommitID).
	ReadPolicyFile(ctx context.Context, dir, opID, revision, path string) ([]byte, PolicyFilePresence, error)
}

// LoadPolicyAt loads the BJJ admission policy from a frozen
// Jujutsu view via reader.
//
// CORRECTION03 §4: the policy bytes are read from revision as
// visible at opID — exactly the same view that produced the
// PublishPlan. A live-working-copy mutation between plan
// resolution and policy load CANNOT influence admission.
//
// Contract (identical to LoadPolicyFromRepo on the parse path):
//
//   - ABSENT in pinned view -> DefaultPolicy(),
//     PolicyOutcomeDefault, nil
//   - PRESENT + parseable   -> parsed Policy,
//     PolicyOutcomeLoaded, nil
//   - PRESENT + malformed   -> nil, PolicyOutcomeMalformed,
//     *Error{CodePolicyInvalid}
//   - infrastructure failure -> nil, PolicySource{}, wrapped
//     error (never silently resolves to DEFAULT_POLICY)
//
// planView MUST be non-nil and carry at least one BookmarkMove
// with a non-nil New.CommitID; an empty planView is a structural
// bug and is rejected with CodeNoBookmarkMove so it cannot be
// confused with the absent-file case.
//
// opID MUST be the pinned operation id the PublishPlan was
// resolved at. Pass empty to opt out: that route is rejected
// with CodeInconsistentView so a caller cannot accidentally fall
// back to the live operation view.
func LoadPolicyAt(ctx context.Context, reader PolicyReader, planView PlanView, dir, opID string) (Policy, PolicySource, error) {
	return LoadPolicyAtWithOpID(ctx, reader, planView, dir, opID)
}

// LoadPolicyAtWithOpID loads the BJJ admission policy from a
// frozen Jujutsu view via reader.
//
// CORRECTION03 §4: the policy bytes are read from revision as
// visible at opID — exactly the same view that produced the
// PublishPlan. A live-working-copy mutation between plan
// resolution and policy load CANNOT influence admission.
//
// Contract (identical to LoadPolicyFromRepo on the parse path):
//
//   - ABSENT in pinned view -> DefaultPolicy(),
//     PolicyOutcomeDefault, nil
//   - PRESENT + parseable   -> parsed Policy,
//     PolicyOutcomeLoaded, nil
//   - PRESENT + malformed   -> nil, PolicyOutcomeMalformed,
//     *Error{CodePolicyInvalid}
//   - infrastructure failure -> nil, PolicySource{}, wrapped
//     error (never silently resolves to DEFAULT_POLICY)
//
// planView MUST be non-nil and carry at least one BookmarkMove
// with a non-nil New.CommitID; an empty planView is a structural
// bug and is rejected with CodeNoBookmarkMove so it cannot be
// confused with the absent-file case.
func LoadPolicyAtWithOpID(
	ctx context.Context,
	reader PolicyReader,
	planView PlanView,
	dir string,
	opID string,
) (Policy, PolicySource, error) {
	if reader == nil {
		return Policy{}, PolicySource{}, NewError(CodePolicyInvalid,
			"admission: nil PolicyReader; refusing to load policy", nil)
	}
	if opID == "" {
		return Policy{}, PolicySource{}, NewError(CodeInconsistentView,
			"admission: empty opID; cannot bind policy to live operation", nil)
	}
	if planView == nil {
		return Policy{}, PolicySource{}, NewError(CodeNoBookmarkMove,
			"admission: nil plan view; cannot bind policy to NEW", nil)
	}
	if planView.NewCommitID() == "" {
		return Policy{}, PolicySource{}, NewError(CodeNoBookmarkMove,
			"admission: plan view has empty NEW; cannot bind policy", nil)
	}

	revision := planView.NewCommitID()
	body, presence, err := reader.ReadPolicyFile(ctx, dir, opID, revision, PolicyFileName)
	if err != nil {
		return Policy{}, PolicySource{}, NewError(CodeJJQueryFailed,
			fmt.Sprintf("admission: read %s @ op=%s rev=%s: %v",
				PolicyFileName, opID, revision, err), err)
	}
	switch presence {
	case PolicyFilePresenceAbsent:
		return DefaultPolicy(), PolicySource{
			Outcome: PolicyOutcomeDefault,
		}, nil
	case PolicyFilePresencePresent:
		tagged := PolicyFileName + "@" + opID + ":" + revision
		p, perr := parsePolicyTOML(tagged, body)
		if perr != nil {
			return Policy{}, PolicySource{
				Outcome:     PolicyOutcomeMalformed,
				Path:        PolicyFileName,
				OperationID: opID,
				Revision:    revision,
			}, perr
		}
		return p, PolicySource{
			Outcome:     PolicyOutcomeLoaded,
			Path:        PolicyFileName,
			OperationID: opID,
			Revision:    revision,
		}, nil
	default:
		return Policy{}, PolicySource{}, NewError(CodeJJQueryFailed,
			fmt.Sprintf("admission: %s @ op=%s rev=%s returned unknown presence %q",
				PolicyFileName, opID, revision, presence), nil)
	}
}

// LoadPolicyFromRepo loads the BJJ admission policy from the
// LIVE filesystem at repoPath.
//
// CORRECTION03 §8: this function is intentionally retained only
// for parser unit tests (which exercise the byte-level
// invariants of the bounded TOML subset) and for callers that
// explicitly need the ambient policy bytes outside of an
// admission decision. The PRODUCTION admission decision path
// MUST use LoadPolicyAtWithOpID (or an equivalent opID-bound
// reader), never this function.
//
// Live-filesystem reads are dangerous because they decouple
// the policy from the Jujutsu view that produced the plan: a
// concurrent mutation between plan resolution and policy load
// could cause a false admit/deny. See CORRECTION03 §1.
//
// Lookup order:
//
//  1. <repoPath>/.bjj/policy.toml   (canonical location)
//
// Per §23:
//
//   - file absent   -> DefaultPolicy(), PolicyOutcomeDefault, nil
//   - file present + parseable -> parsed Policy, PolicyOutcomeLoaded, nil
//   - file present + malformed -> nil, PolicyOutcomeMalformed, *Error{CodePolicyInvalid}
//
// A malformed policy NEVER falls back to defaults. The caller
// must surface the error to the user.
func LoadPolicyFromRepo(repoPath string) (Policy, PolicySource, error) {
	if repoPath == "" {
		return Policy{}, PolicySource{}, NewError(CodePolicyInvalid,
			"empty repoPath; refusing to guess policy location", nil)
	}
	candidate := repoPath + "/" + PolicyFileName
	body, err := os.ReadFile(candidate)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return DefaultPolicy(), PolicySource{
				Outcome: PolicyOutcomeDefault,
			}, nil
		}
		return Policy{}, PolicySource{}, NewError(CodePolicyInvalid,
			fmt.Sprintf("read %s: %v", candidate, err), err)
	}

	p, perr := parsePolicyTOML(candidate, body)
	if perr != nil {
		return Policy{}, PolicySource{
			Outcome: PolicyOutcomeMalformed,
			Path:    candidate,
		}, perr
	}
	return p, PolicySource{
		Outcome: PolicyOutcomeLoaded,
		Path:    candidate,
	}, nil
}

// parsePolicyTOML parses the bounded TOML subset of BJJ's policy
// file.
//
// The supported schema is intentionally small (ACT-BJJ-ADMISSION01
// §8, §24, CORRECTION01 §4, CORRECTION02 §1, §2). Three
// fail-closed rules apply:
//
//   - Unknown keys FAIL CLOSED (CORRECTION01 §4): under
//     schema_version = 1 any key the parser does not recognise
//     is a hard POLICY_INVALID error. This prevents typos like
//     "private_commmits" from silently weakening the policy.
//   - Duplicate keys FAIL CLOSED (CORRECTION02 §1): every key,
//     recognised or not, may appear at most once. The bounded
//     TOML subset is therefore STRICTER than TOML itself, which
//     would normally pick one of the duplicates silently. This
//     prevents "private_commits = \"X\"" followed by
//     "private_commits = \"none()\"" from quietly weakening a
//     private-commits rule.
//   - schema_version is REQUIRED (CORRECTION02 §2): when the
//     file is present, it must contain exactly one
//     `schema_version = 1` line. Files that omit the version are
//     POLICY_INVALID even if all other keys are valid. This
//     makes schema_version an explicit schema boundary that
//     future migrations can rely on.
//
// Unknown values are also rejected: a non-bool allow_* field or a
// malformed private_commits string yields *Error{CodePolicyInvalid}.
//
// We implement a tiny purpose-built parser rather than pulling in
// a TOML dependency, in line with the ACT's preference for a
// minimal in-repo representation.
func parsePolicyTOML(path string, body []byte) (Policy, error) {
	saw := map[string]bool{}
	p := DefaultPolicy()
	// schemaSeen tracks whether the policy file declared an
	// explicit `schema_version` line. Per CORRECTION02 §2 a
	// present file MUST declare exactly one schema_version; the
	// absence of that line is POLICY_INVALID.
	schemaSeen := false

	scanner := newLineScanner(bytes.NewReader(body))
	for scanner.scanLine() {
		line := strings.TrimSpace(scanner.line())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// We accept only flat "key = value" lines.
		eq := strings.Index(line, "=")
		if eq <= 0 {
			return Policy{}, NewError(CodePolicyInvalid,
				fmt.Sprintf("%s: malformed policy line: %q", path, line), nil)
		}
		key := strings.TrimSpace(line[:eq])
		val := strings.TrimSpace(line[eq+1:])
		// Strip surrounding quotes from string values.
		val = unquoteTOML(val)

		// CORRECTION02 §1: duplicate-key detection. Every key,
		// recognised or unknown, may occur at most once.
		if saw[key] {
			return Policy{}, NewError(CodePolicyInvalid,
				fmt.Sprintf("%s: duplicate policy key %q", path, key), nil)
		}
		saw[key] = true

		switch key {
		case "schema_version":
			// schema_version is REQUIRED and must be exactly "1"
			// (CORRECTION02 §2). Repeating the key is caught by
			// the duplicate-key check above.
			if val != "1" {
				return Policy{}, NewError(CodePolicyInvalid,
					fmt.Sprintf("%s: unsupported schema_version %q", path, val), nil)
			}
			schemaSeen = true
		case "allow_new_bookmarks":
			b, err := parseBool(val)
			if err != nil {
				return Policy{}, NewError(CodePolicyInvalid,
					fmt.Sprintf("%s: allow_new_bookmarks: %v", path, err), err)
			}
			p.AllowNewBookmarks = b
		case "allow_non_fast_forward":
			b, err := parseBool(val)
			if err != nil {
				return Policy{}, NewError(CodePolicyInvalid,
					fmt.Sprintf("%s: allow_non_fast_forward: %v", path, err), err)
			}
			p.AllowNonFastForward = b
		case "require_description":
			b, err := parseBool(val)
			if err != nil {
				return Policy{}, NewError(CodePolicyInvalid,
					fmt.Sprintf("%s: require_description: %v", path, err), err)
			}
			p.RequireDescription = b
		case "allow_conflicted_commits":
			b, err := parseBool(val)
			if err != nil {
				return Policy{}, NewError(CodePolicyInvalid,
					fmt.Sprintf("%s: allow_conflicted_commits: %v", path, err), err)
			}
			p.AllowConflictedCommits = b
		case "private_commits":
			if val == "" {
				return Policy{}, NewError(CodePolicyInvalid,
					fmt.Sprintf("%s: private_commits must be non-empty", path), nil)
			}
			p.PrivateCommits = val
		default:
			// Unknown keys: FAIL CLOSED.
			//
			// schema_version is the explicit forward-compatibility
			// boundary. Under schema_version = 1 every key the parser
			// does not recognise is treated as a hard POLICY_INVALID
			// error so that typos like "private_commmits" cannot
			// silently weaken the policy by leaving a default in
			// place of the intended rule.
			//
			// To extend the schema, increment schema_version and
			// teach this parser about the new key deliberately.
			return Policy{}, NewError(CodePolicyInvalid,
				fmt.Sprintf("%s: unknown policy key %q (schema_version=%d)", path, key, p.SchemaVersion), nil)
		}
	}
	if err := scanner.err(); err != nil && !errors.Is(err, io.EOF) {
		return Policy{}, NewError(CodePolicyInvalid,
			fmt.Sprintf("%s: read: %v", path, err), err)
	}
	// CORRECTION02 §2: schema_version is REQUIRED. A file that
	// declares every other key but omits schema_version is
	// POLICY_INVALID. Note that this only fires for PRESENT files;
	// the absent-file case is handled by LoadPolicyFromRepo
	// returning DefaultPolicy.
	if !schemaSeen {
		return Policy{}, NewError(CodePolicyInvalid,
			fmt.Sprintf("%s: policy file is missing required key %q", path, "schema_version"), nil)
	}
	if p.PrivateCommits == "" {
		return Policy{}, NewError(CodePolicyInvalid,
			fmt.Sprintf("%s: private_commits must resolve to a revset expression", path), nil)
	}
	return p, nil
}

func parseBool(s string) (bool, error) {
	switch s {
	case "true", "True", "TRUE":
		return true, nil
	case "false", "False", "FALSE":
		return false, nil
	default:
		return false, fmt.Errorf("expected true|false, got %q", s)
	}
}

// unquoteTOML strips a single pair of surrounding double quotes
// from s. It does NOT interpret escape sequences; BJJ's policy
// schema intentionally avoids characters that need escaping.
func unquoteTOML(s string) string {
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return s[1 : len(s)-1]
	}
	return s
}

// lineScanner is a tiny line reader that records the most recent
// line so callers can attribute parse errors precisely.
type lineScanner struct {
	r        *bytes.Reader
	cur      string
	readErr  error
	finished bool
}

func newLineScanner(r io.Reader) *lineScanner {
	if br, ok := r.(*bytes.Reader); ok {
		return &lineScanner{r: br}
	}
	// Fallback: buffer the entire reader.
	b, _ := io.ReadAll(r)
	return &lineScanner{r: bytes.NewReader(b)}
}

func (s *lineScanner) scanLine() bool {
	if s.finished {
		return false
	}
	var buf bytes.Buffer
	for {
		b, err := s.r.ReadByte()
		if err != nil {
			if errors.Is(err, io.EOF) {
				s.finished = true
				s.cur = buf.String()
				return buf.Len() > 0
			}
			s.readErr = err
			s.finished = true
			s.cur = buf.String()
			return buf.Len() > 0
		}
		if b == '\n' {
			s.cur = buf.String()
			return true
		}
		buf.WriteByte(b)
	}
}

func (s *lineScanner) line() string { return s.cur }
func (s *lineScanner) err() error   { return s.readErr }
