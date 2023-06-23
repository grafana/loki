package log

import (
	"reflect"
	"unsafe"

	"github.com/prometheus/prometheus/model/labels"
)

// NoopStage is a stage that doesn't process a log line.
var NoopStage Stage = &noopStage{}

// Pipeline can create pipelines for each log stream.
type Pipeline interface {
	ForStream(labels labels.Labels) StreamPipeline
}

// StreamPipeline transform and filter log lines and labels.
// A StreamPipeline never mutate the received line.
type StreamPipeline interface {
	BaseLabels() LabelsResult
	// Process processes a log line and returns the transformed line and the labels.
	// The buffer returned for the log line can be reused on subsequent calls to Process and therefore must be copied.
	Process(ts int64, line []byte) (resultLine []byte, resultLabels LabelsResult, matches bool)
	ProcessString(ts int64, line string) (resultLine string, resultLabels LabelsResult, matches bool)
}

// Stage is a single step of a Pipeline.
// A Stage implementation should never mutate the line passed, but instead either
// return the line unchanged or allocate a new line.
type Stage interface {
	Process(ts int64, line []byte, lbs *LabelsBuilder) ([]byte, bool)
	RequiredLabelNames() []string
}

// NewNoopPipeline creates a pipelines that does not process anything and returns log streams as is.
func NewNoopPipeline() Pipeline {
	return &noopPipeline{
		cache: map[uint64]*noopStreamPipeline{},
	}
}

type noopPipeline struct {
	cache map[uint64]*noopStreamPipeline
}

// IsNoopPipeline tells if a pipeline is a Noop.
func IsNoopPipeline(p Pipeline) bool {
	_, ok := p.(*noopPipeline)
	return ok
}

type noopStreamPipeline struct {
	LabelsResult
}

func (n noopStreamPipeline) Process(_ int64, line []byte) ([]byte, LabelsResult, bool) {
	return line, n.LabelsResult, true
}

func (n noopStreamPipeline) ProcessString(_ int64, line string) (string, LabelsResult, bool) {
	return line, n.LabelsResult, true
}

func (n noopStreamPipeline) BaseLabels() LabelsResult { return n.LabelsResult }

func (n *noopPipeline) ForStream(labels labels.Labels) StreamPipeline {
	h := labels.Hash()
	if cached, ok := n.cache[h]; ok {
		return cached
	}
	sp := &noopStreamPipeline{LabelsResult: NewLabelsResult(labels, h)}
	n.cache[h] = sp
	return sp
}

type noopStage struct{}

func (noopStage) Process(_ int64, line []byte, _ *LabelsBuilder) ([]byte, bool) {
	return line, true
}
func (noopStage) RequiredLabelNames() []string { return []string{} }

type StageFunc struct {
	process        func(ts int64, line []byte, lbs *LabelsBuilder) ([]byte, bool)
	requiredLabels []string
}

func (fn StageFunc) Process(ts int64, line []byte, lbs *LabelsBuilder) ([]byte, bool) {
	return fn.process(ts, line, lbs)
}

func (fn StageFunc) RequiredLabelNames() []string {
	if fn.requiredLabels == nil {
		return []string{}
	}
	return fn.requiredLabels
}

// pipeline is a combinations of multiple stages.
// It can also be reduced into a single stage for convenience.
type pipeline struct {
	AnalyzablePipeline
	stages      []Stage
	baseBuilder *BaseLabelsBuilder

	streamPipelines map[uint64]StreamPipeline
}

func (p *pipeline) Stages() []Stage {
	return p.stages
}

func (p *pipeline) LabelsBuilder() *BaseLabelsBuilder {
	return p.baseBuilder
}

type AnalyzablePipeline interface {
	Pipeline
	Stages() []Stage
	LabelsBuilder() *BaseLabelsBuilder
}

// NewPipeline creates a new pipeline for a given set of stages.
func NewPipeline(stages []Stage) Pipeline {
	if len(stages) == 0 {
		return NewNoopPipeline()
	}

	hints := NewParserHint(nil, nil, false, false, "", stages)
	builder := NewBaseLabelsBuilderWithGrouping(nil, hints, false, false)
	return &pipeline{
		stages:          stages,
		baseBuilder:     builder,
		streamPipelines: make(map[uint64]StreamPipeline),
	}
}

type streamPipeline struct {
	stages  []Stage
	builder *LabelsBuilder
}

func NewStreamPipeline(stages []Stage, labelsBuilder *LabelsBuilder) StreamPipeline {
	return &streamPipeline{stages, labelsBuilder}
}

