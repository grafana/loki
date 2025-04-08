package bucket

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strconv"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/loki/v3/pkg/logqlmodel/stats"
	"github.com/grafana/loki/v3/pkg/storage/chunk/cache"
	"github.com/grafana/loki/v3/pkg/util/constants"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"golang.org/x/sync/errgroup"

	"github.com/thanos-io/objstore"
)

const (
	originCache  = "cache"
	originBucket = "bucket"
)

var (
	errObjNotFound = errors.Errorf("object not found")
)

type cachingBucketMetrics struct {
	requestedGetRangeBytes *prometheus.CounterVec
	fetchedGetRangeBytes   *prometheus.CounterVec
	refetchedGetRangeBytes *prometheus.CounterVec

	operationRequests *prometheus.CounterVec
	operationHits     *prometheus.CounterVec
}

func newCachingBucketMetrics(reg prometheus.Registerer) *cachingBucketMetrics {
	return &cachingBucketMetrics{
		requestedGetRangeBytes: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Name: "thanos_store_bucket_cache_getrange_requested_bytes_total",
			Help: "Total number of bytes requested via GetRange.",
		}, []string{"config"}),
		fetchedGetRangeBytes: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Name: "thanos_store_bucket_cache_getrange_fetched_bytes_total",
			Help: "Total number of bytes fetched because of GetRange operation. Data from bucket is then stored to cache.",
		}, []string{"origin", "config"}),
		refetchedGetRangeBytes: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Name: "thanos_store_bucket_cache_getrange_refetched_bytes_total",
			Help: "Total number of bytes re-fetched from storage because of GetRange operation, despite being in cache already.",
		}, []string{"origin", "config"}),

		operationRequests: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Name: "thanos_store_bucket_cache_operation_requests_total",
			Help: "Number of requested operations matching given config.",
		}, []string{"operation", "config"}),
		operationHits: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Name: "thanos_store_bucket_cache_operation_hits_total",
			Help: "Number of operations served from cache for given config.",
		}, []string{"operation", "config"}),
	}
}

// CachingBucket implementation that provides some caching features, based on passed configuration.
type CachingBucket struct {
	objstore.Bucket

	cfg     cache.Config
	logger  log.Logger
	metrics *cachingBucketMetrics

	cache cache.Cache
}

// NewCachingBucket creates new caching bucket with provided configuration. Configuration should not be
// changed after creating caching bucket.
func NewCachingBucket(b objstore.Bucket, cfg cache.Config, logger log.Logger, reg prometheus.Registerer) (*CachingBucket, error) {
	if b == nil {
		return nil, errors.New("bucket is nil")
	}

	cache, err := cache.New(cfg, reg, logger, stats.DataObjCache, constants.Loki)
	if err != nil {
		return nil, err
	}

	cb := &CachingBucket{
		Bucket:  b,
		cfg:     cfg,
		logger:  logger,
		metrics: newCachingBucketMetrics(reg),
		cache:   cache,
	}

	return cb, nil
}

func (cb *CachingBucket) Name() string {
	return "caching: " + cb.Bucket.Name()
}

func (cb *CachingBucket) Iter(ctx context.Context, dir string, f func(string) error, options ...objstore.IterOption) error {
	cfgName, cfg := cb.cfg.FindIterConfig(dir)
	if cfg == nil {
		return cb.Bucket.Iter(ctx, dir, f, options...)
	}

	cb.metrics.operationRequests.WithLabelValues(objstore.OpIter, cfgName).Inc()

	iterVerb := cachekey.BucketCacheKey{Verb: cachekey.IterVerb, Name: dir, ObjectStorageConfigHash: cfg.ConfigHash}
	opts := objstore.ApplyIterOptions(options...)
	if opts.Recursive {
		iterVerb.Verb = cachekey.IterRecursiveVerb
	}

	key := iterVerb.String()
	data := cfg.Cache.Fetch(ctx, []string{key})
	if data[key] != nil {
		list, err := cfg.Codec.Decode(data[key])
		if err == nil {
			cb.metrics.operationHits.WithLabelValues(objstore.OpIter, cfgName).Inc()
			for _, n := range list {
				if err := f(n); err != nil {
					return err
				}
			}
			return nil
		}
		level.Warn(cb.logger).Log("msg", "failed to decode cached Iter result", "key", key, "err", err)
	}

	// Iteration can take a while (esp. since it calls function), and iterTTL is generally low.
	// We will compute TTL based time when iteration started.
	iterTime := time.Now()
	var list []string
	err := cb.Bucket.Iter(ctx, dir, func(s string) error {
		list = append(list, s)
		return f(s)
	}, options...)

	remainingTTL := cfg.TTL - time.Since(iterTime)
	if err == nil && remainingTTL > 0 {
		data, encErr := cfg.Codec.Encode(list)
		if encErr == nil {
			cfg.Cache.Store(map[string][]byte{key: data}, remainingTTL)
			return nil
		}
		level.Warn(cb.logger).Log("msg", "failed to encode Iter result", "key", key, "err", encErr)
	}
	return err
}

