package log

import (
	"strconv"
	"time"

	"github.com/pkg/errors"
	"github.com/prometheus/prometheus/pkg/labels"
)

type SampleExtractor interface {
	Process(line []byte, lbs labels.Labels) (float64, labels.Labels, bool)
}

type SampleExtractorFunc func(line []byte, lbs labels.Labels) (float64, labels.Labels, bool)

func (fn SampleExtractorFunc) Process(line []byte, lbs labels.Labels) (float64, labels.Labels, bool) {
	return fn(line, lbs)
}

type LineExtractor func([]byte) float64

func (l LineExtractor) ToSampleExtractor() SampleExtractor {
	return SampleExtractorFunc(func(line []byte, lbs labels.Labels) (float64, labels.Labels, bool) {
		return l(line), lbs, true
	})
}

var (
	CountExtractor LineExtractor = func(line []byte) float64 { return 1. }
	BytesExtractor LineExtractor = func(line []byte) float64 { return float64(len(line)) }
)

type lineSampleExtractor struct {
	Stage
	LineExtractor
}

func (l lineSampleExtractor) Process(line []byte, lbs labels.Labels) (float64, labels.Labels, bool) {
	labelmap := lbs.Map()
	line, ok := l.Stage.Process(line, labelmap)
	if !ok {
		return 0, nil, false
	}
	return l.LineExtractor(line), labels.FromMap(labelmap), true
}

func (m MultiStage) WithLineExtractor(ex LineExtractor) (SampleExtractor, error) {
	if len(m) == 0 {
		return ex.ToSampleExtractor(), nil
	}
	return lineSampleExtractor{Stage: m.Reduce(), LineExtractor: ex}, nil
}

type convertionFn func(value string) (float64, error)

type labelSampleExtractor struct {
	preStage   Stage
	postFilter Stage

	labelName    string
	conversionFn convertionFn
	groups       []string
	without      bool
}

const (
	ConvertDuration = "duration"
	ConvertFloat    = "float"
)

func (m MultiStage) WithLabelExtractor(
	labelName, conversion string,
	groups []string, without bool,
	postFilter Stage,
) (SampleExtractor, error) {
	var convFn convertionFn
	switch conversion {
	case ConvertDuration:
		convFn = convertDuration
	case ConvertFloat:
		convFn = convertFloat
	default:
		return nil, errors.Errorf("unsupported conversion operation %s", conversion)
	}
	return &labelSampleExtractor{
		preStage:     m.Reduce(),
		conversionFn: convFn,
		groups:       groups,
		labelName:    labelName,
		postFilter:   postFilter,
		without:      without,
	}, nil
}

func (l *labelSampleExtractor) Process(line []byte, lbs labels.Labels) (float64, labels.Labels, bool) {
	// apply pipeline
	labelmap := Labels(lbs.Map())
	line, ok := l.preStage.Process(line, labelmap)
	if !ok {
		return 0, nil, false
	}
	// convert
	var v float64
	stringValue := labelmap[l.labelName]
	if stringValue == "" {
		labelmap.SetError(errSampleExtraction)
	} else {
		var err error
		v, err = l.conversionFn(stringValue)
		if err != nil {
			labelmap.SetError(errSampleExtraction)
		}
	}
	// post filters
	if _, ok = l.postFilter.Process(line, labelmap); !ok {
		return 0, nil, false
	}
	if labelmap.HasError() {
		// we still have an error after post filtering.
		// We need to return now before applying grouping otherwise the error might get lost.
		return v, labels.FromMap(labelmap), true
	}
	return v, l.groupLabels(labels.FromMap(labelmap)), true
}

func (l *labelSampleExtractor) groupLabels(lbs labels.Labels) labels.Labels {
	if l.groups != nil {
		if l.without {
			return lbs.WithoutLabels(append(l.groups, l.labelName)...)
		}
		return lbs.WithLabels(l.groups...)
	}
	return lbs.WithoutLabels(l.labelName)
}

func convertFloat(v string) (float64, error) {
	return strconv.ParseFloat(v, 64)
}

func convertDuration(v string) (float64, error) {
	d, err := time.ParseDuration(v)
	if err != nil {
		return 0, err
	}
	return d.Seconds(), nil
}
