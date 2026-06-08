package boltdb

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"runtime/debug"

	"go.etcd.io/bbolt"
	"go.opentelemetry.io/otel"
)

const maxStackSize = 8 * 1024

var tracer = otel.Tracer("pkg/storage/common/boltdb")

type result struct {
	boltdb *bbolt.DB
	err    error
}

// SafeOpenBoltdbFile will recover from a panic opening a DB file, and return the panic message in the err return object.
func SafeOpenBoltdbFile(path string) (*bbolt.DB, error) {
	result := make(chan *result)
	// Open the file in a separate goroutine because we want to change
	// the behavior of a Fault for just this operation and not for the
	// calling goroutine
	go safeOpenBoltDbFile(path, result)
	res := <-result
	return res.boltdb, res.err
}

func safeOpenBoltDbFile(path string, ret chan *result) {
	// boltdb can throw faults which are not caught by recover unless we turn them into panics
	debug.SetPanicOnFault(true)
	res := &result{}

	defer func() {
		if r := recover(); r != nil {
			logPanic(r)
			res.err = fmt.Errorf("recovered from panic opening boltdb file: %v", r)
		}

		// Return the result object on the channel to unblock the calling thread
		ret <- res
	}()

	b, err := OpenBoltdbFile(path)
	res.boltdb = b
	res.err = err
}

func logPanic(p any) {
	stack := make([]byte, maxStackSize)
	stack = stack[:runtime.Stack(stack, true)]
	// keep a multiline stack
	fmt.Fprintf(os.Stderr, "panic: %v\n%s", p, stack)
}

// DoSingleQuery is the interface for indexes that don't support batching yet.
type DoSingleQuery func(context.Context, Query, QueryPagesCallback) error

// QueryParallelism is the maximum number of subqueries run in
// parallel per higher-level query
var QueryParallelism = 100

// DoParallelQueries translates between our interface for query batching,
// and indexes that don't yet support batching.
func DoParallelQueries(
	ctx context.Context, doSingleQuery DoSingleQuery, queries []Query,
	callback QueryPagesCallback,
) error {
	if len(queries) == 1 {
		return doSingleQuery(ctx, queries[0], callback)
	}

	queue := make(chan Query)
	incomingErrors := make(chan error)
	n := min(len(queries), QueryParallelism)
	// Run n parallel goroutines fetching queries from the queue
	for range n {
		go func() {
			ctx, sp := tracer.Start(ctx, "DoParallelQueries-worker")
			defer sp.End()
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
	for range queries {
		err := <-incomingErrors
		if err != nil {
			lastErr = err
		}
	}
	return lastErr
}
