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

	"github.com/grafana/loki/pkg/storage/chunk/cache"
	"github.com/grafana/loki/pkg/storage/chunk/client"
	"github.com/grafana/loki/pkg/storage/chunk/client/alibaba"
	"github.com/grafana/loki/pkg/storage/chunk/client/aws"
	"github.com/grafana/loki/pkg/storage/chunk/client/azure"
	"github.com/grafana/loki/pkg/storage/chunk/client/baidubce"
	"github.com/grafana/loki/pkg/storage/chunk/client/cassandra"
	"github.com/grafana/loki/pkg/storage/chunk/client/gcp"
	"github.com/grafana/loki/pkg/storage/chunk/client/grpc"
	"github.com/grafana/loki/pkg/storage/chunk/client/hedging"
	"github.com/grafana/loki/pkg/storage/chunk/client/ibmcloud"
	"github.com/grafana/loki/pkg/storage/chunk/client/local"
	"github.com/grafana/loki/pkg/storage/chunk/client/openstack"
	"github.com/grafana/loki/pkg/storage/chunk/client/testutils"
	"github.com/grafana/loki/pkg/storage/config"
	"github.com/grafana/loki/pkg/storage/stores"
	"github.com/grafana/loki/pkg/storage/stores/indexshipper"
	"github.com/grafana/loki/pkg/storage/stores/indexshipper/downloads"
	"github.com/grafana/loki/pkg/storage/stores/indexshipper/gatewayclient"
	"github.com/grafana/loki/pkg/storage/stores/series/index"
	"github.com/grafana/loki/pkg/storage/stores/shipper"
	"github.com/grafana/loki/pkg/storage/stores/shipper/indexgateway"
	util_log "github.com/grafana/loki/pkg/util/log"
)

var (
	indexGatewayClient index.Client
	// singleton for each period
	boltdbIndexClientsWithShipper = make(map[config.DayTime]index.Client)
)

// ResetBoltDBIndexClientsWithShipper allows to reset the singletons.
// MUST ONLY BE USED IN TESTS
func ResetBoltDBIndexClientsWithShipper() {
	for _, client := range boltdbIndexClientsWithShipper {
		client.Stop()
	}

	boltdbIndexClientsWithShipper = make(map[config.DayTime]index.Client)

	if indexGatewayClient != nil {
		indexGatewayClient.Stop()
		indexGatewayClient = nil
	}
}

// StoreLimits helps get Limits specific to Queries for Stores
type StoreLimits interface {
	downloads.Limits
	stores.StoreLimits
	indexgateway.Limits
	CardinalityLimit(string) int
}

// NamedStores helps configure additional object stores from a given storage provider
type NamedStores struct {
	AWS          map[string]aws.StorageConfig         `yaml:"aws"`
	Azure        map[string]azure.BlobStorageConfig   `yaml:"azure"`
	BOS          map[string]baidubce.BOSStorageConfig `yaml:"bos"`
	Filesystem   map[string]local.FSConfig            `yaml:"filesystem"`
	GCS          map[string]gcp.GCSConfig             `yaml:"gcs"`
	AlibabaCloud map[string]alibaba.OssConfig         `yaml:"alibabacloud"`
	Swift        map[string]openstack.SwiftConfig     `yaml:"swift"`
	COS          map[string]ibmcloud.COSConfig        `yaml:"cos"`

	// contains mapping from named store reference name to store type
	storeType map[string]string `yaml:"-"`
}