func (cb *CachingBucket) Exists(ctx context.Context, name string) (bool, error) {
	cfgName, cfg := cb.cfg.FindExistConfig(name)
	if cfg == nil {
		return cb.Bucket.Exists(ctx, name)
	}

	cb.metrics.operationRequests.WithLabelValues(objstore.OpExists, cfgName).Inc()

	existsVerb := cachekey.BucketCacheKey{Verb: cachekey.ExistsVerb, Name: name}
	key := existsVerb.String()
	hits := cfg.Cache.Fetch(ctx, []string{key})

	if ex := hits[key]; ex != nil {
		exists, err := strconv.ParseBool(string(ex))
		if err == nil {
			cb.metrics.operationHits.WithLabelValues(objstore.OpExists, cfgName).Inc()
			return exists, nil
		}
		level.Warn(cb.logger).Log("msg", "unexpected cached 'exists' value", "key", key, "val", string(ex))
	}

	existsTime := time.Now()
	ok, err := cb.Bucket.Exists(ctx, name)
	if err == nil {
		storeExistsCacheEntry(key, ok, existsTime, cfg.Cache, cfg.ExistsTTL, cfg.DoesntExistTTL)
	}

	return ok, err
}

func storeExistsCacheEntry(cachingKey string, exists bool, ts time.Time, cache cache.Cache, existsTTL, doesntExistTTL time.Duration) {
	var ttl time.Duration
	if exists {
		ttl = existsTTL - time.Since(ts)
	} else {
		ttl = doesntExistTTL - time.Since(ts)
	}

	if ttl > 0 {
		cache.Store(map[string][]byte{cachingKey: []byte(strconv.FormatBool(exists))}, ttl)
	}
}

func (cb *CachingBucket) Get(ctx context.Context, name string) (io.ReadCloser, error) {
	cfgName, cfg := cb.cfg.FindGetConfig(name)
	if cfg == nil {
		return cb.Bucket.Get(ctx, name)
	}

	cb.metrics.operationRequests.WithLabelValues(objstore.OpGet, cfgName).Inc()

	contentVerb := cachekey.BucketCacheKey{Verb: cachekey.ContentVerb, Name: name}
	contentKey := contentVerb.String()
	existsVerb := cachekey.BucketCacheKey{Verb: cachekey.ExistsVerb, Name: name}
	existsKey := existsVerb.String()

	hits := cfg.Cache.Fetch(ctx, []string{contentKey, existsKey})
	if hits[contentKey] != nil {
		cb.metrics.operationHits.WithLabelValues(objstore.OpGet, cfgName).Inc()
		return objstore.NopCloserWithSize(bytes.NewBuffer(hits[contentKey])), nil
	}

	// If we know that file doesn't exist, we can return that. Useful for deletion marks.
	if ex := hits[existsKey]; ex != nil {
		if exists, err := strconv.ParseBool(string(ex)); err == nil && !exists {
			cb.metrics.operationHits.WithLabelValues(objstore.OpGet, cfgName).Inc()
			return nil, errObjNotFound
		}
	}

	getTime := time.Now()
	reader, err := cb.Bucket.Get(ctx, name)
	if err != nil {
		if cb.Bucket.IsObjNotFoundErr(err) {
			// Cache that object doesn't exist.
			storeExistsCacheEntry(existsKey, false, getTime, cfg.Cache, cfg.ExistsTTL, cfg.DoesntExistTTL)
		}

		return nil, err
	}

	storeExistsCacheEntry(existsKey, true, getTime, cfg.Cache, cfg.ExistsTTL, cfg.DoesntExistTTL)
	return &getReader{
		c:         cfg.Cache,
		ctx:       ctx,
		r:         reader,
		buf:       new(bytes.Buffer),
		startTime: getTime,
		ttl:       cfg.ContentTTL,
		cacheKey:  contentKey,
		maxSize:   cfg.MaxCacheableSize,
	}, nil
}

