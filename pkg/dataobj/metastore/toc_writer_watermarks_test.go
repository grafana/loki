package metastore

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/index/indexobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/indexpointers"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/watermarks"
)

const (
	wmTenant = "test"
	wmTopic  = "loki"
)

func int64Ptr(v int64) *int64 { return &v }

func wmEntry(path string) TableOfContentsEntry {
	return TableOfContentsEntry{Path: path, StartTime: unixTime(10), EndTime: unixTime(20)}
}

func writeWatermarkEntry(ctx context.Context, w *TableOfContentsWriter, path string, partition int32, end int64, expected *int64) error {
	return w.WriteEntryWithWatermark(ctx, wmTenant, wmEntry(path), WatermarkUpdate{Topic: wmTopic, Partition: partition, EndOffset: end, Expected: expected})
}

func wmTocPath() string { return TableOfContentsPath(wmTenant, unixTime(0)) }

func seedWatermarkToC(t *testing.T, bucket objstore.Bucket, indexPaths []string, rows []watermarks.Watermark) {
	t.Helper()

	b, err := indexobj.NewBuilder(wmTenant, DefaultTocBuilderConfig, nil, indexobj.NewBuilderMetrics(nil))
	require.NoError(t, err)
	for _, p := range indexPaths {
		require.NoError(t, b.AppendIndexPointer(indexpointers.IndexPointer{Path: p, StartTs: unixTime(10), EndTs: unixTime(20)}))
	}
	for _, r := range rows {
		require.NoError(t, b.AppendWatermark(r.Topic, r.Partition, r.Watermark))
	}
	obj, closer, err := b.Flush()
	require.NoError(t, err)
	t.Cleanup(func() { _ = closer.Close() })
	reader, err := obj.Reader(t.Context())
	require.NoError(t, err)
	require.NoError(t, bucket.Upload(t.Context(), wmTocPath(), reader))
	require.NoError(t, reader.Close())
}

func readWatermarks(ctx context.Context, t *testing.T, bucket objstore.Bucket, path string) []watermarks.Watermark {
	t.Helper()

	raw := readRawToC(ctx, t, bucket, path)
	obj, err := dataobj.FromReaderAt(bytes.NewReader(raw), int64(len(raw)))
	require.NoError(t, err)

	var rows []watermarks.Watermark
	for res := range watermarks.Iter(ctx, obj) {
		rows = append(rows, res.MustValue())
	}
	return rows
}

func wm(partition int32, value int64) watermarks.Watermark {
	return watermarks.Watermark{Topic: wmTopic, Partition: partition, Watermark: value}
}

