package stages

import (
	"context"
	"sync"

	"github.com/go-kit/log"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"golang.org/x/time/rate"

	"github.com/grafana/loki/v3/clients/pkg/promtail/api"
)

// PipelineStages contains configuration for each stage within a pipeline
type PipelineStages = []interface{}

// PipelineStage contains configuration for a single pipeline stage
type PipelineStage = map[interface{}]interface{}

var rateLimiter *rate.Limiter
var rateLimiterDrop bool
var rateLimiterDropReason = "global_rate_limiter_drop"

// Pipeline pass down a log entry to each stage for mutation and/or label extraction.
type Pipeline struct {
	logger    log.Logger
	stages    []Stage
	jobName   *string
	dropCount *prometheus.CounterVec
}

// Cleanup implements Stage.
func (p *Pipeline) Cleanup() {
	for _, s := range p.stages {
		s.Cleanup()
	}
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
		logger:    log.With(logger, "component", "pipeline"),
		stages:    st,
		jobName:   jobName,
		dropCount: getDropCountMetric(registerer),
	}, nil
}

// RunWith will reads from the input channel entries, mutate them with the process function and returns them via the output channel.
func RunWith(input chan Entry, process func(e Entry) Entry) chan Entry {
	out := make(chan Entry)
	go func() {
		defer close(out)
		for e := range input {
			out <- process(e)
		}
	}()
	return out
}

// RunWithSkip same as RunWith, except it skip sending it to output channel, if `process` functions returns `skip` true.
func RunWithSkip(input chan Entry, process func(e Entry) (Entry, bool)) chan Entry {
	out := make(chan Entry)
	go func() {
		defer close(out)
		for e := range input {
			ee, skip := process(e)
			if skip {
				continue
			}
			out <- ee
		}
	}()

	return out
}

// RunWithSkiporSendMany same as RunWithSkip, except it can either skip sending it to output channel, if `process` functions returns `skip` true. Or send many entries.
func RunWithSkipOrSendMany(input chan Entry, process func(e Entry) ([]Entry, bool)) chan Entry {
	out := make(chan Entry)
	go func() {
		defer close(out)
		for e := range input {
			results, skip := process(e)
			if skip {
				continue
			}
			for _, result := range results {
				out <- result
			}
		}
	}()

	return out
}

// Run implements Stage
func (p *Pipeline) Run(in chan Entry) chan Entry {
	in = RunWith(in, func(e Entry) Entry {
		// Initialize the extracted map with the initial labels (ie. "filename"),
		// so that stages can operate on initial labels too
		for labelName, labelValue := range e.Labels {
			e.Extracted[string(labelName)] = string(labelValue)
		}
		return e
	})
	// chain all stages together.
	for _, m := range p.stages {
		in = m.Run(in)
	}
	return in
}

// Name implements Stage
func (p *Pipeline) Name() string {
	return StageTypePipeline
}

// Wrap implements EntryMiddleware
func (p *Pipeline) Wrap(next api.EntryHandler) api.EntryHandler {
	handlerIn := make(chan api.Entry)
	nextChan := next.Chan()
	wg, once := sync.WaitGroup{}, sync.Once{}
	pipelineIn := make(chan Entry)
	pipelineOut := p.Run(pipelineIn)
	wg.Add(2)
	go func() {
		defer wg.Done()
		for e := range pipelineOut {
			if rateLimiter != nil {
				if rateLimiterDrop {
					if !rateLimiter.Allow() {
						p.dropCount.WithLabelValues(rateLimiterDropReason).Inc()
						continue
					}
				} else {
					_ = rateLimiter.Wait(context.Background())
				}
			}
			nextChan <- e.Entry
		}
	}()
	go func() {
		defer wg.Done()
		defer close(pipelineIn)
		for e := range handlerIn {
			pipelineIn <- Entry{
				Extracted: map[string]interface{}{},
				Entry:     e,
			}
		}
	}()
	return api.NewEntryHandler(handlerIn, func() {
		once.Do(func() { close(handlerIn) })
		wg.Wait()
		p.Cleanup()
	})
}

// Size gets the current number of stages in the pipeline
func (p *Pipeline) Size() int {
	return len(p.stages)
}

func SetReadLineRateLimiter(rateVal float64, burstVal int, drop bool) {
	rateLimiter = rate.NewLimiter(rate.Limit(rateVal), burstVal)
	rateLimiterDrop = drop
}
