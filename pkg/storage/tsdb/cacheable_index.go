package tsdb

import (
	"bytes"
	"context"
	"encoding/gob"
	"fmt"
	"strings"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/pkg/storage/chunk/cache"
	"github.com/grafana/loki/pkg/storage/tsdb/index"
)

const (
	// sep defines the character used to separate the components of the cache key.
	sep = ":"
)

// CacheableIndex is an implementation of the TSDB index that cache calls to all operations.
type CacheableIndex struct {
	// Index is an implementation of the TSDB index used to evaluate an operation that isn't cached.
	Index

	// Cache is a cache implementation used to store TSDB operation results.
	cache.Cache

	m      CacheableIndexMetrics
	logger log.Logger
}

type CacheableIndexMetrics struct {
	cacheDecodeErrs prometheus.Counter
	cacheEncodeErrs prometheus.Counter
	cacheHits       prometheus.Counter
	cacheMisses     prometheus.Counter
	cacheGets       prometheus.Counter
	cacheGetErrors  prometheus.Counter
	cachePuts       prometheus.Counter
	cachePutErrors  prometheus.Counter
}

func NewCacheMetrics(reg prometheus.Registerer) CacheableIndexMetrics {
	// we may want these metrics to contain the tenant/user id
	m := CacheableIndexMetrics{
		cacheDecodeErrs: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "loki",
			Name:      "tsdb_index_cache_decode_errors_total",
			Help:      "The number of cache decode errors for the tsdb index cache.",
		}),
		cacheEncodeErrs: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "loki",
			Name:      "tsdb_index_cache_encode_errors_total",
			Help:      "The number of errors for the tsdb index cache while encoding the body.",
		}),
		cacheHits: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "loki",
			Name:      "tsdb_index_cache_hits_total",
			Help:      "The number of cache hits for the tsdb index cache.",
		}),
		cacheMisses: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "loki",
			Name:      "tsdb_index_cache_misses_total",
			Help:      "The number of cache misses for the tsdb index cache.",
		}),
		cacheGets: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "loki",
			Name:      "tsdb_index_cache_gets_total",
			Help:      "The number of gets for the tsdb index cache.",
		}),
		cacheGetErrors: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "loki",
			Name:      "tsdb_index_cache_get_errors_total",
			Help:      "The number of gets for the tsdb index cache that failed.",
		}),
		cachePuts: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "loki",
			Name:      "tsdb_index_cache_puts_total",
			Help:      "The number of puts for the tsdb index cache.",
		}),
		cachePutErrors: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "loki",
			Name:      "tsdb_index_cache_put_errors_total",
			Help:      "The number of puts for the tsdb index cache that failed.",
		}),
	}

	if reg != nil {
		reg.MustRegister(m.cacheDecodeErrs)
		reg.MustRegister(m.cacheHits)
		reg.MustRegister(m.cacheMisses)
		reg.MustRegister(m.cacheGets)
		reg.MustRegister(m.cacheGetErrors)
		reg.MustRegister(m.cachePuts)
		reg.MustRegister(m.cachePutErrors)
		reg.MustRegister(m.cacheEncodeErrs)
	}

	return m
}

// NewCacheableIndex instantiate a new cacheable index given an index and a cache implementation.
//
// It assumes the cache is ready to be used.
func NewCacheableIndex(l log.Logger, index *TSDBIndex, cache cache.Cache, m CacheableIndexMetrics) CacheableIndex {
	return CacheableIndex{
		Index:  index,
		Cache:  cache,
		m:      m,
		logger: l,
	}
}

// Stop does everything necessary to stop this index safely.
func (i *CacheableIndex) Stop() {
	i.Cache.Stop()
}

// stringifiedMatchers build and return a promQL string based on the union
// of all the given matchers.
func stringifiedMatchers(matchers []*labels.Matcher) string {
	b := strings.Builder{}

	b.WriteByte('{')
	for i, m := range matchers {
		if i > 0 {
			b.WriteByte(',')
			b.WriteByte(' ')
		}

		b.WriteString(m.String())
	}
	b.WriteByte('}')

	return b.String()
}

// buildCacheKey constructs the key used to retrieve and store values from/into the cache.
//
// It joins all parameters with the collon operator to avoid collisions.
func (i *CacheableIndex) buildCacheKey(userID string, from, through model.Time, shard *index.ShardAnnotation, matchers ...*labels.Matcher) string {
	stringifiedShard := fmt.Sprint(shard)
	keyMembers := []string{
		userID, from.String(), through.String(), stringifiedShard,
	}
	if len(matchers) != 0 {
		keyMembers = append(keyMembers, stringifiedMatchers(matchers))
	}

	return strings.Join(keyMembers, sep)
}

