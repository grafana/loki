package stages

import (
	"time"

	"github.com/mitchellh/mapstructure"
	"github.com/pkg/errors"
	"github.com/prometheus/common/model"
)

const (
	// ErrEmptyLabelAllowStageConfig error returned if config is empty
	ErrEmptyLabelAllowStageConfig = "labelallow stage config cannot be empty"
)

// labelallowConfig is a slice of labels to be included
type LabelAllowConfig []string

func validateLabelAllowConfig(c LabelAllowConfig) error {
	if c == nil || len(c) < 1 {
		return errors.New(ErrEmptyLabelAllowStageConfig)
	}

	return nil
}

func newLabelAllowStage(configs interface{}) (Stage, error) {
	cfgs := &LabelAllowConfig{}
	err := mapstructure.Decode(configs, cfgs)
	if err != nil {
		return nil, err
	}

	err = validateLabelAllowConfig(*cfgs)
	if err != nil {
		return nil, err
	}

	labelMap := make(map[string]struct{})
	for _, label := range *cfgs {
		labelMap[label] = struct{}{}
	}

	return toStage(&labelAllowStage{
		labels: labelMap,
	}), nil
}

type labelAllowStage struct {
	labels map[string]struct{}
}

// Process implements Stage
func (l *labelAllowStage) Process(labels model.LabelSet, extracted map[string]interface{}, t *time.Time, entry *string) {
	for label := range labels {
		if _, ok := l.labels[string(label)]; !ok {
			delete(labels, label)
		}
	}
}

// Name implements Stage
func (l *labelAllowStage) Name() string {
	return StageTypeLabelAllow
}
