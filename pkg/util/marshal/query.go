package marshal

import (
	"fmt"
	"strconv"

	"github.com/pkg/errors"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/promql"
	"github.com/prometheus/prometheus/promql/parser"
	"gopkg.in/launchdarkly/go-jsonstream.v1/jwriter"

	"github.com/grafana/loki/pkg/loghttp"
	"github.com/grafana/loki/pkg/logproto"
	"github.com/grafana/loki/pkg/logqlmodel"
)

func WriteResultValue(v parser.Value, w *jwriter.Writer) error {
	root:= w.Object()
	root.Name("status").String("success")
	dataWriter := root.Name("data")
	err := writeData(v, dataWriter)
	if err != nil {
		return err
	}
	root.End()
	return nil
}

func writeData(v parser.Value, w *jwriter.Writer) error {
	dataObj := w.Object()
	dataObj.Name("resultType").String(string(v.Type()))
	resultWriter := dataObj.Name("result")
	err := writeResult(v, resultWriter)
	if err != nil {
		return err
	}
	//dataObj.Name("stats")

	dataObj.End()
	return nil
}

func writeResult(v parser.Value, w *jwriter.Writer) error {
	switch v.Type() {
	case loghttp.ResultTypeStream:
		s, ok := v.(logqlmodel.Streams)

		if !ok {
			return fmt.Errorf("unexpected type %T for streams", s)
		}

		writeStreams(s, w)

	case loghttp.ResultTypeScalar:
		scalar, ok := v.(promql.Scalar)

		if !ok {
			return fmt.Errorf("unexpected type %T for scalar", scalar)
		}

		writeScalar(scalar, w)

	case loghttp.ResultTypeVector:
		vector, ok := v.(promql.Vector)

		if !ok {
			return fmt.Errorf("unexpected type %T for vector", vector)
		}

		writeVector(vector, w)

	case loghttp.ResultTypeMatrix:
		m, ok := v.(promql.Matrix)

		if !ok {
			return fmt.Errorf("unexpected type %T for matrix", m)
		}

		writeMatrix(m, w)

	default:
		w.Null()
		return fmt.Errorf("v1 endpoints do not support type %s", v.Type())
	}
	return nil
}

func writeStreams(s logqlmodel.Streams, w *jwriter.Writer) error {
	arr := w.Array()
	defer arr.End()

	for _, stream := range s {
		err := writeStream(stream, arr)
		if err != nil {
			return err
		}
	}

	return nil
}

func writeStream(s logproto.Stream, arr jwriter.ArrayState) error {

	obj := arr.Object()
	defer obj.End()

	labels := obj.Name("stream").Object()
	labelSet, err := NewLabelSet(s.Labels)
	if err != nil {
		return err
	}

	for key, value := range labelSet {
		labels.Name(key).String(value)
	}
	labels.End()

	entries := obj.Name("values").Array()
	for _, e := range s.Entries {
		ea := entries.Array()
		ea.String(strconv.FormatInt(e.Timestamp.UnixNano(), 10))
		ea.String(e.Line)
		ea.End()
	}
	entries.End()

	return nil
}

func writeScalar(v promql.Scalar, w *jwriter.Writer) error {
	w.Null()
	return nil
}

func writeVector(v promql.Vector, w *jwriter.Writer) error {
	arr := w.Array()
	defer arr.End()
	return nil
}

func writeMatrix(v promql.Matrix, w *jwriter.Writer) error {
	arr := w.Array()
	defer arr.End()
	return nil
}

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
