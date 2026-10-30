package metastore

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"net/http"
	"slices"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/backoff"
	"github.com/minio/minio-go/v7"
	pkgerrors "github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/v4/pkg/dataobj"
	"github.com/grafana/loki/v4/pkg/dataobj/index/indexobj"
	"github.com/grafana/loki/v4/pkg/dataobj/sections/indexpointers"
)

func TestWriteEntry(t *testing.T) {
	t.Run("appends the entry to an existing ToC", func(t *testing.T) {
		tenantID := "test"
		tocBuilder, err := indexobj.NewBuilder(tenantID, DefaultTocBuilderConfig, nil, indexobj.NewBuilderMetrics(nil))
		require.NoError(t, err)

		err = tocBuilder.AppendIndexPointer(indexpointers.IndexPointer{Path: "testdata/metastore.obj", StartTs: unixTime(10), EndTs: unixTime(20)})
		require.NoError(t, err)

		obj, closer, err := tocBuilder.Flush()
		require.NoError(t, err)
		t.Cleanup(func() { closer.Close() })

		bucket := newInMemoryBucket(t, tenantID, unixTime(0), obj)
		tocBuilder.Reset()

		writer := newTableOfContentsWriter(t, bucket)
		err = writer.WriteEntry(context.Background(), tenantID, TableOfContentsEntry{
			Path:      "testdata/other.obj",
			StartTime: unixTime(20),
			EndTime:   unixTime(30),
		})
		require.NoError(t, err)
		require.ElementsMatch(t, []tocRow{
			{Tenant: tenantID, Path: "testdata/metastore.obj", StartUnix: 10, EndUnix: 20},
			{Tenant: tenantID, Path: "testdata/other.obj", StartUnix: 20, EndUnix: 30},
		}, readToC(context.Background(), t, bucket, TableOfContentsPath(tenantID, unixTime(0))))
	})

	t.Run("fails and reports cancelled when the context is canceled before the ToC is written", func(t *testing.T) {
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
		require.Equal(t, uint64(1), sampleCount(t, writer.metrics.writeSeconds, opWriteEntry, changeCancelled))
	})

	t.Run("writes each tenant's entry only to that tenant's ToC", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		writer := newTableOfContentsWriter(t, bucket)

		window := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
		require.NoError(t, writer.WriteEntry(context.Background(), "tenant-a", TableOfContentsEntry{
			Path:      "indexes/a",
			StartTime: window.Add(time.Hour),
			EndTime:   window.Add(2 * time.Hour),
		}))
		require.NoError(t, writer.WriteEntry(context.Background(), "tenant-b", TableOfContentsEntry{
			Path:      "indexes/b",
			StartTime: window.Add(time.Hour),
			EndTime:   window.Add(2 * time.Hour),
		}))

		require.ElementsMatch(t, []string{
			"tocs/2025-01-01T00_00_00Z/tenant-a/toc.toc",
			"tocs/2025-01-01T00_00_00Z/tenant-b/toc.toc",
		}, slices.Collect(maps.Keys(bucket.Objects())))

		for _, tc := range []struct{ tenant, path string }{
			{"tenant-a", "indexes/a"},
			{"tenant-b", "indexes/b"},
		} {
			rows := readToC(context.Background(), t, bucket, TableOfContentsPath(tc.tenant, window))
			require.Len(t, rows, 1)
			require.Equal(t, tc.tenant, rows[0].Tenant, "a ToC must only hold its own tenant")
			require.Equal(t, tc.path, rows[0].Path)
		}
	})

	t.Run("returns the TimeWindow error and writes nothing when the entry is not valid", func(t *testing.T) {
		bucket := &countingBucket{Bucket: objstore.NewInMemBucket()}
		writer := newTableOfContentsWriter(t, bucket)

		err := writer.WriteEntry(context.Background(), "tenant-a", TableOfContentsEntry{
			Path:      "indexes/a",
			StartTime: unixTime(10),
			EndTime:   unixTime(0).Add(MetastoreWindowSize),
		})
		require.ErrorContains(t, err, "spans more than one ToC window")
		require.Zero(t, bucket.Calls())
	})

	t.Run("leaves the ToC unchanged and reports already_applied when the ToC already holds the entry's path", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		writer := newTableOfContentsWriter(t, bucket)
		entry := TableOfContentsEntry{Path: "indexes/a", StartTime: unixTime(10), EndTime: unixTime(20)}
		tocPath := TableOfContentsPath("tenant-a", unixTime(0))

		require.NoError(t, writer.WriteEntry(context.Background(), "tenant-a", entry))
		before := bucket.Objects()[tocPath]

		require.NoError(t, writer.WriteEntry(context.Background(), "tenant-a", entry))
		require.Equal(t, []tocRow{{Tenant: "tenant-a", Path: "indexes/a", StartUnix: 10, EndUnix: 20}}, readToC(context.Background(), t, bucket, tocPath))
		require.Equal(t, before, bucket.Objects()[tocPath], "WriteEntry must not rewrite the ToC")

		want := map[changeResult]uint64{changeWritten: 1, changeAlreadyApplied: 1, changeSuperseded: 0, changeFailed: 0, changeConflict: 0}
		require.Equal(t, want, sampleCounts(t, writer.metrics.writeAttemptSeconds, opWriteEntry))
		require.Equal(t, want, sampleCounts(t, writer.metrics.writeSeconds, opWriteEntry))
	})

	t.Run("leaves the ToC unchanged when the ToC holds the entry's path with another time range", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		writer := newTableOfContentsWriter(t, bucket)
		tocPath := TableOfContentsPath("tenant-a", unixTime(0))

		require.NoError(t, writer.WriteEntry(context.Background(), "tenant-a", TableOfContentsEntry{Path: "indexes/a", StartTime: unixTime(10), EndTime: unixTime(20)}))
		require.NoError(t, writer.WriteEntry(context.Background(), "tenant-a", TableOfContentsEntry{Path: "indexes/a", StartTime: unixTime(10), EndTime: unixTime(30)}))

		require.Equal(t, []tocRow{{Tenant: "tenant-a", Path: "indexes/a", StartUnix: 10, EndUnix: 20}}, readToC(context.Background(), t, bucket, tocPath))
		require.Equal(t, map[changeResult]uint64{changeWritten: 1, changeAlreadyApplied: 1, changeSuperseded: 0, changeFailed: 0, changeConflict: 0}, sampleCounts(t, writer.metrics.writeSeconds, opWriteEntry))
	})

	t.Run("skips the entry when its path is the last of more than one read batch of pointers", func(t *testing.T) {
		inner := objstore.NewInMemBucket()
		tocPath := TableOfContentsPath("tenant-a", unixTime(0))
		paths := make([]string, 300)
		for i := range paths {
			paths[i] = fmt.Sprintf("indexes/%03d", i)
		}
		uploadToC(t, inner, tocPath, "tenant-a", paths...)
		before := inner.Objects()[tocPath]

		writer := newTableOfContentsWriter(t, inner)
		err := writer.WriteEntry(context.Background(), "tenant-a", TableOfContentsEntry{
			Path:      paths[len(paths)-1],
			StartTime: unixTime(10),
			EndTime:   unixTime(20),
		})
		require.NoError(t, err)
		require.Equal(t, before, inner.Objects()[tocPath])
	})

	t.Run("appends the entry once and reports already_applied when GetAndReplace writes the ToC and then returns an error", func(t *testing.T) {
		inner := objstore.NewInMemBucket()
		bucket := &failAfterWriteBucket{Bucket: inner}
		writer := newTableOfContentsWriter(t, bucket)
		tocPath := TableOfContentsPath("tenant-a", unixTime(0))

		err := writer.WriteEntry(context.Background(), "tenant-a", TableOfContentsEntry{
			Path:      "indexes/a",
			StartTime: unixTime(10),
			EndTime:   unixTime(20),
		})
		require.NoError(t, err)
		require.Equal(t, 2, bucket.calls)
		require.Equal(t, []tocRow{{Tenant: "tenant-a", Path: "indexes/a", StartUnix: 10, EndUnix: 20}}, readToC(context.Background(), t, inner, tocPath))

		require.Equal(t, map[changeResult]uint64{changeWritten: 0, changeAlreadyApplied: 1, changeSuperseded: 0, changeFailed: 1, changeConflict: 0}, sampleCounts(t, writer.metrics.writeAttemptSeconds, opWriteEntry))
		require.Equal(t, map[changeResult]uint64{changeWritten: 0, changeAlreadyApplied: 1, changeSuperseded: 0, changeFailed: 0, changeConflict: 0}, sampleCounts(t, writer.metrics.writeSeconds, opWriteEntry))
	})

	t.Run("keeps the row that another writer added between two attempts and adds the entry once", func(t *testing.T) {
		inner := objstore.NewInMemBucket()
		tocPath := TableOfContentsPath("tenant-a", unixTime(0))
		uploadToC(t, inner, tocPath, "tenant-a", "indexes/old")

		bucket := &conflictBucket{Bucket: inner, concurrentWrite: func() {
			uploadToC(t, inner, tocPath, "tenant-a", "indexes/old", "indexes/other")
		}}
		writer := newTableOfContentsWriter(t, bucket)

		err := writer.WriteEntry(context.Background(), "tenant-a", TableOfContentsEntry{
			Path:      "indexes/a",
			StartTime: unixTime(10),
			EndTime:   unixTime(20),
		})
		require.NoError(t, err)
		require.Equal(t, 2, bucket.calls)
		require.Equal(t, []tocRow{
			{Tenant: "tenant-a", Path: "indexes/a", StartUnix: 10, EndUnix: 20},
			{Tenant: "tenant-a", Path: "indexes/old", StartUnix: 10, EndUnix: 20},
			{Tenant: "tenant-a", Path: "indexes/other", StartUnix: 10, EndUnix: 20},
		}, readToC(context.Background(), t, inner, tocPath))

		require.Equal(t, map[changeResult]uint64{changeWritten: 1, changeAlreadyApplied: 0, changeSuperseded: 0, changeFailed: 0, changeConflict: 1}, sampleCounts(t, writer.metrics.writeAttemptSeconds, opWriteEntry))
		require.Equal(t, map[changeResult]uint64{changeWritten: 1, changeAlreadyApplied: 0, changeSuperseded: 0, changeFailed: 0, changeConflict: 0}, sampleCounts(t, writer.metrics.writeSeconds, opWriteEntry))
	})

	t.Run("returns an error and reports failed after the last attempt when every conditional write conflicts", func(t *testing.T) {
		bucket := &failingBucket{Bucket: objstore.NewInMemBucket(), err: errConditionalWriteConflict}
		writer := newTableOfContentsWriterWithBackoff(t, bucket, 3)

		err := writer.WriteEntry(context.Background(), "tenant-a", TableOfContentsEntry{
			Path:      "indexes/a",
			StartTime: unixTime(10),
			EndTime:   unixTime(20),
		})
		require.ErrorContains(t, err, "terminated after 3 retries")
		require.ErrorIs(t, err, errConditionalWriteConflict)
		require.Equal(t, 3, bucket.Calls())

		require.Equal(t, map[changeResult]uint64{changeWritten: 0, changeAlreadyApplied: 0, changeSuperseded: 0, changeFailed: 0, changeConflict: 3}, sampleCounts(t, writer.metrics.writeAttemptSeconds, opWriteEntry))
		require.Equal(t, map[changeResult]uint64{changeWritten: 0, changeAlreadyApplied: 0, changeSuperseded: 0, changeFailed: 1, changeConflict: 0}, sampleCounts(t, writer.metrics.writeSeconds, opWriteEntry))
	})

	t.Run("returns an error after the last attempt when every write fails", func(t *testing.T) {
		bucket := &failingBucket{Bucket: objstore.NewInMemBucket()}
		writer := newTableOfContentsWriterWithBackoff(t, bucket, 3)

		err := writer.WriteEntry(context.Background(), "tenant-a", TableOfContentsEntry{
			Path:      "indexes/a",
			StartTime: unixTime(10),
			EndTime:   unixTime(20),
		})
		require.ErrorContains(t, err, "terminated after 3 retries")
		require.ErrorIs(t, err, errWriteFailed)
		require.Equal(t, 3, bucket.Calls())

		require.Equal(t, map[changeResult]uint64{changeWritten: 0, changeAlreadyApplied: 0, changeSuperseded: 0, changeFailed: 3, changeConflict: 0}, sampleCounts(t, writer.metrics.writeAttemptSeconds, opWriteEntry))
		require.Equal(t, map[changeResult]uint64{changeWritten: 0, changeAlreadyApplied: 0, changeSuperseded: 0, changeFailed: 1, changeConflict: 0}, sampleCounts(t, writer.metrics.writeSeconds, opWriteEntry))
	})

	t.Run("writes every entry when tenants write concurrently", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		writer := newTableOfContentsWriter(t, bucket)

		var wg sync.WaitGroup
		for i := range 8 {
			wg.Go(func() {
				tenant := fmt.Sprintf("tenant-%d", i)
				for j := range 4 {
					assert.NoError(t, writer.WriteEntry(context.Background(), tenant, TableOfContentsEntry{
						Path:      fmt.Sprintf("indexes/%s/%d", tenant, j),
						StartTime: unixTime(10),
						EndTime:   unixTime(20),
					}))
				}
			})
		}
		wg.Wait()

		for i := range 8 {
			rows := readToC(context.Background(), t, bucket, TableOfContentsPath(fmt.Sprintf("tenant-%d", i), unixTime(0)))
			require.Len(t, rows, 4)
		}
	})

	for _, tc := range []struct {
		name  string
		paths []string
	}{
		{
			name:  "returns an error without retrying and leaves the ToC unchanged when the ToC holds a section of another tenant",
			paths: []string{"indexes/b"},
		},
		{
			name:  "returns an error without retrying and leaves the ToC unchanged when a section of another tenant holds the entry's path",
			paths: []string{"indexes/a"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			inner := objstore.NewInMemBucket()
			tocPath := TableOfContentsPath("tenant-a", unixTime(0))
			uploadToC(t, inner, tocPath, "tenant-b", tc.paths...)
			before := readToC(context.Background(), t, inner, tocPath)

			bucket := &countingBucket{Bucket: inner}
			writer := newTableOfContentsWriter(t, bucket)

			// The timeout makes a regression that retries fail fast instead of
			// waiting through every retry.
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

			want := map[changeResult]uint64{changeWritten: 0, changeAlreadyApplied: 0, changeSuperseded: 0, changeFailed: 1, changeConflict: 0}
			require.Equal(t, want, sampleCounts(t, writer.metrics.writeAttemptSeconds, opWriteEntry))
			require.Equal(t, want, sampleCounts(t, writer.metrics.writeSeconds, opWriteEntry))
		})
	}
}

