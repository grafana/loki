package stages

import (
	"github.com/go-kit/log"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/mitchellh/mapstructure"
	"github.com/prometheus/common/model"
)

func newNonIndexedLabelsStage(params StageCreationParams) (Stage, error) {
	cfgs := &LabelsConfig{}
	err := mapstructure.Decode(params.config, cfgs)
	if err != nil {
		return nil, err
	}
	err = validateLabelsConfig(*cfgs)
	if err != nil {
		return nil, err
	}
	return &nonIndexedLabelsStage{
		cfgs:   *cfgs,
		logger: params.logger,
	}, nil
}

type nonIndexedLabelsStage struct {
	cfgs   LabelsConfig
	logger log.Logger
}

func (s *nonIndexedLabelsStage) Name() string {
	return StageTypeNonIndexedLabels
}

func (s *nonIndexedLabelsStage) Run(in chan Entry) chan Entry {
	return RunWith(in, func(e Entry) Entry {
		processLabelsConfigs(s.logger, e.Extracted, s.cfgs, func(labelName model.LabelName, labelValue model.LabelValue) {
			e.NonIndexedLabels = append(e.NonIndexedLabels, logproto.LabelAdapter{Name: string(labelName), Value: string(labelValue)})
		})
		return e
	})
}
