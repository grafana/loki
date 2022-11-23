package marshal

import (
	"fmt"
	"strconv"

	jsoniter "github.com/json-iterator/go"
	"github.com/pkg/errors"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/promql"
	"github.com/prometheus/prometheus/promql/parser"

	"github.com/grafana/loki/pkg/loghttp"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logqlmodel"
)

// NewResultValue constructs a ResultValue from a promql.Value
func NewResultValue(v parser.Value) (loghttp.ResultValue, error) {
	var err error
	var value loghttp.ResultValue

	switch v.Type() {
	case loghttp.ResultTypeStream:
		s, ok := v.(logqlmodel.Streams)

		if !ok {
			return nil, fmt.Errorf("unexpected type %T for streams", s)
		}

		value, err = NewStreams(s)

		if err != nil {
			return nil, err
		}

	case loghttp.ResultTypeScalar:
		scalar, ok := v.(promql.Scalar)

		if !ok {
			return nil, fmt.Errorf("unexpected type %T for scalar", scalar)
		}

		value = NewScalar(scalar)

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
func NewStreams(s logqlmodel.Streams) (loghttp.Streams, error) {
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
func NewStream(s logproto.Stream) (loghttp.Stream, error) {
	labels, err := NewLabelSet(s.Labels)
	if err != nil {
		return loghttp.Stream{}, errors.Wrapf(err, "err while creating labelset for %s", s.Labels)
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

func NewScalar(s promql.Scalar) loghttp.Scalar {
	return loghttp.Scalar{
		Timestamp: model.Time(s.T),
		Value:     model.SampleValue(s.V),
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

func EncodeResult(v logqlmodel.Result, s *jsoniter.Stream) error {
	s.WriteObjectStart()
	s.WriteObjectField("status")
	s.WriteString("success")

	s.WriteMore()
	s.WriteObjectField("data")
	err := encodeData(v, s)
	if err != nil {
		return err
	}

	s.WriteObjectEnd()
	return nil
}

func encodeData(v logqlmodel.Result, s *jsoniter.Stream) error {
	s.WriteObjectStart()

	s.WriteObjectField("resultType")
	s.WriteString(string(v.Data.Type()))

	s.WriteMore()
	s.WriteObjectField("result")
	err := encodeResult(v.Data, s)
	if err != nil {
		return err
	}

	s.WriteMore()
	s.WriteObjectField("stats")
	s.WriteVal(v.Statistics)

	s.WriteObjectEnd()
	s.Flush()
	return nil
}

func encodeResult(v parser.Value, s *jsoniter.Stream) error {
	switch v.Type() {
	case loghttp.ResultTypeStream:
		result, ok := v.(logqlmodel.Streams)

		if !ok {
			return fmt.Errorf("unexpected type %T for streams", s)
		}

		return encodeStreams(result, s)
	case loghttp.ResultTypeScalar:
		scalar, ok := v.(promql.Scalar)

		if !ok {
			return fmt.Errorf("unexpected type %T for scalar", scalar)
		}

		encodeScalar(scalar, s)

	case loghttp.ResultTypeVector:
		vector, ok := v.(promql.Vector)

		if !ok {
			return fmt.Errorf("unexpected type %T for vector", vector)
		}

		encodeVector(vector, s)

	case loghttp.ResultTypeMatrix:
		m, ok := v.(promql.Matrix)

		if !ok {
			return fmt.Errorf("unexpected type %T for matrix", m)
		}

		encodeMatrix(m, s)

	default:
		s.WriteNil()
		return fmt.Errorf("v1 endpoints do not support type %s", v.Type())
	}
	return nil
}

func encodeStreams(streams logqlmodel.Streams, s *jsoniter.Stream) error {
	s.WriteArrayStart()
	defer s.WriteArrayEnd()

	for i, stream := range streams {
		if i > 0 {
			s.WriteMore()
		}

		err := encodeStream(stream, s)
		if err != nil {
			return err
		}
	}

	return nil
}

func encodeStream(stream logproto.Stream, s *jsoniter.Stream) error {
	s.WriteObjectStart()
	defer s.WriteObjectEnd()

	s.WriteObjectField("stream")
	s.WriteObjectStart()
	labels, err := parser.ParseMetric(stream.Labels)
	if err != nil {
		return err
	}

	for i, l := range labels {
		if i > 0 {
			s.WriteMore()
		}

		s.WriteObjectField(l.Name)
		s.WriteString(l.Value)
	}
	s.WriteObjectEnd()
	s.Flush()

	s.WriteMore()
	s.WriteObjectField("values")
	s.WriteArrayStart()

	for i, e := range stream.Entries {
		if i > 0 {
			s.WriteMore()
		}

		s.WriteArrayStart()
		s.WriteRaw(`"`)
		s.WriteRaw(strconv.FormatInt(e.Timestamp.UnixNano(), 10))
		s.WriteRaw(`"`)
		s.WriteMore()
		s.WriteStringWithHTMLEscaped(e.Line)
		s.WriteArrayEnd()

		s.Flush()
	}

	s.WriteArrayEnd()

	return nil
}

func encodeScalar(v promql.Scalar, s *jsoniter.Stream) {
	s.WriteArrayStart()
	defer s.WriteArrayEnd()

	s.WriteRaw(model.Time(v.T).String())
	s.WriteMore()
	s.WriteString(model.SampleValue(v.V).String())
}

func encodeVector(v promql.Vector, s *jsoniter.Stream) {
	s.WriteArrayStart()
	defer s.WriteArrayEnd()

	for i, sample := range v {
		if i > 0 {
			s.WriteMore()
		}
		encodeSample(sample, s)
		s.Flush()
	}
}

func encodeSample(sample promql.Sample, s *jsoniter.Stream) {
	s.WriteObjectStart()
	defer s.WriteObjectEnd()

	s.WriteObjectField("metric")
	encodeMetric(sample.Metric, s)

	s.WriteMore()
	s.WriteObjectField("value")
	encodeValue(sample.T, sample.V, s)
}

func encodeValue(T int64, V float64, s *jsoniter.Stream) {
	s.WriteArrayStart()
	s.WriteRaw(model.Time(T).String())
	s.WriteMore()
	s.WriteString(model.SampleValue(V).String())
	s.WriteArrayEnd()
}

func encodeMetric(l labels.Labels, s *jsoniter.Stream) {
	s.WriteObjectStart()
	for i, label := range l {
		if i > 0 {
			s.WriteMore()
		}

		s.WriteObjectField(label.Name)
		s.WriteString(label.Value)
	}
	s.WriteObjectEnd()
}

func encodeMatrix(m promql.Matrix, s *jsoniter.Stream) {
	s.WriteArrayStart()
	defer s.WriteArrayEnd()

	for i, sampleStream := range m {
		if i > 0 {
			s.WriteMore()
		}
		encodeSampleStream(sampleStream, s)
		s.Flush()
	}
}

func encodeSampleStream(stream promql.Series, s *jsoniter.Stream) {
	s.WriteObjectStart()
	defer s.WriteObjectEnd()

	s.WriteObjectField("metric")
	encodeMetric(stream.Metric, s)

	s.WriteMore()
	s.WriteObjectField("values")
	s.WriteArrayStart()
	for i, p := range stream.Points {
		if i > 0 {
			s.WriteMore()
		}
		encodeValue(p.T, p.V, s)
	}
	s.WriteArrayEnd()
}
