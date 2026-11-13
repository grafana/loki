package metadata

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/klauspost/compress/zstd"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"golang.org/x/time/rate"

	"github.com/grafana/loki/v3/pkg/storage/chunk/cache"
	"github.com/grafana/loki/v3/pkg/util"
)

var (
	// errNotFound means the cache holds no region item.
	errNotFound = errors.New("group not found")

	// errRegionTooLarge means the metadata region is larger than the limit of the loader.
	errRegionTooLarge = errors.New("the metadata region is above the limit")
)

// Values of the source label of the load successes counter and of the load duration histogram.
const (
	sourceCache   = "cache"
	sourceStorage = "storage"
)

// LoadOption gives a hint to a CachedRegionLoader.
type LoadOption func(*loadOptions)

// loadOptions holds the hints of one load.
type loadOptions struct {
	// regionSize is the size of the region, when the caller knows it. It is nil otherwise.
	regionSize *int64
}

func newLoadOptions(opts []LoadOption) loadOptions {
	var o loadOptions
	for _, opt := range opts {
		opt(&o)
	}
	return o
}

// WithRegionSizeHint tells the loader the size of the region, as the file metadata of the object
// implies it. The region starts at offset 0, so the size is the whole range.
//
// With the size, the loader reads the region from object storage in one read, and not in two. The
// loader ignores a size that is not positive, and then works as it does without a hint.
func WithRegionSizeHint(size int64) LoadOption {
	return func(o *loadOptions) {
		if size > 0 {
			o.regionSize = &size
		}
	}
}

// CachedRegionLoader loads the whole metadata region of a data object: the bytes from offset 0 to the
// end of the metadata of all sections. It reads the region from the cache and from object storage at
// the same time. When object storage answers, it writes the region to the cache as the region item.
//
// The loader skips an object whose region is larger than maxRegionBytes. It then reads at most the
// head of the object, writes nothing to the cache, and returns an error that wraps ErrNotPrefetchable.
//
// It is safe for concurrent use.
type CachedRegionLoader struct {
	cache cache.Cache
	codec CacheItemCodec

	// encoder and decoder compress and decompress the region item.
	encoder *zstd.Encoder
	decoder *zstd.Decoder

	// maxRegionBytes is the largest region size that the loader accepts.
	maxRegionBytes int64

	// headBytes is the size of the first object storage read of an object whose region size is not
	// known. The header and the file metadata of the object must fit in it.
	headBytes int

	// logger receives the warnings of the loader, at most one every warnInterval, so a failing cache
	// does not flood the log.
	logger   log.Logger
	warnRate rate.Sometimes

	loadRequests       prometheus.Counter
	loadSuccesses      *prometheus.CounterVec
	loadDuration       *prometheus.HistogramVec
	loadFailures       prometheus.Counter
	cacheStores        prometheus.Counter
	cacheStoreFailures prometheus.Counter
}

// NewCachedRegionLoader returns a CachedRegionLoader that loads regions of at most maxRegionBytes
// from backend and from object storage, and writes them to backend. headBytes is the size of the
// first object storage read of an object whose region size is not known. The loader skips an object
// whose header and file metadata do not fit in it.
//
// The loader registers its counters on reg. The registerer and the logger may be nil.
//
// It returns an error when it cannot create the zstd encoder or decoder of the region item.
func NewCachedRegionLoader(backend cache.Cache, codec CacheItemCodec, maxRegionBytes int64, headBytes int, reg prometheus.Registerer, logger log.Logger) (*CachedRegionLoader, error) {
	if logger == nil {
		logger = log.NewNopLogger()
	}
	encoder, err := newZstdEncoder()
	if err != nil {
		return nil, fmt.Errorf("creating the zstd encoder: %w", err)
	}
	decoder, err := newZstdDecoder()
	if err != nil {
		return nil, fmt.Errorf("creating the zstd decoder: %w", err)
	}

	l := &CachedRegionLoader{
		cache:          backend,
		codec:          codec,
		encoder:        encoder,
		decoder:        decoder,
		maxRegionBytes: maxRegionBytes,
		headBytes:      headBytes,
		logger:         logger,
		warnRate:       rate.Sometimes{Interval: warnInterval},

		loadRequests: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "loki_dataobj_metadata_prefetcher_region_load_requests_total",
			Help: "Requests to load the whole metadata region of a data object.",
		}),
		loadSuccesses: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Name: "loki_dataobj_metadata_prefetcher_region_load_successes_total",
			Help: "Successful loads of the whole metadata region of a data object, by the source that answered: cache or storage.",
		}, []string{"source"}),
		loadDuration: promauto.With(reg).NewHistogramVec(prometheus.HistogramOpts{
			Name:                            "loki_dataobj_metadata_prefetcher_region_load_duration_seconds",
			Help:                            "Time to load the whole metadata region of a data object, by the source that answered: cache or storage. It counts the successful loads only, and not the write of the region to the cache.",
			Buckets:                         []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMaxBucketNumber:  100,
			NativeHistogramMinResetDuration: time.Hour,
		}, []string{"source"}),
		loadFailures: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "loki_dataobj_metadata_prefetcher_region_load_failures_total",
			Help: "Failed loads of the whole metadata region of a data object. A load that skips an object whose region is too large counts as failed.",
		}),
		cacheStores: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "loki_dataobj_metadata_prefetcher_region_cache_stores_total",
			Help: "Writes of the whole metadata region of a data object to the cache.",
		}),
		cacheStoreFailures: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "loki_dataobj_metadata_prefetcher_region_cache_store_failures_total",
			Help: "Writes of the whole metadata region of a data object to the cache that failed.",
		}),
	}

	// Create every series at 0, so a rate counts the first event.
	for _, source := range []string{sourceCache, sourceStorage} {
		l.loadSuccesses.WithLabelValues(source)
		l.loadDuration.WithLabelValues(source)
	}
	return l, nil
}

