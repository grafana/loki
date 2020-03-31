package storage

import (
	"context"
	"flag"
	"fmt"
	"strings"
	"time"

	"github.com/go-kit/kit/log/level"
	"github.com/pkg/errors"

	"github.com/cortexproject/cortex/pkg/chunk"
	"github.com/cortexproject/cortex/pkg/chunk/aws"
	"github.com/cortexproject/cortex/pkg/chunk/azure"
	"github.com/cortexproject/cortex/pkg/chunk/cache"
	"github.com/cortexproject/cortex/pkg/chunk/cassandra"
	"github.com/cortexproject/cortex/pkg/chunk/gcp"
	"github.com/cortexproject/cortex/pkg/chunk/local"
	"github.com/cortexproject/cortex/pkg/chunk/objectclient"
	"github.com/cortexproject/cortex/pkg/chunk/purger"
	"github.com/cortexproject/cortex/pkg/util"
)

// Supported storage engines
const (
	StorageEngineChunks = "chunks"
	StorageEngineTSDB   = "tsdb"
)

type IndexClientFactoryFunc func() (chunk.IndexClient, error)

var customIndexClients = map[string]IndexClientFactoryFunc{}

func RegisterIndexClient(name string, factory IndexClientFactoryFunc) {
	customIndexClients[name] = factory
}

// useful for cleaning up state after tests
func unregisterAllCustomIndexClients() {
	customIndexClients = map[string]IndexClientFactoryFunc{}
}

// StoreLimits helps get Limits specific to Queries for Stores
type StoreLimits interface {
	CardinalityLimit(userID string) int
	MaxChunksPerQuery(userID string) int
	MaxQueryLength(userID string) time.Duration
}

// Config chooses which storage client to use.
type Config struct {
	Engine                 string                  `yaml:"engine"`
	AWSStorageConfig       aws.StorageConfig       `yaml:"aws"`
	AzureStorageConfig     azure.BlobStorageConfig `yaml:"azure"`
	GCPStorageConfig       gcp.Config              `yaml:"bigtable"`
	GCSConfig              gcp.GCSConfig           `yaml:"gcs"`
	CassandraStorageConfig cassandra.Config        `yaml:"cassandra"`
	BoltDBConfig           local.BoltDBConfig      `yaml:"boltdb"`
	FSConfig               local.FSConfig          `yaml:"filesystem"`

	IndexCacheValidity time.Duration `yaml:"index_cache_validity"`

	IndexQueriesCacheConfig cache.Config `yaml:"index_queries_cache_config"`

	DeleteStoreConfig purger.DeleteStoreConfig `yaml:"delete_store"`
}

// RegisterFlags adds the flags required to configure this flag set.
func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	cfg.AWSStorageConfig.RegisterFlags(f)
	cfg.AzureStorageConfig.RegisterFlags(f)
	cfg.GCPStorageConfig.RegisterFlags(f)
	cfg.GCSConfig.RegisterFlags(f)
	cfg.CassandraStorageConfig.RegisterFlags(f)
	cfg.BoltDBConfig.RegisterFlags(f)
	cfg.FSConfig.RegisterFlags(f)
	cfg.DeleteStoreConfig.RegisterFlags(f)

	f.StringVar(&cfg.Engine, "store.engine", "chunks", "The storage engine to use: chunks or tsdb. Be aware tsdb is experimental and shouldn't be used in production.")
	cfg.IndexQueriesCacheConfig.RegisterFlagsWithPrefix("store.index-cache-read.", "Cache config for index entry reading. ", f)
	f.DurationVar(&cfg.IndexCacheValidity, "store.index-cache-validity", 5*time.Minute, "Cache validity for active index entries. Should be no higher than -ingester.max-chunk-idle.")
}

// Validate config and returns error on failure
func (cfg *Config) Validate() error {
	if cfg.Engine != StorageEngineChunks && cfg.Engine != StorageEngineTSDB {
		return errors.New("unsupported storage engine")
	}
	if err := cfg.CassandraStorageConfig.Validate(); err != nil {
		return errors.Wrap(err, "invalid Cassandra Storage config")
	}
	return nil
}

