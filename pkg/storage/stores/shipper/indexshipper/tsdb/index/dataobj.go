package index

import (
	"context"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
)

// DataobjSectionRef is a fully resolved reference to a section within a data
// object, including time bounds and sizing information.
type DataobjSectionRef struct {
	Path      string
	SectionID int
	MinTime   model.Time
	MaxTime   model.Time
	KB        uint32
	Entries   uint32
	StreamIDs []int64
}

// DataobjResolver is an optional interface implemented by Index types that
// support resolving ChunkMeta references into dataobj section references via a
// companion lookup table.
type DataobjResolver interface {
	GetDataobjSections(ctx context.Context, userID string, from, through model.Time,
		fpFilter FingerprintFilter, matchers ...*labels.Matcher) ([]DataobjSectionRef, error)
}
