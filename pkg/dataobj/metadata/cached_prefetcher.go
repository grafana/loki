package metadata

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/klauspost/compress/zstd"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"golang.org/x/sync/singleflight"
	"golang.org/x/time/rate"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/filemd"
	"github.com/grafana/loki/v3/pkg/storage/chunk/cache"
)

// maxNamedColumns is how many columns a lookup asks for by name. A query that names more reads the
// whole region.
const maxNamedColumns = 64

// warnInterval is the shortest time between two warnings of the prefetcher, or of its region loader,
// about the objects that they read. It keeps a failing cache from flooding the log.
const warnInterval = 10 * time.Second

var (
	errInvalidRanges = errors.New("the ranges are not sorted or overlap")

	// errCacheUnavailable means the cache lookup failed.
	errCacheUnavailable = errors.New("the cache lookup failed")
)

// Values of the outcome label of the successes counter and of the duration histogram.
const (
	outcomeCacheHit         = "cache_hit"
	outcomeCachePartialMiss = "cache_partial_miss"
	outcomeCacheMiss        = "cache_miss"
)

// Values of the reason label of the failures counter.
const (
	reasonCanceled         = "canceled"
	reasonTimeout          = "timeout"
	reasonCacheUnavailable = "cache_unavailable"
	reasonTooLarge         = "too_large"
	reasonHeadTooSmall     = "head_too_small"
	reasonUnsupported      = "unsupported"
	reasonCorrupted        = "corrupted"
	reasonStorage          = "storage_error"
)

// failureReasons lists every value of the reason label.
var failureReasons = []string{
	reasonCanceled,
	reasonTimeout,
	reasonCacheUnavailable,
	reasonTooLarge,
	reasonHeadTooSmall,
	reasonUnsupported,
	reasonCorrupted,
	reasonStorage,
}

// CachedPrefetcher is a Prefetcher that keeps the metadata of an object in a cache, as one cache item for
// each group of prefetched ranges that a query can read.
//
// It is safe for concurrent use.
type CachedPrefetcher struct {
	cache cache.Cache

	// maxRegionBytes is the largest metadata region that the prefetcher handles. A cache item holds at
	// most maxCachedBytes(maxRegionBytes) raw bytes.
	maxRegionBytes int64

	cfg   PrefetcherConfig
	codec CacheItemCodec

	// logger receives the warnings of the prefetcher, at most one every warnInterval.
	logger   log.Logger
	warnRate rate.Sometimes

	// regions loads the region of an object after a miss. It caches the region item. The prefetcher
	// caches the groups.
	regions *CachedRegionLoader

	// encoder and decoder are the zstd coders of the region loader. The prefetcher shares them, so a
	// process holds one encoder and one decoder.
	encoder *zstd.Encoder
	decoder *zstd.Decoder

	loads singleflight.Group

	requests      prometheus.Counter
	successes     *prometheus.CounterVec
	failures      *prometheus.CounterVec
	duration      *prometheus.HistogramVec
	lookupKeys    prometheus.Histogram
	responseBytes prometheus.Histogram
}

