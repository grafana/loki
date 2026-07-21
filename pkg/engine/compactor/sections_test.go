package compactor

import (
	"bytes"
	"context"
	"io"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"

	v2 "github.com/grafana/loki/v3/pkg/dataobj/compaction/v2"
	compactionv2pb "github.com/grafana/loki/v3/pkg/dataobj/compaction/v2/proto"
	"github.com/grafana/loki/v3/pkg/dataobj/consumer/logsobj"
	"github.com/grafana/loki/v3/pkg/dataobj/index/indexobj"
	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/dataobj/metastore/multitenancy"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/stats"
)

// TestLoadTenantIndexes_GroupsByTenant verifies that loadTenantIndexes reads
// a multi-tenant ToC and returns each tenant's index references separately.
// This is the per-cycle planning input: the coordinator iterates the result
// map and skips tenants whose index slice has length ≤ 1 (convergence gate).
func TestLoadTenantIndexes_GroupsByTenant(t *testing.T) {
	ctx := context.Background()
	bucket := objstore.NewInMemBucket()
	window := time.Date(2026, 5, 14, 0, 0, 0, 0, time.UTC).Truncate(metastore.MetastoreWindowSize)

	writeToCWithIndexes(ctx, t, bucket, map[string][]testIndex{
		"tenant-a": {
			{path: "indexes/aa/idx-a-0", start: window.Add(1 * time.Hour), end: window.Add(2 * time.Hour)},
			{path: "indexes/bb/idx-a-1", start: window.Add(3 * time.Hour), end: window.Add(4 * time.Hour)},
		},
		"tenant-b": {
			{path: "indexes/cc/idx-b-0", start: window.Add(1 * time.Hour), end: window.Add(2 * time.Hour)},
		},
	})

	got, err := loadTenantIndexes(ctx, bucket, window)
	require.NoError(t, err)
	require.Len(t, got, 2, "two tenants written")

	require.Len(t, got["tenant-a"], 2)
	require.Len(t, got["tenant-b"], 1)

	requireIndexPaths(t, got["tenant-a"], "indexes/aa/idx-a-0", "indexes/bb/idx-a-1")
	requireIndexPaths(t, got["tenant-b"], "indexes/cc/idx-b-0")
}

// TestLoadTenantIndexes_MissingToCReturnsNotFound verifies the no-ToC case
// produces a bucket.IsObjNotFoundErr-class error. The coordinator treats
// this as "nothing to do this cycle" and waits for the next tick.
func TestLoadTenantIndexes_MissingToCReturnsNotFound(t *testing.T) {
	ctx := context.Background()
	bucket := objstore.NewInMemBucket()
	window := time.Date(2026, 5, 14, 0, 0, 0, 0, time.UTC).Truncate(metastore.MetastoreWindowSize)

	_, err := loadTenantIndexes(ctx, bucket, window)
	require.Error(t, err)
	require.True(t, bucket.IsObjNotFoundErr(err),
		"missing ToC must surface as IsObjNotFoundErr, got %v", err)
}

// TestSectionRefsFor_OneRefPerIndex verifies one SectionRef per index,
// timestamp-only bounds, empty MinKey/MaxKey. The planner patience-sorts on the
// composite (MinKey, MinTimestamp) key, so timestamp-only bounds remain a valid
// (less granular) sort key.
func TestSectionRefsFor_OneRefPerIndex(t *testing.T) {
	window := time.Date(2026, 5, 14, 0, 0, 0, 0, time.UTC)
	indexes := []indexEntry{
		{Path: "indexes/aa/idx-0", Start: window.Add(1 * time.Hour), End: window.Add(2 * time.Hour)},
		{Path: "indexes/bb/idx-1", Start: window.Add(3 * time.Hour), End: window.Add(5 * time.Hour)},
	}

	got := sectionRefsFor(indexes)
	require.Len(t, got, 2)

	require.Equal(t, "indexes/aa/idx-0", got[0].Ref.ObjectPath)
	require.Equal(t, int64(0), got[0].Ref.SectionIndex)
	require.Empty(t, got[0].Ref.MinKey, "timestamp-only bounds")
	require.Empty(t, got[0].Ref.MaxKey, "timestamp-only bounds")
	require.Equal(t, sortKey{timestamp: window.Add(1 * time.Hour).UnixNano()}, got[0].Min)
	require.Equal(t, sortKey{timestamp: window.Add(2 * time.Hour).UnixNano()}, got[0].Max)

	require.Equal(t, "indexes/bb/idx-1", got[1].Ref.ObjectPath)
	require.Equal(t, sortKey{timestamp: window.Add(3 * time.Hour).UnixNano()}, got[1].Min)
	require.Equal(t, sortKey{timestamp: window.Add(5 * time.Hour).UnixNano()}, got[1].Max)
}

