package bucketcache

import (
	"bytes"
	"context"
	"errors"
	"io"
	"sync"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"
	"go.uber.org/atomic"

	"github.com/grafana/loki/v3/pkg/dataobj/metadatacache"
	"github.com/grafana/loki/v3/pkg/dataobj/multirange"
	"github.com/grafana/loki/v3/pkg/storage/chunk/cache"
	"github.com/grafana/loki/v3/pkg/util/rangeio"
)

func TestBucket_GetRange(t *testing.T) {
	t.Run("reads a cold range with one call to the bucket", func(t *testing.T) {
		f := newFixture(t, 9<<20, DefaultSubrangeSize)

		got := f.read(t, "objects/a", 100_001, 8<<20)

		require.Equal(t, f.content[100_001:100_001+8<<20], got)
		require.Equal(t, int64(1), f.inner.multiCalls.Load())
	})

	t.Run("passes only the missing subranges to the bucket", func(t *testing.T) {
		f := newFixture(t, 10_500, 1000)
		f.read(t, "objects/a", 2000, 1000)
		f.inner.reset()

		got := f.read(t, "objects/a", 0, 5000)

		require.Equal(t, f.content[:5000], got)
		require.ElementsMatch(t, []span{{offset: 0, length: 1000}, {offset: 1000, length: 1000}, {offset: 3000, length: 1000}, {offset: 4000, length: 1000}}, f.inner.spans())
		require.Equal(t, int64(1), f.inner.multiCalls.Load())
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

	t.Run("fails with an unexpected EOF, not an EOF, when the bucket returns an EOF", func(t *testing.T) {
		f := newFixture(t, 10_500, 1000)
		f.inner.eof.Store(true)

		_, err := f.bucket.GetRange(t.Context(), "objects/a", 0, 2000)

		require.ErrorIs(t, err, io.ErrUnexpectedEOF)
		require.NotErrorIs(t, err, io.EOF)
		require.Empty(t, f.subranges.GetKeys())
	})

	t.Run("fails with an unexpected EOF, not an EOF, when the attributes lookup fails with an EOF", func(t *testing.T) {
		f := newFixture(t, 10_500, 1000)
		f.inner.attributesErr = errors.Join(errors.New("head failed"), io.EOF)

		_, err := f.bucket.GetRange(t.Context(), "objects/a", 0, 1000)

		require.ErrorIs(t, err, io.ErrUnexpectedEOF)
		require.NotErrorIs(t, err, io.EOF)
	})

	t.Run("keeps the memory of the hits out of the pool until the caller closes the reader", func(t *testing.T) {
		f := newFixture(t, 10_500, 1000)
		f.read(t, "objects/a", 2000, 2000)
		allocating := &allocatingCache{Cache: f.subranges}
		bucket, err := newCachedBucket(f.inner, &Backend{Config: Config{SubrangeSize: 1000}, Subranges: allocating, Attributes: f.attributesCache}, kindLogs, "test", prometheus.NewRegistry())
		require.NoError(t, err)

		rc, err := bucket.GetRange(t.Context(), "objects/a", 2000, 2000)
		require.NoError(t, err)

		require.NotZero(t, allocating.allocations.Load(), "the hits come from the allocator")
		require.Nil(t, bucket.slabs.Get(), "the open reader still holds its slabs")
		got, err := io.ReadAll(rc)
		require.NoError(t, err)
		require.Equal(t, f.content[2000:4000], got)
		require.NoError(t, rc.Close())
	})
}

func TestBucket_Attributes(t *testing.T) {
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
		require.Equal(t, 1, f.attributesBackend.KeysRequested(), "the memory table serves the other lookups")
	})

	t.Run("reads the attributes from the cache backend when the memory table does not hold them", func(t *testing.T) {
		f := newFixture(t, 10_500, 1000)
		f.read(t, "objects/a", 0, 1000)
		f.inner.attributes.Store(0)
		other, err := newCachedBucket(f.inner, &Backend{Config: Config{SubrangeSize: 1000}, Subranges: f.subranges, Attributes: f.attributesCache}, kindLogs, "test", prometheus.NewRegistry())
		require.NoError(t, err)

		got, err := other.Attributes(t.Context(), "objects/a")

		require.NoError(t, err)
		require.Equal(t, int64(len(f.content)), got.Size)
		require.Zero(t, f.inner.attributes.Load())
	})

	t.Run("asks the bucket again for cached attributes that hold the full key of another object", func(t *testing.T) {
		f := newFixture(t, 10_500, 1000)
		f.read(t, "objects/a", 0, 1000)
		keys := f.attributesBackend.GetKeys()
		require.Len(t, keys, 1)
		value := corrupt(t, f.attributesBackend, keys[0])
		require.NoError(t, f.attributesBackend.Store(t.Context(), keys, [][]byte{value}))
		f.inner.attributes.Store(0)
		other, err := newCachedBucket(f.inner, &Backend{Config: Config{SubrangeSize: 1000}, Subranges: f.subranges, Attributes: f.attributesCache}, kindLogs, "test", prometheus.NewRegistry())
		require.NoError(t, err)

		got, err := other.Attributes(t.Context(), "objects/a")

		require.NoError(t, err)
		require.Equal(t, int64(len(f.content)), got.Size)
		require.Equal(t, int64(1), f.inner.attributes.Load())
	})
}