// Load returns the whole region of the object. It returns an error that wraps ErrNotPrefetchable
// when the region is larger than the limit of the loader. With a WithRegionSizeHint option, the
// loader knows the size of the region, and reads it from object storage in one read.
//
// When object storage answers, the loader writes the region item, so the next load of the object
// can read the region from the cache.
//
// The caller must not modify the region. When the cache answers with an item that is not compressed,
// the region aliases a buffer that the cache owns.
func (l *CachedRegionLoader) Load(ctx context.Context, req PrefetchRequest, opts ...LoadOption) (region []byte, err error) {
	start := time.Now()
	l.loadRequests.Inc()
	defer func() {
		if err != nil {
			l.loadFailures.Inc()
		}
	}()

	o := newLoadOptions(opts)
	if o.regionSize != nil {
		if err := l.checkRegionSize(*o.regionSize); err != nil {
			return nil, err
		}
	}

	region, fromCache, err := l.loadFromStorageOrCache(ctx, req, o.regionSize)
	if err != nil {
		return nil, err
	}

	source := sourceStorage
	if fromCache {
		source = sourceCache
	}
	l.loadSuccesses.WithLabelValues(source).Inc()
	l.loadDuration.WithLabelValues(source).Observe(time.Since(start).Seconds())

	if !fromCache {
		l.storeRegion(ctx, req.Path, region)
	}
	return region, nil
}

// storeRegion writes the region item. A failed write does not fail the load.
func (l *CachedRegionLoader) storeRegion(ctx context.Context, path string, region []byte) {
	unhashedKey, hashedKey := prefetchedRangesID{Kind: kindRegion}.cacheKey(path)

	l.cacheStores.Inc()
	if err := l.cache.Store(ctx, []string{hashedKey}, [][]byte{encodeCacheItem(region, unhashedKey, l.codec, l.encoder)}); err != nil {
		l.cacheStoreFailures.Inc()
		l.warn("data object metadata region store failed", path, err)
	}
}

// loadFromStorageOrCache reads the whole region from the cache and from object storage at the same
// time. It returns the first read that succeeds, or the object storage error when both fail.
// fromCache is true when the cache answered.
//
// size is the size of the region when the caller knows it from the file metadata, and nil otherwise.
func (l *CachedRegionLoader) loadFromStorageOrCache(ctx context.Context, req PrefetchRequest, size *int64) (region []byte, fromCache bool, err error) {
	// A miss in the cache is not an answer, so the object storage read decides the result then.
	// Both reads stop when this function returns.
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	type answer struct {
		region    []byte
		fromCache bool
		err       error
	}
	// The channel holds both answers, so the slower read never blocks after this function returns.
	answers := make(chan answer, 2)
	read := func(fromCache bool, fn func() ([]byte, error)) {
		region, err := fn()
		answers <- answer{region, fromCache, err}
	}
	go read(true, func() ([]byte, error) { return l.loadFromCache(ctx, req.Path) })
	go read(false, func() ([]byte, error) { return l.loadFromStorage(ctx, req, size) })

	var storageErr error
	for range 2 {
		a := <-answers
		if a.err == nil {
			return a.region, a.fromCache, nil
		}
		if !a.fromCache {
			storageErr = a.err
		}
	}
	return nil, false, storageErr
}

