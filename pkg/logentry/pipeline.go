package logentry

import (
	"time"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/common/model"

	"github.com/grafana/loki/pkg/logentry/stages"
	"github.com/grafana/loki/pkg/promtail/api"
)

var (
	pipelineDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "logentry",
		Name:      "pipeline_duration_seconds",
		Help:      "Label and metric extraction pipeline processing time, in seconds",
		Buckets:   []float64{.000005, .000010, .000025, .000050, .000100, .000250, .000500, .001000, .002500, .005000, .010000, .025000},
	}, []string{"job_name"})
)

// PipelineStages contains configuration for each stage within a pipeline
type PipelineStages []interface{}

// Pipeline pass down a log entry to each stage for mutation and/or label extraction.
type Pipeline struct {
	logger  log.Logger
	stages  []stages.Stage
	jobName string
}

// NewPipeline creates a new log entry pipeline from a configuration
func NewPipeline(logger log.Logger, stgs PipelineStages, jobName string, logRegistry prometheus.Registerer) (*Pipeline, error) {
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
			newStage, err := stages.New(logger, name, config, logRegistry)
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

// Process mutates an entry and its metadata by using multiple configure stage.
func (p *Pipeline) Process(labels model.LabelSet, ts *time.Time, entry *string) {
	start := time.Now()
	for i, stage := range p.stages {
		level.Debug(p.logger).Log("msg", "processing pipeline", "stage", i, "labels", labels, "time", ts, "entry", entry)
		stage.Process(labels, ts, entry)
	}
	dur := time.Since(start).Seconds()
	level.Debug(p.logger).Log("msg", "finished processing log line", "labels", labels, "time", ts, "entry", entry, "duration_s", dur)
	pipelineDuration.WithLabelValues(p.jobName).Observe(dur)
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
