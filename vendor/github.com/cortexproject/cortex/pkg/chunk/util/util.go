package util

import (
	"bytes"
	"context"
	"fmt"
	"os"

	"io"

	ot "github.com/opentracing/opentracing-go"

	"github.com/cortexproject/cortex/pkg/chunk"
	"github.com/cortexproject/cortex/pkg/util/math"
)

// Callback from an IndexQuery.
type Callback func(chunk.IndexQuery, chunk.ReadBatch) bool

// DoSingleQuery is the interface for indexes that don't support batching yet.
type DoSingleQuery func(context.Context, chunk.IndexQuery, Callback) error

// QueryParallelism is the maximum number of subqueries run in
// parallel per higher-level query
var QueryParallelism = 100

// DoParallelQueries translates between our interface for query batching,
// and indexes that don't yet support batching.
func DoParallelQueries(
	ctx context.Context, doSingleQuery DoSingleQuery, queries []chunk.IndexQuery,
	callback Callback,
) error {
	if len(queries) == 1 {
		return doSingleQuery(ctx, queries[0], callback)
	}

	queue := make(chan chunk.IndexQuery)
	incomingErrors := make(chan error)
	n := math.Min(len(queries), QueryParallelism)
	// Run n parallel goroutines fetching queries from the queue
	for i := 0; i < n; i++ {
		go func() {
			sp, ctx := ot.StartSpanFromContext(ctx, "DoParallelQueries-worker")
			defer sp.Finish()
			for {
				query, ok := <-queue
				if !ok {
					return
				}
				incomingErrors <- doSingleQuery(ctx, query, callback)
			}
		}()
	}
	// Send all the queries into the queue
	go func() {
		for _, query := range queries {
			queue <- query
		}
		close(queue)
	}()

	// Now receive all the results.
	var lastErr error
	for i := 0; i < len(queries); i++ {
		err := <-incomingErrors
		if err != nil {

			lastErr = err
		}
	}
	return lastErr
}

type filteringBatch struct {
	query chunk.IndexQuery
	chunk.ReadBatch
}

func (f filteringBatch) Iterator() chunk.ReadBatchIterator {
	return &filteringBatchIter{
		query:             f.query,
		ReadBatchIterator: f.ReadBatch.Iterator(),
	}
}

type filteringBatchIter struct {
	query chunk.IndexQuery
	chunk.ReadBatchIterator
}

func (f *filteringBatchIter) Next() bool {
	for f.ReadBatchIterator.Next() {
		rangeValue, value := f.ReadBatchIterator.RangeValue(), f.ReadBatchIterator.Value()

		if len(f.query.RangeValuePrefix) != 0 && !bytes.HasPrefix(rangeValue, f.query.RangeValuePrefix) {
			continue
		}
		if len(f.query.RangeValueStart) != 0 && bytes.Compare(f.query.RangeValueStart, rangeValue) > 0 {
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
// useful for the cache and Bigtable backend, which only ever fetches the whole
// row.
func QueryFilter(callback Callback) Callback {
	return func(query chunk.IndexQuery, batch chunk.ReadBatch) bool {
		return callback(query, &filteringBatch{query, batch})
	}
}

// EnsureDirectory makes sure directory is there, if not creates it if not
func EnsureDirectory(dir string) error {
	info, err := os.Stat(dir)
	if os.IsNotExist(err) {
		return os.MkdirAll(dir, 0777)
	} else if err == nil && !info.IsDir() {
		return fmt.Errorf("not a directory: %s", dir)
	}
	return err
}

// ReadCloserWithContextCancelFunc helps with cancelling the context when closing a ReadCloser.
// NOTE: The consumer of ReadCloserWithContextCancelFunc should always call the Close method when it is done reading which otherwise could cause a resource leak.
type ReadCloserWithContextCancelFunc struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func NewReadCloserWithContextCancelFunc(readCloser io.ReadCloser, cancel context.CancelFunc) io.ReadCloser {
	return ReadCloserWithContextCancelFunc{
		ReadCloser: readCloser,
		cancel:     cancel,
	}
}

func (r ReadCloserWithContextCancelFunc) Close() error {
	defer r.cancel()
	return r.ReadCloser.Close()
}
