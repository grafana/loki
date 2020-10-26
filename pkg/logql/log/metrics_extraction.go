package log

import (
	"sort"
	"strconv"
	"time"

	"github.com/pkg/errors"
	"github.com/prometheus/prometheus/pkg/labels"
)

const (
	ConvertDuration = "duration"
	ConvertFloat    = "float"
)

// SampleExtractor extracts sample for a log line.
type SampleExtractor interface {
	Process(line []byte, lbs labels.Labels) (float64, labels.Labels, bool)
}

type SampleExtractorFunc func(line []byte, lbs labels.Labels) (float64, labels.Labels, bool)

func (fn SampleExtractorFunc) Process(line []byte, lbs labels.Labels) (float64, labels.Labels, bool) {
	return fn(line, lbs)
}

// LineExtractor extracts a float64 from a log line.
type LineExtractor func([]byte) float64

// ToSampleExtractor transform a LineExtractor into a SampleExtractor.
// Useful for metric conversion without log Pipeline.
func (l LineExtractor) ToSampleExtractor(groups []string, without bool, noLabels bool) SampleExtractor {
	return SampleExtractorFunc(func(line []byte, lbs labels.Labels) (float64, labels.Labels, bool) {
		// todo(cyriltovena) grouping should be done once per stream/chunk not for everyline.
		// so for now we'll cover just vector without grouping. This requires changes to SampleExtractor interface.
		// For another day !
		if len(groups) == 0 && noLabels {
			return l(line), labels.Labels{}, true
		}
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

	groups   []string
	without  bool
	noLabels bool
	builder  *LabelsBuilder
}

func (l lineSampleExtractor) Process(line []byte, lbs labels.Labels) (float64, labels.Labels, bool) {
	l.builder.Reset(lbs)
	line, ok := l.Stage.Process(line, l.builder)
	if !ok {
		return 0, nil, false
	}
	if len(l.groups) != 0 {
		if l.without {
			return l.LineExtractor(line), l.builder.WithoutLabels(l.groups...), true
		}
		return l.LineExtractor(line), l.builder.WithLabels(l.groups...), true
	}
	if l.noLabels {
		// no grouping but it was a vector operation so we return a single vector
		return l.LineExtractor(line), labels.Labels{}, true
	}
	return l.LineExtractor(line), l.builder.Labels(), true
}

// LineExtractorWithStages creates a SampleExtractor from a LineExtractor.
// Multiple log stages are run before converting the log line.
func LineExtractorWithStages(ex LineExtractor, stages []Stage, groups []string, without bool, noLabels bool) (SampleExtractor, error) {
	if len(stages) == 0 {
		return ex.ToSampleExtractor(groups, without, noLabels), nil
	}
	return lineSampleExtractor{
		Stage:         ReduceStages(stages),
		LineExtractor: ex,
		builder:       NewLabelsBuilder(),
		groups:        groups,
		without:       without,
		noLabels:      noLabels,
	}, nil
}

type convertionFn func(value string) (float64, error)

type labelSampleExtractor struct {
	preStage   Stage
	postFilter Stage
	builder    *LabelsBuilder

	labelName    string
	conversionFn convertionFn
	groups       []string
	without      bool
	noLabels     bool
}

// LabelExtractorWithStages creates a SampleExtractor that will extract metrics from a labels.
// A set of log stage is executed before the conversion. A Filtering stage is executed after the conversion allowing
// to remove sample containing the __error__ label.
func LabelExtractorWithStages(
	labelName, conversion string,
	groups []string, without bool, noLabels bool,
	preStages []Stage,
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
	if len(groups) != 0 && without {
		groups = append(groups, labelName)
		sort.Strings(groups)
	}
	return &labelSampleExtractor{
		preStage:     ReduceStages(preStages),
		conversionFn: convFn,
		groups:       groups,
		labelName:    labelName,
		postFilter:   postFilter,
		without:      without,
		builder:      NewLabelsBuilder(),
		noLabels:     noLabels,
	}, nil
}

func (l *labelSampleExtractor) Process(line []byte, lbs labels.Labels) (float64, labels.Labels, bool) {
	// Apply the pipeline first.
	l.builder.Reset(lbs)
	line, ok := l.preStage.Process(line, l.builder)
	if !ok {
		return 0, nil, false
	}
	// convert the label value.
	var v float64
	stringValue, _ := l.builder.Get(l.labelName)
	if stringValue == "" {
		l.builder.SetErr(errSampleExtraction)
	} else {
		var err error
		v, err = l.conversionFn(stringValue)
		if err != nil {
			l.builder.SetErr(errSampleExtraction)
		}
	}
	// post filters
	if _, ok = l.postFilter.Process(line, l.builder); !ok {
		return 0, nil, false
	}
	if l.builder.HasErr() {
		// we still have an error after post filtering.
		// We need to return now before applying grouping otherwise the error might get lost.
		return v, l.builder.Labels(), true
	}
	return v, l.groupLabels(l.builder), true
}

func (l *labelSampleExtractor) groupLabels(lbs *LabelsBuilder) labels.Labels {
	if len(l.groups) != 0 {
		if l.without {
			return lbs.WithoutLabels(l.groups...)
		}
		return lbs.WithLabels(l.groups...)
	}
	if l.noLabels {
		return labels.Labels{}
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