func TestTableOfContentsEntryTimeWindow(t *testing.T) {
	window := time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	require.Equal(t, window, window.Truncate(MetastoreWindowSize))

	for _, tc := range []struct {
		name       string
		start, end time.Time
		wantWindow time.Time
		wantErr    string
	}{
		{
			name:       "returns the window that holds the entry",
			start:      window.Add(time.Hour),
			end:        window.Add(2 * time.Hour),
			wantWindow: window,
		},
		{
			name:       "returns the window that starts at the entry when the entry is zero-width on a window boundary",
			start:      window,
			end:        window,
			wantWindow: window,
		},
		{
			name:       "returns the window when the entry ends at the last instant of the window",
			start:      window.Add(time.Hour),
			end:        window.Add(MetastoreWindowSize - time.Nanosecond),
			wantWindow: window,
		},
		{
			name:       "returns the window in UTC when the entry times are in another time zone",
			start:      window.Add(time.Hour).In(time.FixedZone("UTC+5", 5*60*60)),
			end:        window.Add(2 * time.Hour).In(time.FixedZone("UTC+5", 5*60*60)),
			wantWindow: window,
		},
		{
			name:    "returns an error when the entry has no time range",
			wantErr: "not after the Unix epoch",
		},
		{
			name:    "returns an error when the entry starts at the Unix epoch",
			start:   unixTime(0),
			end:     unixTime(10),
			wantErr: "not after the Unix epoch",
		},
		{
			name:    "returns an error when the entry ends before it starts",
			start:   window.Add(2 * time.Hour),
			end:     window.Add(time.Hour),
			wantErr: "before its start",
		},
		{
			name:    "returns an error when the entry ends on the next window boundary",
			start:   window.Add(time.Hour),
			end:     window.Add(MetastoreWindowSize),
			wantErr: "spans more than one ToC window",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := TableOfContentsEntry{Path: "indexes/a", StartTime: tc.start, EndTime: tc.end}.TimeWindow()
			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
				require.ErrorContains(t, err, "indexes/a")
				require.True(t, got.IsZero())
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.wantWindow, got)
			require.Equal(t, time.UTC, got.Location())
		})
	}
}

