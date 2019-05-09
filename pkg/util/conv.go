package util

import (
	"github.com/cortexproject/cortex/pkg/ingester/client"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/promql"
)

// ToClientLabels parses the labels and converts them to the Cortex type.
func ToClientLabels(labels string) ([]client.LabelAdapter, error) {
	ls, err := promql.ParseMetric(labels)
	if err != nil {
		return nil, err
	}

	return client.FromLabelsToLabelAdapaters(ls), nil
}

// ModelLabelSetToMap convert a model.LabelSet to a map[string]string
func ModelLabelSetToMap(m model.LabelSet) map[string]string {
	result := map[string]string{}
	for k, v := range m {
		result[string(k)] = string(v)
	}
	return result
}
