package local

import (
	"context"
	"os"
	"path/filepath"

	"github.com/cortexproject/cortex/pkg/chunk"
)

type tableClient struct {
	directory string
}

// NewTableClient returns a new TableClient.
func NewTableClient(directory string) (chunk.TableClient, error) {
	return &tableClient{directory: directory}, nil
}

func (c *tableClient) ListTables(ctx context.Context) ([]string, error) {
	boltDbFiles := []string{}
	err := filepath.Walk(c.directory, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() {
			boltDbFiles = append(boltDbFiles, info.Name())
		}
		return nil
	})

	if err != nil {
		return nil, err
	}
	return boltDbFiles, nil
}

func (c *tableClient) CreateTable(ctx context.Context, desc chunk.TableDesc) error {
	return nil
}

func (c *tableClient) DeleteTable(ctx context.Context, name string) error {
	return os.Remove(filepath.Join(c.directory, name))
}

func (c *tableClient) DescribeTable(ctx context.Context, name string) (desc chunk.TableDesc, isActive bool, err error) {
	return chunk.TableDesc{
		Name: name,
	}, true, nil
}

func (c *tableClient) UpdateTable(ctx context.Context, current, expected chunk.TableDesc) error {
	return nil
}
