package logql

import (
	"strconv"

	"github.com/prometheus/prometheus/pkg/labels"
)

var (
	ExtractBytes = bytesSampleExtractor{}
	ExtractCount = countSampleExtractor{}
)

// SampleExtractor transforms a log entry into a sample.
// In case of failure the second return value will be false.
type SampleExtractor interface {
	Extract(line []byte, lbs labels.Labels) (float64, labels.Labels)
}

type countSampleExtractor struct{}

func (countSampleExtractor) Extract(line []byte, lbs labels.Labels) (float64, labels.Labels) {
	return 1., lbs
}

type bytesSampleExtractor struct{}

func (bytesSampleExtractor) Extract(line []byte, lbs labels.Labels) (float64, labels.Labels) {
	return float64(len(line)), lbs
}

type labelSampleExtractor struct {
	labelName string

	builder *labels.Builder
}

func (l *labelSampleExtractor) Extract(_ []byte, lbs labels.Labels) (float64, labels.Labels) {
	stringValue := lbs.Get(l.labelName)
	if stringValue == "" {
		// todo(cyriltovena) handle errors.
		return 0, lbs
	}
	f, err := strconv.ParseFloat(stringValue, 64)
	if err != nil {
		// todo(cyriltovena) handle errors.
		return 0, lbs
	}
	l.builder.Reset(lbs)
	l.builder.Del(l.labelName)
	return f, l.builder.Labels()
}

func newLabelSampleExtractor(labelName string) *labelSampleExtractor {
	return &labelSampleExtractor{
		labelName: labelName,
		builder:   labels.NewBuilder(nil),
	}
}
