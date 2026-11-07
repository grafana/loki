package metastore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"iter"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	dslog "github.com/grafana/dskit/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/prometheus/model/labels"
	"golang.org/x/sync/errgroup"
	"golang.org/x/sync/singleflight"

	"github.com/grafana/loki/v3/pkg/dataobj/sections/postings"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
	"github.com/grafana/loki/v3/pkg/logql/model/stats"
	"github.com/grafana/loki/v3/pkg/storage/chunk/cache"
	"github.com/grafana/loki/v3/pkg/util/constants"
)

// indexLookupCacheKeyPrefix starts every key of the index lookup cache. It holds the version of the
// entries, so a new version never reads an entry of an older one.
var indexLookupCacheKeyPrefix = "idx/v" + strconv.Itoa(indexLookupCacheVersion) + "/"

const (
	// defaultIndexLookupCacheMaxEntryBytes is the size limit of an entry when the caller does not
	// know the limit of the backend.
	defaultIndexLookupCacheMaxEntryBytes = 1 << 20

	// indexLookupFillTimeout bounds the fill of the entries of one index object. The fill runs for
	// all the requests that wait for it, so no single request bounds it. A request whose fill reports
	// a deadline reads the index object without the cache. The fill does not see the deadline of any
	// request.
	indexLookupFillTimeout = 30 * time.Second

	// maxFillEveryBucketBuckets is the largest number of shard buckets for which a request for one
	// bucket fills the entries of every bucket. For a layout with more buckets, the fill covers only
	// the bucket that was asked for.
	//
	// The fill cuts the rows once for each bucket and stores one entry for each bucket, so its work
	// grows with the number of buckets. A request asks for one bucket only when its query has at
	// least as many shards as buckets, and the query frontend makes at most 256 shards.
	maxFillEveryBucketBuckets = 256
)

// The kinds of entry of the index lookup cache, as the metrics report them.
const (
	cacheKindSelectorMatches    = "selector_matches"
	cacheKindPredicateAdmission = "predicate_admission"
)

// NewIndexLookupCacheBackend builds the cache backend that cfg describes. It also returns the size
// of the largest entry that the backend stores.
//
// That size is the max item size of the memcached client when it is set. In any other case it is
// 1 MiB.
func NewIndexLookupCacheBackend(cfg cache.Config, reg prometheus.Registerer, logger log.Logger) (cache.Cache, int, error) {
	backend, err := cache.New(cfg, reg, logger, stats.DataObjIndexCache, constants.Loki)
	if err != nil {
		return nil, 0, err
	}

	maxEntryBytes := defaultIndexLookupCacheMaxEntryBytes
	if cache.IsMemcacheSet(cfg) && cfg.MemcacheClient.MaxItemSize > 0 {
		maxEntryBytes = cfg.MemcacheClient.MaxItemSize
	}
	return cache.StopOnce(backend), maxEntryBytes, nil
}

// indexLookupCache looks up the stream selector of a request in the index objects, and keeps what it
// finds in a cache backend. An index object never changes, so an entry never goes stale. The cache
// holds, for each index object, the sections that match a stream selector and the answer to each
// equality predicate. Neither depends on the time range of a request, so a request with another time
// range finds the same entries.
//
// An indexLookupCache is safe for concurrent use.
type indexLookupCache struct {
	// objectStoreProvider looks up what the cache does not have.
	objectStoreProvider *objectStoreIndexLookupProvider

	backend       cache.Cache
	maxEntryBytes int

	// fillFlights makes the requests that miss the same entry share one fill, which runs at most for
	// fillTimeout.
	fillFlights singleflight.Group
	fillTimeout time.Duration

	logger        log.Logger
	warningLogger log.Logger
	metrics       *indexLookupCacheMetrics
}

