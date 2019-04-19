package logentry

import (
	"time"

	"github.com/go-kit/kit/log"
	"github.com/grafana/loki/pkg/logentry/stages"
	"github.com/grafana/loki/pkg/promtail/api"
	"github.com/pkg/errors"
	"github.com/prometheus/common/model"
)

type PipelineStages []interface{}

type Pipeline struct {
	stages []stages.Stage
}

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
				json, err := stages.NewJson(log, config)
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

func (p *Pipeline) Process(labels model.LabelSet, time *time.Time, entry *string) {
	//debug log labels, time, and string
	for _, stage := range p.stages {
		stage.Process(labels, time, entry)
		//debug log labels, time, and string
	}
}

func (p *Pipeline) Wrap(next api.EntryHandler) api.EntryHandler {
	return api.EntryHandlerFunc(func(labels model.LabelSet, timestamp time.Time, line string) error {
		p.Process(labels, &timestamp, &line)
		return next.Handle(labels, timestamp, line)
	})
}
