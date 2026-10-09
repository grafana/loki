package metastore

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"slices"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/backoff"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/index/indexobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/indexpointers"
)

func TestTableOfContentsWriter(t *testing.T) {
	t.Run("WriteEntry appends the entry to an existing ToC", func(t *testing.T) {
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

	t.Run("copyFromExistingToc copies every pointer of a ToC that WriteEntry wrote", func(t *testing.T) {
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

		err = writer.copyFromExistingToc(context.Background(), builder, reader, TableOfContentsEntry{Path: "testdata/other.obj"})
		require.NoError(t, err)
	})

	t.Run("copyFromExistingToc returns an error when the ToC holds a row that starts at the Unix epoch", func(t *testing.T) {
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
		err = writer.copyFromExistingToc(context.Background(), target, reader, TableOfContentsEntry{Path: "indexes/b"})
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

	t.Run("WriteEntry writes each tenant's entry only to that tenant's ToC", func(t *testing.T) {
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

	for _, tc := range []struct {
		name    string
		entry   TableOfContentsEntry
		wantErr string
	}{
		{
			name:    "WriteEntry returns an error and writes nothing when the entry has no time range",
			entry:   TableOfContentsEntry{Path: "indexes/a"},
			wantErr: "indexes/a",
		},
		{
			name:    "WriteEntry returns an error and writes nothing when the entry starts at the Unix epoch",
			entry:   TableOfContentsEntry{Path: "indexes/a", StartTime: unixTime(0), EndTime: unixTime(10)},
			wantErr: "indexes/a",
		},
		{
			name:    "WriteEntry returns an error and writes nothing when the entry ends before it starts",
			entry:   TableOfContentsEntry{Path: "indexes/a", StartTime: unixTime(20), EndTime: unixTime(10)},
			wantErr: "indexes/a",
		},
		{
			name: "WriteEntry returns an error and writes nothing when the entry spans two windows",
			entry: TableOfContentsEntry{
				Path:      "indexes/a",
				StartTime: unixTime(10),
				EndTime:   unixTime(10).Add(MetastoreWindowSize),
			},
			wantErr: "spans more than one ToC window",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bucket := &countingBucket{Bucket: objstore.NewInMemBucket()}
			writer := newTableOfContentsWriter(t, bucket)

			err := writer.WriteEntry(context.Background(), "tenant-a", tc.entry)
			require.ErrorContains(t, err, tc.wantErr)
			require.Zero(t, bucket.calls)
		})
	}

	t.Run("WriteEntry leaves the ToC unchanged and counts a skip when the ToC already holds the entry's path", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		writer := newTableOfContentsWriter(t, bucket)
		entry := TableOfContentsEntry{Path: "indexes/a", StartTime: unixTime(10), EndTime: unixTime(20)}
		tocPath := TableOfContentsPath("tenant-a", unixTime(0))

		require.NoError(t, writer.WriteEntry(context.Background(), "tenant-a", entry))
		before := bucket.Objects()[tocPath]

		require.NoError(t, writer.WriteEntry(context.Background(), "tenant-a", entry))
		require.Equal(t, []tocRow{{Tenant: "tenant-a", Path: "indexes/a", StartUnix: 10, EndUnix: 20}}, readToC(context.Background(), t, bucket, tocPath))
		require.Equal(t, before, bucket.Objects()[tocPath], "WriteEntry must not rewrite the ToC")

		want := map[status]uint64{statusSuccess: 1, statusSkipped: 1, statusFailure: 0}
		require.Equal(t, want, sampleCounts(t, writer.metrics.writeEntryAttemptSeconds))
		require.Equal(t, want, sampleCounts(t, writer.metrics.writeEntryTotalSeconds))
	})

	t.Run("WriteEntry skips the entry when its path is the last of more than one read batch of pointers", func(t *testing.T) {
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

	t.Run("WriteEntry appends the entry once and counts a skip when GetAndReplace writes the ToC and then returns an error", func(t *testing.T) {
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

		require.Equal(t, map[status]uint64{statusSuccess: 0, statusSkipped: 1, statusFailure: 1}, sampleCounts(t, writer.metrics.writeEntryAttemptSeconds))
		require.Equal(t, map[status]uint64{statusSuccess: 0, statusSkipped: 1, statusFailure: 0}, sampleCounts(t, writer.metrics.writeEntryTotalSeconds))
	})

	t.Run("WriteEntry returns an error after the last retry when every write fails", func(t *testing.T) {
		bucket := &failingBucket{Bucket: objstore.NewInMemBucket()}
		writer := NewTableOfContentsWriter(bucket, backoff.Config{
			MinBackoff: time.Millisecond,
			MaxBackoff: time.Millisecond,
			MaxRetries: 3,
		}, DefaultTocBuilderConfig, log.NewNopLogger(), NewTocWriterMetrics(nil))

		err := writer.WriteEntry(context.Background(), "tenant-a", TableOfContentsEntry{
			Path:      "indexes/a",
			StartTime: unixTime(10),
			EndTime:   unixTime(20),
		})
		require.ErrorContains(t, err, "terminated after 3 retries")
		require.ErrorIs(t, err, errWriteFailed)
		require.Equal(t, 3, bucket.calls)

		require.Equal(t, map[status]uint64{statusSuccess: 0, statusSkipped: 0, statusFailure: 3}, sampleCounts(t, writer.metrics.writeEntryAttemptSeconds))
		require.Equal(t, map[status]uint64{statusSuccess: 0, statusSkipped: 0, statusFailure: 1}, sampleCounts(t, writer.metrics.writeEntryTotalSeconds))
	})

	for _, tc := range []struct {
		name  string
		paths []string
	}{
		{
			name:  "WriteEntry returns an error without retrying and leaves the ToC unchanged when the ToC holds a section of another tenant",
			paths: []string{"indexes/b"},
		},
		{
			name:  "WriteEntry returns an error without retrying and leaves the ToC unchanged when a section of another tenant holds the entry's path",
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

			// The timeout turns a regression that retries into a failure
			// instead of a slow test.
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
}

// writeTimeRanges records path in the ToC of every tenant in timeRanges.
//
// WriteEntry accepts an entry of one window only, so writeTimeRanges writes
// one entry for each window that a time range overlaps, with the time range
// cut to that window. The builder does the same, because it splits objects by
// window.
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

var errWriteFailed = errors.New("write failed")

// failingBucket returns errWriteFailed from every GetAndReplace call without
// a write. calls counts the GetAndReplace calls.
type failingBucket struct {
	objstore.Bucket
	calls int
}

func (b *failingBucket) GetAndReplace(context.Context, string, func(io.ReadCloser) (io.ReadCloser, error)) error {
	b.calls++
	return errWriteFailed
}

// sampleCounts returns the number of observations of vec for each status.
func sampleCounts(t *testing.T, vec *prometheus.HistogramVec) map[status]uint64 {
	t.Helper()

	counts := make(map[status]uint64)
	for _, s := range []status{statusSuccess, statusSkipped, statusFailure} {
		var m dto.Metric
		require.NoError(t, vec.WithLabelValues(string(s)).(prometheus.Metric).Write(&m))
		counts[s] = m.GetHistogram().GetSampleCount()
	}
	return counts
}

func newTableOfContentsWriter(t *testing.T, bucket objstore.Bucket) *TableOfContentsWriter {
	t.Helper()

	return NewTableOfContentsWriter(
		bucket,
		DefaultTocWriterBackoffConfig,
		DefaultTocBuilderConfig,
		log.NewNopLogger(),
		NewTocWriterMetrics(prometheus.NewPedanticRegistry()),
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
