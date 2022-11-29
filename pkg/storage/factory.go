package storage

import (
	"context"
	"flag"
	"fmt"
	"strings"
	"time"

	"github.com/go-kit/log/level"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/loki/pkg/storage/chunk/cache"
	"github.com/grafana/loki/pkg/storage/chunk/client"
	"github.com/grafana/loki/pkg/storage/chunk/client/aws"
	"github.com/grafana/loki/pkg/storage/chunk/client/azure"
	"github.com/grafana/loki/pkg/storage/chunk/client/baidubce"
	"github.com/grafana/loki/pkg/storage/chunk/client/cassandra"
	"github.com/grafana/loki/pkg/storage/chunk/client/gcp"
	"github.com/grafana/loki/pkg/storage/chunk/client/grpc"
	"github.com/grafana/loki/pkg/storage/chunk/client/hedging"
	"github.com/grafana/loki/pkg/storage/chunk/client/local"
	"github.com/grafana/loki/pkg/storage/chunk/client/openstack"
	"github.com/grafana/loki/pkg/storage/chunk/client/testutils"
	"github.com/grafana/loki/pkg/storage/config"
	"github.com/grafana/loki/pkg/storage/stores/indexshipper"
	"github.com/grafana/loki/pkg/storage/stores/indexshipper/downloads"
	"github.com/grafana/loki/pkg/storage/stores/indexshipper/gatewayclient"
	"github.com/grafana/loki/pkg/storage/stores/series/index"
	"github.com/grafana/loki/pkg/storage/stores/shipper"
	util_log "github.com/grafana/loki/pkg/util/log"
)

// BoltDB Shipper is supposed to be run as a singleton.
// This could also be done in NewBoltDBIndexClientWithShipper factory method but we are doing it here because that method is used
// in tests for creating multiple instances of it at a time.
var boltDBIndexClientWithShipper index.Client

// ResetBoltDBIndexClientWithShipper allows to reset the singleton.
// MUST ONLY BE USED IN TESTS
func ResetBoltDBIndexClientWithShipper() {
	if boltDBIndexClientWithShipper == nil {
		return
	}
	boltDBIndexClientWithShipper.Stop()
	boltDBIndexClientWithShipper = nil
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
	AWSStorageConfig       aws.StorageConfig         `yaml:"aws"`
	AzureStorageConfig     azure.BlobStorageConfig   `yaml:"azure"`
	BOSStorageConfig       baidubce.BOSStorageConfig `yaml:"bos"`
	GCPStorageConfig       gcp.Config                `yaml:"bigtable"`
	GCSConfig              gcp.GCSConfig             `yaml:"gcs"`
	CassandraStorageConfig cassandra.Config          `yaml:"cassandra"`
	BoltDBConfig           local.BoltDBConfig        `yaml:"boltdb"`
	FSConfig               local.FSConfig            `yaml:"filesystem"`
	Swift                  openstack.SwiftConfig     `yaml:"swift"`
	GrpcConfig             grpc.Config               `yaml:"grpc_store"`
	Hedging                hedging.Config            `yaml:"hedging"`

	IndexCacheValidity time.Duration `yaml:"index_cache_validity"`

	IndexQueriesCacheConfig  cache.Config `yaml:"index_queries_cache_config"`
	DisableBroadIndexQueries bool         `yaml:"disable_broad_index_queries"`
	MaxParallelGetChunk      int          `yaml:"max_parallel_get_chunk"`

	MaxChunkBatchSize   int                 `yaml:"max_chunk_batch_size"`
	BoltDBShipperConfig shipper.Config      `yaml:"boltdb_shipper"`
	TSDBShipperConfig   indexshipper.Config `yaml:"tsdb_shipper"`

	// Config for using AsyncStore when using async index stores like `boltdb-shipper`.
	// It is required for getting chunk ids of recently flushed chunks from the ingesters.
	EnableAsyncStore bool          `yaml:"-"`
	AsyncStoreConfig AsyncStoreCfg `yaml:"-"`
}

