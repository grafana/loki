package tsdb

import (
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index"
	"github.com/prometheus/common/model"
)

// TODO(chaudum): Replace with new v1.Interval struct
type Bounded interface {
	Bounds() (model.Time, model.Time)
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