// newIndexLookupCache returns a cache that stores in backend and looks up in objectStoreProvider what
// the backend does not have. It registers its metrics on reg, which may be nil. A maxEntryBytes that
// is not positive selects the default.
func newIndexLookupCache(objectStoreProvider *objectStoreIndexLookupProvider, backend cache.Cache, maxEntryBytes int, logger log.Logger, reg prometheus.Registerer) *indexLookupCache {
	if maxEntryBytes <= 0 {
		maxEntryBytes = defaultIndexLookupCacheMaxEntryBytes
	}
	return &indexLookupCache{
		objectStoreProvider: objectStoreProvider,
		backend:             backend,
		maxEntryBytes:       maxEntryBytes,
		fillTimeout:         indexLookupFillTimeout,
		logger:              logger,

		// A failing backend makes every request log a warning, so the logger limits the rate.
		warningLogger: dslog.NewRateLimitedLogger(logger, 1, 5, nil),
		metrics:       newIndexLookupCacheMetrics(reg),
	}
}

// lookupIndexObjects yields the index objects that the cache has first, and the other ones after it
// reads them from object storage and stores their entries.
func (c *indexLookupCache) lookupIndexObjects(ctx context.Context, req indexLookupRequest, indexes []IndexEntry) iter.Seq2[indexLookupResult, error] {
	return func(yield func(indexLookupResult, error) bool) {
		plan := c.planLookups(ctx, req, indexes)

		for _, hit := range plan.hits {
			if !yield(hit, nil) {
				return
			}
		}
		if len(plan.misses) == 0 {
			return
		}

		for result, err := range lookupIndexObjectsConcurrently(ctx, c.logger, c.objectStoreProvider.parallelism, plan.misses, plan.fillMissingEntries) {
			if !yield(result, err) || err != nil {
				return
			}
		}
	}
}

// indexLookupKeys are the keys of the entries of one index object for one request.
type indexLookupKeys struct {
	// cacheable is false when the shard layout of the index object could not be read. The lookup of
	// such an index object bypasses the cache. It reads the layout again, and reports the error if
	// that read fails too.
	cacheable bool

	selectorMatchesKey     cacheKey
	predicateAdmissionKeys []cacheKey // by equality predicate of the request

	// shardLayout and shardBucketRange are the layout of the index object and the bucket range of
	// the request in it. The range is nil when the request prunes no bucket.
	shardLayout      streams.ShardLayout
	shardBucketRange *postings.ShardBucketRange
}

// cacheKey is the key of an entry. The backend gets the hashed key, which has a bounded length. The
// value holds a version and the full key, so a reader detects a hash collision.
type cacheKey struct {
	full   string
	hashed string
}

func newCacheKey(kindPrefix string, parts ...string) cacheKey {
	full := kindPrefix + "\x00" + strings.Join(parts, "\x00")
	sum := sha256.Sum256([]byte(full))
	return cacheKey{full: full, hashed: indexLookupCacheKeyPrefix + kindPrefix + "/" + hex.EncodeToString(sum[:])}
}

// foundEntries are the entries of one index object that the backend had.
type foundEntries struct {
	// selectorMatches is nil when the backend did not have a usable entry.
	selectorMatches *indexSelectorMatches

	// predicateAdmissions are the admissions by equality predicate of the request. An entry is nil
	// when the backend did not have a usable one.
	predicateAdmissions []*predicateAdmission
}

func (f *foundEntries) complete() bool {
	return f.selectorMatches != nil && !slices.Contains(f.predicateAdmissions, nil)
}

// indexLookupPlan says which index objects of a request the cache has. It is safe for concurrent use
// once built.
type indexLookupPlan struct {
	cache *indexLookupCache
	req   indexLookupRequest

	// canonicalSelector is the stream selector of the request in an order that does not depend on
	// the order of the matchers.
	canonicalSelector string

	keys  map[string]*indexLookupKeys
	found map[string]*foundEntries

	// hits are the index objects whose entries the backend all had.
	hits []indexLookupResult

	// misses are the other index objects.
	misses []IndexEntry
}