func (p *pipeline) ForStream(labels labels.Labels) StreamPipeline {
	hash := p.baseBuilder.Hash(labels)
	if res, ok := p.streamPipelines[hash]; ok {
		return res
	}

	res := NewStreamPipeline(p.stages, p.baseBuilder.ForLabels(labels, hash))
	p.streamPipelines[hash] = res
	return res
}

func (p *streamPipeline) Process(ts int64, line []byte) ([]byte, LabelsResult, bool) {
	var ok bool
	p.builder.Reset()
	for _, s := range p.stages {
		line, ok = s.Process(ts, line, p.builder)
		if !ok {
			return nil, nil, false
		}
	}
	return line, p.builder.LabelsResult(), true
}

func (p *streamPipeline) ProcessString(ts int64, line string) (string, LabelsResult, bool) {
	// Stages only read from the line.
	lb, lr, ok := p.Process(ts, unsafeGetBytes(line))
	// but the returned line needs to be copied.
	return string(lb), lr, ok
}

func (p *streamPipeline) BaseLabels() LabelsResult { return p.builder.currentResult }

// PipelineFilter contains a set of matchers and a pipeline that, when matched,
// causes an entry from a log stream to be skipped. Matching entries must also
// fall between 'start' and 'end', inclusive
type PipelineFilter struct {
	Start    int64
	End      int64
	Matchers []*labels.Matcher
	Pipeline Pipeline
}

// NewFilteringPipeline creates a pipeline where entries from the underlying
// log stream are filtered by pipeline filters before being passed to the
// pipeline representing the queried data. Filters are always upstream of the
// pipeline
func NewFilteringPipeline(f []PipelineFilter, p Pipeline) Pipeline {
	return &filteringPipeline{
		filters:  f,
		pipeline: p,
	}
}

type filteringPipeline struct {
	filters  []PipelineFilter
	pipeline Pipeline
}

func (p *filteringPipeline) ForStream(labels labels.Labels) StreamPipeline {
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

	return &filteringStreamPipeline{
		filters:  streamFilters,
		pipeline: p.pipeline.ForStream(labels),
	}
}

func allMatch(matchers []*labels.Matcher, labels labels.Labels) bool {
	for _, m := range matchers {
		if !m.Matches(labels.Get(m.Name)) {
			return false
		}
	}
	return true
}

type streamFilter struct {
	start    int64
	end      int64
	pipeline StreamPipeline
}

type filteringStreamPipeline struct {
	filters  []streamFilter
	pipeline StreamPipeline
}

func (sp *filteringStreamPipeline) BaseLabels() LabelsResult {
	return sp.pipeline.BaseLabels()
}

func (sp *filteringStreamPipeline) Process(ts int64, line []byte) ([]byte, LabelsResult, bool) {
	for _, filter := range sp.filters {
		if ts < filter.start || ts > filter.end {
			continue
		}

		_, _, matches := filter.pipeline.Process(ts, line)
		if matches { // When the filter matches, don't run the next step
			return nil, nil, false
		}
	}

	return sp.pipeline.Process(ts, line)
}

func (sp *filteringStreamPipeline) ProcessString(ts int64, line string) (string, LabelsResult, bool) {
	for _, filter := range sp.filters {
		if ts < filter.start || ts > filter.end {
			continue
		}

		_, _, matches := filter.pipeline.ProcessString(ts, line)
		if matches { // When the filter matches, don't run the next step
			return "", nil, false
		}
	}

	return sp.pipeline.ProcessString(ts, line)
}

// ReduceStages reduces multiple stages into one.
func ReduceStages(stages []Stage) Stage {
	if len(stages) == 0 {
		return NoopStage
	}
	var requiredLabelNames []string
	for _, s := range stages {
		requiredLabelNames = append(requiredLabelNames, s.RequiredLabelNames()...)
	}
	return StageFunc{
		process: func(ts int64, line []byte, lbs *LabelsBuilder) ([]byte, bool) {
			var ok bool
			for _, p := range stages {
				line, ok = p.Process(ts, line, lbs)
				if !ok {
					return nil, false
				}
			}
			return line, true
		},
		requiredLabels: requiredLabelNames,
	}
}

func unsafeGetBytes(s string) []byte {
	var buf []byte
	p := unsafe.Pointer(&buf)
	*(*string)(p) = s
	(*reflect.SliceHeader)(p).Cap = len(s)
	return buf
}

func unsafeGetString(buf []byte) string {
	return *((*string)(unsafe.Pointer(&buf)))
}
