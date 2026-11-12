package compactor

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"math"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/flagext"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/v3/pkg/dataobj"
	v2 "github.com/grafana/loki/v3/pkg/dataobj/compaction/v2"
	compactionv2pb "github.com/grafana/loki/v3/pkg/dataobj/compaction/v2/proto"
	"github.com/grafana/loki/v3/pkg/dataobj/index/indexobj"
	"github.com/grafana/loki/v3/pkg/dataobj/logsobj"
	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/indexpointers"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/postings"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/stats"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
)

func TestLoadTenantIndexes_ReadsTenantToC(t *testing.T) {
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

	// Each tenant's entries come from its own ToC, never another tenant's.
	gotA, err := loadTenantIndexes(ctx, bucket, window, "tenant-a")
	require.NoError(t, err)
	requireIndexPaths(t, gotA, "indexes/aa/idx-a-0", "indexes/bb/idx-a-1")

	gotB, err := loadTenantIndexes(ctx, bucket, window, "tenant-b")
	require.NoError(t, err)
	requireIndexPaths(t, gotB, "indexes/cc/idx-b-0")
}

// TestLoadTenantIndexes_MissingToCReturnsNotFound verifies the no-ToC case
// produces a bucket.IsObjNotFoundErr-class error. The coordinator treats
// this as "nothing to do this cycle" and waits for the next tick.
func TestLoadTenantIndexes_MissingToCReturnsNotFound(t *testing.T) {
	ctx := context.Background()
	bucket := objstore.NewInMemBucket()
	window := time.Date(2026, 5, 14, 0, 0, 0, 0, time.UTC).Truncate(metastore.MetastoreWindowSize)

	_, err := loadTenantIndexes(ctx, bucket, window, "tenant-a")
	require.Error(t, err)
	require.True(t, bucket.IsObjNotFoundErr(err),
		"missing ToC must surface as IsObjNotFoundErr, got %v", err)
}

func TestLoadTenantIndexes_PanicsOnAnotherTenantsSection(t *testing.T) {
	ctx := context.Background()
	bucket := objstore.NewInMemBucket()
	window := time.Date(2026, 5, 14, 0, 0, 0, 0, time.UTC).Truncate(metastore.MetastoreWindowSize)

	writeToCWithIndexes(ctx, t, bucket, map[string][]testIndex{
		"tenant-b": {{path: "indexes/aa/idx-b-0", start: window.Add(time.Hour), end: window.Add(2 * time.Hour)}},
	})
	// Put tenant-b's ToC where tenant-a's belongs.
	r, err := bucket.Get(ctx, metastore.TableOfContentsPath("tenant-b", window))
	require.NoError(t, err)
	defer r.Close()
	require.NoError(t, bucket.Upload(ctx, metastore.TableOfContentsPath("tenant-a", window), r))

	require.Panics(t, func() { _, _ = loadTenantIndexes(ctx, bucket, window, "tenant-a") })
}

// testIndex captures one index pointer entry (path, time range, sizes) to seed a ToC fixture.
type testIndex struct {
	path  string
	start time.Time
	end   time.Time
}

// writeToCWithIndexes writes synthetic per-tenant ToCs containing one index pointer per
// (tenant, path) entry. Each entry's time range must fall inside the
// MetastoreWindowSize window the ToC covers (otherwise WriteEntry will route
// it to a different ToC file).
func writeToCWithIndexes(ctx context.Context, t *testing.T, bucket objstore.Bucket, entries map[string][]testIndex) {
	t.Helper()
	w := metastore.NewTableOfContentsWriter(bucket, metastore.DefaultTocWriterBackoffConfig, metastore.DefaultTocBuilderConfig, log.NewNopLogger(), metastore.NewTocWriterMetrics(nil), indexobj.NewBuilderMetrics(nil))
	for tenant, paths := range entries {
		for _, e := range paths {
			require.NoError(t, w.WriteEntry(ctx, tenant, metastore.TableOfContentsEntry{
				Path:      e.path,
				StartTime: e.start,
				EndTime:   e.end,
			}))
		}
	}
}

// requireIndexPaths asserts that got holds exactly the paths want, in any
// order, because the order of ToC iteration is not part of the contract.
func requireIndexPaths(t *testing.T, got []indexpointers.IndexPointer, want ...string) {
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

	// shardBuckets is the bucket count of the shard layout that the stats
	// section records. It must not be zero.
	shardBuckets uint32
}

