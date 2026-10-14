package executor

import (
	"context"
	"io"
	"slices"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/oklog/ulid/v2"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/v3/pkg/dataobj"
	v2 "github.com/grafana/loki/v3/pkg/dataobj/compaction/v2"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/indexpointers"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/pointers"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/postings"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/stats"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
)

// buildFilterSourceIndex uploads an index with one postings row and one stats
// row per log object, for each tenant. A small postingsSectionSize splits the
// postings into several sections.
func buildFilterSourceIndex(t *testing.T, bucket objstore.Bucket, path string, postingsSectionSize int, objectsByTenant map[string][]string) {
	t.Helper()
	objBuilder := dataobj.NewBuilder(nil)
	for tenant, objects := range objectsByTenant {
		appendFilterPostings(t, objBuilder, tenant, postingsSectionSize, objects, false)
		var rows []stats.Stat
		for _, object := range objects {
			rows = append(rows, filterStatRow(object, 0))
		}
		appendFilterStats(t, objBuilder, tenant, rows)
	}
	uploadFilterSourceIndex(t, bucket, path, objBuilder)
}

// appendFilterPostings appends one postings section builder with a label row
// per object, and a bloom row per object when withBlooms is true.
func appendFilterPostings(t *testing.T, objBuilder *dataobj.Builder, tenant string, sectionSize int, objects []string, withBlooms bool) {
	t.Helper()
	postingsBuilder := postings.NewBuilder(nil, 0, 0, sectionSize)
	postingsBuilder.SetTenant(tenant)
	for i, object := range objects {
		ts := time.Unix(0, int64(1_000+i))
		postingsBuilder.ObserveLabelPosting(postings.LabelObservation{
			ObjectPath: object, ColumnName: "service_name", LabelValue: object,
			StreamID: 1, Timestamp: ts, UncompressedSize: 100,
		})
		if withBlooms {
			postingsBuilder.PrepareBloomColumn(object, 0, "trace_id", 1, 1)
			require.NoError(t, postingsBuilder.ObserveBloomPosting(postings.BloomObservation{
				ObjectPath: object, ShardBuckets: 1, ColumnName: "trace_id",
				Value: "trace-" + object, StreamID: 1, Timestamp: ts,
			}))
		}
	}
	require.NoError(t, objBuilder.Append(postingsBuilder))
}

// appendFilterStats appends one stats section with rows.
func appendFilterStats(t *testing.T, objBuilder *dataobj.Builder, tenant string, rows []stats.Stat) {
	t.Helper()
	statsBuilder := stats.NewBuilder(nil, stats.ColumnarSectionEncoder(2048, 1000))
	statsBuilder.SetTenant(tenant)
	for _, row := range rows {
		statsBuilder.Append(row)
	}
	require.NoError(t, objBuilder.Append(statsBuilder))
}

func filterStatRow(object string, sectionIndex int64) stats.Stat {
	return stats.Stat{
		ObjectPath: object, SectionIndex: sectionIndex, SortSchema: "label:service_name",
		Labels:       map[string]string{"service_name": object},
		MinTimestamp: 1_000 + sectionIndex, MaxTimestamp: 1_010 + sectionIndex,
		RowCount: 1, UncompressedSize: 100,
	}
}

func uploadFilterSourceIndex(t *testing.T, bucket objstore.Bucket, path string, objBuilder *dataobj.Builder) {
	t.Helper()
	obj, closer, err := objBuilder.Flush()
	require.NoError(t, err)
	defer closer.Close()
	require.NoError(t, uploadObjectToBucket(context.Background(), bucket, path, obj))
}

// readAllStatsRowsFromBucket reads the rows of every stats section of the
// object at path.
func readAllStatsRowsFromBucket(t *testing.T, bucket objstore.Bucket, path string) []stats.Stat {
	t.Helper()
	ctx := context.Background()
	var rows []stats.Stat
	for _, sec := range openObjectFromBucket(ctx, t, bucket, path).Sections() {
		if !stats.CheckSection(sec) {
			continue
		}
		statsSec, err := stats.Open(ctx, sec)
		require.NoError(t, err)
		reader := stats.NewRowReader(ctx, statsSec)
		for reader.Next() {
			rows = append(rows, reader.At())
		}
		require.NoError(t, reader.Err())
		require.NoError(t, reader.Close())
	}
	return rows
}