func TestRebuildToc(t *testing.T) {
	t.Run("keeps every pointer of the existing ToC and adds the new entry", func(t *testing.T) {
		tenantID := "test"
		builder, err := indexobj.NewBuilder(tenantID, DefaultTocBuilderConfig, nil, indexobj.NewBuilderMetrics(nil))
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
		defer reader.Close()

		rebuilt, err := writer.rebuildToc(context.Background(), builder, reader, tocChange{
			add: []TableOfContentsEntry{{Path: "testdata/other.obj", StartTime: unixTime(40), EndTime: unixTime(50)}},
		})
		require.NoError(t, err)
		out := objstore.NewInMemBucket()
		require.NoError(t, out.Upload(context.Background(), "toc", rebuilt))
		require.NoError(t, rebuilt.Close())
		require.ElementsMatch(t, []tocRow{
			{Tenant: tenantID, Path: "testdata/metastore.obj", StartUnix: 10, EndUnix: 30},
			{Tenant: tenantID, Path: "testdata/other.obj", StartUnix: 40, EndUnix: 50},
		}, readToC(context.Background(), t, out, "toc"))
	})

	t.Run("returns an error when the ToC holds a row that starts at the Unix epoch", func(t *testing.T) {
		source, err := indexobj.NewBuilder("test", DefaultTocBuilderConfig, nil, indexobj.NewBuilderMetrics(nil))
		require.NoError(t, err)
		require.NoError(t, source.AppendIndexPointer(indexpointers.IndexPointer{Path: "indexes/a", StartTs: unixTime(0), EndTs: unixTime(10)}))
		obj, closer, err := source.Flush()
		require.NoError(t, err)
		t.Cleanup(func() { _ = closer.Close() })
		reader, err := obj.Reader(context.Background())
		require.NoError(t, err)
		defer reader.Close()

		target, err := indexobj.NewBuilder("test", DefaultTocBuilderConfig, nil, indexobj.NewBuilderMetrics(nil))
		require.NoError(t, err)
		writer := newTableOfContentsWriter(t, objstore.NewInMemBucket())
		_, err = writer.rebuildToc(context.Background(), target, reader, tocChange{
			add: []TableOfContentsEntry{{Path: "indexes/b", StartTime: unixTime(10), EndTime: unixTime(20)}},
		})
		require.ErrorContains(t, err, "reading index pointers")
		require.ErrorContains(t, err, "nil or zero value for min_timestamp")
	})
}

