package storage

import (
	"context"
	"flag"
	"fmt"
	"strings"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/loki/pkg/storage/chunk"
	"github.com/grafana/loki/pkg/storage/chunk/aws"
	"github.com/grafana/loki/pkg/storage/chunk/azure"
	"github.com/grafana/loki/pkg/storage/chunk/cache"
	"github.com/grafana/loki/pkg/storage/chunk/cassandra"
	"github.com/grafana/loki/pkg/storage/chunk/gcp"
	"github.com/grafana/loki/pkg/storage/chunk/grpc"
	"github.com/grafana/loki/pkg/storage/chunk/hedging"
	"github.com/grafana/loki/pkg/storage/chunk/local"
	"github.com/grafana/loki/pkg/storage/chunk/objectclient"
	"github.com/grafana/loki/pkg/storage/chunk/openstack"
	"github.com/grafana/loki/pkg/storage/stores/shipper/downloads"
	util_log "github.com/grafana/loki/pkg/util/log"
)

// Supported storage engines
const (
	StorageEngineChunks = "chunks"
	StorageEngineBlocks = "blocks"
)

// Supported storage clients
const (
	StorageTypeAWS            = "aws"
	StorageTypeAWSDynamo      = "aws-dynamo"
	StorageTypeAzure          = "azure"
	StorageTypeBoltDB         = "boltdb"
	StorageTypeCassandra      = "cassandra"
	StorageTypeInMemory       = "inmemory"
	StorageTypeBigTable       = "bigtable"
	StorageTypeBigTableHashed = "bigtable-hashed"
	StorageTypeFileSystem     = "filesystem"
	StorageTypeGCP            = "gcp"
	StorageTypeGCPColumnKey   = "gcp-columnkey"
	StorageTypeGCS            = "gcs"
	StorageTypeGrpc           = "grpc-store"
	StorageTypeS3             = "s3"
	StorageTypeSwift          = "swift"
)

type indexStoreFactories struct {
	indexClientFactoryFunc IndexClientFactoryFunc
	tableClientFactoryFunc TableClientFactoryFunc
}

// IndexClientFactoryFunc defines signature of function which creates chunk.IndexClient for managing index in index store
type IndexClientFactoryFunc func(limits StoreLimits) (chunk.IndexClient, error)

// TableClientFactoryFunc defines signature of function which creates chunk.TableClient for managing tables in index store
type TableClientFactoryFunc func() (chunk.TableClient, error)

var customIndexStores = map[string]indexStoreFactories{}

// RegisterIndexStore is used for registering a custom index type.
// When an index type is registered here with same name as existing types, the registered one takes the precedence.
func RegisterIndexStore(name string, indexClientFactory IndexClientFactoryFunc, tableClientFactory TableClientFactoryFunc) {
	customIndexStores[name] = indexStoreFactories{indexClientFactory, tableClientFactory}
}

// StoreLimits helps get Limits specific to Queries for Stores
type StoreLimits interface {
	downloads.Limits
	CardinalityLimit(userID string) int
	MaxChunksPerQueryFromStore(userID string) int
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
	Swift                  openstack.SwiftConfig   `yaml:"swift"`

	IndexCacheValidity time.Duration `yaml:"index_cache_validity"`

	IndexQueriesCacheConfig  cache.Config `yaml:"index_queries_cache_config"`
	DisableBroadIndexQueries bool         `yaml:"disable_broad_index_queries"`
	MaxParallelGetChunk      int          `yaml:"max_parallel_get_chunk"`

	GrpcConfig grpc.Config `yaml:"grpc_store"`

	Hedging hedging.Config `yaml:"hedging"`
}

type ClientMetrics struct {
	AzureMetrics azure.BlobStorageMetrics
}

func NewClientMetrics() ClientMetrics {
	return ClientMetrics{
		AzureMetrics: azure.NewBlobStorageMetrics(),
	}
}

func (c *ClientMetrics) Unregister() {
	c.AzureMetrics.Unregister()
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
	cfg.Swift.RegisterFlags(f)
	cfg.GrpcConfig.RegisterFlags(f)
	cfg.Hedging.RegisterFlagsWithPrefix("store.", f)

	f.StringVar(&cfg.Engine, "store.engine", "chunks", "The storage engine to use: chunks or blocks.")
	cfg.IndexQueriesCacheConfig.RegisterFlagsWithPrefix("store.index-cache-read.", "Cache config for index entry reading.", f)
	f.DurationVar(&cfg.IndexCacheValidity, "store.index-cache-validity", 5*time.Minute, "Cache validity for active index entries. Should be no higher than -ingester.max-chunk-idle.")
	f.BoolVar(&cfg.DisableBroadIndexQueries, "store.disable-broad-index-queries", false, "Disable broad index queries which results in reduced cache usage and faster query performance at the expense of somewhat higher QPS on the index store.")
	f.IntVar(&cfg.MaxParallelGetChunk, "store.max-parallel-get-chunk", 150, "Maximum number of parallel chunk reads.")
}