func (cb *CachingBucket) IsObjNotFoundErr(err error) bool {
	return err == errObjNotFound || cb.Bucket.IsObjNotFoundErr(err)
}

func (cb *CachingBucket) GetRange(ctx context.Context, name string, off, length int64) (io.ReadCloser, error) {
	if off < 0 || length <= 0 {
		return cb.Bucket.GetRange(ctx, name, off, length)
	}

	cfgName, cfg := cb.cfg.FindGetRangeConfig(name)
	if cfg == nil {
		return cb.Bucket.GetRange(ctx, name, off, length)
	}

	return cb.cachedGetRange(ctx, name, off, length, cfgName, cfg)
}

func (cb *CachingBucket) Attributes(ctx context.Context, name string) (objstore.ObjectAttributes, error) {
	cfgName, cfg := cb.cfg.FindAttributesConfig(name)
	if cfg == nil {
		return cb.Bucket.Attributes(ctx, name)
	}

	return cb.cachedAttributes(ctx, name, cfgName, cfg.Cache, cfg.TTL)
}

func (cb *CachingBucket) cachedAttributes(ctx context.Context, name, cfgName string, cache cache.Cache, ttl time.Duration) (objstore.ObjectAttributes, error) {
	attrVerb := cachekey.BucketCacheKey{Verb: cachekey.AttributesVerb, Name: name}
	key := attrVerb.String()

	cb.metrics.operationRequests.WithLabelValues(objstore.OpAttributes, cfgName).Inc()

	hits := cache.Fetch(ctx, []string{key})
	if raw, ok := hits[key]; ok {
		var attrs objstore.ObjectAttributes
		err := json.Unmarshal(raw, &attrs)
		if err == nil {
			cb.metrics.operationHits.WithLabelValues(objstore.OpAttributes, cfgName).Inc()
			return attrs, nil
		}

		level.Warn(cb.logger).Log("msg", "failed to decode cached Attributes result", "key", key, "err", err)
	}

	attrs, err := cb.Bucket.Attributes(ctx, name)
	if err != nil {
		return objstore.ObjectAttributes{}, err
	}

	if raw, err := json.Marshal(attrs); err == nil {
		cache.Store(map[string][]byte{key: raw}, ttl)
	} else {
		level.Warn(cb.logger).Log("msg", "failed to encode cached Attributes result", "key", key, "err", err)
	}

	return attrs, nil
}

func (cb *CachingBucket) cachedGetRange(ctx context.Context, name string, offset, length int64, cfgName string, cfg *cache.GetRangeConfig) (io.ReadCloser, error) {
	cb.metrics.operationRequests.WithLabelValues(objstore.OpGetRange, cfgName).Inc()
	cb.metrics.requestedGetRangeBytes.WithLabelValues(cfgName).Add(float64(length))

	attrs, err := cb.cachedAttributes(ctx, name, cfgName, cfg.Cache, cfg.AttributesTTL)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get object attributes: %s", name)
	}

	// If length goes over object size, adjust length. We use it later to limit number of read bytes.
	if offset+length > attrs.Size {
		length = attrs.Size - offset
	}

	// Start and end range are subrange-aligned offsets into object, that we're going to read.
	startRange := (offset / cfg.SubrangeSize) * cfg.SubrangeSize
	endRange := ((offset + length) / cfg.SubrangeSize) * cfg.SubrangeSize
	if (offset+length)%cfg.SubrangeSize > 0 {
		endRange += cfg.SubrangeSize
	}

	// The very last subrange in the object may have length that is not divisible by subrange size.
	lastSubrangeOffset := endRange - cfg.SubrangeSize
	lastSubrangeLength := int(cfg.SubrangeSize)
	if endRange > attrs.Size {
		lastSubrangeOffset = (attrs.Size / cfg.SubrangeSize) * cfg.SubrangeSize
		lastSubrangeLength = int(attrs.Size - lastSubrangeOffset)
	}

	numSubranges := (endRange - startRange) / cfg.SubrangeSize

	offsetKeys := make(map[int64]string, numSubranges)
	keys := make([]string, 0, numSubranges)

	totalRequestedBytes := int64(0)
	for off := startRange; off < endRange; off += cfg.SubrangeSize {
		end := off + cfg.SubrangeSize
		if end > attrs.Size {
			end = attrs.Size
		}
		totalRequestedBytes += (end - off)
		objectSubrange := cachekey.BucketCacheKey{Verb: cachekey.SubrangeVerb, Name: name, Start: off, End: end}
		k := objectSubrange.String()
		keys = append(keys, k)
		offsetKeys[off] = k
	}

	// Try to get all subranges from the cache.
	totalCachedBytes := int64(0)
	hits := cfg.Cache.Fetch(ctx, keys)
	for _, b := range hits {
		totalCachedBytes += int64(len(b))
	}
	cb.metrics.fetchedGetRangeBytes.WithLabelValues(originCache, cfgName).Add(float64(totalCachedBytes))
	cb.metrics.operationHits.WithLabelValues(objstore.OpGetRange, cfgName).Add(float64(len(hits)) / float64(len(keys)))

	if len(hits) < len(keys) {
		if hits == nil {
			hits = map[string][]byte{}
		}

		err := cb.fetchMissingSubranges(ctx, name, startRange, endRange, offsetKeys, hits, lastSubrangeOffset, lastSubrangeLength, cfgName, cfg)
		if err != nil {
			return nil, err
		}
	}

	return io.NopCloser(newSubrangesReader(cfg.SubrangeSize, offsetKeys, hits, offset, length)), nil
}