func TestReplaceIndexPointers(t *testing.T) {
	t.Run("replaces the old paths with the new entry and reports written", func(t *testing.T) {
		ctx := context.Background()
		window := unixTime(0)
		bucket := objstore.NewInMemBucket()
		seedToC(t, bucket, window, []tocRow{
			{Tenant: "tenantA", Path: "idx/a-0", StartUnix: 10, EndUnix: 20},
			{Tenant: "tenantA", Path: "idx/a-1", StartUnix: 30, EndUnix: 40},
			{Tenant: "tenantB", Path: "idx/b-0", StartUnix: 11, EndUnix: 21},
			{Tenant: "tenantB", Path: "idx/b-1", StartUnix: 31, EndUnix: 41},
		})

		writer := newTableOfContentsWriter(t, bucket)
		swapped, err := writer.ReplaceIndexPointers(ctx, window, "tenantA",
			[]string{"idx/a-0", "idx/a-1"},
			[]TableOfContentsEntry{{Path: "idx/a-new", StartTime: unixTime(100), EndTime: unixTime(110)}},
		)
		require.NoError(t, err)
		require.True(t, swapped)
		require.Equal(t, []tocRow{
			{Tenant: "tenantA", Path: "idx/a-new", StartUnix: 100, EndUnix: 110},
			{Tenant: "tenantB", Path: "idx/b-0", StartUnix: 11, EndUnix: 21},
			{Tenant: "tenantB", Path: "idx/b-1", StartUnix: 31, EndUnix: 41},
		}, readWindowToCs(ctx, t, bucket, window))
		require.Equal(t, map[changeResult]uint64{changeWritten: 1, changeAlreadyApplied: 0, changeSuperseded: 0, changeFailed: 0, changeConflict: 0}, sampleCounts(t, writer.metrics.writeSeconds, opReplace))
	})

	t.Run("leaves the ToCs of the other tenants unchanged", func(t *testing.T) {
		ctx := context.Background()
		window := unixTime(0)
		bucket := objstore.NewInMemBucket()
		seedToC(t, bucket, window, []tocRow{
			{Tenant: "tenantA", Path: "idx/a-0", StartUnix: 10, EndUnix: 20},
			{Tenant: "tenantA", Path: "idx/a-1", StartUnix: 30, EndUnix: 40},
			{Tenant: "tenantA", Path: "idx/a-2", StartUnix: 50, EndUnix: 60},
			{Tenant: "tenantB", Path: "idx/b-0", StartUnix: 11, EndUnix: 21},
			{Tenant: "tenantB", Path: "idx/b-1", StartUnix: 31, EndUnix: 41},
			{Tenant: "tenantC", Path: "idx/c-0", StartUnix: 12, EndUnix: 22},
			{Tenant: "tenantC", Path: "idx/c-1", StartUnix: 32, EndUnix: 42},
		})
		otherRowsBefore := filterRows(readWindowToCs(ctx, t, bucket, window), "tenantB", "tenantC")

		writer := newTableOfContentsWriter(t, bucket)
		swapped, err := writer.ReplaceIndexPointers(ctx, window, "tenantA",
			[]string{"idx/a-0", "idx/a-1", "idx/a-2"},
			[]TableOfContentsEntry{{Path: "idx/a-merged", StartTime: unixTime(10), EndTime: unixTime(60)}},
		)
		require.NoError(t, err)
		require.True(t, swapped)

		after := readWindowToCs(ctx, t, bucket, window)
		require.Equal(t, []tocRow{{Tenant: "tenantA", Path: "idx/a-merged", StartUnix: 10, EndUnix: 60}}, filterRows(after, "tenantA"))
		require.Equal(t, otherRowsBefore, filterRows(after, "tenantB", "tenantC"))
	})

	t.Run("returns an error, leaves the ToC unchanged byte for byte without retrying and reports superseded when the ToC lacks one of the old paths", func(t *testing.T) {
		ctx := context.Background()
		window := unixTime(0)
		inner := objstore.NewInMemBucket()
		seedToC(t, inner, window, []tocRow{
			{Tenant: "tenantA", Path: "idx/a-0", StartUnix: 10, EndUnix: 20},
			{Tenant: "tenantA", Path: "idx/a-1", StartUnix: 30, EndUnix: 40},
			{Tenant: "tenantA", Path: "idx/a-2", StartUnix: 50, EndUnix: 60},
		})
		tocPath := TableOfContentsPath("tenantA", window)
		before := readRawToC(ctx, t, inner, tocPath)

		var logs bytes.Buffer
		bucket := &countingBucket{Bucket: inner}
		writer := NewTableOfContentsWriter(
			bucket,
			DefaultTocWriterBackoffConfig,
			DefaultTocBuilderConfig,
			log.NewLogfmtLogger(&logs),
			NewTocWriterMetrics(prometheus.NewPedanticRegistry()),
			indexobj.NewBuilderMetrics(nil),
		)
		swapped, err := writer.ReplaceIndexPointers(ctx, window, "tenantA",
			[]string{"idx/a-0", "idx/a-gone-1", "idx/a-1", "idx/a-gone-0"},
			[]TableOfContentsEntry{{Path: "idx/a-new", StartTime: unixTime(10), EndTime: unixTime(40)}},
		)
		require.ErrorIs(t, err, errChangeSuperseded)
		require.False(t, swapped)
		require.Equal(t, 1, bucket.Calls())
		require.Equal(t, before, readRawToC(ctx, t, inner, tocPath))
		require.Contains(t, logs.String(), "ToC is missing paths to remove: 2 of 4")

		want := map[changeResult]uint64{changeWritten: 0, changeAlreadyApplied: 0, changeSuperseded: 1, changeFailed: 0, changeConflict: 0}
		require.Equal(t, want, sampleCounts(t, writer.metrics.writeAttemptSeconds, opReplace))
		require.Equal(t, want, sampleCounts(t, writer.metrics.writeSeconds, opReplace))
	})

	t.Run("returns an error, leaves the ToC unchanged and reports superseded when the ToC holds one old path twice and lacks the other", func(t *testing.T) {
		ctx := context.Background()
		window := unixTime(0)
		bucket := objstore.NewInMemBucket()
		seedToC(t, bucket, window, []tocRow{
			{Tenant: "tenantA", Path: "idx/a-0", StartUnix: 10, EndUnix: 20},
			{Tenant: "tenantA", Path: "idx/a-0", StartUnix: 10, EndUnix: 20},
			{Tenant: "tenantA", Path: "idx/a-1", StartUnix: 30, EndUnix: 40},
		})
		tocPath := TableOfContentsPath("tenantA", window)
		before := readRawToC(ctx, t, bucket, tocPath)

		writer := newTableOfContentsWriter(t, bucket)
		swapped, err := writer.ReplaceIndexPointers(ctx, window, "tenantA",
			[]string{"idx/a-0", "idx/a-gone"},
			[]TableOfContentsEntry{{Path: "idx/a-new", StartTime: unixTime(10), EndTime: unixTime(20)}},
		)
		require.ErrorIs(t, err, errChangeSuperseded)
		require.False(t, swapped)
		require.Equal(t, before, readRawToC(ctx, t, bucket, tocPath))
		require.Equal(t, map[changeResult]uint64{changeWritten: 0, changeAlreadyApplied: 0, changeSuperseded: 1, changeFailed: 0, changeConflict: 0}, sampleCounts(t, writer.metrics.writeSeconds, opReplace))
	})

	t.Run("removes every pointer of an old path that the ToC holds more than once and applies the swap when the ToC holds every old path", func(t *testing.T) {
		ctx := context.Background()
		window := unixTime(0)
		bucket := objstore.NewInMemBucket()
		seedToC(t, bucket, window, []tocRow{
			{Tenant: "tenantA", Path: "idx/a-0", StartUnix: 10, EndUnix: 20},
			{Tenant: "tenantA", Path: "idx/a-0", StartUnix: 10, EndUnix: 20},
			{Tenant: "tenantA", Path: "idx/a-1", StartUnix: 30, EndUnix: 40},
			{Tenant: "tenantA", Path: "idx/a-2", StartUnix: 50, EndUnix: 60},
		})

		writer := newTableOfContentsWriter(t, bucket)
		swapped, err := writer.ReplaceIndexPointers(ctx, window, "tenantA",
			[]string{"idx/a-0", "idx/a-1"},
			[]TableOfContentsEntry{{Path: "idx/a-new", StartTime: unixTime(10), EndTime: unixTime(40)}},
		)
		require.NoError(t, err)
		require.True(t, swapped)
		require.Equal(t, []tocRow{
			{Tenant: "tenantA", Path: "idx/a-2", StartUnix: 50, EndUnix: 60},
			{Tenant: "tenantA", Path: "idx/a-new", StartUnix: 10, EndUnix: 40},
		}, readWindowToCs(ctx, t, bucket, window))
		require.Equal(t, map[changeResult]uint64{changeWritten: 1, changeAlreadyApplied: 0, changeSuperseded: 0, changeFailed: 0, changeConflict: 0}, sampleCounts(t, writer.metrics.writeSeconds, opReplace))
	})

	t.Run("drops the repeated pointers of a kept path when it rewrites the ToC", func(t *testing.T) {
		ctx := context.Background()
		window := unixTime(0)
		bucket := objstore.NewInMemBucket()
		seedToC(t, bucket, window, []tocRow{
			{Tenant: "tenantA", Path: "idx/a-0", StartUnix: 10, EndUnix: 20},
			{Tenant: "tenantA", Path: "idx/a-1", StartUnix: 30, EndUnix: 40},
			{Tenant: "tenantA", Path: "idx/a-1", StartUnix: 30, EndUnix: 40},
		})

		writer := newTableOfContentsWriter(t, bucket)
		swapped, err := writer.ReplaceIndexPointers(ctx, window, "tenantA",
			[]string{"idx/a-0"},
			[]TableOfContentsEntry{{Path: "idx/a-new", StartTime: unixTime(10), EndTime: unixTime(20)}},
		)
		require.NoError(t, err)
		require.True(t, swapped)
		require.Equal(t, []tocRow{
			{Tenant: "tenantA", Path: "idx/a-1", StartUnix: 30, EndUnix: 40},
			{Tenant: "tenantA", Path: "idx/a-new", StartUnix: 10, EndUnix: 20},
		}, readWindowToCs(ctx, t, bucket, window))
	})

	t.Run("adds a new entry once when the ToC already holds its path", func(t *testing.T) {
		ctx := context.Background()
		window := unixTime(0)
		bucket := objstore.NewInMemBucket()
		seedToC(t, bucket, window, []tocRow{
			{Tenant: "tenantA", Path: "idx/a-0", StartUnix: 10, EndUnix: 20},
			{Tenant: "tenantA", Path: "idx/a-new", StartUnix: 10, EndUnix: 20},
		})

		writer := newTableOfContentsWriter(t, bucket)
		swapped, err := writer.ReplaceIndexPointers(ctx, window, "tenantA",
			[]string{"idx/a-0"},
			[]TableOfContentsEntry{{Path: "idx/a-new", StartTime: unixTime(10), EndTime: unixTime(20)}},
		)
		require.NoError(t, err)
		require.True(t, swapped)
		require.Equal(t, []tocRow{{Tenant: "tenantA", Path: "idx/a-new", StartUnix: 10, EndUnix: 20}}, readWindowToCs(ctx, t, bucket, window))
	})

	t.Run("adds a new entry once when newEntries repeats a path that the ToC does not hold", func(t *testing.T) {
		ctx := context.Background()
		window := unixTime(0)
		bucket := objstore.NewInMemBucket()
		seedToC(t, bucket, window, []tocRow{{Tenant: "tenantA", Path: "idx/a-0", StartUnix: 10, EndUnix: 20}})

		writer := newTableOfContentsWriter(t, bucket)
		swapped, err := writer.ReplaceIndexPointers(ctx, window, "tenantA",
			[]string{"idx/a-0"},
			[]TableOfContentsEntry{
				{Path: "idx/a-new", StartTime: unixTime(10), EndTime: unixTime(20)},
				{Path: "idx/a-new", StartTime: unixTime(10), EndTime: unixTime(20)},
			},
		)
		require.NoError(t, err)
		require.True(t, swapped)
		require.Equal(t, []tocRow{{Tenant: "tenantA", Path: "idx/a-new", StartUnix: 10, EndUnix: 20}}, readWindowToCs(ctx, t, bucket, window))
	})

	t.Run("returns an error, leaves the ToC unchanged and reports superseded when the ToC holds none of the old paths", func(t *testing.T) {
		ctx := context.Background()
		window := unixTime(0)
		bucket := objstore.NewInMemBucket()
		seedToC(t, bucket, window, []tocRow{
			{Tenant: "tenantA", Path: "idx/a-already-rolled-up", StartUnix: 10, EndUnix: 60},
			{Tenant: "tenantB", Path: "idx/b-0", StartUnix: 11, EndUnix: 21},
		})
		before := readWindowToCs(ctx, t, bucket, window)

		writer := newTableOfContentsWriter(t, bucket)
		swapped, err := writer.ReplaceIndexPointers(ctx, window, "tenantA",
			[]string{"idx/a-0", "idx/a-1"},
			[]TableOfContentsEntry{{Path: "idx/a-new", StartTime: unixTime(10), EndTime: unixTime(60)}},
		)
		require.ErrorIs(t, err, errChangeSuperseded)
		require.False(t, swapped)
		require.Equal(t, before, readWindowToCs(ctx, t, bucket, window))
		require.Equal(t, map[changeResult]uint64{changeWritten: 0, changeAlreadyApplied: 0, changeSuperseded: 1, changeFailed: 0, changeConflict: 0}, sampleCounts(t, writer.metrics.writeSeconds, opReplace))
	})

	t.Run("returns false, leaves the ToC unchanged and reports already_applied when the ToC holds every new entry and none of the old paths", func(t *testing.T) {
		ctx := context.Background()
		window := unixTime(0)
		bucket := objstore.NewInMemBucket()
		seedToC(t, bucket, window, []tocRow{
			{Tenant: "tenantA", Path: "idx/a-kept", StartUnix: 30, EndUnix: 40},
			{Tenant: "tenantA", Path: "idx/a-new", StartUnix: 10, EndUnix: 20},
		})
		tocPath := TableOfContentsPath("tenantA", window)
		before := readRawToC(ctx, t, bucket, tocPath)

		writer := newTableOfContentsWriter(t, bucket)
		swapped, err := writer.ReplaceIndexPointers(ctx, window, "tenantA",
			[]string{"idx/a-0", "idx/a-1"},
			[]TableOfContentsEntry{{Path: "idx/a-new", StartTime: unixTime(10), EndTime: unixTime(20)}},
		)
		require.NoError(t, err)
		require.False(t, swapped)
		require.Equal(t, before, readRawToC(ctx, t, bucket, tocPath))
		require.Equal(t, map[changeResult]uint64{changeWritten: 0, changeAlreadyApplied: 1, changeSuperseded: 0, changeFailed: 0, changeConflict: 0}, sampleCounts(t, writer.metrics.writeSeconds, opReplace))
	})

	t.Run("returns an error and reports superseded when the ToC holds some new entries and none of the old paths", func(t *testing.T) {
		ctx := context.Background()
		window := unixTime(0)
		bucket := objstore.NewInMemBucket()
		seedToC(t, bucket, window, []tocRow{{Tenant: "tenantA", Path: "idx/a-new", StartUnix: 10, EndUnix: 20}})
		before := readWindowToCs(ctx, t, bucket, window)

		writer := newTableOfContentsWriter(t, bucket)
		swapped, err := writer.ReplaceIndexPointers(ctx, window, "tenantA",
			[]string{"idx/a-0"},
			[]TableOfContentsEntry{
				{Path: "idx/a-new", StartTime: unixTime(10), EndTime: unixTime(20)},
				{Path: "idx/a-other", StartTime: unixTime(10), EndTime: unixTime(20)},
			},
		)
		require.ErrorIs(t, err, errChangeSuperseded)
		require.False(t, swapped)
		require.Equal(t, before, readWindowToCs(ctx, t, bucket, window))
		require.Equal(t, map[changeResult]uint64{changeWritten: 0, changeAlreadyApplied: 0, changeSuperseded: 1, changeFailed: 0, changeConflict: 0}, sampleCounts(t, writer.metrics.writeSeconds, opReplace))
	})

	t.Run("returns an error, creates no ToC and reports superseded when the ToC does not exist", func(t *testing.T) {
		ctx := context.Background()
		window := unixTime(0)
		bucket := objstore.NewInMemBucket()

		writer := newTableOfContentsWriter(t, bucket)
		swapped, err := writer.ReplaceIndexPointers(ctx, window, "tenantA",
			[]string{"idx/a-0"},
			[]TableOfContentsEntry{{Path: "idx/a-new", StartTime: unixTime(10), EndTime: unixTime(20)}},
		)
		require.ErrorIs(t, err, errChangeSuperseded)
		require.False(t, swapped)

		exists, err := bucket.Exists(ctx, TableOfContentsPath("tenantA", window))
		require.NoError(t, err)
		require.False(t, exists)
		require.Equal(t, map[changeResult]uint64{changeWritten: 0, changeAlreadyApplied: 0, changeSuperseded: 1, changeFailed: 0, changeConflict: 0}, sampleCounts(t, writer.metrics.writeSeconds, opReplace))
	})

	t.Run("applies the swap once and returns false when the conditional write lands and then returns an error", func(t *testing.T) {
		ctx := context.Background()
		window := unixTime(0)
		inner := objstore.NewInMemBucket()
		seedToC(t, inner, window, []tocRow{{Tenant: "tenantA", Path: "idx/a-0", StartUnix: 10, EndUnix: 20}})

		bucket := &failAfterWriteBucket{Bucket: inner}
		writer := newTableOfContentsWriter(t, bucket)
		swapped, err := writer.ReplaceIndexPointers(ctx, window, "tenantA",
			[]string{"idx/a-0"},
			[]TableOfContentsEntry{{Path: "idx/a-new", StartTime: unixTime(10), EndTime: unixTime(20)}},
		)
		require.NoError(t, err)
		require.False(t, swapped)
		require.Equal(t, 2, bucket.calls)
		require.Equal(t, []tocRow{{Tenant: "tenantA", Path: "idx/a-new", StartUnix: 10, EndUnix: 20}}, readWindowToCs(ctx, t, inner, window))

		require.Equal(t, map[changeResult]uint64{changeWritten: 0, changeAlreadyApplied: 1, changeSuperseded: 0, changeFailed: 1, changeConflict: 0}, sampleCounts(t, writer.metrics.writeAttemptSeconds, opReplace))
		require.Equal(t, map[changeResult]uint64{changeWritten: 0, changeAlreadyApplied: 1, changeSuperseded: 0, changeFailed: 0, changeConflict: 0}, sampleCounts(t, writer.metrics.writeSeconds, opReplace))
	})

	t.Run("retries and applies the swap when the first conditional write conflicts", func(t *testing.T) {
		ctx := context.Background()
		window := unixTime(0)
		inner := objstore.NewInMemBucket()
		seedToC(t, inner, window, []tocRow{
			{Tenant: "tenantA", Path: "idx/a-0", StartUnix: 10, EndUnix: 20},
			{Tenant: "tenantB", Path: "idx/b-0", StartUnix: 11, EndUnix: 21},
		})

		bucket := &conflictBucket{Bucket: inner}
		writer := newTableOfContentsWriter(t, bucket)
		swapped, err := writer.ReplaceIndexPointers(ctx, window, "tenantA",
			[]string{"idx/a-0"},
			[]TableOfContentsEntry{{Path: "idx/a-new", StartTime: unixTime(100), EndTime: unixTime(110)}},
		)
		require.NoError(t, err)
		require.True(t, swapped)
		require.Equal(t, 2, bucket.calls)
		require.Equal(t, []tocRow{
			{Tenant: "tenantA", Path: "idx/a-new", StartUnix: 100, EndUnix: 110},
			{Tenant: "tenantB", Path: "idx/b-0", StartUnix: 11, EndUnix: 21},
		}, readWindowToCs(ctx, t, inner, window))
		require.Equal(t, map[changeResult]uint64{changeWritten: 1, changeAlreadyApplied: 0, changeSuperseded: 0, changeFailed: 0, changeConflict: 1}, sampleCounts(t, writer.metrics.writeAttemptSeconds, opReplace))
	})

	t.Run("returns an error after the last attempt when every write fails", func(t *testing.T) {
		ctx := context.Background()
		window := unixTime(0)
		inner := objstore.NewInMemBucket()
		seedToC(t, inner, window, []tocRow{{Tenant: "tenantA", Path: "idx/a-0", StartUnix: 10, EndUnix: 20}})

		bucket := &failingBucket{Bucket: inner}
		writer := newTableOfContentsWriterWithBackoff(t, bucket, 3)
		swapped, err := writer.ReplaceIndexPointers(ctx, window, "tenantA",
			[]string{"idx/a-0"},
			[]TableOfContentsEntry{{Path: "idx/a-new", StartTime: unixTime(10), EndTime: unixTime(20)}},
		)
		require.ErrorIs(t, err, errWriteFailed)
		require.False(t, swapped)
		require.Equal(t, 3, bucket.Calls())
	})

	t.Run("returns an error without retrying and leaves the ToC unchanged when the ToC holds a section of another tenant", func(t *testing.T) {
		ctx := context.Background()
		window := unixTime(0)
		inner := objstore.NewInMemBucket()
		tocPath := TableOfContentsPath("tenantA", window)
		uploadToC(t, inner, tocPath, "tenantB", "idx/a-0")
		before := readToC(ctx, t, inner, tocPath)

		bucket := &countingBucket{Bucket: inner}
		writer := newTableOfContentsWriter(t, bucket)
		swapped, err := writer.ReplaceIndexPointers(ctx, window, "tenantA",
			[]string{"idx/a-0"},
			[]TableOfContentsEntry{{Path: "idx/a-new", StartTime: unixTime(10), EndTime: unixTime(20)}},
		)
		require.ErrorIs(t, err, errUnrecoverable)
		require.False(t, swapped)
		require.Equal(t, 1, bucket.Calls())
		require.Equal(t, before, readToC(ctx, t, inner, tocPath))
	})

	for _, tc := range []struct {
		name       string
		oldPaths   []string
		newEntries []TableOfContentsEntry
		wantErr    string
	}{
		{
			name: "returns false without touching storage when oldPaths and newEntries are nil",
		},
		{
			name:       "returns false without touching storage when oldPaths and newEntries are empty",
			oldPaths:   []string{},
			newEntries: []TableOfContentsEntry{},
		},
		{
			name:       "returns an error without touching storage when oldPaths is empty",
			newEntries: []TableOfContentsEntry{{Path: "idx/a-new", StartTime: unixTime(100), EndTime: unixTime(110)}},
			wantErr:    "no old entries",
		},
		{
			name:     "returns an error without touching storage when newEntries is empty",
			oldPaths: []string{"idx/a-old"},
			wantErr:  "no new entries",
		},
		{
			name:       "returns the TimeWindow error without touching storage when a new entry is not valid",
			oldPaths:   []string{"idx/a-0"},
			newEntries: []TableOfContentsEntry{{Path: "idx/a-new", StartTime: unixTime(20), EndTime: unixTime(10)}},
			wantErr:    "ends at",
		},
		{
			name:     "returns an error without touching storage when a new entry is in the next window",
			oldPaths: []string{"idx/a-0"},
			newEntries: []TableOfContentsEntry{{
				Path:      "idx/a-new",
				StartTime: unixTime(0).Add(MetastoreWindowSize),
				EndTime:   unixTime(0).Add(MetastoreWindowSize + time.Hour),
			}},
			wantErr: "want window",
		},
		{
			name:     "returns an error without touching storage when a valid entry and an entry of another window are new",
			oldPaths: []string{"idx/a-0"},
			newEntries: []TableOfContentsEntry{
				{Path: "idx/a-new", StartTime: unixTime(10), EndTime: unixTime(20)},
				{Path: "idx/b-new", StartTime: unixTime(0).Add(MetastoreWindowSize), EndTime: unixTime(0).Add(MetastoreWindowSize + time.Hour)},
			},
			wantErr: "idx/b-new",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bucket := &failingBucket{Bucket: objstore.NewInMemBucket()}
			writer := newTableOfContentsWriter(t, bucket)

			swapped, err := writer.ReplaceIndexPointers(context.Background(), unixTime(0), "tenantA", tc.oldPaths, tc.newEntries)
			if tc.wantErr == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, tc.wantErr)
			}
			require.False(t, swapped)
			require.Zero(t, bucket.Calls())
		})
	}

	t.Run("applies the swap when the new entry lies in a window that the call names with an unaligned time", func(t *testing.T) {
		window := unixTime(0).Add(MetastoreWindowSize)
		bucket := objstore.NewInMemBucket()
		seedToC(t, bucket, window, []tocRow{{Tenant: "tenantA", Path: "idx/a-0", StartUnix: window.Unix(), EndUnix: window.Unix()}})
		writer := newTableOfContentsWriter(t, bucket)

		swapped, err := writer.ReplaceIndexPointers(context.Background(), window.Add(time.Hour), "tenantA",
			[]string{"idx/a-0"},
			[]TableOfContentsEntry{{Path: "idx/a-new", StartTime: window.Add(time.Hour), EndTime: window.Add(2 * time.Hour)}},
		)
		require.NoError(t, err)
		require.True(t, swapped)
		require.Equal(t, []tocRow{{Tenant: "tenantA", Path: "idx/a-new", StartUnix: window.Add(time.Hour).Unix(), EndUnix: window.Add(2 * time.Hour).Unix()}},
			readWindowToCs(context.Background(), t, bucket, window))
	})
}

