package syntax

import (
	"github.com/prometheus/prometheus/model/labels"
	promql_parser "github.com/prometheus/prometheus/promql/parser"
)

// ParseLabels parses labels from a string using logql parser.
func ParseLabels(lbs string) (labels.Labels, error) {
	ls, err := promql_parser.ParseMetric(lbs)
	if err != nil {
		return labels.EmptyLabels(), err
	}

	// Use the label builder to trim empty label values.
	// Empty label values are equivalent to absent labels
	// in Prometheus, but they unfortunately alter the
	// Hash values created. This can cause problems in Loki
	// if we can't rely on a set of labels to have a deterministic
	// hash value.
	// Therefore we must normalize early in the write path.
	// See https://github.com/grafana/loki/pull/7355
	// for more information
	return labels.NewBuilder(ls).Labels(), nil
}
