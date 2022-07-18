package indexshipper

import (
	"context"

	"github.com/grafana/loki/pkg/storage/chunk/client"
	"github.com/grafana/loki/pkg/storage/config"
	"github.com/grafana/loki/pkg/storage/stores/indexshipper/storage"
	"github.com/grafana/loki/pkg/storage/stores/series/index"
)

type tableClient struct {
	indexStorageClient storage.Client
}

// NewTableClient creates a client for managing tables in object storage based index store.
// It is typically used when running a table manager.
func NewTableClient(objectClient client.ObjectClient, storageKeyPrefix string) index.TableClient {
	return &tableClient{storage.NewIndexStorageClient(objectClient, storageKeyPrefix)}
}

func (b *tableClient) ListTables(ctx context.Context) ([]string, error) {
	b.indexStorageClient.RefreshIndexListCache(ctx)
	return b.indexStorageClient.ListTables(ctx)
}

func (b *tableClient) CreateTable(ctx context.Context, desc config.TableDesc) error {
	return nil
}

func (b *tableClient) Stop() {
	b.indexStorageClient.Stop()
}

func (b *tableClient) DeleteTable(ctx context.Context, tableName string) error {
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

func (b *tableClient) DescribeTable(ctx context.Context, name string) (desc config.TableDesc, isActive bool, err error) {
	return config.TableDesc{
		Name: name,
	}, true, nil
}

func (b *tableClient) UpdateTable(ctx context.Context, current, expected config.TableDesc) error {
	return nil
}