// NewCachedPrefetcher returns a CachedPrefetcher that keeps cache items in backend. It registers its
// counters, and the counters of its region loader, on reg. The registerer and the logger may be nil.
//
// The max region size of cfg is the largest metadata region that the prefetcher handles. The metadata
// region of an object is the bytes from offset 0 to the end of the metadata of all sections. The
// prefetcher skips an object with a larger region: it reads at most the head of the object, writes
// nothing to the cache, and returns an error that wraps ErrNotPrefetchable.
//
// maxItemBytes is the largest cache item that backend holds. The max region size, plus
// cacheItemOverheadBytes, must fit in it, so a region item fits in a cache item. NewCachedPrefetcher
// returns an error otherwise. A value of zero or less means that backend has no limit. The limit is
// then the largest cache item that the prefetcher can decode.
func NewCachedPrefetcher(backend cache.Cache, maxItemBytes int64, cfg PrefetcherConfig, reg prometheus.Registerer, logger log.Logger) (*CachedPrefetcher, error) {
	if maxItemBytes <= 0 || maxItemBytes > maxZstdDecodedBytes {
		maxItemBytes = maxZstdDecodedBytes
	}
	maxRegionBytes := int64(cfg.MaxRegionBytes)
	if maxRegionBytes+cacheItemOverheadBytes > maxItemBytes {
		return nil, fmt.Errorf("the max region size of the metadata prefetcher, %d bytes, and the entry overhead of %d bytes do not fit in a cache item of %d bytes", maxRegionBytes, cacheItemOverheadBytes, maxItemBytes)
	}
	if logger == nil {
		logger = log.NewNopLogger()
	}

	codec := CacheItemCodec{Zstd: cfg.Compression == compressionZstd, MinRatio: cfg.MinCompressionRatio}
	regions, err := NewCachedRegionLoader(backend, codec, maxRegionBytes, cfg.HeadBytes, reg, logger)
	if err != nil {
		return nil, err
	}

	p := &CachedPrefetcher{
		cache:          backend,
		maxRegionBytes: maxRegionBytes,
		cfg:            cfg,
		codec:          codec,
		logger:         logger,
		warnRate:       rate.Sometimes{Interval: warnInterval},
		regions:        regions,
		encoder:        regions.encoder,
		decoder:        regions.decoder,

		requests: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "loki_dataobj_metadata_prefetcher_requests_total",
			Help: "Requests to prefetch the metadata of a data object.",
		}),
		successes: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Name: "loki_dataobj_metadata_prefetcher_successes_total",
			Help: "Prefetch requests that returned metadata, by cache outcome: cache_hit when the cache had every group the request needs, cache_partial_miss when it had some of them, cache_miss when it had none.",
		}, []string{"outcome"}),
		failures: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Name: "loki_dataobj_metadata_prefetcher_failures_total",
			Help: "Prefetch requests that returned an error, by reason: canceled, timeout, cache_unavailable for a failed cache lookup, too_large for a metadata region above the limit, head_too_small for a file metadata that does not fit in the head read, unsupported for an object that a newer writer made, corrupted for metadata that is not valid, and storage_error for a failed object storage read.",
		}, []string{"reason"}),
		duration: promauto.With(reg).NewHistogramVec(prometheus.HistogramOpts{
			Name:                            "loki_dataobj_metadata_prefetcher_duration_seconds",
			Help:                            "Time that a successful prefetch request took, by cache outcome.",
			Buckets:                         []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMaxBucketNumber:  100,
			NativeHistogramMinResetDuration: time.Hour,
		}, []string{"outcome"}),
		lookupKeys: promauto.With(reg).NewHistogram(prometheus.HistogramOpts{
			Name:                            "loki_dataobj_metadata_prefetcher_cache_lookup_keys",
			Help:                            "Number of cache keys that a prefetch request looks up in the cache. It counts the file metadata group, and then one key for each other group of ranges, or one key for the whole region.",
			Buckets:                         prometheus.ExponentialBuckets(1, 2, 8),
			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMaxBucketNumber:  100,
			NativeHistogramMinResetDuration: time.Hour,
		}),
		responseBytes: promauto.With(reg).NewHistogram(prometheus.HistogramOpts{
			Name:                            "loki_dataobj_metadata_prefetcher_response_bytes",
			Help:                            "Bytes of metadata that a successful prefetch request returned.",
			Buckets:                         prometheus.ExponentialBuckets(1024, 4, 9),
			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMaxBucketNumber:  100,
			NativeHistogramMinResetDuration: time.Hour,
		}),
	}

	// Create every series at 0, so a rate counts the first event.
	for _, outcome := range []string{outcomeCacheHit, outcomeCachePartialMiss, outcomeCacheMiss} {
		p.successes.WithLabelValues(outcome)
		p.duration.WithLabelValues(outcome)
	}
	for _, reason := range failureReasons {
		p.failures.WithLabelValues(reason)
	}
	return p, nil
}

// Stop stops the cache.
func (p *CachedPrefetcher) Stop() { p.cache.Stop() }