func buildIndexWithStats(ctx context.Context, t *testing.T, bucket objstore.Bucket, tenant, path string, rows []stats.Stat) {
	t.Helper()
	buildIndex(ctx, t, bucket, testIndexObject{tenant: tenant, path: path, sectionSize: 1 << 21, stats: rows, shardBuckets: streams.DefaultShardBuckets})
}

func buildIndexWithPostings(ctx context.Context, t *testing.T, bucket objstore.Bucket, tenant, path string, sectionSize flagext.Bytes, rows []postings.Row) {
	t.Helper()
	buildIndex(ctx, t, bucket, testIndexObject{tenant: tenant, path: path, sectionSize: sectionSize, postings: rows, shardBuckets: streams.DefaultShardBuckets})
}

func buildIndex(ctx context.Context, t *testing.T, bucket objstore.Bucket, object testIndexObject) {
	t.Helper()
	require.NotZero(t, object.shardBuckets, "testIndexObject.shardBuckets must be set")
	cfg := logsobj.BuilderBaseConfig{
		TargetPageSize:          2048,
		MaxPageRows:             10000,
		TargetObjectSize:        1 << 22,
		TargetSectionSize:       object.sectionSize,
		BufferSize:              2048 * 8,
		SectionStripeMergeLimit: 2,
	}
	builder, err := indexobj.NewMergeBuilder(cfg, nil, object.tenant, layoutOfStats(object.shardBuckets, object.stats))
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

// layoutOfStats returns the layout with shardBuckets shard buckets and the
// sort schema of the first of rows as schema labels.
func layoutOfStats(shardBuckets uint32, rows []stats.Stat) streams.Layout {
	var schemaLabels []string
	if len(rows) > 0 && rows[0].SortSchema != "" {
		schemaLabels = strings.Split(rows[0].SortSchema, ",")
	}
	return streams.NewLayout(streams.MustShardLayoutFromBuckets(shardBuckets), schemaLabels)
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
			Labels: map[string]string{"service_name": "auth"}, MinTimestamp: 500, MaxTimestamp: 1000, RowCount: 3, UncompressedSize: 300},
		{ObjectPath: "logs/log-0", SectionIndex: 0, SortSchema: "label:service_name",
			Labels: map[string]string{"service_name": "billing"}, MinTimestamp: 100, MaxTimestamp: 900, RowCount: 2, UncompressedSize: 200},
	})

	refs, _, err := logSectionRefsFor(ctx, bucket, "acme", runOfPath(path))
	require.NoError(t, err)
	require.Equal(t, []v2.Section[logSortPrefix]{
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
			PlanningUnit: "logs/log-0",
			Min:          newLogSortPrefix(0, []string{"auth"}),
			Max:          newLogSortPrefix(0, []string{"billing"}),
			Keys: []v2.KeySize[logSortPrefix]{
				{Key: newLogSortPrefix(0, []string{"auth"}), UncompressedSize: 300},
				{Key: newLogSortPrefix(0, []string{"billing"}), UncompressedSize: 200},
			},
		},
	}, refs)
}

func TestLogSectionRefsFor_OrdersPhysicalSections(t *testing.T) {
	ctx := context.Background()
	bucket := objstore.NewInMemBucket()
	path := "indexes/aa/objects"

	buildIndexWithStats(ctx, t, bucket, "acme", path, []stats.Stat{
		{ObjectPath: "logs/log-b", SectionIndex: 1, SortSchema: "label:service_name",
			Labels: map[string]string{"service_name": "delta"}, MinTimestamp: 30, MaxTimestamp: 40, UncompressedSize: 100},
		{ObjectPath: "logs/log-a", SectionIndex: 0, SortSchema: "label:service_name",
			Labels: map[string]string{"service_name": "echo"}, MinTimestamp: 50, MaxTimestamp: 60, UncompressedSize: 100},
		{ObjectPath: "logs/log-b", SectionIndex: 0, SortSchema: "label:service_name",
			Labels: map[string]string{"service_name": "alpha"}, MinTimestamp: 10, MaxTimestamp: 20, UncompressedSize: 100},
	})

	sections, _, err := logSectionRefsFor(ctx, bucket, "acme", runOfPath(path))
	require.NoError(t, err)
	require.Len(t, sections, 3)
	require.Equal(t, []string{"logs/log-a", "logs/log-b", "logs/log-b"}, []string{
		sections[0].Ref.ObjectPath,
		sections[1].Ref.ObjectPath,
		sections[2].Ref.ObjectPath,
	})
	require.Equal(t, []int64{0, 0, 1}, []int64{
		sections[0].Ref.SectionIndex,
		sections[1].Ref.SectionIndex,
		sections[2].Ref.SectionIndex,
	})
}

