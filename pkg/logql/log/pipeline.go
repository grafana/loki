package log

import (
	"github.com/prometheus/prometheus/pkg/labels"
)

// Pipeline transform and filter log lines and labels.
type Pipeline interface {
	Process(line []byte, lbs labels.Labels) ([]byte, labels.Labels, bool)
}

// Stage is a single step of a Pipeline.
type Stage interface {
	Process(line []byte, lbs *LabelsBuilder) ([]byte, bool)
}

var (
	NoopPipeline Pipeline = &noopPipeline{}
	NoopStage    Stage    = &noopStage{}
)

type noopPipeline struct{}

func (noopPipeline) Process(line []byte, lbs labels.Labels) ([]byte, labels.Labels, bool) {
	return line, lbs, true
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
	stages  []Stage
	builder *LabelsBuilder
}

func NewPipeline(stages []Stage) Pipeline {
	return &pipeline{
		stages:  stages,
		builder: NewLabelsBuilder(),
	}
}

func (p *pipeline) Process(line []byte, lbs labels.Labels) ([]byte, labels.Labels, bool) {
	var ok bool
	if len(p.stages) == 0 {
		return line, lbs, true
	}
	p.builder.Reset(lbs)
	for _, s := range p.stages {
		line, ok = s.Process(line, p.builder)
		if !ok {
			return nil, nil, false
		}
	}
	return line, p.builder.Labels(), true
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
