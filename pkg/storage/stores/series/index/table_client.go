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

type byName []config.TableDesc

func (a byName) Len() int           { return len(a) }
func (a byName) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a byName) Less(i, j int) bool { return a[i].Name < a[j].Name }