// RegisterFlags adds the flags required to configure this flag set.
func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	cfg.AWSStorageConfig.RegisterFlags(f)
	cfg.AzureStorageConfig.RegisterFlags(f)
	cfg.BOSStorageConfig.RegisterFlags(f)
	cfg.GCPStorageConfig.RegisterFlags(f)
	cfg.GCSConfig.RegisterFlags(f)
	cfg.CassandraStorageConfig.RegisterFlags(f)
	cfg.BoltDBConfig.RegisterFlags(f)
	cfg.FSConfig.RegisterFlags(f)
	cfg.Swift.RegisterFlags(f)
	cfg.GrpcConfig.RegisterFlags(f)
	cfg.Hedging.RegisterFlagsWithPrefix("store.", f)

	cfg.IndexQueriesCacheConfig.RegisterFlagsWithPrefix("store.index-cache-read.", "Cache config for index entry reading.", f)
	f.DurationVar(&cfg.IndexCacheValidity, "store.index-cache-validity", 5*time.Minute, "Cache validity for active index entries. Should be no higher than -ingester.max-chunk-idle.")
	f.BoolVar(&cfg.DisableBroadIndexQueries, "store.disable-broad-index-queries", false, "Disable broad index queries which results in reduced cache usage and faster query performance at the expense of somewhat higher QPS on the index store.")
	f.IntVar(&cfg.MaxParallelGetChunk, "store.max-parallel-get-chunk", 150, "Maximum number of parallel chunk reads.")
	cfg.BoltDBShipperConfig.RegisterFlags(f)
	f.IntVar(&cfg.MaxChunkBatchSize, "store.max-chunk-batch-size", 50, "The maximum number of chunks to fetch per batch.")
	cfg.TSDBShipperConfig.RegisterFlagsWithPrefix("tsdb.", f)
}

// Validate config and returns error on failure
func (cfg *Config) Validate() error {
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
	if err := cfg.BoltDBShipperConfig.Validate(); err != nil {
		return errors.Wrap(err, "invalid boltdb-shipper config")
	}
	if err := cfg.TSDBShipperConfig.Validate(); err != nil {
		return errors.Wrap(err, "invalid tsdb config")
	}
	return nil
}

// NewIndexClient makes a new index client of the desired type.
func NewIndexClient(name string, cfg Config, schemaCfg config.SchemaConfig, limits StoreLimits, cm ClientMetrics, ownsTenantFn downloads.IndexGatewayOwnsTenant, registerer prometheus.Registerer) (index.Client, error) {
	switch name {
	case config.StorageTypeInMemory:
		store := testutils.NewMockStorage()
		return store, nil
	case config.StorageTypeAWS, config.StorageTypeAWSDynamo:
		if cfg.AWSStorageConfig.DynamoDB.URL == nil {
			return nil, fmt.Errorf("Must set -dynamodb.url in aws mode")
		}
		path := strings.TrimPrefix(cfg.AWSStorageConfig.DynamoDB.URL.Path, "/")
		if len(path) > 0 {
			level.Warn(util_log.Logger).Log("msg", "ignoring DynamoDB URL path", "path", path)
		}
		return aws.NewDynamoDBIndexClient(cfg.AWSStorageConfig.DynamoDBConfig, schemaCfg, registerer)
	case config.StorageTypeGCP:
		return gcp.NewStorageClientV1(context.Background(), cfg.GCPStorageConfig, schemaCfg)
	case config.StorageTypeGCPColumnKey, config.StorageTypeBigTable:
		return gcp.NewStorageClientColumnKey(context.Background(), cfg.GCPStorageConfig, schemaCfg)
	case config.StorageTypeBigTableHashed:
		cfg.GCPStorageConfig.DistributeKeys = true
		return gcp.NewStorageClientColumnKey(context.Background(), cfg.GCPStorageConfig, schemaCfg)
	case config.StorageTypeCassandra:
		return cassandra.NewStorageClient(cfg.CassandraStorageConfig, schemaCfg, registerer)
	case config.StorageTypeBoltDB:
		return local.NewBoltDBIndexClient(cfg.BoltDBConfig)
	case config.StorageTypeGrpc:
		return grpc.NewStorageClient(cfg.GrpcConfig, schemaCfg)
	case config.BoltDBShipperType:
		if boltDBIndexClientWithShipper != nil {
			return boltDBIndexClientWithShipper, nil
		}

		if shouldUseIndexGatewayClient(cfg.BoltDBShipperConfig.Config) {
			gateway, err := gatewayclient.NewGatewayClient(cfg.BoltDBShipperConfig.IndexGatewayClientConfig, registerer, util_log.Logger)
			if err != nil {
				return nil, err
			}

			boltDBIndexClientWithShipper = gateway
			return gateway, nil
		}

		objectClient, err := NewObjectClient(cfg.BoltDBShipperConfig.SharedStoreType, cfg, cm)
		if err != nil {
			return nil, err
		}

		tableRanges := getIndexStoreTableRanges(config.BoltDBShipperType, schemaCfg.Configs)

		boltDBIndexClientWithShipper, err = shipper.NewShipper(cfg.BoltDBShipperConfig, objectClient, limits,
			ownsTenantFn, tableRanges, registerer)

		return boltDBIndexClientWithShipper, err
	default:
		return nil, fmt.Errorf("Unrecognized storage client %v, choose one of: %v, %v, %v, %v, %v, %v", name, config.StorageTypeAWS, config.StorageTypeCassandra, config.StorageTypeInMemory, config.StorageTypeGCP, config.StorageTypeBigTable, config.StorageTypeBigTableHashed)
	}
}

