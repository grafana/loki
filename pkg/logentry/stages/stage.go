package stages

import (
	"time"

	"github.com/go-kit/kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
)

// Stage takes an existing set of labels, timestamp and log entry and returns either a possibly mutated
// timestamp and log entry
type Stage interface {
	Process(labels model.LabelSet, time *time.Time, entry *string)
}

// StageFunc is modelled on http.HandlerFunc.
type StageFunc func(labels model.LabelSet, time *time.Time, entry *string)

// Process implements EntryHandler.
func (s StageFunc) Process(labels model.LabelSet, time *time.Time, entry *string) {
	s(labels, time, entry)
}

func New(logger log.Logger, stageType string, cfg interface{}, registerer prometheus.Registerer) (Stage, error) {
	switch stageType {
	case "docker":
		return NewDocker(logger)
	case "cri":
		return NewCRI(logger)
	}
	stageConf, err := NewConfig(cfg)
	if err != nil {
		return nil, err
	}
	m, err := newMutator(logger, stageType, stageConf)
	if err != nil {
		return nil, err
	}
	return withMatcher(withMetric(m, stageConf.Metrics, registerer), stageConf.Match)
}
