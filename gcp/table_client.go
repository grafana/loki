package gcp

import (
	"context"
	"strings"

	"cloud.google.com/go/bigtable"
	"google.golang.org/grpc/status"

	"github.com/cortexproject/cortex/pkg/chunk"
)

type tableClient struct {
	cfg    Config
	client *bigtable.AdminClient
}

// NewTableClient returns a new TableClient.
func NewTableClient(ctx context.Context, cfg Config) (chunk.TableClient, error) {
	opts := toBigtableOpts(cfg.GRPCClientConfig.DialOption(instrumentation()))
	client, err := bigtable.NewAdminClient(ctx, cfg.Project, cfg.Instance, opts...)
	if err != nil {
		return nil, err
	}
	return &tableClient{
		cfg:    cfg,
		client: client,
	}, nil
}

func (c *tableClient) ListTables(ctx context.Context) ([]string, error) {
	tables, err := c.client.Tables(ctx)
	if err != nil {
		return nil, err
	}

	// Check each table has the right column family.  If not, omit it.
	output := make([]string, 0, len(tables))
	for _, table := range tables {
		info, err := c.client.TableInfo(ctx, table)
		if err != nil {
			return nil, err
		}

		if hasColumnFamily(info.FamilyInfos) {
			output = append(output, table)
		}
	}

	return output, nil
}

func hasColumnFamily(infos []bigtable.FamilyInfo) bool {
	for _, family := range infos {
		if family.Name == columnFamily {
			return true
		}
	}
	return false
}

func (c *tableClient) CreateTable(ctx context.Context, desc chunk.TableDesc) error {
	if err := c.client.CreateTable(ctx, desc.Name); err != nil {
		if !alreadyExistsError(err) {
			return err
		}
	}
	return c.client.CreateColumnFamily(ctx, desc.Name, columnFamily)
}

func alreadyExistsError(err error) bool {
	// This is super fragile, but I can't find a better way of doing it.
	// Have filed bug upstream: https://github.com/GoogleCloudPlatform/google-cloud-go/issues/672
	serr, ok := status.FromError(err)
	return ok && strings.Contains(serr.Message(), "already exists")
}

func (c *tableClient) DeleteTable(ctx context.Context, name string) error {
	if err := c.client.DeleteTable(ctx, name); err != nil {
		return err
	}

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
