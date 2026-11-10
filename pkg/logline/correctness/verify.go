package correctness

import (
	"time"

	"github.com/grafana/loki/v3/pkg/logproto"

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
// ranges against the supplied result entries. It performs pure computation
// with no I/O.
func verifyHints(
	hintRanges []hintprovider.HintTimeRange,
	resultEntries []logproto.Entry,
) *verificationReport {
	timestamps := make([]time.Time, len(resultEntries))
	for i, e := range resultEntries {
		timestamps[i] = e.Timestamp
	}

	r := verification.VerifyEntries(hintRanges, timestamps)

	return &verificationReport{
		HintRanges:              r.HintRanges,
		TotalResults:            r.TotalEntries,
		CoveredResults:          r.CoveredEntries,
		FalseNegatives:          r.FalseNegatives,
		FalseNegativeTimestamps: r.FalseNegativeTimestamps,
		FalsePositives:          verification.CountFalsePositives(hintRanges, timestamps),
		Correct:                 r.FalseNegatives == 0 && r.TotalEntries > 0,
	}
}
