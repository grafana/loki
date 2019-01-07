package local

import (
	"context"

	"github.com/cortexproject/cortex/pkg/chunk"
)

type tableClient struct{}

// NewTableClient returns a new TableClient.
func NewTableClient() (chunk.TableClient, error) {
	return &tableClient{}, nil
}

func (c *tableClient) ListTables(ctx context.Context) ([]string, error) {
	return nil, nil
}

func (c *tableClient) CreateTable(ctx context.Context, desc chunk.TableDesc) error {
	return nil
}

func (c *tableClient) DescribeTable(ctx context.Context, name string) (desc chunk.TableDesc, isActive bool, err error) {
	return chunk.TableDesc{
		Name: name,
	}, true, nil
}

func (c *tableClient) UpdateTable(ctx context.Context, current, expected chunk.TableDesc) error {
	return nil
}
