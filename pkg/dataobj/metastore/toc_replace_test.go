package metastore

import (
	"bytes"
	"context"
	stderrors "errors"
	"io"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/backoff"
	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/index/indexobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/indexpointers"
)

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
			for i := 0; i < n; i++ {
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

// seedToC writes a ToC at the given window containing the supplied (tenant,path,start,end) rows.
// Uses the same indexobj.Builder + tocBuilderCfg path that the production writer uses.
func seedToC(t *testing.T, bucket objstore.Bucket, window time.Time, rows []tocRow) {
	t.Helper()
	b, err := indexobj.NewBuilder(tocBuilderCfg, nil)
	require.NoError(t, err)
	for _, r := range rows {
		require.NoError(t, b.AppendIndexPointer(
			r.Tenant, r.Path,
			time.Unix(r.StartUnix, 0).UTC(),
			time.Unix(r.EndUnix, 0).UTC(),
		))
	}
	obj, closer, err := b.Flush()
	require.NoError(t, err)
	t.Cleanup(func() { _ = closer.Close() })
	reader, err := obj.Reader(t.Context())
	require.NoError(t, err)
	defer reader.Close()
	require.NoError(t, bucket.Upload(t.Context(), TableOfContentsPath(window), reader))
}

func TestReplaceIndexPointers_RoundTrip(t *testing.T) {
	ctx := context.Background()
	window := unixTime(0)
	bucket := objstore.NewInMemBucket()

	seedToC(t, bucket, window, []tocRow{
		{"tenantA", "idx/a-0", 10, 20},
		{"tenantA", "idx/a-1", 30, 40},
		{"tenantB", "idx/b-0", 11, 21},
		{"tenantB", "idx/b-1", 31, 41},
	})

	writer := &TableOfContentsWriter{
		bucket:      bucket,
		metrics:     newTableOfContentsMetrics(),
		logger:      log.NewNopLogger(),
		builderOnce: sync.Once{},
	}

	swapped, err := writer.ReplaceIndexPointers(ctx, window, "tenantA",
		[]string{"idx/a-0", "idx/a-1"},
		[]TableOfContentsEntry{
			{Path: "idx/a-new", StartTime: unixTime(100), EndTime: unixTime(110)},
		},
	)
	require.NoError(t, err)
	require.True(t, swapped, "expected swap to apply")

	got := readToC(ctx, t, bucket, TableOfContentsPath(window))
	want := []tocRow{
		{"tenantA", "idx/a-new", 100, 110},
		{"tenantB", "idx/b-0", 11, 21},
		{"tenantB", "idx/b-1", 31, 41},
	}
	require.Equal(t, want, got)
}

func TestReplaceIndexPointers_MultiTenantPreservation(t *testing.T) {
	ctx := context.Background()
	window := unixTime(0)
	bucket := objstore.NewInMemBucket()

	// Three tenants, each with three rows at well-separated time ranges.
	seedRows := []tocRow{
		{"tenantA", "idx/a-0", 10, 20},
		{"tenantA", "idx/a-1", 30, 40},
		{"tenantA", "idx/a-2", 50, 60},
		{"tenantB", "idx/b-0", 11, 21},
		{"tenantB", "idx/b-1", 31, 41},
		{"tenantB", "idx/b-2", 51, 61},
		{"tenantC", "idx/c-0", 12, 22},
		{"tenantC", "idx/c-1", 32, 42},
		{"tenantC", "idx/c-2", 52, 62},
	}
	seedToC(t, bucket, window, seedRows)

	// Capture B and C rows from the pre-swap state to compare verbatim.
	preSwap := readToC(ctx, t, bucket, TableOfContentsPath(window))
	bcRowsBefore := filterRows(preSwap, "tenantB", "tenantC")

	writer := &TableOfContentsWriter{
		bucket:      bucket,
		metrics:     newTableOfContentsMetrics(),
		logger:      log.NewNopLogger(),
		builderOnce: sync.Once{},
	}

	swapped, err := writer.ReplaceIndexPointers(ctx, window, "tenantA",
		[]string{"idx/a-0", "idx/a-1", "idx/a-2"},
		[]TableOfContentsEntry{
			{Path: "idx/a-merged", StartTime: unixTime(10), EndTime: unixTime(60)},
		},
	)
	require.NoError(t, err)
	require.True(t, swapped, "expected tenantA swap to apply")

	postSwap := readToC(ctx, t, bucket, TableOfContentsPath(window))

	// 1. Tenant A is exactly the new single row.
	aAfter := filterRows(postSwap, "tenantA")
	require.Equal(t, []tocRow{
		{"tenantA", "idx/a-merged", 10, 60},
	}, aAfter)

	// 2. Tenants B and C are byte-equivalent (same rows, same time ranges) to pre-swap.
	bcRowsAfter := filterRows(postSwap, "tenantB", "tenantC")
	require.Equal(t, bcRowsBefore, bcRowsAfter,
		"non-target tenant rows must be preserved unchanged")
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
		{"tenantA", "idx/a-already-rolled-up", 10, 60}, // simulates "the other coordinator's swap already landed"
		{"tenantB", "idx/b-0", 11, 21},
	})
	preSwap := readToC(ctx, t, bucket, TableOfContentsPath(window))

	writer := &TableOfContentsWriter{
		bucket:      bucket,
		metrics:     newTableOfContentsMetrics(),
		logger:      log.NewNopLogger(),
		builderOnce: sync.Once{},
	}

	// Caller still believes "idx/a-0" / "idx/a-1" are present — they're not.
	swapped, err := writer.ReplaceIndexPointers(ctx, window, "tenantA",
		[]string{"idx/a-0", "idx/a-1"},
		[]TableOfContentsEntry{
			{Path: "idx/a-new", StartTime: unixTime(10), EndTime: unixTime(60)},
		},
	)
	require.NoError(t, err)
	require.False(t, swapped, "expected no-op when oldPaths are no longer present")

	postSwap := readToC(ctx, t, bucket, TableOfContentsPath(window))
	require.Equal(t, preSwap, postSwap, "ToC must be unchanged on race-loss")
}

