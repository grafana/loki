package metastore

import (
	"bytes"
	"context"
	"io"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/go-kit/log"
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
func readToC(t *testing.T, ctx context.Context, bucket objstore.Bucket, path string) []tocRow {
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

	got := readToC(t, ctx, bucket, TableOfContentsPath(window))
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
	preSwap := readToC(t, ctx, bucket, TableOfContentsPath(window))
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

	postSwap := readToC(t, ctx, bucket, TableOfContentsPath(window))

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