// Prefetch returns the metadata of the object that the prefetch set names.
//
// It returns an error that wraps ErrNotPrefetchable when the layout is invalid, the region is too
// large, or the cache fails and cannot answer the prefetch set.
func (p *CachedPrefetcher) Prefetch(ctx context.Context, req PrefetchRequest) (res PrefetchResponse, err error) {
	var (
		start   = time.Now()
		outcome = outcomeCacheMiss
	)

	p.requests.Inc()
	defer func() {
		if err != nil {
			p.failures.WithLabelValues(failureReason(err)).Inc()
			return
		}
		p.successes.WithLabelValues(outcome).Inc()
		p.duration.WithLabelValues(outcome).Observe(time.Since(start).Seconds())
		p.responseBytes.Observe(float64(res.PrefetchedRanges.size()))
	}()

	// A prefetch set that names too many columns reads every column.
	prefetchSet := req.PrefetchSet
	if len(prefetchSet.MetadataNames) > maxNamedColumns {
		prefetchSet.AllMetadata = true
	}

	// One fetch returns everything the prefetch set reads. On a miss, the prefetcher reads the whole
	// region and fills the cache.
	found, lookupErr := p.fetchMetadataRangesFromCache(ctx, req.Path, prefetchSet)
	if err := ctx.Err(); err != nil {
		return PrefetchResponse{}, err
	}
	if found.satisfies(prefetchSet) {
		hit := PrefetchResponse{PrefetchedRanges: prefetchSet.prefetchedRangesFor(found.ranges, found.region)}
		if hit.PrefetchedRanges.Valid() {
			outcome = outcomeCacheHit
			return hit, nil
		}

		// Entries that overlap each other come from a faulty writer. Treat the lookup as cold, so the
		// fill replaces every group.
		p.warn("data object metadata prefetcher groups do not fit together", req.Path, errInvalidRanges)
		found = newFetchResult(prefetchSet.prefetchedRangesIDs())
	}
	if !found.isEmpty() {
		outcome = outcomeCachePartialMiss
	}

	// The whole region costs more to read than the direct read does, and a failed cache would drop
	// the fill. So a failed lookup sends the caller to the direct read.
	if lookupErr != nil {
		return PrefetchResponse{}, fmt.Errorf("%w: %w: %w", ErrNotPrefetchable, errCacheUnavailable, lookupErr)
	}

	loaded, err := p.fetchAndFillMetadataRangesCacheSingleFlight(ctx, req, found)
	if err != nil {
		return PrefetchResponse{}, err
	}

	ranges := prefetchSet.prefetchedRangesFor(loaded.groups.Groups, loaded.region)
	if !prefetchSet.AllMetadata {
		// The ranges alias the whole region. A copy stops a small range from keeping the whole region in
		// memory.
		for i := range ranges {
			ranges[i].Data = bytes.Clone(ranges[i].Data)
		}
	}
	return PrefetchResponse{PrefetchedRanges: ranges}, nil
}

// fetchMetadataRangesFromCache asks the cache for the groups that the prefetch set reads. A group
// that does not decode, or is not valid, counts as missing. It returns the groups it found with the
// error of the cache, if any. The result is never nil. It holds the groups that the cache returned, even when the cache also returned an error.
func (p *CachedPrefetcher) fetchMetadataRangesFromCache(ctx context.Context, path string, prefetchSet PrefetchSet) (*fetchResult, error) {
	var (
		ids        = prefetchSet.prefetchedRangesIDs()
		f          = newFetchResult(ids)
		maxRawSize = maxCachedBytes(p.maxRegionBytes)
	)

	p.lookupKeys.Observe(float64(len(ids)))

	type askedGroup struct {
		id          prefetchedRangesID
		unhashedKey string
	}
	keys := make([]string, len(ids))
	askedByKey := make(map[string]askedGroup, len(ids))
	for i, id := range ids {
		unhashedKey, hashedKey := id.cacheKey(path)
		keys[i] = hashedKey
		askedByKey[hashedKey] = askedGroup{id: id, unhashedKey: unhashedKey}
	}

	found, bufs, _, fetchErr := p.cache.Fetch(ctx, keys)
	if fetchErr != nil && ctx.Err() == nil {
		p.warn("data object metadata prefetcher lookup failed", path, fetchErr)
	}

	for i, key := range found {
		group, ok := askedByKey[key]
		if !ok {
			// A cache returns only the keys that it was asked for, so this is a bug of the cache.
			p.warn("data object metadata prefetcher cache returned a key that the lookup did not ask for", path, fmt.Errorf("unexpected key %q", key))
			continue
		}
		if err := f.addCacheItem(group.id, group.unhashedKey, bufs[i], maxRawSize, p.decoder); err != nil {
			p.warn("data object metadata prefetcher group did not decode", path, err)
		}
	}
	return f, fetchErr
}

