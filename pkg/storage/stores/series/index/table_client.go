package index

import (
	"context"

	"github.com/grafana/loki/v3/pkg/storage/config"
)

// TableClient is a client for telling Dynamo what to do with tables.
type TableClient interface {
	ListTables(ctx context.Context) ([]string, error)
	CreateTable(ctx context.Context, desc config.TableDesc) error
	DeleteTable(ctx context.Context, name string) error
	DescribeTable(ctx context.Context, name string) (desc config.TableDesc, isActive bool, err error)
	UpdateTable(ctx context.Context, current, expected config.TableDesc) error
	Stop()
}