// indexObjectPaths returns the sorted, distinct log object paths that the
// postings and stats rows of the index at path reference.
func indexObjectPaths(t *testing.T, bucket objstore.Bucket, path string) (postingsPaths, statsPaths []string) {
	t.Helper()
	ctx := context.Background()
	for _, row := range readAllPostingsRowsFromBucket(ctx, t, bucket, path) {
		postingsPaths = append(postingsPaths, row.ObjectPath)
	}
	for _, row := range readAllStatsRowsFromBucket(t, bucket, path) {
		statsPaths = append(statsPaths, row.ObjectPath)
	}
	slices.Sort(postingsPaths)
	slices.Sort(statsPaths)
	return slices.Compact(postingsPaths), slices.Compact(statsPaths)
}

// sizeRecordingBucket records the size that each upload reader reports
// before the upload reads it.
type sizeRecordingBucket struct {
	objstore.Bucket
	sizes   map[string]int64
	sizeErr error
}

func (b *sizeRecordingBucket) Upload(ctx context.Context, name string, r io.Reader) error {
	size, err := objstore.TryToGetSize(r)
	if err != nil {
		b.sizeErr = err
	}
	b.sizes[name] = size
	return b.Bucket.Upload(ctx, name, r)
}

func TestDoIndexFilter(t *testing.T) {
	ctx := context.Background()
	const sourcePath = "indexes/source"

	t.Run("keeps only the postings and stats rows of the listed objects", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		buildFilterSourceIndex(t, bucket, sourcePath, 1<<20, map[string][]string{
			"acme": {"logs/a", "logs/b", "logs/c"},
		})

		artifact, err := newTestExecutorContext(t, bucket).doIndexFilter(ctx, &physical.IndexFilter{
			NodeID: ulid.Make(), Tenant: "acme", SourceIndexPath: sourcePath,
			ObjectPaths: []string{"logs/a", "logs/c"},
		})
		require.NoError(t, err)

		postingsPaths, statsPaths := indexObjectPaths(t, bucket, artifact.Path)
		require.Equal(t, []string{"logs/a", "logs/c"}, postingsPaths)
		require.Equal(t, []string{"logs/a", "logs/c"}, statsPaths)
	})

	t.Run("reads every postings section of the source index", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		objects := []string{"logs/a", "logs/b", "logs/c", "logs/d", "logs/e", "logs/f"}
		buildFilterSourceIndex(t, bucket, sourcePath, 64, map[string][]string{"acme": objects})
		sourcePostings := 0
		for _, sec := range openObjectFromBucket(ctx, t, bucket, sourcePath).Sections() {
			if postings.CheckSection(sec) {
				sourcePostings++
			}
		}
		require.Greater(t, sourcePostings, 1, "the source index must hold several postings sections")

		artifact, err := newTestExecutorContext(t, bucket).doIndexFilter(ctx, &physical.IndexFilter{
			NodeID: ulid.Make(), Tenant: "acme", SourceIndexPath: sourcePath,
			ObjectPaths: []string{"logs/a", "logs/f"},
		})
		require.NoError(t, err)

		postingsPaths, _ := indexObjectPaths(t, bucket, artifact.Path)
		require.Equal(t, []string{"logs/a", "logs/f"}, postingsPaths)
	})

	t.Run("keeps the bloom postings rows of the listed objects only", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		objBuilder := dataobj.NewBuilder(nil)
		appendFilterPostings(t, objBuilder, "acme", 1<<20, []string{"logs/a", "logs/b"}, true)
		appendFilterStats(t, objBuilder, "acme", []stats.Stat{filterStatRow("logs/a", 0), filterStatRow("logs/b", 0)})
		uploadFilterSourceIndex(t, bucket, sourcePath, objBuilder)

		artifact, err := newTestExecutorContext(t, bucket).doIndexFilter(ctx, &physical.IndexFilter{
			NodeID: ulid.Make(), Tenant: "acme", SourceIndexPath: sourcePath,
			ObjectPaths: []string{"logs/a"},
		})
		require.NoError(t, err)

		var blooms []string
		for _, row := range readAllPostingsRowsFromBucket(ctx, t, bucket, artifact.Path) {
			if row.Kind == postings.KindBloom {
				blooms = append(blooms, row.ObjectPath)
			}
		}
		require.Equal(t, []string{"logs/a"}, blooms)
	})

	t.Run("keeps every stats row of a listed object across several stats sections", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		objBuilder := dataobj.NewBuilder(nil)
		appendFilterPostings(t, objBuilder, "acme", 1<<20, []string{"logs/a", "logs/b"}, false)
		appendFilterStats(t, objBuilder, "acme", []stats.Stat{filterStatRow("logs/a", 0), filterStatRow("logs/b", 0)})
		appendFilterStats(t, objBuilder, "acme", []stats.Stat{filterStatRow("logs/a", 1), filterStatRow("logs/b", 1)})
		uploadFilterSourceIndex(t, bucket, sourcePath, objBuilder)
		sourceStats := 0
		for _, sec := range openObjectFromBucket(ctx, t, bucket, sourcePath).Sections() {
			if stats.CheckSection(sec) {
				sourceStats++
			}
		}
		require.Equal(t, 2, sourceStats, "the source index must hold two stats sections")

		artifact, err := newTestExecutorContext(t, bucket).doIndexFilter(ctx, &physical.IndexFilter{
			NodeID: ulid.Make(), Tenant: "acme", SourceIndexPath: sourcePath,
			ObjectPaths: []string{"logs/a"},
		})
		require.NoError(t, err)

		type key struct {
			object  string
			section int64
		}
		var got []key
		for _, row := range readAllStatsRowsFromBucket(t, bucket, artifact.Path) {
			got = append(got, key{row.ObjectPath, row.SectionIndex})
		}
		require.ElementsMatch(t, []key{{"logs/a", 0}, {"logs/a", 1}}, got)
	})

	t.Run("copies every field of the kept postings and stats rows", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		objBuilder := dataobj.NewBuilder(nil)
		appendFilterPostings(t, objBuilder, "acme", 1<<20, []string{"logs/a", "logs/b"}, true)
		appendFilterStats(t, objBuilder, "acme", []stats.Stat{filterStatRow("logs/a", 0), filterStatRow("logs/a", 1), filterStatRow("logs/b", 0)})
		uploadFilterSourceIndex(t, bucket, sourcePath, objBuilder)

		artifact, err := newTestExecutorContext(t, bucket).doIndexFilter(ctx, &physical.IndexFilter{
			NodeID: ulid.Make(), Tenant: "acme", SourceIndexPath: sourcePath,
			ObjectPaths: []string{"logs/a"},
		})
		require.NoError(t, err)

		var wantPostings []postings.Row
		for _, row := range readAllPostingsRowsFromBucket(ctx, t, bucket, sourcePath) {
			if row.ObjectPath == "logs/a" {
				wantPostings = append(wantPostings, row)
			}
		}
		var wantStats []stats.Stat
		for _, row := range readAllStatsRowsFromBucket(t, bucket, sourcePath) {
			if row.ObjectPath == "logs/a" {
				wantStats = append(wantStats, row)
			}
		}
		require.ElementsMatch(t, wantPostings, readAllPostingsRowsFromBucket(ctx, t, bucket, artifact.Path))
		require.ElementsMatch(t, wantStats, readAllStatsRowsFromBucket(t, bucket, artifact.Path))
	})

	t.Run("skips pointers and streams sections of the source index", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		objBuilder := dataobj.NewBuilder(nil)
		appendFilterPostings(t, objBuilder, "acme", 1<<20, []string{"logs/a", "logs/b"}, false)
		appendFilterStats(t, objBuilder, "acme", []stats.Stat{filterStatRow("logs/a", 0), filterStatRow("logs/b", 0)})

		pointersBuilder := pointers.NewBuilder(nil, 0, 0)
		pointersBuilder.SetTenant("acme")
		pointersBuilder.ObserveStream("logs/a", 0, 1, 1, time.Unix(0, 1_000), 100)
		require.NoError(t, objBuilder.Append(pointersBuilder))

		streamsBuilder := streams.NewBuilder(nil, 8192, 0)
		streamsBuilder.SetTenant("acme")
		streamsBuilder.Record(labels.FromStrings("service_name", "logs/a"), time.Unix(0, 1_000), 100)
		require.NoError(t, objBuilder.Append(streamsBuilder))

		uploadFilterSourceIndex(t, bucket, sourcePath, objBuilder)

		artifact, err := newTestExecutorContext(t, bucket).doIndexFilter(ctx, &physical.IndexFilter{
			NodeID: ulid.Make(), Tenant: "acme", SourceIndexPath: sourcePath,
			ObjectPaths: []string{"logs/a"},
		})
		require.NoError(t, err)

		for _, sec := range openObjectFromBucket(ctx, t, bucket, artifact.Path).Sections() {
			require.False(t, pointers.CheckSection(sec), "output must not hold a pointers section")
			require.False(t, streams.CheckSection(sec), "output must not hold a streams section")
		}
		postingsPaths, statsPaths := indexObjectPaths(t, bucket, artifact.Path)
		require.Equal(t, []string{"logs/a"}, postingsPaths)
		require.Equal(t, []string{"logs/a"}, statsPaths)
	})

	t.Run("returns an error when the source index holds an unsupported section type", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		objBuilder := dataobj.NewBuilder(nil)
		appendFilterPostings(t, objBuilder, "acme", 1<<20, []string{"logs/a"}, false)
		appendFilterStats(t, objBuilder, "acme", []stats.Stat{filterStatRow("logs/a", 0)})

		indexPointersBuilder := indexpointers.NewBuilder(nil, 1024, 0)
		indexPointersBuilder.SetTenant("acme")
		indexPointersBuilder.Append("indexes/other", time.Unix(0, 1_000), time.Unix(0, 2_000))
		require.NoError(t, objBuilder.Append(indexPointersBuilder))

		uploadFilterSourceIndex(t, bucket, sourcePath, objBuilder)

		_, err := newTestExecutorContext(t, bucket).doIndexFilter(ctx, &physical.IndexFilter{
			NodeID: ulid.Make(), Tenant: "acme", SourceIndexPath: sourcePath,
			ObjectPaths: []string{"logs/a"},
		})
		require.ErrorContains(t, err, "unknown section")
	})

	t.Run("does not copy rows of other tenants", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		buildFilterSourceIndex(t, bucket, sourcePath, 1<<20, map[string][]string{
			"acme":  {"logs/a"},
			"other": {"logs/a", "logs/other"},
		})

		artifact, err := newTestExecutorContext(t, bucket).doIndexFilter(ctx, &physical.IndexFilter{
			NodeID: ulid.Make(), Tenant: "acme", SourceIndexPath: sourcePath,
			ObjectPaths: []string{"logs/a"},
		})
		require.NoError(t, err)

		output := openObjectFromBucket(ctx, t, bucket, artifact.Path)
		for _, sec := range output.Sections() {
			require.Equal(t, "acme", sec.Tenant)
		}
		postingsPaths, statsPaths := indexObjectPaths(t, bucket, artifact.Path)
		require.Equal(t, []string{"logs/a"}, postingsPaths)
		require.Equal(t, []string{"logs/a"}, statsPaths)
	})

	t.Run("returns an error when a listed object has no rows in the source index", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		buildFilterSourceIndex(t, bucket, sourcePath, 1<<20, map[string][]string{"acme": {"logs/a"}})

		_, err := newTestExecutorContext(t, bucket).doIndexFilter(ctx, &physical.IndexFilter{
			NodeID: ulid.Make(), Tenant: "acme", SourceIndexPath: sourcePath,
			ObjectPaths: []string{"logs/a", "logs/missing"},
		})
		require.ErrorContains(t, err, "logs/missing")
	})

	t.Run("returns an error when a listed object has stats rows but no postings rows", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		objBuilder := dataobj.NewBuilder(nil)
		appendFilterPostings(t, objBuilder, "acme", 1<<20, []string{"logs/b"}, false)
		appendFilterStats(t, objBuilder, "acme", []stats.Stat{filterStatRow("logs/a", 0), filterStatRow("logs/b", 0)})
		uploadFilterSourceIndex(t, bucket, sourcePath, objBuilder)

		_, err := newTestExecutorContext(t, bucket).doIndexFilter(ctx, &physical.IndexFilter{
			NodeID: ulid.Make(), Tenant: "acme", SourceIndexPath: sourcePath,
			ObjectPaths: []string{"logs/a"},
		})
		require.ErrorContains(t, err, `no postings rows for object "logs/a"`)
	})

	t.Run("returns an error when a listed object has postings rows but no stats rows", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		objBuilder := dataobj.NewBuilder(nil)
		appendFilterPostings(t, objBuilder, "acme", 1<<20, []string{"logs/a", "logs/b"}, false)
		appendFilterStats(t, objBuilder, "acme", []stats.Stat{filterStatRow("logs/b", 0)})
		uploadFilterSourceIndex(t, bucket, sourcePath, objBuilder)

		_, err := newTestExecutorContext(t, bucket).doIndexFilter(ctx, &physical.IndexFilter{
			NodeID: ulid.Make(), Tenant: "acme", SourceIndexPath: sourcePath,
			ObjectPaths: []string{"logs/a"},
		})
		require.ErrorContains(t, err, `no stats rows for object "logs/a"`)
	})

	t.Run("returns an error when the plan has no tenant or no source index", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		buildFilterSourceIndex(t, bucket, sourcePath, 1<<20, map[string][]string{"acme": {"logs/a"}})
		execCtx := newTestExecutorContext(t, bucket)

		_, err := execCtx.doIndexFilter(ctx, &physical.IndexFilter{
			NodeID: ulid.Make(), SourceIndexPath: sourcePath, ObjectPaths: []string{"logs/a"},
		})
		require.ErrorContains(t, err, "malformed plan")

		_, err = execCtx.doIndexFilter(ctx, &physical.IndexFilter{
			NodeID: ulid.Make(), Tenant: "acme", ObjectPaths: []string{"logs/a"},
		})
		require.ErrorContains(t, err, "malformed plan")
	})

	t.Run("returns an error when no objects are listed", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		buildFilterSourceIndex(t, bucket, sourcePath, 1<<20, map[string][]string{"acme": {"logs/a"}})

		_, err := newTestExecutorContext(t, bucket).doIndexFilter(ctx, &physical.IndexFilter{
			NodeID: ulid.Make(), Tenant: "acme", SourceIndexPath: sourcePath,
		})
		require.ErrorContains(t, err, "no object paths")
	})

	t.Run("uploads the index with its size known before the upload reads it", func(t *testing.T) {
		inner := objstore.NewInMemBucket()
		buildFilterSourceIndex(t, inner, sourcePath, 1<<20, map[string][]string{"acme": {"logs/a", "logs/b"}})
		bucket := &sizeRecordingBucket{Bucket: inner, sizes: map[string]int64{}}

		artifact, err := newTestExecutorContext(t, bucket).doIndexFilter(ctx, &physical.IndexFilter{
			NodeID: ulid.Make(), Tenant: "acme", SourceIndexPath: sourcePath,
			ObjectPaths: []string{"logs/a"},
		})
		require.NoError(t, err)
		require.NoError(t, bucket.sizeErr)

		attrs, err := inner.Attributes(ctx, artifact.Path)
		require.NoError(t, err)
		require.Equal(t, attrs.Size, bucket.sizes[artifact.Path])
	})

	t.Run("returns an error when the source index does not exist", func(t *testing.T) {
		_, err := newTestExecutorContext(t, objstore.NewInMemBucket()).doIndexFilter(ctx, &physical.IndexFilter{
			NodeID: ulid.Make(), Tenant: "acme", SourceIndexPath: sourcePath,
			ObjectPaths: []string{"logs/a"},
		})
		require.ErrorContains(t, err, sourcePath)
	})
}