func (ns *NamedStores) populateStoreType() error {
	ns.storeType = make(map[string]string)

	checkForDuplicates := func(name string) error {
		switch name {
		case config.StorageTypeAWS, config.StorageTypeAWSDynamo, config.StorageTypeS3,
			config.StorageTypeGCP, config.StorageTypeGCPColumnKey, config.StorageTypeBigTable, config.StorageTypeBigTableHashed, config.StorageTypeGCS,
			config.StorageTypeAzure, config.StorageTypeBOS, config.StorageTypeSwift, config.StorageTypeCassandra,
			config.StorageTypeFileSystem, config.StorageTypeInMemory, config.StorageTypeGrpc:
			return fmt.Errorf("named store %q should not match with the name of a predefined storage type", name)
		}

		if st, ok := ns.storeType[name]; ok {
			return fmt.Errorf("named store %q is already defined under %s", name, st)
		}

		return nil
	}

	for name := range ns.AWS {
		if err := checkForDuplicates(name); err != nil {
			return err
		}
		ns.storeType[name] = config.StorageTypeAWS
	}

	for name := range ns.Azure {
		if err := checkForDuplicates(name); err != nil {
			return err
		}
		ns.storeType[name] = config.StorageTypeAzure
	}
	for name := range ns.AlibabaCloud {
		if err := checkForDuplicates(name); err != nil {
			return err
		}
		ns.storeType[name] = config.StorageTypeAlibabaCloud
	}
	for name := range ns.BOS {
		if err := checkForDuplicates(name); err != nil {
			return err
		}
		ns.storeType[name] = config.StorageTypeBOS
	}

	for name := range ns.Filesystem {
		if err := checkForDuplicates(name); err != nil {
			return err
		}
		ns.storeType[name] = config.StorageTypeFileSystem
	}

	for name := range ns.GCS {
		if err := checkForDuplicates(name); err != nil {
			return err
		}
		ns.storeType[name] = config.StorageTypeGCS
	}

	for name := range ns.Swift {
		if err := checkForDuplicates(name); err != nil {
			return err
		}
		ns.storeType[name] = config.StorageTypeSwift
	}

	return nil
}

func (ns *NamedStores) validate() error {
	for name, awsCfg := range ns.AWS {
		if err := awsCfg.Validate(); err != nil {
			return errors.Wrap(err, fmt.Sprintf("invalid AWS Storage config with name %s", name))
		}
	}

	for name, azureCfg := range ns.Azure {
		if err := azureCfg.Validate(); err != nil {
			return errors.Wrap(err, fmt.Sprintf("invalid Azure Storage config with name %s", name))
		}
	}

	for name, swiftCfg := range ns.Swift {
		if err := swiftCfg.Validate(); err != nil {
			return errors.Wrap(err, fmt.Sprintf("invalid Swift Storage config with name %s", name))
		}
	}

	return ns.populateStoreType()
}

// Config chooses which storage client to use.
type Config struct {
	AlibabaStorageConfig   alibaba.OssConfig         `yaml:"alibabacloud"`
	AWSStorageConfig       aws.StorageConfig         `yaml:"aws"`
	AzureStorageConfig     azure.BlobStorageConfig   `yaml:"azure"`
	BOSStorageConfig       baidubce.BOSStorageConfig `yaml:"bos"`
	GCPStorageConfig       gcp.Config                `yaml:"bigtable" doc:"description=Configures storing indexes in Bigtable. Required fields only required when bigtable is defined in config."`
	GCSConfig              gcp.GCSConfig             `yaml:"gcs" doc:"description=Configures storing chunks in GCS. Required fields only required when gcs is defined in config."`
	CassandraStorageConfig cassandra.Config          `yaml:"cassandra" doc:"description=Configures storing chunks and/or the index in Cassandra."`
	BoltDBConfig           local.BoltDBConfig        `yaml:"boltdb" doc:"description=Configures storing index in BoltDB. Required fields only required when boltdb is present in the configuration."`
	FSConfig               local.FSConfig            `yaml:"filesystem" doc:"description=Configures storing the chunks on the local file system. Required fields only required when filesystem is present in the configuration."`
	Swift                  openstack.SwiftConfig     `yaml:"swift"`
	GrpcConfig             grpc.Config               `yaml:"grpc_store"`
	Hedging                hedging.Config            `yaml:"hedging"`
	NamedStores            NamedStores               `yaml:"named_stores"`
	COSConfig              ibmcloud.COSConfig        `yaml:"cos"`
	IndexCacheValidity     time.Duration             `yaml:"index_cache_validity"`

	IndexQueriesCacheConfig  cache.Config `yaml:"index_queries_cache_config"`
	DisableBroadIndexQueries bool         `yaml:"disable_broad_index_queries"`
	MaxParallelGetChunk      int          `yaml:"max_parallel_get_chunk"`

	MaxChunkBatchSize   int                 `yaml:"max_chunk_batch_size"`
	BoltDBShipperConfig shipper.Config      `yaml:"boltdb_shipper" doc:"description=Configures storing index in an Object Store (GCS/S3/Azure/Swift/COS/Filesystem) in the form of boltdb files. Required fields only required when boltdb-shipper is defined in config."`
	TSDBShipperConfig   indexshipper.Config `yaml:"tsdb_shipper"`

	// Config for using AsyncStore when using async index stores like `boltdb-shipper`.
	// It is required for getting chunk ids of recently flushed chunks from the ingesters.
	AsyncStoreConfig AsyncStoreCfg `yaml:"-"`
}

