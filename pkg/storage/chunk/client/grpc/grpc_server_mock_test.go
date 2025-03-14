package grpc

import (
	"context"
	"log"
	"net"
	"testing"

	"github.com/golang/protobuf/ptypes/empty"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"

	"github.com/grafana/loki/v3/pkg/storage/config"
)

type server struct {
	Cfg Config `yaml:"cfg,omitempty"`
}

// indexClient RPCs
func (s server) WriteIndex(_ context.Context, writes *WriteIndexRequest) (*empty.Empty, error) {
	rangeValue := "JSI0YbyRLVmLKkLBiAKf5ctf8mWtn9U6CXCzuYmWkMk 5f3DoSEa2cDzymQ7u8VZ6c/ku1HlYIdMWqdg1QKCYh4  8"
	value := "localhost:9090"
	if writes.Writes[0].TableName == "index_2625" &&
		writes.Writes[0].HashValue == "fake:d18381:5f3DoSEa2cDzymQ7u8VZ6c/ku1HlYIdMWqdg1QKCYh4" && string(writes.Writes[0].RangeValue) == rangeValue &&
		string(writes.Writes[0].Value) == value {
		return &empty.Empty{}, nil
	}
	err := errors.New("batch write request from indexClient doesn't match with the gRPC client")
	return &empty.Empty{}, err
}

func (s server) QueryIndex(query *QueryIndexRequest, _ GrpcStore_QueryIndexServer) error {
	if query.TableName == "table" && query.HashValue == "foo" {
		return nil
	}
	err := errors.New("query pages from indexClient request doesn't match with the gRPC client")
	return err
}

func (s server) DeleteIndex(_ context.Context, deletes *DeleteIndexRequest) (*empty.Empty, error) {
	if deletes.Deletes[0].TableName == "index_2625" && deletes.Deletes[0].HashValue == "fake:d18381:5f3DoSEa2cDzymQ7u8VZ6c/ku1HlYIdMWqdg1QKCYh4" &&
		string(deletes.Deletes[0].RangeValue) == "JSI0YbyRLVmLKkLBiAKf5ctf8mWtn9U6CXCzuYmWkMk 5f3DoSEa2cDzymQ7u8VZ6c/ku1HlYIdMWqdg1QKCYh4  8" {
		return &empty.Empty{}, nil
	}
	err := errors.New("delete from indexClient request doesn't match with the gRPC client")
	return &empty.Empty{}, err
}

// storageClient RPCs
//
// Support new and old chunk key formats
func (s server) PutChunks(_ context.Context, request *PutChunksRequest) (*empty.Empty, error) {
	if request.Chunks[0].TableName == "" && (request.Chunks[0].Key == "fake/ddf337b84e835f32:171bc00155a:171bc00155a:e6a518a") {
		return &empty.Empty{}, nil
	}
	err := errors.New("putChunks from storageClient request doesn't match with test from gRPC client")
	return &empty.Empty{}, err
}

func (s server) GetChunks(request *GetChunksRequest, _ GrpcStore_GetChunksServer) error {
	if request.Chunks[0].TableName == "" && (request.Chunks[0].Key == "fake/ddf337b84e835f32:171bc00155a:171bc00155a:d9a103b5") &&
		request.Chunks[0].Encoded == nil {
		return nil
	}
	err := errors.New("getChunks from storageClient request doesn't match with test gRPC client")
	return err
}

func (s server) DeleteChunks(_ context.Context, id *ChunkID) (*empty.Empty, error) {
	if id.ChunkID == "" {
		return &empty.Empty{}, nil
	}
	err := errors.New("deleteChunks from storageClient request doesn't match with test gRPC client")
	return &empty.Empty{}, err
}

// tableClient RPCs
func (s server) ListTables(_ context.Context, _ *empty.Empty) (*ListTablesResponse, error) {
	return &ListTablesResponse{
		TableNames: []string{"chunk_2604, chunk_2613, index_2594, index_2603"},
	}, nil
}

func (s server) CreateTable(_ context.Context, createTableRequest *CreateTableRequest) (*empty.Empty, error) {
	if createTableRequest.Desc.Name == "chunk_2607" && !createTableRequest.Desc.UseOnDemandIOMode && createTableRequest.Desc.ProvisionedRead == 300 && createTableRequest.Desc.ProvisionedWrite == 1 && createTableRequest.Desc.Tags == nil {
		return &empty.Empty{}, nil
	}
	err := errors.New("create table from tableClient request doesn't match with test gRPC client")
	return &empty.Empty{}, err
}

// nolint
func (s server) DeleteTable(_ context.Context, name *DeleteTableRequest) (*empty.Empty, error) {
	if name.TableName == "chunk_2591" {
		return &empty.Empty{}, nil
	}
	err := errors.New("delete table from tableClient request doesn't match with test gRPC client")
	return &empty.Empty{}, err
}

func (s server) DescribeTable(_ context.Context, name *DescribeTableRequest) (*DescribeTableResponse, error) {
	if name.TableName == "chunk_2591" {
		return &DescribeTableResponse{
			Desc: &TableDesc{
				Name:              "chunk_2591",
				UseOnDemandIOMode: false,
				ProvisionedRead:   0,
				ProvisionedWrite:  0,
				Tags:              nil,
			},
			IsActive: true,
		}, nil
	}
	err := errors.New("describe table from tableClient request doesn't match with test gRPC client")
	return &DescribeTableResponse{}, err
}

func (s server) UpdateTable(_ context.Context, request *UpdateTableRequest) (*empty.Empty, error) {
	if request.Current.Name == "chunk_2591" && !request.Current.UseOnDemandIOMode && request.Current.ProvisionedWrite == 0 &&
		request.Current.ProvisionedRead == 0 && request.Current.Tags == nil && request.Expected.Name == "chunk_2591" &&
		!request.Expected.UseOnDemandIOMode && request.Expected.ProvisionedWrite == 1 &&
		request.Expected.ProvisionedRead == 300 && request.Expected.Tags == nil {
		return &empty.Empty{}, nil
	}
	err := errors.New("update table from tableClient request doesn't match with test gRPC client")
	return &empty.Empty{}, err
}

// NewTestStorageClient returns a new StorageClient.
func NewTestStorageClient(cfg Config, schemaCfg config.SchemaConfig) (*StorageClient, error) {
	grpcClient, _, err := connectToGrpcServer(cfg.Address)
	if err != nil {
		return nil, err
	}
	client := &StorageClient{
		schemaCfg: schemaCfg,
		client:    grpcClient,
	}
	return client, nil
}

//***********************  gRPC mock server *********************************//

// NewTestTableClient returns a new TableClient.
func NewTestTableClient(cfg Config) (*TableClient, error) {
	grpcClient, _, err := connectToGrpcServer(cfg.Address)
	if err != nil {
		return nil, err
	}
	client := &TableClient{
		client: grpcClient,
	}
	return client, nil
}

// newTestStorageServer returns a new StorageServer.
func newTestStorageServer(cfg Config) *server {
	client := &server{
		Cfg: cfg,
	}
	return client
}

func createTestGrpcServer(t *testing.T) (func(), string) {
	var cfg server
	lis, err := net.Listen("tcp", "localhost:0")
	require.NoError(t, err)
	s := grpc.NewServer()

	s1 := newTestStorageServer(cfg.Cfg)

	RegisterGrpcStoreServer(s, s1)
	go func() {
		if err := s.Serve(lis); err != nil {
			log.Fatalf("Failed to serve: %v", err)
		}
	}()
	cleanup := func() {
		s.GracefulStop()
	}

	return cleanup, lis.Addr().String()
}
