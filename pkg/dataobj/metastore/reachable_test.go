package metastore

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"testing/iotest"
	"time"

	"github.com/go-kit/log"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"
	"github.com/thanos-io/objstore/providers/filesystem"
	"go.uber.org/atomic"

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
		t.Run("returns the sorted windows and skips legacy shared ToCs in the "+tc.name+" bucket", func(t *testing.T) {
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

	for _, kind := range []struct {
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
		t.Run("returns the index objects of every window and the log objects that they reference in the "+kind.name+" bucket", func(t *testing.T) {
			bucket := kind.bucket(t)
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
	}

	t.Run("returns the log objects of an index object that the index builder wrote", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		builder, err := indexobj.NewBuilder(tenant, DefaultTocBuilderConfig, nil, indexobj.NewBuilderMetrics(nil))
		require.NoError(t, err)
		require.NoError(t, builder.SetLayout(streams.NewLayout(streams.DefaultShardLayout(), nil)))
		require.NoError(t, builder.AppendStat("objects/00/in-stats", 0, 0, nil, time.Unix(10, 0), time.Unix(20, 0), 1, 1))
		builder.ObserveLabelPosting(postings.LabelObservation{ObjectPath: "objects/00/in-postings", ColumnName: "app", LabelValue: "foo", StreamID: 1, Time: postings.At(time.Unix(10, 0))})
		require.NoError(t, builder.AppendWatermark("topic", 0, 10))
		obj, closer, err := builder.Flush()
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, closer.Close()) })
		uploadObject(t, bucket, "indexes/aa/built", obj)
		registerIndex(t, bucket, tenant, first, "indexes/aa/built")

		paths, err := ReachableObjects(t.Context(), bucket, tenant, 1)
		require.NoError(t, err)
		require.Equal(t, []string{"index/v0/indexes/aa/built", "objects/00/in-postings", "objects/00/in-stats"}, paths)
	})

	t.Run("returns the log objects of an index object with stats and no postings", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		storeIndex(t, bucket, "indexes/aa/index", tenant, nil, []string{"objects/00/in-stats"})
		registerIndex(t, bucket, tenant, first, "indexes/aa/index")

		paths, err := ReachableObjects(t.Context(), bucket, tenant, 1)
		require.NoError(t, err)
		require.Equal(t, []string{"index/v0/indexes/aa/index", "objects/00/in-stats"}, paths)
	})

	t.Run("returns only the index object when it references no log object", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		builder, err := indexobj.NewBuilder(tenant, DefaultTocBuilderConfig, nil, indexobj.NewBuilderMetrics(nil))
		require.NoError(t, err)
		require.NoError(t, builder.AppendWatermark("topic", 0, 10))
		obj, closer, err := builder.Flush()
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, closer.Close()) })
		uploadObject(t, bucket, "indexes/aa/empty", obj)
		registerIndex(t, bucket, tenant, first, "indexes/aa/empty")

		paths, err := ReachableObjects(t.Context(), bucket, tenant, 1)
		require.NoError(t, err)
		require.Equal(t, []string{"index/v0/indexes/aa/empty"}, paths)
	})

	t.Run("returns every log object when the paths fill several read batches", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		logPaths := make([]string, 2500)
		for i := range logPaths {
			logPaths[i] = fmt.Sprintf("objects/00/log-%05d", i)
		}
		storeIndex(t, bucket, "indexes/aa/index", tenant, logPaths, nil)
		registerIndex(t, bucket, tenant, first, "indexes/aa/index")

		paths, err := ReachableObjects(t.Context(), bucket, tenant, 1)
		require.NoError(t, err)
		require.Equal(t, append([]string{"index/v0/indexes/aa/index"}, logPaths...), paths)
	})

	t.Run("returns a log object once when two index objects reference it", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		storeIndex(t, bucket, "indexes/aa/one", tenant, []string{"objects/00/shared"}, nil)
		storeIndex(t, bucket, "indexes/aa/two", tenant, []string{"objects/00/shared"}, nil)
		registerIndex(t, bucket, tenant, first, "indexes/aa/one")
		registerIndex(t, bucket, tenant, first, "indexes/aa/two")

		paths, err := ReachableObjects(t.Context(), bucket, tenant, 2)
		require.NoError(t, err)
		require.Equal(t, []string{"index/v0/indexes/aa/one", "index/v0/indexes/aa/two", "objects/00/shared"}, paths)
	})

	t.Run("opens at most max open files index objects at the same time", func(t *testing.T) {
		files := objstore.NewInMemBucket()
		for i := range 8 {
			indexPath := fmt.Sprintf("indexes/aa/index-%d", i)
			storeIndex(t, files, indexPath, tenant, []string{fmt.Sprintf("objects/00/log-%d", i)}, nil)
			registerIndex(t, files, tenant, first, indexPath)
		}
		bucket := &concurrencyBucket{Bucket: files, prefix: "index/v0/indexes/"}

		_, err := ReachableObjects(t.Context(), bucket, tenant, 3)
		require.NoError(t, err)
		require.LessOrEqual(t, bucket.peak.Load(), int32(3))
		require.Greater(t, bucket.peak.Load(), int32(1))
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

	t.Run("returns an error and no paths when a ToC read fails with an error other than not found", func(t *testing.T) {
		files := objstore.NewInMemBucket()
		storeIndex(t, files, "indexes/aa/index", tenant, []string{"objects/00/log"}, nil)
		registerIndex(t, files, tenant, first, "indexes/aa/index")
		bucket := &failingGetBucket{Bucket: files, name: "index/v0/" + TableOfContentsPath(tenant, first)}

		paths, err := ReachableObjects(t.Context(), bucket, tenant, 1)
		require.ErrorIs(t, err, errGetFailed)
		require.Nil(t, paths)
	})

	t.Run("returns an error and no paths when a ToC read fails partway", func(t *testing.T) {
		files := objstore.NewInMemBucket()
		storeIndex(t, files, "indexes/aa/index", tenant, []string{"objects/00/log"}, nil)
		registerIndex(t, files, tenant, first, "indexes/aa/index")
		bucket := &failingGetBucket{Bucket: files, name: "index/v0/" + TableOfContentsPath(tenant, first), partial: true}

		paths, err := ReachableObjects(t.Context(), bucket, tenant, 1)
		require.ErrorIs(t, err, errGetFailed)
		require.Nil(t, paths)
	})

	t.Run("returns the index read error and not a context error when a later ToC read sees the cancellation", func(t *testing.T) {
		files := objstore.NewInMemBucket()
		registerIndex(t, files, tenant, first, "indexes/aa/broken")
		storeIndex(t, files, "indexes/aa/later", tenant, []string{"objects/00/log"}, nil)
		registerIndex(t, files, tenant, second, "indexes/aa/later")
		bucket := &cancelAwareBucket{Bucket: files, failIndex: "index/v0/indexes/aa/broken", blockToC: "index/v0/" + TableOfContentsPath(tenant, second)}

		_, err := ReachableObjects(t.Context(), bucket, tenant, 1)
		require.ErrorContains(t, err, "opening index object indexes/aa/broken")
		require.NotErrorIs(t, err, context.Canceled)
	})

	t.Run("returns an error and no paths when a ToC holds the sections of another tenant", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		storeIndex(t, bucket, "indexes/aa/other", "tenant-b", []string{"objects/00/other"}, nil)
		registerIndex(t, bucket, "tenant-b", first, "indexes/aa/other")
		indexBucket := NewIndexBucket(bucket)
		r, err := indexBucket.Get(t.Context(), TableOfContentsPath("tenant-b", first))
		require.NoError(t, err)
		require.NoError(t, indexBucket.Upload(t.Context(), TableOfContentsPath(tenant, first), r))
		require.NoError(t, r.Close())

		paths, err := ReachableObjects(t.Context(), bucket, tenant, 1)
		require.ErrorContains(t, err, `ToC holds tenant "tenant-b"`)
		require.Nil(t, paths)
	})

	t.Run("returns an error and no paths when an index object holds a section of an unknown type", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		objBuilder := dataobj.NewBuilder(nil)
		streamsBuilder := streams.NewBuilder(streams.NewMetrics(), 1<<20, 1000, streams.DefaultShardLayout())
		streamsBuilder.SetTenant(tenant)
		streamsBuilder.Record(labels.FromStrings("app", "foo"), time.Unix(10, 0), 1)
		require.NoError(t, objBuilder.Append(streamsBuilder))
		obj, closer, err := objBuilder.Flush()
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, closer.Close()) })
		uploadObject(t, bucket, "indexes/aa/unknown", obj)
		registerIndex(t, bucket, tenant, first, "indexes/aa/unknown")

		paths, err := ReachableObjects(t.Context(), bucket, tenant, 1)
		require.ErrorContains(t, err, "has unknown type")
		require.Nil(t, paths)
	})

	for _, tc := range []struct {
		name    string
		path    string
		wantErr string
	}{
		{"returns an error and no paths for a log object path with a leading slash", "/objects/00/log", "not a clean relative path"},
		{"returns an error and no paths for a log object path that is not clean", "objects//00/log", "not a clean relative path"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bucket := objstore.NewInMemBucket()
			storeIndex(t, bucket, "indexes/aa/index", tenant, []string{tc.path}, nil)
			registerIndex(t, bucket, tenant, first, "indexes/aa/index")

			paths, err := ReachableObjects(t.Context(), bucket, tenant, 1)
			require.ErrorContains(t, err, tc.wantErr)
			require.Nil(t, paths)
		})
	}

	for _, tc := range []struct {
		name      string
		indexPath string
		wantErr   string
	}{
		{"returns an error and no paths for an index pointer outside indexes/", "index/v0/indexes/aa/index", "not in indexes/"},
		{"returns an error and no paths for an index pointer that is not clean", "indexes/aa/../bb/index", "not a clean relative path"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bucket := objstore.NewInMemBucket()
			registerIndex(t, bucket, tenant, first, tc.indexPath)

			paths, err := ReachableObjects(t.Context(), bucket, tenant, 1)
			require.ErrorContains(t, err, tc.wantErr)
			require.Nil(t, paths)
		})
	}

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
// of bucket. It holds a postings section that references postingsPaths, and a
// stats section that references statsPaths. It leaves out a section whose
// paths are empty.
func storeIndex(t *testing.T, bucket objstore.Bucket, path, tenant string, postingsPaths, statsPaths []string) {
	t.Helper()

	objBuilder := dataobj.NewBuilder(nil)
	if len(postingsPaths) > 0 {
		postingsBuilder := postings.NewBuilder(nil, 0, 0, 1<<20)
		postingsBuilder.SetTenant(tenant)
		for i, objectPath := range postingsPaths {
			postingsBuilder.ObserveLabelPosting(postings.LabelObservation{
				ObjectPath: objectPath, ColumnName: "app", LabelValue: "foo", StreamID: int64(i + 1), Time: postings.At(time.Unix(10, 0)),
			})
		}
		require.NoError(t, objBuilder.Append(postingsBuilder))
	}
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
	uploadObject(t, bucket, path, obj)
}

