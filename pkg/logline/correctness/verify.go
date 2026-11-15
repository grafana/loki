package correctness

import (
	"time"

	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/logline"
	"github.com/grafana/loki/v3/pkg/logline/hintprovider"
	"github.com/grafana/loki/v3/pkg/logline/verification"
)

// verificationReport summarizes whether provided hints cover actual query results.
type verificationReport struct {
	HintRanges              int
	TotalResults            int
	CoveredResults          int
	FalseNegatives          int
	FalsePositives          int
	FalseNegativeTimestamps []time.Time
	Correct                 bool
}

// verifyHints measures coverage and false-positive rate of pre-fetched hint
// ranges against the supplied result entries, per stream. It performs pure
// computation with no I/O.
func verifyHints(
	hintRanges []hintprovider.HintTimeRange,
	resultEntries []queryEntry,
) *verificationReport {
	entries := verificationEntries(resultEntries)
	r := verification.VerifyStreamEntries(hintRanges, entries)

	return &verificationReport{
		HintRanges:              r.HintRanges,
		TotalResults:            r.TotalEntries,
		CoveredResults:          r.CoveredEntries,
		FalseNegatives:          r.FalseNegatives,
		FalseNegativeTimestamps: r.FalseNegativeTimestamps,
		FalsePositives:          verification.CountStreamFalsePositives(hintRanges, entries),
		Correct:                 r.FalseNegatives == 0 && r.TotalEntries > 0,
	}
}

// verificationEntries pairs each entry's timestamp with its stream's
// fingerprint. Stream labels must be the stream's own labels, without parsed
// or structured metadata labels (categorize-labels responses).
func verificationEntries(entries []queryEntry) []verification.Entry {
	out := make([]verification.Entry, len(entries))
	var buf []byte
	for i, e := range entries {
		out[i].Timestamp = e.Timestamp
		out[i].Fingerprint, buf = logline.StreamFingerprint(labels.FromMap(e.StreamLabels), buf)
	}
	return out
}
