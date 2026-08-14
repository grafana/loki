package compactor

import (
	"bytes"
	"context"
	"io"
	"math"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/flagext"
	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/v3/pkg/dataobj"
	v2 "github.com/grafana/loki/v3/pkg/dataobj/compaction/v2"
	compactionv2pb "github.com/grafana/loki/v3/pkg/dataobj/compaction/v2/proto"
	"github.com/grafana/loki/v3/pkg/dataobj/consumer/logsobj"
	"github.com/grafana/loki/v3/pkg/dataobj/index/indexobj"
	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/dataobj/metastore/multitenancy"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/postings"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/stats"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
)

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

type testIndexObject struct {
	tenant, path string
	sectionSize  flagext.Bytes
	stats        []stats.Stat
	postings     []postings.Row
}

func buildIndexWithStats(ctx context.Context, t *testing.T, bucket objstore.Bucket, tenant, path string, rows []stats.Stat) {
	t.Helper()
	for i := range rows {
		schema, _, err := parseSortSchema(rows[i].SortSchema)
		require.NoError(t, err)
		rows[i].PhysicalSortLayout = (logs.SortLayout{
			SchemaLabels: schema,
			StreamOrder:  logs.StreamOrderStableHashV1,
			ShardCount:   32,
		}).ID()
	}
	buildIndex(ctx, t, bucket, testIndexObject{tenant: tenant, path: path, sectionSize: 1 << 21, stats: rows})
}

func buildIndexWithPostings(ctx context.Context, t *testing.T, bucket objstore.Bucket, tenant, path string, sectionSize flagext.Bytes, rows []postings.Row) {
	t.Helper()
	buildIndex(ctx, t, bucket, testIndexObject{tenant: tenant, path: path, sectionSize: sectionSize, postings: rows})
}

func buildIndex(ctx context.Context, t *testing.T, bucket objstore.Bucket, object testIndexObject) {
	t.Helper()
	cfg := logsobj.BuilderBaseConfig{
		TargetPageSize:          2048,
		MaxPageRows:             10000,
		TargetObjectSize:        1 << 22,
		TargetSectionSize:       object.sectionSize,
		BufferSize:              2048 * 8,
		SectionStripeMergeLimit: 2,
	}
	builder, err := indexobj.NewMergeBuilder(cfg, nil)
	require.NoError(t, err)
	for _, stat := range object.stats {
		require.NoError(t, builder.AppendStat(object.tenant, stat))
	}
	for _, row := range object.postings {
		switch row.Kind {
		case postings.KindBloom:
			require.NoError(t, builder.AppendPostingsBloomEntry(object.tenant, row.BloomEntry()))
		case postings.KindLabel:
			require.NoError(t, builder.AppendPostingsLabelEntry(object.tenant, row.LabelEntry()))
		default:
			t.Fatalf("unsupported posting kind %d", row.Kind)
		}
	}

	obj, closer, err := builder.Flush()
	require.NoError(t, err)
	defer closer.Close()

	reader, err := obj.Reader(ctx)
	require.NoError(t, err)
	defer reader.Close()
	objBytes, err := io.ReadAll(reader)
	require.NoError(t, err)
	require.NoError(t, bucket.Upload(ctx, object.path, io.NopCloser(bytes.NewReader(objBytes))))
}