func TestLogSectionRefsFor_MultiKeySchemaOrdersValuesAndReturnsFQN(t *testing.T) {
	ctx := context.Background()
	bucket := objstore.NewInMemBucket()
	path := "indexes/aa/multikey"

	buildIndexWithStats(ctx, t, bucket, "acme", path, []stats.Stat{
		{ObjectPath: "logs/log-0", SectionIndex: 0, SortSchema: "label:service_name,label:namespace",
			Labels: map[string]string{"service_name": "auth", "namespace": "eu"}, MinTimestamp: 10, MaxTimestamp: 20, RowCount: 1, UncompressedSize: 100},
	})

	refs, _, err := logSectionRefsFor(ctx, bucket, "acme", runOfPath(path))
	require.NoError(t, err)
	require.Equal(t, []v2.Section[logSortPrefix]{
		{
			Ref: &compactionv2pb.SectionRef{
				ObjectPath:       "logs/log-0",
				MinKey:           []string{"auth", "eu"},
				MaxKey:           []string{"auth", "eu"},
				MinTimestamp:     10,
				MaxTimestamp:     20,
				UncompressedSize: 100,
			},
			PlanningUnit: "logs/log-0",
			Min:          newLogSortPrefix(0, []string{"auth", "eu"}),
			Max:          newLogSortPrefix(0, []string{"auth", "eu"}),
			Keys:         []v2.KeySize[logSortPrefix]{{Key: newLogSortPrefix(0, []string{"auth", "eu"}), UncompressedSize: 100}},
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

	refs, _, err := logSectionRefsFor(ctx, bucket, "acme", runOfPath(path))
	require.NoError(t, err)
	require.Len(t, refs, 1)
	require.Equal(t, logSortPrefix{}, refs[0].Min)
	require.Equal(t, logSortPrefix{}, refs[0].Max)
	require.Equal(t, int64(10), refs[0].Ref.MinTimestamp)
	require.Equal(t, int64(20), refs[0].Ref.MaxTimestamp)
	require.Equal(t, "logs/log-0", refs[0].Ref.ObjectPath)
	require.Equal(t, int64(100), refs[0].Ref.UncompressedSize)
}

func TestIndexLayout(t *testing.T) {
	ctx := context.Background()
	const tenant = "acme"

	t.Run("returns the layout that the stats sections record", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		path := "indexes/current"
		buildIndexWithStats(ctx, t, bucket, tenant, path, []stats.Stat{{
			ObjectPath: "logs/current", SortSchema: "label:service_name",
		}})

		layout, ok, err := indexLayoutAt(ctx, bucket, tenant, path)
		require.NoError(t, err)
		require.True(t, ok)
		require.Equal(t, streams.NewLayout(streams.DefaultShardLayout(), []string{"label:service_name"}), layout)
	})

	t.Run("returns a corrupt index error when stats sections record different layouts", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		path := "indexes/mixed"
		builder := dataobj.NewBuilder(nil)
		for _, schema := range []string{"label:service_name", "label:namespace"} {
			statsBuilder := stats.NewBuilder(nil, stats.ColumnarSectionEncoder(2048, 10000), streams.NewLayout(streams.DefaultShardLayout(), []string{schema}))
			statsBuilder.SetTenant(tenant)
			statsBuilder.Append(stats.Stat{ObjectPath: "logs/" + schema, SortSchema: schema})
			require.NoError(t, builder.Append(statsBuilder))
		}
		obj, closer, err := builder.Flush()
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, closer.Close()) })

		_, _, err = indexLayout(ctx, obj, tenant, path)
		require.ErrorIs(t, err, errCorruptIndex)
		require.ErrorIs(t, err, indexobj.ErrMixedLayouts)

		uploadObject(ctx, t, bucket, path, obj)
		_, _, err = logSectionRefsFor(ctx, bucket, tenant, runOfPath(path))
		require.ErrorIs(t, err, errCorruptIndex)
	})
}