// writeTimeRanges records path in the ToC of every tenant in timeRanges.
//
// WriteEntry accepts an entry of one window only, so writeTimeRanges writes
// one entry for each window that a time range overlaps, with the time range
// cut to that window. The dataobj-builder does the same: it splits objects by
// window before it indexes them.
func writeTimeRanges(ctx context.Context, w *TableOfContentsWriter, path string, timeRanges []dataobj.TimeRange) error {
	for _, tr := range timeRanges {
		for window := tr.MinTime.UTC().Truncate(MetastoreWindowSize); !window.After(tr.MaxTime); window = window.Add(MetastoreWindowSize) {
			start, end := tr.MinTime, tr.MaxTime
			if start.Before(window) {
				start = window
			}
			if lastInstant := window.Add(MetastoreWindowSize - time.Nanosecond); end.After(lastInstant) {
				end = lastInstant
			}
			if err := w.WriteEntry(ctx, tr.Tenant, TableOfContentsEntry{
				Path:      path,
				StartTime: start,
				EndTime:   end,
			}); err != nil {
				return err
			}
		}
	}
	return nil
}

var errWriteFailed = errors.New("write failed")

// countingBucket counts GetAndReplace calls and passes them through.
type countingBucket struct {
	objstore.Bucket
	mu    sync.Mutex
	calls int
}