// planLookups finds out which index objects of the request the backend has. A backend that fails
// makes the index objects whose entries it did not return a miss.
func (c *indexLookupCache) planLookups(ctx context.Context, req indexLookupRequest, indexes []IndexEntry) *indexLookupPlan {
	plan := &indexLookupPlan{
		cache:             c,
		req:               req,
		canonicalSelector: canonicalSelector(req.streamMatchers),
		keys:              make(map[string]*indexLookupKeys, len(indexes)),
		found:             make(map[string]*foundEntries, len(indexes)),
	}

	// A request without a hash range reads every bucket, so its keys do not depend on the layout.
	// With a hash range, the key holds the bucket range, which needs the layout of each index object.
	var layouts []shardLayoutResolution
	if req.streamHashRange != nil {
		layouts = c.objectStoreProvider.layoutResolver.resolveShardLayouts(ctx, req.tenantID, indexes)
	}

	var hashedKeys []string
	for i, index := range indexes {
		keys := &indexLookupKeys{cacheable: true}
		if layouts != nil {
			keys.cacheable = layouts[i].err == nil
			if layouts[i].err != nil {
				level.Warn(c.warningLogger).Log("msg", "reading the shard layout failed, so the lookup bypasses the cache", "index", index.Path, "err", layouts[i].err)
			}
			keys.shardLayout = layouts[i].layout
			keys.shardBucketRange = shardBucketRangeFor(layouts[i].layout, req.streamHashRange)
		}
		plan.keys[index.Path] = keys
		if !keys.cacheable {
			continue
		}

		keys.selectorMatchesKey = plan.selectorMatchesKey(index, keys.shardLayout, keys.shardBucketRange)
		hashedKeys = append(hashedKeys, keys.selectorMatchesKey.hashed)
		for _, predicate := range req.equalityPredicates {
			key := newCacheKey("a", req.tenantID, index.Path, predicate.Name, predicate.Value)
			keys.predicateAdmissionKeys = append(keys.predicateAdmissionKeys, key)
			hashedKeys = append(hashedKeys, key.hashed)
		}
	}

	values, fetchFailed := c.fetchEntries(ctx, hashedKeys)

	for _, index := range indexes {
		keys := plan.keys[index.Path]
		if !keys.cacheable {
			plan.misses = append(plan.misses, index)
			continue
		}

		found := &foundEntries{predicateAdmissions: make([]*predicateAdmission, len(req.equalityPredicates))}
		plan.found[index.Path] = found

		if value, ok := values[keys.selectorMatchesKey.hashed]; ok {
			matches, err := decodeSelectorMatches(keys.selectorMatchesKey.full, value)
			c.countFetchedEntry(cacheKindSelectorMatches, index.Path, err)
			if err == nil {
				found.selectorMatches = matches
			}
		} else {
			c.metrics.countMissing(cacheKindSelectorMatches, fetchFailed)
		}
		for i, key := range keys.predicateAdmissionKeys {
			if value, ok := values[key.hashed]; ok {
				admission, err := decodePredicateAdmission(key.full, value, req.equalityPredicates[i])
				c.countFetchedEntry(cacheKindPredicateAdmission, index.Path, err)
				if err == nil {
					found.predicateAdmissions[i] = admission
				}
			} else {
				c.metrics.countMissing(cacheKindPredicateAdmission, fetchFailed)
			}
		}

		if !found.complete() {
			plan.misses = append(plan.misses, index)
			continue
		}
		plan.hits = append(plan.hits, indexLookupResult{index: index, selectorMatches: found.selectorMatches, predicateAdmissions: found.predicateAdmissions})
	}
	return plan
}

// selectorMatchesKey returns the key of the selector matches of the index object for the bucket
// range. A nil range means every bucket.
func (p *indexLookupPlan) selectorMatchesKey(index IndexEntry, layout streams.ShardLayout, bucketRange *postings.ShardBucketRange) cacheKey {
	// The bucket range only means something together with the layout that cut the buckets, so the
	// key has both. A range that prunes nothing is the same for every layout.
	bucketRangeKey := "all"
	if bucketRange != nil {
		bucketRangeKey = fmt.Sprintf("%d:%d-%d", layout.Bits(), bucketRange.From, bucketRange.To)
	}
	return newCacheKey("m", p.req.tenantID, index.Path, p.canonicalSelector, bucketRangeKey)
}

// canonicalSelector returns the matchers in an order that does not depend on their order in the
// request.
func canonicalSelector(matchers []*labels.Matcher) string {
	texts := make([]string, len(matchers))
	for i, matcher := range matchers {
		texts[i] = matcher.String()
	}
	slices.Sort(texts)
	return strings.Join(texts, ",")
}