type rng struct {
	start, end int64
}

// fetchMissingSubranges fetches missing subranges, stores them into "hits" map
// and into cache as well (using provided cacheKeys).
func (cb *CachingBucket) fetchMissingSubranges(ctx context.Context, name string, startRange, endRange int64, cacheKeys map[int64]string, hits map[string][]byte, lastSubrangeOffset int64, lastSubrangeLength int, cfgName string, cfg *cache.GetRangeConfig) error {
	// Ordered list of missing sub-ranges.
	var missing []rng

	for off := startRange; off < endRange; off += cfg.SubrangeSize {
		if hits[cacheKeys[off]] == nil {
			missing = append(missing, rng{start: off, end: off + cfg.SubrangeSize})
		}
	}

	missing = mergeRanges(missing, 0) // Merge adjacent ranges.
	// Keep merging until we have only max number of ranges (= requests).
	for limit := cfg.SubrangeSize; cfg.MaxSubRequests > 0 && len(missing) > cfg.MaxSubRequests; limit = limit * 2 {
		missing = mergeRanges(missing, limit)
	}

	var hitsMutex sync.Mutex

	// Run parallel queries for each missing range. Fetched data is stored into 'hits' map, protected by hitsMutex.
	g, gctx := errgroup.WithContext(ctx)
	for _, m := range missing {
		m := m
		g.Go(func() error {
			r, err := cb.Bucket.GetRange(gctx, name, m.start, m.end-m.start)
			if err != nil {
				return errors.Wrapf(err, "fetching range [%d, %d]", m.start, m.end)
			}
			defer runutil.CloseWithLogOnErr(cb.logger, r, "fetching range [%d, %d]", m.start, m.end)

			var bufSize int64
			if lastSubrangeOffset >= m.end {
				bufSize = m.end - m.start
			} else {
				bufSize = ((m.end - m.start) - cfg.SubrangeSize) + int64(lastSubrangeLength)
			}

			buf := make([]byte, bufSize)
			_, err = io.ReadFull(r, buf)
			if err != nil {
				return errors.Wrapf(err, "fetching range [%d, %d]", m.start, m.end)
			}

			for off := m.start; off < m.end && gctx.Err() == nil; off += cfg.SubrangeSize {
				key := cacheKeys[off]
				if key == "" {
					return errors.Errorf("fetching range [%d, %d]: caching key for offset %d not found", m.start, m.end, off)
				}

				// We need a new buffer for each subrange, both for storing into hits, and also for caching.
				var subrangeData []byte
				if off == lastSubrangeOffset {
					// The very last subrange in the object may have different length,
					// if object length isn't divisible by subrange size.
					subrangeData = buf[off-m.start : off-m.start+int64(lastSubrangeLength)]
				} else {
					subrangeData = buf[off-m.start : off-m.start+cfg.SubrangeSize]
				}

				storeToCache := false
				hitsMutex.Lock()
				if _, ok := hits[key]; !ok {
					storeToCache = true
					hits[key] = subrangeData
				}
				hitsMutex.Unlock()

				if storeToCache {
					cb.metrics.fetchedGetRangeBytes.WithLabelValues(originBucket, cfgName).Add(float64(len(subrangeData)))
					cfg.Cache.Store(map[string][]byte{key: subrangeData}, cfg.SubrangeTTL)
				} else {
					cb.metrics.refetchedGetRangeBytes.WithLabelValues(originCache, cfgName).Add(float64(len(subrangeData)))
				}
			}

			return gctx.Err()
		})
	}

	return g.Wait()
}

