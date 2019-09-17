package v1

import "github.com/prometheus/prometheus/promql"

//LabelResponse represents the http json response to a label query
type LabelResponse struct {
	Values []string `json:"values,omitempty"`
}

//Labels is a key/value pair mapping of labels
type LabelSet map[string]string

func NewLabelSet(s string) (LabelSet, error) {
	// gross to rely on promql.ParseMetric() to do this
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
