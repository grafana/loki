package log

import (
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/logqlmodel"
)

var (
	// Possible errors thrown by a log pipeline.
	errJSON             = "JSONParserErr"
	errLogfmt           = "LogfmtParserErr"
	errSampleExtraction = "SampleExtractionErr"
	errLabelFilter      = "LabelFilterErr"
	errTemplateFormat   = "TemplateFormatErr"

	// errValues holds every value a log pipeline sets __error__ to. Leaving a new error out of it
	// makes a filter naming that error fail the query instead of keeping the errored lines.
	errValues = []string{errJSON, errLogfmt, errSampleExtraction, errLabelFilter, errTemplateFormat}
)

// errorLabelHints reports the StageHints.ReadsErrorLabel and StageHints.KeepsErroredLines of a
// label filter that compares m and passes a line when passes reports true for the label's value.
func errorLabelHints(m *labels.Matcher, passes func(string) bool) (readsError, keepsError bool) {
	if m == nil || m.Name != logqlmodel.ErrorLabel {
		return false, false
	}

	for _, v := range errValues {
		if passes(v) {
			return true, true
		}
	}
	return true, false
}
