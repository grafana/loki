package stages

import (
	"sync"
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
	stages     []ChainedStage
	jobName    *string
	plDuration *prometheus.HistogramVec

	watcherMut sync.Mutex
	watchers   []func(StageData)
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

	st := []ChainedStage{}
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

// TODO(rfratto): remove this, added for testing to make sure there are no out of
// order problems.
type delayStage struct {
	ch chan func()
}

func (s *delayStage) Name() string { return "delay" }
func (s *delayStage) Run() {
	for cb := range s.ch {
		time.Sleep(time.Millisecond * 100)
		cb()
	}
}
func (s *delayStage) ProcessChain(data StageData, next func(StageData)) {
	s.ch <- func() { next(data) }
}

func (p *Pipeline) ProcessChain(data StageData, next func(StageData)) {
	// Initialize the extracted map with the initial labels (ie. "filename"),
	// so that stages can operate on initial labels too
	for labelName, labelValue := range data.Labels {
		data.Extracted[string(labelName)] = string(labelValue)
	}

	ds := &delayStage{ch: make(chan func())}
	go ds.Run()

	allStages := append([]ChainedStage{ds}, p.stages...)
	run := processableStages{stages: allStages}
	run.ProcessChain(data, next)
}

type processableStages struct {
	stages []ChainedStage
	idx    int
}

func (ss *processableStages) ProcessChain(data StageData, next func(StageData)) {
	ss.stages[ss.idx].ProcessChain(data, func(data StageData) {
		ss.idx++
		if ss.idx >= len(ss.stages) {
			next(data)
		} else {
			ss.ProcessChain(data, next)
		}
	})
}

// Process implements Stage, allowing a pipeline stage to also be an entire pipeline.
func (p *Pipeline) Process(labels model.LabelSet, extracted map[string]interface{}, t *time.Time, entry *string) {
	FlattenStage(p).Process(labels, extracted, t, entry)
}

// Name implements Stage
func (p *Pipeline) Name() string {
	return StageTypePipeline
}

// Wrap implements EntryMiddleware
func (p *Pipeline) Wrap(next api.EntryHandler) api.EntryHandler {
	return api.EntryHandlerFunc(func(labels model.LabelSet, timestamp time.Time, line string) error {
		extracted := map[string]interface{}{}

		data := StageData{
			Labels:    labels,
			Extracted: extracted,
			Time:      timestamp,
			Entry:     line,
		}

		p.ProcessChain(data, func(out StageData) {
			// if the labels set contains the __drop__ label we don't send this entry to the next EntryHandler
			if _, ok := labels[dropLabel]; ok {
				return
			}

			err := next.Handle(out.Labels, out.Time, out.Entry)
			if err != nil {
				level.Error(p.logger).Log("msg", "error calling next", "err", err)
			}
		})

		return nil
	})
}

// AddStage adds a stage to the pipeline
func (p *Pipeline) AddStage(stage Stage) {
	p.stages = append(p.stages, MakeChained(stage))
}

// Size gets the current number of stages in the pipeline
func (p *Pipeline) Size() int {
	return len(p.stages)
}