func TestWriteEntryWithWatermark(t *testing.T) {
	ctx := context.Background()

	t.Run("writes the pointer and the first watermark when the caller and the ToC have no value", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		writer := newTableOfContentsWriter(t, bucket)

		require.NoError(t, writeWatermarkEntry(ctx, writer, "indexes/a", 0, 99, nil))

		require.Equal(t, []tocRow{{Tenant: wmTenant, Path: "indexes/a", StartUnix: 10, EndUnix: 20}}, readToC(ctx, t, bucket, wmTocPath()))
		require.Equal(t, []watermarks.Watermark{wm(0, 99)}, readWatermarks(ctx, t, bucket, wmTocPath()))
	})

	t.Run("raises the watermark when the ToC holds the expected value", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		seedWatermarkToC(t, bucket, []string{"indexes/a"}, []watermarks.Watermark{wm(0, 99)})
		writer := newTableOfContentsWriter(t, bucket)

		require.NoError(t, writeWatermarkEntry(ctx, writer, "indexes/b", 0, 199, int64Ptr(99)))

		require.Equal(t, []tocRow{
			{Tenant: wmTenant, Path: "indexes/a", StartUnix: 10, EndUnix: 20},
			{Tenant: wmTenant, Path: "indexes/b", StartUnix: 10, EndUnix: 20},
		}, readToC(ctx, t, bucket, wmTocPath()))
		require.Equal(t, []watermarks.Watermark{wm(0, 199)}, readWatermarks(ctx, t, bucket, wmTocPath()))
		require.Equal(t, uint64(1), sampleCount(t, writer.metrics.writeSeconds, opWriteEntry, changeWritten))
	})

	t.Run("writes nothing and returns a mismatch error when the ToC holds another value than expected", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		seedWatermarkToC(t, bucket, []string{"indexes/a"}, []watermarks.Watermark{wm(0, 150)})
		before := readRawToC(ctx, t, bucket, wmTocPath())
		bucketCalls := &countingBucket{Bucket: bucket}
		writer := newTableOfContentsWriter(t, bucketCalls)

		err := writeWatermarkEntry(ctx, writer, "indexes/b", 0, 199, int64Ptr(99))

		require.ErrorIs(t, err, ErrWatermarkMismatch)
		require.ErrorContains(t, err, "expected 99, ToC holds 150")
		require.Equal(t, before, readRawToC(ctx, t, bucket, wmTocPath()))
		require.Equal(t, 1, bucketCalls.Calls(), "a mismatch must not retry")
		require.Equal(t, uint64(1), sampleCount(t, writer.metrics.writeSeconds, opWriteEntry, changeWatermarkMismatch))
		require.Equal(t, uint64(1), sampleCount(t, writer.metrics.writeAttemptSeconds, opWriteEntry, changeWatermarkMismatch))
	})

	t.Run("returns a mismatch error when the caller has a watermark and the ToC has none", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		seedWatermarkToC(t, bucket, []string{"indexes/a"}, nil)
		writer := newTableOfContentsWriter(t, bucket)

		err := writeWatermarkEntry(ctx, writer, "indexes/b", 0, 199, int64Ptr(99))

		require.ErrorIs(t, err, ErrWatermarkMismatch)
		require.ErrorContains(t, err, "expected 99, ToC holds none")
		require.Len(t, readToC(ctx, t, bucket, wmTocPath()), 1)
	})

	t.Run("returns a mismatch error when the caller has no watermark and the ToC has one", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		seedWatermarkToC(t, bucket, []string{"indexes/a"}, []watermarks.Watermark{wm(0, 99)})
		writer := newTableOfContentsWriter(t, bucket)

		err := writeWatermarkEntry(ctx, writer, "indexes/b", 0, 199, nil)

		require.ErrorIs(t, err, ErrWatermarkMismatch)
		require.ErrorContains(t, err, "expected none, ToC holds 99")
		require.Len(t, readToC(ctx, t, bucket, wmTocPath()), 1)
	})

	t.Run("ignores the watermarks of other partitions and topics when it checks the watermark", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		seedWatermarkToC(t, bucket, []string{"indexes/a"}, []watermarks.Watermark{
			wm(1, 500),
			{Topic: "other", Partition: 0, Watermark: 7},
		})
		writer := newTableOfContentsWriter(t, bucket)

		require.NoError(t, writeWatermarkEntry(ctx, writer, "indexes/b", 0, 99, nil))
		require.NoError(t, writeWatermarkEntry(ctx, writer, "indexes/c", 1, 600, int64Ptr(500)))

		require.ElementsMatch(t, []watermarks.Watermark{
			wm(0, 99),
			wm(1, 600),
			{Topic: "other", Partition: 0, Watermark: 7},
		}, readWatermarks(ctx, t, bucket, wmTocPath()))
	})

	t.Run("lets two partitions write the same ToC one after the other", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		writer := newTableOfContentsWriter(t, bucket)

		require.NoError(t, writeWatermarkEntry(ctx, writer, "indexes/a", 0, 10, nil))
		require.NoError(t, writeWatermarkEntry(ctx, writer, "indexes/b", 1, 20, nil))
		require.NoError(t, writeWatermarkEntry(ctx, writer, "indexes/c", 0, 30, int64Ptr(10)))

		require.ElementsMatch(t, []watermarks.Watermark{wm(0, 30), wm(1, 20)}, readWatermarks(ctx, t, bucket, wmTocPath()))
		require.Len(t, readToC(ctx, t, bucket, wmTocPath()), 3)
	})

	t.Run("adds a watermark section to a ToC that has none", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		seedToC(t, bucket, unixTime(0), []tocRow{{Tenant: wmTenant, Path: "indexes/a", StartUnix: 10, EndUnix: 20}})
		require.Empty(t, readWatermarks(ctx, t, bucket, wmTocPath()))
		writer := newTableOfContentsWriter(t, bucket)

		require.NoError(t, writeWatermarkEntry(ctx, writer, "indexes/b", 0, 99, nil))

		require.Len(t, readToC(ctx, t, bucket, wmTocPath()), 2)
		require.Equal(t, []watermarks.Watermark{wm(0, 99)}, readWatermarks(ctx, t, bucket, wmTocPath()))
	})

	t.Run("keeps the watermarks when an entry has no watermark update", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		seedWatermarkToC(t, bucket, []string{"indexes/a"}, []watermarks.Watermark{wm(0, 99), wm(1, 5)})
		writer := newTableOfContentsWriter(t, bucket)

		require.NoError(t, writer.WriteEntry(ctx, wmTenant, TableOfContentsEntry{Path: "indexes/b", StartTime: unixTime(10), EndTime: unixTime(20)}))

		require.Len(t, readToC(ctx, t, bucket, wmTocPath()), 2)
		require.ElementsMatch(t, []watermarks.Watermark{wm(0, 99), wm(1, 5)}, readWatermarks(ctx, t, bucket, wmTocPath()))
	})

	t.Run("returns nil and writes nothing when the pointer and a watermark at the end offset exist", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		seedWatermarkToC(t, bucket, []string{"indexes/a"}, []watermarks.Watermark{wm(0, 199)})
		before := readRawToC(ctx, t, bucket, wmTocPath())
		writer := newTableOfContentsWriter(t, bucket)

		require.NoError(t, writeWatermarkEntry(ctx, writer, "indexes/a", 0, 199, int64Ptr(99)))

		require.Equal(t, before, readRawToC(ctx, t, bucket, wmTocPath()))
		require.Equal(t, uint64(1), sampleCount(t, writer.metrics.writeSeconds, opWriteEntry, changeAlreadyApplied))
	})

	t.Run("returns nil and writes nothing when the pointer exists and the watermark is above the end offset", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		seedWatermarkToC(t, bucket, []string{"indexes/a"}, []watermarks.Watermark{wm(0, 500)})
		before := readRawToC(ctx, t, bucket, wmTocPath())
		writer := newTableOfContentsWriter(t, bucket)

		require.NoError(t, writeWatermarkEntry(ctx, writer, "indexes/a", 0, 199, int64Ptr(99)))

		require.Equal(t, before, readRawToC(ctx, t, bucket, wmTocPath()))
	})

	t.Run("raises the watermark without a second pointer when the pointer exists and the watermark is lower", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		seedWatermarkToC(t, bucket, []string{"indexes/a"}, []watermarks.Watermark{wm(0, 99)})
		writer := newTableOfContentsWriter(t, bucket)

		require.NoError(t, writeWatermarkEntry(ctx, writer, "indexes/a", 0, 199, int64Ptr(99)))

		require.Equal(t, []tocRow{{Tenant: wmTenant, Path: "indexes/a", StartUnix: 10, EndUnix: 20}}, readToC(ctx, t, bucket, wmTocPath()))
		require.Equal(t, []watermarks.Watermark{wm(0, 199)}, readWatermarks(ctx, t, bucket, wmTocPath()))
	})

	t.Run("returns a mismatch error when the pointer exists, the watermark is lower and it differs from the expected value", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		seedWatermarkToC(t, bucket, []string{"indexes/a"}, []watermarks.Watermark{wm(0, 50)})
		writer := newTableOfContentsWriter(t, bucket)

		err := writeWatermarkEntry(ctx, writer, "indexes/a", 0, 199, int64Ptr(99))

		require.ErrorIs(t, err, ErrWatermarkMismatch)
		require.Equal(t, []watermarks.Watermark{wm(0, 50)}, readWatermarks(ctx, t, bucket, wmTocPath()))
	})

	t.Run("adds the first watermark when the pointer exists and the ToC has no watermark", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		seedToC(t, bucket, unixTime(0), []tocRow{{Tenant: wmTenant, Path: "indexes/a", StartUnix: 10, EndUnix: 20}})
		writer := newTableOfContentsWriter(t, bucket)

		require.NoError(t, writeWatermarkEntry(ctx, writer, "indexes/a", 0, 199, nil))

		require.Len(t, readToC(ctx, t, bucket, wmTocPath()), 1)
		require.Equal(t, []watermarks.Watermark{wm(0, 199)}, readWatermarks(ctx, t, bucket, wmTocPath()))
	})

	t.Run("returns nil on the retry when a write succeeded and its response was lost", func(t *testing.T) {
		bucket := &failAfterWriteBucket{Bucket: objstore.NewInMemBucket()}
		seedWatermarkToC(t, bucket, []string{"indexes/a"}, []watermarks.Watermark{wm(0, 99)})
		writer := newTableOfContentsWriterWithBackoff(t, bucket, 3)

		require.NoError(t, writeWatermarkEntry(ctx, writer, "indexes/b", 0, 199, int64Ptr(99)))

		require.Len(t, readToC(ctx, t, bucket, wmTocPath()), 2)
		require.Equal(t, []watermarks.Watermark{wm(0, 199)}, readWatermarks(ctx, t, bucket, wmTocPath()))
		require.Equal(t, uint64(1), sampleCount(t, writer.metrics.writeAttemptSeconds, opWriteEntry, changeAlreadyApplied))
	})

	t.Run("retries a lost conditional write and passes when the other writer used another partition", func(t *testing.T) {
		inner := objstore.NewInMemBucket()
		seedWatermarkToC(t, inner, []string{"indexes/a"}, []watermarks.Watermark{wm(0, 99)})
		other := newTableOfContentsWriter(t, inner)
		bucket := &conflictBucket{Bucket: inner, concurrentWrite: func() {
			require.NoError(t, writeWatermarkEntry(ctx, other, "indexes/other", 1, 7, nil))
		}}
		writer := newTableOfContentsWriterWithBackoff(t, bucket, 3)

		require.NoError(t, writeWatermarkEntry(ctx, writer, "indexes/b", 0, 199, int64Ptr(99)))

		require.Equal(t, 2, bucket.calls)
		require.Len(t, readToC(ctx, t, inner, wmTocPath()), 3)
		require.ElementsMatch(t, []watermarks.Watermark{wm(0, 199), wm(1, 7)}, readWatermarks(ctx, t, inner, wmTocPath()))
		require.Equal(t, uint64(1), sampleCount(t, writer.metrics.writeAttemptSeconds, opWriteEntry, changeConflict))
	})

	t.Run("retries a lost conditional write and returns a mismatch error when the other writer used the same partition", func(t *testing.T) {
		inner := objstore.NewInMemBucket()
		seedWatermarkToC(t, inner, []string{"indexes/a"}, []watermarks.Watermark{wm(0, 99)})
		other := newTableOfContentsWriter(t, inner)
		bucket := &conflictBucket{Bucket: inner, concurrentWrite: func() {
			require.NoError(t, writeWatermarkEntry(ctx, other, "indexes/other", 0, 150, int64Ptr(99)))
		}}
		writer := newTableOfContentsWriterWithBackoff(t, bucket, 3)

		err := writeWatermarkEntry(ctx, writer, "indexes/b", 0, 199, int64Ptr(99))

		require.ErrorIs(t, err, ErrWatermarkMismatch)
		require.ErrorContains(t, err, "ToC holds 150")
		require.Equal(t, 2, bucket.calls, "the mismatch must stop the retries")
		require.Len(t, readToC(ctx, t, inner, wmTocPath()), 2)
		require.Equal(t, []watermarks.Watermark{wm(0, 150)}, readWatermarks(ctx, t, inner, wmTocPath()))
	})

	t.Run("returns a mismatch error and creates no ToC when the ToC does not exist and the caller has a value", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		writer := newTableOfContentsWriter(t, bucket)

		err := writeWatermarkEntry(ctx, writer, "indexes/a", 0, 199, int64Ptr(99))

		require.ErrorIs(t, err, ErrWatermarkMismatch)
		require.Empty(t, bucket.Objects())
		require.Equal(t, uint64(1), sampleCount(t, writer.metrics.writeSeconds, opWriteEntry, changeWatermarkMismatch))
	})

	t.Run("writes nothing when the entry is written again and the ToC holds watermarks", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		seedWatermarkToC(t, bucket, []string{"indexes/a"}, []watermarks.Watermark{wm(0, 99)})
		before := readRawToC(ctx, t, bucket, wmTocPath())
		writer := newTableOfContentsWriter(t, bucket)

		require.NoError(t, writer.WriteEntry(ctx, wmTenant, wmEntry("indexes/a")))

		require.Equal(t, before, readRawToC(ctx, t, bucket, wmTocPath()))
		require.Equal(t, uint64(1), sampleCount(t, writer.metrics.writeSeconds, opWriteEntry, changeAlreadyApplied))
	})

	t.Run("starts a ToC of another window with no watermark", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		writer := newTableOfContentsWriter(t, bucket)
		nextWindow := unixTime(int64(MetastoreWindowSize / time.Second))
		nextEntry := TableOfContentsEntry{Path: "indexes/next", StartTime: nextWindow.Add(10 * time.Second), EndTime: nextWindow.Add(20 * time.Second)}
		update := WatermarkUpdate{Topic: wmTopic, Partition: 0, EndOffset: 200}

		require.NoError(t, writeWatermarkEntry(ctx, writer, "indexes/a", 0, 99, nil))
		require.NoError(t, writer.WriteEntryWithWatermark(ctx, wmTenant, nextEntry, update))

		require.Equal(t, []watermarks.Watermark{wm(0, 99)}, readWatermarks(ctx, t, bucket, wmTocPath()))
		require.Equal(t, []watermarks.Watermark{wm(0, 200)}, readWatermarks(ctx, t, bucket, TableOfContentsPath(wmTenant, nextWindow)))
	})

	t.Run("accepts an end offset and an expected value of zero", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		writer := newTableOfContentsWriter(t, bucket)

		require.NoError(t, writeWatermarkEntry(ctx, writer, "indexes/a", 0, 0, nil))
		require.NoError(t, writeWatermarkEntry(ctx, writer, "indexes/b", 0, 0, int64Ptr(0)))

		require.Len(t, readToC(ctx, t, bucket, wmTocPath()), 2)
		require.Equal(t, []watermarks.Watermark{wm(0, 0)}, readWatermarks(ctx, t, bucket, wmTocPath()))
	})

	t.Run("returns a mismatch error and keeps the ToC when the pointer is new and the watermark is above the end offset", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		seedWatermarkToC(t, bucket, []string{"indexes/a"}, []watermarks.Watermark{wm(0, 500)})
		before := readRawToC(ctx, t, bucket, wmTocPath())
		writer := newTableOfContentsWriter(t, bucket)

		err := writeWatermarkEntry(ctx, writer, "indexes/b", 0, 199, int64Ptr(99))

		require.ErrorIs(t, err, ErrWatermarkMismatch)
		require.Equal(t, before, readRawToC(ctx, t, bucket, wmTocPath()))
	})

	t.Run("adds the pointer and keeps the watermark when the expected value equals the end offset", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		seedWatermarkToC(t, bucket, []string{"indexes/a"}, []watermarks.Watermark{wm(0, 99)})
		writer := newTableOfContentsWriter(t, bucket)

		require.NoError(t, writeWatermarkEntry(ctx, writer, "indexes/b", 0, 99, int64Ptr(99)))

		require.Len(t, readToC(ctx, t, bucket, wmTocPath()), 2)
		require.Equal(t, []watermarks.Watermark{wm(0, 99)}, readWatermarks(ctx, t, bucket, wmTocPath()))
	})

	t.Run("returns a validation error and writes nothing when the update is not valid", func(t *testing.T) {
		for _, tc := range []struct {
			name    string
			update  WatermarkUpdate
			wantErr string
		}{
			{"returns an error when the topic is empty", WatermarkUpdate{Partition: 0, EndOffset: 1}, "has no topic"},
			{"returns an error when the partition is negative", WatermarkUpdate{Topic: wmTopic, Partition: -1, EndOffset: 1}, "negative partition"},
			{"returns an error when the end offset is negative", WatermarkUpdate{Topic: wmTopic, EndOffset: -1}, "negative end offset"},
			{"returns an error when the expected value is negative", WatermarkUpdate{Topic: wmTopic, EndOffset: 1, Expected: int64Ptr(-1)}, "negative expected value"},
			{"returns an error when the end offset is below the expected value", WatermarkUpdate{Topic: wmTopic, EndOffset: 1, Expected: int64Ptr(2)}, "below expected value"},
		} {
			t.Run(tc.name, func(t *testing.T) {
				bucket := objstore.NewInMemBucket()
				writer := newTableOfContentsWriter(t, bucket)

				err := writer.WriteEntryWithWatermark(ctx, wmTenant, wmEntry("indexes/a"), tc.update)

				require.ErrorContains(t, err, tc.wantErr)
				require.NotErrorIs(t, err, ErrWatermarkMismatch)
				require.Empty(t, bucket.Objects())
			})
		}
	})
}
