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
	"github.com/grafana/loki/pkg/storage/stores/tsdb/index"
)

type OldCacheableIndex struct {
	// Index is an implementation of the TSDB index used to evaluate an operation that isn't cached.
	Index

	// Cache is a cache implementation used to store TSDB operation results.
	cache.Cache

	m      OldCacheableIndexMetrics
	logger log.Logger
}

type OldCacheableIndexMetrics struct {
	cacheDecodeErrs prometheus.Counter
	cacheEncodeErrs prometheus.Counter
	cacheHits       prometheus.Counter
	cacheMisses     prometheus.Counter
	cacheGets       prometheus.Counter
	cacheGetErrors  prometheus.Counter
	cachePuts       prometheus.Counter
	cachePutErrors  prometheus.Counter
}

func NewOldCacheMetrics(reg prometheus.Registerer) OldCacheableIndexMetrics {
	// we may want these metrics to contain the tenant/user id
	m := OldCacheableIndexMetrics{
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

func NewOldCacheableIndex(l log.Logger, index *TSDBIndex, cache cache.Cache, m OldCacheableIndexMetrics) OldCacheableIndex {
	return OldCacheableIndex{
		Index:  index,
		Cache:  cache,
		m:      m,
		logger: l,
	}
}

// Stop does everything necessary to stop this index safely.
func (i *OldCacheableIndex) Stop() {
	i.Cache.Stop()
}

func (i *OldCacheableIndex) buildCacheKey(userID string, from, through model.Time, shard *index.ShardAnnotation, matchers ...*labels.Matcher) string {
	stringifiedShard := fmt.Sprint(shard)
	keyMembers := []string{
		userID, from.String(), through.String(), stringifiedShard,
	}
	if len(matchers) != 0 {
		keyMembers = append(keyMembers, stringifiedMatchers(matchers))
	}

	return strings.Join(keyMembers, sep)
}

func (i *OldCacheableIndex) GetChunkRefs(ctx context.Context, userID string, from, through model.Time, res []ChunkRef, shard *index.ShardAnnotation, matchers ...*labels.Matcher) ([]ChunkRef, error) {
	fallbackFn := func(ctx context.Context, userID string, from, through model.Time, shard *index.ShardAnnotation, matcher ...*labels.Matcher) ([]ChunkRef, [][]byte, error) {
		chunks, err := i.Index.GetChunkRefs(ctx, userID, from, through, res, shard, matcher...)
		if err != nil {
			return nil, nil, errors.Wrap(err, "call to GetChunkRefs")
		}

		var encodeBuf bytes.Buffer
		enc := gob.NewEncoder(&encodeBuf)
		if err := enc.Encode(chunks); err != nil {
			i.m.cacheEncodeErrs.Inc()
			return nil, nil, errors.Wrap(err, "GetChunkRefs encoding")
		}

		return chunks, [][]byte{encodeBuf.Bytes()}, nil
	}

	key := i.buildCacheKey(userID, from, through, shard, matchers...)
	keys := []string{key}
	_, response, misses := i.cacheFetch(ctx, keys)
	// It's possible that the cache call failed entirely, and so we need to look for all keys
	if len(response) == 0 && len(misses) == 0 {
		misses = keys
	}

	// Turn the cache result into the right type
	var values []ChunkRef
	for _, val := range response {
		decoderBuf := bytes.NewBuffer(val)
		dec := gob.NewDecoder(decoderBuf)
		if err := dec.Decode(&values); err != nil {
			i.m.cacheDecodeErrs.Inc()
			return nil, errors.Wrap(err, "cacheable index Series decoding")
		}

		res = append(res, values...)
	}

	// fill misses and populate cache with them.
	for _, miss := range misses {
		result, resultBytes, err := fallbackFn(ctx, userID, from, through, shard, matchers...)
		if err != nil {
			return nil, errors.Wrap(err, "fallback call failed in Series")
		}
		res = append(res, result...)

		if err := i.Cache.Store(ctx, []string{miss}, resultBytes); err != nil {
			return nil, errors.Wrap(err, "cache store failed in Series")
		}
	}

	return res, nil
}

func (i *OldCacheableIndex) Series(ctx context.Context, userID string, from, through model.Time, res []Series, shard *index.ShardAnnotation, matchers ...*labels.Matcher) ([]Series, error) {
	fallbackFn := func(ctx context.Context, userID string, from, through model.Time, shard *index.ShardAnnotation, matcher ...*labels.Matcher) ([]Series, [][]byte, error) {
		series, err := i.Index.Series(ctx, userID, from, through, res, shard, matcher...)
		if err != nil {
			return nil, nil, errors.Wrap(err, "call to Series")
		}

		var encodeBuf bytes.Buffer
		enc := gob.NewEncoder(&encodeBuf)
		if err := enc.Encode(series); err != nil {
			return nil, nil, errors.Wrap(err, "Series encoding")

		}

		return series, [][]byte{encodeBuf.Bytes()}, nil
	}

	key := i.buildCacheKey(userID, from, through, shard, matchers...)
	keys := []string{key}
	_, response, misses := i.cacheFetch(ctx, keys)
	// It's possible that the cache call failed entirely, and so we need to look for all keys
	if len(response) == 0 && len(misses) == 0 {
		misses = keys
	}

	// Turn the cache result into the right type
	var values []Series
	for _, val := range response {
		decoderBuf := bytes.NewBuffer(val)
		dec := gob.NewDecoder(decoderBuf)
		if err := dec.Decode(&values); err != nil {
			return nil, errors.Wrap(err, "cacheable index Series decoding")
		}

		res = append(res, values...)
	}

	// fill misses and populate cache with them.
	for _, miss := range misses {
		result, resultBytes, err := fallbackFn(ctx, userID, from, through, shard, matchers...)
		if err != nil {
			return nil, errors.Wrap(err, "fallback call failed in Series")
		}
		res = append(res, result...)

		if err := i.Cache.Store(ctx, []string{miss}, resultBytes); err != nil {
			return nil, errors.Wrap(err, "cache store failed in Series")
		}
	}

	return res, nil
}

func (i *OldCacheableIndex) LabelNames(ctx context.Context, userID string, from, through model.Time, matchers ...*labels.Matcher) ([]string, error) {
	fallbackFn := func(ctx context.Context, userID string, from, through model.Time, matchers ...*labels.Matcher) ([]string, [][]byte, error) {
		names, err := i.Index.LabelNames(ctx, userID, from, through, matchers...)
		if err != nil {
			return nil, nil, errors.Wrap(err, "call to LabelNames")
		}

		var encodeBuf bytes.Buffer
		enc := gob.NewEncoder(&encodeBuf)

		if err := enc.Encode(names); err != nil {
			i.m.cacheEncodeErrs.Inc()
			return nil, nil, errors.Wrap(err, "LabelNames encoding")
		}

		return names, [][]byte{encodeBuf.Bytes()}, nil
	}

	key := i.buildCacheKey(userID, from, through, nil, matchers...)
	keys := []string{key}
	_, response, misses := i.cacheFetch(ctx, keys)
	// It's possible that the cache call failed entirely, and so we need to look for all keys
	if len(response) == 0 && len(misses) == 0 {
		misses = keys
	}

	// Turn the cache result into the right type
	var values []string
	var res []string
	for _, val := range response {
		decoderBuf := bytes.NewBuffer(val)
		dec := gob.NewDecoder(decoderBuf)
		if err := dec.Decode(&values); err != nil {
			i.m.cacheDecodeErrs.Inc()
			return nil, errors.Wrap(err, "cacheable index LabelNames decoding")
		}

		res = append(res, values...)
	}

	// fill misses and populate cache with them.
	for _, miss := range misses {
		result, resultBytes, err := fallbackFn(ctx, userID, from, through, matchers...)
		if err != nil {
			return nil, errors.Wrap(err, "fallback call failed in LabelNames")
		}
		res = append(res, result...)

		if err := i.Cache.Store(ctx, []string{miss}, resultBytes); err != nil {
			return nil, errors.Wrap(err, "cache store failed in LabelNames")
		}
	}

	return res, nil
}

// LabelValues is a cached implementation of LabelValues.
//
// It uses as a key for the cache only the matcher and the name parameter.
func (i *OldCacheableIndex) LabelValues(ctx context.Context, userID string, from, through model.Time, name string, matchers ...*labels.Matcher) ([]string, error) {
	fallbackFn := func(ctx context.Context, userID string, from, through model.Time, matchers ...*labels.Matcher) ([]string, [][]byte, error) {
		names, err := i.Index.LabelValues(ctx, userID, from, through, name, matchers...)
		if err != nil {
			return nil, nil, errors.Wrap(err, "call to LabelValues")
		}

		var encodeBuf bytes.Buffer
		enc := gob.NewEncoder(&encodeBuf)
		if err := enc.Encode(names); err != nil {
			i.m.cacheEncodeErrs.Inc()
			return nil, nil, errors.Wrap(err, "LabelValues encoding")
		}

		return names, [][]byte{encodeBuf.Bytes()}, nil
	}

	key := i.buildCacheKey(userID, from, through, nil, matchers...)
	keys := []string{key}
	_, response, misses := i.cacheFetch(ctx, keys)
	// It's possible that the cache call failed entirely, and so we need to look for all keys
	if len(response) == 0 && len(misses) == 0 {
		misses = keys
	}

	// Turn the cache result into the right type
	var res []string
	var values []string
	for _, val := range response {
		decoderBuf := bytes.NewBuffer(val)
		dec := gob.NewDecoder(decoderBuf)
		if err := dec.Decode(&values); err != nil {
			i.m.cacheDecodeErrs.Inc()
			return nil, errors.Wrap(err, "cacheable index LabelValues decoding")
		}

		res = append(res, values...)
	}

	// fill misses and populate cache with them.
	for _, miss := range misses {
		result, resultBytes, err := fallbackFn(ctx, userID, from, through, matchers...)
		if err != nil {
			return nil, errors.Wrap(err, "fallback call failed in LabelValues")
		}
		res = append(res, result...)
		i.m.cachePuts.Inc()
		if err := i.Cache.Store(ctx, []string{miss}, resultBytes); err != nil {
			i.m.cachePutErrors.Inc()
			return nil, errors.Wrap(err, "cacheable op cache store")
		}
	}

	return res, nil
}

// wrapper for cache fetch calls that handles metrics for you
func (i *OldCacheableIndex) cacheFetch(ctx context.Context, cacheKeys []string) ([]string, [][]byte, []string) {
	found, response, misses, err := i.Cache.Fetch(ctx, cacheKeys)
	i.m.cacheGets.Inc()
	if err != nil {
		i.m.cacheGetErrors.Inc()
		level.Warn(i.logger).Log("msg", "cache fetch failed", "error", err)
	}

	i.m.cacheHits.Add(float64(len(response)))
	i.m.cacheMisses.Add(float64(len(misses)))

	return found, response, misses
}
