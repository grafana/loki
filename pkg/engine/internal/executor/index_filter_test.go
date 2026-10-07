package executor

import (
	"context"
	"io"
	"slices"
	"testing"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/oklog/ulid/v2"
	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/v3/pkg/dataobj"
	v2 "github.com/grafana/loki/v3/pkg/dataobj/compaction/v2"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/postings"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/stats"
	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
)

// buildFilterSourceIndex uploads an index with one postings row and one stats
// row per log object, for each tenant. A small postingsSectionSize splits the
// postings into several sections.
func buildFilterSourceIndex(t *testing.T, bucket objstore.Bucket, path string, postingsSectionSize int, objectsByTenant map[string][]string) {
	t.Helper()
	objBuilder := dataobj.NewBuilder(nil)
	for tenant, objects := range objectsByTenant {
		postingsBuilder := postings.NewBuilder(nil, 0, 0, postingsSectionSize)
		postingsBuilder.SetTenant(tenant)
		statsBuilder := stats.NewBuilder(nil, stats.ColumnarSectionEncoder(2048, 1000))
		statsBuilder.SetTenant(tenant)
		for i, object := range objects {
			ts := time.Unix(0, int64(1_000+i))
			postingsBuilder.ObserveLabelPosting(postings.LabelObservation{
				ObjectPath: object, ColumnName: "service_name", LabelValue: object,
				StreamID: 1, Timestamp: ts, UncompressedSize: 100,
			})
			statsBuilder.Append(stats.Stat{
				ObjectPath: object, SortSchema: "label:service_name",
				Labels:       map[string]string{"service_name": object},
				MinTimestamp: ts.UnixNano(), MaxTimestamp: ts.UnixNano() + 10,
				RowCount: 1, UncompressedSize: 100,
			})
		}
		require.NoError(t, objBuilder.Append(postingsBuilder))
		require.NoError(t, objBuilder.Append(statsBuilder))
	}
	obj, closer, err := objBuilder.Flush()
	require.NoError(t, err)
	defer closer.Close()
	require.NoError(t, uploadObjectToBucket(context.Background(), bucket, path, obj))
}

// indexObjectPaths returns the sorted, distinct log object paths that the
// postings and stats rows of the index at path reference.
func indexObjectPaths(t *testing.T, bucket objstore.Bucket, path string) (postingsPaths, statsPaths []string) {
	t.Helper()
	ctx := context.Background()
	for _, row := range readAllPostingsRowsFromBucket(ctx, t, bucket, path) {
		postingsPaths = append(postingsPaths, row.ObjectPath)
	}
	for _, row := range readStatsRowsFromBucket(ctx, t, bucket, path) {
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

	t.Run("returns an error when no objects are listed", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		buildFilterSourceIndex(t, bucket, sourcePath, 1<<20, map[string][]string{"acme": {"logs/a"}})

		_, err := newTestExecutorContext(t, bucket).doIndexFilter(ctx, &physical.IndexFilter{
			NodeID: ulid.Make(), Tenant: "acme", SourceIndexPath: sourcePath,
		})
		require.Error(t, err)
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
