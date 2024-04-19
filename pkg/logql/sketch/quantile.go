package sketch

import (
	"errors"
	"fmt"
	"sync"

	"github.com/DataDog/sketches-go/ddsketch"
	"github.com/DataDog/sketches-go/ddsketch/mapping"
	"github.com/DataDog/sketches-go/ddsketch/store"
	"github.com/influxdata/tdigest"

	"github.com/grafana/loki/v3/pkg/logproto"
)

// QuantileSketch estimates quantiles over time.
type QuantileSketch interface {
	Add(float64) error
	Quantile(float64) (float64, error)
	Merge(QuantileSketch) (QuantileSketch, error)
	ToProto() *logproto.QuantileSketch
	Release()
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

const relativeAccuracy = 0.01

var ddsketchPool = sync.Pool{
	New: func() any {
		m, _ := mapping.NewCubicallyInterpolatedMapping(relativeAccuracy)
		return ddsketch.NewDDSketch(m, store.NewCollapsingLowestDenseStore(2048), store.NewCollapsingLowestDenseStore(2048))
	},
}

func NewDDSketch() *DDSketchQuantile {
	s := ddsketchPool.Get().(*ddsketch.DDSketch)
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

func (d *DDSketchQuantile) Release() {
	d.DDSketch.Clear()
	ddsketchPool.Put(d.DDSketch)
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

func (d *TDigestQuantile) Release() {}

func TDigestQuantileFromProto(proto *logproto.TDigest) *TDigestQuantile {
	q := &TDigestQuantile{tdigest.NewWithCompression(proto.Compression)}

	centroids := make([]tdigest.Centroid, len(proto.Processed))
	for i, c := range proto.Processed {
		centroids[i] = tdigest.Centroid{Mean: c.Mean, Weight: c.Weight}
	}
	q.AddCentroidList(centroids)
	return q
}