func TestBucket_MultiGetRange(t *testing.T) {
	t.Run("reads and stores only the subranges that the ranges need", func(t *testing.T) {
		f := newFixture(t, 10_500, 1000)

		got := f.multiRead(t, "objects/a", span{offset: 1500, length: 1000}, span{offset: 8200, length: 100})

		require.Equal(t, [][]byte{f.content[1500:2500], f.content[8200:8300]}, got)
		require.ElementsMatch(t, []span{{offset: 1000, length: 1000}, {offset: 2000, length: 1000}, {offset: 8000, length: 1000}}, f.inner.spans())
		require.Equal(t, int64(1), f.inner.multiCalls.Load())
		require.Len(t, f.subranges.GetKeys(), 3)
	})

	t.Run("looks up a subrange that two ranges share once", func(t *testing.T) {
		f := newFixture(t, 10_500, 1000)

		got := f.multiRead(t, "objects/a", span{offset: 1100, length: 100}, span{offset: 1500, length: 100})

		require.Equal(t, [][]byte{f.content[1100:1200], f.content[1500:1600]}, got)
		require.Equal(t, 1, f.subranges.KeysRequested())
		require.Equal(t, []span{{offset: 1000, length: 1000}}, f.inner.spans())
	})

	t.Run("serves cached ranges without a call to the bucket", func(t *testing.T) {
		f := newFixture(t, 10_500, 1000)
		f.multiRead(t, "objects/a", span{offset: 1500, length: 1000}, span{offset: 8200, length: 100})
		f.inner.reset()

		got := f.multiRead(t, "objects/a", span{offset: 8200, length: 100}, span{offset: 1500, length: 1000})

		require.Equal(t, [][]byte{f.content[8200:8300], f.content[1500:2500]}, got)
		require.Empty(t, f.inner.spans())
		require.Zero(t, f.inner.multiCalls.Load())
	})

	t.Run("passes a call with a negative offset to the bucket without caching it", func(t *testing.T) {
		f := newFixture(t, 10_500, 1000)

		_, err := f.tryMultiRead(t, "objects/a", span{offset: 1500, length: 100}, span{offset: -1, length: 10})

		require.Error(t, err)
		require.Equal(t, int64(1), f.inner.multiCalls.Load())
		require.Empty(t, f.subranges.GetKeys())
	})

	t.Run("reads nothing for a call with only empty ranges", func(t *testing.T) {
		f := newFixture(t, 10_500, 1000)

		got := f.multiRead(t, "objects/a", span{offset: 1500, length: 0})

		require.Equal(t, [][]byte{{}}, got)
		require.Zero(t, f.inner.attributes.Load())
		require.Zero(t, f.inner.multiCalls.Load())
	})

	t.Run("passes a call with a range past the end of the object to the bucket without caching it", func(t *testing.T) {
		f := newFixture(t, 10_500, 1000)

		_, err := f.tryMultiRead(t, "objects/a", span{offset: 1500, length: 100}, span{offset: 10_400, length: 200})

		require.ErrorIs(t, err, io.ErrUnexpectedEOF)
		require.Equal(t, []span{{offset: 1500, length: 100}, {offset: 10_400, length: 200}}, f.inner.spans())
		require.Empty(t, f.subranges.GetKeys())
	})
}

type fixture struct {
	bucket            *cachedBucket
	inner             *recordingBucket
	subranges         cache.MockCache
	attributesBackend cache.MockCache
	attributesCache   *metadatacache.Cache
	content           []byte
}