func buildIndexWithPostingsSections(ctx context.Context, t *testing.T, bucket objstore.Bucket, tenant, path string, sections ...[]postings.Row) {
	t.Helper()
	builder := dataobj.NewBuilder(nil)
	for _, rows := range sections {
		postingsBuilder := postings.NewMergeBuilder(nil, 2048, 10000, math.MaxInt)
		postingsBuilder.SetTenant(tenant)
		for _, row := range rows {
			require.Equal(t, postings.KindLabel, row.Kind)
			require.NoError(t, postingsBuilder.AppendLabelEntry(row.LabelEntry()))
		}
		require.NoError(t, builder.Append(postingsBuilder))
	}

	obj, closer, err := builder.Flush()
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
			Labels: map[string]string{"service_name": "auth"}, ShardBucket: 2,
			MinTimestamp: 500, MaxTimestamp: 1000, RowCount: 3, UncompressedSize: 300},
		{ObjectPath: "logs/log-0", SectionIndex: 0, SortSchema: "label:service_name",
			Labels: map[string]string{"service_name": "billing"}, ShardBucket: 7,
			MinTimestamp: 100, MaxTimestamp: 900, RowCount: 2, UncompressedSize: 200},
	})

	refs, compatible, err := logSectionRefsFor(ctx, bucket, "acme", path, []string{"label:service_name"})
	require.NoError(t, err)
	require.True(t, compatible)
	require.Len(t, refs, 1)
	require.Equal(t, &compactionv2pb.SectionRef{
		ObjectPath:       "logs/log-0",
		SectionIndex:     0,
		MinKey:           []string{"auth"},
		MaxKey:           []string{"billing"},
		MinTimestamp:     100,
		MaxTimestamp:     1000,
		UncompressedSize: 500,
	}, refs[0].Ref)
	require.Equal(t, sortKey{shard: 2, labels: []string{"auth"}, timestamp: 1000}, refs[0].Min)
	require.Equal(t, sortKey{shard: 7, labels: []string{"billing"}, timestamp: 100}, refs[0].Max)
}

func TestLogSectionRefsFor_MultiKeySchemaOrdersValuesAndReturnsFQN(t *testing.T) {
	ctx := context.Background()
	bucket := objstore.NewInMemBucket()
	path := "indexes/aa/multikey"

	buildIndexWithStats(ctx, t, bucket, "acme", path, []stats.Stat{
		{ObjectPath: "logs/log-0", SectionIndex: 0, SortSchema: "label:service_name,label:namespace",
			Labels: map[string]string{"service_name": "auth", "namespace": "eu"}, MinTimestamp: 10, MaxTimestamp: 20, RowCount: 1, UncompressedSize: 100},
	})

	refs, compatible, err := logSectionRefsFor(ctx, bucket, "acme", path, []string{"label:service_name", "label:namespace"})
	require.NoError(t, err)
	require.True(t, compatible)
	require.Len(t, refs, 1)
	require.Equal(t, refs[0].Min.shard, refs[0].Max.shard)
	require.Equal(t, []string{"auth", "eu"}, refs[0].Min.labels)
	require.Equal(t, []string{"auth", "eu"}, refs[0].Max.labels)
}

func TestLogSectionRefsFor_EmptySortSchema(t *testing.T) {
	ctx := context.Background()
	bucket := objstore.NewInMemBucket()
	path := "indexes/aa/emptyschema"

	buildIndexWithStats(ctx, t, bucket, "acme", path, []stats.Stat{
		{ObjectPath: "logs/log-0", SectionIndex: 0, SortSchema: "",
			Labels: map[string]string{}, MinTimestamp: 10, MaxTimestamp: 20, RowCount: 1, UncompressedSize: 100},
	})

	refs, compatible, err := logSectionRefsFor(ctx, bucket, "acme", path, nil)
	require.NoError(t, err)
	require.True(t, compatible)
	require.Len(t, refs, 1)
	require.Equal(t, refs[0].Min.shard, refs[0].Max.shard)
	require.Equal(t, int64(20), refs[0].Min.timestamp)
	require.Equal(t, int64(10), refs[0].Max.timestamp)
	require.Equal(t, "logs/log-0", refs[0].Ref.ObjectPath)
	require.Equal(t, int64(100), refs[0].Ref.UncompressedSize)
}

