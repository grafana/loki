package log

import (
	"github.com/prometheus/prometheus/pkg/labels"
)

type Pipeline interface {
	Process(line []byte, lbs labels.Labels) ([]byte, labels.Labels, bool)
}

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

type MultiStage []Stage

func (m MultiStage) Process(line []byte, lbs labels.Labels) ([]byte, labels.Labels, bool) {
	var ok bool
	if len(m) == 0 {
		return line, lbs, ok
	}
	labelmap := lbs.Map()
	for _, p := range m {
		line, ok = p.Process(line, labelmap)
		if !ok {
			return line, labels.FromMap(labelmap), ok
		}
	}
	return line, labels.FromMap(labelmap), ok
}
