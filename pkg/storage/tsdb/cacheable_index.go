package tsdb

import (
	"bytes"
	"context"
	"encoding/gob"

	"github.com/pkg/errors"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/pkg/storage/chunk/cache"
	"github.com/grafana/loki/pkg/storage/tsdb/index"
)

const (
	// sep defines the character used to separate the components of the cache key.
	sep = "/"
)

// CacheableIndex is an implementation of the TSDB index that cache calls to all operations.
type CacheableIndex struct {
	// Index is an implementation of the TSDB index used to evaluate an operation that isn't cached.
	Index

	// Cache is a cache implementation used to store TSDB operation results.
	cache.Cache
}

// NewCacheableIndex instantiate a new cacheable index given an index and a cache implementation.
//
// It assumes the cache is ready to be used.
func NewCacheableIndex(index *TSDBIndex, cache cache.Cache) CacheableIndex {
	return CacheableIndex{
		Index: index,
		Cache: cache,
	}
}

// Stop does everything necessary to stop this index safely.
func (i *CacheableIndex) Stop() error {
	i.Cache.Stop()
	return nil
}

// GetChunkRefs is a cached implementation of GetChunkRefs.
//
// It uses as a key for the cache all parameters (userID, from, through, shard, matchers) separated by a slash (/).
func (i *CacheableIndex) GetChunkRefs(ctx context.Context, userID string, from, through model.Time, res []ChunkRef, shard *index.ShardAnnotation, matchers ...*labels.Matcher) ([]ChunkRef, error) {
	const opName = "GetChunkRefs"

	mountKeyFn := func(userID string, from, through model.Time, shard *index.ShardAnnotation, matcher *labels.Matcher) string {
		stringfiedShard := ""
		if shard != nil {
			stringfiedShard = shard.String()
		}
		if matcher == nil {
			return userID + sep + from.String() + sep + through.String() + stringfiedShard + sep
		}
		return userID + sep + from.String() + sep + through.String() + stringfiedShard + sep + matcher.String()
	}

	fallbackFn := func(ctx context.Context, userID string, from, through model.Time, shard *index.ShardAnnotation, matcher ...*labels.Matcher) ([][]byte, error) {
		series, err := i.Index.GetChunkRefs(ctx, userID, from, through, res, shard, matcher...)
		if err != nil {
			return nil, errors.Wrap(err, "call to GetChunkRefs")
		}

		var encodeBuf bytes.Buffer
		enc := gob.NewEncoder(&encodeBuf)
		if err := enc.Encode(series); err != nil {
			return nil, errors.Wrap(err, "GetChunkRefs encoding")
		}

		return [][]byte{encodeBuf.Bytes()}, nil
	}

	results, err := i.CacheableOp(ctx, opName, mountKeyFn, fallbackFn, userID, from, through, shard, matchers...)
	if err != nil {
		return nil, errors.Wrap(err, "cacheable index call to cacheable op GetChunkRef")
	}

	var values []ChunkRef
	for _, val := range results {
		decoderBuf := bytes.NewBuffer(val)
		dec := gob.NewDecoder(decoderBuf)
		if err := dec.Decode(&values); err != nil {
			return nil, errors.Wrap(err, "cacheable index GetChunkRef decoding")
		}

		res = append(res, values...)
	}

	return res, nil
}

