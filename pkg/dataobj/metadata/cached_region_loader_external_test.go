// These tests are in package metadata_test because they use objtest, which imports pkg/dataobj, which
// imports metadata. A test in package metadata that imports objtest is an import cycle.

package metadata_test

import (
	"context"
	"encoding/binary"
	"errors"
	"testing"
	"time"

	"github.com/grafana/dskit/metrics"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/metadata"
	"github.com/grafana/loki/v3/pkg/storage/chunk/cache"
)

func TestCachedRegionLoader(t *testing.T) {
	obj := newTestObject(t)
	region := obj.raw[:obj.metadataRegionEnd]

	// newRegionLoader returns a loader that has its own registry, so a test reads only its own metrics.
	newRegionLoader := func(t *testing.T, backend cache.Cache, maxRegionBytes int64) (*metadata.CachedRegionLoader, *prometheus.Registry) {
		t.Helper()

		reg := prometheus.NewPedanticRegistry()
		return metadata.NewCachedRegionLoader(backend, metadata.CacheItemCodec{Zstd: true, MinRatio: 1.5}, maxRegionBytes, headBytes, reg, nil), reg
	}

	t.Run("loads the whole region from object storage with a head read and a tail read, and writes it to the cache", func(t *testing.T) {
		backend := newFakeCache()
		l, reg := newRegionLoader(t, backend, obj.metadataRegionEnd)
		req := obj.request(metadata.PrefetchSet{})

		got, err := l.Load(t.Context(), req)

		require.NoError(t, err)
		require.Equal(t, region, got)
		require.Equal(t, []readCall{{0, headBytes}, {headBytes, obj.metadataRegionEnd - headBytes}}, source(req).calls())
		require.NotEmpty(t, backend.entry("region"))
		require.Equal(t, regionLoaderMetrics{requests: 1, storageSuccesses: 1, storageDurations: 1, stores: 1}, gatherRegionLoaderMetrics(t, reg))
	})

	t.Run("exposes the load duration as a classic and a native histogram", func(t *testing.T) {
		l, reg := newRegionLoader(t, newFakeCache(), obj.metadataRegionEnd)
		_, err := l.Load(t.Context(), obj.request(metadata.PrefetchSet{}))
		require.NoError(t, err)

		mfm, err := metrics.NewMetricFamilyMapFromGatherer(reg)
		require.NoError(t, err)
		h, err := metrics.FindHistogramWithNameAndLabels(mfm, "loki_dataobj_metadata_prefetcher_region_load_duration_seconds", "source", "storage")

		require.NoError(t, err)
		require.NotEmpty(t, h.GetBucket(), "the histogram has classic buckets")
		require.NotEmpty(t, h.GetPositiveSpan(), "the histogram has native buckets")
	})

	t.Run("loads the whole region with one read when it knows the size", func(t *testing.T) {
		l, reg := newRegionLoader(t, newFakeCache(), obj.metadataRegionEnd)
		req := obj.request(metadata.PrefetchSet{})

		got, err := l.Load(t.Context(), req, metadata.WithRegionSizeHint(obj.metadataRegionEnd))

		require.NoError(t, err)
		require.Equal(t, region, got)
		require.Equal(t, []readCall{{0, obj.metadataRegionEnd}}, source(req).calls())
		require.Equal(t, regionLoaderMetrics{requests: 1, storageSuccesses: 1, storageDurations: 1, stores: 1}, gatherRegionLoaderMetrics(t, reg))
	})

	t.Run("reads the region from the cache when object storage is slow, and does not write it again", func(t *testing.T) {
		l, reg := newRegionLoader(t, newFakeCache(), obj.metadataRegionEnd)
		req := obj.request(metadata.PrefetchSet{})
		_, err := l.Load(t.Context(), req)
		require.NoError(t, err)
		source(req).block = true
		ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
		defer cancel()

		got, err := l.Load(ctx, req)

		require.NoError(t, err)
		require.Equal(t, region, got)
		require.Equal(t, regionLoaderMetrics{
			requests:         2,
			storageSuccesses: 1,
			storageDurations: 1,
			cacheSuccesses:   1,
			cacheDurations:   1,
			stores:           1,
		}, gatherRegionLoaderMetrics(t, reg))
	})

	t.Run("replaces a cached region that does not decode with the region from object storage", func(t *testing.T) {
		backend := newFakeCache()
		l, reg := newRegionLoader(t, backend, obj.metadataRegionEnd)
		req := obj.request(metadata.PrefetchSet{})
		_, err := l.Load(t.Context(), req)
		require.NoError(t, err)
		backend.corrupt("region")

		got, err := l.Load(t.Context(), req)
		require.NoError(t, err)
		require.Equal(t, region, got)

		source(req).block = true
		ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
		defer cancel()
		got, err = l.Load(ctx, req)
		require.NoError(t, err)
		require.Equal(t, region, got, "the cache holds a valid region again")
		require.Equal(t, regionLoaderMetrics{
			requests:         3,
			storageSuccesses: 2,
			storageDurations: 2,
			cacheSuccesses:   1,
			cacheDurations:   1,
			stores:           2,
		}, gatherRegionLoaderMetrics(t, reg))
	})

	t.Run("loads the region from object storage when the cache read fails", func(t *testing.T) {
		backend := newFakeCache()
		backend.failReads = true
		l, reg := newRegionLoader(t, backend, obj.metadataRegionEnd)

		got, err := l.Load(t.Context(), obj.request(metadata.PrefetchSet{}))

		require.NoError(t, err)
		require.Equal(t, region, got)
		require.Equal(t, regionLoaderMetrics{requests: 1, storageSuccesses: 1, storageDurations: 1, stores: 1}, gatherRegionLoaderMetrics(t, reg))
	})

	t.Run("returns the region and counts a failed store when the cache write fails", func(t *testing.T) {
		backend := newFakeCache()
		backend.failWrites = true
		l, reg := newRegionLoader(t, backend, obj.metadataRegionEnd)

		got, err := l.Load(t.Context(), obj.request(metadata.PrefetchSet{}))

		require.NoError(t, err)
		require.Equal(t, region, got)
		require.Equal(t, regionLoaderMetrics{requests: 1, storageSuccesses: 1, storageDurations: 1, stores: 1, storeFailures: 1}, gatherRegionLoaderMetrics(t, reg))
	})

	t.Run("returns the object storage error when the cache has no region and the object cannot be read", func(t *testing.T) {
		backend := newFakeCache()
		l, reg := newRegionLoader(t, backend, obj.metadataRegionEnd)
		req := obj.request(metadata.PrefetchSet{})
		readErr := errors.New("object storage is down")
		source(req).err = readErr

		_, err := l.Load(t.Context(), req)

		require.ErrorIs(t, err, readErr)
		require.Zero(t, backend.sets())
		require.Equal(t, regionLoaderMetrics{requests: 1, failures: 1}, gatherRegionLoaderMetrics(t, reg))
	})

	t.Run("fails the load when closing the object storage reader fails", func(t *testing.T) {
		backend := newFakeCache()
		l, reg := newRegionLoader(t, backend, obj.metadataRegionEnd)
		req := obj.request(metadata.PrefetchSet{})
		closeErr := errors.New("close failed")
		source(req).closeErr = closeErr

		_, err := l.Load(t.Context(), req)

		require.ErrorIs(t, err, closeErr)
		require.Zero(t, backend.sets())
		require.Equal(t, regionLoaderMetrics{requests: 1, failures: 1}, gatherRegionLoaderMetrics(t, reg))
	})

	t.Run("skips an object whose region is above the limit with one head read and no cache write", func(t *testing.T) {
		backend := newFakeCache()
		l, reg := newRegionLoader(t, backend, obj.metadataRegionEnd-1)
		req := obj.request(metadata.PrefetchSet{})

		_, err := l.Load(t.Context(), req)

		require.ErrorIs(t, err, metadata.ErrNotPrefetchable)
		require.Equal(t, []readCall{{0, headBytes}}, source(req).calls())
		require.Zero(t, backend.sets())
		require.Equal(t, regionLoaderMetrics{requests: 1, failures: 1}, gatherRegionLoaderMetrics(t, reg))
	})

	t.Run("skips an object whose known region size is above the limit without a read", func(t *testing.T) {
		backend := newFakeCache()
		l, reg := newRegionLoader(t, backend, obj.metadataRegionEnd-1)
		req := obj.request(metadata.PrefetchSet{})

		_, err := l.Load(t.Context(), req, metadata.WithRegionSizeHint(obj.metadataRegionEnd))

		require.ErrorIs(t, err, metadata.ErrNotPrefetchable)
		require.Empty(t, source(req).calls())
		require.Zero(t, backend.sets())
		require.Equal(t, regionLoaderMetrics{requests: 1, failures: 1}, gatherRegionLoaderMetrics(t, reg))
	})

	t.Run("skips an object whose file metadata does not fit in the head with one head read and no cache write", func(t *testing.T) {
		backend := newFakeCache()
		l, reg := newRegionLoader(t, backend, obj.metadataRegionEnd)
		req := obj.request(metadata.PrefetchSet{})
		head := append([]byte("DOBJ"), binary.LittleEndian.AppendUint32(nil, 4*headBytes)...)
		source(req).data = append(head, make([]byte, 2*headBytes)...)

		_, err := l.Load(t.Context(), req)

		require.ErrorIs(t, err, metadata.ErrNotPrefetchable)
		require.Equal(t, []readCall{{0, headBytes}}, source(req).calls())
		require.Zero(t, backend.sets())
		require.Equal(t, regionLoaderMetrics{requests: 1, failures: 1}, gatherRegionLoaderMetrics(t, reg))
	})
}

