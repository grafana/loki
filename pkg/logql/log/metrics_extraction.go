package log

import (
	"sort"
	"strconv"
	"time"

	"github.com/pkg/errors"
	"github.com/prometheus/prometheus/pkg/labels"

	"github.com/dustin/go-humanize"
)

const (
	ConvertBytes    = "bytes"
	ConvertDuration = "duration"
	ConvertFloat    = "float"
)

// LineExtractor extracts a float64 from a log line.
type LineExtractor func([]byte) float64

var (
	CountExtractor LineExtractor = func(line []byte) float64 { return 1. }
	BytesExtractor LineExtractor = func(line []byte) float64 { return float64(len(line)) }
)

// SampleExtractor creates StreamSampleExtractor that can extract samples for a given log stream.
type SampleExtractor interface {
	ForStream(labels labels.Labels) StreamSampleExtractor
}

// StreamSampleExtractor extracts sample for a log line.
// A StreamSampleExtractor never mutate the received line.
type StreamSampleExtractor interface {
	Process(line []byte) (float64, LabelsResult, bool)
	ProcessString(line string) (float64, LabelsResult, bool)
}

type lineSampleExtractor struct {
	Stage
	LineExtractor

	baseBuilder      *BaseLabelsBuilder
	streamExtractors map[uint64]StreamSampleExtractor
}

// NewLineSampleExtractor creates a SampleExtractor from a LineExtractor.
// Multiple log stages are run before converting the log line.
func NewLineSampleExtractor(ex LineExtractor, stages []Stage, groups []string, without bool, noLabels bool) (SampleExtractor, error) {
	s := ReduceStages(stages)
	var expectedLabels []string
	if !without {
		expectedLabels = append(expectedLabels, s.RequiredLabelNames()...)
		expectedLabels = uniqueString(append(expectedLabels, groups...))
	}
	return &lineSampleExtractor{
		Stage:            s,
		LineExtractor:    ex,
		baseBuilder:      NewBaseLabelsBuilderWithGrouping(groups, expectedLabels, without, noLabels),
		streamExtractors: make(map[uint64]StreamSampleExtractor),
	}, nil
}

func (l *lineSampleExtractor) ForStream(labels labels.Labels) StreamSampleExtractor {
	hash := l.baseBuilder.Hash(labels)
	if res, ok := l.streamExtractors[hash]; ok {
		return res
	}

	res := &streamLineSampleExtractor{
		Stage:         l.Stage,
		LineExtractor: l.LineExtractor,
		builder:       l.baseBuilder.ForLabels(labels, hash),
	}
	l.streamExtractors[hash] = res
	return res
}

type streamLineSampleExtractor struct {
	Stage
	LineExtractor
	builder *LabelsBuilder
}

func (l *streamLineSampleExtractor) Process(line []byte) (float64, LabelsResult, bool) {
	// short circuit.
	if l.Stage == NoopStage {
		return l.LineExtractor(line), l.builder.GroupedLabels(), true
	}
	l.builder.Reset()
	line, ok := l.Stage.Process(line, l.builder)
	if !ok {
		return 0, nil, false
	}
	return l.LineExtractor(line), l.builder.GroupedLabels(), true
}

func (l *streamLineSampleExtractor) ProcessString(line string) (float64, LabelsResult, bool) {
	// unsafe get bytes since we have the guarantee that the line won't be mutated.
	return l.Process(unsafeGetBytes(line))
}

type convertionFn func(value string) (float64, error)

type labelSampleExtractor struct {
	preStage     Stage
	postFilter   Stage
	labelName    string
	conversionFn convertionFn

	baseBuilder      *BaseLabelsBuilder
	streamExtractors map[uint64]StreamSampleExtractor
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
	case ConvertBytes:
		convFn = convertBytes
	case ConvertDuration:
		convFn = convertDuration
	case ConvertFloat:
		convFn = convertFloat
	default:
		return nil, errors.Errorf("unsupported conversion operation %s", conversion)
	}
	if len(groups) == 0 || without {
		without = true
		groups = append(groups, labelName)
		sort.Strings(groups)
	}
	preStage := ReduceStages(preStages)
	var expectedLabels []string
	if !without {
		expectedLabels = append(expectedLabels, preStage.RequiredLabelNames()...)
		expectedLabels = append(expectedLabels, groups...)
		expectedLabels = append(expectedLabels, postFilter.RequiredLabelNames()...)
		expectedLabels = uniqueString(expectedLabels)
	}
	return &labelSampleExtractor{
		preStage:         preStage,
		conversionFn:     convFn,
		labelName:        labelName,
		postFilter:       postFilter,
		baseBuilder:      NewBaseLabelsBuilderWithGrouping(groups, expectedLabels, without, noLabels),
		streamExtractors: make(map[uint64]StreamSampleExtractor),
	}, nil
}

type streamLabelSampleExtractor struct {
	*labelSampleExtractor
	builder *LabelsBuilder
}

func (l *labelSampleExtractor) ForStream(labels labels.Labels) StreamSampleExtractor {
	hash := l.baseBuilder.Hash(labels)
	if res, ok := l.streamExtractors[hash]; ok {
		return res
	}

	res := &streamLabelSampleExtractor{
		labelSampleExtractor: l,
		builder:              l.baseBuilder.ForLabels(labels, hash),
	}
	l.streamExtractors[hash] = res
	return res
}

func (l *streamLabelSampleExtractor) Process(line []byte) (float64, LabelsResult, bool) {
	// Apply the pipeline first.
	l.builder.Reset()
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
	return v, l.builder.GroupedLabels(), true
}

func (l *streamLabelSampleExtractor) ProcessString(line string) (float64, LabelsResult, bool) {
	// unsafe get bytes since we have the guarantee that the line won't be mutated.
	return l.Process(unsafeGetBytes(line))
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

func convertBytes(v string) (float64, error) {
	b, err := humanize.ParseBytes(v)
	if err != nil {
		return 0, err
	}
	return float64(b), nil
}