// Series is a cached implementation of Series.
//
// It uses as a key for the cache all parameters (userID, from, through, shard, matchers) separated by a slash (/).
func (i *CacheableIndex) Series(ctx context.Context, userID string, from, through model.Time, res []Series, shard *index.ShardAnnotation, matchers ...*labels.Matcher) ([]Series, error) {
	const opName = "Series"

	mountKeyFn := func(userID string, from, through model.Time, shard *index.ShardAnnotation, matcher *labels.Matcher) string {
		stringfiedShard := ""
		if shard != nil {
			stringfiedShard = shard.String()
		}

		if matcher == nil {
			return userID + sep + from.String() + sep + through.String() + stringfiedShard + sep
		}
		return userID + sep + from.String() + sep + through.String() + stringfiedShard + sep + matcher.String()
	}

	fallbackFn := func(ctx context.Context, userID string, from, through model.Time, shard *index.ShardAnnotation, matcher ...*labels.Matcher) ([][]byte, error) {
		series, err := i.Index.Series(ctx, userID, from, through, res, shard, matcher...)
		if err != nil {
			return nil, errors.Wrap(err, "call to Series")
		}

		var encodeBuf bytes.Buffer
		enc := gob.NewEncoder(&encodeBuf)
		if err := enc.Encode(series); err != nil {
			return nil, errors.Wrap(err, "Series encoding")

		}

		return [][]byte{encodeBuf.Bytes()}, nil
	}

	results, err := i.CacheableOp(ctx, opName, mountKeyFn, fallbackFn, userID, from, through, shard, matchers...)
	if err != nil {
		return nil, errors.Wrap(err, "cacheable index call to cacheable op Series")
	}

	var values []Series
	for _, val := range results {
		decoderBuf := bytes.NewBuffer(val)
		dec := gob.NewDecoder(decoderBuf)
		if err := dec.Decode(&values); err != nil {
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
	const opName = "LabelNames"

	mountKeyFn := func(userID string, from, through model.Time, shard *index.ShardAnnotation, matcher *labels.Matcher) string {
		if matcher == nil {
			return ""
		}
		return matcher.String()
	}

	fallbackFn := func(ctx context.Context, userID string, from, through model.Time, shard *index.ShardAnnotation, matcher ...*labels.Matcher) ([][]byte, error) {
		names, err := i.Index.LabelNames(ctx, userID, from, through, matchers...)
		if err != nil {
			return nil, errors.Wrap(err, "call to LabelNames")
		}

		var encodeBuf bytes.Buffer
		enc := gob.NewEncoder(&encodeBuf)

		if err := enc.Encode(names); err != nil {
			return nil, errors.Wrap(err, "LabelNames encoding")
		}

		return [][]byte{encodeBuf.Bytes()}, nil
	}

	results, err := i.CacheableOp(ctx, opName, mountKeyFn, fallbackFn, userID, from, through, nil, matchers...)
	if err != nil {
		return nil, errors.Wrap(err, "cacheable index call to cacheable op LabelNames")
	}

	var values []string
	var res []string
	for _, val := range results {
		decoderBuf := bytes.NewBuffer(val)
		dec := gob.NewDecoder(decoderBuf)
		if err := dec.Decode(&values); err != nil {
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
	const opName = "LabelValues"

	mountKeyFn := func(userID string, from, through model.Time, shard *index.ShardAnnotation, matcher *labels.Matcher) string {
		if matcher == nil {
			return name
		}
		return name + sep + matcher.String()
	}

	fallbackFn := func(ctx context.Context, userID string, from, through model.Time, shard *index.ShardAnnotation, matcher ...*labels.Matcher) ([][]byte, error) {
		names, err := i.Index.LabelValues(ctx, userID, from, through, name, matchers...)
		if err != nil {
			return nil, errors.Wrap(err, "call to LabelValues")
		}

		var encodeBuf bytes.Buffer
		enc := gob.NewEncoder(&encodeBuf)
		if err := enc.Encode(names); err != nil {
			return nil, errors.Wrap(err, "LabelValues encoding")
		}

		return [][]byte{encodeBuf.Bytes()}, nil
	}

	results, err := i.CacheableOp(ctx, opName, mountKeyFn, fallbackFn, userID, from, through, nil, matchers...)
	if err != nil {
		return nil, errors.Wrap(err, "cacheable index call to cacheable op LabelValues")
	}

	var values []string
	var res []string
	for _, val := range results {
		decoderBuf := bytes.NewBuffer(val)
		dec := gob.NewDecoder(decoderBuf)
		if err := dec.Decode(&values); err != nil {
			return nil, errors.Wrap(err, "cacheable index LabelValues decoding")
		}

		res = append(res, values...)
	}

	return res, nil
}

type mountKeyFunc func(userID string, from model.Time, through model.Time, shard *index.ShardAnnotation, matcher *labels.Matcher) string

type fallbackFunc func(ctx context.Context, userID string, from model.Time, through model.Time, shard *index.ShardAnnotation, matcher ...*labels.Matcher) ([][]byte, error)

// CacheableOp abstracts an operation that cache its results.
//
// It expects two generic functions: one to mount the key used to store and retrieve from cache and one that is invoked
// whenever a cache miss occur, called fallbackFunc.
// It uses the opName as a prefix key to insert in the cache to avoid collisions between different operations.
func (i *CacheableIndex) CacheableOp(ctx context.Context, opName string, keyFn mountKeyFunc, fallbackFn fallbackFunc,
	userID string, from, through model.Time, shard *index.ShardAnnotation, matchers ...*labels.Matcher) ([][]byte, error) {
	var res [][]byte

	// prepare a mapping of keys to matchers, used to retrieve matchers later.
	keysMapping := make(map[string][]*labels.Matcher, len(matchers))
	if len(matchers) == 0 {
		key := keyFn(userID, from, through, shard, nil)
		keysMapping[opName+sep+key] = []*labels.Matcher{}
	}
	for _, matcher := range matchers {
		key := keyFn(userID, from, through, shard, matcher)
		keysMapping[opName+sep+key] = []*labels.Matcher{matcher}
	}

	// define keys used to fetch data from cache.
	var keys []string
	for k := range keysMapping {
		keys = append(keys, k)
	}

	// fetch data from cache.
	_ /* hits */, response, misses, err := i.Cache.Fetch(ctx, keys)
	if err != nil {
		return nil, errors.Wrap(err, "cacheable op cache fetch")
	}

	res = append(res, response...)

	// fill misses and populate cache with them.
	for _, miss := range misses {
		missedMatchers := keysMapping[miss]
		result, err := fallbackFn(ctx, userID, from, through, shard, missedMatchers...)
		if err != nil {
			return nil, errors.Wrap(err, "fallback call")
		}
		res = append(res, result...)

		if err := i.Cache.Store(ctx, []string{miss}, result); err != nil {
			return nil, errors.Wrap(err, "cacheable op cache store")
		}
	}

	return res, nil
}