// NewStore makes the storage clients based on the configuration.
func NewStore(cfg Config, storeCfg chunk.StoreConfig, schemaCfg chunk.SchemaConfig, limits StoreLimits) (chunk.Store, error) {
	indexReadCache, err := cache.New(cfg.IndexQueriesCacheConfig)
	if err != nil {
		return nil, err
	}

	writeDedupeCache, err := cache.New(storeCfg.WriteDedupeCacheConfig)
	if err != nil {
		return nil, err
	}

	chunkCacheCfg := storeCfg.ChunkCacheConfig
	chunkCacheCfg.Prefix = "chunks"
	chunksCache, err := cache.New(chunkCacheCfg)
	if err != nil {
		return nil, err
	}

	// Cache is shared by multiple stores, which means they will try and Stop
	// it more than once.  Wrap in a StopOnce to prevent this.
	indexReadCache = cache.StopOnce(indexReadCache)
	chunksCache = cache.StopOnce(chunksCache)
	writeDedupeCache = cache.StopOnce(writeDedupeCache)

	err = schemaCfg.Load()
	if err != nil {
		return nil, errors.Wrap(err, "error loading schema config")
	}
	stores := chunk.NewCompositeStore()

	for _, s := range schemaCfg.Configs {
		index, err := NewIndexClient(s.IndexType, cfg, schemaCfg)
		if err != nil {
			return nil, errors.Wrap(err, "error creating index client")
		}
		index = newCachingIndexClient(index, indexReadCache, cfg.IndexCacheValidity, limits)

		objectStoreType := s.ObjectType
		if objectStoreType == "" {
			objectStoreType = s.IndexType
		}
		chunks, err := NewChunkClient(objectStoreType, cfg, schemaCfg)
		if err != nil {
			return nil, errors.Wrap(err, "error creating object client")
		}

		err = stores.AddPeriod(storeCfg, s, index, chunks, limits, chunksCache, writeDedupeCache)
		if err != nil {
			return nil, err
		}
	}

	return stores, nil
}

// NewIndexClient makes a new index client of the desired type.
func NewIndexClient(name string, cfg Config, schemaCfg chunk.SchemaConfig) (chunk.IndexClient, error) {
	if factory, ok := customIndexClients[name]; ok {
		return factory()
	}

	switch name {
	case "inmemory":
		store := chunk.NewMockStorage()
		return store, nil
	case "aws", "aws-dynamo":
		if cfg.AWSStorageConfig.DynamoDB.URL == nil {
			return nil, fmt.Errorf("Must set -dynamodb.url in aws mode")
		}
		path := strings.TrimPrefix(cfg.AWSStorageConfig.DynamoDB.URL.Path, "/")
		if len(path) > 0 {
			level.Warn(util.Logger).Log("msg", "ignoring DynamoDB URL path", "path", path)
		}
		return aws.NewDynamoDBIndexClient(cfg.AWSStorageConfig.DynamoDBConfig, schemaCfg)
	case "gcp":
		return gcp.NewStorageClientV1(context.Background(), cfg.GCPStorageConfig, schemaCfg)
	case "gcp-columnkey", "bigtable":
		return gcp.NewStorageClientColumnKey(context.Background(), cfg.GCPStorageConfig, schemaCfg)
	case "bigtable-hashed":
		cfg.GCPStorageConfig.DistributeKeys = true
		return gcp.NewStorageClientColumnKey(context.Background(), cfg.GCPStorageConfig, schemaCfg)
	case "cassandra":
		return cassandra.NewStorageClient(cfg.CassandraStorageConfig, schemaCfg)
	case "boltdb":
		return local.NewBoltDBIndexClient(cfg.BoltDBConfig)
	default:
		return nil, fmt.Errorf("Unrecognized storage client %v, choose one of: aws, cassandra, inmemory, gcp, bigtable, bigtable-hashed", name)
	}
}

