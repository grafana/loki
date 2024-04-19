package boltdb

import (
	"context"
	"fmt"

	"github.com/grafana/dskit/tenant"
	"go.etcd.io/bbolt"

	"github.com/grafana/loki/v3/pkg/storage/stores/series/index"
	shipperindex "github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/index"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/util"
)

type Writer interface {
	ForEach(ctx context.Context, tableName string, callback func(b *bbolt.DB) error) error
}

type Querier interface {
	QueryPages(ctx context.Context, queries []index.Query, callback index.QueryPagesCallback) error
}

type querier struct {
	writer       Writer
	indexShipper Shipper
}

func NewQuerier(writer Writer, indexShipper Shipper) Querier {
	return &querier{
		writer:       writer,
		indexShipper: indexShipper,
	}
}

// QueryPages queries both the writer and indexShipper for the given queries.
func (q *querier) QueryPages(ctx context.Context, queries []index.Query, callback index.QueryPagesCallback) error {
	userID, err := tenant.TenantID(ctx)
	if err != nil {
		return err
	}

	userIDBytes := util.GetUnsafeBytes(userID)
	queriesByTable := util.QueriesByTable(queries)
	for table, queries := range queriesByTable {
		err := util.DoParallelQueries(ctx, func(ctx context.Context, queries []index.Query, callback index.QueryPagesCallback) error {
			// writer could be nil when running in ReadOnly mode
			if q.writer != nil {
				err := q.writer.ForEach(ctx, table, func(b *bbolt.DB) error {
					return QueryBoltDB(ctx, b, userIDBytes, queries, callback)
				})
				if err != nil {
					return err
				}
			}

			return q.indexShipper.ForEach(ctx, table, userID, func(_ bool, idx shipperindex.Index) error {
				boltdbIndexFile, ok := idx.(*IndexFile)
				if !ok {
					return fmt.Errorf("unexpected index type %T", idx)
				}

				return QueryBoltDB(ctx, boltdbIndexFile.GetBoltDB(), userIDBytes, queries, callback)
			})
		}, queries, callback)
		if err != nil {
			return err
		}
	}

	return nil
}
