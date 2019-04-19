package logentry

import (
	"time"

	"github.com/go-kit/kit/log"
	"github.com/grafana/loki/pkg/logentry/stages"
	"github.com/grafana/loki/pkg/promtail/api"
	"github.com/pkg/errors"
	"github.com/prometheus/common/model"
)

// PipelineStages contains configuration for each stage within a pipeline
type PipelineStages []interface{}

// Pipeline pass down a log entry to each stage for mutation.
type Pipeline struct {
	stages []stages.Stage
}

// NewPipeline creates a new log entry pipeline from a configuration
func NewPipeline(log log.Logger, stgs PipelineStages) (*Pipeline, error) {
	st := []stages.Stage{}
	for _, s := range stgs {
		stage, ok := s.(map[interface{}]interface{})
		if !ok {
			return nil, errors.New("invalid YAML config")
		}
		if len(stage) > 1 {
			return nil, errors.New("pipeline stage must contain only one key")
		}
		for key, config := range stage {
			name, ok := key.(string)
			if !ok {
				return nil, errors.New("pipeline stage key must be a string")
			}
			switch name {
			case "json":
				json, err := stages.NewJSON(log, config)
				if err != nil {
					return nil, errors.Wrap(err, "invalid json stage config")
				}
				st = append(st, json)
			case "regex":
			}
		}
	}
	return &Pipeline{
		stages: st,
	}, nil
}

// Process mutates an entry and its metadata by using multiple configure stage.
func (p *Pipeline) Process(labels model.LabelSet, time *time.Time, entry *string) {
	//debug log labels, time, and string
	for _, stage := range p.stages {
		stage.Process(labels, time, entry)
		//debug log labels, time, and string
	}
}

// Wrap implements EntryMiddleware
func (p *Pipeline) Wrap(next api.EntryHandler) api.EntryHandler {
	return api.EntryHandlerFunc(func(labels model.LabelSet, timestamp time.Time, line string) error {
		p.Process(labels, &timestamp, &line)
		return next.Handle(labels, timestamp, line)
	})
}
