package storage

import (
	"context"
	"flag"
	"fmt"
	"strings"
	"time"

	"github.com/cortexproject/cortex/pkg/chunk"
	"github.com/cortexproject/cortex/pkg/chunk/aws"
	"github.com/cortexproject/cortex/pkg/chunk/cache"
	"github.com/cortexproject/cortex/pkg/chunk/cassandra"
	"github.com/cortexproject/cortex/pkg/chunk/gcp"
	"github.com/cortexproject/cortex/pkg/chunk/local"
	"github.com/cortexproject/cortex/pkg/util"
	"github.com/cortexproject/cortex/pkg/util/validation"
	"github.com/go-kit/kit/log/level"
	"github.com/pkg/errors"
)

// Config chooses which storage client to use.
type Config struct {
	AWSStorageConfig       aws.StorageConfig  `yaml:"aws"`
	GCPStorageConfig       gcp.Config         `yaml:"bigtable"`
	GCSConfig              gcp.GCSConfig      `yaml:"gcs"`
	CassandraStorageConfig cassandra.Config   `yaml:"cassandra"`
	BoltDBConfig           local.BoltDBConfig `yaml:"boltdb"`
	FSConfig               local.FSConfig     `yaml:"filesystem"`

	IndexCacheValidity time.Duration

	IndexQueriesCacheConfig cache.Config `yaml:"index_queries_cache_config,omitempty"`
}

// RegisterFlags adds the flags required to configure this flag set.
func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	cfg.AWSStorageConfig.RegisterFlags(f)
	cfg.GCPStorageConfig.RegisterFlags(f)
	cfg.GCSConfig.RegisterFlags(f)
	cfg.CassandraStorageConfig.RegisterFlags(f)
	cfg.BoltDBConfig.RegisterFlags(f)
	cfg.FSConfig.RegisterFlags(f)

	cfg.IndexQueriesCacheConfig.RegisterFlagsWithPrefix("store.index-cache-read.", "Cache config for index entry reading. ", f)
	f.DurationVar(&cfg.IndexCacheValidity, "store.index-cache-validity", 5*time.Minute, "Cache validity for active index entries. Should be no higher than -ingester.max-chunk-idle.")
}

// NewStore makes the storage clients based on the configuration.
func NewStore(cfg Config, storeCfg chunk.StoreConfig, schemaCfg chunk.SchemaConfig, limits *validation.Overrides) (chunk.Store, error) {
	tieredCache, err := cache.New(cfg.IndexQueriesCacheConfig)
	if err != nil {
		return nil, err
	}

	// Cache is shared by multiple stores, which means they will try and Stop
	// it more than once.  Wrap in a StopOnce to prevent this.
	tieredCache = cache.StopOnce(tieredCache)

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
		index = newCachingIndexClient(index, tieredCache, cfg.IndexCacheValidity, limits)

		objectStoreType := s.ObjectType
		if objectStoreType == "" {
			objectStoreType = s.IndexType
		}
		chunks, err := NewObjectClient(objectStoreType, cfg, schemaCfg)
		if err != nil {
			return nil, errors.Wrap(err, "error creating object client")
		}

		err = stores.AddPeriod(storeCfg, s, index, chunks, limits)
		if err != nil {
			return nil, err
		}
	}

	return stores, nil
}

// NewIndexClient makes a new index client of the desired type.
func NewIndexClient(name string, cfg Config, schemaCfg chunk.SchemaConfig) (chunk.IndexClient, error) {
	switch name {
	case "inmemory":
		store := chunk.NewMockStorage()
		return store, nil
	case "aws", "aws-dynamo", "dynamo":
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

// NewObjectClient makes a new ObjectClient of the desired types.
func NewObjectClient(name string, cfg Config, schemaCfg chunk.SchemaConfig) (chunk.ObjectClient, error) {
	switch name {
	case "inmemory":
		store := chunk.NewMockStorage()
		return store, nil
	case "aws", "s3":
		return aws.NewS3ObjectClient(cfg.AWSStorageConfig, schemaCfg)
	case "aws-dynamo", "dynamo":
		if cfg.AWSStorageConfig.DynamoDB.URL == nil {
			return nil, fmt.Errorf("Must set -dynamodb.url in aws mode")
		}
		path := strings.TrimPrefix(cfg.AWSStorageConfig.DynamoDB.URL.Path, "/")
		if len(path) > 0 {
			level.Warn(util.Logger).Log("msg", "ignoring DynamoDB URL path", "path", path)
		}
		return aws.NewDynamoDBObjectClient(cfg.AWSStorageConfig.DynamoDBConfig, schemaCfg)
	case "gcp":
		return gcp.NewBigtableObjectClient(context.Background(), cfg.GCPStorageConfig, schemaCfg)
	case "gcp-columnkey", "bigtable", "bigtable-hashed":
		return gcp.NewBigtableObjectClient(context.Background(), cfg.GCPStorageConfig, schemaCfg)
	case "gcs":
		return gcp.NewGCSObjectClient(context.Background(), cfg.GCSConfig, schemaCfg)
	case "cassandra":
		return cassandra.NewStorageClient(cfg.CassandraStorageConfig, schemaCfg)
	case "filesystem":
		return local.NewFSObjectClient(cfg.FSConfig)
	default:
		return nil, fmt.Errorf("Unrecognized storage client %v, choose one of: aws, cassandra, inmemory, gcp, bigtable, bigtable-hashed", name)
	}
}

// NewTableClient makes a new table client based on the configuration.
func NewTableClient(name string, cfg Config) (chunk.TableClient, error) {
	switch name {
	case "inmemory":
		return chunk.NewMockStorage(), nil
	case "aws", "aws-dynamo":
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