// testIndex captures one index pointer entry (path, time range, sizes) to seed a ToC fixture.
type testIndex struct {
	path                 string
	start                time.Time
	end                  time.Time
	fileSize             uint64
	uncompressedLogsSize uint64
}

// writeToCWithIndexes writes a synthetic ToC containing one index pointer per
// (tenant, path) entry. Each entry's time range must fall inside the
// MetastoreWindowSize window the ToC covers (otherwise WriteEntry will route
// it to a different ToC file).
func writeToCWithIndexes(ctx context.Context, t *testing.T, bucket objstore.Bucket, entries map[string][]testIndex) {
	t.Helper()
	w := metastore.NewTableOfContentsWriter(bucket, log.NewNopLogger())
	for tenant, paths := range entries {
		for _, e := range paths {
			require.NoError(t, w.WriteEntry(ctx, e.path, []multitenancy.TimeRange{{
				Tenant:               tenant,
				MinTime:              e.start,
				MaxTime:              e.end,
				FileSize:             e.fileSize,
				UncompressedLogsSize: e.uncompressedLogsSize,
			}}))
		}
	}
}

// requireIndexPaths asserts the indexEntry slice contains exactly the supplied
// paths (set equality; order-independent because ToC iteration order is not a
// contract).
func requireIndexPaths(t *testing.T, got []indexEntry, want ...string) {
	t.Helper()
	gotPaths := make([]string, len(got))
	for i, e := range got {
		gotPaths[i] = e.Path
	}
	require.ElementsMatch(t, want, gotPaths)
}

// buildIndexWithStats writes an index object with one stats section for tenant
// containing the supplied rows, and uploads it at path. At least one row is
// required; an index with no sections cannot be flushed.
func buildIndexWithStats(ctx context.Context, t *testing.T, bucket objstore.Bucket, tenant, path string, rows []stats.Stat) {
	t.Helper()
	cfg := logsobj.BuilderBaseConfig{
		TargetPageSize:          2048,
		MaxPageRows:             10000,
		TargetObjectSize:        1 << 22,
		TargetSectionSize:       1 << 21,
		BufferSize:              2048 * 8,
		SectionStripeMergeLimit: 2,
	}
	b, err := indexobj.NewBuilder(cfg, nil)
	require.NoError(t, err)
	for _, r := range rows {
		require.NoError(t, b.AppendStat(
			tenant, r.ObjectPath, r.SectionIndex, r.SortSchema, r.Labels,
			time.Unix(0, r.MinTimestamp), time.Unix(0, r.MaxTimestamp),
			int(r.RowCount), r.UncompressedSize,
		))
	}
	obj, closer, err := b.Flush()
	require.NoError(t, err)
	defer closer.Close()

	reader, err := obj.Reader(ctx)
	require.NoError(t, err)
	defer reader.Close()
	objBytes, err := io.ReadAll(reader)
	require.NoError(t, err)
	require.NoError(t, bucket.Upload(ctx, path, io.NopCloser(bytes.NewReader(objBytes))))
}

