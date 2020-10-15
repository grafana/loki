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
	Process(line []byte, lbs Labels) ([]byte, bool)
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

func (noopStage) Process(line []byte, lbs Labels) ([]byte, bool) {
	return line, true
}

type StageFunc func(line []byte, lbs Labels) ([]byte, bool)

func (fn StageFunc) Process(line []byte, lbs Labels) ([]byte, bool) {
	return fn(line, lbs)
}

// MultiStage is a combinations of multiple stages. Which implement Pipeline
// or can be reduced into a single stage for convenience.
type MultiStage []Stage

func (m MultiStage) Process(line []byte, lbs labels.Labels) ([]byte, labels.Labels, bool) {
	var ok bool
	if len(m) == 0 {
		return line, lbs, true
	}
	// todo(cyriltovena): this should be deferred within a specific Labels type.
	// Not all stages will need to access the labels map (e.g line filter).
	// This could optimize queries that uses only those stages.
	labelmap := lbs.Map()
	for _, p := range m {
		line, ok = p.Process(line, labelmap)
		if !ok {
			return nil, nil, false
		}
	}
	return line, labels.FromMap(labelmap), true
}

func (m MultiStage) Reduce() Stage {
	if len(m) == 0 {
		return NoopStage
	}
	return StageFunc(func(line []byte, lbs Labels) ([]byte, bool) {
		var ok bool
		for _, p := range m {
			line, ok = p.Process(line, lbs)
			if !ok {
				return nil, false
			}
		}
		return line, true
	})
}
