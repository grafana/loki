package builder

import (
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/prometheus/prometheus/model/labels"
)

// parseLabelsOrNil parses a stream labels string and returns nil on empty/invalid
// input. Tests and benchmarks use this helper when they don't already have
// parsed labels from kafka.Decoder.Decode.
func parseLabelsOrNil(streamLabels string) *labels.Labels {
	if len(streamLabels) == 0 {
		return nil
	}
	parsed, err := syntax.ParseLabels(streamLabels)
	if err != nil {
		return nil
	}
	return &parsed
}