// NewChunkClient makes a new chunk.Client of the desired types.
func NewChunkClient(name string, cfg Config, schemaCfg config.SchemaConfig, clientMetrics ClientMetrics, registerer prometheus.Registerer) (client.Client, error) {
	switch name {
	case config.StorageTypeInMemory:
		return testutils.NewMockStorage(), nil
	case config.StorageTypeAWS, config.StorageTypeS3:
		c, err := aws.NewS3ObjectClient(cfg.AWSStorageConfig.S3Config, cfg.Hedging)
		if err != nil {
			return nil, err
		}
		return client.NewClientWithMaxParallel(c, nil, cfg.MaxParallelGetChunk, schemaCfg), nil
	case config.StorageTypeAWSDynamo:
		if cfg.AWSStorageConfig.DynamoDB.URL == nil {
			return nil, fmt.Errorf("Must set -dynamodb.url in aws mode")
		}
		path := strings.TrimPrefix(cfg.AWSStorageConfig.DynamoDB.URL.Path, "/")
		if len(path) > 0 {
			level.Warn(util_log.Logger).Log("msg", "ignoring DynamoDB URL path", "path", path)
		}
		return aws.NewDynamoDBChunkClient(cfg.AWSStorageConfig.DynamoDBConfig, schemaCfg, registerer)
	case config.StorageTypeAzure:
		c, err := azure.NewBlobStorage(&cfg.AzureStorageConfig, clientMetrics.AzureMetrics, cfg.Hedging)
		if err != nil {
			return nil, err
		}
		return client.NewClientWithMaxParallel(c, nil, cfg.MaxParallelGetChunk, schemaCfg), nil
	case config.StorageTypeBOS:
		c, err := baidubce.NewBOSObjectStorage(&cfg.BOSStorageConfig)
		if err != nil {
			return nil, err
		}
		return client.NewClientWithMaxParallel(c, nil, cfg.MaxChunkBatchSize, schemaCfg), nil
	case config.StorageTypeGCP:
		return gcp.NewBigtableObjectClient(context.Background(), cfg.GCPStorageConfig, schemaCfg)
	case config.StorageTypeGCPColumnKey, config.StorageTypeBigTable, config.StorageTypeBigTableHashed:
		return gcp.NewBigtableObjectClient(context.Background(), cfg.GCPStorageConfig, schemaCfg)
	case config.StorageTypeGCS:
		c, err := gcp.NewGCSObjectClient(context.Background(), cfg.GCSConfig, cfg.Hedging)
		if err != nil {
			return nil, err
		}
		return client.NewClientWithMaxParallel(c, nil, cfg.MaxParallelGetChunk, schemaCfg), nil
	case config.StorageTypeSwift:
		c, err := openstack.NewSwiftObjectClient(cfg.Swift, cfg.Hedging)
		if err != nil {
			return nil, err
		}
		return client.NewClientWithMaxParallel(c, nil, cfg.MaxParallelGetChunk, schemaCfg), nil
	case config.StorageTypeCassandra:
		return cassandra.NewObjectClient(cfg.CassandraStorageConfig, schemaCfg, registerer, cfg.MaxParallelGetChunk)
	case config.StorageTypeFileSystem:
		store, err := local.NewFSObjectClient(cfg.FSConfig)
		if err != nil {
			return nil, err
		}
		return client.NewClientWithMaxParallel(store, client.FSEncoder, cfg.MaxParallelGetChunk, schemaCfg), nil
	case config.StorageTypeGrpc:
		return grpc.NewStorageClient(cfg.GrpcConfig, schemaCfg)
	default:
		return nil, fmt.Errorf("Unrecognized storage client %v, choose one of: %v, %v, %v, %v, %v, %v, %v, %v", name, config.StorageTypeAWS, config.StorageTypeAzure, config.StorageTypeCassandra, config.StorageTypeInMemory, config.StorageTypeGCP, config.StorageTypeBigTable, config.StorageTypeBigTableHashed, config.StorageTypeGrpc)
	}
}

