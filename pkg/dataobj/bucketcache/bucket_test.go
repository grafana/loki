package bucketcache

import (
	"bytes"
	"context"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"
	"go.uber.org/atomic"

	"github.com/grafana/loki/v3/pkg/dataobj/metadatacache"
	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/storage/chunk/cache"
)

func TestBucket_GetRange(t *testing.T) {
	t.Run("reads a cold range with one request", func(t *testing.T) {
		f := newFixture(t, 9<<20, DefaultSubrangeSize)

		got := f.read(t, "objects/a", 100_001, 8<<20)

		require.Equal(t, f.content[100_001:100_001+8<<20], got)
		require.Len(t, f.inner.spans(), 1, "the caller already split the range for parallel reads")
	})

	t.Run("reads each run of missing subranges with its own request", func(t *testing.T) {
		f := newFixture(t, 10_500, 1000)
		f.read(t, "objects/a", 2000, 1000)
		f.inner.reset()

		got := f.read(t, "objects/a", 0, 5000)

		require.Equal(t, f.content[:5000], got)
		require.ElementsMatch(t, []span{{offset: 0, length: 2000}, {offset: 3000, length: 2000}}, f.inner.spans())
		require.Zero(t, testutil.ToFloat64(f.bucket.metrics.overreadBytes))
	})

	t.Run("merges the closest runs when the missing subranges need more requests than the limit", func(t *testing.T) {
		f := newFixture(t, 10_500, 1000, func(cfg *Config) { cfg.MaxGetRangeSubrequests = 2 })
		f.read(t, "objects/a", 1000, 1000)
		f.read(t, "objects/a", 3000, 3000)
		f.inner.reset()

		got := f.read(t, "objects/a", 0, 7000)

		require.Equal(t, f.content[:7000], got)
		require.ElementsMatch(t, []span{{offset: 0, length: 3000}, {offset: 6000, length: 1000}}, f.inner.spans())
		require.Equal(t, 1000.0, testutil.ToFloat64(f.bucket.metrics.overreadBytes), "the merged request reads the cached subrange between its parts again")
	})

	t.Run("returns the bytes up to the end of the object for a range that ends past it", func(t *testing.T) {
		f := newFixture(t, 10_500, 1000)

		got := f.read(t, "objects/a", 10_000, 5000)

		require.Equal(t, f.content[10_000:], got)
	})

	t.Run("passes a read past the end of the object to the bucket without caching it", func(t *testing.T) {
		f := newFixture(t, 10_500, 1000)

		got := f.read(t, "objects/a", 11_000, 10)

		require.Empty(t, got)
		require.Equal(t, []span{{offset: 11_000, length: 10}}, f.inner.spans())
		require.Empty(t, f.subranges.GetKeys())
	})

	t.Run("does not cache an object outside the matcher", func(t *testing.T) {
		f := newFixture(t, 10_500, 1000)

		got := f.read(t, "other/b", 1500, 2000)
		_, err := f.bucket.Attributes(t.Context(), "other/b")
		require.NoError(t, err)
		_, err = f.bucket.Attributes(t.Context(), "other/b")
		require.NoError(t, err)

		require.Equal(t, f.content[1500:3500], got)
		require.Empty(t, f.subranges.GetKeys())
		require.Equal(t, int64(2), f.inner.attributes.Load())
	})

	t.Run("reads again a cached subrange that has the wrong length", func(t *testing.T) {
		f := newFixture(t, 10_500, 1000)
		f.read(t, "objects/a", 2000, 1000)
		keys := f.subranges.GetKeys()
		require.Len(t, keys, 1)
		require.NoError(t, f.subranges.Store(t.Context(), keys, [][]byte{[]byte("short")}))
		f.inner.reset()

		got := f.read(t, "objects/a", 2000, 1000)

		require.Equal(t, f.content[2000:3000], got)
		require.Len(t, f.inner.spans(), 1)
		require.Equal(t, 1.0, testutil.ToFloat64(f.bucket.metrics.corruptedEntries))
	})

	t.Run("reads again a cached subrange that holds the full key of another subrange", func(t *testing.T) {
		f := newFixture(t, 10_500, 1000)
		f.read(t, "objects/a", 2000, 1000)
		keys := f.subranges.GetKeys()
		require.Len(t, keys, 1)
		value := corrupt(t, f.subranges, keys[0])
		require.NoError(t, f.subranges.Store(t.Context(), keys, [][]byte{value}))
		f.inner.reset()

		got := f.read(t, "objects/a", 2000, 1000)

		require.Equal(t, f.content[2000:3000], got)
		require.Len(t, f.inner.spans(), 1)
		require.Equal(t, 1.0, testutil.ToFloat64(f.bucket.metrics.corruptedEntries))
	})

	t.Run("counts the subranges that a read asks for and the ones that hit", func(t *testing.T) {
		f := newFixture(t, 10_500, 1000)
		f.read(t, "objects/a", 2000, 1000)

		f.read(t, "objects/a", 1000, 3000)

		require.Equal(t, 4.0, testutil.ToFloat64(f.bucket.metrics.requestedSubranges))
		require.Equal(t, 1.0, testutil.ToFloat64(f.bucket.metrics.hitSubranges))
		require.Zero(t, testutil.ToFloat64(f.bucket.metrics.corruptedEntries))
	})

	t.Run("asks the bucket again for cached attributes that hold the full key of another object", func(t *testing.T) {
		f := newFixture(t, 10_500, 1000)
		f.read(t, "objects/a", 0, 1000)
		keys := f.attributesBackend.GetKeys()
		require.Len(t, keys, 1)
		value := corrupt(t, f.attributesBackend, keys[0])
		require.NoError(t, f.attributesBackend.Store(t.Context(), keys, [][]byte{value}))
		f.inner.attributes.Store(0)
		other, err := New(f.inner, f.subranges, f.attributesCache, Config{SubrangeSize: 1000}, "test", prometheus.NewRegistry())
		require.NoError(t, err)

		got, err := other.Attributes(t.Context(), "objects/a")

		require.NoError(t, err)
		require.Equal(t, int64(len(f.content)), got.Size)
		require.Equal(t, int64(1), f.inner.attributes.Load())
	})

	t.Run("asks the bucket for the attributes of an object once and returns them", func(t *testing.T) {
		f := newFixture(t, 10_500, 1000)
		want, err := f.inner.Bucket.Attributes(t.Context(), "objects/a")
		require.NoError(t, err)
		f.inner.attributes.Store(0)

		f.read(t, "objects/a", 0, 1000)
		f.read(t, "objects/a", 6000, 1000)
		got, err := f.bucket.Attributes(t.Context(), "objects/a")

		require.NoError(t, err)
		require.Equal(t, want.Size, got.Size)
		require.Equal(t, want.LastModified.UnixMilli(), got.LastModified.UnixMilli())
		require.Equal(t, int64(1), f.inner.attributes.Load())
	})

	t.Run("stores nothing when the bucket read fails", func(t *testing.T) {
		f := newFixture(t, 10_500, 1000)
		f.inner.failReads.Store(true)

		_, err := f.bucket.GetRange(t.Context(), "objects/a", 0, 1000)

		require.EqualError(t, err, "read failed")
		require.Empty(t, f.subranges.GetKeys())
	})

	t.Run("reads from the bucket when the cache fails", func(t *testing.T) {
		f := newFixture(t, 10_500, 1000)
		f.subranges.SetErr(errors.New("store failed"), errors.New("fetch failed"))

		got := f.read(t, "objects/a", 1500, 2000)

		require.Equal(t, f.content[1500:3500], got)
		require.Equal(t, 1.0, testutil.ToFloat64(f.bucket.metrics.errors.WithLabelValues(operationFetch)))
		require.Equal(t, 1.0, testutil.ToFloat64(f.bucket.metrics.errors.WithLabelValues(operationStore)))
	})
}

