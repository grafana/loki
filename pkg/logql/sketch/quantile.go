package sketch

import (
	"errors"
	"fmt"

	"github.com/DataDog/sketches-go/ddsketch"
	"github.com/influxdata/tdigest"
	"github.com/prometheus/prometheus/model/labels"
	promql_parser "github.com/prometheus/prometheus/promql/parser"

	"github.com/grafana/loki/pkg/logproto"
)

// QuantileSketchVector represents multiple qunatile sketches at the same point in
// time.
type QuantileSketchVector []quantileSketchSample

// QuantileSketchMatrix contains multiples QuantileSketchVectors across many
// points in time.
type QuantileSketchMatrix []QuantileSketchVector

// ToProto converts a quantile sketch vector to its protobuf definition.
func (q QuantileSketchVector) ToProto() *logproto.QuantileSketchVector {
	samples := make([]*logproto.QuantileSketchSample, len(q))
	for i, sample := range q {
		samples[i] = sample.ToProto()
	}
	return &logproto.QuantileSketchVector{Samples: samples}
}

func QuantileSketchVectorFromProto(proto *logproto.QuantileSketchVector) (QuantileSketchVector, error) {
	out := make([]quantileSketchSample, len(proto.Samples))
	var err error
	for i, s := range proto.Samples {
		out[i], err = quantileSketchSampleFromProto(s)
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (QuantileSketchMatrix) String() string {
	return "QuantileSketchMatrix()"
}

func (QuantileSketchMatrix) Type() promql_parser.ValueType { return "QuantileSketchMatrix" }

func (m QuantileSketchMatrix) ToProto() *logproto.QuantileSketchMatrix {
	values := make([]*logproto.QuantileSketchVector, len(m))
	for i, vec := range m {
		values[i] = vec.ToProto()
	}
	return &logproto.QuantileSketchMatrix{Values: values}
}

func QuantileSketchMatrixFromProto(proto *logproto.QuantileSketchMatrix) (QuantileSketchMatrix, error) {
	out := make([]QuantileSketchVector, len(proto.Values))
	var err error
	for i, v := range proto.Values {
		out[i], err = QuantileSketchVectorFromProto(v)
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

type quantileSketchSample struct {
	T int64
	F QuantileSketch

	Metric labels.Labels
}

func (q quantileSketchSample) ToProto() *logproto.QuantileSketchSample {
	metric := make([]*logproto.LabelPair, len(q.Metric))
	for i, m := range q.Metric {
		metric[i] = &logproto.LabelPair{Name: m.Name, Value: m.Value}
	}

	sketch := q.F.ToProto()

	return &logproto.QuantileSketchSample{
		F:           sketch,
		TimestampMs: q.T,
		Metric:      metric,
	}
}

func quantileSketchSampleFromProto(proto *logproto.QuantileSketchSample) (quantileSketchSample, error) {
	sketch, err := QuantileSketchFromProto(proto.F)
	if err != nil {
		return quantileSketchSample{}, err
	}
	out := quantileSketchSample{
		T:      proto.TimestampMs,
		F:      sketch,
		Metric: make(labels.Labels, len(proto.Metric)),
	}

	for i, p := range proto.Metric {
		out.Metric[i] = labels.Label{Name: p.Name, Value: p.Value}
	}

	return out, nil
}

// QuantileSketch estimates quantiles over time.
type QuantileSketch interface {
	Add(float64) error
	Quantile(float64) (float64, error)
	Merge(QuantileSketch) (QuantileSketch, error)
	ToProto() *logproto.QuantileSketch
}

type QuantileSketchFactory func() QuantileSketch

func QuantileSketchFromProto(proto *logproto.QuantileSketch) (QuantileSketch, error) {
	switch concrete := proto.Sketch.(type) {
	case *logproto.QuantileSketch_Tdigest:
		return TDigestQuantileFromProto(concrete.Tdigest), nil
	case *logproto.QuantileSketch_Ddsketch:
		return DDSketchQuantileFromProto(concrete.Ddsketch)
	}

	return nil, fmt.Errorf("unknown quantile sketch type: %T", proto.Sketch)
}

// DDSketchQuantile is a QuantileSketch implementation based on DataDog's
// "DDSketch: A fast and fully-mergeable quantile sketch with relative-error
// guarantees." paper.
type DDSketchQuantile struct {
	*ddsketch.DDSketch
}

func NewDDSketch() *DDSketchQuantile {
	s, _ := ddsketch.NewDefaultDDSketch(0.01)
	return &DDSketchQuantile{s}
}

func (d *DDSketchQuantile) Quantile(quantile float64) (float64, error) {
	if quantile >= 1.0 || quantile <= 0 {
		return 0.0, errors.New("invalid quantile value, must be between 0.0 and 1.0 ")
	}
	return d.GetValueAtQuantile(quantile)
}

func (d *DDSketchQuantile) Merge(other QuantileSketch) (QuantileSketch, error) {
	cast, ok := other.(*DDSketchQuantile)
	if !ok {
		return nil, fmt.Errorf("invalid sketch type: want %T, got %T", d, cast)
	}

	err := d.MergeWith(cast.DDSketch)
	return d, err
}

func (d *DDSketchQuantile) ToProto() *logproto.QuantileSketch {
	sketch := &logproto.QuantileSketch_Ddsketch{}
	d.DDSketch.Encode(&sketch.Ddsketch, false)
	return &logproto.QuantileSketch{
		Sketch: sketch,
	}
}

func DDSketchQuantileFromProto(buf []byte) (*DDSketchQuantile, error) {
	sketch := NewDDSketch()
	err := sketch.DDSketch.DecodeAndMergeWith(buf)
	return sketch, err
}

type TDigestQuantile struct {
	*tdigest.TDigest
}

func NewTDigestSketch() QuantileSketch {
	s := tdigest.New()

	return &TDigestQuantile{s}
}

func (d *TDigestQuantile) Add(count float64) error {
	d.TDigest.Add(count, 1)
	return nil
}

func (d *TDigestQuantile) Quantile(quantile float64) (float64, error) {
	if quantile >= 1.0 || quantile <= 0 {
		return 0.0, errors.New("invalid quantile value, must be between 0.0 and 1.0 ")
	}
	return d.TDigest.Quantile(quantile), nil
}

func (d *TDigestQuantile) Merge(other QuantileSketch) (QuantileSketch, error) {
	cast, ok := other.(*TDigestQuantile)
	if !ok {
		return nil, fmt.Errorf("invalid sketch type: want %T, got %T", d, cast)
	}

	d.TDigest.Merge(cast.TDigest)
	return d, nil
}

func (d *TDigestQuantile) ToProto() *logproto.QuantileSketch {
	centroids := make(tdigest.CentroidList, 0)
	centroids = d.Centroids(centroids)
	processed := make([]*logproto.TDigest_Centroid, len(centroids))
	for i, c := range centroids {
		processed[i] = &logproto.TDigest_Centroid{
			Mean:   c.Mean,
			Weight: c.Weight,
		}
	}

	return &logproto.QuantileSketch{
		Sketch: &logproto.QuantileSketch_Tdigest{
			Tdigest: &logproto.TDigest{
				Compression: d.Compression,
				Processed:   processed,
			},
		},
	}
}

func TDigestQuantileFromProto(proto *logproto.TDigest) *TDigestQuantile {
	q := &TDigestQuantile{tdigest.NewWithCompression(proto.Compression)}

	centroids := make([]tdigest.Centroid, len(proto.Processed))
	for i, c := range proto.Processed {
		centroids[i] = tdigest.Centroid{Mean: c.Mean, Weight: c.Weight}
	}
	q.AddCentroidList(centroids)
	return q
}
