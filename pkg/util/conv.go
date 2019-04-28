package util

import (
	"github.com/cortexproject/cortex/pkg/ingester/client"
	"github.com/grafana/loki/pkg/parser"
)

// ToClientLabels parses the labels and converts them to the Cortex type.
func ToClientLabels(labels string) ([]client.LabelAdapter, error) {
	ls, err := parser.Labels(labels)
	if err != nil {
		return nil, err
	}

	pairs := make([]client.LabelAdapter, 0, len(ls))
	for i := 0; i < len(ls); i++ {
		pairs = append(pairs, client.LabelAdapter{
			Name:  ls[i].Name,
			Value: ls[i].Value,
		})
	}
	return pairs, nil
}