// uploadObject stores obj at path in the index storage of bucket.
func uploadObject(t *testing.T, bucket objstore.Bucket, path string, obj *dataobj.Object) {
	t.Helper()
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

var errGetFailed = errors.New("get failed")

// failingGetBucket fails the Get of the object name. If partial is set, the
// Get succeeds, and the read fails after the first bytes.
type failingGetBucket struct {
	objstore.Bucket
	name    string
	partial bool
}

func (b *failingGetBucket) Get(ctx context.Context, name string) (io.ReadCloser, error) {
	if name != b.name {
		return b.Bucket.Get(ctx, name)
	}
	if !b.partial {
		return nil, errGetFailed
	}
	r, err := b.Bucket.Get(ctx, name)
	if err != nil {
		return nil, err
	}
	return struct {
		io.Reader
		io.Closer
	}{io.MultiReader(io.LimitReader(r, 8), iotest.ErrReader(errGetFailed)), r}, nil
}

// cancelAwareBucket fails every read of the index object failIndex. A Get of
// the ToC blockToC waits until its context ends and returns the context
// error.
type cancelAwareBucket struct {
	objstore.Bucket
	failIndex string
	blockToC  string
}

func (b *cancelAwareBucket) Get(ctx context.Context, name string) (io.ReadCloser, error) {
	switch name {
	case b.failIndex:
		return nil, errGetFailed
	case b.blockToC:
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return b.Bucket.Get(ctx, name)
}

func (b *cancelAwareBucket) GetRange(ctx context.Context, name string, off, length int64) (io.ReadCloser, error) {
	if name == b.failIndex {
		return nil, errGetFailed
	}
	return b.Bucket.GetRange(ctx, name, off, length)
}

func (b *cancelAwareBucket) Attributes(ctx context.Context, name string) (objstore.ObjectAttributes, error) {
	if name == b.failIndex {
		return objstore.ObjectAttributes{}, errGetFailed
	}
	return b.Bucket.Attributes(ctx, name)
}

// concurrencyBucket records the peak number of reads of objects below prefix
// that run at the same time. Each read waits a little, so that reads of
// different objects overlap.
type concurrencyBucket struct {
	objstore.Bucket
	prefix string
	active atomic.Int32
	peak   atomic.Int32
}

func (b *concurrencyBucket) GetRange(ctx context.Context, name string, off, length int64) (io.ReadCloser, error) {
	if strings.HasPrefix(name, b.prefix) {
		n := b.active.Inc()
		defer b.active.Dec()
		for {
			peak := b.peak.Load()
			if n <= peak || b.peak.CompareAndSwap(peak, n) {
				break
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	return b.Bucket.GetRange(ctx, name, off, length)
}
