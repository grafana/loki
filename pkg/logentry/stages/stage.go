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
	StageTypeMetric    = "metric"
	StageTypeLabel     = "labels"
	StageTypeTimestamp = "timestamp"
	StageTypeOutput    = "output"
	StageTypeDocker    = "docker"
	StageTypeCRI       = "cri"
)

// Stage takes an existing set of labels, timestamp and log entry and returns either a possibly mutated
// timestamp and log entry
type Stage interface {
	Process(labels model.LabelSet, extracted map[string]interface{}, time *time.Time, entry *string)
}

// StageFunc is modelled on http.HandlerFunc.
type StageFunc func(labels model.LabelSet, extracted map[string]interface{}, time *time.Time, entry *string)

// Process implements EntryHandler.
func (s StageFunc) Process(labels model.LabelSet, extracted map[string]interface{}, time *time.Time, entry *string) {
	s(labels, extracted, time, entry)
}

// New creates a new stage for the given type and configuration.
func New(logger log.Logger, stageType string, cfg interface{}, registerer prometheus.Registerer) (Stage, error) {
	var s Stage
	var err error
	switch stageType {
	case StageTypeDocker:
		s, err = NewDocker(logger)
		if err != nil {
			return nil, err
		}
	case StageTypeCRI:
		s, err = NewCRI(logger)
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
		s, err = newMetric(cfg, registerer)
		if err != nil {
			return nil, err
		}
	case StageTypeLabel:
		s, err = newLabel(logger, cfg)
		if err != nil {
			return nil, err
		}
	case StageTypeTimestamp:
		s, err = newTimestamp(logger, cfg)
		if err != nil {
			return nil, err
		}
	case StageTypeOutput:
		s, err = newOutput(logger, cfg)
		if err != nil {
			return nil, err
		}
	default:
		return nil, errors.Errorf("Unknown stage type: %s", stageType)
	}
	return s, nil
}
