package sketch

import (
	"errors"
	"github.com/influxdata/tdigest"

	"github.com/DataDog/sketches-go/ddsketch"
)

type QuantileSketch interface {
	Add(float64)
	Quantile(float64) (float64, error)
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
	d.sketch.Add(count)
}

func (d *DDSketchQuantile) Quantile(quantile float64) (float64, error) {
	if quantile >= 1.0 || quantile <= 0 {
		return 0.0, errors.New("invalid quantile value, must be between 0.0 and 1.0 ")
	}
	return d.sketch.GetValueAtQuantile(quantile)
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
