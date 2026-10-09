package metastore

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"slices"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/backoff"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/assert"
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

	t.Run("rebuildToc keeps every pointer of the existing ToC and adds the new entry", func(t *testing.T) {
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

	t.Run("rebuildToc returns an error when the ToC holds a row that starts at the Unix epoch", func(t *testing.T) {
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

	t.Run("WriteEntry leaves the ToC unchanged and reports already_present when the ToC already holds the entry's path", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		writer := newTableOfContentsWriter(t, bucket)
		entry := TableOfContentsEntry{Path: "indexes/a", StartTime: unixTime(10), EndTime: unixTime(20)}
		tocPath := TableOfContentsPath("tenant-a", unixTime(0))

		require.NoError(t, writer.WriteEntry(context.Background(), "tenant-a", entry))
		before := bucket.Objects()[tocPath]

		require.NoError(t, writer.WriteEntry(context.Background(), "tenant-a", entry))
		require.Equal(t, []tocRow{{Tenant: "tenant-a", Path: "indexes/a", StartUnix: 10, EndUnix: 20}}, readToC(context.Background(), t, bucket, tocPath))
		require.Equal(t, before, bucket.Objects()[tocPath], "WriteEntry must not rewrite the ToC")

		want := map[changeResult]uint64{changeWritten: 1, changePresent: 1, changeRaceLost: 0, changeFailed: 0}
		require.Equal(t, want, sampleCounts(t, writer.metrics.changeAttemptSeconds, opWriteEntry))
		require.Equal(t, want, sampleCounts(t, writer.metrics.changeTotalSeconds, opWriteEntry))
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

	t.Run("WriteEntry appends the entry once and reports already_present when GetAndReplace writes the ToC and then returns an error", func(t *testing.T) {
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

		require.Equal(t, map[changeResult]uint64{changeWritten: 0, changePresent: 1, changeRaceLost: 0, changeFailed: 1}, sampleCounts(t, writer.metrics.changeAttemptSeconds, opWriteEntry))
		require.Equal(t, map[changeResult]uint64{changeWritten: 0, changePresent: 1, changeRaceLost: 0, changeFailed: 0}, sampleCounts(t, writer.metrics.changeTotalSeconds, opWriteEntry))
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

		require.Equal(t, map[changeResult]uint64{changeWritten: 0, changePresent: 0, changeRaceLost: 0, changeFailed: 3}, sampleCounts(t, writer.metrics.changeAttemptSeconds, opWriteEntry))
		require.Equal(t, map[changeResult]uint64{changeWritten: 0, changePresent: 0, changeRaceLost: 0, changeFailed: 1}, sampleCounts(t, writer.metrics.changeTotalSeconds, opWriteEntry))
	})

	t.Run("WriteEntry writes every entry when tenants write concurrently", func(t *testing.T) {
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

// sampleCounts returns the number of observations of vec for op and each
// change result.
func sampleCounts(t *testing.T, vec *prometheus.HistogramVec, op string) map[changeResult]uint64 {
	t.Helper()

	counts := make(map[changeResult]uint64)
	for _, result := range []changeResult{changeWritten, changePresent, changeRaceLost, changeFailed} {
		var m dto.Metric
		require.NoError(t, vec.WithLabelValues(op, string(result)).(prometheus.Metric).Write(&m))
		counts[result] = m.GetHistogram().GetSampleCount()
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

// tocRow is a flattened (tenant, path, start, end) view of a ToC for assertion convenience.
type tocRow struct {
	Tenant    string
	Path      string
	StartUnix int64
	EndUnix   int64
}

// readToC reads all index pointers from a ToC at the given path, flattened by tenant.
func readToC(ctx context.Context, t *testing.T, bucket objstore.Bucket, path string) []tocRow {
	t.Helper()
	rc, err := bucket.Get(ctx, path)
	require.NoError(t, err)
	defer rc.Close()
	raw, err := io.ReadAll(rc)
	require.NoError(t, err)
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

// seedToC writes one ToC per tenant at the given window containing the
// supplied (tenant,path,start,end) rows. Uses the same indexobj.Builder +
// DefaultTocBuilderConfig path that the production writer uses.
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

func TestReplaceIndexPointers_RoundTrip(t *testing.T) {
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
		[]TableOfContentsEntry{
			{Path: "idx/a-new", StartTime: unixTime(100), EndTime: unixTime(110)},
		},
	)
	require.NoError(t, err)
	require.True(t, swapped, "expected swap to apply")

	got := readWindowToCs(ctx, t, bucket, window)
	want := []tocRow{
		{Tenant: "tenantA", Path: "idx/a-new", StartUnix: 100, EndUnix: 110},
		{Tenant: "tenantB", Path: "idx/b-0", StartUnix: 11, EndUnix: 21},
		{Tenant: "tenantB", Path: "idx/b-1", StartUnix: 31, EndUnix: 41},
	}
	require.Equal(t, want, got)
}

func TestReplaceIndexPointers_MultiTenantPreservation(t *testing.T) {
	tests := []struct {
		name           string
		seedRows       []tocRow
		targetTenant   string
		oldPaths       []string
		newEntries     []TableOfContentsEntry
		wantTargetRows []tocRow
		otherTenants   []string
	}{
		{
			// Disjoint per-tenant index paths: each tenant owns its own set
			// of idx/... paths. This is the L1 → L1 re-compaction shape.
			name: "disjoint indexes per tenant",
			seedRows: []tocRow{
				{Tenant: "tenantA", Path: "idx/a-0", StartUnix: 10, EndUnix: 20},
				{Tenant: "tenantA", Path: "idx/a-1", StartUnix: 30, EndUnix: 40},
				{Tenant: "tenantA", Path: "idx/a-2", StartUnix: 50, EndUnix: 60},
				{Tenant: "tenantB", Path: "idx/b-0", StartUnix: 11, EndUnix: 21},
				{Tenant: "tenantB", Path: "idx/b-1", StartUnix: 31, EndUnix: 41},
				{Tenant: "tenantB", Path: "idx/b-2", StartUnix: 51, EndUnix: 61},
				{Tenant: "tenantC", Path: "idx/c-0", StartUnix: 12, EndUnix: 22},
				{Tenant: "tenantC", Path: "idx/c-1", StartUnix: 32, EndUnix: 42},
				{Tenant: "tenantC", Path: "idx/c-2", StartUnix: 52, EndUnix: 62},
			},
			targetTenant: "tenantA",
			oldPaths:     []string{"idx/a-0", "idx/a-1", "idx/a-2"},
			newEntries: []TableOfContentsEntry{
				{Path: "idx/a-merged", StartTime: unixTime(10), EndTime: unixTime(60)},
			},
			wantTargetRows: []tocRow{{Tenant: "tenantA", Path: "idx/a-merged", StartUnix: 10, EndUnix: 60}},
			otherTenants:   []string{"tenantB", "tenantC"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			window := unixTime(0)
			bucket := objstore.NewInMemBucket()
			seedToC(t, bucket, window, tt.seedRows)

			// Capture other tenants' rows pre-swap so we can show their ToCs
			// are left untouched.
			preSwap := readWindowToCs(ctx, t, bucket, window)
			otherRowsBefore := filterRows(preSwap, tt.otherTenants...)

			writer := newTableOfContentsWriter(t, bucket)

			swapped, err := writer.ReplaceIndexPointers(ctx, window,
				tt.targetTenant, tt.oldPaths, tt.newEntries,
			)
			require.NoError(t, err)
			require.True(t, swapped, "expected %s swap to apply", tt.targetTenant)

			postSwap := readWindowToCs(ctx, t, bucket, window)

			// 1. Target tenant ends up with exactly the expected rows.
			targetAfter := filterRows(postSwap, tt.targetTenant)
			require.Equal(t, tt.wantTargetRows, targetAfter)

			// 2. Other tenants' rows are unchanged.
			otherRowsAfter := filterRows(postSwap, tt.otherTenants...)
			require.Equal(t, otherRowsBefore, otherRowsAfter,
				"non-target tenant rows must be preserved unchanged")
		})
	}
}

// uploadToC writes a ToC to path that holds one section of tenant with the
// given index paths. The tenant does not have to match the path.
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

// countingBucket counts GetAndReplace calls and passes them through.
type countingBucket struct {
	objstore.Bucket
	callsMu sync.Mutex
	calls   int
}

func (b *countingBucket) GetAndReplace(ctx context.Context, name string, fn func(io.ReadCloser) (io.ReadCloser, error)) error {
	b.callsMu.Lock()
	b.calls++
	b.callsMu.Unlock()
	return b.Bucket.GetAndReplace(ctx, name, fn)
}

func (b *countingBucket) Calls() int {
	b.callsMu.Lock()
	defer b.callsMu.Unlock()
	return b.calls
}

func TestReplaceIndexPointers(t *testing.T) {
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

	t.Run("returns an error without touching storage when a new entry ends before it starts", func(t *testing.T) {
		bucket := &countingBucket{Bucket: objstore.NewInMemBucket()}
		writer := newTableOfContentsWriter(t, bucket)

		swapped, err := writer.ReplaceIndexPointers(context.Background(), unixTime(0), "tenantA",
			[]string{"idx/a-0"},
			[]TableOfContentsEntry{{Path: "idx/a-new", StartTime: unixTime(20), EndTime: unixTime(10)}},
		)
		require.ErrorContains(t, err, "idx/a-new")
		require.False(t, swapped)
		require.Zero(t, bucket.Calls())
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
			[]TableOfContentsEntry{
				{Path: "idx/a-new", StartTime: unixTime(10), EndTime: unixTime(20)},
				{Path: "idx/a-new", StartTime: unixTime(10), EndTime: unixTime(20)},
			},
		)
		require.NoError(t, err)
		require.True(t, swapped)
		require.Equal(t, []tocRow{{Tenant: "tenantA", Path: "idx/a-new", StartUnix: 10, EndUnix: 20}}, readWindowToCs(ctx, t, bucket, window))
	})

	t.Run("drops the repeated pointers of a path when it rewrites the ToC", func(t *testing.T) {
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
		require.ElementsMatch(t, []tocRow{
			{Tenant: "tenantA", Path: "idx/a-1", StartUnix: 30, EndUnix: 40},
			{Tenant: "tenantA", Path: "idx/a-new", StartUnix: 10, EndUnix: 20},
		}, readWindowToCs(ctx, t, bucket, window))
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

		require.Equal(t, map[changeResult]uint64{changeWritten: 0, changePresent: 1, changeRaceLost: 0, changeFailed: 1}, sampleCounts(t, writer.metrics.changeAttemptSeconds, opReplace))
		require.Equal(t, map[changeResult]uint64{changeWritten: 0, changePresent: 1, changeRaceLost: 0, changeFailed: 0}, sampleCounts(t, writer.metrics.changeTotalSeconds, opReplace))
	})

	t.Run("returns an error without touching storage when a new entry does not overlap the window", func(t *testing.T) {
		bucket := &countingBucket{Bucket: objstore.NewInMemBucket()}
		writer := newTableOfContentsWriter(t, bucket)

		swapped, err := writer.ReplaceIndexPointers(context.Background(), unixTime(0), "tenantA",
			[]string{"idx/a-0"},
			[]TableOfContentsEntry{{
				Path:      "idx/a-new",
				StartTime: unixTime(0).Add(MetastoreWindowSize),
				EndTime:   unixTime(0).Add(MetastoreWindowSize + time.Hour),
			}},
		)
		require.ErrorContains(t, err, "does not overlap the window")
		require.False(t, swapped)
		require.Zero(t, bucket.Calls())
	})
}

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

func TestReplaceIndexPointers_RaceLossOldPathsAlreadyGone(t *testing.T) {
	ctx := context.Background()
	window := unixTime(0)
	bucket := objstore.NewInMemBucket()

	seedToC(t, bucket, window, []tocRow{
		{Tenant: "tenantA", Path: "idx/a-already-rolled-up", StartUnix: 10, EndUnix: 60}, // simulates "the other coordinator's swap already landed"
		{Tenant: "tenantB", Path: "idx/b-0", StartUnix: 11, EndUnix: 21},
	})
	preSwap := readWindowToCs(ctx, t, bucket, window)

	writer := newTableOfContentsWriter(t, bucket)

	// Caller still believes "idx/a-0" / "idx/a-1" are present — they're not.
	swapped, err := writer.ReplaceIndexPointers(ctx, window, "tenantA",
		[]string{"idx/a-0", "idx/a-1"},
		[]TableOfContentsEntry{
			{Path: "idx/a-new", StartTime: unixTime(10), EndTime: unixTime(60)},
		},
	)
	require.NoError(t, err)
	require.False(t, swapped, "expected no-op when oldPaths are no longer present")
	require.Equal(t, map[changeResult]uint64{changeWritten: 0, changePresent: 0, changeRaceLost: 1, changeFailed: 0}, sampleCounts(t, writer.metrics.changeTotalSeconds, opReplace))

	postSwap := readWindowToCs(ctx, t, bucket, window)
	require.Equal(t, preSwap, postSwap, "ToC must be unchanged on race-loss")
}

func TestReplaceIndexPointers_MissingToC(t *testing.T) {
	ctx := context.Background()
	window := unixTime(0)
	bucket := objstore.NewInMemBucket()
	tocPath := TableOfContentsPath("tenantA", window)

	writer := newTableOfContentsWriter(t, bucket)

	swapped, err := writer.ReplaceIndexPointers(ctx, window, "tenantA",
		[]string{"idx/a-0"},
		[]TableOfContentsEntry{
			{Path: "idx/a-new", StartTime: unixTime(10), EndTime: unixTime(20)},
		},
	)
	require.NoError(t, err)
	require.False(t, swapped, "missing ToC must no-op")

	// Verify the no-op did NOT materialize an empty object at tocPath.
	exists, err := bucket.Exists(ctx, tocPath)
	require.NoError(t, err)
	require.False(t, exists, "missing-ToC no-op must not create an empty ToC blob")
}

// flakyBucket wraps an objstore.Bucket and, on the first N GetAndReplace calls,
// returns the supplied error WITHOUT invoking the callback. Subsequent calls
// pass through. Used to simulate a 412 PreconditionFailed on the first attempt.
type flakyBucket struct {
	objstore.Bucket
	mu              sync.Mutex
	remainingErrors []error
}

func (b *flakyBucket) GetAndReplace(ctx context.Context, name string, fn func(io.ReadCloser) (io.ReadCloser, error)) error {
	b.mu.Lock()
	if len(b.remainingErrors) > 0 {
		err := b.remainingErrors[0]
		b.remainingErrors = b.remainingErrors[1:]
		b.mu.Unlock()
		return err
	}
	b.mu.Unlock()
	return b.Bucket.GetAndReplace(ctx, name, fn)
}

// alwaysFailBucket wraps an objstore.Bucket and returns errPreconditionFailed
// from every GetAndReplace call. Used to drive the retry-exhaustion test.
type alwaysFailBucket struct {
	objstore.Bucket
}

func (b *alwaysFailBucket) GetAndReplace(_ context.Context, _ string, _ func(io.ReadCloser) (io.ReadCloser, error)) error {
	return errPreconditionFailed
}

// errPreconditionFailed is a synthetic 412-shaped error used by the retry tests.
var errPreconditionFailed = errors.New("PreconditionFailed: simulated If-Match mismatch")

func TestReplaceIndexPointers_RetriesOnConditionalWriteFailure(t *testing.T) {
	ctx := context.Background()
	window := unixTime(0)
	inner := objstore.NewInMemBucket()

	seedToC(t, inner, window, []tocRow{
		{Tenant: "tenantA", Path: "idx/a-0", StartUnix: 10, EndUnix: 20},
		{Tenant: "tenantB", Path: "idx/b-0", StartUnix: 11, EndUnix: 21},
	})

	flaky := &flakyBucket{
		Bucket:          inner,
		remainingErrors: []error{errPreconditionFailed},
	}

	writer := newTableOfContentsWriter(t, flaky)

	swapped, err := writer.ReplaceIndexPointers(ctx, window, "tenantA",
		[]string{"idx/a-0"},
		[]TableOfContentsEntry{
			{Path: "idx/a-new", StartTime: unixTime(100), EndTime: unixTime(110)},
		},
	)
	require.NoError(t, err)
	require.True(t, swapped)

	got := readWindowToCs(ctx, t, inner, window)
	require.Equal(t, []tocRow{
		{Tenant: "tenantA", Path: "idx/a-new", StartUnix: 100, EndUnix: 110},
		{Tenant: "tenantB", Path: "idx/b-0", StartUnix: 11, EndUnix: 21},
	}, got)
}

func TestReplaceIndexPointers_RetryExhaustion(t *testing.T) {
	ctx := context.Background()
	window := unixTime(0)
	inner := objstore.NewInMemBucket()
	seedToC(t, inner, window, []tocRow{{Tenant: "tenantA", Path: "idx/a-0", StartUnix: 10, EndUnix: 20}})

	// Always fail. Build a wrapper that returns errPreconditionFailed every call.
	alwaysFail := &alwaysFailBucket{Bucket: inner}

	// A tight backoff keeps this test fast while it still runs the retry loop.
	writer := NewTableOfContentsWriter(alwaysFail, backoff.Config{
		MinBackoff: 1 * time.Millisecond,
		MaxBackoff: 5 * time.Millisecond,
		MaxRetries: 3,
	}, DefaultTocBuilderConfig, log.NewNopLogger(), NewTocWriterMetrics(nil))

	swapped, err := writer.ReplaceIndexPointers(ctx, window, "tenantA",
		[]string{"idx/a-0"},
		[]TableOfContentsEntry{{Path: "idx/a-new", StartTime: unixTime(10), EndTime: unixTime(20)}},
	)
	require.Error(t, err)
	require.ErrorIs(t, err, errPreconditionFailed)
	require.False(t, swapped)
}

// countingFailBucket wraps an objstore.Bucket and tracks the number of
// GetAndReplace calls. Used to prove the empty-oldPaths fast-path bypasses
// storage entirely.
type countingFailBucket struct {
	objstore.Bucket
	mu    sync.Mutex
	calls int
}

func (b *countingFailBucket) GetAndReplace(_ context.Context, _ string, _ func(io.ReadCloser) (io.ReadCloser, error)) error {
	b.mu.Lock()
	b.calls++
	b.mu.Unlock()
	return errPreconditionFailed
}

func TestReplaceIndexPointers_EmptyOldAndNewPaths_BypassesStorage(t *testing.T) {
	ctx := context.Background()
	window := unixTime(0)
	bucket := &countingFailBucket{Bucket: objstore.NewInMemBucket()}

	writer := newTableOfContentsWriter(t, bucket)

	// Even with a permanently-failing bucket, empty old and new paths must no-op
	// without touching storage. This is the deterministic-no-op contract.
	swapped, err := writer.ReplaceIndexPointers(ctx, window, "tenantA",
		nil,
		nil,
	)
	require.NoError(t, err)
	require.False(t, swapped)
	require.Equal(t, 0, bucket.calls, "empty oldPaths and newEntries must bypass GetAndReplace entirely")

	// Same property for empty slice (not nil).
	swapped, err = writer.ReplaceIndexPointers(ctx, window, "tenantA",
		[]string{},
		[]TableOfContentsEntry{},
	)
	require.NoError(t, err)
	require.False(t, swapped)
	require.Equal(t, 0, bucket.calls, "empty oldPaths and newEntries must bypass GetAndReplace entirely")
}

func TestReplaceIndexPointers_EmptyOldOrNewPaths_Errors(t *testing.T) {
	ctx := context.Background()
	window := unixTime(0)
	bucket := &countingFailBucket{Bucket: objstore.NewInMemBucket()}

	writer := newTableOfContentsWriter(t, bucket)

	// Empty old, non empty new => error without calling storage.
	swapped, err := writer.ReplaceIndexPointers(ctx, window, "tenantA",
		nil,
		[]TableOfContentsEntry{
			{Path: "idx/a-new", StartTime: unixTime(100), EndTime: unixTime(110)},
		},
	)
	require.Error(t, err)
	require.False(t, swapped)
	require.Equal(t, 0, bucket.calls, "must bypass GetAndReplace entirely")

	// Non-empty old, empty new => error without calling storage.
	swapped, err = writer.ReplaceIndexPointers(ctx, window, "tenantA",
		[]string{"idx/a-old"},
		nil,
	)
	require.Error(t, err)
	require.False(t, swapped)
	require.Equal(t, 0, bucket.calls, "must bypass GetAndReplace entirely")
}