// NewChunkClient makes a new chunk.Client of the desired types.
func NewChunkClient(name string, cfg Config, schemaCfg chunk.SchemaConfig) (chunk.Client, error) {
	switch name {
	case "inmemory":
		return chunk.NewMockStorage(), nil
	case "aws", "s3":
		return newChunkClientFromStore(aws.NewS3ObjectClient(cfg.AWSStorageConfig.S3Config, chunk.DirDelim))
	case "aws-dynamo":
		if cfg.AWSStorageConfig.DynamoDB.URL == nil {
			return nil, fmt.Errorf("Must set -dynamodb.url in aws mode")
		}
		path := strings.TrimPrefix(cfg.AWSStorageConfig.DynamoDB.URL.Path, "/")
		if len(path) > 0 {
			level.Warn(util.Logger).Log("msg", "ignoring DynamoDB URL path", "path", path)
		}
		return aws.NewDynamoDBChunkClient(cfg.AWSStorageConfig.DynamoDBConfig, schemaCfg)
	case "azure":
		return newChunkClientFromStore(azure.NewBlobStorage(&cfg.AzureStorageConfig, chunk.DirDelim))
	case "gcp":
		return gcp.NewBigtableObjectClient(context.Background(), cfg.GCPStorageConfig, schemaCfg)
	case "gcp-columnkey", "bigtable", "bigtable-hashed":
		return gcp.NewBigtableObjectClient(context.Background(), cfg.GCPStorageConfig, schemaCfg)
	case "gcs":
		return newChunkClientFromStore(gcp.NewGCSObjectClient(context.Background(), cfg.GCSConfig, chunk.DirDelim))
	case "cassandra":
		return cassandra.NewStorageClient(cfg.CassandraStorageConfig, schemaCfg)
	case "filesystem":
		store, err := local.NewFSObjectClient(cfg.FSConfig)
		if err != nil {
			return nil, err
		}
		return objectclient.NewClient(store, objectclient.Base64Encoder), nil
	default:
		return nil, fmt.Errorf("Unrecognized storage client %v, choose one of: aws, azure, cassandra, inmemory, gcp, bigtable, bigtable-hashed", name)
	}
}

func newChunkClientFromStore(store chunk.ObjectClient, err error) (chunk.Client, error) {
	if err != nil {
		return nil, err
	}
	return objectclient.NewClient(store, nil), nil
}

// NewTableClient makes a new table client based on the configuration.
func NewTableClient(name string, cfg Config) (chunk.TableClient, error) {
	switch name {
	case "inmemory":
		return chunk.NewMockStorage(), nil
	case "aws", "aws-dynamo":
		if cfg.AWSStorageConfig.DynamoDB.URL == nil {
			return nil, fmt.Errorf("Must set -dynamodb.url in aws mode")
		}
		path := strings.TrimPrefix(cfg.AWSStorageConfig.DynamoDB.URL.Path, "/")
		if len(path) > 0 {
			level.Warn(util.Logger).Log("msg", "ignoring DynamoDB URL path", "path", path)
		}
		return aws.NewDynamoDBTableClient(cfg.AWSStorageConfig.DynamoDBConfig)
	case "gcp", "gcp-columnkey", "bigtable", "bigtable-hashed":
		return gcp.NewTableClient(context.Background(), cfg.GCPStorageConfig)
	case "cassandra":
		return cassandra.NewTableClient(context.Background(), cfg.CassandraStorageConfig)
	case "boltdb":
		return local.NewTableClient(cfg.BoltDBConfig.Directory)
	default:
		return nil, fmt.Errorf("Unrecognized storage client %v, choose one of: aws, cassandra, inmemory, gcp, bigtable, bigtable-hashed", name)
	}
}

// NewBucketClient makes a new bucket client based on the configuration.
func NewBucketClient(storageConfig Config) (chunk.BucketClient, error) {
	if storageConfig.FSConfig.Directory != "" {
		return local.NewFSObjectClient(storageConfig.FSConfig)
	}

	return nil, nil
}

// NewObjectClient makes a new StorageClient of the desired types.
func NewObjectClient(name string, cfg Config) (chunk.ObjectClient, error) {
	switch name {
	case "inmemory":
		return chunk.NewMockStorage(), nil
	case "filesystem":
		return local.NewFSObjectClient(cfg.FSConfig)
	default:
		return nil, fmt.Errorf("Unrecognized storage client %v, choose one of: filesystem", name)
	}
}
