package metastore

import (
	"bytes"
	"context"
	"io"
	"maps"
	"slices"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/index/indexobj"
	"github.com/grafana/loki/v3/pkg/dataobj/logsobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/indexpointers"
)

func TestTableOfContentsWriter(t *testing.T) {
	t.Run("append new top-level object to new metastore", func(t *testing.T) {
		tenantID := "test"
		tocBuilder, err := indexobj.NewBuilder(tenantID, logsobj.BuilderBaseConfig{
			TargetPageSize:          tocBuilderCfg.TargetPageSize,
			TargetObjectSize:        tocBuilderCfg.TargetObjectSize,
			TargetSectionSize:       tocBuilderCfg.TargetSectionSize,
			BufferSize:              tocBuilderCfg.BufferSize,
			SectionStripeMergeLimit: tocBuilderCfg.SectionStripeMergeLimit,
		}, nil, indexobj.NewBuilderMetrics(nil))
		require.NoError(t, err)

		err = tocBuilder.AppendIndexPointer(indexpointers.IndexPointer{Path: "testdata/metastore.obj", StartTs: unixTime(10), EndTs: unixTime(20)})
		require.NoError(t, err)

		obj, closer, err := tocBuilder.Flush()
		require.NoError(t, err)
		t.Cleanup(func() { closer.Close() })

		bucket := newInMemoryBucket(t, tenantID, unixTime(0), obj)
		tocBuilder.Reset()

		writer := NewTableOfContentsWriter(bucket, log.NewNopLogger())
		err = writer.WriteEntry(context.Background(), tenantID, TableOfContentsEntry{
			Path:      "testdata/metastore.obj",
			StartTime: unixTime(20),
			EndTime:   unixTime(30),
		})
		require.NoError(t, err)
	})

	t.Run("append object whose time range sits exactly on a window boundary", func(t *testing.T) {
		tenantID := "test"
		bucket := objstore.NewInMemBucket()

		writer := newTableOfContentsWriter(t, bucket)

		// An object holding a single log at a 12h-aligned instant produces a
		// zero-width time range sitting exactly on the ToC window boundary.
		// The pointer must still land in that window; regressing the overlap
		// check leaves the builder empty and WriteEntry retries forever, so we
		// bound the context to fail fast rather than hang.
		boundary := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
		require.Equal(t, boundary, boundary.Truncate(MetastoreWindowSize))

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		err := writer.WriteEntry(ctx, tenantID, TableOfContentsEntry{
			Path:      "testdata/metastore.obj",
			StartTime: boundary,
			EndTime:   boundary,
		})
		require.NoError(t, err)

		reader, err := bucket.Get(context.Background(), TableOfContentsPath(tenantID, boundary))
		require.NoError(t, err)
		object, err := io.ReadAll(reader)
		require.NoError(t, err)
		dobj, err := dataobj.FromReaderAt(bytes.NewReader(object), int64(len(object)))
		require.NoError(t, err)
		require.NotEmpty(t, dobj.Sections())
	})

	t.Run("append default to new top-level metastore v1", func(t *testing.T) {
		tenantID := "test"
		builder, err := indexobj.NewBuilder(tenantID, logsobj.BuilderBaseConfig{
			TargetPageSize:          tocBuilderCfg.TargetPageSize,
			TargetObjectSize:        tocBuilderCfg.TargetObjectSize,
			TargetSectionSize:       tocBuilderCfg.TargetSectionSize,
			BufferSize:              tocBuilderCfg.BufferSize,
			SectionStripeMergeLimit: tocBuilderCfg.SectionStripeMergeLimit,
		}, nil, indexobj.NewBuilderMetrics(nil))
		require.NoError(t, err)

		bucket := objstore.NewInMemBucket()

		writer := newTableOfContentsWriter(t, bucket)
		err = writer.WriteEntry(context.Background(), tenantID, TableOfContentsEntry{
			Path:      "testdata/metastore.obj",
			StartTime: unixTime(10),
			EndTime:   unixTime(30),
		})
		require.NoError(t, err)

		reader, err := bucket.Get(context.Background(), TableOfContentsPath(tenantID, unixTime(0)))
		require.NoError(t, err)

		object, err := io.ReadAll(reader)
		require.NoError(t, err)

		dobj, err := dataobj.FromReaderAt(bytes.NewReader(object), int64(len(object)))
		require.NoError(t, err)

		err = copyFromExistingToc(context.Background(), builder, dobj)
		require.NoError(t, err)
	})

	t.Run("copyFromExistingToc returns an error when the ToC holds a row that starts at the Unix epoch", func(t *testing.T) {
		source, err := indexobj.NewBuilder("test", tocBuilderCfg, nil, indexobj.NewBuilderMetrics(nil))
		require.NoError(t, err)
		require.NoError(t, source.AppendIndexPointer(indexpointers.IndexPointer{Path: "indexes/a", StartTs: unixTime(0), EndTs: unixTime(10)}))
		obj, closer, err := source.Flush()
		require.NoError(t, err)
		t.Cleanup(func() { _ = closer.Close() })

		target, err := indexobj.NewBuilder("test", tocBuilderCfg, nil, indexobj.NewBuilderMetrics(nil))
		require.NoError(t, err)
		err = copyFromExistingToc(context.Background(), target, obj)
		require.ErrorContains(t, err, "reading index pointers")
		require.ErrorContains(t, err, "nil or zero value for min_timestamp")
	})

	t.Run("WriteEntry fails when the context is canceled before the ToC is written", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		writer := newTableOfContentsWriter(t, bucket)

		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		err := writer.WriteEntry(ctx, "test", TableOfContentsEntry{
			Path:      "testdata/metastore.obj",
			StartTime: unixTime(10),
			EndTime:   unixTime(20),
		})
		require.ErrorIs(t, err, context.Canceled, "a caller must not treat an unwritten entry as recorded")
		require.Empty(t, bucket.Objects())
	})

	t.Run("WriteEntry writes the tenant's ToC for every window it overlaps", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		writer := newTableOfContentsWriter(t, bucket)

		var (
			w1 = time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
			w2 = w1.Add(MetastoreWindowSize)
		)
		// tenant-a spans two windows, tenant-b only the first one.
		require.NoError(t, writer.WriteEntry(context.Background(), "tenant-a", TableOfContentsEntry{
			Path:      "indexes/a",
			StartTime: w1.Add(time.Hour),
			EndTime:   w2.Add(time.Hour),
		}))
		require.NoError(t, writer.WriteEntry(context.Background(), "tenant-b", TableOfContentsEntry{
			Path:      "indexes/b",
			StartTime: w1.Add(time.Hour),
			EndTime:   w1.Add(2 * time.Hour),
		}))

		require.ElementsMatch(t, []string{
			"tocs/2025-01-01T00_00_00Z/tenant-a/toc.toc",
			"tocs/2025-01-01T12_00_00Z/tenant-a/toc.toc",
			"tocs/2025-01-01T00_00_00Z/tenant-b/toc.toc",
		}, slices.Collect(maps.Keys(bucket.Objects())))

		for _, tc := range []struct {
			tenant, path string
			window       time.Time
		}{
			{"tenant-a", "indexes/a", w1},
			{"tenant-a", "indexes/a", w2},
			{"tenant-b", "indexes/b", w1},
		} {
			rows := readToC(context.Background(), t, bucket, TableOfContentsPath(tc.tenant, tc.window))
			require.Len(t, rows, 1)
			require.Equal(t, tc.tenant, rows[0].Tenant, "a ToC must only hold its own tenant")
			require.Equal(t, tc.path, rows[0].Path)
		}
	})

	for _, tc := range []struct {
		name  string
		entry TableOfContentsEntry
	}{
		{
			name:  "WriteEntry returns an error and writes nothing when the entry has no time range",
			entry: TableOfContentsEntry{Path: "indexes/a"},
		},
		{
			name:  "WriteEntry returns an error and writes nothing when the entry starts at the Unix epoch",
			entry: TableOfContentsEntry{Path: "indexes/a", StartTime: unixTime(0), EndTime: unixTime(10)},
		},
		{
			name:  "WriteEntry returns an error and writes nothing when the entry ends before it starts",
			entry: TableOfContentsEntry{Path: "indexes/a", StartTime: unixTime(20), EndTime: unixTime(10)},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bucket := &countingBucket{Bucket: objstore.NewInMemBucket()}
			writer := newTableOfContentsWriter(t, bucket)

			err := writer.WriteEntry(context.Background(), "tenant-a", tc.entry)
			require.ErrorContains(t, err, "indexes/a")
			require.Zero(t, bucket.calls)
		})
	}

	t.Run("WriteEntry returns an error without retrying and leaves the ToC unchanged when the ToC holds a section of another tenant", func(t *testing.T) {
		inner := objstore.NewInMemBucket()
		tocPath := TableOfContentsPath("tenant-a", unixTime(0))
		uploadToC(t, inner, tocPath, "tenant-b", "indexes/b")
		before := readToC(context.Background(), t, inner, tocPath)

		bucket := &countingBucket{Bucket: inner}
		writer := newTableOfContentsWriter(t, bucket)

		// WriteEntry retries other errors until the context is done, so the
		// timeout turns a regression into a failure instead of a hang.
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		err := writer.WriteEntry(ctx, "tenant-a", TableOfContentsEntry{
			Path:      "indexes/a",
			StartTime: unixTime(10),
			EndTime:   unixTime(20),
		})
		require.ErrorIs(t, err, errUnrecoverable)
		require.NotErrorIs(t, err, context.DeadlineExceeded)
		require.Equal(t, 1, bucket.Calls())
		require.Equal(t, before, readToC(context.Background(), t, inner, tocPath))
	})
}

