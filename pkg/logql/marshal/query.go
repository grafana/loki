package marshal

import (
	"fmt"

	"github.com/grafana/loki/pkg/loghttp"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logql"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/promql"
)

// NewResultValue constructs a ResultValue from a promql.Value
func NewResultValue(v promql.Value) (loghttp.ResultValue, error) {
	var err error
	var value loghttp.ResultValue

	switch v.Type() {
	case loghttp.ResultTypeStream:
		s, ok := v.(logql.Streams)

		if !ok {
			return nil, fmt.Errorf("unexpected type %T for streams", s)
		}

		value, err = NewStreams(s)

		if err != nil {
			return nil, err
		}
	case loghttp.ResultTypeVector:
		vector, ok := v.(promql.Vector)

		if !ok {
			return nil, fmt.Errorf("unexpected type %T for vector", vector)
		}

		value = NewVector(vector)
	case loghttp.ResultTypeMatrix:
		m, ok := v.(promql.Matrix)

		if !ok {
			return nil, fmt.Errorf("unexpected type %T for matrix", m)
		}

		value = NewMatrix(m)
	default:
		return nil, fmt.Errorf("v1 endpoints do not support type %s", v.Type())
	}

	return value, nil
}

// NewStreams constructs a Streams from a logql.Streams
func NewStreams(s logql.Streams) (loghttp.Streams, error) {
	var err error
	ret := make([]loghttp.Stream, len(s))

	for i, stream := range s {
		ret[i], err = NewStream(stream)

		if err != nil {
			return nil, err
		}
	}

	return ret, nil
}

// NewStream constructs a Stream from a logproto.Stream
func NewStream(s *logproto.Stream) (loghttp.Stream, error) {
	labels, err := NewLabelSet(s.Labels)
	if err != nil {
		return loghttp.Stream{}, err
	}

	ret := loghttp.Stream{
		Labels:  labels,
		Entries: make([]loghttp.Entry, len(s.Entries)),
	}

	for i, e := range s.Entries {
		ret.Entries[i] = NewEntry(e)
	}

	return ret, nil
}

// NewEntry constructs an Entry from a logproto.Entry
func NewEntry(e logproto.Entry) loghttp.Entry {
	return loghttp.Entry{
		Timestamp: e.Timestamp,
		Line:      e.Line,
	}
}

// NewVector constructs a Vector from a promql.Vector
func NewVector(v promql.Vector) loghttp.Vector {
	ret := make([]model.Sample, len(v))

	for i, s := range v {
		ret[i] = NewSample(s)
	}

	return ret
}

// NewSample constructs a model.Sample from a promql.Sample
func NewSample(s promql.Sample) model.Sample {

	ret := model.Sample{
		Value:     model.SampleValue(s.V),
		Timestamp: model.Time(s.T),
		Metric:    NewMetric(s.Metric),
	}

	return ret
}

// NewMatrix constructs a Matrix from a promql.Matrix
func NewMatrix(m promql.Matrix) loghttp.Matrix {
	ret := make([]model.SampleStream, len(m))

	for i, s := range m {
		ret[i] = NewSampleStream(s)
	}

	return ret
}

// NewSampleStream constructs a model.SampleStream from a promql.Series
func NewSampleStream(s promql.Series) model.SampleStream {
	ret := model.SampleStream{
		Metric: NewMetric(s.Metric),
		Values: make([]model.SamplePair, len(s.Points)),
	}

	for i, p := range s.Points {
		ret.Values[i].Timestamp = model.Time(p.T)
		ret.Values[i].Value = model.SampleValue(p.V)
	}

	return ret
}

// NewMetric constructs a labels.Labels from a model.Metric
func NewMetric(l labels.Labels) model.Metric {
	ret := make(map[model.LabelName]model.LabelValue)

	for _, label := range l {
		ret[model.LabelName(label.Name)] = model.LabelValue(label.Value)
	}

	return ret
}