// loadFromCache reads the region item from the cache. It returns errNotFound when there is none, and
// an error when the item does not decode. It warns about an item that is malformed or corrupt.
func (l *CachedRegionLoader) loadFromCache(ctx context.Context, path string) ([]byte, error) {
	unhashedKey, hashedKey := prefetchedRangesID{Kind: kindRegion}.cacheKey(path)
	found, bufs, _, err := l.cache.Fetch(ctx, []string{hashedKey})
	if len(found) == 0 {
		if err != nil {
			// A cancelled context also fails the read, when the other read wins the race.
			if ctx.Err() == nil {
				l.warn("data object metadata region read failed", path, err)
			}
			return nil, err
		}
		return nil, errNotFound
	}

	region, err := decodeCacheItem(bufs[0], unhashedKey, maxCachedBytes(l.maxRegionBytes), l.decoder)
	if err != nil {
		// A healthy cache can hold an item of another key or version, so do not warn about it. The load
		// counts it as a miss, and the storage read replaces the item.
		if !errors.Is(err, errKeyMismatch) && !errors.Is(err, errVersionMismatch) {
			l.warn("data object metadata region in the cache is invalid", path, err)
		}
		return nil, err
	}
	return region, nil
}

// loadFromStorage reads the region from object storage. Without the size it first reads the head, and
// then the rest. The head must hold the header and the file metadata. When it does not, the object is
// not prefetchable.
func (l *CachedRegionLoader) loadFromStorage(ctx context.Context, req PrefetchRequest, size *int64) ([]byte, error) {
	if size != nil {
		region, err := readRange(ctx, req.Source, 0, *size)
		if err != nil {
			return nil, err
		}
		if int64(len(region)) != *size {
			return nil, fmt.Errorf("read %d bytes of the metadata region, want %d", len(region), *size)
		}
		return region, nil
	}

	// Read the head of the dataobj.
	head, err := readRange(ctx, req.Source, 0, int64(l.headBytes))
	if err != nil {
		return nil, err
	}

	md, startOffset, err := decodeHeaderAndFileMetadata(head)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrNotPrefetchable, err)
	}

	metadataSize, err := metadataRegionSize(md, startOffset)
	if err != nil {
		return nil, err
	}
	if err := l.checkRegionSize(metadataSize); err != nil {
		return nil, err
	}
	if metadataSize <= int64(len(head)) {
		return bytes.Clone(head[:metadataSize]), nil
	}

	// The head read didn't contain the full metadata region. Read the rest of it
	// issuing a subsequent request.
	tail, err := readRange(ctx, req.Source, int64(len(head)), metadataSize-int64(len(head)))
	if err != nil {
		return nil, err
	}
	region := append(head, tail...)
	if int64(len(region)) != metadataSize {
		return nil, fmt.Errorf("read %d bytes of the metadata region, want %d", len(region), metadataSize)
	}

	return region, nil
}

// checkRegionSize returns an error that wraps ErrNotPrefetchable when a region of at least size
// bytes is larger than the limit of the loader.
func (l *CachedRegionLoader) checkRegionSize(size int64) error {
	if size > l.maxRegionBytes {
		return fmt.Errorf("%w: %w: it has at least %d bytes, and the limit is %d", ErrNotPrefetchable, errRegionTooLarge, size, l.maxRegionBytes)
	}
	return nil
}

// warn writes a warning about the object at path, unless it wrote one in the last warnInterval.
func (l *CachedRegionLoader) warn(msg, path string, err error) {
	l.warnRate.Do(func() { level.Warn(l.logger).Log("msg", msg, "path", path, "err", err) })
}

// readRange reads up to length bytes from offset. It returns fewer bytes when the object ends first,
// and an error when no byte is left. When the read succeeds, it returns the error of Close.
func readRange(ctx context.Context, src DataobjSourceReader, offset, length int64) (data []byte, err error) {
	rc, err := src.ReadRange(ctx, offset, length)
	if err != nil {
		return nil, fmt.Errorf("reading the metadata region: %w", err)
	}
	defer util.CloseAndHandleError(rc, &err)

	buf := make([]byte, length)
	n, err := io.ReadFull(rc, buf)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) {
		return nil, fmt.Errorf("reading the metadata region: %w", err)
	}
	return buf[:n], nil
}
