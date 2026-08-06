package client

import (
	"context"
	"errors"

	"github.com/grafana/loki/v3/pkg/storage/chunk"
)

var (
	// ErrMethodNotImplemented when any of the storage clients do not implement a method
	ErrMethodNotImplemented = errors.New("method is not implemented")
	// ErrStorageObjectNotFound when object storage does not have requested object
	ErrStorageObjectNotFound = errors.New("object not found in storage")
	// ErrChunkDecodeFailed lets callers classify a decode failure by identity. The
	// text is the leading verb of the error getChunk builds, so it must not change.
	ErrChunkDecodeFailed = errors.New("failed to decode chunk")
)

// Client is for storing and retrieving chunks.
type Client interface {
	Stop()
	PutChunks(ctx context.Context, chunks []chunk.Chunk) error
	GetChunks(ctx context.Context, chunks []chunk.Chunk) ([]chunk.Chunk, error)
	DeleteChunk(ctx context.Context, userID, chunkID string) error
	IsChunkNotFoundErr(err error) bool
	IsRetryableErr(err error) bool
}