// uploadObject uploads obj to bucket at path.
func uploadObject(ctx context.Context, t *testing.T, bucket objstore.Bucket, path string, obj *dataobj.Object) {
	t.Helper()
	reader, err := obj.Reader(ctx)
	require.NoError(t, err)
	defer reader.Close()
	objBytes, err := io.ReadAll(reader)
	require.NoError(t, err)
	require.NoError(t, bucket.Upload(ctx, path, io.NopCloser(bytes.NewReader(objBytes))))
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

	refs, err := indexSectionRefsFor(ctx, bucket, "acme", []indexpointers.IndexPointer{{Path: path}})
	require.NoError(t, err)
	require.Equal(t, []v2.Section[indexSortKey]{
		{
			Ref:          &compactionv2pb.SectionRef{ObjectPath: path},
			PlanningUnit: path,
			Min:          indexSortKey{kind: postings.KindLabel, columnName: "service_name", labelValue: "api", minTimestamp: 10, maxTimestamp: 20},
			Max:          indexSortKey{kind: postings.KindLabel, columnName: "service_name", labelValue: "web", minTimestamp: 5, maxTimestamp: 50},
		},
	}, refs)
}

func TestIndexSectionRefsFor_UsesAbsoluteSectionIndexes(t *testing.T) {
	ctx := context.Background()
	bucket := objstore.NewInMemBucket()
	path := "indexes/aa/idx-0"
	buildIndex(ctx, t, bucket, testIndexObject{
		tenant:       "acme",
		path:         path,
		sectionSize:  1,
		shardBuckets: streams.DefaultShardBuckets,
		stats: []stats.Stat{{
			ObjectPath: "logs/a", SortSchema: "label:service_name",
			Labels: map[string]string{"service_name": "api"}, MinTimestamp: 1, MaxTimestamp: 2,
		}},
		postings: []postings.Row{
			{Kind: postings.KindLabel, ObjectPath: "logs/a", ColumnName: "a", LabelValue: "1", MinTimestamp: 1, MaxTimestamp: 2},
			{Kind: postings.KindLabel, ObjectPath: "logs/b", ColumnName: "b", LabelValue: "2", MinTimestamp: 3, MaxTimestamp: 4},
		},
	})

	refs, err := indexSectionRefsFor(ctx, bucket, "acme", []indexpointers.IndexPointer{{Path: path}})
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

	refs, err := indexSectionRefsFor(ctx, bucket, "acme", []indexpointers.IndexPointer{{Path: goodPath}, {Path: "indexes/aa/missing"}})
	require.Error(t, err)
	require.Nil(t, refs)
}

func TestCompareLogSortPrefix(t *testing.T) {
	schema := []string{"label:service_name", "label:namespace"}
	planKey := func(t *testing.T, ls labels.Labels) logSortPrefix {
		t.Helper()
		return newLogSortPrefix(
			streams.DefaultShardLayout().BucketFromHash(labels.StableHash(ls)),
			[]string{ls.Get("service_name"), ls.Get("namespace")},
		)
	}
	streamKey := func(t *testing.T, ls labels.Labels) streams.SortKey {
		t.Helper()
		schemaKey, err := logsobj.ComputeSchemaKey(ls, schema)
		require.NoError(t, err)
		return streams.DefaultShardLayout().NewSortKey(ls, schemaKey)
	}

	t.Run("planner order is nondecreasing over streams in stream ID order", func(t *testing.T) {
		var all []labels.Labels
		for _, svc := range []string{"", "a", "a\x00", "a\x00b", "ab", "b"} {
			for _, ns := range []string{"", "\x00", "b", "eu", "us"} {
				for i := range 4 {
					all = append(all, labels.FromStrings(
						"service_name", svc, "namespace", ns, "pod", fmt.Sprint(i),
					))
				}
			}
		}
		slices.SortFunc(all, func(a, b labels.Labels) int {
			return streams.CompareSortKey(streamKey(t, a), streamKey(t, b))
		})

		keys := make([]logSortPrefix, len(all))
		for i, ls := range all {
			keys[i] = planKey(t, ls)
		}
		require.True(t, slices.IsSortedFunc(keys, compareLogSortPrefix))
	})

	t.Run("planner key equals the stream sort key prefix", func(t *testing.T) {
		ls := labels.FromStrings("service_name", "a\x00", "namespace", "b")
		require.Equal(t, streamKey(t, ls).Prefix(), planKey(t, ls).prefix)
	})
}

