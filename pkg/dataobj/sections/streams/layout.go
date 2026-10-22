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
// NewLayout copies schemaLabels.
func NewLayout(shardLayout ShardLayout, schemaLabels []string) Layout {
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
	return &datasetmd.SortInfo{
		SchemaLabels: slices.Clone(l.schemaLabels),
		StreamOrder:  streamOrderProto(l.streamOrder),
		ShardCount:   l.shardLayout.Buckets(),
	}
}

// layoutFromSortInfo decodes the layout that si records. It returns an error
// for any state that [NewLayout] cannot produce: a shard count that is not a
// valid shard layout, a stream order other than [StreamOrderStableHashV1], or
// column sorts.
func layoutFromSortInfo(si *datasetmd.SortInfo) (Layout, error) {
	if si.StreamOrder != datasetmd.STREAM_ORDER_STABLE_HASH_V1 {
		return Layout{}, fmt.Errorf("unsupported stream order %s", si.StreamOrder)
	}
	if len(si.ColumnSorts) > 0 {
		return Layout{}, fmt.Errorf("streams section has %d column sorts, want none", len(si.ColumnSorts))
	}
	shardLayout, err := ShardLayoutFromBuckets(si.ShardCount)
	if err != nil {
		return Layout{}, fmt.Errorf("decoding shard layout: %w", err)
	}
	return Layout{
		shardLayout:  shardLayout,
		schemaLabels: slices.Clone(si.SchemaLabels),
		streamOrder:  StreamOrderStableHashV1,
	}, nil
}

func streamOrderProto(order StreamOrder) datasetmd.StreamOrder {
	switch order {
	case StreamOrderStableHashV1:
		return datasetmd.STREAM_ORDER_STABLE_HASH_V1
	default:
		return datasetmd.STREAM_ORDER_UNSPECIFIED
	}
}