func TestReplaceIndexPointers_EmptyOldPaths(t *testing.T) {
	ctx := context.Background()
	window := unixTime(0)
	bucket := objstore.NewInMemBucket()

	seedToC(t, bucket, window, []tocRow{
		{"tenantA", "idx/a-0", 10, 20},
		{"tenantB", "idx/b-0", 11, 21},
	})
	preSwap := readToC(ctx, t, bucket, TableOfContentsPath(window))

	writer := &TableOfContentsWriter{
		bucket:      bucket,
		metrics:     newTableOfContentsMetrics(),
		logger:      log.NewNopLogger(),
		builderOnce: sync.Once{},
	}

	// No removes, only adds — semantics: nothing matched, so no-op even though newEntries non-empty.
	swapped, err := writer.ReplaceIndexPointers(ctx, window, "tenantA",
		nil,
		[]TableOfContentsEntry{
			{Path: "idx/a-new", StartTime: unixTime(100), EndTime: unixTime(110)},
		},
	)
	require.NoError(t, err)
	require.False(t, swapped, "empty oldPaths must no-op (use WriteEntry for pure appends)")
	require.Equal(t, preSwap, readToC(ctx, t, bucket, TableOfContentsPath(window)))
}

func TestReplaceIndexPointers_MissingToC(t *testing.T) {
	ctx := context.Background()
	window := unixTime(0)
	bucket := objstore.NewInMemBucket()
	tocPath := TableOfContentsPath(window)

	writer := &TableOfContentsWriter{
		bucket:      bucket,
		metrics:     newTableOfContentsMetrics(),
		logger:      log.NewNopLogger(),
		builderOnce: sync.Once{},
	}

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
	gotSizedReader  bool
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

	return b.Bucket.GetAndReplace(ctx, name, func(existing io.ReadCloser) (io.ReadCloser, error) {
		rc, err := fn(existing)
		if err != nil || rc == nil {
			return rc, err
		}
		if _, sizeErr := objstore.TryToGetSize(rc); sizeErr == nil {
			b.mu.Lock()
			b.gotSizedReader = true
			b.mu.Unlock()
		}
		return rc, nil
	})
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
var errPreconditionFailed = stderrors.New("PreconditionFailed: simulated If-Match mismatch")

func TestReplaceIndexPointers_RetriesOnConditionalWriteFailure(t *testing.T) {
	ctx := context.Background()
	window := unixTime(0)
	inner := objstore.NewInMemBucket()

	seedToC(t, inner, window, []tocRow{
		{"tenantA", "idx/a-0", 10, 20},
		{"tenantB", "idx/b-0", 11, 21},
	})

	flaky := &flakyBucket{
		Bucket:          inner,
		remainingErrors: []error{errPreconditionFailed},
	}

	writer := &TableOfContentsWriter{
		bucket:      flaky,
		metrics:     newTableOfContentsMetrics(),
		logger:      log.NewNopLogger(),
		builderOnce: sync.Once{},
	}

	swapped, err := writer.ReplaceIndexPointers(ctx, window, "tenantA",
		[]string{"idx/a-0"},
		[]TableOfContentsEntry{
			{Path: "idx/a-new", StartTime: unixTime(100), EndTime: unixTime(110)},
		},
	)
	require.NoError(t, err)
	require.True(t, swapped)
	require.True(t, flaky.gotSizedReader, "callback must return a sized ReadCloser so If-Match survives on S3")

	got := readToC(ctx, t, inner, TableOfContentsPath(window))
	require.Equal(t, []tocRow{
		{"tenantA", "idx/a-new", 100, 110},
		{"tenantB", "idx/b-0", 11, 21},
	}, got)
}

func TestReplaceIndexPointers_RetryExhaustion(t *testing.T) {
	ctx := context.Background()
	window := unixTime(0)
	inner := objstore.NewInMemBucket()
	seedToC(t, inner, window, []tocRow{{"tenantA", "idx/a-0", 10, 20}})

	// Always fail. Build a wrapper that returns errPreconditionFailed every call.
	alwaysFail := &alwaysFailBucket{Bucket: inner}

	writer := &TableOfContentsWriter{
		bucket:      alwaysFail,
		metrics:     newTableOfContentsMetrics(),
		logger:      log.NewNopLogger(),
		builderOnce: sync.Once{},
	}

	// Use the same-package internal helper to override backoff to a tight budget,
	// keeping this test fast (<100ms) while still exercising the retry loop.
	tightBackoff := backoff.Config{
		MinBackoff: 1 * time.Millisecond,
		MaxBackoff: 5 * time.Millisecond,
		MaxRetries: 3,
	}
	swapped, err := writer.replaceIndexPointers(ctx, window, "tenantA",
		[]string{"idx/a-0"}, nil, tightBackoff,
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

func TestReplaceIndexPointers_EmptyOldPaths_BypassesStorage(t *testing.T) {
	ctx := context.Background()
	window := unixTime(0)
	bucket := &countingFailBucket{Bucket: objstore.NewInMemBucket()}

	writer := &TableOfContentsWriter{
		bucket:      bucket,
		metrics:     newTableOfContentsMetrics(),
		logger:      log.NewNopLogger(),
		builderOnce: sync.Once{},
	}

	// Even with a permanently-failing bucket, empty oldPaths must no-op
	// without touching storage. This is the deterministic-no-op contract.
	swapped, err := writer.ReplaceIndexPointers(ctx, window, "tenantA",
		nil,
		[]TableOfContentsEntry{
			{Path: "idx/a-new", StartTime: unixTime(100), EndTime: unixTime(110)},
		},
	)
	require.NoError(t, err)
	require.False(t, swapped)
	require.Equal(t, 0, bucket.calls, "empty oldPaths must bypass GetAndReplace entirely")

	// Same property for empty slice (not nil).
	swapped, err = writer.ReplaceIndexPointers(ctx, window, "tenantA",
		[]string{},
		[]TableOfContentsEntry{
			{Path: "idx/a-new", StartTime: unixTime(100), EndTime: unixTime(110)},
		},
	)
	require.NoError(t, err)
	require.False(t, swapped)
	require.Equal(t, 0, bucket.calls)
}

func TestReplaceIndexPointers_DeleteOnly(t *testing.T) {
	ctx := context.Background()
	window := unixTime(0)
	bucket := objstore.NewInMemBucket()

	seedToC(t, bucket, window, []tocRow{
		{"tenantA", "idx/a-0", 10, 20},
		{"tenantA", "idx/a-1", 30, 40},
		{"tenantB", "idx/b-0", 11, 21},
	})

	writer := &TableOfContentsWriter{
		bucket:      bucket,
		metrics:     newTableOfContentsMetrics(),
		logger:      log.NewNopLogger(),
		builderOnce: sync.Once{},
	}

	// Remove tenant A's pointers without adding anything. Valid use case for
	// callers that need to drop stale entries (e.g. retention).
	swapped, err := writer.ReplaceIndexPointers(ctx, window, "tenantA",
		[]string{"idx/a-0", "idx/a-1"},
		nil,
	)
	require.NoError(t, err)
	require.True(t, swapped, "delete-only swap must apply when oldPaths match")

	got := readToC(ctx, t, bucket, TableOfContentsPath(window))
	require.Equal(t, []tocRow{
		{"tenantB", "idx/b-0", 11, 21},
	}, got, "tenant A must be fully drained; tenant B preserved")
}
