package log

import (
	"context"
	"sort"
	"strconv"
	"time"

	"github.com/pkg/errors"
	"github.com/prometheus/prometheus/model/labels"

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
	// BaseLabels returns the labels of the log stream this extractor serves.
	//
	// The returned LabelsResult is the stream's identity, fixed for the extractor's
	// lifetime and the same for every line. It is not the sample's output labels:
	// those come from Process/ProcessString and reflect the pipeline, grouping, and
	// structured metadata, so they may differ per line and differ from BaseLabels.
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
	streamExtractors map[uint64]cachedStreamSampleExtractor
}

// cachedStreamSampleExtractor is a per-stream extractor cached by labels hash, kept with its labels so a hash
// collision (two streams, one hash) is detected rather than served the wrong stream's extractor.
type cachedStreamSampleExtractor struct {
	extractor  StreamSampleExtractor
	baseLabels labels.Labels
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
		streamExtractors: make(map[uint64]cachedStreamSampleExtractor),
	}, nil
}

func (l *lineSampleExtractor) ForStream(lbls labels.Labels) StreamSampleExtractor {
	hash := l.baseBuilder.Hash(lbls)
	// Verify the cached extractor is for these exact labels: Hash can collide, and serving a
	// colliding stream's extractor would report its samples under the wrong labels.
	if c, ok := l.streamExtractors[hash]; ok && labels.Equal(c.baseLabels, lbls) {
		return c.extractor
	}

	se := l.newStreamSampleExtractor(lbls, hash)
	l.streamExtractors[hash] = cachedStreamSampleExtractor{extractor: se, baseLabels: lbls}
	return se
}

func (l *lineSampleExtractor) newStreamSampleExtractor(lbls labels.Labels, hash uint64) StreamSampleExtractor {
	builder := l.baseBuilder.ForLabels(lbls, hash)

	// Fast path: when the output labels are the same for every line of the stream (e.g. structured metadata
	// can't change them), build them once and skip the per-line label builder.
	if l.canUseConstantLabelsWithoutStructuredMetadata(lbls) {
		// Build the stream's constant label sets once:
		// 1. Reset clears any pipeline overlay left on the shared builder, so only the stream's base labels remain.
		// 2. LabelsResult then returns those base labels (the stream identity, for BaseLabels/StreamHash)
		// 3. GroupedLabels returns the grouping applied to them (the constant output labels)
		builder.Reset()
		baseLabels := builder.LabelsResult()
		groupedLabels := builder.GroupedLabels()

		if l.Stage == NoopStage {
			return &noopConstantLabelStreamExtractor{line: l.LineExtractor, groupedLabels: groupedLabels, baseLabels: baseLabels}
		}
		// A safe stage (a line filter, decolorize, or a filter reading only stream labels) still runs per
		// line to decide the match and produce the line for the value, but the output labels are the
		// cached constant set.
		return &filteredConstantLabelStreamExtractor{stage: l.Stage, line: l.LineExtractor, groupedLabels: groupedLabels, baseLabels: baseLabels, builder: builder}
	}

	return &streamLineSampleExtractor{
		Stage:         l.Stage,
		LineExtractor: l.LineExtractor,
		builder:       builder,
	}
}