func (b *countingBucket) GetAndReplace(ctx context.Context, name string, fn func(io.ReadCloser) (io.ReadCloser, error)) error {
	b.mu.Lock()
	b.calls++
	b.mu.Unlock()
	return b.Bucket.GetAndReplace(ctx, name, fn)
}

func (b *countingBucket) Calls() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.calls
}

// failingBucket returns err from every GetAndReplace call without a write, or
// errWriteFailed if err is nil.
type failingBucket struct {
	objstore.Bucket
	err   error
	mu    sync.Mutex
	calls int
}

func (b *failingBucket) GetAndReplace(context.Context, string, func(io.ReadCloser) (io.ReadCloser, error)) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.calls++
	if b.err != nil {
		return b.err
	}
	return errWriteFailed
}

func (b *failingBucket) Calls() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.calls
}

// failAfterWriteBucket simulates a conditional write whose response is lost.
// On the first GetAndReplace call it writes the object and then returns an
// error. Later calls pass through. calls counts the GetAndReplace calls.
type failAfterWriteBucket struct {
	objstore.Bucket
	calls int
}

func (b *failAfterWriteBucket) GetAndReplace(ctx context.Context, name string, fn func(io.ReadCloser) (io.ReadCloser, error)) error {
	b.calls++
	err := b.Bucket.GetAndReplace(ctx, name, fn)
	if err == nil && b.calls == 1 {
		return errors.New("response lost after the write")
	}
	return err
}

