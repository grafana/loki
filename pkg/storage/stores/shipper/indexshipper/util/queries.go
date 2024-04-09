package util

import (
	"context"
	"sync"

	"github.com/grafana/dskit/concurrency"

	"github.com/grafana/loki/v3/pkg/storage/stores/series/index"
	util_math "github.com/grafana/loki/v3/pkg/util/math"
)

const (
	maxQueriesBatch = 100
	maxConcurrency  = 10
)

type QueryIndexFunc func(ctx context.Context, queries []index.Query, callback index.QueryPagesCallback) error

// QueriesByTable groups and returns queries by tables.
func QueriesByTable(queries []index.Query) map[string][]index.Query {
	queriesByTable := make(map[string][]index.Query)
	for _, query := range queries {
		if _, ok := queriesByTable[query.TableName]; !ok {
			queriesByTable[query.TableName] = []index.Query{}
		}

		queriesByTable[query.TableName] = append(queriesByTable[query.TableName], query)
	}

	return queriesByTable
}

func DoParallelQueries(ctx context.Context, queryIndex QueryIndexFunc, queries []index.Query, callback index.QueryPagesCallback) error {
	if len(queries) == 0 {
		return nil
	}
	if len(queries) <= maxQueriesBatch {
		return queryIndex(ctx, queries, NewCallbackDeduper(callback, len(queries)))
	}

	jobsCount := len(queries) / maxQueriesBatch
	if len(queries)%maxQueriesBatch != 0 {
		jobsCount++
	}
	callback = NewSyncCallbackDeduper(callback, len(queries))
	return concurrency.ForEachJob(ctx, jobsCount, maxConcurrency, func(ctx context.Context, idx int) error {
		return queryIndex(ctx, queries[idx*maxQueriesBatch:util_math.Min((idx+1)*maxQueriesBatch, len(queries))], callback)
	})
}

// NewSyncCallbackDeduper should always be used on table level not the whole query level because it just looks at range values which can be repeated across tables
// NewSyncCallbackDeduper is safe to used by multiple goroutines
// Cortex anyways dedupes entries across tables
func NewSyncCallbackDeduper(callback index.QueryPagesCallback, queries int) index.QueryPagesCallback {
	syncMap := &syncMap{
		seen: make(map[string]map[string]struct{}, queries),
	}
	return func(q index.Query, rbr index.ReadBatchResult) bool {
		return callback(q, &readBatchDeduperSync{
			syncMap:           syncMap,
			hashValue:         q.HashValue,
			ReadBatchIterator: rbr.Iterator(),
		})
	}
}

// NewCallbackDeduper should always be used on table level not the whole query level because it just looks at range values which can be repeated across tables
// NewCallbackDeduper is safe not to used by multiple goroutines
// Cortex anyways dedupes entries across tables
func NewCallbackDeduper(callback index.QueryPagesCallback, queries int) index.QueryPagesCallback {
	f := &readBatchDeduper{
		seen: make(map[string]map[string]struct{}, queries),
	}
	return func(q index.Query, rbr index.ReadBatchResult) bool {
		f.hashValue = q.HashValue
		f.ReadBatchIterator = rbr.Iterator()
		return callback(q, f)
	}
}

type readBatchDeduper struct {
	index.ReadBatchIterator
	hashValue string
	seen      map[string]map[string]struct{}
}

func (f *readBatchDeduper) Iterator() index.ReadBatchIterator {
	return f
}

func (f *readBatchDeduper) Next() bool {
	for f.ReadBatchIterator.Next() {
		rangeValue := f.RangeValue()
		hashes, ok := f.seen[f.hashValue]
		if !ok {
			hashes = map[string]struct{}{}
			hashes[GetUnsafeString(rangeValue)] = struct{}{}
			f.seen[f.hashValue] = hashes
			return true
		}
		h := GetUnsafeString(rangeValue)
		if _, loaded := hashes[h]; loaded {
			continue
		}
		hashes[h] = struct{}{}
		return true
	}

	return false
}

type syncMap struct {
	seen map[string]map[string]struct{}
	rw   sync.RWMutex // nolint: structcheck
}

type readBatchDeduperSync struct {
	index.ReadBatchIterator
	hashValue string
	*syncMap
}

func (f *readBatchDeduperSync) Iterator() index.ReadBatchIterator {
	return f
}

func (f *readBatchDeduperSync) Next() bool {
	for f.ReadBatchIterator.Next() {
		rangeValue := f.RangeValue()
		f.rw.RLock()
		hashes, ok := f.seen[f.hashValue]
		if ok {
			h := GetUnsafeString(rangeValue)
			if _, loaded := hashes[h]; loaded {
				f.rw.RUnlock()
				continue
			}
			f.rw.RUnlock()
			f.rw.Lock()
			if _, loaded := hashes[h]; loaded {
				f.rw.Unlock()
				continue
			}
			hashes[h] = struct{}{}
			f.rw.Unlock()
			return true
		}
		f.rw.RUnlock()
		f.rw.Lock()
		if _, ok := f.seen[f.hashValue]; ok {
			f.rw.Unlock()
			continue
		}
		f.seen[f.hashValue] = map[string]struct{}{
			GetUnsafeString(rangeValue): {},
		}
		f.rw.Unlock()
		return true
	}

	return false
}
