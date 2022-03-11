package tsdb

import "github.com/prometheus/common/model"

type Bounded interface {
	Bounds() (model.Time, model.Time)
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
