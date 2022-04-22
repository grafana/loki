package local

import (
	"context"
	"os"
	"path/filepath"

	"github.com/grafana/loki/pkg/storage/config"
	"github.com/grafana/loki/pkg/storage/stores/series/index"
)

type TableClient struct {
	directory string
}

// NewTableClient returns a new TableClient.
func NewTableClient(directory string) (index.TableClient, error) {
	return &TableClient{directory: directory}, nil
}

func (c *TableClient) ListTables(ctx context.Context) ([]string, error) {
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

func (c *TableClient) CreateTable(ctx context.Context, desc config.TableDesc) error {
	file, err := os.OpenFile(filepath.Join(c.directory, desc.Name), os.O_CREATE|os.O_RDONLY, 0o666)
	if err != nil {
		return err
	}

	return file.Close()
}

func (c *TableClient) DeleteTable(ctx context.Context, name string) error {
	return os.Remove(filepath.Join(c.directory, name))
}

func (c *TableClient) DescribeTable(ctx context.Context, name string) (desc config.TableDesc, isActive bool, err error) {
	return config.TableDesc{
		Name: name,
	}, true, nil
}

func (c *TableClient) UpdateTable(ctx context.Context, current, expected config.TableDesc) error {
	return nil
}

func (*TableClient) Stop() {}