// Validate config and returns error on failure
func (cfg *Config) Validate() error {
	if cfg.Engine != StorageEngineChunks && cfg.Engine != StorageEngineBlocks {
		return errors.New("unsupported storage engine")
	}
	if err := cfg.CassandraStorageConfig.Validate(); err != nil {
		return errors.Wrap(err, "invalid Cassandra Storage config")
	}
	if err := cfg.GCPStorageConfig.Validate(util_log.Logger); err != nil {
		return errors.Wrap(err, "invalid GCP Storage Storage config")
	}
	if err := cfg.Swift.Validate(); err != nil {
		return errors.Wrap(err, "invalid Swift Storage config")
	}
	if err := cfg.IndexQueriesCacheConfig.Validate(); err != nil {
		return errors.Wrap(err, "invalid Index Queries Cache config")
	}
	if err := cfg.AzureStorageConfig.Validate(); err != nil {
		return errors.Wrap(err, "invalid Azure Storage config")
	}
	if err := cfg.AWSStorageConfig.Validate(); err != nil {
		return errors.Wrap(err, "invalid AWS Storage config")
	}
	return nil
}

// NewStore makes the storage clients based on the configuration.
func NewStore(
	cfg Config,
	storeCfg chunk.StoreConfig,
	schemaCfg chunk.SchemaConfig,
	limits StoreLimits,
	clientMetrics ClientMetrics,
	reg prometheus.Registerer,
	cacheGenNumLoader chunk.CacheGenNumLoader,
	logger log.Logger,
) (chunk.Store, error) {
	chunkMetrics := newChunkClientMetrics(reg)

	indexReadCache, err := cache.New(cfg.IndexQueriesCacheConfig, reg, logger)
	if err != nil {
		return nil, err
	}

	writeDedupeCache, err := cache.New(storeCfg.WriteDedupeCacheConfig, reg, logger)
	if err != nil {
		return nil, err
	}

	chunkCacheCfg := storeCfg.ChunkCacheConfig
	chunkCacheCfg.Prefix = "chunks"
	chunksCache, err := cache.New(chunkCacheCfg, reg, logger)
	if err != nil {
		return nil, err
	}

	// Cache is shared by multiple stores, which means they will try and Stop
	// it more than once.  Wrap in a StopOnce to prevent this.
	indexReadCache = cache.StopOnce(indexReadCache)
	chunksCache = cache.StopOnce(chunksCache)
	writeDedupeCache = cache.StopOnce(writeDedupeCache)

	// Lets wrap all caches except chunksCache with CacheGenMiddleware to facilitate cache invalidation using cache generation numbers.
	// chunksCache is not wrapped because chunks content can't be anyways modified without changing its ID so there is no use of
	// invalidating chunks cache. Also chunks can be fetched only by their ID found in index and we are anyways removing the index and invalidating index cache here.
	indexReadCache = cache.NewCacheGenNumMiddleware(indexReadCache)
	writeDedupeCache = cache.NewCacheGenNumMiddleware(writeDedupeCache)

	err = schemaCfg.Load()
	if err != nil {
		return nil, errors.Wrap(err, "error loading schema config")
	}
	stores := chunk.NewCompositeStore(cacheGenNumLoader)

	for _, s := range schemaCfg.Configs {
		indexClientReg := prometheus.WrapRegistererWith(
			prometheus.Labels{"component": "index-store-" + s.From.String()}, reg)

		index, err := NewIndexClient(s.IndexType, cfg, schemaCfg, limits, indexClientReg)
		if err != nil {
			return nil, errors.Wrap(err, "error creating index client")
		}
		index = newCachingIndexClient(index, indexReadCache, cfg.IndexCacheValidity, limits, logger, cfg.DisableBroadIndexQueries)

		objectStoreType := s.ObjectType
		if objectStoreType == "" {
			objectStoreType = s.IndexType
		}

		chunkClientReg := prometheus.WrapRegistererWith(
			prometheus.Labels{"component": "chunk-store-" + s.From.String()}, reg)

		chunks, err := NewChunkClient(objectStoreType, cfg, schemaCfg, clientMetrics, chunkClientReg)
		if err != nil {
			return nil, errors.Wrap(err, "error creating object client")
		}

		chunks = newMetricsChunkClient(chunks, chunkMetrics)

		err = stores.AddPeriod(storeCfg, s, index, chunks, limits, chunksCache, writeDedupeCache)
		if err != nil {
			return nil, err
		}
	}

	return stores, nil
}

