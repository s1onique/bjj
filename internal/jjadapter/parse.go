package jjadapter

// ParseBookmarkLines parses the bounded stdout of
// `jj bookmark list --all-remotes` into typed BookmarkRef rows.
//
// It is the exported form of parseBookmarkLines, intended for
// direct unit testing by callers that need to assert the parser
// fails closed on malformed machine output (per
// ACT-BJJ-PLAN01-CORRECTION02 §3).
func ParseBookmarkLines(prog string, argv []string, body []byte) ([]BookmarkRef, error) {
	return parseBookmarkLines(prog, argv, body)
}

// ParseCommitLines parses the bounded stdout of `jj log` into
// typed CommitRef rows. It is the exported form of
// parseCommitLines.
func ParseCommitLines(prog string, argv []string, body []byte) ([]CommitRef, error) {
	return parseCommitLines(prog, argv, body)
}

// ParseCommitObsLines parses the bounded stdout of `jj log` under
// CommitObsTemplate into typed CommitObs rows.
//
// Exported for direct unit testing by callers that need to assert
// the parser fails closed on malformed machine output (per
// ACT-BJJ-ADMISSION01 §35).
func ParseCommitObsLines(prog string, argv []string, body []byte) ([]CommitObs, error) {
	return parseCommitObsLines(prog, argv, body)
}
