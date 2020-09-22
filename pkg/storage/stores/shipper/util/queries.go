package util

import (
	"context"

	"github.com/cortexproject/cortex/pkg/chunk"
	chunk_util "github.com/cortexproject/cortex/pkg/chunk/util"
	"github.com/cortexproject/cortex/pkg/util"
)

const maxQueriesPerGoroutine = 100

type TableQuerier interface {
	MultiQueries(ctx context.Context, queries []chunk.IndexQuery, callback chunk_util.Callback) error
}

// QueriesByTable groups and returns queries by tables.
func QueriesByTable(queries []chunk.IndexQuery) map[string][]chunk.IndexQuery {
	queriesByTable := make(map[string][]chunk.IndexQuery)
	for _, query := range queries {
		if _, ok := queriesByTable[query.TableName]; !ok {
			queriesByTable[query.TableName] = []chunk.IndexQuery{}
		}

		queriesByTable[query.TableName] = append(queriesByTable[query.TableName], query)
	}

	return queriesByTable
}

func DoParallelQueries(ctx context.Context, tableQuerier TableQuerier, queries []chunk.IndexQuery, callback chunk_util.Callback) error {
	errs := make(chan error)

	for i := 0; i < len(queries); i += maxQueriesPerGoroutine {
		q := queries[i:util.Min(i+maxQueriesPerGoroutine, len(queries))]
		go func(queries []chunk.IndexQuery) {
			errs <- tableQuerier.MultiQueries(ctx, queries, callback)
		}(q)
	}

	var lastErr error
	for i := 0; i < len(queries); i += maxQueriesPerGoroutine {
		err := <-errs
		if err != nil {
			lastErr = err
		}
	}

	return lastErr
}
