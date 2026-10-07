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

	// errValues holds every value a log pipeline sets __error__ to. A filter that accepts an empty
	// value asks to keep the errored lines only when it also accepts one of these, so leaving a new
	// error out makes such a filter fail the query instead.
	errValues = []string{errJSON, errLogfmt, errSampleExtraction, errLabelFilter, errTemplateFormat}
)

// errorLabelHints reports the StageHints.ReadsErrorLabel and StageHints.KeepsErroredLines of a
// label filter that compares m and passes a line when passes reports true for the label's value.
func errorLabelHints(m *labels.Matcher, passes func(string) bool) (readsError, keepsError bool) {
	if m == nil {
		return false, false
	}

	// A comparison against __error_details__ reads an error label, but it never asks to keep the
	// errored lines. The filter reads the label set, which holds no error details, so the
	// comparison cannot name an error.
	if m.Name == logqlmodel.ErrorDetailsLabel {
		return true, false
	}

	if m.Name != logqlmodel.ErrorLabel {
		return false, false
	}

	// A comparison that rejects an empty value accepts only a non-empty one, so it asks for the
	// errored lines whatever error they carry.
	if !passes("") {
		return true, true
	}

	for _, v := range errValues {
		if passes(v) {
			return true, true
		}
	}
	return true, false
}
