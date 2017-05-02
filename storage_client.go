package chunk

import (
	"flag"
	"fmt"
	"strings"

	"github.com/prometheus/common/log"
	"golang.org/x/net/context"
)

// StorageClient is a client for the persistent storage for Cortex. (e.g. DynamoDB + S3).
type StorageClient interface {
	// For the write path.
	NewWriteBatch() WriteBatch
	BatchWrite(context.Context, WriteBatch) error

	// For the read path.
	QueryPages(ctx context.Context, query IndexQuery, callback func(result ReadBatch, lastPage bool) (shouldContinue bool)) error

	// For storing and retrieving chunks.
	PutChunk(ctx context.Context, key string, data []byte) error
	GetChunk(ctx context.Context, key string) ([]byte, error)
}

// WriteBatch represents a batch of writes.
type WriteBatch interface {
	Add(tableName, hashValue string, rangeValue []byte, value []byte)
}

// ReadBatch represents the results of a QueryPages.
type ReadBatch interface {
	Len() int
	RangeValue(index int) []byte
	Value(index int) []byte
}

// StorageClientConfig chooses which storage client to use.
type StorageClientConfig struct {
	StorageClient string
	AWSStorageConfig
}

// RegisterFlags adds the flags required to configure this flag set.
func (cfg *StorageClientConfig) RegisterFlags(f *flag.FlagSet) {
	flag.StringVar(&cfg.StorageClient, "chunk.storage-client", "aws", "Which storage client to use (aws, inmemory).")
	cfg.AWSStorageConfig.RegisterFlags(f)
}

// NewStorageClient makes a storage client based on the configuration.
func NewStorageClient(cfg StorageClientConfig) (StorageClient, error) {
	switch cfg.StorageClient {
	case "inmemory":
		return NewMockStorage(), nil
	case "aws":
		path := strings.TrimPrefix(cfg.DynamoDB.URL.Path, "/")
		if len(path) > 0 {
			log.Warnf("Ignoring DynamoDB URL path: %v.", path)
		}
		return NewAWSStorageClient(cfg.AWSStorageConfig)
	default:
		return nil, fmt.Errorf("Unrecognized storage client %v, choose one of: aws, inmemory", cfg.StorageClient)
	}
}
