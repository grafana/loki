package chunk

import (
	"context"
	"errors"
	"io"
	"time"
)

var (
	// ErrStorageObjectNotFound when object storage does not have requested object
	ErrStorageObjectNotFound = errors.New("object not found in storage")
	// ErrMethodNotImplemented when any of the storage clients do not implement a method
	ErrMethodNotImplemented = errors.New("method is not implemented")
)

// IndexClient is a client for the storage of the index (e.g. DynamoDB or Bigtable).
type IndexClient interface {
	Stop()

	// For the write path.
	NewWriteBatch() WriteBatch
	BatchWrite(context.Context, WriteBatch) error

	// For the read path.
	QueryPages(ctx context.Context, queries []IndexQuery, callback func(IndexQuery, ReadBatch) (shouldContinue bool)) error
}

// Client is for storing and retrieving chunks.
type Client interface {
	Stop()

	PutChunks(ctx context.Context, chunks []Chunk) error
	GetChunks(ctx context.Context, chunks []Chunk) ([]Chunk, error)
	DeleteChunk(ctx context.Context, userID, chunkID string) error
}

// ObjectAndIndexClient allows optimisations where the same client handles both
type ObjectAndIndexClient interface {
	PutChunksAndIndex(ctx context.Context, chunks []Chunk, index WriteBatch) error
}

// WriteBatch represents a batch of writes.
type WriteBatch interface {
	Add(tableName, hashValue string, rangeValue []byte, value []byte)
	Delete(tableName, hashValue string, rangeValue []byte)
}

// ReadBatch represents the results of a QueryPages.
type ReadBatch interface {
	Iterator() ReadBatchIterator
}

// ReadBatchIterator is an iterator over a ReadBatch.
type ReadBatchIterator interface {
	Next() bool
	RangeValue() []byte
	Value() []byte
}

// ObjectClient is used to store arbitrary data in Object Store (S3/GCS/Azure/...)
type ObjectClient interface {
	PutObject(ctx context.Context, objectKey string, object io.ReadSeeker) error
	// NOTE: The consumer of GetObject should always call the Close method when it is done reading which otherwise could cause a resource leak.
	GetObject(ctx context.Context, objectKey string) (io.ReadCloser, error)

	// List objects with given prefix.
	//
	// If delimiter is empty, all objects are returned, even if they are in nested in "subdirectories".
	// If delimiter is not empty, it is used to compute common prefixes ("subdirectories"),
	// and objects containing delimiter in the name will not be returned in the result.
	//
	// For example, if the prefix is "notes/" and the delimiter is a slash (/) as in "notes/summer/july", the common prefix is "notes/summer/".
	// Common prefixes will always end with passed delimiter.
	//
	// Keys of returned storage objects have given prefix.
	List(ctx context.Context, prefix string, delimiter string) ([]StorageObject, []StorageCommonPrefix, error)
	DeleteObject(ctx context.Context, objectKey string) error
	Stop()
}

// StorageObject represents an object being stored in an Object Store
type StorageObject struct {
	Key        string
	ModifiedAt time.Time
}

// StorageCommonPrefix represents a common prefix aka a synthetic directory in Object Store.
// It is guaranteed to always end with delimiter passed to List method.
type StorageCommonPrefix string
