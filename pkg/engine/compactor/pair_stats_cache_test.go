package compactor

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
	"github.com/thanos-io/objstore"
	"go.uber.org/atomic"

	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/indexpointers"
)

// countingBucket counts the reads of index objects, and fails them while
// fail is set.
type countingBucket struct {
	objstore.Bucket
	reads atomic.Int64
	fail  atomic.Bool
}

func newCountingBucket(bucket objstore.Bucket) *countingBucket {
	return &countingBucket{Bucket: bucket}
}

func (b *countingBucket) onRead(_ context.Context, name string) error {
	if !strings.HasPrefix(name, "indexes/") {
		return nil
	}
	b.reads.Add(1)
	if b.fail.Load() {
		return errors.New("read failed")
	}
	return nil
}

func (b *countingBucket) wrapped() objstore.Bucket {
	return indexReadBucket{Bucket: b.Bucket, onRead: b.onRead}
}

func TestLogSectionsCache(t *testing.T) {
	ctx := context.Background()
	newCache := func(t *testing.T) (*logSectionsCache, *coordinatorMetrics, *countingBucket, objstore.Bucket) {
		t.Helper()
		bucket := objstore.NewInMemBucket()
		seedPairIndex(ctx, t, bucket, "indexes/a", pairTestSchema, testLogObject{"logs/a", "a", "z"})
		seedPairIndex(ctx, t, bucket, "indexes/b", pairTestSchema, testLogObject{"logs/b", "a", "z"})
		metrics := newCoordinatorMetrics(prometheus.NewRegistry())
		counting := newCountingBucket(bucket)
		return newLogSectionsCache("acme", metrics), metrics, counting, counting.wrapped()
	}
	requests := func(metrics *coordinatorMetrics, result string) float64 {
		return testutil.ToFloat64(metrics.pairStatsCacheRequestsTotal.WithLabelValues("acme", result))
	}

	t.Run("a second read of an index run returns the cached read without a bucket read", func(t *testing.T) {
		cache, metrics, counting, bucket := newCache(t)

		first, err := cache.read(ctx, bucket, imWindow(), runOfPath("indexes/a"))
		require.NoError(t, err)
		readsAfterFirst := counting.reads.Load()
		second, err := cache.read(ctx, bucket, imWindow(), runOfPath("indexes/a"))
		require.NoError(t, err)

		require.NotZero(t, readsAfterFirst)
		require.Equal(t, readsAfterFirst, counting.reads.Load())
		require.Equal(t, first, second)
		require.Equal(t, float64(1), requests(metrics, statsCacheMiss))
		require.Equal(t, float64(1), requests(metrics, statsCacheHit))
	})

	t.Run("a failed read is not cached and the next read tries the bucket again", func(t *testing.T) {
		cache, metrics, counting, bucket := newCache(t)
		counting.fail.Store(true)

		_, err := cache.read(ctx, bucket, imWindow(), runOfPath("indexes/a"))
		require.Error(t, err)
		counting.fail.Store(false)
		_, err = cache.read(ctx, bucket, imWindow(), runOfPath("indexes/a"))
		require.NoError(t, err)

		require.Equal(t, float64(2), requests(metrics, statsCacheMiss))
	})

	t.Run("a hit returns a corrupt index error when the ToC lists the paths with another run length", func(t *testing.T) {
		cache, _, _, bucket := newCache(t)
		_, err := cache.read(ctx, bucket, imWindow(), runOfPath("indexes/a"))
		require.NoError(t, err)
		run := runOfEntry(indexpointers.IndexPointer{Path: "indexes/a", RunID: 1, RunLength: 2})

		_, err = cache.read(ctx, bucket, imWindow(), run)

		require.ErrorIs(t, err, errCorruptIndex)
	})

	t.Run("a ToC drops the cached reads of its window that it does not list and keeps the others", func(t *testing.T) {
		cache, metrics, _, bucket := newCache(t)
		_, err := cache.read(ctx, bucket, imWindow(), runOfPath("indexes/a"))
		require.NoError(t, err)
		_, err = cache.read(ctx, bucket, imWindow(), runOfPath("indexes/b"))
		require.NoError(t, err)
		require.Equal(t, float64(2), testutil.ToFloat64(metrics.pairStatsCacheIndexRuns.WithLabelValues("acme")))
		require.Equal(t, float64(4), testutil.ToFloat64(metrics.pairStatsCacheKeys.WithLabelValues("acme")))

		cache.dropUnlisted(imWindow(), []indexRun{runOfPath("indexes/b")})

		require.Equal(t, float64(1), testutil.ToFloat64(metrics.pairStatsCacheIndexRuns.WithLabelValues("acme")))
		require.Equal(t, float64(2), testutil.ToFloat64(metrics.pairStatsCacheKeys.WithLabelValues("acme")))
		_, err = cache.read(ctx, bucket, imWindow(), runOfPath("indexes/b"))
		require.NoError(t, err)
		require.Equal(t, float64(1), requests(metrics, statsCacheHit))
	})

	t.Run("a ToC drops no cached read of another window", func(t *testing.T) {
		cache, metrics, _, bucket := newCache(t)
		_, err := cache.read(ctx, bucket, imWindow(), runOfPath("indexes/a"))
		require.NoError(t, err)

		cache.dropUnlisted(imWindow().Add(-metastore.MetastoreWindowSize), nil)

		require.Equal(t, float64(1), testutil.ToFloat64(metrics.pairStatsCacheIndexRuns.WithLabelValues("acme")))
	})

	t.Run("a window without a ToC drops every cached read of the window", func(t *testing.T) {
		cache, metrics, _, bucket := newCache(t)
		_, err := cache.read(ctx, bucket, imWindow(), runOfPath("indexes/a"))
		require.NoError(t, err)

		cache.dropUnlisted(imWindow(), nil)

		require.Zero(t, testutil.ToFloat64(metrics.pairStatsCacheIndexRuns.WithLabelValues("acme")))
		require.Zero(t, testutil.ToFloat64(metrics.pairStatsCacheKeys.WithLabelValues("acme")))
	})

	t.Run("two reads of one index run that both miss count it once in the size", func(t *testing.T) {
		bucket := objstore.NewInMemBucket()
		seedPairIndex(ctx, t, bucket, "indexes/a", pairTestSchema, testLogObject{"logs/a", "a", "z"})
		metrics := newCoordinatorMetrics(prometheus.NewRegistry())
		cache := newLogSectionsCache("acme", metrics)
		var arrived atomic.Int32
		bothMissed := make(chan struct{})
		barrier := indexReadBucket{Bucket: bucket, onRead: func(context.Context, string) error {
			if n := arrived.Inc(); n == 2 {
				close(bothMissed)
			} else if n == 1 {
				<-bothMissed
			}
			return nil
		}}

		errs := make(chan error, 2)
		var reads sync.WaitGroup
		for range 2 {
			reads.Go(func() {
				_, err := cache.read(ctx, barrier, imWindow(), runOfPath("indexes/a"))
				errs <- err
			})
		}
		reads.Wait()
		require.NoError(t, <-errs)
		require.NoError(t, <-errs)

		require.Equal(t, float64(2), requests(metrics, statsCacheMiss))
		require.Equal(t, float64(1), testutil.ToFloat64(metrics.pairStatsCacheIndexRuns.WithLabelValues("acme")))
		require.Equal(t, float64(2), testutil.ToFloat64(metrics.pairStatsCacheKeys.WithLabelValues("acme")))
	})

	t.Run("a window that leaves the planned range drops its cached reads", func(t *testing.T) {
		cache, metrics, _, bucket := newCache(t)
		_, err := cache.read(ctx, bucket, imWindow(), runOfPath("indexes/a"))
		require.NoError(t, err)

		cache.dropWindowsExcept([]time.Time{imWindow().Add(metastore.MetastoreWindowSize)})

		require.Zero(t, testutil.ToFloat64(metrics.pairStatsCacheIndexRuns.WithLabelValues("acme")))
		require.Zero(t, testutil.ToFloat64(metrics.pairStatsCacheKeys.WithLabelValues("acme")))
	})
}

