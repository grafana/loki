package shipper

import (
	"context"

	"github.com/grafana/loki/pkg/storage/chunk/client"
	"github.com/grafana/loki/pkg/storage/config"
	"github.com/grafana/loki/pkg/storage/stores/series/index"
	"github.com/grafana/loki/pkg/storage/stores/shipper/storage"
)

type boltDBShipperTableClient struct {
	indexStorageClient storage.Client
}

func NewBoltDBShipperTableClient(objectClient client.ObjectClient, storageKeyPrefix string) index.TableClient {
	return &boltDBShipperTableClient{storage.NewIndexStorageClient(objectClient, storageKeyPrefix)}
}

func (b *boltDBShipperTableClient) ListTables(ctx context.Context) ([]string, error) {
	b.indexStorageClient.RefreshIndexListCache(ctx)
	return b.indexStorageClient.ListTables(ctx)
}

func (b *boltDBShipperTableClient) CreateTable(ctx context.Context, desc config.TableDesc) error {
	return nil
}

func (b *boltDBShipperTableClient) Stop() {
	b.indexStorageClient.Stop()
}

func (b *boltDBShipperTableClient) DeleteTable(ctx context.Context, tableName string) error {
	files, _, err := b.indexStorageClient.ListFiles(ctx, tableName, true)
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

func (b *boltDBShipperTableClient) DescribeTable(ctx context.Context, name string) (desc config.TableDesc, isActive bool, err error) {
	return config.TableDesc{
		Name: name,
	}, true, nil
}

func (b *boltDBShipperTableClient) UpdateTable(ctx context.Context, current, expected config.TableDesc) error {
	return nil
}
