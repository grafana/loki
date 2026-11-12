package marshal

import (
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/loghttp"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
)

// NewLabelSet constructs a Labelset from a promql metric list as a string
func NewLabelSet(s string) (loghttp.LabelSet, error) {
	lbls, err := syntax.ParseMetric(s)
	if err != nil {
		return nil, err
	}

	ret := make(map[string]string, lbls.Len())
	lbls.Range(func(lbl labels.Label) {
		ret[lbl.Name] = lbl.Value
	})

	return ret, nil
}
