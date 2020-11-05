package log

import (
	"github.com/prometheus/prometheus/pkg/labels"
)

var (
	// NoopStage is a stage that doesn't process a log line.
	NoopStage Stage = &noopStage{}
)

// Pipeline can create pipelines for each log stream.
type Pipeline interface {
	ForStream(labels labels.Labels) StreamPipeline
}

// StreamPipeline transform and filter log lines and labels.
type StreamPipeline interface {
	Process(line []byte) ([]byte, LabelsResult, bool)
}

// Stage is a single step of a Pipeline.
type Stage interface {
	Process(line []byte, lbs *LabelsBuilder) ([]byte, bool)
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

type noopStreamPipeline struct {
	LabelsResult
}

func (n noopStreamPipeline) Process(line []byte) ([]byte, LabelsResult, bool) {
	return line, n.LabelsResult, true
}

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

func (noopStage) Process(line []byte, lbs *LabelsBuilder) ([]byte, bool) {
	return line, true
}

type StageFunc func(line []byte, lbs *LabelsBuilder) ([]byte, bool)

func (fn StageFunc) Process(line []byte, lbs *LabelsBuilder) ([]byte, bool) {
	return fn(line, lbs)
}

// pipeline is a combinations of multiple stages.
// It can also be reduced into a single stage for convenience.
type pipeline struct {
	stages      []Stage
	baseBuilder *BaseLabelsBuilder

	streamPipelines map[uint64]StreamPipeline
}

// NewPipeline creates a new pipeline for a given set of stages.
func NewPipeline(stages []Stage) Pipeline {
	if len(stages) == 0 {
		return NewNoopPipeline()
	}
	return &pipeline{
		stages:          stages,
		baseBuilder:     NewBaseLabelsBuilder(),
		streamPipelines: make(map[uint64]StreamPipeline),
	}
}

type streamPipeline struct {
	stages  []Stage
	builder *LabelsBuilder
}

func (p *pipeline) ForStream(labels labels.Labels) StreamPipeline {
	hash := p.baseBuilder.Hash(labels)
	if res, ok := p.streamPipelines[hash]; ok {
		return res
	}

	res := &streamPipeline{
		stages:  p.stages,
		builder: p.baseBuilder.ForLabels(labels, hash),
	}
	p.streamPipelines[hash] = res
	return res
}

func (p *streamPipeline) Process(line []byte) ([]byte, LabelsResult, bool) {
	var ok bool
	p.builder.Reset()
	for _, s := range p.stages {
		line, ok = s.Process(line, p.builder)
		if !ok {
			return nil, nil, false
		}
	}
	return line, p.builder.LabelsResult(), true
}

// ReduceStages reduces multiple stages into one.
func ReduceStages(stages []Stage) Stage {
	if len(stages) == 0 {
		return NoopStage
	}
	return StageFunc(func(line []byte, lbs *LabelsBuilder) ([]byte, bool) {
		var ok bool
		for _, p := range stages {
			line, ok = p.Process(line, lbs)
			if !ok {
				return nil, false
			}
		}
		return line, true
	})
}