// GetChunkRefs is a cached implementation of GetChunkRefs.
//
// It uses as a key for the cache all parameters (userID, from, through, shard, matchers) separated by a colon (:).
func (i *CacheableIndex) GetChunkRefs(ctx context.Context, userID string, from, through model.Time, res []ChunkRef, shard *index.ShardAnnotation, matchers ...*labels.Matcher) ([]ChunkRef, error) {
	fallbackFn := func(ctx context.Context, userID string, from, through model.Time, shard *index.ShardAnnotation, matcher ...*labels.Matcher) ([][]byte, error) {
		series, err := i.Index.GetChunkRefs(ctx, userID, from, through, res, shard, matcher...)
		if err != nil {
			return nil, errors.Wrap(err, "call to GetChunkRefs")
		}

		var encodeBuf bytes.Buffer
		enc := gob.NewEncoder(&encodeBuf)
		if err := enc.Encode(series); err != nil {
			i.m.cacheEncodeErrs.Inc()
			return nil, errors.Wrap(err, "GetChunkRefs encoding")
		}

		return [][]byte{encodeBuf.Bytes()}, nil
	}

	results, err := i.CacheableOp(ctx, "GetChunkRefs", i.buildCacheKey, fallbackFn, userID, from, through, shard, matchers...)
	if err != nil {
		return nil, errors.Wrap(err, "cacheable index call to cacheable op GetChunkRef")
	}

	var values []ChunkRef
	for _, val := range results {
		decoderBuf := bytes.NewBuffer(val)
		dec := gob.NewDecoder(decoderBuf)
		if err := dec.Decode(&values); err != nil {
			i.m.cacheDecodeErrs.Inc()
			return nil, errors.Wrap(err, "cacheable index GetChunkRef decoding")
		}

		res = append(res, values...)
	}

	return res, nil
}

// Series is a cached implementation of Series.
//
// It uses as a key for the cache all parameters (userID, from, through, shard, matchers) separated by a colon (:).
func (i *CacheableIndex) Series(ctx context.Context, userID string, from, through model.Time, res []Series, shard *index.ShardAnnotation, matchers ...*labels.Matcher) ([]Series, error) {
	fallbackFn := func(ctx context.Context, userID string, from, through model.Time, shard *index.ShardAnnotation, matcher ...*labels.Matcher) ([][]byte, error) {
		series, err := i.Index.Series(ctx, userID, from, through, res, shard, matcher...)
		if err != nil {
			return nil, errors.Wrap(err, "call to Series")
		}

		var encodeBuf bytes.Buffer
		enc := gob.NewEncoder(&encodeBuf)
		if err := enc.Encode(series); err != nil {
			i.m.cacheEncodeErrs.Inc()
			return nil, errors.Wrap(err, "Series encoding")

		}

		return [][]byte{encodeBuf.Bytes()}, nil
	}

	results, err := i.CacheableOp(ctx, "Series", i.buildCacheKey, fallbackFn, userID, from, through, shard, matchers...)
	if err != nil {
		return nil, errors.Wrap(err, "cacheable index call to cacheable op Series")
	}

	var values []Series
	for _, val := range results {
		decoderBuf := bytes.NewBuffer(val)
		dec := gob.NewDecoder(decoderBuf)
		if err := dec.Decode(&values); err != nil {
			i.m.cacheDecodeErrs.Inc()
			return nil, errors.Wrap(err, "cacheable index Series decoding")
		}

		res = append(res, values...)
	}

	return res, nil
}

// LabelNames is a cached implementation of LabelNames.
//
// It uses as a key for the cache only the matcher.
func (i *CacheableIndex) LabelNames(ctx context.Context, userID string, from, through model.Time, matchers ...*labels.Matcher) ([]string, error) {
	fallbackFn := func(ctx context.Context, userID string, from, through model.Time, shard *index.ShardAnnotation, matcher ...*labels.Matcher) ([][]byte, error) {
		names, err := i.Index.LabelNames(ctx, userID, from, through, matchers...)
		if err != nil {
			return nil, errors.Wrap(err, "call to LabelNames")
		}

		var encodeBuf bytes.Buffer
		enc := gob.NewEncoder(&encodeBuf)

		if err := enc.Encode(names); err != nil {
			i.m.cacheEncodeErrs.Inc()
			return nil, errors.Wrap(err, "LabelNames encoding")
		}

		return [][]byte{encodeBuf.Bytes()}, nil
	}

	results, err := i.CacheableOp(ctx, "LabelNames", i.buildCacheKey, fallbackFn, userID, from, through, nil, matchers...)
	if err != nil {
		return nil, errors.Wrap(err, "cacheable index call to cacheable op LabelNames")
	}

	var values []string
	var res []string
	for _, val := range results {
		decoderBuf := bytes.NewBuffer(val)
		dec := gob.NewDecoder(decoderBuf)
		if err := dec.Decode(&values); err != nil {
			i.m.cacheDecodeErrs.Inc()
			return nil, errors.Wrap(err, "cacheable index LabelNames decoding")
		}

		res = append(res, values...)
	}

	return res, nil
}

