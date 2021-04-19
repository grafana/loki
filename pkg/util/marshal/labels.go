package marshal

import (
	"github.com/prometheus/prometheus/promql/parser"

	"github.com/grafana/loki/pkg/loghttp"
)

// NewLabelSet constructs a Labelset from a promql metric list as a string
func NewLabelSet(s string) (loghttp.LabelSet, error) {
	labels, err := parser.ParseMetric(s)
	if err != nil {
		return nil, err
	}
	ret := make(map[string]string)

	for _, l := range labels {
		ret[l.Name] = l.Value
	}

	return ret, nil
}
