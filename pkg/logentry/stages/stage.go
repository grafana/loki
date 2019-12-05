package stages

import (
	"context"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
)

const (
	StageTypeJSON      = "json"
	StageTypeRegex     = "regex"
	StageTypeMetric    = "metrics"
	StageTypeLabel     = "labels"
	StageTypeTimestamp = "timestamp"
	StageTypeOutput    = "output"
	StageTypeDocker    = "docker"
	StageTypeCRI       = "cri"
	StageTypeMatch     = "match"
	StageTypeTemplate  = "template"
	StageTypePipeline  = "pipeline"
	StageTypeTenant    = "tenant"
)

// Stage takes an existing set of labels, timestamp and log entry and returns either a possibly mutated
// timestamp and log entry
type Stage interface {
	Process(labels model.LabelSet, extracted map[string]interface{}, time *time.Time, entry *string)
	Name() string
}

// AsyncStage is like Stage, but runs forever, or until the provided context is cancelled. Throughout
// its execution, AsyncStage should read lines as input, modify them as needed, and pass through
// zero or more output lines.
type AsyncStage interface {
	ProcessAsync(ctx context.Context, stream DataStream)
	Name() string
}

// AsyncStageFunc is a function that implements AsyncStage.
type AsyncStageFunc func(ctx context.Context, stream DataStream)

// ProcessAsync implements AsyncStage.
func (f AsyncStageFunc) ProcessAsync(ctx context.Context, stream DataStream) {
	f(ctx, stream)
}

// Name implements AsyncStage.
func (f AsyncStageFunc) Name() string { return "function" }

// DataStream is a combination of an input and output channel for StageData. Used as an
// argument in AsyncStage.
type DataStream struct {
	In  <-chan StageData
	Out chan<- StageData
}

// Send sends data on the out channel.
func (s DataStream) Send(data StageData) { s.Out <- data }

// Do will continually listen for data coming in from the DataStream's In channel, calling
// f each time data is received. If the provided context is cancelled, f will
// no longer be called.
func (s DataStream) Do(ctx context.Context, f func(data StageData)) {
	for {
		select {
		case <-ctx.Done():
			return
		case data := <-s.In:
			f(data)
		}
	}
}

// StageData holds data that is passed through stages.
type StageData struct {
	// Labels is the set of labels associated with a log line.
	Labels model.LabelSet

	// Extracted is the extracted data map associated with a log line.
	Extracted map[string]interface{}

	// Time is the current timestamp associated with a log line.
	Time time.Time

	// Entry is the current log line's text value.
	Entry string
}

// New creates a new AsyncStage for the given type and configuration.
func New(logger log.Logger, jobName *string, stageType string,
	cfg interface{}, registerer prometheus.Registerer) (AsyncStage, error) {
	var (
		s   Stage
		as  AsyncStage
		err error
	)

	switch stageType {
	case StageTypeDocker:
		s, err = NewDocker(logger, registerer)
		if err != nil {
			return nil, err
		}
	case StageTypeCRI:
		s, err = NewCRI(logger, registerer)
		if err != nil {
			return nil, err
		}
	case StageTypeJSON:
		s, err = newJSONStage(logger, cfg)
		if err != nil {
			return nil, err
		}
	case StageTypeRegex:
		s, err = newRegexStage(logger, cfg)
		if err != nil {
			return nil, err
		}
	case StageTypeMetric:
		s, err = newMetricStage(logger, cfg, registerer)
		if err != nil {
			return nil, err
		}
	case StageTypeLabel:
		s, err = newLabelStage(logger, cfg)
		if err != nil {
			return nil, err
		}
	case StageTypeTimestamp:
		s, err = newTimestampStage(logger, cfg)
		if err != nil {
			return nil, err
		}
	case StageTypeOutput:
		s, err = newOutputStage(logger, cfg)
		if err != nil {
			return nil, err
		}
	case StageTypeMatch:
		s, err = newMatcherStage(logger, jobName, cfg, registerer)
		if err != nil {
			return nil, err
		}
	case StageTypeTemplate:
		s, err = newTemplateStage(logger, cfg)
		if err != nil {
			return nil, err
		}
	case StageTypeTenant:
		s, err = newTenantStage(logger, cfg)
		if err != nil {
			return nil, err
		}
	default:
		return nil, errors.Errorf("Unknown stage type: %s", stageType)
	}

	if as == nil && s != nil {
		as = MakeAsync(s)
	}
	return as, nil
}

// MakeAsync takes a synchronous stage and converts it into an AsyncStage.
func MakeAsync(s Stage) AsyncStage {
	return &asyncWrapper{s}
}

type asyncWrapper struct{ s Stage }

func (w *asyncWrapper) Name() string { return w.s.Name() }

func (w *asyncWrapper) ProcessAsync(ctx context.Context, stream DataStream) {
	stream.Do(ctx, func(data StageData) {
		w.s.Process(data.Labels, data.Extracted, &data.Time, &data.Entry)
		stream.Send(data)
	})
}

// RunSync runs a stage synchronously, returning as soon as data is received.
func RunSync(s AsyncStage, labels model.LabelSet, extracted map[string]interface{}, t *time.Time, entry *string) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	in := make(chan StageData)
	out := make(chan StageData)

	stream := DataStream{In: in, Out: out}
	go s.ProcessAsync(ctx, stream)

	in <- StageData{
		Labels:    labels,
		Extracted: extracted,
		Time:      *t,
		Entry:     *entry,
	}

	data := <-out
	*t = data.Time
	*entry = data.Entry
}