// canUseConstantLabelsWithoutStructuredMetadata reports whether the output labels are the same for every
// log line, and the pipeline needs no per-line label builder.
func (l *lineSampleExtractor) canUseConstantLabelsWithoutStructuredMetadata(streamLabels labels.Labels) bool {
	// First, no stage can write labels. A stage that adds, removes, or replaces a label, or sets __error__,
	// makes the output vary per line.
	if l.Stage.Hints().CanModifyLabels {
		return false
	}

	// Second, no stage reads a label the stream does not carry. The fast path never adds per-line structured
	// metadata to the builder, so a stage that reads a non-stream label would see it missing and mis-filter.
	for _, name := range l.Stage.RequiredLabelNames() {
		if !streamLabels.Has(name) {
			return false
		}
	}

	// Third, the grouping resolves to stream labels only. Grouping to nothing (noLabels) or to labels the
	// stream already carries is constant; a `without`, no grouping, or a group key the stream lacks (so it
	// comes from metadata) is not.
	b := l.baseBuilder
	if b.noLabels {
		return true
	}
	if b.without || len(b.groups) == 0 {
		return false
	}
	for _, g := range b.groups {
		if !streamLabels.Has(g) {
			return false
		}
	}
	return true
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

// noopConstantLabelStreamExtractor is a constant-label specialization for the NoopStage case. It
// requires that the output labels are the same for every line and that no stage runs, so no line is
// filtered and the line's structured metadata is never read. Every line yields a sample with the cached
// constant labels, and only the value depends on the line. It does no per-line builder work and allocates
// nothing.
type noopConstantLabelStreamExtractor struct {
	line          LineExtractor
	groupedLabels LabelsResult // the grouped output labels, constant for the stream
	baseLabels    LabelsResult // the stream's own labels, for series identity (BaseLabels/StreamHash)
}

func (e *noopConstantLabelStreamExtractor) Process(_ int64, line []byte, _ labels.Labels) (ExtractedSample, bool) {
	return ExtractedSample{Value: e.line(line), Labels: e.groupedLabels}, true
}

func (e *noopConstantLabelStreamExtractor) ProcessString(_ int64, line string, _ labels.Labels) (ExtractedSample, bool) {
	return ExtractedSample{Value: e.line(unsafeGetBytes(line)), Labels: e.groupedLabels}, true
}

func (e *noopConstantLabelStreamExtractor) BaseLabels() LabelsResult {
	return e.baseLabels
}

func (e *noopConstantLabelStreamExtractor) ReferencedStructuredMetadata() bool {
	return false
}

// filteredConstantLabelStreamExtractor is a constant-label specialization for a pipeline with a stage. It
// requires that the output labels are the same for every line, that the stage cannot change those labels,
// and that the stage does not read the line's structured metadata (it reads only stream labels). The stage
// runs per line to drop or transform the line; the output labels are the cached constant set.
type filteredConstantLabelStreamExtractor struct {
	stage         Stage
	line          LineExtractor
	groupedLabels LabelsResult
	baseLabels    LabelsResult
	builder       *LabelsBuilder
}

func (e *filteredConstantLabelStreamExtractor) Process(ts int64, line []byte, _ labels.Labels) (ExtractedSample, bool) {
	// The base builder is shared among extractors for different log streams, so we have to Reset
	// it each time, right before using it.
	e.builder.Reset()

	// The structured metadata is not added to the label builder because there's the guarantee that
	// this stage doesn't read it.
	out, ok := e.stage.Process(ts, line, e.builder)
	if !ok {
		return ExtractedSample{}, false
	}
	return ExtractedSample{Value: e.line(out), Labels: e.groupedLabels}, true
}

func (e *filteredConstantLabelStreamExtractor) ProcessString(ts int64, line string, structuredMetadata labels.Labels) (ExtractedSample, bool) {
	return e.Process(ts, unsafeGetBytes(line), structuredMetadata)
}

func (e *filteredConstantLabelStreamExtractor) BaseLabels() LabelsResult {
	return e.baseLabels
}

func (e *filteredConstantLabelStreamExtractor) ReferencedStructuredMetadata() bool {
	return false
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

func (l *labelSampleExtractor) ForStream(lbls labels.Labels) StreamSampleExtractor {
	hash := l.baseBuilder.Hash(lbls)
	// Verify the cached extractor is for these exact labels (Hash can collide).
	if res, ok := l.streamExtractors[hash]; ok && labels.Equal(res.(*streamLabelSampleExtractor).builder.base, lbls) {
		return res
	}

	res := &streamLabelSampleExtractor{
		labelSampleExtractor: l,
		builder:              l.baseBuilder.ForLabels(lbls, hash),
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