func newFixture(t *testing.T, size, subrangeSize int) *fixture {
	t.Helper()

	content := make([]byte, size)
	for i := range content {
		content[i] = byte(i*31 + i/251)
	}
	mem := objstore.NewInMemBucket()
	require.NoError(t, mem.Upload(t.Context(), "objects/a", bytes.NewReader(content)))

	inner := &recordingBucket{Bucket: mem}
	subranges := cache.NewMockCache()
	attributesBackend := cache.NewMockCache()
	attributes := metadatacache.NewCache(attributesBackend, 0, metadatacache.KindObjectAttributes, metadatacache.NewMetrics(prometheus.NewRegistry()), nil)
	cfg := Config{SubrangeSize: subrangeSize, AttributesInMemoryMaxItems: 100}
	bucket, err := newCachedBucket(inner, &Backend{Config: cfg, Subranges: subranges, Attributes: attributes}, kindLogs, "test", prometheus.NewRegistry())
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

// multiRead fills one range for each span with MultiGetRange, and returns the bytes of each range.
func (f *fixture) multiRead(t *testing.T, name string, spans ...span) [][]byte {
	t.Helper()

	got, err := f.tryMultiRead(t, name, spans...)
	require.NoError(t, err)
	return got
}

func (f *fixture) tryMultiRead(t *testing.T, name string, spans ...span) ([][]byte, error) {
	t.Helper()

	ranges := make([]rangeio.Range, len(spans))
	for i, s := range spans {
		ranges[i] = rangeio.Range{Data: make([]byte, s.length), Offset: s.offset}
	}
	err := f.bucket.MultiGetRange(t.Context(), name, ranges)

	got := make([][]byte, len(ranges))
	for i, r := range ranges {
		got[i] = r.Data
	}
	return got, err
}

type span struct{ offset, length int64 }

// recordingBucket records the ranges that reach the bucket, and counts the MultiGetRange and Attributes calls.
type recordingBucket struct {
	objstore.Bucket

	attributes atomic.Int64
	multiCalls atomic.Int64
	failReads  atomic.Bool
	eof        atomic.Bool // MultiGetRange returns io.EOF.

	attributesErr error // Set it before the first call.

	mtx   sync.Mutex
	calls []span
}

func (b *recordingBucket) GetRange(ctx context.Context, name string, offset, length int64) (io.ReadCloser, error) {
	b.mtx.Lock()
	b.calls = append(b.calls, span{offset: offset, length: length})
	b.mtx.Unlock()
	return b.Bucket.GetRange(ctx, name, offset, length)
}

func (b *recordingBucket) MultiGetRange(ctx context.Context, name string, ranges []rangeio.Range) error {
	b.multiCalls.Add(1)
	b.mtx.Lock()
	for _, r := range ranges {
		b.calls = append(b.calls, span{offset: r.Offset, length: r.Len()})
	}
	b.mtx.Unlock()

	if b.failReads.Load() {
		return errors.New("read failed")
	} else if b.eof.Load() {
		return io.EOF
	}
	for _, r := range ranges {
		if r.Offset < 0 {
			return errors.New("negative offset")
		}
	}
	return multirange.Read(ctx, b.Bucket, name, ranges)
}

func (b *recordingBucket) Attributes(ctx context.Context, name string) (objstore.ObjectAttributes, error) {
	b.attributes.Add(1)
	if b.attributesErr != nil {
		return objstore.ObjectAttributes{}, b.attributesErr
	}
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
	b.multiCalls.Store(0)
}

// allocatingCache copies the values that it returns into memory from the allocator option, as
// memcached does.
type allocatingCache struct {
	cache.Cache

	allocations atomic.Int64
}

func (c *allocatingCache) Fetch(ctx context.Context, keys []string, opts ...cache.Option) ([]string, [][]byte, []string, error) {
	found, bufs, missing, err := c.Cache.Fetch(ctx, keys)

	var o cache.Options
	for _, opt := range opts {
		opt(&o)
	}
	if o.Allocator == nil {
		return found, bufs, missing, err
	}
	for i, buf := range bufs {
		allocated := *o.Allocator.Get(len(buf))
		copy(allocated, buf)
		bufs[i] = allocated
		c.allocations.Add(1)
	}
	return found, bufs, missing, err
}
