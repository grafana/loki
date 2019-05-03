package logentry

import (
	"time"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/pkg/errors"
	"github.com/prometheus/common/model"

	"github.com/grafana/loki/pkg/logentry/stages"
	"github.com/grafana/loki/pkg/promtail/api"
)

// PipelineStages contains configuration for each stage within a pipeline
type PipelineStages []interface{}

// Pipeline pass down a log entry to each stage for mutation and/or label extraction.
type Pipeline struct {
	logger log.Logger
	stages []stages.Stage
}

// NewPipeline creates a new log entry pipeline from a configuration
func NewPipeline(logger log.Logger, stgs PipelineStages) (*Pipeline, error) {
	st := []stages.Stage{}
	for _, s := range stgs {
		stage, ok := s.(map[interface{}]interface{})
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
			switch name {
			case "json":
				json, err := stages.NewJSON(logger, config)
				if err != nil {
					return nil, errors.Wrap(err, "invalid json stage config")
				}
				st = append(st, json)
			case "regex":
				regex, err := stages.NewRegex(logger, config)
				if err != nil {
					return nil, errors.Wrap(err, "invalid regex stage config")
				}
				st = append(st, regex)
			case "docker":
				docker, err := stages.NewDocker(logger)
				if err != nil {
					return nil, errors.Wrap(err, "invalid docker stage config")
				}
				st = append(st, docker)
			case "cri":
				cri, err := stages.NewCri(logger)
				if err != nil {
					return nil, errors.Wrap(err, "invalid cri stage config")
				}
				st = append(st, cri)
			}
		}
	}
	return &Pipeline{
		logger: log.With(logger, "component", "pipeline"),
		stages: st,
	}, nil
}

// Process mutates an entry and its metadata by using multiple configure stage.
func (p *Pipeline) Process(labels model.LabelSet, time *time.Time, entry *string) {
	for i, stage := range p.stages {
		level.Debug(p.logger).Log("msg", "processing pipeline", "stage", i, "labels", labels, "time", time, "entry", entry)
		stage.Process(labels, time, entry)
	}
	level.Debug(p.logger).Log("msg", "finished processing log line", "labels", labels, "time", time, "entry", entry)
}

// Wrap implements EntryMiddleware
func (p *Pipeline) Wrap(next api.EntryHandler) api.EntryHandler {
	return api.EntryHandlerFunc(func(labels model.LabelSet, timestamp time.Time, line string) error {
		p.Process(labels, &timestamp, &line)
		return next.Handle(labels, timestamp, line)
	})
}

// AddStage adds a stage to the pipeline
func (p *Pipeline) AddStage(stage stages.Stage) {
	p.stages = append(p.stages, stage)
}

// Size gets the current number of stages in the pipeline
func (p *Pipeline) Size() int {
	return len(p.stages)
}
