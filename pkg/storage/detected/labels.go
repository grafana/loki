package detected

import (
	"github.com/axiomhq/hyperloglog"

	"github.com/grafana/loki/v3/pkg/logproto"
)

type UnmarshaledDetectedLabel struct {
	Label  string
	Sketch *hyperloglog.Sketch
}

func unmarshalDetectedLabel(l *logproto.DetectedLabel) (*UnmarshaledDetectedLabel, error) {
	sketch := hyperloglog.New()
	err := sketch.UnmarshalBinary(l.Sketch)
	if err != nil {
		return nil, err
	}
	return &UnmarshaledDetectedLabel{
		Label:  l.Label,
		Sketch: sketch,
	}, nil
}

func (m *UnmarshaledDetectedLabel) Merge(dl *logproto.DetectedLabel) error {
	sketch := hyperloglog.New()
	err := sketch.UnmarshalBinary(dl.Sketch)
	if err != nil {
		return err
	}
	return m.Sketch.Merge(sketch)
}

func MergeLabels(labels []*logproto.DetectedLabel) (result []*logproto.DetectedLabel, err error) {
	mergedLabels := make(map[string]*UnmarshaledDetectedLabel)
	for _, label := range labels {
		l, ok := mergedLabels[label.Label]
		if !ok {
			unmarshaledLabel, err := unmarshalDetectedLabel(label)
			if err != nil {
				return nil, err
			}
			mergedLabels[label.Label] = unmarshaledLabel
		} else {
			err := l.Merge(label)
			if err != nil {
				return nil, err
			}
		}
	}

	for _, label := range mergedLabels {
		detectedLabel := &logproto.DetectedLabel{
			Label:       label.Label,
			Cardinality: label.Sketch.Estimate(),
			Sketch:      nil,
		}

		result = append(result, detectedLabel)
	}

	return
}
