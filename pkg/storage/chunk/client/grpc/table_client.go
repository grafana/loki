package grpc

import (
	"context"

	"github.com/golang/protobuf/ptypes/empty"
	"github.com/pkg/errors"
	"google.golang.org/grpc"

	"github.com/grafana/loki/v3/pkg/storage/config"
)

type TableClient struct {
	client GrpcStoreClient
	conn   *grpc.ClientConn
}

// NewTableClient returns a new TableClient.
func NewTableClient(cfg Config) (*TableClient, error) {
	grpcClient, conn, err := connectToGrpcServer(cfg.Address)
	if err != nil {
		return nil, err
	}
	client := &TableClient{
		client: grpcClient,
		conn:   conn,
	}
	return client, nil
}

func (c *TableClient) ListTables(ctx context.Context) ([]string, error) {
	tables, err := c.client.ListTables(ctx, &empty.Empty{})
	if err != nil {
		return nil, errors.WithStack(err)
	}
	return tables.TableNames, nil
}

func (c *TableClient) DeleteTable(ctx context.Context, name string) error {
	tableName := &DeleteTableRequest{TableName: name}
	_, err := c.client.DeleteTable(ctx, tableName)
	if err != nil {
		return errors.WithStack(err)
	}
	return nil
}

func (c *TableClient) DescribeTable(ctx context.Context, name string) (desc config.TableDesc, isActive bool, err error) {
	tableName := &DescribeTableRequest{TableName: name}
	tableDesc, err := c.client.DescribeTable(ctx, tableName)
	if err != nil {
		return desc, false, errors.WithStack(err)
	}
	desc.Name = tableDesc.Desc.Name
	desc.ProvisionedRead = tableDesc.Desc.ProvisionedRead
	desc.ProvisionedWrite = tableDesc.Desc.ProvisionedWrite
	desc.UseOnDemandIOMode = tableDesc.Desc.UseOnDemandIOMode
	desc.Tags = tableDesc.Desc.Tags
	return desc, tableDesc.IsActive, nil
}

func (c *TableClient) UpdateTable(ctx context.Context, current, expected config.TableDesc) error {
	currentTable := &TableDesc{}
	expectedTable := &TableDesc{}

	currentTable.Name = current.Name
	currentTable.UseOnDemandIOMode = current.UseOnDemandIOMode
	currentTable.ProvisionedWrite = current.ProvisionedWrite
	currentTable.ProvisionedRead = current.ProvisionedRead
	currentTable.Tags = current.Tags

	expectedTable.Name = expected.Name
	expectedTable.UseOnDemandIOMode = expected.UseOnDemandIOMode
	expectedTable.ProvisionedWrite = expected.ProvisionedWrite
	expectedTable.ProvisionedRead = expected.ProvisionedRead
	expectedTable.Tags = expected.Tags

	updateTableRequest := &UpdateTableRequest{
		Current:  currentTable,
		Expected: expectedTable,
	}
	_, err := c.client.UpdateTable(ctx, updateTableRequest)
	if err != nil {
		return errors.WithStack(err)
	}
	return nil
}

func (c *TableClient) CreateTable(ctx context.Context, desc config.TableDesc) error {
	req := &CreateTableRequest{}
	req.Desc = &TableDesc{}
	req.Desc.Name = desc.Name
	req.Desc.ProvisionedRead = desc.ProvisionedRead
	req.Desc.ProvisionedWrite = desc.ProvisionedWrite
	req.Desc.Tags = desc.Tags
	req.Desc.UseOnDemandIOMode = desc.UseOnDemandIOMode

	_, err := c.client.CreateTable(ctx, req)
	if err != nil {
		return errors.WithStack(err)
	}
	return nil
}

func (c *TableClient) Stop() {
	c.conn.Close()
}
