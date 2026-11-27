package marshal

import (
	"bytes"
	"encoding/json/jsontext"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/pkg/errors"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/promql"
	"github.com/prometheus/prometheus/promql/parser"

	"github.com/grafana/loki/v3/pkg/loghttp"
	legacy "github.com/grafana/loki/v3/pkg/loghttp/legacy"
	"github.com/grafana/loki/v3/pkg/logproto"
	logqlmodel "github.com/grafana/loki/v3/pkg/logql/model"
	"github.com/grafana/loki/v3/pkg/logql/model/stats"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/util/httpreq"
	utiljson "github.com/grafana/loki/v3/pkg/util/json"
)

var (
	// The rune error replacement is rejected by Prometheus hence replacing them with space.
	removeInvalidUtf = func(r rune) rune {
		if r == utf8.RuneError {
			return 32 // rune value for space
		}
		return r
	}
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
		if strings.ContainsRune(stream.Labels, utf8.RuneError) {
			stream.Labels = string(bytes.Map(removeInvalidUtf, []byte(stream.Labels)))
		}
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

	// Always use a non-nil slice (even if it's length 0) to be consistent with
	// the decoding.
	entries := make([]loghttp.Entry, 0, len(s.Entries))
	for _, ent := range s.Entries {
		conv := loghttp.Entry{
			Timestamp:          ent.Timestamp,
			Line:               ent.Line,
			StructuredMetadata: logproto.FromLabelAdaptersToLabels(ent.StructuredMetadata),
			Parsed:             logproto.FromLabelAdaptersToLabels(ent.Parsed),
		}
		entries = append(entries, conv)
	}

	ret := loghttp.Stream{
		Labels:  labels,
		Entries: entries,
	}

	return ret, nil
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
		Value:     model.SampleValue(s.F),
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
		Values: make([]model.SamplePair, len(s.Floats)),
	}

	for i, p := range s.Floats {
		ret.Values[i].Timestamp = model.Time(p.T)
		ret.Values[i].Value = model.SampleValue(p.F)
	}

	return ret
}

// NewMetric constructs a labels.Labels from a model.Metric
func NewMetric(l labels.Labels) model.Metric {
	ret := make(map[model.LabelName]model.LabelValue)

	l.Range(func(label labels.Label) {
		ret[model.LabelName(label.Name)] = model.LabelValue(label.Value)
	})

	return ret
}

func EncodeResult(data parser.Value, warnings []string, statistics stats.Result, enc *jsontext.Encoder, encodeFlags httpreq.EncodingFlags) error {
	e := utiljson.Latching(enc)
	e.Tokens(jsontext.BeginObject)
	e.StringField("status", "success")

	if len(warnings) > 0 {
		e.Tokens(jsontext.String("warnings"), jsontext.BeginArray)
		for _, w := range warnings {
			e.Tokens(jsontext.String(w))
		}
		e.Tokens(jsontext.EndArray)
	}

	e.Tokens(jsontext.String("data"))
	encodeData(data, statistics, e, encodeFlags)
	e.Tokens(jsontext.EndObject)
	return e.Err()
}

func EncodeTailResult(data legacy.TailResponse, enc *jsontext.Encoder, encodeFlags httpreq.EncodingFlags) error {
	e := utiljson.Latching(enc)
	e.Tokens(jsontext.BeginObject, jsontext.String("streams"))
	encodeStreams(data.Streams, e, encodeFlags)

	if len(data.DroppedEntries) > 0 {
		e.Tokens(jsontext.String("dropped_entries"))
		encodeDroppedEntries(data.DroppedEntries, e)
	}

	if len(encodeFlags) > 0 {
		e.Tokens(jsontext.String("encodingFlags"))
		encodeEncodingFlags(e, encodeFlags)
	}

	e.Tokens(jsontext.EndObject)
	return e.Err()
}

func encodeDroppedEntries(entries []legacy.DroppedEntry, e *utiljson.Encoder) {
	e.Tokens(jsontext.BeginArray)

	for i := range entries {
		if e.Err() != nil {
			return
		}
		ds, err := NewDroppedStream(&entries[i])
		if err != nil {
			e.Fail(err)
			return
		}
		e.MarshalJSONFrom(&ds)
	}

	e.Tokens(jsontext.EndArray)
}

func encodeEncodingFlags(e *utiljson.Encoder, flags httpreq.EncodingFlags) {
	e.Tokens(jsontext.BeginArray)
	for flag := range flags {
		e.Tokens(jsontext.String(string(flag)))
	}
	e.Tokens(jsontext.EndArray)
}

func encodeData(data parser.Value, statistics stats.Result, e *utiljson.Encoder, encodeFlags httpreq.EncodingFlags) {
	e.Tokens(jsontext.BeginObject)
	e.StringField("resultType", string(data.Type()))

	if len(encodeFlags) > 0 {
		e.Tokens(jsontext.String("encodingFlags"))
		encodeEncodingFlags(e, encodeFlags)
	}

	e.Tokens(jsontext.String("result"))
	encodeResult(data, e, encodeFlags)

	e.Tokens(jsontext.String("stats"))
	e.Marshal(statistics)

	e.Tokens(jsontext.EndObject)
}

