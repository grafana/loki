package util

import (
	"context"
	segment "github.com/blugelabs/bluge_segment_api"
	"github.com/grafana/loki/storage/stores/shipper/bluge_db"

	"github.com/cortexproject/cortex/pkg/util"
)

const maxQueriesPerGoroutine = 100

type TableQuerier interface {
	MultiQueries(ctx context.Context, queries []bluge_db.IndexQuery, callback segment.StoredFieldVisitor) error
}

// QueriesByTable groups and returns queries by tables.
func QueriesByTable(queries []bluge_db.IndexQuery) map[string][]bluge_db.IndexQuery {
	queriesByTable := make(map[string][]bluge_db.IndexQuery)
	for _, query := range queries {
		if _, ok := queriesByTable[query.TableName]; !ok {
			queriesByTable[query.TableName] = []bluge_db.IndexQuery{}
		}

		queriesByTable[query.TableName] = append(queriesByTable[query.TableName], query)
	}

	return queriesByTable
}

// 实现并发查询 最大queries数为100
func DoParallelQueries(ctx context.Context, tableQuerier TableQuerier, queries []bluge_db.IndexQuery, callback segment.StoredFieldVisitor) error {
	errs := make(chan error)

	for i := 0; i < len(queries); i += maxQueriesPerGoroutine {
		q := queries[i:util.Min(i+maxQueriesPerGoroutine, len(queries))]
		go func(queries []bluge_db.IndexQuery) {
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
