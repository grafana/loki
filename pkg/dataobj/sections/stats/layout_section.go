package stats

import (
	"errors"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/internal/columnar"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
)

// LayoutSectionBuilder builds a stats section with no rows that records a
// stream layout. Each index object of an index run records the layout of the
// run in a stats section, and an index object of a run can hold no stats rows.
//
// The section has no columns, so a reader that predates it fails to read its
// rows. [RowReader] reads it as a section with no rows.
type LayoutSectionBuilder struct {
	tenant string
	layout streams.Layout
}

var _ dataobj.SectionBuilder = (*LayoutSectionBuilder)(nil)

// NewLayoutSectionBuilder returns a builder of a stats section of tenant with
// no rows that records layout.
func NewLayoutSectionBuilder(tenant string, layout streams.Layout) *LayoutSectionBuilder {
	return &LayoutSectionBuilder{tenant: tenant, layout: layout}
}

// Type returns the type of a stats section.
func (b *LayoutSectionBuilder) Type() dataobj.SectionType { return sectionType }

// Flush writes the section to w. A LayoutSectionBuilder can flush any number
// of sections.
//
// Flush returns an error for the zero layout. The zero layout has one shard
// bucket and no stream order, so readers fail to decode a section that
// records it. An index object without a layout has no stats section instead.
func (b *LayoutSectionBuilder) Flush(w dataobj.SectionWriter) (int64, error) {
	if b.layout.StreamOrder() == streams.StreamOrderUnspecified {
		return 0, errors.New("layout section of the zero layout")
	}
	enc := columnar.NewEncoder()
	defer enc.Reset()
	enc.SetSortInfo(streams.NewSortInfo(b.layout, nil))
	enc.SetTenant(b.tenant)
	return enc.Flush(w)
}

// Reset does nothing, because a LayoutSectionBuilder holds no rows.
func (b *LayoutSectionBuilder) Reset() {}