// indexLayoutAt opens the index at idxPath and returns its layout as
// indexLayout does.
func indexLayoutAt(ctx context.Context, bucket objstore.Bucket, tenant, idxPath string) (streams.Layout, bool, error) {
	obj, err := dataobj.FromBucket(ctx, bucket, idxPath, prefetchBytes)
	if err != nil {
		return streams.Layout{}, false, fmt.Errorf("open source index tenant=%s index=%s: %w", tenant, idxPath, err)
	}
	return indexLayout(ctx, obj, tenant, idxPath)
}

func TestLogSectionRefsFor(t *testing.T) {
	ctx := context.Background()
	runOf := func(paths ...string) indexRun {
		var run indexRun
		for _, path := range paths {
			run.entries = append(run.entries, indexpointers.IndexPointer{Path: path, RunID: 1})
		}
		return run
	}
	stat := func(service string, size int64) stats.Stat {
		return stats.Stat{
			ObjectPath: "logs/log-0", SectionIndex: 0, SortSchema: "label:service_name",
			Labels: map[string]string{"service_name": service}, MinTimestamp: 100, MaxTimestamp: 200,
			RowCount: 1, UncompressedSize: size,
		}
	}

	t.Run("folds the stats rows of one log section from every index object of a run into one reference", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		buildIndexWithStats(ctx, t, bucket, "acme", "indexes/run/a", []stats.Stat{stat("auth", 300)})
		buildIndexWithStats(ctx, t, bucket, "acme", "indexes/run/b", []stats.Stat{stat("billing", 200)})

		refs, buckets, err := logSectionRefsFor(ctx, bucket, "acme", runOf("indexes/run/a", "indexes/run/b"))
		require.NoError(t, err)
		require.Len(t, refs, 1)
		require.Equal(t, int64(500), refs[0].Ref.UncompressedSize)
		require.Equal(t, []string{"auth"}, refs[0].Ref.MinKey)
		require.Equal(t, []string{"billing"}, refs[0].Ref.MaxKey)
		layout, ok := buckets.Layout()
		require.True(t, ok)
		require.Equal(t, streams.DefaultShardLayout(), layout)
	})

	t.Run("ignores the stats rows of another tenant in an index object that holds both tenants", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		builder := dataobj.NewBuilder(nil)
		for _, tenant := range []string{"acme", "other"} {
			statsBuilder := stats.NewBuilder(nil, stats.ColumnarSectionEncoder(2048, 10000), streams.NewLayout(streams.DefaultShardLayout(), []string{"label:service_name"}))
			statsBuilder.SetTenant(tenant)
			row := stat("auth", 300)
			row.ObjectPath = "logs/" + tenant
			statsBuilder.Append(row)
			require.NoError(t, builder.Append(statsBuilder))
		}
		obj, closer, err := builder.Flush()
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, closer.Close()) })
		uploadObject(ctx, t, bucket, "indexes/run/a", obj)

		refs, _, err := logSectionRefsFor(ctx, bucket, "acme", runOf("indexes/run/a"))
		require.NoError(t, err)
		require.Len(t, refs, 1)
		require.Equal(t, "logs/acme", refs[0].Ref.ObjectPath)
	})

	t.Run("returns a corrupt index error when an index object of a run records no layout and another does", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		buildIndexWithStats(ctx, t, bucket, "acme", "indexes/run/a", []stats.Stat{stat("auth", 300)})
		buildIndexWithPostings(ctx, t, bucket, "acme", "indexes/run/b", 1<<20, []postings.Row{{Kind: postings.KindLabel, ObjectPath: "logs/log-0", ColumnName: "app", LabelValue: "api"}})

		_, _, err := logSectionRefsFor(ctx, bucket, "acme", runOf("indexes/run/a", "indexes/run/b"))
		require.ErrorIs(t, err, errCorruptIndex)
	})
}
