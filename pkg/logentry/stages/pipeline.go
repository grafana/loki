package stages

import (
	"context"
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
	stages     []AsyncStage
	jobName    *string
	plDuration *prometheus.HistogramVec
	input      chan StageData

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

	st := []AsyncStage{}
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
		input:      make(chan StageData),
	}, nil
}

// Start runs the pipeline until the provided context is cancelled.
func (p *Pipeline) Start(ctx context.Context) {
	out := make(chan StageData)

	go func() {
		p.ProcessAsync(ctx, p.input, out)
	}()

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case data := <-out:
				p.notify(data)
			}
		}
	}()
}

func (p *Pipeline) notify(data StageData) {
	p.watcherMut.Lock()
	defer p.watcherMut.Unlock()

	for _, watcher := range p.watchers {
		watcher(data)
	}
}

func (p *Pipeline) AddWatcher(watcher func(StageData)) {
	p.watcherMut.Lock()
	defer p.watcherMut.Unlock()

	p.watchers = append(p.watchers, watcher)
}

// TODO(rfratto): remove this, added for testing to make sure there are no out of
// order problems.
type delayStage struct{}

func (s *delayStage) Name() string { return "delay" }
func (s *delayStage) ProcessAsync(ctx context.Context, in <-chan StageData, out chan<- StageData) {
	for {
		select {
		case <-ctx.Done():
			return
		case data := <-in:
			out <- data
			time.Sleep(time.Millisecond * 20)
		}

	}
}

// ProcessAsync implements AsyncStage, allowing a pipeline stage to also be an entire pipeline.
func (p *Pipeline) ProcessAsync(ctx context.Context, in <-chan StageData, out chan<- StageData) {
	currentIn := in

	// Prepend a stage to extract labels into the metadata
	allStages := append([]AsyncStage{
		&delayStage{},
		MakeAsync(StageFunc(p.extractLabels)),
	}, p.stages...)

	for _, stage := range allStages {
		out := make(chan StageData)

		go func(s AsyncStage, in <-chan StageData, out chan<- StageData) {
			s.ProcessAsync(ctx, in, out)
		}(stage, currentIn, out)

		currentIn = out
	}

	for {
		select {
		case <-ctx.Done():
			return
		case data := <-currentIn:
			out <- data
		}
	}
}

func (p *Pipeline) extractLabels(labels model.LabelSet, extracted map[string]interface{}, t *time.Time, entry *string) {
	// Initialize the extracted map with the initial labels (ie. "filename"),
	// so that stages can operate on initial labels too
	for labelName, labelValue := range labels {
		extracted[string(labelName)] = string(labelValue)
	}
}

// Process implements Stage, allowing a pipeline stage to also be an entire pipeline.
func (p *Pipeline) Process(labels model.LabelSet, extracted map[string]interface{}, t *time.Time, entry *string) {
	RunSync(p, labels, extracted, t, entry)
}

// Name implements Stage
func (p *Pipeline) Name() string {
	return StageTypePipeline
}

// Wrap implements EntryMiddleware
func (p *Pipeline) Wrap(next api.EntryHandler) api.EntryHandler {
	p.AddWatcher(func(data StageData) {
		// if the labels set contains the __drop__ label we don't send this entry to the next EntryHandler
		if _, ok := data.Labels[dropLabel]; ok {
			return
		}

		err := next.Handle(data.Labels, data.Time, data.Entry)
		level.Error(p.logger).Log("msg", "error calling next", "err", err)
	})

	return api.EntryHandlerFunc(func(labels model.LabelSet, timestamp time.Time, line string) error {
		extracted := map[string]interface{}{}
		p.input <- StageData{
			Labels:    labels,
			Extracted: extracted,
			Time:      timestamp,
			Entry:     line,
		}
		return nil
	})
}

// AddStage adds a stage to the pipeline
func (p *Pipeline) AddStage(stage Stage) {
	p.stages = append(p.stages, MakeAsync(stage))
}

// Size gets the current number of stages in the pipeline
func (p *Pipeline) Size() int {
	return len(p.stages)
}
