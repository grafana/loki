package sketch

import (
	"errors"
	"fmt"

	"github.com/DataDog/sketches-go/ddsketch"
	"github.com/influxdata/tdigest"

	"github.com/grafana/loki/pkg/logproto"
)

type QuantileSketch interface {
	Add(float64)
	Quantile(float64) (float64, error)
	Merge(QuantileSketch) (QuantileSketch, error)
	ToProto() *logproto.TDigest // TODO: support DDSketch as well.
}

type DDSketchQuantile struct {
	sketch *ddsketch.DDSketch
}

func NewDDSketch() QuantileSketch {
	//
	s, _ := ddsketch.NewDefaultDDSketch(0.01)
	return &DDSketchQuantile{sketch: s}
}

func (d *DDSketchQuantile) Add(count float64) {
	// TODO(karsten): check error and propagate to iterator.
	d.sketch.Add(count) //nolint:errcheck
}

func (d *DDSketchQuantile) Quantile(quantile float64) (float64, error) {
	if quantile >= 1.0 || quantile <= 0 {
		return 0.0, errors.New("invalid quantile value, must be between 0.0 and 1.0 ")
	}
	return d.sketch.GetValueAtQuantile(quantile)
}

func (d *DDSketchQuantile) Merge(other QuantileSketch) (QuantileSketch, error) {
	cast, ok := other.(*DDSketchQuantile)
	if !ok {
		return nil, fmt.Errorf("invalid sketch type: want %T, got %T", d, cast)
	}

	err := d.sketch.MergeWith(cast.sketch)
	return d, err
}

func (d *DDSketchQuantile) ToProto() *logproto.TDigest {
	return nil
}

type TDigestQuantile struct {
	sketch *tdigest.TDigest
}

func NewTDigestSketch() QuantileSketch {
	s := tdigest.New()

	return &TDigestQuantile{sketch: s}
}

func (d *TDigestQuantile) Add(count float64) {
	d.sketch.Add(count, 1)
}

func (d *TDigestQuantile) Quantile(quantile float64) (float64, error) {
	if quantile >= 1.0 || quantile <= 0 {
		return 0.0, errors.New("invalid quantile value, must be between 0.0 and 1.0 ")
	}
	return d.sketch.Quantile(quantile), nil
}

func (d *TDigestQuantile) Merge(other QuantileSketch) (QuantileSketch, error) {
	cast, ok := other.(*TDigestQuantile)
	if !ok {
		return nil, fmt.Errorf("invalid sketch type: want %T, got %T", d, cast)
	}

	d.sketch.Merge(cast.sketch)
	return d, nil
}

func (d *TDigestQuantile) ToProto() *logproto.TDigest {
	centroids := make(tdigest.CentroidList, 0)
	centroids = d.sketch.Centroids(centroids)
	processed := make([]*logproto.TDigest_Centroid, len(centroids))
	for i, c := range centroids {
		processed[i] = &logproto.TDigest_Centroid{
			Mean:   c.Mean,
			Weight: c.Weight,
		}
	}
	return &logproto.TDigest{
		Compression: d.sketch.Compression,
		Processed:   processed,
	}
}