func TestIsCacheable(t *testing.T) {
	t.Run("accepts log objects and index objects", func(t *testing.T) {
		require.True(t, IsCacheable("objects/ab/cdef"))
		require.True(t, IsCacheable("objects/tenants/tenant/ab/cdef"))
		require.True(t, IsCacheable(metastore.IndexStoragePrefix+"/indexes/ab/cdef"))
		require.True(t, IsCacheable(metastore.IndexStoragePrefix+"/indexes/tenants/tenant/ab/cdef"))
	})

	t.Run("rejects the ToC", func(t *testing.T) {
		toc := metastore.IndexStoragePrefix + "/" + metastore.TableOfContentsPath("tenant", time.Unix(0, 0).UTC())

		require.False(t, IsCacheable(toc))
	})
}

func TestPlanSubrangeRuns(t *testing.T) {
	tests := []struct {
		name    string
		data    [][]byte
		maxRuns int
		want    []subrangeRun
	}{
		{
			name:    "returns no run when the cache holds every subrange",
			data:    subrangeData(4),
			maxRuns: 3,
		},
		{
			name:    "puts adjacent missing subranges in one run",
			data:    subrangeData(6, 1, 2, 3),
			maxRuns: 3,
			want:    []subrangeRun{{first: 1, last: 3}},
		},
		{
			name:    "keeps the runs apart when they fit the limit",
			data:    subrangeData(6, 0, 2, 5),
			maxRuns: 3,
			want:    []subrangeRun{{first: 0, last: 0}, {first: 2, last: 2}, {first: 5, last: 5}},
		},
		{
			name:    "keeps the runs apart when the limit is 0",
			data:    subrangeData(6, 0, 2, 5),
			maxRuns: 0,
			want:    []subrangeRun{{first: 0, last: 0}, {first: 2, last: 2}, {first: 5, last: 5}},
		},
		{
			name:    "merges the runs with the fewest cached subranges between them first",
			data:    subrangeData(10, 0, 2, 7),
			maxRuns: 2,
			want:    []subrangeRun{{first: 0, last: 2}, {first: 7, last: 7}},
		},
		{
			name:    "stops merging as soon as the limit holds",
			data:    subrangeData(10, 0, 2, 5, 9),
			maxRuns: 2,
			want:    []subrangeRun{{first: 0, last: 5}, {first: 9, last: 9}},
		},
		{
			name:    "widens the gap until one run remains",
			data:    subrangeData(10, 0, 2, 5, 9),
			maxRuns: 1,
			want:    []subrangeRun{{first: 0, last: 9}},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, planSubrangeRuns(tc.data, tc.maxRuns))
		})
	}
}

