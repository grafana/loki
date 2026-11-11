package metastore

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/go-kit/log"
	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"
	"github.com/thanos-io/objstore/providers/filesystem"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/index/indexobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/postings"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/stats"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
)

func TestListTableOfContentsWindows(t *testing.T) {
	first := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	second := first.Add(MetastoreWindowSize)

	for _, tc := range []struct {
		name   string
		bucket func(t *testing.T) objstore.Bucket
	}{
		{name: "in-memory", bucket: func(*testing.T) objstore.Bucket { return objstore.NewInMemBucket() }},
		{name: "filesystem", bucket: func(t *testing.T) objstore.Bucket {
			bucket, err := filesystem.NewBucket(t.TempDir())
			require.NoError(t, err)
			return bucket
		}},
	} {
		t.Run("returns the sorted windows and skips legacy shared ToCs in a "+tc.name+" bucket", func(t *testing.T) {
			bucket := tc.bucket(t)
			for _, name := range []string{
				TableOfContentsPath("tenant-a", second),
				TableOfContentsPath("tenant-b", first),
				TableOfContentsPath("tenant-a", first),
				"tocs/2025-01-01T00_00_00Z.toc",
			} {
				require.NoError(t, bucket.Upload(t.Context(), name, strings.NewReader("")))
			}

			windows, err := ListTableOfContentsWindows(t.Context(), bucket)
			require.NoError(t, err)
			require.Equal(t, []time.Time{first, second}, windows)
		})
	}

	t.Run("returns no windows when no ToC exists", func(t *testing.T) {
		windows, err := ListTableOfContentsWindows(t.Context(), objstore.NewInMemBucket())
		require.NoError(t, err)
		require.Empty(t, windows)
	})

	t.Run("returns an error for a ToC directory that is not a window", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		require.NoError(t, bucket.Upload(t.Context(), "tocs/not-a-window/tenant-a/toc.toc", strings.NewReader("")))

		_, err := ListTableOfContentsWindows(t.Context(), bucket)
		require.ErrorContains(t, err, "tocs/not-a-window/ is not a window")
	})
}

func TestReachableObjects(t *testing.T) {
	const tenant = "tenant-a"
	first := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	second := first.Add(MetastoreWindowSize)

	t.Run("returns the index objects of every window and the log objects that they reference", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		storeIndex(t, bucket, "indexes/aa/first", tenant, []string{"objects/00/log-1", "objects/00/log-2"}, []string{"objects/00/log-1"})
		storeIndex(t, bucket, "indexes/tenants/tenant-a/bb/second", tenant, []string{"objects/tenants/tenant-a/cc/log-3"}, nil)
		registerIndex(t, bucket, tenant, first, "indexes/aa/first")
		registerIndex(t, bucket, tenant, second, "indexes/tenants/tenant-a/bb/second")

		paths, err := ReachableObjects(t.Context(), bucket, tenant, 2)
		require.NoError(t, err)
		require.Equal(t, []string{
			"index/v0/indexes/aa/first",
			"index/v0/indexes/tenants/tenant-a/bb/second",
			"objects/00/log-1",
			"objects/00/log-2",
			"objects/tenants/tenant-a/cc/log-3",
		}, paths)
	})

	t.Run("returns the log objects of both the postings and the stats sections", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		storeIndex(t, bucket, "indexes/aa/index", tenant, []string{"objects/00/in-postings"}, []string{"objects/00/in-stats"})
		registerIndex(t, bucket, tenant, first, "indexes/aa/index")

		paths, err := ReachableObjects(t.Context(), bucket, tenant, 1)
		require.NoError(t, err)
		require.Equal(t, []string{"index/v0/indexes/aa/index", "objects/00/in-postings", "objects/00/in-stats"}, paths)
	})

	t.Run("returns an index object once when the ToCs of two windows point to it", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		storeIndex(t, bucket, "indexes/aa/index", tenant, []string{"objects/00/log"}, nil)
		registerIndex(t, bucket, tenant, first, "indexes/aa/index")
		registerIndex(t, bucket, tenant, second, "indexes/aa/index")

		paths, err := ReachableObjects(t.Context(), bucket, tenant, 1)
		require.NoError(t, err)
		require.Equal(t, []string{"index/v0/indexes/aa/index", "objects/00/log"}, paths)
	})

	t.Run("does not return the objects of another tenant or of legacy shared ToCs", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		storeIndex(t, bucket, "indexes/aa/mine", tenant, []string{"objects/00/mine"}, nil)
		storeIndex(t, bucket, "indexes/aa/other", "tenant-b", []string{"objects/00/other"}, nil)
		registerIndex(t, bucket, tenant, first, "indexes/aa/mine")
		registerIndex(t, bucket, "tenant-b", second, "indexes/aa/other")
		require.NoError(t, NewIndexBucket(bucket).Upload(t.Context(), "tocs/2025-01-01T12_00_00Z.toc", strings.NewReader("legacy")))

		paths, err := ReachableObjects(t.Context(), bucket, tenant, 1)
		require.NoError(t, err)
		require.Equal(t, []string{"index/v0/indexes/aa/mine", "objects/00/mine"}, paths)
	})

	t.Run("returns no paths when the tenant has no ToC", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		storeIndex(t, bucket, "indexes/aa/other", "tenant-b", []string{"objects/00/other"}, nil)
		registerIndex(t, bucket, "tenant-b", first, "indexes/aa/other")

		paths, err := ReachableObjects(t.Context(), bucket, tenant, 1)
		require.NoError(t, err)
		require.Empty(t, paths)
	})

	t.Run("returns an error and no paths when an index object that a ToC points to is missing", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		storeIndex(t, bucket, "indexes/aa/present", tenant, []string{"objects/00/log"}, nil)
		registerIndex(t, bucket, tenant, first, "indexes/aa/present")
		registerIndex(t, bucket, tenant, second, "indexes/aa/missing")

		paths, err := ReachableObjects(t.Context(), bucket, tenant, 1)
		require.ErrorContains(t, err, "opening index object indexes/aa/missing")
		require.Nil(t, paths)
	})

	t.Run("returns an error and no paths when a ToC cannot be decoded", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		require.NoError(t, NewIndexBucket(bucket).Upload(t.Context(), TableOfContentsPath(tenant, first), strings.NewReader("not a data object")))

		paths, err := ReachableObjects(t.Context(), bucket, tenant, 1)
		require.ErrorContains(t, err, "decoding ToC")
		require.Nil(t, paths)
	})

	t.Run("returns an error when the ToC listing fails", func(t *testing.T) {
		_, err := ReachableObjects(t.Context(), failingIterBucket{objstore.NewInMemBucket()}, tenant, 1)
		require.ErrorContains(t, err, "listing ToC windows")
	})

	for _, tc := range []struct {
		name         string
		tenant       string
		maxOpenFiles int
		wantErr      string
	}{
		{"returns an error for an empty tenant", "", 1, "tenant must not be empty"},
		{"returns an error for an unsafe tenant", "..", 1, "invalid tenant"},
		{"returns an error when max open files is zero", tenant, 0, "max open files"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ReachableObjects(t.Context(), objstore.NewInMemBucket(), tc.tenant, tc.maxOpenFiles)
			require.ErrorContains(t, err, tc.wantErr)
		})
	}
}

