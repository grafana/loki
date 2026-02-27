package tsdb

import (
	"math"

	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index"
	"github.com/prometheus/common/model"
)

// TODO(chaudum): Replace with new v1.Interval struct
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

// overlapIndex checks whether the given index bounds
// overlap with the bounds of a query range.
// chunk/index bounds are defined as [from, through]
// query bounds are defined as [from, through)
func overlapIndex(index Index, qry Bounded) bool {
	idxFrom, idxThrough := index.Bounds()
	qryFrom, qryThrough := qry.Bounds()

	return idxFrom < qryThrough && idxThrough >= qryFrom
}

// Same as overlapIndex but for ChunkMeta. The code is repeated for performance:
// attempting to share the implementation via the Bounded interface results in a lot of
// small memory allocations because ChunkMeta is passed by value and interfaces require a pointer.
func overlapChunk(chk index.ChunkMeta, qry bounds) bool {
	chkFrom, chkThrough := chk.Bounds()
	qryFrom, qryThrough := qry.Bounds()

	return chkFrom < qryThrough && chkThrough >= qryFrom
}
