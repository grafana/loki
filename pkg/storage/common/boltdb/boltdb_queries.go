package boltdb

import (
	"context"
	"sync"
	"unsafe"
)

const (
	maxQueriesBatch = 100
	maxConcurrency  = 10
)

type QueryIndexFunc func(ctx context.Context, queries []Query, callback QueryPagesCallback) error

// QueriesByTable groups and returns queries by tables.
func QueriesByTable(queries []Query) map[string][]Query {
	queriesByTable := make(map[string][]Query)
	for _, query := range queries {
		if _, ok := queriesByTable[query.TableName]; !ok {
			queriesByTable[query.TableName] = []Query{}
		}

		queriesByTable[query.TableName] = append(queriesByTable[query.TableName], query)
	}

	return queriesByTable
}

// NewSyncCallbackDeduper should always be used on table level not the whole query level because it just looks at range values which can be repeated across tables
// NewSyncCallbackDeduper is safe to used by multiple goroutines
// Cortex anyways dedupes entries across tables
func NewSyncCallbackDeduper(callback QueryPagesCallback, queries int) QueryPagesCallback {
	syncMap := &syncMap{
		seen: make(map[string]map[string]struct{}, queries),
	}
	return func(q Query, rbr ReadBatchResult) bool {
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
func NewCallbackDeduper(callback QueryPagesCallback, queries int) QueryPagesCallback {
	f := &readBatchDeduper{
		seen: make(map[string]map[string]struct{}, queries),
	}
	return func(q Query, rbr ReadBatchResult) bool {
		f.hashValue = q.HashValue
		f.ReadBatchIterator = rbr.Iterator()
		return callback(q, f)
	}
}

type readBatchDeduper struct {
	ReadBatchIterator
	hashValue string
	seen      map[string]map[string]struct{}
}

func (f *readBatchDeduper) Iterator() ReadBatchIterator {
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
	ReadBatchIterator
	hashValue string
	*syncMap
}

func (f *readBatchDeduperSync) Iterator() ReadBatchIterator {
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

func GetUnsafeBytes(s string) []byte {
	return *((*[]byte)(unsafe.Pointer(&s))) // #nosec G103 -- we know the string is not mutated -- nosemgrep: use-of-unsafe-block
}

func GetUnsafeString(buf []byte) string {
	return *((*string)(unsafe.Pointer(&buf))) // #nosec G103 -- we know the string is not mutated -- nosemgrep: use-of-unsafe-block
}