func TestLogSectionRefsFor_MixedSchemasRequireMigration(t *testing.T) {
	ctx := context.Background()
	bucket := objstore.NewInMemBucket()
	path := "indexes/aa/mixed"

	buildIndexWithStats(ctx, t, bucket, "acme", path, []stats.Stat{
		{ObjectPath: "logs/log-0", SectionIndex: 0, SortSchema: "label:service_name",
			Labels: map[string]string{"service_name": "api"}, MinTimestamp: 10, MaxTimestamp: 20},
		{ObjectPath: "logs/log-1", SectionIndex: 0, SortSchema: "label:namespace",
			Labels: map[string]string{"namespace": "prod"}, MinTimestamp: 10, MaxTimestamp: 20},
	})

	refs, compatible, err := logSectionRefsFor(ctx, bucket, "acme", path, []string{"label:service_name"})
	require.NoError(t, err)
	require.False(t, compatible)
	require.Len(t, refs, 2)
	for _, ref := range refs {
		require.Equal(t, sortKey{}, ref.Min)
		require.Equal(t, sortKey{}, ref.Max)
	}
}

func TestLogSectionRefsFor_ShardBoundsAvoidFalseOverlap(t *testing.T) {
	ctx := context.Background()
	bucket := objstore.NewInMemBucket()
	path := "indexes/aa/physical-bounds"

	buildIndexWithStats(ctx, t, bucket, "acme", path, []stats.Stat{
		{ObjectPath: "logs/log-0", SectionIndex: 0, SortSchema: "label:service_name",
			Labels: map[string]string{"service_name": "api"}, ShardBucket: 2, MinTimestamp: 10, MaxTimestamp: 20},
		{ObjectPath: "logs/log-1", SectionIndex: 0, SortSchema: "label:service_name",
			Labels: map[string]string{"service_name": "api"}, ShardBucket: 7, MinTimestamp: 10, MaxTimestamp: 20},
	})

	refs, compatible, err := logSectionRefsFor(ctx, bucket, "acme", path, []string{"label:service_name"})
	require.NoError(t, err)
	require.True(t, compatible)
	require.Len(t, refs, 2)
	require.True(t, v2.AreObjectsConverged(refs, compareSortKey))
}

func TestLogSectionRefsFor_InvalidShardRequiresMigration(t *testing.T) {
	targetLayout := (logs.SortLayout{
		SchemaLabels: []string{"label:service_name"},
		StreamOrder:  logs.StreamOrderStableHashV1,
		ShardCount:   16,
	}).ID()

	for _, test := range []struct {
		name string
		stat stats.Stat
	}{
		{
			name: "out_of_range",
			stat: stats.Stat{ObjectPath: "logs/log-0", SortSchema: "label:service_name",
				PhysicalSortLayout: targetLayout, Labels: map[string]string{"service_name": "api"},
				ShardBucket: streams.ShardFactor},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := context.Background()
			bucket := objstore.NewInMemBucket()
			path := "indexes/aa/" + test.name
			buildIndex(ctx, t, bucket, testIndexObject{
				tenant: "acme", path: path, sectionSize: 1 << 21, stats: []stats.Stat{test.stat},
			})

			refs, compatible, err := logSectionRefsFor(ctx, bucket, "acme", path, []string{"label:service_name"})
			require.NoError(t, err)
			require.False(t, compatible)
			require.Len(t, refs, 1)
			require.Equal(t, sortKey{}, refs[0].Min)
			require.Equal(t, sortKey{}, refs[0].Max)
		})
	}
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

func TestCompareIndexSortKey(t *testing.T) {
	base := indexSortKey{kind: postings.KindLabel, columnName: "service", labelValue: "api", minTimestamp: 10, maxTimestamp: 20}
	tests := []indexSortKey{
		{kind: postings.KindBloom, columnName: "z", labelValue: "z", minTimestamp: 99, maxTimestamp: 99},
		{kind: postings.KindLabel, columnName: "namespace", labelValue: "z", minTimestamp: 99, maxTimestamp: 99},
		{kind: postings.KindLabel, columnName: "service", labelValue: "aaa", minTimestamp: 99, maxTimestamp: 99},
		{kind: postings.KindLabel, columnName: "service", labelValue: "api", minTimestamp: 9, maxTimestamp: 99},
		{kind: postings.KindLabel, columnName: "service", labelValue: "api", minTimestamp: 10, maxTimestamp: 19},
	}
	for _, before := range tests {
		require.Negative(t, compareIndexSortKey(before, base))
		require.Positive(t, compareIndexSortKey(base, before))
	}
	require.Zero(t, compareIndexSortKey(base, base))
}

