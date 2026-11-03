package sketch

import (
	"errors"
	"fmt"
	"math"
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

// Quantile returns the value at the given quantile.
//
// It linearly interpolates between the two order statistics bracketing the
// quantile's rank, the same way the exact (unsharded) quantile_over_time
// aggregator does. It also matches that aggregator's contract at the edges:
// an empty sketch returns NaN, and a quantile outside [0, 1] returns -Inf or
// +Inf, rather than an error.
func (d *DDSketchQuantile) Quantile(quantile float64) (float64, error) {
	count := d.GetCount()
	if count == 0 {
		return math.NaN(), nil
	}
	if quantile < 0 {
		return math.Inf(-1), nil
	}
	if quantile > 1 {
		return math.Inf(1), nil
	}
	if count == 1 {
		// Rank is always 0: nothing to interpolate.
		return d.GetValueAtQuantile(quantile)
	}

	// quantile * (count - 1) can be computed with more precision than a float64 actually
	// stores. Go lets the compiler carry that extra precision into the calculations below
	// instead of discarding it right away, and whether it does depends on the CPU and
	// compiler. The float64(...) conversion discards the extra precision immediately, so
	// rank is a plain float64 from here on, the same on every machine. GetValueAtQuantile
	// does this same conversion, for the same reason, on its own rank.
	rank := float64(quantile * (count - 1))
	lower := math.Floor(rank)
	upper := math.Min(count-1, lower+1)

	lowerValue := d.valueAtRank(lower)
	if lower == upper {
		return lowerValue, nil
	}

	weight := rank - lower
	upperValue := d.valueAtRank(upper)
	return lowerValue*(1-weight) + upperValue*weight, nil
}

// valueAtRank returns the sketch's approximate value of the element at the
// given 0-indexed rank among all added values in ascending order. Rank must
// be in [0, count-1].
//
// This function mirrors the logic that [ddsketch.DDSketch.GetValueAtQuantile]
// uses internally.
func (d *DDSketchQuantile) valueAtRank(rank float64) float64 {
	var (
		negativeValueCount = d.GetNegativeValueStore().TotalCount()
		zeroCount          = d.GetZeroCount()
	)

	switch {
	case rank < negativeValueCount:
		return -d.Value(d.GetNegativeValueStore().KeyAtRank(negativeValueCount - 1 - rank))
	case rank < zeroCount+negativeValueCount:
		return 0
	default:
		return d.Value(d.GetPositiveValueStore().KeyAtRank(rank - zeroCount - negativeValueCount))
	}
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
	d.Encode(&sketch.Ddsketch, false)
	return &logproto.QuantileSketch{
		Sketch: sketch,
	}
}

func (d *DDSketchQuantile) Release() {
	d.Clear()
	ddsketchPool.Put(d.DDSketch)
}

func DDSketchQuantileFromProto(buf []byte) (*DDSketchQuantile, error) {
	sketch := NewDDSketch()
	err := sketch.DecodeAndMergeWith(buf)
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
