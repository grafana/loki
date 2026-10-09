package metastore

import (
	"testing"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/index/indexobj"
	"github.com/grafana/loki/v3/pkg/dataobj/logsobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/postings"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
)

// testIndexBuilder builds an index object for tenantID in the layout that the
// index Calculator writes: a streams section and postings sections.
type testIndexBuilder struct {
	tb      testing.TB
	builder *indexobj.Builder
}

func newTestIndexBuilder(tb testing.TB) *testIndexBuilder {
	tb.Helper()

	builder, err := indexobj.NewBuilder(tenantID, logsobj.BuilderBaseConfig{
		TargetPageSize:          1024 * 1024,
		TargetObjectSize:        10 * 1024 * 1024,
		TargetSectionSize:       1024 * 1024,
		BufferSize:              1024 * 1024,
		SectionStripeMergeLimit: 2,
	}, nil, indexobj.NewBuilderMetrics(nil))
	require.NoError(tb, err)
	return &testIndexBuilder{tb: tb, builder: builder}
}

// observeLine records one log line of a stream in a section of the logs object
// at path. streamID is the ID of the stream in that logs object. It records the
// stream in the streams section and a label posting for each label of lbls.
func (b *testIndexBuilder) observeLine(path string, section, streamID int64, lbls labels.Labels, ts time.Time, size int64) {
	b.tb.Helper()

	_, err := b.builder.AppendStream(streams.Stream{
		ID:               streamID,
		Labels:           lbls,
		MinTimestamp:     ts,
		MaxTimestamp:     ts,
		UncompressedSize: size,
	})
	require.NoError(b.tb, err)

	shardBucket := streams.ShardBucket(lbls)
	lbls.Range(func(l labels.Label) {
		b.builder.ObserveLabelPosting(postings.LabelObservation{
			ObjectPath:       path,
			ShardBuckets:     int64(streams.ShardFactor),
			SectionIndex:     section,
			ColumnName:       l.Name,
			LabelValue:       l.Value,
			StreamID:         streamID,
			Timestamp:        ts,
			UncompressedSize: size,
			ShardBucket:      shardBucket,
		})
	})
}

// observeMetadata records the values of a structured metadata column of a
// stream in a bloom posting.
func (b *testIndexBuilder) observeMetadata(path string, section, streamID int64, ts time.Time, column string, values ...string) {
	b.tb.Helper()

	b.builder.PrepareBloomColumn(path, section, column, uint(len(values)), int64(streams.ShardFactor))
	for _, value := range values {
		require.NoError(b.tb, b.builder.ObserveBloomPosting(postings.BloomObservation{
			ObjectPath:   path,
			ShardBuckets: int64(streams.ShardFactor),
			SectionIndex: section,
			ColumnName:   column,
			Value:        value,
			StreamID:     streamID,
			Timestamp:    ts,
		}))
	}
}

// timeRange returns the tenant and the time range of the recorded lines. Call
// it before flush, because flush resets the builder.
func (b *testIndexBuilder) timeRange() dataobj.TimeRange {
	return b.builder.TimeRange()
}

// flush builds the index object. The test cleanup releases it.
func (b *testIndexBuilder) flush() *dataobj.Object {
	b.tb.Helper()

	obj, closer, err := b.builder.Flush()
	require.NoError(b.tb, err)
	b.tb.Cleanup(func() { _ = closer.Close() })
	return obj
}
