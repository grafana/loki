package util

import (
	"github.com/cortexproject/cortex/pkg/ingester/client"
	"github.com/cortexproject/cortex/pkg/util/wire"
	"github.com/grafana/loki/pkg/parser"
)

// ToClientLabels parses the labels and converts them to the Cortex type.
func ToClientLabels(labels string) ([]client.LabelPair, error) {
	ls, err := parser.Labels(labels)
	if err != nil {
		return nil, err
	}

	pairs := make([]client.LabelPair, 0, len(ls))
	for i := 0; i < len(ls); i++ {
		pairs = append(pairs, client.LabelPair{
			Name:  wire.Bytes(ls[i].Name),
			Value: wire.Bytes(ls[i].Value),
		})
	}
	return pairs, nil
}