// fetchAndFillMetadataRangesCacheSingleFlight reads the whole region and fills the cache.
// Concurrent calls for one object share one run.
func (p *CachedPrefetcher) fetchAndFillMetadataRangesCacheSingleFlight(ctx context.Context, req PrefetchRequest, f *fetchResult) (*loadedRegion, error) {
	ch := p.loads.DoChan(req.Path, func() (any, error) {
		// The run outlives the caller that starts it, so it keeps the values of ctx and drops its
		// cancellation. Callers that wait for it can still leave.
		loadCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), p.cfg.LoadTimeout)
		defer cancel()
		return p.fetchAndFillMetadataRangesCache(loadCtx, req, f)
	})

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case res := <-ch:
		if res.Err != nil {
			return nil, res.Err
		}
		return res.Val.(*loadedRegion), nil
	}
}

// fetchAndFillMetadataRangesCache loads the whole region, splits it into groups, and
// writes to the cache the groups that f did not find. The loader writes the region item.
func (p *CachedPrefetcher) fetchAndFillMetadataRangesCache(ctx context.Context, req PrefetchRequest, f *fetchResult) (*loadedRegion, error) {
	var opts []LoadOption
	if f.fileMetadata != nil {
		// With the file metadata from the lookup, the loader knows the size of the region.
		size, err := metadataRegionSize(f.fileMetadata, f.startOffset)
		if err != nil {
			return nil, err
		}
		opts = append(opts, WithRegionSizeHint(size))
	}

	region, err := p.regions.Load(ctx, req, opts...)
	if err != nil {
		return nil, err
	}

	md, startOffset, err := decodeHeaderAndFileMetadata(region)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrNotPrefetchable, err)
	}

	groups, err := splitMetadataRegionToGroups(md, startOffset, region)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrNotPrefetchable, err)
	}

	p.fillMetadataRangesCache(ctx, req.Path, groups, f)
	return &loadedRegion{region: region, groups: groups}, nil
}

// fillMetadataRangesCache writes to the cache the groups of an object that the lookup did not find.
func (p *CachedPrefetcher) fillMetadataRangesCache(ctx context.Context, path string, groups prefetchedRangesGroups, f *fetchResult) {
	// A lookup always asks for the file metadata group. Without a valid one, the cache does not know the
	// object. A cold lookup then writes every group that it did not find, asked or not, so a later query
	// with another prefetch set hits. Any other lookup writes only the missing groups that it asked
	// for, because it cannot tell whether the others exist.
	cold := f.fileMetadata == nil
	missing := func(id prefetchedRangesID) bool {
		if _, asked := f.asked[id]; !asked && !cold {
			return false
		}
		return !f.has(id)
	}

	var (
		keys   []string
		values [][]byte
	)
	add := func(id prefetchedRangesID, raw []byte) {
		unhashedKey, hashedKey := id.cacheKey(path)
		keys = append(keys, hashedKey)
		values = append(values, encodeCacheItem(raw, unhashedKey, p.codec, p.encoder))
	}

	for _, id := range []prefetchedRangesID{{Kind: kindFileMetadata}, {Kind: kindCore}, {Kind: kindMessage}} {
		if !missing(id) {
			continue
		}
		var columnNames map[string]struct{}
		if id.Kind == kindCore {
			columnNames = groups.ColumnNames
		}
		add(id, marshalPrefetchedRanges(groups.Groups[id], columnNames))
	}
	for name := range groups.ColumnNames {
		id := prefetchedRangesID{Kind: kindColumn, ColumnName: name}
		if missing(id) {
			add(id, marshalPrefetchedRanges(groups.Groups[id], nil))
		}
	}

	// A lookup for all metadata asks for the file metadata group and the region item. When it found the
	// group, nothing is left to write, because the loader writes the region item.
	if len(keys) == 0 {
		return
	}

	// A failed write costs a later miss. It does not fail the load.
	if err := p.cache.Store(ctx, keys, values); err != nil {
		p.warn("data object metadata prefetcher fill failed", path, err)
	}
}

// warn writes a warning about the object at path, unless it wrote one in the last warnInterval.
func (p *CachedPrefetcher) warn(msg, path string, err error) {
	p.warnRate.Do(func() { level.Warn(p.logger).Log("msg", msg, "path", path, "err", err) })
}

// loadedRegion is the metadata region of an object, and the groups that the region splits into.
type loadedRegion struct {
	region []byte
	groups prefetchedRangesGroups
}