// RegisterFlags adds the flags required to configure this flag set.
func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	cfg.AWSStorageConfig.RegisterFlags(f)
	cfg.AzureStorageConfig.RegisterFlags(f)
	cfg.BOSStorageConfig.RegisterFlags(f)
	cfg.COSConfig.RegisterFlags(f)
	cfg.GCPStorageConfig.RegisterFlags(f)
	cfg.GCSConfig.RegisterFlags(f)
	cfg.CassandraStorageConfig.RegisterFlags(f)
	cfg.BoltDBConfig.RegisterFlags(f)
	cfg.FSConfig.RegisterFlags(f)
	cfg.Swift.RegisterFlags(f)
	cfg.GrpcConfig.RegisterFlags(f)
	cfg.Hedging.RegisterFlagsWithPrefix("store.", f)

	cfg.IndexQueriesCacheConfig.RegisterFlagsWithPrefix("store.index-cache-read.", "", f)
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

	return cfg.NamedStores.validate()
}

// NewIndexClient makes a new index client of the desired type.
func NewIndexClient(periodCfg config.PeriodConfig, tableRange config.TableRange, cfg Config, schemaCfg config.SchemaConfig, limits StoreLimits, cm ClientMetrics, ownsTenantFn downloads.IndexGatewayOwnsTenant, registerer prometheus.Registerer, overrides indexgateway.Limits, logger log.Logger) (index.Client, error) {
	switch periodCfg.IndexType {
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
		if shouldUseIndexGatewayClient(cfg.BoltDBShipperConfig.Config) {
			if indexGatewayClient != nil {
				return indexGatewayClient, nil
			}

			gateway, err := gatewayclient.NewGatewayClient(cfg.BoltDBShipperConfig.IndexGatewayClientConfig, registerer, overrides, logger)
			if err != nil {
				return nil, err
			}

			indexGatewayClient = gateway
			return gateway, nil
		}

		if client, ok := boltdbIndexClientsWithShipper[periodCfg.From]; ok {
			return client, nil
		}

		objectType := periodCfg.ObjectType
		if cfg.BoltDBShipperConfig.SharedStoreType != "" {
			objectType = cfg.BoltDBShipperConfig.SharedStoreType
		}

		objectClient, err := NewObjectClient(objectType, cfg, cm)
		if err != nil {
			return nil, err
		}

		shipper, err := shipper.NewShipper(cfg.BoltDBShipperConfig, objectClient, limits,
			ownsTenantFn, tableRange, registerer, logger)
		if err != nil {
			return nil, err
		}

		boltdbIndexClientsWithShipper[periodCfg.From] = shipper
		return shipper, nil
	default:
		return nil, fmt.Errorf("Unrecognized storage client %v, choose one of: %v, %v, %v, %v, %v, %v", periodCfg.IndexType, config.StorageTypeAWS, config.StorageTypeCassandra, config.StorageTypeInMemory, config.StorageTypeGCP, config.StorageTypeBigTable, config.StorageTypeBigTableHashed)
	}
}

