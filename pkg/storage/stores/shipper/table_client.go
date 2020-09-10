package shipper

import (
	"context"
	"fmt"
	"strings"

	"github.com/go-kit/kit/log/level"

	"github.com/cortexproject/cortex/pkg/chunk"
	cortex_util "github.com/cortexproject/cortex/pkg/util"

	"github.com/grafana/loki/pkg/storage/stores/util"
)

type boltDBShipperTableClient struct {
	objectClient chunk.ObjectClient
}

func NewBoltDBShipperTableClient(objectClient chunk.ObjectClient) chunk.TableClient {
	return &boltDBShipperTableClient{util.NewPrefixedObjectClient(objectClient, StorageKeyPrefix)}
}

func (b *boltDBShipperTableClient) ListTables(ctx context.Context) ([]string, error) {
	_, dirs, err := b.objectClient.List(ctx, "")
	if err != nil {
		return nil, err
	}

	tables := make([]string, len(dirs))
	for i, dir := range dirs {
		tables[i] = strings.TrimSuffix(string(dir), "/")
	}

	return tables, nil
}

func (b *boltDBShipperTableClient) CreateTable(ctx context.Context, desc chunk.TableDesc) error {
	return nil
}

func (b *boltDBShipperTableClient) Stop() {
	b.objectClient.Stop()
}

func (b *boltDBShipperTableClient) DeleteTable(ctx context.Context, name string) error {
	objects, dirs, err := b.objectClient.List(ctx, name)
	if err != nil {
		return err
	}

	if len(dirs) != 0 {
		level.Error(cortex_util.Logger).Log("msg", fmt.Sprintf("unexpected directories in %s folder, not touching them", name), "directories", fmt.Sprint(dirs))
	}

	for _, object := range objects {
		err := b.objectClient.DeleteObject(ctx, object.Key)
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