// Merges ranges that are close to each other. Modifies input.
func mergeRanges(input []rng, limit int64) []rng {
	if len(input) == 0 {
		return input
	}

	last := 0
	for ix := 1; ix < len(input); ix++ {
		if (input[ix].start - input[last].end) <= limit {
			input[last].end = input[ix].end
		} else {
			last++
			input[last] = input[ix]
		}
	}
	return input[:last+1]
}

// Reader implementation that uses in-memory subranges.
type subrangesReader struct {
	subrangeSize int64

	// Mapping of subrangeSize-aligned offsets to keys in hits.
	offsetsKeys map[int64]string
	subranges   map[string][]byte

	// Offset for next read, used to find correct subrange to return data from.
	readOffset int64

	// Remaining data to return from this reader. Once zero, this reader reports EOF.
	remaining int64
}

func newSubrangesReader(subrangeSize int64, offsetsKeys map[int64]string, subranges map[string][]byte, readOffset, remaining int64) *subrangesReader {
	return &subrangesReader{
		subrangeSize: subrangeSize,
		offsetsKeys:  offsetsKeys,
		subranges:    subranges,

		readOffset: readOffset,
		remaining:  remaining,
	}
}

func (c *subrangesReader) Read(p []byte) (n int, err error) {
	if c.remaining <= 0 {
		return 0, io.EOF
	}

	currentSubrangeOffset := (c.readOffset / c.subrangeSize) * c.subrangeSize
	currentSubrange, err := c.subrangeAt(currentSubrangeOffset)
	if err != nil {
		return 0, errors.Wrapf(err, "read position: %d", c.readOffset)
	}

	offsetInSubrange := int(c.readOffset - currentSubrangeOffset)
	toCopy := len(currentSubrange) - offsetInSubrange
	if toCopy <= 0 {
		// This can only happen if subrange's length is not subrangeSize, and reader is told to read more data.
		return 0, errors.Errorf("no more data left in subrange at position %d, subrange length %d, reading position %d", currentSubrangeOffset, len(currentSubrange), c.readOffset)
	}

	if len(p) < toCopy {
		toCopy = len(p)
	}
	if c.remaining < int64(toCopy) {
		toCopy = int(c.remaining) // Conversion is safe, c.remaining is small enough.
	}

	copy(p, currentSubrange[offsetInSubrange:offsetInSubrange+toCopy])
	c.readOffset += int64(toCopy)
	c.remaining -= int64(toCopy)

	return toCopy, nil
}

func (c *subrangesReader) subrangeAt(offset int64) ([]byte, error) {
	b := c.subranges[c.offsetsKeys[offset]]
	if b == nil {
		return nil, errors.Errorf("subrange for offset %d not found", offset)
	}
	return b, nil
}

type getReader struct {
	c         cache.Cache
	ctx       context.Context
	r         io.ReadCloser
	buf       *bytes.Buffer
	startTime time.Time
	ttl       time.Duration
	cacheKey  string
	maxSize   int
}

func (g *getReader) Close() error {
	// We don't know if entire object was read, don't store it here.
	g.buf = nil
	return g.r.Close()
}

func (g *getReader) Read(p []byte) (n int, err error) {
	n, err = g.r.Read(p)
	if n > 0 && g.buf != nil {
		if g.buf.Len()+n <= g.maxSize {
			g.buf.Write(p[:n])
		} else {
			// Object is larger than max size, stop caching.
			g.buf = nil
		}
	}

	if err == io.EOF && g.buf != nil {
		remainingTTL := g.ttl - time.Since(g.startTime)
		if remainingTTL > 0 {
			g.c.Store(map[string][]byte{g.cacheKey: g.buf.Bytes()}, remainingTTL)
		}
		// Clear reference, to avoid doing another Store on next read.
		g.buf = nil
	}

	return n, err
}

// JSONIterCodec encodes iter results into JSON. Suitable for root dir.
type JSONIterCodec struct{}

func (jic JSONIterCodec) Encode(files []string) ([]byte, error) {
	return json.Marshal(files)
}

func (jic JSONIterCodec) Decode(data []byte) ([]string, error) {
	var list []string
	err := json.Unmarshal(data, &list)
	return list, err
}