// source returns the object storage source of the request.
func source(req metadata.PrefetchRequest) *testSource {
	return req.Source.(*testSource)
}

// regionLoaderMetrics holds the values of the metrics of a CachedRegionLoader.
type regionLoaderMetrics struct {
	requests, failures               float64
	storageSuccesses, cacheSuccesses float64
	storageDurations, cacheDurations uint64
	stores, storeFailures            float64
}

func gatherRegionLoaderMetrics(t *testing.T, reg *prometheus.Registry) regionLoaderMetrics {
	t.Helper()

	const prefix = "loki_dataobj_metadata_prefetcher_region_"
	mfm, err := metrics.NewMetricFamilyMapFromGatherer(reg)
	require.NoError(t, err)
	durations := func(source string) uint64 {
		h, err := metrics.FindHistogramWithNameAndLabels(mfm, prefix+"load_duration_seconds", "source", source)
		require.NoError(t, err)
		return h.GetSampleCount()
	}
	return regionLoaderMetrics{
		requests:         mfm.SumCounters(prefix + "load_requests_total"),
		failures:         mfm.SumCounters(prefix + "load_failures_total"),
		storageSuccesses: counterWithLabels(t, mfm, prefix+"load_successes_total", "source", "storage"),
		cacheSuccesses:   counterWithLabels(t, mfm, prefix+"load_successes_total", "source", "cache"),
		storageDurations: durations("storage"),
		cacheDurations:   durations("cache"),
		stores:           mfm.SumCounters(prefix + "cache_stores_total"),
		storeFailures:    mfm.SumCounters(prefix + "cache_store_failures_total"),
	}
}
