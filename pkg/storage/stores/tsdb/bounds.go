package tsdb

import (
	"math"

	"github.com/prometheus/common/model"
)

type Bounded interface {
	Bounds() (model.Time, model.Time)
}

// InclusiveBounds will ensure the underlying Bounded implementation
// is turned into [lower,upper] inclusivity.
// Generally, we consider bounds to be `[lower,upper)` inclusive
// This helper will account for integer overflow.
// Because model.Time is millisecond-precise, but Loki uses nanosecond precision,
// be careful usage can handle an extra millisecond being added.
func inclusiveBounds(b Bounded) (model.Time, model.Time) {
	lower, upper := b.Bounds()

	if int64(upper) < math.MaxInt64 {
		upper++
	}

	return lower, upper
}

type bounds struct {
	mint, maxt model.Time
}

func newBounds(mint, maxt model.Time) bounds { return bounds{mint: mint, maxt: maxt} }

func (b bounds) Bounds() (model.Time, model.Time) { return b.mint, b.maxt }

func Overlap(a, b Bounded) bool {
	aFrom, aThrough := a.Bounds()
	bFrom, bThrough := b.Bounds()

	return aFrom < bThrough && aThrough > bFrom
}