// fetchResult holds the groups that a fetch from the cache found for one object.
type fetchResult struct {
	// asked holds the groups that the fetch asked for.
	asked map[prefetchedRangesID]struct{}

	// ranges holds the decoded groups, except the region. A group that exists and holds no range
	// maps to an empty slice, so has can tell it from a miss.
	ranges map[prefetchedRangesID]PrefetchedRanges

	// columnNames lists the structured metadata columns of the object, as the core group records them. It
	// is nil when the core group is missing.
	columnNames map[string]struct{}

	// region is the whole region, when the fetch asked for it and found it.
	region []byte

	// fileMetadata is the file metadata that the file metadata group holds, and startOffset is the offset
	// where its sections start. fileMetadata is nil when the group is missing or does not decode.
	fileMetadata *filemd.Metadata
	startOffset  int64
}

// newFetchResult returns a fetchResult that has found nothing yet, for a fetch that asks for the groups
// askedIDs.
func newFetchResult(askedIDs []prefetchedRangesID) *fetchResult {
	f := &fetchResult{
		asked:  make(map[prefetchedRangesID]struct{}, len(askedIDs)),
		ranges: map[prefetchedRangesID]PrefetchedRanges{},
	}
	for _, id := range askedIDs {
		f.asked[id] = struct{}{}
	}
	return f
}

// has reports whether the fetch found the group id.
func (f *fetchResult) has(id prefetchedRangesID) bool {
	_, ok := f.ranges[id]
	return ok
}

// isEmpty reports whether the fetch found no group at all, and not the region.
func (f *fetchResult) isEmpty() bool {
	return len(f.ranges) == 0 && f.region == nil
}

// addCacheItem decodes a cache item that holds the group id, and adds the group to the result. The
// item must not hold more than maxRawSize raw bytes. It returns an error, and leaves the result as it
// was, when the item does not decode or the group is not valid.
func (f *fetchResult) addCacheItem(id prefetchedRangesID, unhashedKey string, stored []byte, maxRawSize int64, decoder *zstd.Decoder) error {
	raw, err := decodeCacheItem(stored, unhashedKey, maxRawSize, decoder)
	if err != nil {
		return err
	}
	if id.Kind == kindRegion {
		f.region = raw
		return nil
	}

	// The region item holds the raw bytes of the region. Any other item holds an encoded list of ranges
	// with their offsets, and the core group also lists the column names. So it needs decoding.
	ranges, columnNames, err := unmarshalPrefetchedRanges(raw)
	if err != nil {
		return err
	}
	if !ranges.Valid() {
		return errInvalidRanges
	}

	if id.Kind == kindFileMetadata {
		md, startOffset, err := ranges.parseFileMetadata()
		if err != nil {
			return err
		}
		f.fileMetadata, f.startOffset = md, startOffset
	}

	f.ranges[id] = ranges
	if id.Kind == kindCore {
		f.columnNames = columnNames
	}
	return nil
}

// satisfies reports whether the fetch found everything the prefetch set reads. A column that is not in
// columnNames needs no group, because the object does not have it.
func (f *fetchResult) satisfies(prefetchSet PrefetchSet) bool {
	if prefetchSet.AllMetadata {
		return f.region != nil
	}
	if !f.has(prefetchedRangesID{Kind: kindFileMetadata}) || !f.has(prefetchedRangesID{Kind: kindCore}) {
		return false
	}
	if prefetchSet.Message && !f.has(prefetchedRangesID{Kind: kindMessage}) {
		return false
	}
	for _, name := range prefetchSet.MetadataNames {
		if f.has(prefetchedRangesID{Kind: kindColumn, ColumnName: name}) {
			continue
		}
		if _, listed := f.columnNames[name]; listed {
			return false
		}
	}
	return true
}

// failureReason returns the value of the reason label for the error of a failed prefetch. It names
// the cause of the failure, and not the operation that failed.
func failureReason(err error) string {
	switch {
	case errors.Is(err, context.Canceled):
		return reasonCanceled
	case errors.Is(err, context.DeadlineExceeded):
		return reasonTimeout
	case errors.Is(err, errCacheUnavailable):
		return reasonCacheUnavailable
	case errors.Is(err, errRegionTooLarge):
		return reasonTooLarge
	case errors.Is(err, errHeadTooShort):
		return reasonHeadTooSmall
	}
	if _, ok := errors.AsType[unsupportedError](err); ok {
		return reasonUnsupported
	}

	// The remaining errors that wrap ErrNotPrefetchable come from a header, a file metadata or a section
	// layout that is not valid. Any other error comes from object storage.
	if errors.Is(err, ErrNotPrefetchable) {
		return reasonCorrupted
	}
	return reasonStorage
}
