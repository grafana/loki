package streams

import (
	"fmt"
	"slices"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
)

// StreamOrder identifies the stream key that orders the stream IDs of a
// section.
type StreamOrder int

const (
	// StreamOrderUnspecified means that stream IDs follow no known key.
	StreamOrderUnspecified StreamOrder = 0

	// StreamOrderStableHashV1 orders stream IDs by shard bucket, then schema
	// key, then labels.StableHash, then labels.
	StreamOrderStableHashV1 StreamOrder = 2
)

// Layout describes how the streams of one tenant in a data object are laid
// out: the shard layout of the shard bucket column, the schema labels of the
// stream key, and the stream key version that orders stream IDs.
//
// The streams section records the layout and defines the stream IDs. The logs
// sections of the tenant use those IDs and sort rows by them, so all of them
// share one order. Stream IDs of two objects are comparable only if their
// layouts are equal.
//
// The zero value means "no layout": it has one shard bucket, no schema labels,
// and an unspecified stream order. [NewLayout] never returns it.
type Layout struct {
	shardLayout  ShardLayout
	schemaLabels []string
	streamOrder  StreamOrder
}

// NewLayout returns the layout with shardLayout and schemaLabels. Stream IDs
// follow [StreamOrderStableHashV1]. An empty schemaLabels orders streams by
// shard bucket, then labels.StableHash.
//
// NewLayout copies schemaLabels. It stores empty schemaLabels as nil, so
// layouts that [Layout.Equal] reports as equal are also deeply equal.
func NewLayout(shardLayout ShardLayout, schemaLabels []string) Layout {
	if len(schemaLabels) == 0 {
		schemaLabels = nil
	}
	return Layout{
		shardLayout:  shardLayout,
		schemaLabels: slices.Clone(schemaLabels),
		streamOrder:  StreamOrderStableHashV1,
	}
}

// ShardLayout returns the shard layout of the shard bucket column.
func (l Layout) ShardLayout() ShardLayout { return l.shardLayout }

// SchemaLabels returns the fully-qualified schema labels of the stream key, in
// order. The caller must not modify the returned slice.
//
// SchemaLabels does not copy, because writers call it once per appended
// stream.
func (l Layout) SchemaLabels() []string { return l.schemaLabels }

// StreamOrder returns the stream key version that orders stream IDs.
func (l Layout) StreamOrder() StreamOrder { return l.streamOrder }

// Equal reports whether l and other describe the same layout.
func (l Layout) Equal(other Layout) bool {
	return l.shardLayout == other.shardLayout &&
		l.streamOrder == other.streamOrder &&
		slices.Equal(l.schemaLabels, other.schemaLabels)
}

func (l Layout) sortInfo() *datasetmd.SortInfo {
	si := &datasetmd.SortInfo{}
	l.WriteSortInfo(si)
	return si
}

// WriteSortInfo sets the layout fields of si to l: the schema labels, the
// stream order, and the shard count. It keeps the column sorts of si, so
// sections that sort their own rows can record a layout too.
func (l Layout) WriteSortInfo(si *datasetmd.SortInfo) {
	si.SchemaLabels = slices.Clone(l.schemaLabels)
	si.StreamOrder = streamOrderProto(l.streamOrder)
	si.ShardCount = l.shardLayout.Buckets()
}

// LayoutFromSortInfo decodes the layout that [Layout.WriteSortInfo] wrote to
// si, and ignores the column sorts of si. ok is false if si is nil or sets
// none of the stream order, the shard count, and the schema labels.
//
// LayoutFromSortInfo returns an error for any other state that [NewLayout]
// cannot produce: a shard count that is not a valid shard layout, or a stream
// order other than [StreamOrderStableHashV1].
func LayoutFromSortInfo(si *datasetmd.SortInfo) (layout Layout, ok bool, err error) {
	if si == nil || (si.StreamOrder == datasetmd.STREAM_ORDER_UNSPECIFIED && si.ShardCount == 0 && len(si.SchemaLabels) == 0) {
		return Layout{}, false, nil
	}
	if si.StreamOrder != datasetmd.STREAM_ORDER_STABLE_HASH_V1 {
		return Layout{}, false, fmt.Errorf("unsupported stream order %s", si.StreamOrder)
	}
	shardLayout, err := ShardLayoutFromBuckets(si.ShardCount)
	if err != nil {
		return Layout{}, false, fmt.Errorf("decoding shard layout: %w", err)
	}
	return NewLayout(shardLayout, si.SchemaLabels), true, nil
}

func streamOrderProto(order StreamOrder) datasetmd.StreamOrder {
	switch order {
	case StreamOrderStableHashV1:
		return datasetmd.STREAM_ORDER_STABLE_HASH_V1
	default:
		return datasetmd.STREAM_ORDER_UNSPECIFIED
	}
}
