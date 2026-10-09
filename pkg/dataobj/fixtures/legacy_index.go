package fixtures

import (
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/pointers"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
	"github.com/grafana/loki/v3/pkg/scratch"
)

// LegacyIndexBuilder builds an index object with one streams section and one
// pointers section, the layout of index objects without postings sections.
//
// The index Calculator does not write pointers sections. Use
// LegacyIndexBuilder to test the readers that still read index objects in this
// layout.
type LegacyIndexBuilder struct {
	tenant   string
	streams  *streams.Builder
	pointers *pointers.Builder
}

// NewLegacyIndexBuilder returns an empty [LegacyIndexBuilder] for tenant.
func NewLegacyIndexBuilder(tenant string) *LegacyIndexBuilder {
	sb := streams.NewBuilder(streams.NewMetrics(), defaultStreamSectionConfig.pageSize, defaultStreamSectionConfig.pageRowCount)
	sb.SetTenant(tenant)
	pb := pointers.NewBuilder(pointers.NewMetrics(), defaultStreamSectionConfig.pageSize, defaultStreamSectionConfig.pageRowCount)
	pb.SetTenant(tenant)

	return &LegacyIndexBuilder{tenant: tenant, streams: sb, pointers: pb}
}

// AppendStream records stream in the streams section and returns the ID of
// the stream in the index object. It records the minimum and the maximum
// timestamp of stream, and counts its uncompressed size once.
func (b *LegacyIndexBuilder) AppendStream(stream streams.Stream) int64 {
	id := b.streams.Record(stream.Labels, stream.MinTimestamp, stream.UncompressedSize)
	_ = b.streams.Record(stream.Labels, stream.MaxTimestamp, 0)
	return id
}

// ObserveLogLine records one log line of a stream in the pointers section.
// streamIDInObject is the ID of the stream in the logs object at path, and
// streamIDInIndex is the ID that AppendStream returned for it.
func (b *LegacyIndexBuilder) ObserveLogLine(path string, section, streamIDInObject, streamIDInIndex int64, ts time.Time, uncompressedSize int64) {
	b.pointers.ObserveStream(path, section, streamIDInObject, streamIDInIndex, ts, uncompressedSize)
}

// AppendColumnIndex records a column index pointer with the bloom filter of
// the column values in the pointers section.
func (b *LegacyIndexBuilder) AppendColumnIndex(path string, section int64, columnName string, columnIndex int64, valuesBloom []byte) {
	b.pointers.RecordColumnIndex(path, section, columnName, columnIndex, valuesBloom)
}

// TimeRange returns the tenant and the time range of the streams that
// AppendStream recorded.
func (b *LegacyIndexBuilder) TimeRange() dataobj.TimeRange {
	minTime, maxTime := b.streams.TimeRange()
	return dataobj.TimeRange{Tenant: b.tenant, MinTime: minTime, MaxTime: maxTime}
}

// Flush builds the index object. It leaves out a section that holds no rows.
// The caller must close the returned [io.Closer] after it stops reading the
// object.
func (b *LegacyIndexBuilder) Flush(tb testing.TB) (*dataobj.Object, io.Closer) {
	tb.Helper()

	objBuilder := dataobj.NewBuilder(scratch.NewMemory())
	if b.streams.EstimatedSize() > 0 {
		require.NoError(tb, objBuilder.Append(b.streams))
	}
	if b.pointers.EstimatedSize() > 0 {
		require.NoError(tb, objBuilder.Append(b.pointers))
	}

	obj, closer, err := objBuilder.Flush()
	require.NoError(tb, err)
	return obj, closer
}