// NewIndexClient makes a new index client of the desired type.
func NewIndexClient(name string, cfg Config, schemaCfg chunk.SchemaConfig, limits StoreLimits, registerer prometheus.Registerer) (chunk.IndexClient, error) {
	if indexClientFactory, ok := customIndexStores[name]; ok {
		if indexClientFactory.indexClientFactoryFunc != nil {
			return indexClientFactory.indexClientFactoryFunc(limits)
		}
	}

	switch name {
	case StorageTypeInMemory:
		store := chunk.NewMockStorage()
		return store, nil
	case StorageTypeAWS, StorageTypeAWSDynamo:
		if cfg.AWSStorageConfig.DynamoDB.URL == nil {
			return nil, fmt.Errorf("Must set -dynamodb.url in aws mode")
		}
		path := strings.TrimPrefix(cfg.AWSStorageConfig.DynamoDB.URL.Path, "/")
		if len(path) > 0 {
			level.Warn(util_log.Logger).Log("msg", "ignoring DynamoDB URL path", "path", path)
		}
		return aws.NewDynamoDBIndexClient(cfg.AWSStorageConfig.DynamoDBConfig, schemaCfg, registerer)
	case StorageTypeGCP:
		return gcp.NewStorageClientV1(context.Background(), cfg.GCPStorageConfig, schemaCfg)
	case StorageTypeGCPColumnKey, StorageTypeBigTable:
		return gcp.NewStorageClientColumnKey(context.Background(), cfg.GCPStorageConfig, schemaCfg)
	case StorageTypeBigTableHashed:
		cfg.GCPStorageConfig.DistributeKeys = true
		return gcp.NewStorageClientColumnKey(context.Background(), cfg.GCPStorageConfig, schemaCfg)
	case StorageTypeCassandra:
		return cassandra.NewStorageClient(cfg.CassandraStorageConfig, schemaCfg, registerer)
	case StorageTypeBoltDB:
		return local.NewBoltDBIndexClient(cfg.BoltDBConfig)
	case StorageTypeGrpc:
		return grpc.NewStorageClient(cfg.GrpcConfig, schemaCfg)
	default:
		return nil, fmt.Errorf("Unrecognized storage client %v, choose one of: %v, %v, %v, %v, %v, %v", name, StorageTypeAWS, StorageTypeCassandra, StorageTypeInMemory, StorageTypeGCP, StorageTypeBigTable, StorageTypeBigTableHashed)
	}
}

// NewChunkClient makes a new chunk.Client of the desired types.
func NewChunkClient(name string, cfg Config, schemaCfg chunk.SchemaConfig, clientMetrics ClientMetrics, registerer prometheus.Registerer) (chunk.Client, error) {
	switch name {
	case StorageTypeInMemory:
		return chunk.NewMockStorage(), nil
	case StorageTypeAWS, StorageTypeS3:
		c, err := aws.NewS3ObjectClient(cfg.AWSStorageConfig.S3Config, cfg.Hedging)
		if err != nil {
			return nil, err
		}
		return objectclient.NewClientWithMaxParallel(c, nil, cfg.MaxParallelGetChunk, schemaCfg), nil
	case StorageTypeAWSDynamo:
		if cfg.AWSStorageConfig.DynamoDB.URL == nil {
			return nil, fmt.Errorf("Must set -dynamodb.url in aws mode")
		}
		path := strings.TrimPrefix(cfg.AWSStorageConfig.DynamoDB.URL.Path, "/")
		if len(path) > 0 {
			level.Warn(util_log.Logger).Log("msg", "ignoring DynamoDB URL path", "path", path)
		}
		return aws.NewDynamoDBChunkClient(cfg.AWSStorageConfig.DynamoDBConfig, schemaCfg, registerer)
	case StorageTypeAzure:
		c, err := azure.NewBlobStorage(&cfg.AzureStorageConfig, clientMetrics.AzureMetrics, cfg.Hedging)
		if err != nil {
			return nil, err
		}
		return objectclient.NewClientWithMaxParallel(c, nil, cfg.MaxParallelGetChunk, schemaCfg), nil
	case StorageTypeGCP:
		return gcp.NewBigtableObjectClient(context.Background(), cfg.GCPStorageConfig, schemaCfg)
	case StorageTypeGCPColumnKey, StorageTypeBigTable, StorageTypeBigTableHashed:
		return gcp.NewBigtableObjectClient(context.Background(), cfg.GCPStorageConfig, schemaCfg)
	case StorageTypeGCS:
		c, err := gcp.NewGCSObjectClient(context.Background(), cfg.GCSConfig, cfg.Hedging)
		if err != nil {
			return nil, err
		}
		return objectclient.NewClientWithMaxParallel(c, nil, cfg.MaxParallelGetChunk, schemaCfg), nil
	case StorageTypeSwift:
		c, err := openstack.NewSwiftObjectClient(cfg.Swift, cfg.Hedging)
		if err != nil {
			return nil, err
		}
		return objectclient.NewClientWithMaxParallel(c, nil, cfg.MaxParallelGetChunk, schemaCfg), nil
	case StorageTypeCassandra:
		return cassandra.NewObjectClient(cfg.CassandraStorageConfig, schemaCfg, registerer, cfg.MaxParallelGetChunk)
	case StorageTypeFileSystem:
		store, err := local.NewFSObjectClient(cfg.FSConfig)
		if err != nil {
			return nil, err
		}
		return objectclient.NewClientWithMaxParallel(store, objectclient.FSEncoder, cfg.MaxParallelGetChunk, schemaCfg), nil
	case StorageTypeGrpc:
		return grpc.NewStorageClient(cfg.GrpcConfig, schemaCfg)
	default:
		return nil, fmt.Errorf("Unrecognized storage client %v, choose one of: %v, %v, %v, %v, %v, %v, %v, %v", name, StorageTypeAWS, StorageTypeAzure, StorageTypeCassandra, StorageTypeInMemory, StorageTypeGCP, StorageTypeBigTable, StorageTypeBigTableHashed, StorageTypeGrpc)
	}
}

