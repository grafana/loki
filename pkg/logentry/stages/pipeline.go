package stages

import (
	"context"
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
	logger  log.Logger
	stages  []AsyncStage
	jobName *string
}

// NewPipeline creates a new log entry pipeline from a configuration
func NewPipeline(logger log.Logger, stgs PipelineStages, jobName *string, registerer prometheus.Registerer) (*Pipeline, error) {
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
		logger:  log.With(logger, "component", "pipeline"),
		stages:  st,
		jobName: jobName,
	}, nil
}

// ProcessAsync implements AsyncStage, allowing a pipeline stage to also be an entire pipeline.
func (p *Pipeline) ProcessAsync(ctx context.Context, stream DataStream) {
	currentIn := stream.In

	// Prepend a stage to extract labels into the metadata
	allStages := append([]AsyncStage{
		AsyncStageFunc(initializeLabelsStage),
	}, p.stages...)

	// Launch all the stages and wire the channels together. The output of one stage should be the
	// input of the next.
	for _, stage := range allStages {
		out := make(chan StageData)
		stream := DataStream{In: currentIn, Out: out}
		go stage.ProcessAsync(ctx, stream)
		currentIn = out
	}

	for {
		select {
		case <-ctx.Done():
			return
		case data := <-currentIn:
			stream.Send(data)
		}
	}
}

func initializeLabelsStage(ctx context.Context, stream DataStream) {
	stream.Do(ctx, func(data StageData) {
		// Initialize the extracted map with the initial labels (ie. "filename"),
		// so that stages can operate on initial labels too
		for labelName, labelValue := range data.Labels {
			data.Extracted[string(labelName)] = string(labelValue)
		}

		stream.Send(data)
	})
}

// Process implements Stage, allowing a pipeline stage to also be an entire pipeline.
func (p *Pipeline) Process(labels model.LabelSet, extracted map[string]interface{}, t *time.Time, entry *string) {
	RunSync(p, labels, extracted, t, entry)
}

// Name implements Stage
func (p *Pipeline) Name() string {
	return StageTypePipeline
}

// Wrap implements EntryMiddleware. When the function is invoked, data will be sent to the
// pipeline over a channel. Whenever the pipeline returns data, next will be invoked.
func (p *Pipeline) Wrap(ctx context.Context, next api.EntryHandler) api.EntryHandler {
	in := make(chan StageData)
	out := make(chan StageData)

	stream := DataStream{In: in, Out: out}
	go p.ProcessAsync(ctx, stream)

	go func() {
		// Wait for data coming through the pipeline.
		for data := range out {
			// if the labels set contains the __drop__ label we don't send this entry to the next EntryHandler
			if _, ok := data.Labels[dropLabel]; ok {
				continue
			}

			err := next.Handle(data.Labels, data.Time, data.Entry)
			level.Error(p.logger).Log("msg", "error calling next", "err", err)
		}
	}()

	return api.EntryHandlerFunc(func(labels model.LabelSet, timestamp time.Time, line string) error {
		extracted := map[string]interface{}{}
		in <- StageData{
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
