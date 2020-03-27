package local

import (
	"context"
	"sync"

	"github.com/cortexproject/cortex/pkg/chunk"
	"github.com/cortexproject/cortex/pkg/chunk/local"
	chunk_util "github.com/cortexproject/cortex/pkg/chunk/util"
	"go.etcd.io/bbolt"
)

type BoltdbIndexClientWithArchiver struct {
	*local.BoltIndexClient

	shipper *Shipper

	done chan struct{}
	wait sync.WaitGroup
}

// NewBoltDBIndexClient creates a new IndexClient that used BoltDB.
func NewBoltDBIndexClient(cfg local.BoltDBConfig, archiveStoreClient chunk.ObjectClient, archiverCfg ShipperConfig) (chunk.IndexClient, error) {
	boltDBIndexClient, err := local.NewBoltDBIndexClient(cfg)
	if err != nil {
		return nil, err
	}

	shipper, err := NewShipper(archiverCfg, archiveStoreClient, boltDBIndexClient)
	if err != nil {
		return nil, err
	}

	indexClient := BoltdbIndexClientWithArchiver{
		BoltIndexClient: boltDBIndexClient,
		shipper:         shipper,
		done:            make(chan struct{}),
	}

	return &indexClient, nil
}

func (b *BoltdbIndexClientWithArchiver) Stop() {
	close(b.done)

	b.BoltIndexClient.Stop()
	b.shipper.Stop()

	b.wait.Wait()
}

func (b *BoltdbIndexClientWithArchiver) QueryPages(ctx context.Context, queries []chunk.IndexQuery, callback func(chunk.IndexQuery, chunk.ReadBatch) (shouldContinue bool)) error {
	return chunk_util.DoParallelQueries(ctx, b.query, queries, callback)
}

func (b *BoltdbIndexClientWithArchiver) query(ctx context.Context, query chunk.IndexQuery, callback func(chunk.ReadBatch) (shouldContinue bool)) error {
	db, err := b.GetDB(query.TableName, local.DBOperationRead)
	if err != nil && err != local.ErrUnexistentBoltDB {
		return err
	}

	if db != nil {
		if err := b.QueryDB(ctx, db, query, callback); err != nil {
			return err
		}
	}

	return b.shipper.forEach(ctx, query.TableName, func(db *bbolt.DB) error {
		return b.QueryDB(ctx, db, query, callback)
	})
}