// writeTimeRanges records path in the ToC of every tenant in timeRanges.
func writeTimeRanges(ctx context.Context, w *TableOfContentsWriter, path string, timeRanges []dataobj.TimeRange) error {
	for _, tr := range timeRanges {
		if err := w.WriteEntry(ctx, tr.Tenant, TableOfContentsEntry{
			Path:      path,
			StartTime: tr.MinTime,
			EndTime:   tr.MaxTime,
		}); err != nil {
			return err
		}
	}
	return nil
}

func newTableOfContentsWriter(t *testing.T, bucket objstore.Bucket) *TableOfContentsWriter {
	t.Helper()

	updater := &TableOfContentsWriter{
		bucket:  bucket,
		metrics: newTableOfContentsMetrics(),
		logger:  log.NewNopLogger(),
	}

	err := updater.RegisterMetrics(prometheus.NewPedanticRegistry())
	require.NoError(t, err)

	return updater
}

func newInMemoryBucket(t *testing.T, tenant string, window time.Time, obj *dataobj.Object) objstore.Bucket {
	t.Helper()

	var (
		bucket = objstore.NewInMemBucket()
		path   = TableOfContentsPath(tenant, window)
	)

	if obj != nil && obj.Size() > 0 {
		reader, err := obj.Reader(t.Context())
		require.NoError(t, err)
		defer reader.Close()

		require.NoError(t, bucket.Upload(t.Context(), path, reader))
	}

	return bucket
}

func unixTime(sec int64) time.Time {
	return time.Unix(sec, 0).UTC()
}