// fillMissingEntries returns the lookup of an index object that the backend did not have in full. It
// reads what the backend lacked from object storage and stores it. When a fill reports a deadline,
// it reads the index object without the cache.
func (p *indexLookupPlan) fillMissingEntries(ctx context.Context, index IndexEntry) (indexLookupResult, error) {
	keys := p.keys[index.Path]
	if !keys.cacheable {
		return p.cache.objectStoreProvider.lookupIndexObject(ctx, p.req, index)
	}

	// Both fills need the index object opened, and one open serves them.
	opened := &lazyOpenedIndexObject{scanner: p.cache.objectStoreProvider.scanner, tenantID: p.req.tenantID, indexPath: index.Path}
	found := p.found[index.Path]

	// The two fills are independent, so they can overlap. Nothing recovers a panic of a goroutine
	// that the group starts, so each goroutine recovers its own.
	var (
		matches    = found.selectorMatches
		admissions predicateAdmissions
		group      errgroup.Group
	)
	if matches == nil {
		group.Go(func() (err error) {
			defer recoverPanicAsError(ctx, p.cache.logger, index.Path, &err)

			matches, err = p.fillSelectorMatches(ctx, index, opened)
			return err
		})
	}
	group.Go(func() (err error) {
		defer recoverPanicAsError(ctx, p.cache.logger, index.Path, &err)

		admissions, err = p.fillPredicateAdmissions(ctx, index, found, opened)
		return err
	})
	if err := group.Wait(); err != nil {
		// The fill reads more than the request needs, so on a huge index object it can take longer
		// than the read of the request alone. The cache must not fail a request that works without
		// it. The fills ignore the deadline of the request, so while the request is alive a deadline
		// error comes from the time limit of a fill, or from a timeout of the object store client.
		// The read without the cache is right for both. Both fills share one open, so either of them
		// can report the deadline, and the other one can report the error of the open without its own
		// limit.
		if errors.Is(err, context.DeadlineExceeded) && ctx.Err() == nil {
			level.Warn(p.cache.warningLogger).Log("msg", "filling the index lookup entries reached a deadline, so the lookup bypasses the cache", "index", index.Path, "err", err)
			return p.cache.objectStoreProvider.lookupIndexObject(ctx, p.req, index)
		}
		return indexLookupResult{}, err
	}
	return indexLookupResult{index: index, selectorMatches: matches, predicateAdmissions: admissions}, nil
}

// fillSelectorMatches returns the selector matches of the index object, and stores them.
func (p *indexLookupPlan) fillSelectorMatches(ctx context.Context, index IndexEntry, opened *lazyOpenedIndexObject) (*indexSelectorMatches, error) {
	keys := p.keys[index.Path]

	if isSingleShardBucket(keys.shardBucketRange) && keys.shardLayout.Buckets() <= maxFillEveryBucketBuckets {
		// A request for one bucket comes from a query with at least as many shards as buckets, and the
		// other shards of the query follow. The fill reads the rows of every bucket and stores the
		// matches of each one, so that the other shards find their bucket in the backend. The requests
		// for any bucket of the index object share the fill. A fill for each bucket would read the
		// index object once for each bucket.
		fillKey := newCacheKey("f", p.req.tenantID, index.Path, p.canonicalSelector, fmt.Sprint(keys.shardLayout.Bits())).hashed
		value, err := p.cache.fill(ctx, index.Path, fillKey, func(ctx context.Context) (any, error) {
			return p.fillEveryBucket(ctx, index, opened)
		})
		if err != nil {
			return nil, err
		}
		return value.([]*indexSelectorMatches)[keys.shardBucketRange.From], nil
	}

	value, err := p.cache.fill(ctx, index.Path, keys.selectorMatchesKey.hashed, func(ctx context.Context) (any, error) {
		openedIndex, err := opened.get(ctx)
		if err != nil {
			return nil, err
		}
		matches, err := openedIndex.scanSelectorMatches(ctx, p.req.streamMatchers, keys.shardBucketRange)
		if err != nil {
			return nil, fmt.Errorf("matching the stream selector in index object %q: %w", index.Path, err)
		}

		p.cache.storeSelectorMatches(ctx, []selectorMatchesEntry{{key: keys.selectorMatchesKey, matches: matches}})
		return matches, nil
	})
	if err != nil {
		return nil, err
	}
	return value.(*indexSelectorMatches), nil
}

