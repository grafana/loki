package shipper

import (
	"context"

	"github.com/grafana/loki/pkg/storage/stores/shipper/storage"

	"github.com/grafana/loki/pkg/storage/chunk"
)

type boltDBShipperTableClient struct {
	indexStorageClient storage.Client
}

func NewBoltDBShipperTableClient(objectClient chunk.ObjectClient, storageKeyPrefix string) chunk.TableClient {
	return &boltDBShipperTableClient{storage.NewIndexStorageClient(objectClient, storageKeyPrefix)}
}

func (b *boltDBShipperTableClient) ListTables(ctx context.Context) ([]string, error) {
	return b.indexStorageClient.ListTables(ctx)
}

func (b *boltDBShipperTableClient) CreateTable(ctx context.Context, desc chunk.TableDesc) error {
	return nil
}

func (b *boltDBShipperTableClient) Stop() {
	b.indexStorageClient.Stop()
}

func (b *boltDBShipperTableClient) DeleteTable(ctx context.Context, tableName string) error {
	files, err := b.indexStorageClient.ListFiles(ctx, tableName)
	if err != nil {
		return err
	}

	for _, file := range files {
		err := b.indexStorageClient.DeleteFile(ctx, tableName, file.Name)
		if err != nil {
			return err
		}
	}

	return nil
}

func (b *boltDBShipperTableClient) DescribeTable(ctx context.Context, name string) (desc chunk.TableDesc, isActive bool, err error) {
	return chunk.TableDesc{
		Name: name,
	}, true, nil
}

func (b *boltDBShipperTableClient) UpdateTable(ctx context.Context, current, expected chunk.TableDesc) error {
	return nil
}
