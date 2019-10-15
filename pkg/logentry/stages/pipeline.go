package stages

import (
	"time"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
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
	logger     log.Logger
	stages     []Stage
	jobName    *string
	plDuration *prometheus.HistogramVec
}

// NewPipeline creates a new log entry pipeline from a configuration
func NewPipeline(logger log.Logger, stgs PipelineStages, jobName *string, registerer prometheus.Registerer) (*Pipeline, error) {
	hist := prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: "logentry",
		Name:      "pipeline_duration_seconds",
		Help:      "Label and metric extraction pipeline processing time, in seconds",
		Buckets:   []float64{.000005, .000010, .000025, .000050, .000100, .000250, .000500, .001000, .002500, .005000, .010000, .025000},
	}, []string{"job_name"})
	err := registerer.Register(hist)
	if err != nil {
		if existing, ok := err.(prometheus.AlreadyRegisteredError); ok {
			hist = existing.ExistingCollector.(*prometheus.HistogramVec)
		} else {
			// Same behavior as MustRegister if the error is not for AlreadyRegistered
			panic(err)
		}
	}

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
		logger:     log.With(logger, "component", "pipeline"),
		stages:     st,
		jobName:    jobName,
		plDuration: hist,
	}, nil
}

// Process implements Stage allowing a pipeline stage to also be an entire pipeline
func (p *Pipeline) Process(labels model.LabelSet, extracted map[string]interface{}, ts *time.Time, entry *string) {
	start := time.Now()

	// Initialize the extracted map with the initial labels (ie. "filename"),
	// so that stages can operate on initial labels too
	for labelName, labelValue := range labels {
		extracted[string(labelName)] = string(labelValue)
	}

	for i, stage := range p.stages {
		if Debug {
			level.Debug(p.logger).Log("msg", "processing pipeline", "stage", i, "name", stage.Name(), "labels", labels, "time", ts, "entry", entry)
		}
		stage.Process(labels, extracted, ts, entry)
	}
	dur := time.Since(start).Seconds()
	if Debug {
		level.Debug(p.logger).Log("msg", "finished processing log line", "labels", labels, "time", ts, "entry", entry, "duration_s", dur)
	}
	if p.jobName != nil {
		p.plDuration.WithLabelValues(*p.jobName).Observe(dur)
	}
}

// Name implements Stage
func (p *Pipeline) Name() string {
	return StageTypePipeline
}

// Wrap implements EntryMiddleware
func (p *Pipeline) Wrap(next api.EntryHandler) api.EntryHandler {
	return api.EntryHandlerFunc(func(labels model.LabelSet, timestamp time.Time, line string) error {
		extracted := map[string]interface{}{}
		p.Process(labels, extracted, &timestamp, &line)
		// if the labels set contains the __drop__ label we don't send this entry to the next EntryHandler
		if _, ok := labels[dropLabel]; ok {
			return nil
		}
		return next.Handle(labels, timestamp, line)
	})
}

// AddStage adds a stage to the pipeline
func (p *Pipeline) AddStage(stage Stage) {
	p.stages = append(p.stages, stage)
}

// Size gets the current number of stages in the pipeline
func (p *Pipeline) Size() int {
	return len(p.stages)
}