// NewTableClient makes a new table client based on the configuration.
func NewTableClient(name string, cfg Config, cm ClientMetrics, registerer prometheus.Registerer) (index.TableClient, error) {
	switch name {
	case config.StorageTypeInMemory:
		return testutils.NewMockStorage(), nil
	case config.StorageTypeAWS, config.StorageTypeAWSDynamo:
		if cfg.AWSStorageConfig.DynamoDB.URL == nil {
			return nil, fmt.Errorf("Must set -dynamodb.url in aws mode")
		}
		path := strings.TrimPrefix(cfg.AWSStorageConfig.DynamoDB.URL.Path, "/")
		if len(path) > 0 {
			level.Warn(util_log.Logger).Log("msg", "ignoring DynamoDB URL path", "path", path)
		}
		return aws.NewDynamoDBTableClient(cfg.AWSStorageConfig.DynamoDBConfig, registerer)
	case config.StorageTypeGCP, config.StorageTypeGCPColumnKey, config.StorageTypeBigTable, config.StorageTypeBigTableHashed:
		return gcp.NewTableClient(context.Background(), cfg.GCPStorageConfig)
	case config.StorageTypeCassandra:
		return cassandra.NewTableClient(context.Background(), cfg.CassandraStorageConfig, registerer)
	case config.StorageTypeBoltDB:
		return local.NewTableClient(cfg.BoltDBConfig.Directory)
	case config.StorageTypeGrpc:
		return grpc.NewTableClient(cfg.GrpcConfig)
	case config.BoltDBShipperType, config.TSDBType:
		objectClient, err := NewObjectClient(cfg.BoltDBShipperConfig.SharedStoreType, cfg, cm)
		if err != nil {
			return nil, err
		}
		sharedStoreKeyPrefix := cfg.BoltDBShipperConfig.SharedStoreKeyPrefix
		if name == config.TSDBType {
			sharedStoreKeyPrefix = cfg.TSDBShipperConfig.SharedStoreKeyPrefix
		}
		return indexshipper.NewTableClient(objectClient, sharedStoreKeyPrefix), nil
	default:
		return nil, fmt.Errorf("Unrecognized storage client %v, choose one of: %v, %v, %v, %v, %v, %v, %v", name, config.StorageTypeAWS, config.StorageTypeCassandra, config.StorageTypeInMemory, config.StorageTypeGCP, config.StorageTypeBigTable, config.StorageTypeBigTableHashed, config.StorageTypeGrpc)
	}
}

// // NewTableClient creates a TableClient for managing tables for index/chunk store.
// // ToDo: Add support in Cortex for registering custom table client like index client.
// func NewTableClient(name string, cfg Config) (chunk.TableClient, error) {
// 	if name == shipper.BoltDBShipperType {
// 		name = "boltdb"
// 		cfg.FSConfig = chunk_local.FSConfig{Directory: cfg.BoltDBShipperConfig.ActiveIndexDirectory}
// 	}
// 	return storage.NewTableClient(name, cfg.Config, prometheus.DefaultRegisterer)
// }

// NewBucketClient makes a new bucket client based on the configuration.
func NewBucketClient(storageConfig Config) (index.BucketClient, error) {
	if storageConfig.FSConfig.Directory != "" {
		return local.NewFSObjectClient(storageConfig.FSConfig)
	}

	return nil, nil
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

// NewObjectClient makes a new StorageClient of the desired types.
func NewObjectClient(name string, cfg Config, clientMetrics ClientMetrics) (client.ObjectClient, error) {
	switch name {
	case config.StorageTypeAWS, config.StorageTypeS3:
		return aws.NewS3ObjectClient(cfg.AWSStorageConfig.S3Config, cfg.Hedging)
	case config.StorageTypeGCS:
		return gcp.NewGCSObjectClient(context.Background(), cfg.GCSConfig, cfg.Hedging)
	case config.StorageTypeAzure:
		return azure.NewBlobStorage(&cfg.AzureStorageConfig, clientMetrics.AzureMetrics, cfg.Hedging)
	case config.StorageTypeSwift:
		return openstack.NewSwiftObjectClient(cfg.Swift, cfg.Hedging)
	case config.StorageTypeInMemory:
		return testutils.NewMockStorage(), nil
	case config.StorageTypeFileSystem:
		return local.NewFSObjectClient(cfg.FSConfig)
	case config.StorageTypeBOS:
		return baidubce.NewBOSObjectStorage(&cfg.BOSStorageConfig)
	default:
		return nil, fmt.Errorf("Unrecognized storage client %v, choose one of: %v, %v, %v, %v, %v", name, config.StorageTypeAWS, config.StorageTypeS3, config.StorageTypeGCS, config.StorageTypeAzure, config.StorageTypeFileSystem)
	}
}
