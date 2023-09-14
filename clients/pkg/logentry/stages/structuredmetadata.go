package stages

import (
	"github.com/go-kit/log"
	"github.com/mitchellh/mapstructure"
	"github.com/prometheus/common/model"

	"github.com/grafana/loki/pkg/logproto"
)

func newStructuredMetadataStage(params StageCreationParams) (Stage, error) {
	cfgs := &LabelsConfig{}
	err := mapstructure.Decode(params.config, cfgs)
	if err != nil {
		return nil, err
	}
	err = validateLabelsConfig(*cfgs)
	if err != nil {
		return nil, err
	}
	return &structuredMetadataStage{
		cfgs:   *cfgs,
		logger: params.logger,
	}, nil
}

type structuredMetadataStage struct {
	cfgs   LabelsConfig
	logger log.Logger
}

func (s *structuredMetadataStage) Name() string {
	return StageTypeStructuredMetadata
}

func (s *structuredMetadataStage) Run(in chan Entry) chan Entry {
	return RunWith(in, func(e Entry) Entry {
		processLabelsConfigs(s.logger, e.Extracted, s.cfgs, func(labelName model.LabelName, labelValue model.LabelValue) {
			e.StructuredMetadata = append(e.StructuredMetadata, logproto.LabelAdapter{Name: string(labelName), Value: string(labelValue)})
		})
		return e
	})
}