func TestIndexRunCacheKey(t *testing.T) {
	t.Run("returns the same key for the paths of an index run in any order", func(t *testing.T) {
		require.Equal(t, indexRunCacheKey(runOfPaths("indexes/b", "indexes/a")), indexRunCacheKey(runOfPaths("indexes/a", "indexes/b")))
	})

	t.Run("returns different keys for different index runs", func(t *testing.T) {
		require.NotEqual(t, indexRunCacheKey(runOfPaths("indexes/a")), indexRunCacheKey(runOfPaths("indexes/a", "indexes/b")))
	})
}

// tocFailingBucket fails the reads of ToCs while fail is set.
type tocFailingBucket struct {
	objstore.Bucket
	fail atomic.Bool
}

func (b *tocFailingBucket) Get(ctx context.Context, name string) (io.ReadCloser, error) {
	if b.fail.Load() && metastore.IsTableOfContentsPath(name) {
		return nil, errors.New("ToC read failed")
	}
	return b.Bucket.Get(ctx, name)
}

func (b *tocFailingBucket) GetRange(ctx context.Context, name string, offset, length int64) (io.ReadCloser, error) {
	if b.fail.Load() && metastore.IsTableOfContentsPath(name) {
		return nil, errors.New("ToC read failed")
	}
	return b.Bucket.GetRange(ctx, name, offset, length)
}
