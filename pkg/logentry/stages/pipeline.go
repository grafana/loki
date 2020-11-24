package stages

import (
	"time"

	"github.com/go-kit/kit/log"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"

	"github.com/grafana/loki/pkg/promtail/api"
)

const dropLabel = "__drop__"

// PipelineStages contains configuration for each stage within a pipeline
type PipelineStages = []interface{}

// PipelineStage contains configuration for a single pipeline stage
type PipelineStage = map[interface{}]interface{}

// Pipeline pass down a log entry to each stage for mutation and/or label extraction.
type Pipeline struct {
	logger  log.Logger
	stages  []Stage
	jobName *string
}

// NewPipeline creates a new log entry pipeline from a configuration
func NewPipeline(logger log.Logger, stgs PipelineStages, jobName *string, registerer prometheus.Registerer) (*Pipeline, error) {
	st := []Stage{}
	for _, s := range stgs {
		stage, ok := s.(PipelineStage)
		if !ok {
			return nil, errors.Errorf("invalid YAML config, "+
				"make sure each stage of your pipeline is a YAML object (must end with a `:`), check stage `- %s`", s)
		}
		if len(stage) > 1 {
			return nil, errors.New("pipeline stage must contain only one key")
		}
		for key, config := range stage {
			name, ok := key.(string)
			if !ok {
				return nil, errors.New("pipeline stage key must be a string")
			}
			newStage, err := New(logger, jobName, name, config, registerer)
			if err != nil {
				return nil, errors.Wrapf(err, "invalid %s stage config", name)
			}
			st = append(st, newStage)
		}
	}
	return &Pipeline{
		logger:  log.With(logger, "component", "pipeline"),
		stages:  st,
		jobName: jobName,
	}, nil
}

func RunWith(in chan Entry, process func(e Entry) Entry) chan Entry {
	out := make(chan Entry)
	go func() {
		for e := range in {
			out <- process(e)
		}
		close(out)
	}()
	return out
}

func (p *Pipeline) Run(in chan Entry) chan Entry {
	in = RunWith(in, func(e Entry) Entry {
		// Initialize the extracted map with the initial labels (ie. "filename"),
		// so that stages can operate on initial labels too
		for labelName, labelValue := range e.Labels {
			e.Extracted[string(labelName)] = string(labelValue)
		}
		return e
	})
	for _, m := range p.stages {
		in = m.Run(in)
	}
	return in
}

// Name implements Stage
func (p *Pipeline) Name() string {
	return StageTypePipeline
}

// type Runner interface {
// 	Run(in chan Entry) chan Entry
// }

// type RunnerFunc func(in chan Entry) chan Entry

// func(f RunnerFunc) Run(in chan Entry) chan Entry{
// 	f(in)
// }

// func (p *Pipeline) Wrap(r Runner) Runner {
// 	res := RunnerFunc(func(in chan Entry) chan Entry {
// 		out := p.Run(in)
// 		go func() {
// 			for e := range out {

// 			}
// 		}
// 	})

// 	return res
// }

// Wrap implements EntryMiddleware
func (p *Pipeline) Wrap(next api.EntryHandler) api.EntryHandler {
	in := make(chan Entry)
	out := p.Run(in)
	go func() {
		for e := range out {
			_ = next.Handle(e.Labels, e.Timestamp, e.Line)
		}
		close(in)
	}()
	return api.EntryHandlerFunc(func(labels model.LabelSet, timestamp time.Time, line string) error {
		in <- Entry{
			Labels:    labels,
			Extracted: map[string]interface{}{},
			Line:      line,
			Timestamp: timestamp,
		}
		return nil
	})
}

// Size gets the current number of stages in the pipeline
func (p *Pipeline) Size() int {
	return len(p.stages)
}