// storeIndex stores an index object of tenant at path in the index storage
// of bucket. It holds one postings section that references postingsPaths, and
// one stats section that references statsPaths if statsPaths is not empty.
func storeIndex(t *testing.T, bucket objstore.Bucket, path, tenant string, postingsPaths, statsPaths []string) {
	t.Helper()

	objBuilder := dataobj.NewBuilder(nil)
	postingsBuilder := postings.NewBuilder(nil, 0, 0, 1<<20)
	postingsBuilder.SetTenant(tenant)
	for i, objectPath := range postingsPaths {
		postingsBuilder.ObserveLabelPosting(postings.LabelObservation{
			ObjectPath: objectPath, ColumnName: "app", LabelValue: "foo", StreamID: int64(i + 1), Time: postings.At(time.Unix(10, 0)),
		})
	}
	require.NoError(t, objBuilder.Append(postingsBuilder))
	if len(statsPaths) > 0 {
		statsBuilder := stats.NewBuilder(nil, stats.ColumnarSectionEncoder(2048, 1000), streams.NewLayout(streams.DefaultShardLayout(), nil))
		statsBuilder.SetTenant(tenant)
		for _, objectPath := range statsPaths {
			statsBuilder.Append(stats.Stat{ObjectPath: objectPath})
		}
		require.NoError(t, objBuilder.Append(statsBuilder))
	}
	obj, closer, err := objBuilder.Flush()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, closer.Close()) })

	r, err := obj.Reader(t.Context())
	require.NoError(t, err)
	defer r.Close()
	require.NoError(t, NewIndexBucket(bucket).Upload(t.Context(), path, r))
}

// registerIndex adds the index object at path to the ToC of tenant for
// window.
func registerIndex(t *testing.T, bucket objstore.Bucket, tenant string, window time.Time, path string) {
	t.Helper()
	toc := NewTableOfContentsWriter(NewIndexBucket(bucket), DefaultTocWriterBackoffConfig, DefaultTocBuilderConfig, log.NewNopLogger(), NewTocWriterMetrics(nil), indexobj.NewBuilderMetrics(nil))
	require.NoError(t, toc.WriteEntry(t.Context(), tenant, TableOfContentsEntry{
		Path:      path,
		StartTime: window.Add(time.Minute),
		EndTime:   window.Add(2 * time.Minute),
	}))
}

var errListingFailed = errors.New("listing failed")

// failingIterBucket fails every listing.
type failingIterBucket struct {
	objstore.Bucket
}

func (failingIterBucket) Iter(context.Context, string, func(string) error, ...objstore.IterOption) error {
	return errListingFailed
}