type fixture struct {
	bucket            *Bucket
	inner             *recordingBucket
	subranges         cache.MockCache
	attributesBackend cache.MockCache
	attributesCache   *metadatacache.Cache
	content           []byte
}

func newFixture(t *testing.T, size, subrangeSize int, options ...func(*Config)) *fixture {
	t.Helper()

	content := make([]byte, size)
	for i := range content {
		content[i] = byte(i*31 + i/251)
	}
	mem := objstore.NewInMemBucket()
	for _, name := range []string{"objects/a", "other/b"} {
		require.NoError(t, mem.Upload(t.Context(), name, bytes.NewReader(content)))
	}

	inner := &recordingBucket{Bucket: mem}
	subranges := cache.NewMockCache()
	attributesBackend := cache.NewMockCache()
	attributes := metadatacache.NewCache(attributesBackend, 0, metadatacache.KindObjectAttributes, metadatacache.NewMetrics(prometheus.NewRegistry()), nil)
	cfg := Config{SubrangeSize: subrangeSize, AttributesInMemoryMaxItems: 100, MaxGetRangeSubrequests: DefaultMaxGetRangeSubrequests}
	for _, option := range options {
		option(&cfg)
	}
	bucket, err := New(inner, subranges, attributes, cfg, "test", prometheus.NewRegistry())
	require.NoError(t, err)

	return &fixture{bucket: bucket, inner: inner, subranges: subranges, attributesBackend: attributesBackend, attributesCache: attributes, content: content}
}

// corrupt returns a copy of the cached value of key with one byte of its full key changed.
func corrupt(t *testing.T, backend cache.Cache, key string) []byte {
	t.Helper()

	_, values, _, err := backend.Fetch(t.Context(), []string{key})
	require.NoError(t, err)
	require.Len(t, values, 1)
	value := bytes.Clone(values[0])
	value[0] ^= 0xff
	return value
}

func (f *fixture) read(t *testing.T, name string, offset, length int64) []byte {
	t.Helper()

	rc, err := f.bucket.GetRange(t.Context(), name, offset, length)
	require.NoError(t, err)
	got, err := io.ReadAll(rc)
	require.NoError(t, err)
	require.NoError(t, rc.Close())
	return got
}

type span struct{ offset, length int64 }

// recordingBucket records the GetRange calls that reach the bucket, and counts the Attributes calls.
type recordingBucket struct {
	objstore.Bucket

	attributes atomic.Int64
	failReads  atomic.Bool

	mtx   sync.Mutex
	calls []span
}

func (b *recordingBucket) GetRange(ctx context.Context, name string, offset, length int64) (io.ReadCloser, error) {
	b.mtx.Lock()
	b.calls = append(b.calls, span{offset: offset, length: length})
	b.mtx.Unlock()
	if b.failReads.Load() {
		return nil, errors.New("read failed")
	}
	return b.Bucket.GetRange(ctx, name, offset, length)
}

func (b *recordingBucket) Attributes(ctx context.Context, name string) (objstore.ObjectAttributes, error) {
	b.attributes.Add(1)
	return b.Bucket.Attributes(ctx, name)
}

func (b *recordingBucket) spans() []span {
	b.mtx.Lock()
	defer b.mtx.Unlock()
	return append([]span(nil), b.calls...)
}

func (b *recordingBucket) reset() {
	b.mtx.Lock()
	defer b.mtx.Unlock()
	b.calls = nil
}

// subrangeData returns the cached data of n subranges. The subranges in missing have no data.
func subrangeData(n int, missing ...int) [][]byte {
	data := make([][]byte, n)
	for i := range data {
		data[i] = []byte{}
	}
	for _, i := range missing {
		data[i] = nil
	}
	return data
}
