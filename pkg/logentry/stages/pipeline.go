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
// All pipeline stages are processed first before running chain.NextStage.
func (p *Pipeline) Process(labels model.LabelSet, extracted map[string]interface{}, ts time.Time, entry string, chain StageChain) {
	// Initialize the extracted map with the initial labels (ie. "filename"),
	// so that stages can operate on initial labels too
	for labelName, labelValue := range labels {
		extracted[string(labelName)] = string(labelValue)
	}

	pc := &pipelineChain{
		pipeline:  p,
		nextStage: 0,
		chain:     chain,
		startTime: time.Now(),
	}

	pc.NextStage(labels, extracted, ts, entry)
}

// Name implements Stage
func (p *Pipeline) Name() string {
	return StageTypePipeline
}

// Wrap implements EntryMiddleware
func (p *Pipeline) Wrap(next api.EntryHandler) api.EntryHandler {
	return api.EntryHandlerFunc(func(labels model.LabelSet, timestamp time.Time, line string) error {
		extracted := map[string]interface{}{}
		hc := handlerChain{
			next: next,
		}
		p.Process(labels, extracted, timestamp, line, hc)
		return hc.error
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

type pipelineChain struct {
	pipeline  *Pipeline
	nextStage int
	chain     StageChain
	startTime time.Time
}

func (pc *pipelineChain) NextStage(labels model.LabelSet, extracted map[string]interface{}, ts time.Time, entry string) {
	if pc.nextStage < len(pc.pipeline.stages) {
		stage := pc.pipeline.stages[pc.nextStage]
		if Debug {
			level.Debug(pc.pipeline.logger).Log("msg", "processing pipeline", "stage", pc.nextStage, "name", stage.Name(), "labels", labels, "time", ts, "entry", entry)
		}

		pc.nextStage++

		stage.Process(labels, extracted, ts, entry, pc)
	} else {
		if Debug || pc.pipeline.jobName != nil {
			dur := time.Since(pc.startTime).Seconds()
			if Debug {
				level.Debug(pc.pipeline.logger).Log("msg", "pipeline finished", "labels", labels, "time", ts, "entry", entry, "duration_s", dur)
			}

			if pc.pipeline.jobName != nil {
				pc.pipeline.plDuration.WithLabelValues(*pc.pipeline.jobName).Observe(dur)
			}
		}

		pc.chain.NextStage(labels, extracted, ts, entry)
	}
}

type handlerChain struct {
	next  api.EntryHandler
	error error
}

func (hc handlerChain) NextStage(labels model.LabelSet, extracted map[string]interface{}, ts time.Time, entry string) {
	// if the labels set contains the __drop__ label we don't send this entry to the next EntryHandler
	if _, ok := labels[dropLabel]; ok {
		hc.error = nil
	} else {
		hc.error = hc.next.Handle(labels, ts, entry)
	}
}
