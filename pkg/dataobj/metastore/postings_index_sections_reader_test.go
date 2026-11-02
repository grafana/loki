package metastore

import (
	"context"
	"io"
	"math"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/user"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/index/indexobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/postings"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
)

func TestPostingsIndexSectionsReader_ResolvesAndEmitsResultBatch(t *testing.T) {
	ctx := user.InjectOrgID(context.Background(), tenantID)

	ts := now.Add(-2 * time.Hour)
	obj, closer := buildIndexObject(t, tenantID, []postings.LabelObservation{
		{ObjectPath: "src-obj", SectionIndex: 3, ColumnName: "app", LabelValue: "nginx", StreamID: 7, Timestamp: ts, UncompressedSize: 0},
		{ObjectPath: "src-obj", SectionIndex: 3, ColumnName: "app", LabelValue: "loki", StreamID: 9, Timestamp: ts, UncompressedSize: 0},
	})
	defer closer()

	matchers := []*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "app", "nginx")}
	r := newPostingsIndexSectionsReader(log.NewNopLogger(), obj, now.Add(-4*time.Hour), now, matchers, nil, 8192, nil)
	t.Cleanup(r.Close)
	require.NoError(t, r.Open(ctx))

	rec, err := r.Read(ctx)
	require.NoError(t, err)
	require.NotNil(t, rec)
	require.Equal(t, int64(1), rec.NumRows())

	// The emitted batch must decode through the same path CollectSections
	// uses, preserving section identity, stream ID, and the timestamp envelope.
	rows, err := resolvedRowsFromRecordBatch(rec)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, "src-obj", rows[0].objectPath)
	require.Equal(t, int64(3), rows[0].sectionIndex)
	require.Equal(t, int64(7), rows[0].streamID)
	require.Equal(t, ts.UnixNano(), rows[0].minTimestamp)
	require.Equal(t, ts.UnixNano(), rows[0].maxTimestamp)

	// Drained.
	_, err = r.Read(ctx)
	require.ErrorIs(t, err, io.EOF)
}

func TestPostingsIndexSectionsReader_MissingOrgIDReturnsError(t *testing.T) {
	obj, closer := buildIndexObject(t, tenantID, []postings.LabelObservation{
		{ObjectPath: "src-obj", SectionIndex: 0, ColumnName: "app", LabelValue: "nginx", StreamID: 1, Timestamp: now, UncompressedSize: 0},
	})
	defer closer()

	matchers := []*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "app", "nginx")}
	r := newPostingsIndexSectionsReader(log.NewNopLogger(), obj, now.Add(-time.Hour), now, matchers, nil, 8192, nil)
	t.Cleanup(r.Close)

	// No org ID injected into the context.
	err := r.Open(context.Background())
	require.Error(t, err)
}

func TestPostingsIndexSectionsReader_PaginatesByBatchSize(t *testing.T) {
	ctx := user.InjectOrgID(context.Background(), tenantID)

	ts := now.Add(-2 * time.Hour)
	obj, closer := buildIndexObject(t, tenantID, []postings.LabelObservation{
		{ObjectPath: "src-obj", SectionIndex: 0, ColumnName: "app", LabelValue: "nginx", StreamID: 1, Timestamp: ts, UncompressedSize: 0},
		{ObjectPath: "src-obj", SectionIndex: 0, ColumnName: "app", LabelValue: "nginx", StreamID: 2, Timestamp: ts, UncompressedSize: 0},
		{ObjectPath: "src-obj", SectionIndex: 0, ColumnName: "app", LabelValue: "nginx", StreamID: 3, Timestamp: ts, UncompressedSize: 0},
	})
	defer closer()

	matchers := []*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "app", "nginx")}
	r := newPostingsIndexSectionsReader(log.NewNopLogger(), obj, now.Add(-4*time.Hour), now, matchers, nil, 2, nil)
	t.Cleanup(r.Close)
	require.NoError(t, r.Open(ctx))

	var total int64
	var batches int
	for {
		rec, err := r.Read(ctx)
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		require.LessOrEqual(t, rec.NumRows(), int64(2), "no batch exceeds batchSize")
		total += rec.NumRows()
		batches++
	}
	require.Equal(t, int64(3), total, "every matching stream is delivered")
	require.Equal(t, 2, batches, "3 rows at batchSize 2 paginates into 2 batches")
}

