package util

import (
	"github.com/cortexproject/cortex/pkg/ingester/client"
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