func encodeResult(v parser.Value, e *utiljson.Encoder, encodeFlags httpreq.EncodingFlags) {
	switch v.Type() {
	case loghttp.ResultTypeStream:
		result, ok := v.(logqlmodel.Streams)
		if !ok {
			e.Fail(fmt.Errorf("unexpected type %T for streams", v))
			return
		}
		encodeStreams(result, e, encodeFlags)
	case loghttp.ResultTypeScalar:
		scalar, ok := v.(promql.Scalar)
		if !ok {
			e.Fail(fmt.Errorf("unexpected type %T for scalar", scalar))
			return
		}
		encodeScalar(scalar, e)
	case loghttp.ResultTypeVector:
		vector, ok := v.(promql.Vector)
		if !ok {
			e.Fail(fmt.Errorf("unexpected type %T for vector", vector))
			return
		}
		encodeVector(vector, e)
	case loghttp.ResultTypeMatrix:
		m, ok := v.(promql.Matrix)
		if !ok {
			e.Fail(fmt.Errorf("unexpected type %T for matrix", m))
			return
		}
		encodeMatrix(m, e)
	default:
		e.Tokens(jsontext.Null)
		e.Fail(fmt.Errorf("v1 endpoints do not support type %s", v.Type()))
	}
}

func encodeStreams(streams logqlmodel.Streams, e *utiljson.Encoder, encodeFlags httpreq.EncodingFlags) {
	e.Tokens(jsontext.BeginArray)
	for _, stream := range streams {
		if e.Err() != nil {
			return
		}
		encodeStream(stream, e, encodeFlags)
	}
	e.Tokens(jsontext.EndArray)
}

func encodeLabels(labels []logproto.LabelAdapter, e *utiljson.Encoder) {
	for _, label := range labels {
		e.StringField(label.Name, label.Value)
	}
}

// encodeStream encodes a logproto.Stream to JSON.
// If the FlagCategorizeLabels is set, the stream labels are grouped by their group name.
// Otherwise, the stream labels are written one after the other.
func encodeStream(stream logproto.Stream, e *utiljson.Encoder, encodeFlags httpreq.EncodingFlags) {
	categorizeLabels := encodeFlags.Has(httpreq.FlagCategorizeLabels)

	lbls, err := syntax.ParseMetric(stream.Labels)
	if err != nil {
		e.Fail(err)
		return
	}

	e.Tokens(jsontext.BeginObject, jsontext.String("stream"), jsontext.BeginObject)
	encodeLabels(logproto.FromLabelsToLabelAdapters(lbls), e)
	e.Tokens(jsontext.EndObject, jsontext.String("values"), jsontext.BeginArray)

	for _, entry := range stream.Entries {
		if e.Err() != nil {
			return
		}
		e.Tokens(
			jsontext.BeginArray,
			jsontext.String(strconv.FormatInt(entry.Timestamp.UnixNano(), 10)),
			jsontext.String(entry.Line),
		)

		if categorizeLabels {
			e.Tokens(jsontext.BeginObject)
			if len(entry.StructuredMetadata) > 0 {
				e.Tokens(jsontext.String("structuredMetadata"), jsontext.BeginObject)
				encodeLabels(entry.StructuredMetadata, e)
				e.Tokens(jsontext.EndObject)
			}
			if len(entry.Parsed) > 0 {
				e.Tokens(jsontext.String("parsed"), jsontext.BeginObject)
				encodeLabels(entry.Parsed, e)
				e.Tokens(jsontext.EndObject)
			}
			e.Tokens(jsontext.EndObject)
		}
		e.Tokens(jsontext.EndArray)
	}

	e.Tokens(jsontext.EndArray, jsontext.EndObject)
}

func encodeScalar(v promql.Scalar, e *utiljson.Encoder) {
	encodeValue(v.T, v.V, e)
}

func encodeVector(v promql.Vector, e *utiljson.Encoder) {
	e.Tokens(jsontext.BeginArray)
	for _, sample := range v {
		if e.Err() != nil {
			return
		}
		encodeSample(sample, e)
	}
	e.Tokens(jsontext.EndArray)
}

func encodeSample(sample promql.Sample, e *utiljson.Encoder) {
	e.Tokens(jsontext.BeginObject, jsontext.String("metric"))
	encodeMetric(sample.Metric, e)
	e.Tokens(jsontext.String("value"))
	encodeValue(sample.T, sample.F, e)
	e.Tokens(jsontext.EndObject)
}

// encodeValue writes a [timestamp, "value"] pair, where the timestamp is a
// bare number of seconds.
func encodeValue(T int64, V float64, e *utiljson.Encoder) {
	e.Tokens(jsontext.BeginArray)
	e.Value(jsontext.Value(model.Time(T).String()))
	e.Tokens(jsontext.String(model.SampleValue(V).String()), jsontext.EndArray)
}

func encodeMetric(l labels.Labels, e *utiljson.Encoder) {
	e.Tokens(jsontext.BeginObject)
	l.Range(func(label labels.Label) {
		e.StringField(label.Name, label.Value)
	})
	e.Tokens(jsontext.EndObject)
}

func encodeMatrix(m promql.Matrix, e *utiljson.Encoder) {
	e.Tokens(jsontext.BeginArray)
	for _, sampleStream := range m {
		if e.Err() != nil {
			return
		}
		encodeSampleStream(sampleStream, e)
	}
	e.Tokens(jsontext.EndArray)
}

func encodeSampleStream(stream promql.Series, e *utiljson.Encoder) {
	e.Tokens(jsontext.BeginObject, jsontext.String("metric"))
	encodeMetric(stream.Metric, e)
	e.Tokens(jsontext.String("values"), jsontext.BeginArray)
	for _, p := range stream.Floats {
		if e.Err() != nil {
			return
		}
		encodeValue(p.T, p.F, e)
	}
	e.Tokens(jsontext.EndArray, jsontext.EndObject)
}
