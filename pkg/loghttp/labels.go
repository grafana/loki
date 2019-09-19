package loghttp

import "github.com/prometheus/prometheus/promql"

// LabelResponse represents the http json response to a label query
type LabelResponse struct {
	Values []string `json:"values,omitempty"`
}

// LabelSet is a key/value pair mapping of labels
type LabelSet map[string]string

// NewLabelSet constructs a Labelset from a promql metric list as a string
func NewLabelSet(s string) (LabelSet, error) {
	labels, err := promql.ParseMetric(s)
	if err != nil {
		return nil, err
	}
	ret := make(map[string]string)

	for _, l := range labels {
		ret[l.Name] = l.Value
	}

	return ret, nil
}
