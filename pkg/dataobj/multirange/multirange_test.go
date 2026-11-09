package multirange

import (
	"bytes"
	"context"
	"io"
	"sync"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"
	"go.uber.org/atomic"

	"github.com/grafana/loki/v3/pkg/util/rangeio"
)

const objectSize = 4 << 20

func TestCoalescingBucket(t *testing.T) {
	t.Run("merges close ranges into one read and fills each buffer", func(t *testing.T) {
		f := newFixture(t)
		ranges := f.ranges(span{100, 10}, span{200, 10}, span{3 << 20, 50})

		require.NoError(t, f.bucket.MultiGetRange(t.Context(), "a", ranges))

		for _, r := range ranges {
			require.Equal(t, f.content[r.Offset:r.Offset+r.Len()], r.Data)
		}
		require.ElementsMatch(t, []span{{100, 110}, {3 << 20, 50}}, f.inner.reads())
	})

	t.Run("counts the bytes that the ranges need and the bytes of the gaps that it read", func(t *testing.T) {
		f := newFixture(t)

		require.NoError(t, f.bucket.MultiGetRange(t.Context(), "a", f.ranges(span{100, 10}, span{200, 10})))

		require.Equal(t, 20.0, testutil.ToFloat64(f.needed))
		require.Equal(t, 90.0, testutil.ToFloat64(f.overread))
	})

	t.Run("closes every body that it opens", func(t *testing.T) {
		f := newFixture(t)

		require.NoError(t, f.bucket.MultiGetRange(t.Context(), "a", f.ranges(span{100, 10}, span{3 << 20, 50})))
		require.Error(t, f.bucket.MultiGetRange(t.Context(), "a", f.ranges(span{objectSize - 10, 20})))

		require.NotZero(t, f.inner.opened.Load())
		require.Equal(t, f.inner.opened.Load(), f.inner.closed.Load())
	})
}

func TestRead(t *testing.T) {
	t.Run("calls MultiGetRange with the ranges unchanged when the bucket has it", func(t *testing.T) {
		bucket := &multiBucket{Bucket: objstore.NewInMemBucket()}
		ranges := []rangeio.Range{{Data: make([]byte, 10), Offset: 100}, {Data: make([]byte, 10), Offset: 200}}

		require.NoError(t, Read(t.Context(), bucket, "a", ranges))

		require.Equal(t, "a", bucket.name)
		require.Equal(t, ranges, bucket.ranges)
	})

	t.Run("reads the ranges with GetRange when the bucket lacks it", func(t *testing.T) {
		f := newFixture(t)
		ranges := f.ranges(span{100, 10}, span{200, 10})

		require.NoError(t, Read(t.Context(), f.inner, "a", ranges))

		require.Equal(t, f.content[200:210], ranges[1].Data)
	})
}

func TestNewPrefixedBucket(t *testing.T) {
	t.Run("reads the ranges of the prefixed name when the bucket has MultiGetRange", func(t *testing.T) {
		f := newFixture(t)
		prefixed, ok := NewPrefixedBucket(f.bucket, "p/").(Bucket)
		require.True(t, ok)
		ranges := f.ranges(span{100, 10})

		require.NoError(t, prefixed.MultiGetRange(t.Context(), "a", ranges))

		require.Equal(t, bytes.Repeat([]byte{7}, 10), ranges[0].Data)
	})

	t.Run("has no MultiGetRange when the bucket lacks it", func(t *testing.T) {
		_, ok := NewPrefixedBucket(objstore.NewInMemBucket(), "p").(Bucket)

		require.False(t, ok)
	})
}

type span struct{ offset, length int64 }

type fixture struct {
	bucket   Bucket
	inner    *recordingBucket
	needed   prometheus.Counter
	overread prometheus.Counter
	content  []byte
}

// newFixture returns a coalescing bucket over a bucket that holds the object "a", and the object "p/a" filled with 7.
func newFixture(t *testing.T) *fixture {
	t.Helper()

	content := make([]byte, objectSize)
	for i := range content {
		content[i] = byte(i*31 + i/251)
	}
	mem := objstore.NewInMemBucket()
	require.NoError(t, mem.Upload(t.Context(), "a", bytes.NewReader(content)))
	require.NoError(t, mem.Upload(t.Context(), "p/a", bytes.NewReader(bytes.Repeat([]byte{7}, objectSize))))

	inner := &recordingBucket{Bucket: mem}
	bucket := NewCoalescingBucket(inner, prometheus.NewRegistry())
	coalescing := bucket.(*coalescingBucket)
	return &fixture{bucket: bucket, inner: inner, needed: coalescing.neededBytes, overread: coalescing.overreadBytes, content: content}
}

func (f *fixture) ranges(spans ...span) []rangeio.Range {
	ranges := make([]rangeio.Range, len(spans))
	for i, s := range spans {
		ranges[i] = rangeio.Range{Data: make([]byte, s.length), Offset: s.offset}
	}
	return ranges
}

// multiBucket records the arguments of its MultiGetRange call.
type multiBucket struct {
	objstore.Bucket

	name   string
	ranges []rangeio.Range
}

func (b *multiBucket) MultiGetRange(_ context.Context, name string, ranges []rangeio.Range) error {
	b.name, b.ranges = name, ranges
	return nil
}

// recordingBucket records the GetRange calls that reach the bucket, and counts the bodies that it opens and closes.
type recordingBucket struct {
	objstore.Bucket

	opened atomic.Int64
	closed atomic.Int64

	mtx   sync.Mutex
	calls []span
}

func (b *recordingBucket) GetRange(ctx context.Context, name string, offset, length int64) (io.ReadCloser, error) {
	b.mtx.Lock()
	b.calls = append(b.calls, span{offset, length})
	b.mtx.Unlock()

	rc, err := b.Bucket.GetRange(ctx, name, offset, length)
	if err != nil {
		return nil, err
	}
	b.opened.Add(1)
	return &countingCloser{ReadCloser: rc, closed: &b.closed}, nil
}

// countingCloser counts the closes of a body.
type countingCloser struct {
	io.ReadCloser

	closed *atomic.Int64
}

func (c *countingCloser) Close() error {
	c.closed.Add(1)
	return c.ReadCloser.Close()
}

func (b *recordingBucket) reads() []span {
	b.mtx.Lock()
	defer b.mtx.Unlock()
	return append([]span(nil), b.calls...)
}