// errConditionalWriteConflict is the error that the S3 bucket returns when a
// conditional write loses to another writer.
var errConditionalWriteConflict = pkgerrors.Wrap(minio.ErrorResponse{Code: "PreconditionFailed", StatusCode: http.StatusPreconditionFailed}, "upload s3 object")

// conflictBucket simulates a conditional write that loses to another writer.
// On the first GetAndReplace call it runs concurrentWrite, if set, and returns
// errConditionalWriteConflict without a write. Later calls pass through. calls
// counts the GetAndReplace calls.
type conflictBucket struct {
	objstore.Bucket
	concurrentWrite func()
	calls           int
}

func (b *conflictBucket) GetAndReplace(ctx context.Context, name string, fn func(io.ReadCloser) (io.ReadCloser, error)) error {
	b.calls++
	if b.calls == 1 {
		if b.concurrentWrite != nil {
			b.concurrentWrite()
		}
		return errConditionalWriteConflict
	}
	return b.Bucket.GetAndReplace(ctx, name, fn)
}

// sampleCounts returns the number of observations of vec for op and each
// change result except changeCancelled.
func sampleCounts(t *testing.T, vec *prometheus.HistogramVec, op tocOp) map[changeResult]uint64 {
	t.Helper()

	counts := make(map[changeResult]uint64)
	for _, result := range []changeResult{changeWritten, changeAlreadyApplied, changeSuperseded, changeFailed, changeConflict} {
		counts[result] = sampleCount(t, vec, op, result)
	}
	return counts
}

