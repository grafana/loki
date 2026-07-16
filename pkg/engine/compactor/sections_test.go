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

	require.Equal(t, "indexes/aa/idx-0", got[0].ObjectPath)
	require.Equal(t, int64(0), got[0].SectionIndex)
	require.Empty(t, got[0].MinKey, "timestamp-only bounds")
	require.Empty(t, got[0].MaxKey, "timestamp-only bounds")
	require.Equal(t, window.Add(1*time.Hour).UnixNano(), got[0].MinTimestamp)
	require.Equal(t, window.Add(2*time.Hour).UnixNano(), got[0].MaxTimestamp)

	require.Equal(t, "indexes/bb/idx-1", got[1].ObjectPath)
	require.Equal(t, window.Add(3*time.Hour).UnixNano(), got[1].MinTimestamp)
	require.Equal(t, window.Add(5*time.Hour).UnixNano(), got[1].MaxTimestamp)
}

// testIndex captures one (path, time range) entry to seed a ToC fixture.
type testIndex struct {
	path  string
	start time.Time
	end   time.Time
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
				Tenant:  tenant,
				MinTime: e.start,
				MaxTime: e.end,
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

func TestLogSectionRefsFor_OneRefPerStatRow(t *testing.T) {
	ctx := context.Background()
	bucket := objstore.NewInMemBucket()
	path := "indexes/aa/converged"

	buildIndexWithStats(ctx, t, bucket, "acme", path, []stats.Stat{
		{ObjectPath: "logs/log-0", SectionIndex: 0, SortSchema: "label:service_name",
			Labels: map[string]string{"service_name": "auth"}, MinTimestamp: 10, MaxTimestamp: 20, RowCount: 3, UncompressedSize: 300},
		{ObjectPath: "logs/log-0", SectionIndex: 0, SortSchema: "label:service_name",
			Labels: map[string]string{"service_name": "billing"}, MinTimestamp: 15, MaxTimestamp: 25, RowCount: 2, UncompressedSize: 200},
	})

	refs, schema, err := logSectionRefsFor(ctx, bucket, "acme", path)
	require.NoError(t, err)
	require.Equal(t, []string{"label:service_name"}, schema)
	require.Len(t, refs, 2)

	byKey := map[string]*compactionv2pb.SectionRef{}
	for _, r := range refs {
		byKey[r.MinKey[0]] = r
	}

	auth := byKey["auth"]
	require.NotNil(t, auth)
	require.Equal(t, "logs/log-0", auth.ObjectPath)
	require.Equal(t, []string{"auth"}, auth.MinKey)
	require.Equal(t, []string{"auth"}, auth.MaxKey)
	require.Equal(t, int64(300), auth.UncompressedSize)

	require.Equal(t, int64(200), byKey["billing"].UncompressedSize)

	// MaxKey must be a distinct slice from MinKey, not a shared backing array.
	auth.MinKey[0] = "MUTATED"
	require.Equal(t, "auth", auth.MaxKey[0], "MaxKey must be a distinct slice from MinKey")
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
	require.Len(t, refs, 1)
	require.Equal(t, []string{"auth", "eu"}, refs[0].MinKey, "values ordered by schema, not map order")
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
	require.Empty(t, refs[0].MinKey, "no sort keys -> empty MinKey")
	require.Empty(t, refs[0].MaxKey)
	require.Equal(t, "logs/log-0", refs[0].ObjectPath)
	require.Equal(t, int64(100), refs[0].UncompressedSize)
}
