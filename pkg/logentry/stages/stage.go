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
	StageTypeReplace   = "replace"
	StageTypeMetric    = "metrics"
	StageTypeLabel     = "labels"
	StageTypeLabelDrop = "labeldrop"
	StageTypeTimestamp = "timestamp"
	StageTypeOutput    = "output"
	StageTypeDocker    = "docker"
	StageTypeCRI       = "cri"
	StageTypeMatch     = "match"
	StageTypeTemplate  = "template"
	StageTypePipeline  = "pipeline"
	StageTypeTenant    = "tenant"
	StageTypeDrop      = "drop"
)

// Stage takes an existing set of labels, timestamp and log entry and returns either a possibly mutated
// timestamp and log entry
type Processor interface {
	Process(labels model.LabelSet, extracted map[string]interface{}, time *time.Time, entry *string)
	Name() string
}

type Entry struct {
	Labels    model.LabelSet
	Extracted map[string]interface{}
	Timestamp time.Time
	Line      string
}

type Stage interface {
	Name() string
	Run(chan Entry) chan Entry
}

type stageProcessor struct {
	Processor
}

func (s stageProcessor) Run(in chan Entry) chan Entry {
	return RunWith(in, func(e Entry) Entry {
		s.Process(e.Labels, e.Extracted, &e.Timestamp, &e.Line)
		return e
	})
}

func toStage(p Processor, err error) (Stage, error) {
	if err != nil {
		return nil, err
	}
	return &stageProcessor{Processor: p}, nil
}

// StageFunc is modelled on http.HandlerFunc.
type StageFunc func(labels model.LabelSet, extracted map[string]interface{}, time *time.Time, entry *string)

// Process implements EntryHandler.
func (s StageFunc) Process(labels model.LabelSet, extracted map[string]interface{}, time *time.Time, entry *string) {
	s(labels, extracted, time, entry)
}

// New creates a new stage for the given type and configuration.
func New(logger log.Logger, jobName *string, stageType string,
	cfg interface{}, registerer prometheus.Registerer) (Stage, error) {
	var s Stage
	var err error
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
		s, err = toStage(newJSONStage(logger, cfg))
		if err != nil {
			return nil, err
		}
	case StageTypeRegex:
		s, err = toStage(newRegexStage(logger, cfg))
		if err != nil {
			return nil, err
		}
	case StageTypeMetric:
		s, err = toStage(newMetricStage(logger, cfg, registerer))
		if err != nil {
			return nil, err
		}
	case StageTypeLabel:
		s, err = toStage(newLabelStage(logger, cfg))
		if err != nil {
			return nil, err
		}
	case StageTypeLabelDrop:
		s, err = toStage(newLabelDropStage(cfg))
		if err != nil {
			return nil, err
		}
	case StageTypeTimestamp:
		s, err = toStage(newTimestampStage(logger, cfg))
		if err != nil {
			return nil, err
		}
	case StageTypeOutput:
		s, err = toStage(newOutputStage(logger, cfg))
		if err != nil {
			return nil, err
		}
	case StageTypeMatch:
		s, err = newMatcherStage(logger, jobName, cfg, registerer)
		if err != nil {
			return nil, err
		}
	case StageTypeTemplate:
		s, err = toStage(newTemplateStage(logger, cfg))
		if err != nil {
			return nil, err
		}
	case StageTypeTenant:
		s, err = toStage(newTenantStage(logger, cfg))
		if err != nil {
			return nil, err
		}
	case StageTypeReplace:
		s, err = toStage(newReplaceStage(logger, cfg))
		if err != nil {
			return nil, err
		}
	case StageTypeDrop:
		s, err = newDropStage(logger, cfg, registerer)
		if err != nil {
			return nil, err
		}
	default:
		return nil, errors.Errorf("Unknown stage type: %s", stageType)
	}
	return s, nil
}