// NewTableClient makes a new table client based on the configuration.
func NewTableClient(name string, cfg Config, registerer prometheus.Registerer) (chunk.TableClient, error) {
	if indexClientFactory, ok := customIndexStores[name]; ok {
		if indexClientFactory.tableClientFactoryFunc != nil {
			return indexClientFactory.tableClientFactoryFunc()
		}
	}

	switch name {
	case StorageTypeInMemory:
		return chunk.NewMockStorage(), nil
	case StorageTypeAWS, StorageTypeAWSDynamo:
		if cfg.AWSStorageConfig.DynamoDB.URL == nil {
			return nil, fmt.Errorf("Must set -dynamodb.url in aws mode")
		}
		path := strings.TrimPrefix(cfg.AWSStorageConfig.DynamoDB.URL.Path, "/")
		if len(path) > 0 {
			level.Warn(util_log.Logger).Log("msg", "ignoring DynamoDB URL path", "path", path)
		}
		return aws.NewDynamoDBTableClient(cfg.AWSStorageConfig.DynamoDBConfig, registerer)
	case StorageTypeGCP, StorageTypeGCPColumnKey, StorageTypeBigTable, StorageTypeBigTableHashed:
		return gcp.NewTableClient(context.Background(), cfg.GCPStorageConfig)
	case StorageTypeCassandra:
		return cassandra.NewTableClient(context.Background(), cfg.CassandraStorageConfig, registerer)
	case StorageTypeBoltDB:
		return local.NewTableClient(cfg.BoltDBConfig.Directory)
	case StorageTypeGrpc:
		return grpc.NewTableClient(cfg.GrpcConfig)
	default:
		return nil, fmt.Errorf("Unrecognized storage client %v, choose one of: %v, %v, %v, %v, %v, %v, %v", name, StorageTypeAWS, StorageTypeCassandra, StorageTypeInMemory, StorageTypeGCP, StorageTypeBigTable, StorageTypeBigTableHashed, StorageTypeGrpc)
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
func NewObjectClient(name string, cfg Config, clientMetrics ClientMetrics) (chunk.ObjectClient, error) {
	switch name {
	case StorageTypeAWS, StorageTypeS3:
		return aws.NewS3ObjectClient(cfg.AWSStorageConfig.S3Config, cfg.Hedging)
	case StorageTypeGCS:
		return gcp.NewGCSObjectClient(context.Background(), cfg.GCSConfig, cfg.Hedging)
	case StorageTypeAzure:
		return azure.NewBlobStorage(&cfg.AzureStorageConfig, clientMetrics.AzureMetrics, cfg.Hedging)
	case StorageTypeSwift:
		return openstack.NewSwiftObjectClient(cfg.Swift, cfg.Hedging)
	case StorageTypeInMemory:
		return chunk.NewMockStorage(), nil
	case StorageTypeFileSystem:
		return local.NewFSObjectClient(cfg.FSConfig)
	default:
		return nil, fmt.Errorf("Unrecognized storage client %v, choose one of: %v, %v, %v, %v, %v", name, StorageTypeAWS, StorageTypeS3, StorageTypeGCS, StorageTypeAzure, StorageTypeFileSystem)
	}
}