func TestLogSectionRefsFor_AggregatesStatsRows(t *testing.T) {
	ctx := context.Background()
	bucket := objstore.NewInMemBucket()
	path := "indexes/aa/converged"

	buildIndexWithStats(ctx, t, bucket, "acme", path, []stats.Stat{
		{ObjectPath: "logs/log-0", SectionIndex: 0, SortSchema: "label:service_name",
			Labels: map[string]string{"service_name": "auth"}, MinTimestamp: 500, MaxTimestamp: 1000, RowCount: 3, UncompressedSize: 300},
		{ObjectPath: "logs/log-0", SectionIndex: 0, SortSchema: "label:service_name",
			Labels: map[string]string{"service_name": "billing"}, MinTimestamp: 100, MaxTimestamp: 900, RowCount: 2, UncompressedSize: 200},
	})

	refs, schema, err := logSectionRefsFor(ctx, bucket, "acme", path)
	require.NoError(t, err)
	require.Equal(t, []string{"label:service_name"}, schema)
	require.Equal(t, []v2.Section[sortKey]{
		{
			Ref: &compactionv2pb.SectionRef{
				ObjectPath:       "logs/log-0",
				SectionIndex:     0,
				MinKey:           []string{"auth"},
				MaxKey:           []string{"billing"},
				MinTimestamp:     100,
				MaxTimestamp:     1000,
				UncompressedSize: 500,
			},
			Min: sortKey{labels: []string{"auth"}, timestamp: 500},
			Max: sortKey{labels: []string{"billing"}, timestamp: 900},
		},
	}, refs)
}

func TestLogSectionRefsFor_MultiKeySchemaOrdersValuesAndReturnsFQN(t *testing.T) {
	ctx := context.Background()
	bucket := objstore.NewInMemBucket()
	path := "indexes/aa/multikey"

	buildIndexWithStats(ctx, t, bucket, "acme", path, []stats.Stat{
		{ObjectPath: "logs/log-0", SectionIndex: 0, SortSchema: "label:service_name,label:namespace",
			Labels: map[string]string{"service_name": "auth", "namespace": "eu"}, MinTimestamp: 10, MaxTimestamp: 20, RowCount: 1, UncompressedSize: 100},
	})

	refs, schema, err := logSectionRefsFor(ctx, bucket, "acme", path)
	require.NoError(t, err)
	require.Equal(t, []string{"label:service_name", "label:namespace"}, schema)
	require.Equal(t, []v2.Section[sortKey]{
		{
			Ref: &compactionv2pb.SectionRef{
				ObjectPath:       "logs/log-0",
				MinKey:           []string{"auth", "eu"},
				MaxKey:           []string{"auth", "eu"},
				MinTimestamp:     10,
				MaxTimestamp:     20,
				UncompressedSize: 100,
			},
			Min: sortKey{labels: []string{"auth", "eu"}, timestamp: 10},
			Max: sortKey{labels: []string{"auth", "eu"}, timestamp: 20},
		},
	}, refs)
}

func TestLogSectionRefsFor_EmptySortSchema(t *testing.T) {
	ctx := context.Background()
	bucket := objstore.NewInMemBucket()
	path := "indexes/aa/emptyschema"

	buildIndexWithStats(ctx, t, bucket, "acme", path, []stats.Stat{
		{ObjectPath: "logs/log-0", SectionIndex: 0, SortSchema: "",
			Labels: map[string]string{}, MinTimestamp: 10, MaxTimestamp: 20, RowCount: 1, UncompressedSize: 100},
	})

	refs, schema, err := logSectionRefsFor(ctx, bucket, "acme", path)
	require.NoError(t, err)
	require.Empty(t, schema, "empty sort_schema yields no schema keys (not a bogus label: entry)")
	require.Len(t, refs, 1)
	require.Equal(t, sortKey{timestamp: 10}, refs[0].Min)
	require.Equal(t, sortKey{timestamp: 20}, refs[0].Max)
	require.Equal(t, "logs/log-0", refs[0].Ref.ObjectPath)
	require.Equal(t, int64(100), refs[0].Ref.UncompressedSize)
}

