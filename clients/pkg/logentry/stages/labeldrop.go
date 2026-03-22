package stages

import (
	"time"

	"github.com/mitchellh/mapstructure"
	"github.com/pkg/errors"
	"github.com/prometheus/common/model"
)

const (
	// ErrEmptyLabelDropStageConfig error returned if config is empty
	ErrEmptyLabelDropStageConfig = "labeldrop stage config cannot be empty"
)

// LabelDropConfig is a slice of labels to be dropped
type LabelDropConfig []string

func validateLabelDropConfig(c LabelDropConfig) error {
	if len(c) < 1 {
		return errors.New(ErrEmptyLabelDropStageConfig)
	}

	return nil
}

func newLabelDropStage(configs interface{}) (Stage, error) {
	cfgs := &LabelDropConfig{}
	err := mapstructure.Decode(configs, cfgs)
	if err != nil {
		return nil, err
	}

	err = validateLabelDropConfig(*cfgs)
	if err != nil {
		return nil, err
	}

	return toStage(&labelDropStage{
		cfgs: *cfgs,
	}), nil
}

type labelDropStage struct {
	cfgs LabelDropConfig
}

// Process implements Stage
func (l *labelDropStage) Process(labels model.LabelSet, _ map[string]interface{}, _ *time.Time, _ *string) {
	for _, label := range l.cfgs {
		delete(labels, model.LabelName(label))
	}
}

// Name implements Stage
func (l *labelDropStage) Name() string {
	return StageTypeLabelDrop
}
