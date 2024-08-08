package stages

import (
	"github.com/go-kit/log"
	"github.com/mitchellh/mapstructure"
	"github.com/prometheus/common/model"

	"github.com/grafana/loki/v3/pkg/logproto"
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

// Cleanup implements Stage.
func (*structuredMetadataStage) Cleanup() {
	// no-op
}

func (s *structuredMetadataStage) Run(in chan Entry) chan Entry {
	return RunWith(in, func(e Entry) Entry {
		processLabelsConfigs(s.logger, e.Extracted, s.cfgs, func(labelName model.LabelName, labelValue model.LabelValue) {
			e.StructuredMetadata = append(e.StructuredMetadata, logproto.LabelAdapter{Name: string(labelName), Value: string(labelValue)})
		})
		return s.extractFromLabels(e)
	})
}

func (s *structuredMetadataStage) extractFromLabels(e Entry) Entry {
	labels := e.Labels
	foundLabels := []model.LabelName{}

	for lName, lSrc := range s.cfgs {
		labelKey := model.LabelName(*lSrc)
		if lValue, ok := labels[labelKey]; ok {
			e.StructuredMetadata = append(e.StructuredMetadata, logproto.LabelAdapter{Name: lName, Value: string(lValue)})
			foundLabels = append(foundLabels, labelKey)
		}
	}

	// Remove found labels, do this after append to structure metadata
	for _, fl := range foundLabels {
		delete(labels, fl)
	}
	e.Labels = labels
	return e
}
