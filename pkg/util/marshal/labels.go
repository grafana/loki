package marshal

import (
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/promql/parser"

	"github.com/grafana/loki/pkg/loghttp"
	"github.com/grafana/loki/pkg/logproto"
)

// NewLabelSet constructs a Labelset from a promql metric list as a string
func NewLabelSet(s string) (loghttp.LabelSet, error) {
	lbls, err := parser.ParseMetric(s)
	if err != nil {
		return nil, err
	}

	return NewLabelSetFromLabels(lbls), nil
}

func NewLabelSetFromLabels(lbls labels.Labels) loghttp.LabelSet {
	if len(lbls) == 0 {
		return nil
	}

	ret := make(map[string]string, len(lbls))
	for _, l := range lbls {
		ret[l.Name] = l.Value
	}

	return ret
}

func NewCategorizedLabelSet(labels logproto.CategorizedLabels) loghttp.CategorizedLabelSet {
	return loghttp.CategorizedLabelSet{
		Stream:             NewLabelSetFromLabels(logproto.FromLabelAdaptersToLabels(labels.Stream)),
		StructuredMetadata: NewLabelSetFromLabels(logproto.FromLabelAdaptersToLabels(labels.StructuredMetadata)),
		Parsed:             NewLabelSetFromLabels(logproto.FromLabelAdaptersToLabels(labels.Parsed)),
	}
}