func TestIndexSectionRefsFor_UsesFullPostingsBounds(t *testing.T) {
	ctx := context.Background()
	bucket := objstore.NewInMemBucket()
	path := "indexes/aa/idx-0"
	buildIndexWithPostings(ctx, t, bucket, "acme", path, 1<<20, []postings.Row{
		{Kind: postings.KindLabel, ObjectPath: "logs/b", ColumnName: "service_name", LabelValue: "api", MinTimestamp: 30, MaxTimestamp: 40},
		{Kind: postings.KindLabel, ObjectPath: "logs/a", ColumnName: "service_name", LabelValue: "api", MinTimestamp: 10, MaxTimestamp: 20},
		{Kind: postings.KindLabel, ObjectPath: "logs/c", ColumnName: "service_name", LabelValue: "web", MinTimestamp: 5, MaxTimestamp: 50},
	})

	refs, err := indexSectionRefsFor(ctx, bucket, "acme", []indexEntry{{Path: path}})
	require.NoError(t, err)
	require.Equal(t, []v2.Section[indexSortKey]{
		{
			Ref: &compactionv2pb.SectionRef{ObjectPath: path},
			Min: indexSortKey{kind: postings.KindLabel, columnName: "service_name", labelValue: "api", minTimestamp: 10, maxTimestamp: 20},
			Max: indexSortKey{kind: postings.KindLabel, columnName: "service_name", labelValue: "web", minTimestamp: 5, maxTimestamp: 50},
		},
	}, refs)
}

func TestIndexSectionRefsFor_UsesAbsoluteSectionIndexes(t *testing.T) {
	ctx := context.Background()
	bucket := objstore.NewInMemBucket()
	path := "indexes/aa/idx-0"
	buildIndex(ctx, t, bucket, testIndexObject{
		tenant:      "acme",
		path:        path,
		sectionSize: 1,
		stats: []stats.Stat{{
			ObjectPath: "logs/a", SortSchema: "label:service_name",
			Labels: map[string]string{"service_name": "api"}, MinTimestamp: 1, MaxTimestamp: 2,
		}},
		postings: []postings.Row{
			{Kind: postings.KindLabel, ObjectPath: "logs/a", ColumnName: "a", LabelValue: "1", MinTimestamp: 1, MaxTimestamp: 2},
			{Kind: postings.KindLabel, ObjectPath: "logs/b", ColumnName: "b", LabelValue: "2", MinTimestamp: 3, MaxTimestamp: 4},
		},
	})

	refs, err := indexSectionRefsFor(ctx, bucket, "acme", []indexEntry{{Path: path}})
	require.NoError(t, err)
	require.Len(t, refs, 2)
	require.Equal(t, int64(1), refs[0].Ref.SectionIndex)
	require.Equal(t, int64(2), refs[1].Ref.SectionIndex)
}

func TestIndexSectionRefsFor_FailsOnUnreadableObject(t *testing.T) {
	ctx := context.Background()
	bucket := objstore.NewInMemBucket()
	goodPath := "indexes/aa/good"
	buildIndexWithPostings(ctx, t, bucket, "acme", goodPath, 1<<20, []postings.Row{{
		Kind: postings.KindLabel, ObjectPath: "logs/a", ColumnName: "service_name", LabelValue: "api", MinTimestamp: 1, MaxTimestamp: 2,
	}})

	refs, err := indexSectionRefsFor(ctx, bucket, "acme", []indexEntry{{Path: goodPath}, {Path: "indexes/aa/missing"}})
	require.Error(t, err)
	require.Nil(t, refs)
}