func TestExecuteIndexFilter(t *testing.T) {
	t.Run("emits one record with the content-hash path of the uploaded index", func(t *testing.T) {
		ctx := context.Background()
		bucket := objstore.NewInMemBucket()
		buildFilterSourceIndex(t, bucket, "indexes/source", 1<<20, map[string][]string{"acme": {"logs/a", "logs/b"}})

		pipeline := newTestExecutorContext(t, bucket).executeIndexFilter(&physical.IndexFilter{
			NodeID: ulid.Make(), Tenant: "acme", SourceIndexPath: "indexes/source",
			ObjectPaths: []string{"logs/a"},
		})
		reader := TranslateEOF(pipeline)
		defer reader.Close()
		require.NoError(t, reader.Open(ctx))
		var records []arrow.RecordBatch
		for {
			rec, err := reader.Read(ctx)
			if err == io.EOF {
				break
			}
			require.NoError(t, err)
			records = append(records, rec)
		}
		require.Len(t, records, 1)

		var artifact v2.ResultArtifact
		require.NoError(t, artifact.FromRecordBatch(records[0]))
		exists, err := bucket.Exists(ctx, artifact.Path)
		require.NoError(t, err)
		require.True(t, exists)

		uploaded, err := bucket.Get(ctx, artifact.Path)
		require.NoError(t, err)
		defer uploaded.Close()
		wantPath, err := v2.CompactedIndexPath("acme", uploaded)
		require.NoError(t, err)
		require.Equal(t, wantPath, artifact.Path)
	})
}
