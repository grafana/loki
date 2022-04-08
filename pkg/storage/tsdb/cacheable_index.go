package tsdb

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/gogo/protobuf/proto"
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
	fallbackFn := func(ctx context.Context, userID string, from, through model.Time, shard *index.ShardAnnotation, matcher ...*labels.Matcher) ([]ChunkRef, []byte, error) {
		chunks, err := i.Index.GetChunkRefs(ctx, userID, from, through, res, shard, matcher...)
		if err != nil {
			return nil, nil, errors.Wrap(err, "call to GetChunkRefs")
		}

		buf, err := proto.Marshal(&ChunkRefs{chunks})
		if err != nil {
			return nil, nil, errors.Wrap(err, "Series encoding")
		}

		return chunks, buf, nil
	}

	key := i.buildCacheKey(userID, from, through, shard, matchers...)
	keys := []string{key}
	_, response, misses := i.cacheFetch(ctx, keys)
	// It's possible that the cache call failed entirely, and so we need to look for all keys
	if len(response) == 0 && len(misses) == 0 {
		misses = keys
	}

	// Turn the cache result into the right type
	// TODO: Callum, is this even any faster to encode/decode than using gob since with a slice of protobuf types we likely
	// have to do similar run time calculations on the size of each struct in the slice of protobuf structs that is ChunkRefs
	var value ChunkRefs
	for _, val := range response {
		if err := proto.Unmarshal(val, &value); err != nil {
			return nil, errors.Wrap(err, "cacheable index Series decoding")
		}

		res = append(res, value.Refs...)
	}

	// fill misses and populate cache with them.
	for _, miss := range misses {
		result, resultBytes, err := fallbackFn(ctx, userID, from, through, shard, matchers...)
		if err != nil {
			return nil, errors.Wrap(err, "fallback call failed in Series")
		}
		res = append(res, result...)

		if err := i.Cache.Store(ctx, []string{miss}, [][]byte{resultBytes}); err != nil {
			return nil, errors.Wrap(err, "cache store failed in Series")
		}
	}

	return res, nil
}

// Series is a cached implementation of Series.
//
// It uses as a key for the cache all parameters (userID, from, through, shard, matchers) separated by a colon (:).
func (i *CacheableIndex) Series(ctx context.Context, userID string, from, through model.Time, res []Series, shard *index.ShardAnnotation, matchers ...*labels.Matcher) ([]Series, error) {
	fallbackFn := func(ctx context.Context, userID string, from, through model.Time, shard *index.ShardAnnotation, matcher ...*labels.Matcher) ([]Series, []byte, error) {
		series, err := i.Index.Series(ctx, userID, from, through, res, shard, matcher...)
		if err != nil {
			return nil, nil, errors.Wrap(err, "call to Series")
		}

		buf, err := proto.Marshal(&MutliSeries{series})
		if err != nil {
			return nil, nil, errors.Wrap(err, "Series encoding")
		}

		return series, buf, nil
	}

	key := i.buildCacheKey(userID, from, through, shard, matchers...)
	keys := []string{key}
	_, response, misses := i.cacheFetch(ctx, keys)
	// It's possible that the cache call failed entirely, and so we need to look for all keys
	if len(response) == 0 && len(misses) == 0 {
		misses = keys
	}

	// Turn the cache result into the right type
	var value MutliSeries
	for _, val := range response {
		err := proto.Unmarshal(val, &value)
		if err != nil {
			return nil, errors.Wrap(err, "cacheable index Series decoding")
		}

		res = append(res, value.Series...)
	}

	// fill misses and populate cache with them.
	for _, miss := range misses {
		result, resultBytes, err := fallbackFn(ctx, userID, from, through, shard, matchers...)
		if err != nil {
			return nil, errors.Wrap(err, "fallback call failed in Series")
		}
		res = append(res, result...)
		if err := i.Cache.Store(ctx, []string{miss}, [][]byte{resultBytes}); err != nil {
			return nil, errors.Wrap(err, "cache store failed in Series")
		}
	}

	return res, nil
}

// LabelNames is a cached implementation of LabelNames.
//
// It uses as a key for the cache only the matcher.
func (i *CacheableIndex) LabelNames(ctx context.Context, userID string, from, through model.Time, matchers ...*labels.Matcher) ([]string, error) {
	fallbackFn := func(ctx context.Context, userID string, from, through model.Time, matchers ...*labels.Matcher) ([]string, [][]byte, error) {
		names, err := i.Index.LabelNames(ctx, userID, from, through, matchers...)
		if err != nil {
			return nil, nil, errors.Wrap(err, "call to LabelNames")
		}

		var encodeBuf bytes.Buffer
		for _, n := range names {
			encodeBuf.WriteString(n + ",")
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
	var value string
	var err error
	for _, val := range response {
		decoderBuf := bytes.NewBuffer(val)
		for {
			value, err = decoderBuf.ReadString(',')
			value, _, _ = cutStr(value, ",")
			if err != nil && err != io.EOF {
				i.m.cacheDecodeErrs.Inc()
				return nil, errors.Wrap(err, "cacheable index LabelNames decoding")
			}
			if err == io.EOF {
				break
			}

			res = append(res, value)
		}
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
func (i *CacheableIndex) LabelValues(ctx context.Context, userID string, from, through model.Time, name string, matchers ...*labels.Matcher) ([]string, error) {
	fallbackFn := func(ctx context.Context, userID string, from, through model.Time, matchers ...*labels.Matcher) ([]string, [][]byte, error) {
		names, err := i.Index.LabelValues(ctx, userID, from, through, name, matchers...)
		if err != nil {
			return nil, nil, errors.Wrap(err, "call to LabelValues")
		}

		var encodeBuf bytes.Buffer
		for _, n := range names {
			encodeBuf.WriteString(n + ",")
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
	var value string
	var err error
	for _, val := range response {
		decoderBuf := bytes.NewBuffer(val)
		for {
			value, err = decoderBuf.ReadString(',')
			value, _, _ = cutStr(value, ",")
			if err != nil && err != io.EOF {
				i.m.cacheDecodeErrs.Inc()
				return nil, errors.Wrap(err, "cacheable index LabelValues decoding")
			}
			if err == io.EOF {
				break
			}

			res = append(res, value)
		}
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
func (i *CacheableIndex) cacheFetch(ctx context.Context, cacheKeys []string) ([]string, [][]byte, []string) {
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

// labelsToLabelsProto transforms labels into proto labels. The buffer slice
// will be used to avoid allocations if it is big enough to store the labels.
// TODO: Callum, this is copied from prometheus and some of the proto definitions I've added
// are copied as well, is there a nice way to not do this (ie import some proto + define new types using those imported types)
func labelsToLabelsProto(labels labels.Labels, buf []Label) Labels {
	result := buf[:0]
	if cap(buf) < len(labels) {
		result = make([]Label, 0, len(labels))
	}
	for _, l := range labels {
		result = append(result, Label{
			Name:  l.Name,
			Value: l.Value,
		})
	}
	return Labels{result}
}

// cutStr is a temporary implementation copy of strings.Cut.
//
// strings.Cut was added in Go v1.18, but since Loki hasn't migrated to it,
// we are using a forked copy of the new Cut method.
// Source code reference: https://github.com/golang/go/issues/46336
func cutStr(s, sep string) (before, after string, found bool) {
	if i := strings.Index(s, sep); i >= 0 {
		return s[:i], s[i+len(sep):], true
	}
	return s, "", false
}