// fillEveryBucket reads the rows of every shard bucket of the index object, and stores the matches of
// each bucket. It returns the matches by bucket.
func (p *indexLookupPlan) fillEveryBucket(ctx context.Context, index IndexEntry, opened *lazyOpenedIndexObject) ([]*indexSelectorMatches, error) {
	layout := p.keys[index.Path].shardLayout

	openedIndex, err := opened.get(ctx)
	if err != nil {
		return nil, err
	}

	perBucket, err := openedIndex.scanSelectorMatchesOfEachBucket(ctx, p.req.streamMatchers, layout.Buckets())
	if err != nil {
		return nil, fmt.Errorf("matching the stream selector in index object %q: %w", index.Path, err)
	}

	entries := make([]selectorMatchesEntry, len(perBucket))
	for bucket, matches := range perBucket {
		bucketRange := &postings.ShardBucketRange{From: uint32(bucket), To: uint32(bucket)}
		entries[bucket] = selectorMatchesEntry{key: p.selectorMatchesKey(index, layout, bucketRange), matches: matches}
	}
	p.cache.storeSelectorMatches(ctx, entries)
	return perBucket, nil
}

// isSingleShardBucket reports whether the range holds one bucket.
func isSingleShardBucket(bucketRange *postings.ShardBucketRange) bool {
	return bucketRange != nil && bucketRange.From == bucketRange.To
}

// fillPredicateAdmissions returns the admissions of the index object for the equality predicates
// of the request, in the order of the request. It reads the ones that found does not hold and
// stores them.
func (p *indexLookupPlan) fillPredicateAdmissions(ctx context.Context, index IndexEntry, found *foundEntries, opened *lazyOpenedIndexObject) (predicateAdmissions, error) {
	keys := p.keys[index.Path]

	var (
		missing          []int
		missingKeys      []string
		missingPredicate []*labels.Matcher
	)
	for i, admission := range found.predicateAdmissions {
		if admission == nil {
			missing = append(missing, i)
			missingKeys = append(missingKeys, keys.predicateAdmissionKeys[i].hashed)
			missingPredicate = append(missingPredicate, p.req.equalityPredicates[i])
		}
	}
	if len(missing) == 0 {
		return found.predicateAdmissions, nil
	}

	// The fills of the same predicates in the same order share one read. The read answers all the
	// predicates at once, which reads the bloom filters once.
	value, err := p.cache.fill(ctx, index.Path, strings.Join(missingKeys, ","), func(ctx context.Context) (any, error) {
		openedIndex, err := opened.get(ctx)
		if err != nil {
			return nil, err
		}
		admissions, err := openedIndex.scanPredicateAdmissions(ctx, missingPredicate)
		if err != nil {
			return nil, fmt.Errorf("reading the predicate admissions of index object %q: %w", index.Path, err)
		}

		// Check that the scan returned one admission for each predicate, in order. The code after
		// these checks pairs each admission with a key by position. A scan that returns another
		// predicate in a slot would store its admission under the key of the wrong predicate, and the
		// entry would serve wrong sections until it expires.
		if len(admissions) != len(missingPredicate) {
			return nil, fmt.Errorf("reading the predicate admissions of index object %q: got %d admissions for %d predicates", index.Path, len(admissions), len(missingPredicate))
		}
		for i, admission := range admissions {
			if admission.predicateName != missingPredicate[i].Name || admission.predicateValue != missingPredicate[i].Value {
				return nil, fmt.Errorf("reading the predicate admissions of index object %q: admission %d is not for the predicate %s", index.Path, i, missingPredicate[i])
			}
		}

		entries := make([]predicateAdmissionEntry, len(admissions))
		for i, admission := range admissions {
			entries[i] = predicateAdmissionEntry{key: keys.predicateAdmissionKeys[missing[i]], admission: admission}
		}
		p.cache.storePredicateAdmissions(ctx, entries)
		return admissions, nil
	})
	if err != nil {
		return nil, err
	}

	admissions := slices.Clone(found.predicateAdmissions)
	for i, admission := range value.(predicateAdmissions) {
		admissions[missing[i]] = admission
	}
	return admissions, nil
}