// sampleCount returns the number of observations of vec for op and result.
func sampleCount(t *testing.T, vec *prometheus.HistogramVec, op tocOp, result changeResult) uint64 {
	t.Helper()

	var m dto.Metric
	require.NoError(t, vec.WithLabelValues(string(op), string(result)).(prometheus.Metric).Write(&m))
	return m.GetHistogram().GetSampleCount()
}

func newTableOfContentsWriter(t *testing.T, bucket objstore.Bucket) *TableOfContentsWriter {
	t.Helper()

	return NewTableOfContentsWriter(
		bucket,
		DefaultTocWriterBackoffConfig,
		DefaultTocBuilderConfig,
		log.NewNopLogger(),
		NewTocWriterMetrics(prometheus.NewPedanticRegistry()),
		indexobj.NewBuilderMetrics(nil),
	)
}

// newTableOfContentsWriterWithBackoff returns a writer that makes at most
// attempts attempts, with a short backoff to keep tests fast.
func newTableOfContentsWriterWithBackoff(t *testing.T, bucket objstore.Bucket, attempts int) *TableOfContentsWriter {
	t.Helper()

	return NewTableOfContentsWriter(
		bucket,
		backoff.Config{MinBackoff: time.Millisecond, MaxBackoff: time.Millisecond, MaxRetries: attempts},
		DefaultTocBuilderConfig,
		log.NewNopLogger(),
		NewTocWriterMetrics(prometheus.NewPedanticRegistry()),
		indexobj.NewBuilderMetrics(nil),
	)
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

// tocRow is a flattened (tenant, path, start, end) view of a ToC for assertion convenience.
type tocRow struct {
	Tenant    string
	Path      string
	StartUnix int64
	EndUnix   int64
}

// readRawToC returns the bytes of the ToC object at path.
func readRawToC(ctx context.Context, t *testing.T, bucket objstore.Bucket, path string) []byte {
	t.Helper()
	rc, err := bucket.Get(ctx, path)
	require.NoError(t, err)
	defer rc.Close()
	raw, err := io.ReadAll(rc)
	require.NoError(t, err)
	return raw
}

// readToC reads all index pointers from a ToC at the given path, flattened by
// tenant and sorted by tenant and path.
func readToC(ctx context.Context, t *testing.T, bucket objstore.Bucket, path string) []tocRow {
	t.Helper()
	raw := readRawToC(ctx, t, bucket, path)
	obj, err := dataobj.FromReaderAt(bytes.NewReader(raw), int64(len(raw)))
	require.NoError(t, err)

	var rows []tocRow
	var reader indexpointers.RowReader
	defer reader.Close()
	buf := make([]indexpointers.IndexPointer, 64)
	for _, section := range obj.Sections().Filter(indexpointers.CheckSection) {
		sec, err := indexpointers.Open(ctx, section)
		require.NoError(t, err)
		reader.Reset(sec)
		require.NoError(t, reader.Open(ctx))
		for {
			n, err := reader.Read(ctx, buf)
			for i := range n {
				rows = append(rows, tocRow{
					Tenant:    section.Tenant,
					Path:      buf[i].Path,
					StartUnix: buf[i].StartTs.UTC().Unix(),
					EndUnix:   buf[i].EndTs.UTC().Unix(),
				})
			}
			if err == io.EOF {
				break
			}
			require.NoError(t, err)
			if n == 0 {
				break
			}
		}
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Tenant != rows[j].Tenant {
			return rows[i].Tenant < rows[j].Tenant
		}
		return rows[i].Path < rows[j].Path
	})
	return rows
}

// readWindowToCs reads the ToCs of every tenant in the window, flattened by
// tenant.
func readWindowToCs(ctx context.Context, t *testing.T, bucket objstore.Bucket, window time.Time) []tocRow {
	t.Helper()
	tenants, err := ListTableOfContentsTenants(ctx, bucket, window)
	require.NoError(t, err)

	var rows []tocRow
	for _, tenant := range tenants {
		rows = append(rows, readToC(ctx, t, bucket, TableOfContentsPath(tenant, window))...)
	}
	return rows
}

// seedToC writes one ToC per tenant in the window with the given rows. It uses
// the builder and config of the production writer.
func seedToC(t *testing.T, bucket objstore.Bucket, window time.Time, rows []tocRow) {
	t.Helper()
	rowsByTenant := make(map[string][]tocRow)
	for _, r := range rows {
		rowsByTenant[r.Tenant] = append(rowsByTenant[r.Tenant], r)
	}

	for tenant, rows := range rowsByTenant {
		b, err := indexobj.NewBuilder(tenant, DefaultTocBuilderConfig, nil, indexobj.NewBuilderMetrics(nil))
		require.NoError(t, err)
		for _, r := range rows {
			require.NoError(t, b.AppendIndexPointer(indexpointers.IndexPointer{Path: r.Path, StartTs: time.Unix(r.StartUnix, 0).UTC(), EndTs: time.Unix(r.EndUnix, 0).UTC()}))
		}
		obj, closer, err := b.Flush()
		require.NoError(t, err)
		t.Cleanup(func() { _ = closer.Close() })
		reader, err := obj.Reader(t.Context())
		require.NoError(t, err)
		require.NoError(t, bucket.Upload(t.Context(), TableOfContentsPath(tenant, window), reader))
		require.NoError(t, reader.Close())
	}
}

// uploadToC writes a ToC to path that holds one section of tenant with the
// given index paths, each from 10 to 20 seconds after the Unix epoch. The
// tenant does not have to match the path.
func uploadToC(t *testing.T, bucket objstore.Bucket, path, tenant string, indexPaths ...string) {
	t.Helper()
	b, err := indexobj.NewBuilder(tenant, DefaultTocBuilderConfig, nil, indexobj.NewBuilderMetrics(nil))
	require.NoError(t, err)
	for _, indexPath := range indexPaths {
		require.NoError(t, b.AppendIndexPointer(indexpointers.IndexPointer{Path: indexPath, StartTs: unixTime(10), EndTs: unixTime(20)}))
	}
	obj, closer, err := b.Flush()
	require.NoError(t, err)
	t.Cleanup(func() { _ = closer.Close() })
	reader, err := obj.Reader(t.Context())
	require.NoError(t, err)
	require.NoError(t, bucket.Upload(t.Context(), path, reader))
	require.NoError(t, reader.Close())
}

// filterRows returns the rows of the given tenants.
func filterRows(rows []tocRow, tenants ...string) []tocRow {
	keep := make(map[string]struct{}, len(tenants))
	for _, t := range tenants {
		keep[t] = struct{}{}
	}
	out := make([]tocRow, 0, len(rows))
	for _, r := range rows {
		if _, ok := keep[r.Tenant]; ok {
			out = append(out, r)
		}
	}
	return out
}
