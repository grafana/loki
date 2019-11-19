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
	return s, nil
}