// LabelValues is a cached implementation of LabelValues.
//
// It uses as a key for the cache only the matcher and the name parameter.
func (i *CacheableIndex) LabelValues(ctx context.Context, userID string, from, through model.Time, name string, matchers ...*labels.Matcher) ([]string, error) {
	fallbackFn := func(ctx context.Context, userID string, from, through model.Time, shard *index.ShardAnnotation, matcher ...*labels.Matcher) ([][]byte, error) {
		names, err := i.Index.LabelValues(ctx, userID, from, through, name, matchers...)
		if err != nil {
			return nil, errors.Wrap(err, "call to LabelValues")
		}

		var encodeBuf bytes.Buffer
		enc := gob.NewEncoder(&encodeBuf)
		if err := enc.Encode(names); err != nil {
			i.m.cacheEncodeErrs.Inc()
			return nil, errors.Wrap(err, "LabelValues encoding")
		}

		return [][]byte{encodeBuf.Bytes()}, nil
	}

	results, err := i.CacheableOp(ctx, "LabelValues", i.buildCacheKey, fallbackFn, userID, from, through, nil, matchers...)
	if err != nil {
		return nil, errors.Wrap(err, "cacheable index call to cacheable op LabelValues")
	}

	var values []string
	var res []string
	for _, val := range results {
		decoderBuf := bytes.NewBuffer(val)
		dec := gob.NewDecoder(decoderBuf)
		if err := dec.Decode(&values); err != nil {
			i.m.cacheDecodeErrs.Inc()
			return nil, errors.Wrap(err, "cacheable index LabelValues decoding")
		}

		res = append(res, values...)
	}

	return res, nil
}

type mountKeyFunc func(userID string, from model.Time, through model.Time, shard *index.ShardAnnotation, matchers ...*labels.Matcher) string

type fallbackFunc func(ctx context.Context, userID string, from model.Time, through model.Time, shard *index.ShardAnnotation, matcher ...*labels.Matcher) ([][]byte, error)

// CacheableOp abstracts an operation that cache its results.
//
// It expects two generic functions: one to mount the key used to store and retrieve data from cache and one that is invoked
// whenever a cache miss occur, called fallbackFunc.
// It uses the opName as a prefix key to insert in the cache to avoid collisions between different operations.
func (i *CacheableIndex) CacheableOp(ctx context.Context, opName string, keyFn mountKeyFunc, fallbackFn fallbackFunc,
	userID string, from, through model.Time, shard *index.ShardAnnotation, matchers ...*labels.Matcher) ([][]byte, error) {
	var res [][]byte

	// TODO: opName is not used in key function, but function header comment says that it is?
	key := keyFn(userID, from, through, shard, matchers...)
	keys := []string{key}

	// fetch data from cache.
	_ /* hits */, response, misses, err := i.Cache.Fetch(ctx, keys)
	i.m.cacheGets.Inc()
	if err != nil {
		i.m.cacheGetErrors.Inc()
		level.Warn(i.logger).Log("msg", "cache fetch failed for operation", "operation", opName, "error", err)
	}

	i.m.cacheHits.Add(float64(len(response)))
	i.m.cacheMisses.Add(float64(len(misses)))

	res = append(res, response...)

	// It's possible that the cache call failed entirely, and so we need to look for all keys
	if len(response) == 0 && len(misses) == 0 {
		misses = keys
	}

	// fill misses and populate cache with them.
	for _, miss := range misses {
		result, err := fallbackFn(ctx, userID, from, through, shard, matchers...)
		if err != nil {
			return nil, errors.Wrap(err, "fallback call")
		}
		res = append(res, result...)
		i.m.cachePuts.Inc()
		if err := i.Cache.Store(ctx, []string{miss}, result); err != nil {
			i.m.cachePutErrors.Inc()
			return nil, errors.Wrap(err, "cacheable op cache store")
		}
	}

	return res, nil
}
