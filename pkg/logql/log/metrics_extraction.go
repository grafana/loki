package log

import (
	"context"
	"math"
	"sort"
	"strconv"
	"time"

	"github.com/cespare/xxhash/v2"
	"github.com/dustin/go-humanize"
	"github.com/pkg/errors"
	"github.com/prometheus/prometheus/model/labels"
)

const (
	ConvertBytes    = "bytes"
	ConvertDuration = "duration"
	ConvertFloat    = "float"
)

// LineExtractor extracts a float64 from a log line.
type LineExtractor func([]byte) float64

var (
	CountExtractor LineExtractor = func(_ []byte) float64 { return 1. }
	BytesExtractor LineExtractor = func(line []byte) float64 { return float64(len(line)) }
)

// SampleExtractor creates StreamSampleExtractor that can extract samples for a given log stream.
type SampleExtractor interface {
	ForStream(labels labels.Labels) StreamSampleExtractor
}

// StreamSampleExtractor extracts at most one sample from a log line.
// A StreamSampleExtractor never mutates the received line.
type StreamSampleExtractor interface {
	BaseLabels() LabelsResult
	// Process extracts the sample for a log line. It returns the zero sample and
	// false when it extracts none. A true result always carries non-nil Labels.
	Process(ts int64, line []byte, structuredMetadata labels.Labels) (ExtractedSample, bool)
	// ProcessString extracts the sample for a log line. It returns the zero sample
	// and false when it extracts none. A true result always carries non-nil Labels.
	ProcessString(ts int64, line string, structuredMetadata labels.Labels) (ExtractedSample, bool)
	ReferencedStructuredMetadata() bool
}

// ExtractedSample is the sample a StreamSampleExtractor derives from a log line.
type ExtractedSample struct {
	Value  float64
	Labels LabelsResult
}

// SampleExtractorWrapper takes an extractor, wraps it is some desired functionality
// and returns a new pipeline
type SampleExtractorWrapper interface {
	Wrap(ctx context.Context, extractor SampleExtractor, query, tenant string) SampleExtractor
}

type lineSampleExtractor struct {
	Stage
	LineExtractor

	baseBuilder      *BaseLabelsBuilder
	streamExtractors map[uint64]StreamSampleExtractor
}

