package util

import (
	"bytes"
	"context"
	"strings"

	"github.com/weaveworks/cortex/pkg/chunk"
)

// DoSingleQuery is the interface for indexes that don't support batching yet.
type DoSingleQuery func(
	ctx context.Context, query chunk.IndexQuery,
	callback func(chunk.ReadBatch) bool,
) error

// DoParallelQueries translates between our interface for query batching,
// and indexes that don't yet support batching.
func DoParallelQueries(
	ctx context.Context, doSingleQuery DoSingleQuery, queries []chunk.IndexQuery,
	callback func(chunk.IndexQuery, chunk.ReadBatch) bool,
) error {
	incomingErrors := make(chan error)
	for _, query := range queries {
		go func(query chunk.IndexQuery) {
			incomingErrors <- doSingleQuery(ctx, query, func(r chunk.ReadBatch) bool {
				return callback(query, r)
			})
		}(query)
	}
	var lastErr error
	for i := 0; i < len(queries); i++ {
		err := <-incomingErrors
		if err != nil {

			lastErr = err
		}
	}
	return lastErr
}

// Callback from an IndexQuery.
type Callback func(chunk.IndexQuery, chunk.ReadBatch) bool

type filteringBatch struct {
	query chunk.IndexQuery
	chunk.ReadBatch
}

func (f *filteringBatch) Next() bool {
	for f.ReadBatch.Next() {
		rangeValue, value := f.ReadBatch.RangeValue(), f.ReadBatch.Value()

		if len(f.query.RangeValuePrefix) != 0 && !strings.HasPrefix(string(rangeValue), string(f.query.RangeValuePrefix)) {
			continue
		}
		if len(f.query.RangeValueStart) != 0 && string(rangeValue) < string(f.query.RangeValueStart) {
			continue
		}
		if len(f.query.ValueEqual) != 0 && !bytes.Equal(value, f.query.ValueEqual) {
			continue
		}

		return true
	}

	return false
}

// QueryFilter wraps a callback to ensure the results are filtered correctly;
// useful for the cache and BigTable backend, which only ever fetches the whole
// row.
func QueryFilter(callback Callback) Callback {
	return func(query chunk.IndexQuery, batch chunk.ReadBatch) bool {
		return callback(query, &filteringBatch{query, batch})
	}
}
