package grpc

import (
	"context"
	"io"

	"github.com/pkg/errors"
	"google.golang.org/grpc"

	"github.com/grafana/loki/v3/pkg/storage/chunk"
	"github.com/grafana/loki/v3/pkg/storage/config"
)

type StorageClient struct {
	schemaCfg  config.SchemaConfig
	client     GrpcStoreClient
	connection *grpc.ClientConn
}

// NewStorageClient returns a new StorageClient.
func NewStorageClient(cfg Config, schemaCfg config.SchemaConfig) (*StorageClient, error) {
	grpcClient, conn, err := connectToGrpcServer(cfg.Address)
	if err != nil {
		return nil, err
	}
	client := &StorageClient{
		schemaCfg:  schemaCfg,
		client:     grpcClient,
		connection: conn,
	}
	return client, nil
}

func (s *StorageClient) Stop() {
	s.connection.Close()
}

// PutChunks implements chunk.ObjectClient.
func (s *StorageClient) PutChunks(ctx context.Context, chunks []chunk.Chunk) error {
	req := &PutChunksRequest{}
	for i := range chunks {
		buf, err := chunks[i].Encoded()
		if err != nil {
			return errors.WithStack(err)
		}

		key := s.schemaCfg.ExternalKey(chunks[i].ChunkRef)
		tableName, err := s.schemaCfg.ChunkTableFor(chunks[i].From)
		if err != nil {
			return errors.WithStack(err)
		}
		writeChunk := &Chunk{
			Encoded:   buf,
			Key:       key,
			TableName: tableName,
		}

		req.Chunks = append(req.Chunks, writeChunk)
	}

	_, err := s.client.PutChunks(ctx, req)
	if err != nil {
		return errors.WithStack(err)
	}

	return nil
}

func (s *StorageClient) DeleteChunk(ctx context.Context, _, chunkID string) error {
	chunkInfo := &ChunkID{ChunkID: chunkID}
	_, err := s.client.DeleteChunks(ctx, chunkInfo)
	if err != nil {
		return errors.WithStack(err)
	}
	return nil
}

func (s *StorageClient) IsChunkNotFoundErr(_ error) bool {
	return false
}

func (s *StorageClient) IsRetryableErr(_ error) bool {
	return false
}

func (s *StorageClient) GetChunks(ctx context.Context, input []chunk.Chunk) ([]chunk.Chunk, error) {
	req := &GetChunksRequest{}
	req.Chunks = []*Chunk{}
	var err error
	for _, inputInfo := range input {
		chunkInfo := &Chunk{}
		// send the table name from upstream gRPC client as gRPC server is unaware of schema
		chunkInfo.TableName, err = s.schemaCfg.ChunkTableFor(inputInfo.From)
		if err != nil {
			return nil, errors.WithStack(err)
		}
		chunkInfo.Key = s.schemaCfg.ExternalKey(inputInfo.ChunkRef)
		req.Chunks = append(req.Chunks, chunkInfo)
	}
	streamer, err := s.client.GetChunks(ctx, req)
	if err != nil {
		return nil, errors.WithStack(err)
	}
	var result []chunk.Chunk
	decodeContext := chunk.NewDecodeContext()
	for {
		receivedChunks, err := streamer.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, errors.WithStack(err)
		}
		for _, chunkResponse := range receivedChunks.GetChunks() {
			var c chunk.Chunk
			if chunkResponse != nil {
				err = c.Decode(decodeContext, chunkResponse.Encoded)
				if err != nil {
					return result, err
				}
			}
			result = append(result, c)
		}
	}

	return result, err
}