// lazyOpenedIndexObject opens an index object the first time that a caller asks for it.
type lazyOpenedIndexObject struct {
	scanner   *postingsIndexScanner
	tenantID  string
	indexPath string

	once   sync.Once
	opened *openedIndexObject
	err    error
}

func (l *lazyOpenedIndexObject) get(ctx context.Context) (*openedIndexObject, error) {
	l.once.Do(func() {
		// A panic in open ends the once without a result. The error below then tells the other
		// callers that the open failed, instead of a nil index object.
		l.err = errors.New("opening the index object did not finish")
		l.opened, l.err = l.scanner.open(ctx, l.tenantID, l.indexPath)
	})
	return l.opened, l.err
}

// fill calls fn once for all the concurrent calls with the same key, and returns its result to each
// of them. fn does not see the cancellation or the deadline of the calls, and it has the time limit
// of the cache. When the context of a call ends, the call returns the cause of the context and does
// not stop fn. A panic in fn becomes an error that names indexPath.
func (c *indexLookupCache) fill(ctx context.Context, indexPath, key string, fn func(ctx context.Context) (any, error)) (any, error) {
	// The fill below outlives the call. A call that is already canceled must not start one: when a
	// resolution is canceled, each index object that is left would start a fill that nothing waits
	// for.
	if ctx.Err() != nil {
		return nil, context.Cause(ctx)
	}

	results := c.fillFlights.DoChan(key, func() (value any, err error) {
		// The library raises a panic of this function again on a new goroutine, and then nothing can
		// recover it. So the function recovers it itself.
		defer recoverPanicAsError(ctx, c.logger, indexPath, &err)

		// The call that started the fill can give up while other calls still wait for it, so the fill
		// ignores the cancellation of that call. The timeout bounds a fill that no call waits for.
		fillCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), c.fillTimeout)
		defer cancel()

		return fn(fillCtx)
	})

	select {
	case <-ctx.Done():
		return nil, context.Cause(ctx)
	case result := <-results:
		return result.Val, result.Err
	}
}

// fetchEntries returns the values that the backend has for the keys, by key, in one call to the
// backend. failed is true when the backend failed. The values that it still returned are valid.
func (c *indexLookupCache) fetchEntries(ctx context.Context, keys []string) (values map[string][]byte, failed bool) {
	if len(keys) == 0 {
		return nil, false
	}

	found, buffers, _, err := c.backend.Fetch(ctx, keys)
	if err != nil {
		failed = true
		level.Warn(c.warningLogger).Log("msg", "fetching the index lookup entries failed", "keys", len(keys), "err", err)
	}

	values = make(map[string][]byte, len(found))
	for i, key := range found {
		values[key] = buffers[i]
	}
	return values, failed
}

type selectorMatchesEntry struct {
	key     cacheKey
	matches *indexSelectorMatches
}

type predicateAdmissionEntry struct {
	key       cacheKey
	admission *predicateAdmission
}

// storeSelectorMatches stores the entries in the backend. It reports a failure in the metrics and the
// logs, not to the caller.
func (c *indexLookupCache) storeSelectorMatches(ctx context.Context, entries []selectorMatchesEntry) {
	encoded := make([]encodedEntry, 0, len(entries))
	for _, entry := range entries {
		value, err := encodeSelectorMatches(entry.key.full, entry.matches)
		encoded = append(encoded, encodedEntry{hashedKey: entry.key.hashed, value: value, err: err})
	}
	c.store(ctx, cacheKindSelectorMatches, encoded)
}

// storePredicateAdmissions stores the entries in the backend, as storeSelectorMatches does.
func (c *indexLookupCache) storePredicateAdmissions(ctx context.Context, entries []predicateAdmissionEntry) {
	encoded := make([]encodedEntry, 0, len(entries))
	for _, entry := range entries {
		value, err := encodePredicateAdmission(entry.key.full, entry.admission)
		encoded = append(encoded, encodedEntry{hashedKey: entry.key.hashed, value: value, err: err})
	}
	c.store(ctx, cacheKindPredicateAdmission, encoded)
}

type encodedEntry struct {
	hashedKey string
	value     []byte
	err       error
}