func TestPostingsIndexSectionsReader_ZeroMatchersEOF(t *testing.T) {
	ctx := user.InjectOrgID(context.Background(), tenantID)
	obj, closer := buildIndexObject(t, tenantID, []postings.LabelObservation{
		{ObjectPath: "src-obj", SectionIndex: 0, ColumnName: "app", LabelValue: "nginx", StreamID: 1, Timestamp: now, UncompressedSize: 0},
	})
	defer closer()

	r := newPostingsIndexSectionsReader(log.NewNopLogger(), obj, now.Add(-time.Hour), now, nil, nil, 8192, nil)
	t.Cleanup(r.Close)
	require.NoError(t, r.Open(ctx))

	rec, err := r.Read(ctx)
	require.ErrorIs(t, err, io.EOF)
	require.Nil(t, rec)
}

func TestPostingsIndexSectionsReader_ReadBeforeOpenErrors(t *testing.T) {
	r := newPostingsIndexSectionsReader(log.NewNopLogger(), nil, now, now, nil, nil, 8192, nil)
	rec, err := r.Read(context.Background())
	require.ErrorIs(t, err, errIndexSectionsReaderNotOpen)
	require.Nil(t, rec)
}

func TestPostingsReader_ExpandsBitmapToStreamRows(t *testing.T) {
	results := []SectionStreams{{
		Section:        postings.SectionRef{ObjectPath: "obj-a", SectionIndex: 1},
		StreamBitmap:   []byte{0b0000_0110}, // streams 1, 2
		MinTimestamp:   100,
		MaxTimestamp:   400,
		AmbiguousNames: []string{"pod"},
	}}

	rows := expandResults(results)
	require.Len(t, rows, 2)
	require.Equal(t, int64(1), rows[0].streamID)
	require.Equal(t, int64(2), rows[1].streamID)
	for _, row := range rows {
		require.Equal(t, "obj-a", row.objectPath)
		require.Equal(t, int64(1), row.sectionIndex)
		require.Equal(t, int64(100), row.minTimestamp)
		require.Equal(t, int64(400), row.maxTimestamp)
		require.Equal(t, []string{"pod"}, row.ambiguousNames)
	}

	rec := sectionResultsToRecordBatch(rows)
	require.Equal(t, int64(2), rec.NumRows())
}

func TestPostingsIndexSectionsReader_ShardBucketRange(t *testing.T) {
	ctx := user.InjectOrgID(t.Context(), tenantID)
	observations := []postings.LabelObservation{
		{ObjectPath: "src-obj", SectionIndex: 0, ColumnName: "app", LabelValue: "foo", StreamID: 1, Timestamp: now},
	}
	eightBuckets := streams.NewLayout(streams.MustShardLayoutFromBuckets(8), nil)
	sixtyFourBuckets := streams.NewLayout(streams.MustShardLayoutFromBuckets(64), nil)

	setup := func(t *testing.T, hashRange *streams.HashRange, layouts ...streams.Layout) *postingsIndexSectionsReader {
		t.Helper()

		obj, closer := buildIndexObject(t, tenantID, observations, layouts...)
		t.Cleanup(closer)
		matchers := []*labels.Matcher{labels.MustNewMatcher(labels.MatchEqual, "app", "foo")}
		reader := newPostingsIndexSectionsReader(log.NewNopLogger(), obj, now.Add(-time.Hour), now.Add(time.Hour), matchers, nil, 0, hashRange)
		t.Cleanup(reader.Close)
		return reader
	}

	t.Run("maps the upper half of the hashes to the upper buckets of the layout that the index records", func(t *testing.T) {
		reader := setup(t, &streams.HashRange{From: 1 << 63, To: math.MaxUint64}, eightBuckets)

		got, err := reader.shardBucketRange(ctx, tenantID)
		require.NoError(t, err)
		require.Equal(t, &postings.ShardBucketRange{From: 4, To: 7}, got)
	})

	t.Run("returns no range when the range covers every bucket of the layout", func(t *testing.T) {
		reader := setup(t, &streams.HashRange{From: 0, To: math.MaxUint64}, eightBuckets)

		got, err := reader.shardBucketRange(ctx, tenantID)
		require.NoError(t, err)
		require.Nil(t, got)
	})

	t.Run("fails when the stats sections record different layouts", func(t *testing.T) {
		reader := setup(t, &streams.HashRange{From: 1 << 63, To: math.MaxUint64}, eightBuckets, sixtyFourBuckets)

		_, err := reader.shardBucketRange(ctx, tenantID)
		require.ErrorIs(t, err, indexobj.ErrMixedLayouts)
		require.ErrorIs(t, reader.Open(ctx), indexobj.ErrMixedLayouts)
	})

	t.Run("reads no layout when the request has no hash range", func(t *testing.T) {
		reader := setup(t, nil, eightBuckets, sixtyFourBuckets)

		require.NoError(t, reader.Open(ctx))
	})
}