func TestLogSectionRefsFor_RejectsMixedSchemas(t *testing.T) {
	ctx := context.Background()
	bucket := objstore.NewInMemBucket()
	path := "indexes/aa/mixed"

	buildIndexWithStats(ctx, t, bucket, "acme", path, []stats.Stat{
		{ObjectPath: "logs/log-0", SectionIndex: 0, SortSchema: "label:service_name",
			Labels: map[string]string{"service_name": "api"}, MinTimestamp: 10, MaxTimestamp: 20},
		{ObjectPath: "logs/log-1", SectionIndex: 0, SortSchema: "label:namespace",
			Labels: map[string]string{"namespace": "prod"}, MinTimestamp: 10, MaxTimestamp: 20},
	})

	_, _, err := logSectionRefsFor(ctx, bucket, "acme", path)
	require.ErrorContains(t, err, `contains log sort schemas "label:service_name" and "label:namespace"`)
}

// TestLoadTenantIndexes_PopulatesSizeColumns verifies that loadTenantIndexes
// populates indexEntry.FileSize and indexEntry.UncompressedLogsSize from a ToC
// whose rows were written with non-zero sizes.
func TestLoadTenantIndexes_PopulatesSizeColumns(t *testing.T) {
	ctx := context.Background()
	bucket := objstore.NewInMemBucket()
	window := time.Date(2026, 5, 14, 0, 0, 0, 0, time.UTC).Truncate(metastore.MetastoreWindowSize)

	writeToCWithIndexes(ctx, t, bucket, map[string][]testIndex{
		"tenant-a": {
			{path: "indexes/aa/idx-a-0", start: window.Add(1 * time.Hour), end: window.Add(2 * time.Hour),
				fileSize: 1024, uncompressedLogsSize: 2048},
			{path: "indexes/bb/idx-a-1", start: window.Add(3 * time.Hour), end: window.Add(4 * time.Hour),
				fileSize: 512, uncompressedLogsSize: 4096},
		},
	})

	got, err := loadTenantIndexes(ctx, bucket, window)
	require.NoError(t, err)
	require.Len(t, got["tenant-a"], 2)

	// Build map by path to avoid order-dependent assertions (ToC order is not a contract).
	byPath := make(map[string]indexEntry)
	for _, e := range got["tenant-a"] {
		byPath[e.Path] = e
	}

	idx0 := byPath["indexes/aa/idx-a-0"]
	require.Equal(t, uint64(1024), idx0.FileSize)
	require.Equal(t, uint64(2048), idx0.UncompressedLogsSize)

	idx1 := byPath["indexes/bb/idx-a-1"]
	require.Equal(t, uint64(512), idx1.FileSize)
	require.Equal(t, uint64(4096), idx1.UncompressedLogsSize)
}

// TestSectionRefsFor_CopiesUncompressedSize verifies that sectionRefsFor
// copies each entry's UncompressedLogsSize into the produced SectionRef.UncompressedSize.
func TestSectionRefsFor_CopiesUncompressedSize(t *testing.T) {
	window := time.Date(2026, 5, 14, 0, 0, 0, 0, time.UTC)
	indexes := []indexEntry{
		{Path: "indexes/aa/idx-0", Start: window.Add(1 * time.Hour), End: window.Add(2 * time.Hour),
			FileSize: 1024, UncompressedLogsSize: 2048},
		{Path: "indexes/bb/idx-1", Start: window.Add(3 * time.Hour), End: window.Add(5 * time.Hour),
			FileSize: 512, UncompressedLogsSize: 4096},
	}

	got := sectionRefsFor(indexes)
	require.Len(t, got, 2)

	require.Equal(t, int64(2048), got[0].Ref.UncompressedSize)
	require.Equal(t, int64(4096), got[1].Ref.UncompressedSize)
}