func (c *indexLookupCache) store(ctx context.Context, kind string, entries []encodedEntry) {
	var (
		keys   = make([]string, 0, len(entries))
		values = make([][]byte, 0, len(entries))
	)
	for _, entry := range entries {
		switch {
		case entry.err != nil:
			c.metrics.countStore(kind, storeResultError)
			level.Warn(c.warningLogger).Log("msg", "encoding an index lookup entry failed", "kind", kind, "err", entry.err)
		case len(entry.value) > c.maxEntryBytes:
			// The backend would reject the entry, and the entry would cost a request to find out.
			c.metrics.countStore(kind, storeResultOversize)
		default:
			c.metrics.entryBytes.WithLabelValues(kind).Observe(float64(len(entry.value)))
			keys = append(keys, entry.hashedKey)
			values = append(values, entry.value)
		}
	}
	if len(keys) == 0 {
		return
	}

	if err := c.backend.Store(ctx, keys, values); err != nil {
		c.metrics.countStoreN(kind, storeResultError, len(keys))
		level.Warn(c.warningLogger).Log("msg", "storing the index lookup entries failed", "kind", kind, "entries", len(keys), "err", err)
		return
	}
	c.metrics.countStoreN(kind, storeResultStored, len(keys))
}

// countFetchedEntry counts an entry that the backend returned. An entry that the cache cannot decode
// counts as invalid, and the cache logs it.
func (c *indexLookupCache) countFetchedEntry(kind, indexPath string, decodeErr error) {
	if decodeErr != nil {
		// A value that does not decode is a hash collision or a corrupt write. The lookup reads the
		// index object, and its fill then overwrites the entry.
		c.metrics.requests.WithLabelValues(kind, requestResultInvalid).Inc()
		level.Warn(c.warningLogger).Log("msg", "dropping an index lookup entry that the cache cannot use", "kind", kind, "index", indexPath, "err", decodeErr)
		return
	}
	c.metrics.requests.WithLabelValues(kind, requestResultHit).Inc()
}

const (
	requestResultHit     = "hit"
	requestResultMiss    = "miss"
	requestResultError   = "error"
	requestResultInvalid = "invalid"
)

const (
	storeResultStored   = "stored"
	storeResultError    = "error"
	storeResultOversize = "oversize"
)

// indexLookupCacheMetrics are the metrics of an indexLookupCache.
type indexLookupCacheMetrics struct {
	requests   *prometheus.CounterVec
	stores     *prometheus.CounterVec
	entryBytes *prometheus.HistogramVec
}

func newIndexLookupCacheMetrics(reg prometheus.Registerer) *indexLookupCacheMetrics {
	factory := promauto.With(reg)
	return &indexLookupCacheMetrics{
		requests: factory.NewCounterVec(prometheus.CounterOpts{
			Name: "loki_dataobj_index_cache_requests_total",
			Help: "Fetches of an entry of the index lookup cache, by kind of entry and result. An error is a backend that failed. An invalid entry is one that the cache cannot decode, or that belongs to another key or version. Both make the lookup read the index object.",
		}, []string{"kind", "result"}),
		stores: factory.NewCounterVec(prometheus.CounterOpts{
			Name: "loki_dataobj_index_cache_stores_total",
			Help: "Entries that the index lookup cache tried to store in its backend, by kind of entry and result.",
		}, []string{"kind", "result"}),
		entryBytes: factory.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "loki_dataobj_index_cache_entry_bytes",
			Help:    "Size of the entries that the index lookup cache stores, by kind of entry.",
			Buckets: prometheus.ExponentialBuckets(64, 4, 8),
		}, []string{"kind"}),
	}
}

// countMissing counts an entry that the backend did not have. It is an error when the backend failed.
func (m *indexLookupCacheMetrics) countMissing(kind string, backendFailed bool) {
	if backendFailed {
		m.requests.WithLabelValues(kind, requestResultError).Inc()
		return
	}
	m.requests.WithLabelValues(kind, requestResultMiss).Inc()
}

func (m *indexLookupCacheMetrics) countStore(kind, result string) {
	m.countStoreN(kind, result, 1)
}

func (m *indexLookupCacheMetrics) countStoreN(kind, result string, n int) {
	m.stores.WithLabelValues(kind, result).Add(float64(n))
}
