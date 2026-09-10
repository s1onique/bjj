package check

import (
	"sort"
)

// Aggregate produces the canonical CheckResult from a list of
// CheckOutcomes for the given subject.
//
// Aggregate is pure Go. Two runs with the same inputs (including
// the same ordering of outcomes) produce byte-identical output.
// The aggregator:
//
//   - sorts outcomes by CheckID (stable, deterministic)
//
//   - does NOT depend on map iteration
//
//   - does NOT include wall-clock duration or workspace path
//     (these are diagnostic-only fields on CheckOutcome and are
//     json:"-" on the canonical body)
//
//   - folds outcomes into a single aggregate status:
//
//     any ERROR -> error
//     else any FAIL -> fail
//     else -> pass
//
// Aggregate MUST NOT mutate its inputs.
//
// FileCount is the number of entries in the workspace manifest,
// or zero if manifest is nil. It is a diagnostic field that lets
// the caller detect an unexpected empty materialization.
func Aggregate(subject SubjectIdentity, manifest *Manifest, outcomes []CheckOutcome) CheckResult {
	cp := make([]CheckOutcome, len(outcomes))
	copy(cp, outcomes)
	sort.SliceStable(cp, func(i, j int) bool { return cp[i].ID < cp[j].ID })

	var fc int
	if manifest != nil {
		fc = len(manifest.Files)
	}

	overall := StatusPass
	for _, o := range cp {
		switch o.Status {
		case StatusError:
			overall = StatusError
		case StatusFail:
			if overall != StatusError {
				overall = StatusFail
			}
		}
	}

	return CheckResult{
		SchemaVersion: SchemaVersion,
		Status:        overall,
		Subject:       subject,
		Checks:        cp,
		FileCount:     fc,
	}
}
