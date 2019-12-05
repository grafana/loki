package stages

import (
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

// StageFunc is modelled on http.HandlerFunc.
type StageFunc func(labels model.LabelSet, extracted map[string]interface{}, time *time.Time, entry *string)

// Process implements EntryHandler.
func (s StageFunc) Process(labels model.LabelSet, extracted map[string]interface{}, time *time.Time, entry *string) {
	s(labels, extracted, time, entry)
}

func (s StageFunc) Name() string { return "function" }

// ChainedStage is like Stage, but should call the next method as many times as desired for passing
// through data.
type ChainedStage interface {
	ProcessChain(data StageData, next func(StageData))
	Name() string
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
	cfg interface{}, registerer prometheus.Registerer) (ChainedStage, error) {
	var (
		s   Stage
		cs  ChainedStage
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

	if cs == nil && s != nil {
		cs = MakeChained(s)
	}
	return cs, nil
}

// MakeChained takes a synchronous stage and converts it into an ChainedStage.
func MakeChained(s Stage) ChainedStage {
	return &chainedWrapper{s}
}

type chainedWrapper struct{ s Stage }

func (w *chainedWrapper) ProcessChain(data StageData, next func(StageData)) {
	w.s.Process(data.Labels, data.Extracted, &data.Time, &data.Entry)
	next(data)
}

func (w *chainedWrapper) Name() string {
	return w.s.Name()
}

// FlattenStage creates a normal Stage from a Chain stage, allowing it to be executed by waiting for the
// callback to be called.
func FlattenStage(s ChainedStage) Stage {
	return &unchainedWrapper{s}
}

type unchainedWrapper struct {
	s ChainedStage
}

func (w *unchainedWrapper) Process(labels model.LabelSet, extracted map[string]interface{}, time *time.Time, entry *string) {
	wait := make(chan bool)
	go func() {
		in := StageData{
			Labels:    labels,
			Extracted: extracted,
			Time:      *time,
			Entry:     *entry,
		}

		w.s.ProcessChain(in, func(out StageData) {
			*time = out.Time
			*entry = out.Entry
			close(wait)
		})
	}()

	<-wait
}

func (w *unchainedWrapper) Name() string { return w.s.Name() }