// NewLineSampleExtractor creates a SampleExtractor from a LineExtractor.
// Multiple log stages are run before converting the log line.
func NewLineSampleExtractor(ex LineExtractor, stages []Stage, groups []string, without, noLabels bool) (SampleExtractor, error) {
	s := ReduceStages(stages)
	hints := NewParserHint(s.RequiredLabelNames(), groups, without, noLabels, "", stages)
	return &lineSampleExtractor{
		Stage:            s,
		LineExtractor:    ex,
		baseBuilder:      NewBaseLabelsBuilderWithGrouping(groups, hints, without, noLabels),
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

func (l *streamLineSampleExtractor) ReferencedStructuredMetadata() bool {
	return l.builder.referencedStructuredMetadata
}

func (l *streamLineSampleExtractor) Process(ts int64, line []byte, structuredMetadata labels.Labels) (ExtractedSample, bool) {
	l.builder.Reset()
	l.builder.Add(StructuredMetadataLabel, structuredMetadata)

	// short circuit.
	if l.Stage == NoopStage {
		return ExtractedSample{Value: l.LineExtractor(line), Labels: l.builder.GroupedLabels()}, true
	}

	line, ok := l.Stage.Process(ts, line, l.builder)
	if !ok {
		return ExtractedSample{}, false
	}

	return ExtractedSample{Value: l.LineExtractor(line), Labels: l.builder.GroupedLabels()}, true
}

func (l *streamLineSampleExtractor) ProcessString(ts int64, line string, structuredMetadata labels.Labels) (ExtractedSample, bool) {
	// unsafe get bytes since we have the guarantee that the line won't be mutated.
	return l.Process(ts, unsafeGetBytes(line), structuredMetadata)
}

func (l *streamLineSampleExtractor) BaseLabels() LabelsResult { return l.builder.currentResult }

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
	groups []string, without, noLabels bool,
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
	hints := NewParserHint(append(preStage.RequiredLabelNames(), postFilter.RequiredLabelNames()...), groups, without, noLabels, labelName, append(preStages, postFilter))
	return &labelSampleExtractor{
		preStage:         preStage,
		conversionFn:     convFn,
		labelName:        labelName,
		postFilter:       postFilter,
		baseBuilder:      NewBaseLabelsBuilderWithGrouping(groups, hints, without, noLabels),
		streamExtractors: make(map[uint64]StreamSampleExtractor),
	}, nil
}

type streamLabelSampleExtractor struct {
	*labelSampleExtractor
	builder *LabelsBuilder
}

func (l *labelSampleExtractor) ReferencedStructuredMetadata() bool {
	return l.baseBuilder.referencedStructuredMetadata
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

func (l *streamLabelSampleExtractor) Process(ts int64, line []byte, structuredMetadata labels.Labels) (ExtractedSample, bool) {
	// Apply the pipeline first.
	l.builder.Reset()
	l.builder.Add(StructuredMetadataLabel, structuredMetadata)
	line, ok := l.preStage.Process(ts, line, l.builder)
	if !ok {
		return ExtractedSample{}, false
	}
	// convert the label value.
	var v float64
	stringValue, _ := l.builder.Get(l.labelName)
	if stringValue == "" {
		// NOTE: It's totally fine for log line to not have this particular label.
		// See Issue: https://github.com/grafana/loki/issues/6713
		return ExtractedSample{}, false
	}

	var err error
	v, err = l.conversionFn(stringValue)
	if err != nil {
		l.builder.SetErr(errSampleExtraction)
		l.builder.SetErrorDetails(err.Error())
	}

	// post filters
	if _, ok = l.postFilter.Process(ts, line, l.builder); !ok {
		return ExtractedSample{}, false
	}
	return ExtractedSample{Value: v, Labels: l.builder.GroupedLabels()}, true
}

func (l *streamLabelSampleExtractor) ProcessString(ts int64, line string, structuredMetadata labels.Labels) (ExtractedSample, bool) {
	// unsafe get bytes since we have the guarantee that the line won't be mutated.
	return l.Process(ts, unsafeGetBytes(line), structuredMetadata)
}

func (l *streamLabelSampleExtractor) BaseLabels() LabelsResult { return l.builder.currentResult }

// NewDistinctValueSampleExtractor hashes the raw string value of a label or
// extracted field into Sample.Value via xxhash64 / Float64frombits. Missing or
// empty values are skipped. Output labels are restricted to the grouping set.
func NewDistinctValueSampleExtractor(labelName string, stages []Stage, groups []string) (SampleExtractor, error) {
	if labelName == "" {
		return nil, errors.New("distinct value extractor requires a non-empty label name")
	}
	sort.Strings(groups)
	preStage := ReduceStages(stages)
	hints := NewParserHint(preStage.RequiredLabelNames(), groups, false, false, labelName, stages)
	return &distinctValueSampleExtractor{
		preStage:         preStage,
		labelName:        labelName,
		baseBuilder:      NewBaseLabelsBuilderWithGrouping(groups, hints, false, false),
		streamExtractors: make(map[uint64]StreamSampleExtractor),
	}, nil
}

type distinctValueSampleExtractor struct {
	preStage         Stage
	labelName        string
	baseBuilder      *BaseLabelsBuilder
	streamExtractors map[uint64]StreamSampleExtractor
}

type streamDistinctValueSampleExtractor struct {
	*distinctValueSampleExtractor
	builder *LabelsBuilder
}

func (d *distinctValueSampleExtractor) ForStream(labels labels.Labels) StreamSampleExtractor {
	hash := d.baseBuilder.Hash(labels)
	if res, ok := d.streamExtractors[hash]; ok {
		return res
	}
	res := &streamDistinctValueSampleExtractor{
		distinctValueSampleExtractor: d,
		builder:                      d.baseBuilder.ForLabels(labels, hash),
	}
	d.streamExtractors[hash] = res
	return res
}

func (d *distinctValueSampleExtractor) ReferencedStructuredMetadata() bool {
	return d.baseBuilder.referencedStructuredMetadata
}

func (d *streamDistinctValueSampleExtractor) Process(ts int64, line []byte, structuredMetadata labels.Labels) (ExtractedSample, bool) {
	d.builder.Reset()
	d.builder.Add(StructuredMetadataLabel, structuredMetadata)
	_, ok := d.preStage.Process(ts, line, d.builder)
	if !ok {
		return ExtractedSample{}, false
	}
	stringValue, found := d.builder.Get(d.labelName)
	if !found || stringValue == "" {
		return ExtractedSample{}, false
	}
	h := xxhash.Sum64(unsafeGetBytes(stringValue))
	return ExtractedSample{
		Value:  math.Float64frombits(h),
		Labels: d.builder.GroupedLabels(),
	}, true
}

func (d *streamDistinctValueSampleExtractor) ProcessString(ts int64, line string, structuredMetadata labels.Labels) (ExtractedSample, bool) {
	return d.Process(ts, unsafeGetBytes(line), structuredMetadata)
}

func (d *streamDistinctValueSampleExtractor) BaseLabels() LabelsResult {
	return d.builder.currentResult
}

// NewFilteringSampleExtractor creates a sample extractor where entries from
// the underlying log stream are filtered by pipeline filters before being
// passed to extract samples. Filters are always upstream of the extractor.
func NewFilteringSampleExtractor(f []PipelineFilter, e SampleExtractor) SampleExtractor {
	return &filteringSampleExtractor{
		filters:   f,
		extractor: e,
	}
}

type filteringSampleExtractor struct {
	filters   []PipelineFilter
	extractor SampleExtractor
}

func (p *filteringSampleExtractor) ForStream(labels labels.Labels) StreamSampleExtractor {
	var streamFilters []streamFilter
	for _, f := range p.filters {
		if allMatch(f.Matchers, labels) {
			streamFilters = append(streamFilters, streamFilter{
				start:    f.Start,
				end:      f.End,
				pipeline: f.Pipeline.ForStream(labels),
			})
		}
	}

	return &filteringStreamExtractor{
		filters:   streamFilters,
		extractor: p.extractor.ForStream(labels),
	}
}

type filteringStreamExtractor struct {
	filters   []streamFilter
	extractor StreamSampleExtractor
}

func (sp *filteringStreamExtractor) ReferencedStructuredMetadata() bool {
	return false
}

func (sp *filteringStreamExtractor) BaseLabels() LabelsResult {
	return sp.extractor.BaseLabels()
}

func (sp *filteringStreamExtractor) Process(ts int64, line []byte, structuredMetadata labels.Labels) (ExtractedSample, bool) {
	for _, filter := range sp.filters {
		if ts < filter.start || ts > filter.end {
			continue
		}

		_, _, matches := filter.pipeline.Process(ts, line, structuredMetadata)
		if matches { // When the filter matches, don't run the next step
			return ExtractedSample{}, false
		}
	}

	return sp.extractor.Process(ts, line, structuredMetadata)
}

func (sp *filteringStreamExtractor) ProcessString(ts int64, line string, structuredMetadata labels.Labels) (ExtractedSample, bool) {
	for _, filter := range sp.filters {
		if ts < filter.start || ts > filter.end {
			continue
		}

		_, _, matches := filter.pipeline.ProcessString(ts, line, structuredMetadata)
		if matches { // When the filter matches, don't run the next step
			return ExtractedSample{}, false
		}
	}

	return sp.extractor.ProcessString(ts, line, structuredMetadata)
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