// NewChunkClient makes a new chunk.Client of the desired types.
func NewChunkClient(name string, cfg Config, schemaCfg config.SchemaConfig, clientMetrics ClientMetrics, registerer prometheus.Registerer) (client.Client, error) {
	var (
		storeType = name
	)

	// lookup storeType for named stores
	if nsType, ok := cfg.NamedStores.storeType[name]; ok {
		storeType = nsType
	}

	switch storeType {
	case config.StorageTypeInMemory:
		return testutils.NewMockStorage(), nil
	case config.StorageTypeAWS, config.StorageTypeS3:
		c, err := NewObjectClient(name, cfg, clientMetrics)
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
		c, err := NewObjectClient(name, cfg, clientMetrics)
		if err != nil {
			return nil, err
		}
		return client.NewClientWithMaxParallel(c, nil, cfg.MaxParallelGetChunk, schemaCfg), nil
	case config.StorageTypeAlibabaCloud:
		c, err := alibaba.NewOssObjectClient(context.Background(), cfg.AlibabaStorageConfig)
		if err != nil {
			return nil, err
		}
		return client.NewClientWithMaxParallel(c, nil, cfg.MaxParallelGetChunk, schemaCfg), nil
	case config.StorageTypeBOS:
		c, err := NewObjectClient(name, cfg, clientMetrics)
		if err != nil {
			return nil, err
		}
		return client.NewClientWithMaxParallel(c, nil, cfg.MaxChunkBatchSize, schemaCfg), nil
	case config.StorageTypeGCP:
		return gcp.NewBigtableObjectClient(context.Background(), cfg.GCPStorageConfig, schemaCfg)
	case config.StorageTypeGCPColumnKey, config.StorageTypeBigTable, config.StorageTypeBigTableHashed:
		return gcp.NewBigtableObjectClient(context.Background(), cfg.GCPStorageConfig, schemaCfg)
	case config.StorageTypeGCS:
		c, err := NewObjectClient(name, cfg, clientMetrics)
		if err != nil {
			return nil, err
		}
		return client.NewClientWithMaxParallel(c, nil, cfg.MaxParallelGetChunk, schemaCfg), nil
	case config.StorageTypeSwift:
		c, err := NewObjectClient(name, cfg, clientMetrics)
		if err != nil {
			return nil, err
		}
		return client.NewClientWithMaxParallel(c, nil, cfg.MaxParallelGetChunk, schemaCfg), nil
	case config.StorageTypeCassandra:
		return cassandra.NewObjectClient(cfg.CassandraStorageConfig, schemaCfg, registerer, cfg.MaxParallelGetChunk)
	case config.StorageTypeFileSystem:
		c, err := NewObjectClient(name, cfg, clientMetrics)
		if err != nil {
			return nil, err
		}
		return client.NewClientWithMaxParallel(c, client.FSEncoder, cfg.MaxParallelGetChunk, schemaCfg), nil
	case config.StorageTypeGrpc:
		return grpc.NewStorageClient(cfg.GrpcConfig, schemaCfg)
	case config.StorageTypeCOS:
		c, err := NewObjectClient(name, cfg, clientMetrics)
		if err != nil {
			return nil, err
		}
		return client.NewClientWithMaxParallel(c, nil, cfg.MaxParallelGetChunk, schemaCfg), nil
	default:
		return nil, fmt.Errorf("Unrecognized storage client %v, choose one of: %v, %v, %v, %v, %v, %v, %v, %v, %v", name, config.StorageTypeAWS, config.StorageTypeAzure, config.StorageTypeCassandra, config.StorageTypeInMemory, config.StorageTypeGCP, config.StorageTypeBigTable, config.StorageTypeBigTableHashed, config.StorageTypeGrpc, config.StorageTypeCOS)
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
	var (
		namedStore string
		storeType  = name
	)

	// lookup storeType for named stores
	if nsType, ok := cfg.NamedStores.storeType[name]; ok {
		storeType = nsType
		namedStore = name
	}

	switch storeType {
	case config.StorageTypeAWS, config.StorageTypeS3:
		s3Cfg := cfg.AWSStorageConfig.S3Config
		if namedStore != "" {
			awsCfg, ok := cfg.NamedStores.AWS[namedStore]
			if !ok {
				return nil, fmt.Errorf("Unrecognized named aws storage config %s", name)
			}

			s3Cfg = awsCfg.S3Config
		}

		return aws.NewS3ObjectClient(s3Cfg, cfg.Hedging)
	case config.StorageTypeAlibabaCloud:
		ossCfg := cfg.AlibabaStorageConfig
		if namedStore != "" {
			var ok bool
			ossCfg, ok = cfg.NamedStores.AlibabaCloud[namedStore]
			if !ok {
				return nil, fmt.Errorf("Unrecognized named alibabacloud oss storage config %s", name)
			}
		}
		return alibaba.NewOssObjectClient(context.Background(), ossCfg)
	case config.StorageTypeGCS:
		gcsCfg := cfg.GCSConfig
		if namedStore != "" {
			var ok bool
			gcsCfg, ok = cfg.NamedStores.GCS[namedStore]
			if !ok {
				return nil, fmt.Errorf("Unrecognized named gcs storage config %s", name)
			}
		}

		return gcp.NewGCSObjectClient(context.Background(), gcsCfg, cfg.Hedging)
	case config.StorageTypeAzure:
		azureCfg := cfg.AzureStorageConfig
		if namedStore != "" {
			var ok bool
			azureCfg, ok = cfg.NamedStores.Azure[namedStore]
			if !ok {
				return nil, fmt.Errorf("Unrecognized named azure storage config %s", name)
			}
		}

		return azure.NewBlobStorage(&azureCfg, clientMetrics.AzureMetrics, cfg.Hedging)
	case config.StorageTypeSwift:
		swiftCfg := cfg.Swift
		if namedStore != "" {
			var ok bool
			swiftCfg, ok = cfg.NamedStores.Swift[namedStore]
			if !ok {
				return nil, fmt.Errorf("Unrecognized named swift storage config %s", name)
			}
		}

		return openstack.NewSwiftObjectClient(swiftCfg, cfg.Hedging)
	case config.StorageTypeInMemory:
		return testutils.NewMockStorage(), nil
	case config.StorageTypeFileSystem:
		fsCfg := cfg.FSConfig
		if namedStore != "" {
			var ok bool
			fsCfg, ok = cfg.NamedStores.Filesystem[namedStore]
			if !ok {
				return nil, fmt.Errorf("Unrecognized named filesystem storage config %s", name)
			}
		}

		return local.NewFSObjectClient(fsCfg)
	case config.StorageTypeBOS:
		bosCfg := cfg.BOSStorageConfig
		if namedStore != "" {
			var ok bool
			bosCfg, ok = cfg.NamedStores.BOS[namedStore]
			if !ok {
				return nil, fmt.Errorf("Unrecognized named bos storage config %s", name)
			}
		}

		return baidubce.NewBOSObjectStorage(&bosCfg)

	case config.StorageTypeCOS:
		cosCfg := cfg.COSConfig
		if namedStore != "" {
			var ok bool
			cosCfg, ok = cfg.NamedStores.COS[namedStore]
			if !ok {
				return nil, fmt.Errorf("Unrecognized named cos storage config %s", name)
			}
		}
		return ibmcloud.NewCOSObjectClient(cosCfg, cfg.Hedging)
	default:
		return nil, fmt.Errorf("Unrecognized storage client %v, choose one of: %v, %v, %v, %v, %v, %v", name, config.StorageTypeAWS, config.StorageTypeS3, config.StorageTypeGCS, config.StorageTypeAzure, config.StorageTypeFileSystem, config.StorageTypeCOS)
	}
}
