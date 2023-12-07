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

	"github.com/grafana/dskit/flagext"

	"github.com/grafana/loki/pkg/storage/chunk/cache"
	"github.com/grafana/loki/pkg/storage/chunk/client"
	"github.com/grafana/loki/pkg/storage/chunk/client/alibaba"
	"github.com/grafana/loki/pkg/storage/chunk/client/aws"
	"github.com/grafana/loki/pkg/storage/chunk/client/azure"
	"github.com/grafana/loki/pkg/storage/chunk/client/baidubce"
	"github.com/grafana/loki/pkg/storage/chunk/client/cassandra"
	"github.com/grafana/loki/pkg/storage/chunk/client/congestion"
	"github.com/grafana/loki/pkg/storage/chunk/client/gcp"
	"github.com/grafana/loki/pkg/storage/chunk/client/grpc"
	"github.com/grafana/loki/pkg/storage/chunk/client/hedging"
	"github.com/grafana/loki/pkg/storage/chunk/client/ibmcloud"
	"github.com/grafana/loki/pkg/storage/chunk/client/local"
	"github.com/grafana/loki/pkg/storage/chunk/client/openstack"
	"github.com/grafana/loki/pkg/storage/chunk/client/testutils"
	"github.com/grafana/loki/pkg/storage/config"
	"github.com/grafana/loki/pkg/storage/stores"
	"github.com/grafana/loki/pkg/storage/stores/series/index"
	bloomshipperconfig "github.com/grafana/loki/pkg/storage/stores/shipper/bloomshipper/config"
	"github.com/grafana/loki/pkg/storage/stores/shipper/indexshipper"
	"github.com/grafana/loki/pkg/storage/stores/shipper/indexshipper/boltdb"
	"github.com/grafana/loki/pkg/storage/stores/shipper/indexshipper/downloads"
	"github.com/grafana/loki/pkg/storage/stores/shipper/indexshipper/gatewayclient"
	"github.com/grafana/loki/pkg/storage/stores/shipper/indexshipper/indexgateway"
	"github.com/grafana/loki/pkg/util"
	"github.com/grafana/loki/pkg/util/constants"
)

var (
	indexGatewayClient index.Client
	// singleton for each period
	boltdbIndexClientsWithShipper = make(map[config.DayTime]*boltdb.IndexClient)

	supportedIndexTypes = []string{
		config.BoltDBShipperType,
		config.TSDBType,
	}

	deprecatedIndexTypes = []string{
		config.StorageTypeAWS,
		config.StorageTypeAWSDynamo,
		config.StorageTypeBigTable,
		config.StorageTypeBigTableHashed,
		config.StorageTypeBoltDB,
		config.StorageTypeCassandra,
		config.StorageTypeGCP,
		config.StorageTypeGCPColumnKey,
		config.StorageTypeGrpc,
	}

	supportedStorageTypes = []string{
		// local file system
		config.StorageTypeFileSystem,
		// remote object storages
		config.StorageTypeAWS,
		config.StorageTypeAlibabaCloud,
		config.StorageTypeAzure,
		config.StorageTypeBOS,
		config.StorageTypeCOS,
		config.StorageTypeGCS,
		config.StorageTypeS3,
		config.StorageTypeSwift,
	}

	deprecatedStorageTypes = []string{
		config.StorageTypeAWSDynamo,
		config.StorageTypeBigTable,
		config.StorageTypeBigTableHashed,
		config.StorageTypeCassandra,
		config.StorageTypeGCP,
		config.StorageTypeGCPColumnKey,
		config.StorageTypeGrpc,
	}

	testingStorageTypes = []string{
		config.StorageTypeInMemory,
	}
)

// ResetBoltDBIndexClientsWithShipper allows to reset the singletons.
// MUST ONLY BE USED IN TESTS
func ResetBoltDBIndexClientsWithShipper() {
	for _, client := range boltdbIndexClientsWithShipper {
		client.Stop()
	}

	boltdbIndexClientsWithShipper = make(map[config.DayTime]*boltdb.IndexClient)

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

// Storage configs defined as Named stores don't get any defaults as they do not
// register flags. To get around this we implement Unmarshaler interface that
// assigns the defaults before calling unmarshal.

// We cannot implement Unmarshaler directly on aws.StorageConfig or other stores
// as it would end up overriding values set as part of ApplyDynamicConfig().
// Note: we unmarshal a second time after applying dynamic configs
//
// Implementing the Unmarshaler for Named*StorageConfig types is fine as
// we do not apply any dynamic config on them.

type NamedAWSStorageConfig aws.StorageConfig

// UnmarshalYAML implements the yaml.Unmarshaler interface.
func (cfg *NamedAWSStorageConfig) UnmarshalYAML(unmarshal func(interface{}) error) error {
	flagext.DefaultValues((*aws.StorageConfig)(cfg))
	return unmarshal((*aws.StorageConfig)(cfg))
}

func (cfg *NamedAWSStorageConfig) Validate() error {
	return (*aws.StorageConfig)(cfg).Validate()
}

type NamedBlobStorageConfig azure.BlobStorageConfig

// UnmarshalYAML implements the yaml.Unmarshaler interface.
func (cfg *NamedBlobStorageConfig) UnmarshalYAML(unmarshal func(interface{}) error) error {
	flagext.DefaultValues((*azure.BlobStorageConfig)(cfg))
	return unmarshal((*azure.BlobStorageConfig)(cfg))
}

func (cfg *NamedBlobStorageConfig) Validate() error {
	return (*azure.BlobStorageConfig)(cfg).Validate()
}

type NamedBOSStorageConfig baidubce.BOSStorageConfig

// UnmarshalYAML implements the yaml.Unmarshaler interface.
func (cfg *NamedBOSStorageConfig) UnmarshalYAML(unmarshal func(interface{}) error) error {
	flagext.DefaultValues((*baidubce.BOSStorageConfig)(cfg))
	return unmarshal((*baidubce.BOSStorageConfig)(cfg))
}

type NamedFSConfig local.FSConfig

// UnmarshalYAML implements the yaml.Unmarshaler interface.
func (cfg *NamedFSConfig) UnmarshalYAML(unmarshal func(interface{}) error) error {
	flagext.DefaultValues((*local.FSConfig)(cfg))
	return unmarshal((*local.FSConfig)(cfg))
}

type NamedGCSConfig gcp.GCSConfig

// UnmarshalYAML implements the yaml.Unmarshaler interface.
func (cfg *NamedGCSConfig) UnmarshalYAML(unmarshal func(interface{}) error) error {
	flagext.DefaultValues((*gcp.GCSConfig)(cfg))
	return unmarshal((*gcp.GCSConfig)(cfg))
}

type NamedOssConfig alibaba.OssConfig

// UnmarshalYAML implements the yaml.Unmarshaler interface.
func (cfg *NamedOssConfig) UnmarshalYAML(unmarshal func(interface{}) error) error {
	flagext.DefaultValues((*alibaba.OssConfig)(cfg))
	return unmarshal((*alibaba.OssConfig)(cfg))
}

type NamedSwiftConfig openstack.SwiftConfig

// UnmarshalYAML implements the yaml.Unmarshaler interface.
func (cfg *NamedSwiftConfig) UnmarshalYAML(unmarshal func(interface{}) error) error {
	flagext.DefaultValues((*openstack.SwiftConfig)(cfg))
	return unmarshal((*openstack.SwiftConfig)(cfg))
}

func (cfg *NamedSwiftConfig) Validate() error {
	return (*openstack.SwiftConfig)(cfg).Validate()
}

type NamedCOSConfig ibmcloud.COSConfig

// UnmarshalYAML implements the yaml.Unmarshaler interface.
func (cfg *NamedCOSConfig) UnmarshalYAML(unmarshal func(interface{}) error) error {
	flagext.DefaultValues((*ibmcloud.COSConfig)(cfg))
	return unmarshal((*ibmcloud.COSConfig)(cfg))
}

// NamedStores helps configure additional object stores from a given storage provider
type NamedStores struct {
	AWS          map[string]NamedAWSStorageConfig  `yaml:"aws"`
	Azure        map[string]NamedBlobStorageConfig `yaml:"azure"`
	BOS          map[string]NamedBOSStorageConfig  `yaml:"bos"`
	Filesystem   map[string]NamedFSConfig          `yaml:"filesystem"`
	GCS          map[string]NamedGCSConfig         `yaml:"gcs"`
	AlibabaCloud map[string]NamedOssConfig         `yaml:"alibabacloud"`
	Swift        map[string]NamedSwiftConfig       `yaml:"swift"`
	COS          map[string]NamedCOSConfig         `yaml:"cos"`

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

func (ns *NamedStores) Validate() error {
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
	GCPStorageConfig       gcp.Config                `yaml:"bigtable" doc:"description=Deprecated: Configures storing indexes in Bigtable. Required fields only required when bigtable is defined in config."`
	GCSConfig              gcp.GCSConfig             `yaml:"gcs" doc:"description=Configures storing chunks in GCS. Required fields only required when gcs is defined in config."`
	CassandraStorageConfig cassandra.Config          `yaml:"cassandra" doc:"description=Deprecated: Configures storing chunks and/or the index in Cassandra."`
	BoltDBConfig           local.BoltDBConfig        `yaml:"boltdb" doc:"description=Deprecated: Configures storing index in BoltDB. Required fields only required when boltdb is present in the configuration."`
	FSConfig               local.FSConfig            `yaml:"filesystem" doc:"description=Configures storing the chunks on the local file system. Required fields only required when filesystem is present in the configuration."`
	Swift                  openstack.SwiftConfig     `yaml:"swift"`
	GrpcConfig             grpc.Config               `yaml:"grpc_store" doc:"deprecated"`
	Hedging                hedging.Config            `yaml:"hedging"`
	NamedStores            NamedStores               `yaml:"named_stores"`
	COSConfig              ibmcloud.COSConfig        `yaml:"cos"`
	IndexCacheValidity     time.Duration             `yaml:"index_cache_validity"`
	CongestionControl      congestion.Config         `yaml:"congestion_control,omitempty"`
	ObjectPrefix           string                    `yaml:"object_prefix" doc:"description=Experimental. Sets a constant prefix for all keys inserted into object storage. Example: loki/"`

	IndexQueriesCacheConfig  cache.Config `yaml:"index_queries_cache_config"`
	DisableBroadIndexQueries bool         `yaml:"disable_broad_index_queries"`
	MaxParallelGetChunk      int          `yaml:"max_parallel_get_chunk"`

	MaxChunkBatchSize   int                       `yaml:"max_chunk_batch_size"`
	BoltDBShipperConfig boltdb.IndexCfg           `yaml:"boltdb_shipper" doc:"description=Configures storing index in an Object Store (GCS/S3/Azure/Swift/COS/Filesystem) in the form of boltdb files. Required fields only required when boltdb-shipper is defined in config."`
	TSDBShipperConfig   indexshipper.Config       `yaml:"tsdb_shipper" doc:"description=Configures storing index in an Object Store (GCS/S3/Azure/Swift/COS/Filesystem) in a prometheus TSDB-like format. Required fields only required when TSDB is defined in config."`
	BloomShipperConfig  bloomshipperconfig.Config `yaml:"bloom_shipper" doc:"description=Configures Bloom Shipper."`

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
	cfg.COSConfig.RegisterFlags(f)
	cfg.GCPStorageConfig.RegisterFlags(f)
	cfg.GCSConfig.RegisterFlags(f)
	cfg.CassandraStorageConfig.RegisterFlags(f)
	cfg.BoltDBConfig.RegisterFlags(f)
	cfg.FSConfig.RegisterFlags(f)
	cfg.Swift.RegisterFlags(f)
	cfg.GrpcConfig.RegisterFlags(f)
	cfg.Hedging.RegisterFlagsWithPrefix("store.", f)
	cfg.CongestionControl.RegisterFlagsWithPrefix("store.", f)

	cfg.IndexQueriesCacheConfig.RegisterFlagsWithPrefix("store.index-cache-read.", "", f)
	f.DurationVar(&cfg.IndexCacheValidity, "store.index-cache-validity", 5*time.Minute, "Cache validity for active index entries. Should be no higher than -ingester.max-chunk-idle.")
	f.StringVar(&cfg.ObjectPrefix, "store.object-prefix", "", "The prefix to all keys inserted in object storage. Example: loki-instances/west/")
	f.BoolVar(&cfg.DisableBroadIndexQueries, "store.disable-broad-index-queries", false, "Disable broad index queries which results in reduced cache usage and faster query performance at the expense of somewhat higher QPS on the index store.")
	f.IntVar(&cfg.MaxParallelGetChunk, "store.max-parallel-get-chunk", 150, "Maximum number of parallel chunk reads.")
	cfg.BoltDBShipperConfig.RegisterFlags(f)
	f.IntVar(&cfg.MaxChunkBatchSize, "store.max-chunk-batch-size", 50, "The maximum number of chunks to fetch per batch.")
	cfg.TSDBShipperConfig.RegisterFlagsWithPrefix("tsdb.", f)
	cfg.BloomShipperConfig.RegisterFlagsWithPrefix("bloom.", f)
}

// Validate config and returns error on failure
func (cfg *Config) Validate() error {
	if err := cfg.CassandraStorageConfig.Validate(); err != nil {
		return errors.Wrap(err, "invalid Cassandra Storage config")
	}
	if err := cfg.GCPStorageConfig.Validate(); err != nil {
		return errors.Wrap(err, "invalid GCP Storage Storage config")
	}
	if err := cfg.Swift.Validate(); err != nil {
		return errors.Wrap(err, "invalid Swift Storage config")
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
	if err := cfg.BloomShipperConfig.Validate(); err != nil {
		return errors.Wrap(err, "invalid bloom shipper config")
	}

	return cfg.NamedStores.Validate()
}

// NewIndexClient creates a new index client of the desired type specified in the PeriodConfig
func NewIndexClient(periodCfg config.PeriodConfig, tableRange config.TableRange, cfg Config, schemaCfg config.SchemaConfig, limits StoreLimits, cm ClientMetrics, shardingStrategy indexgateway.ShardingStrategy, registerer prometheus.Registerer, logger log.Logger, metricsNamespace string) (index.Client, error) {

	switch true {
	case util.StringsContain(testingStorageTypes, periodCfg.IndexType):
		switch periodCfg.IndexType {
		case config.StorageTypeInMemory:
			store := testutils.NewMockStorage()
			return store, nil
		}

	case util.StringsContain(supportedIndexTypes, periodCfg.IndexType):
		switch periodCfg.IndexType {
		case config.BoltDBShipperType:
			if shouldUseIndexGatewayClient(cfg.BoltDBShipperConfig.Config) {
				if indexGatewayClient != nil {
					return indexGatewayClient, nil
				}

				gateway, err := gatewayclient.NewGatewayClient(cfg.BoltDBShipperConfig.IndexGatewayClientConfig, registerer, limits, logger, constants.Loki)
				if err != nil {
					return nil, err
				}

				indexGatewayClient = gateway
				return gateway, nil
			}

			if client, ok := boltdbIndexClientsWithShipper[periodCfg.From]; ok {
				return client, nil
			}

			objectClient, err := NewObjectClient(periodCfg.ObjectType, cfg, cm)
			if err != nil {
				return nil, err
			}

			var filterFn downloads.TenantFilter
			if shardingStrategy != nil {
				filterFn = shardingStrategy.FilterTenants
			}
			indexClient, err := boltdb.NewIndexClient(periodCfg.IndexTables.PathPrefix, cfg.BoltDBShipperConfig, objectClient, limits, filterFn, tableRange, registerer, logger)
			if err != nil {
				return nil, err
			}

			boltdbIndexClientsWithShipper[periodCfg.From] = indexClient
			return indexClient, nil

		case config.TSDBType:
			// TODO(chaudum): Move TSDB index client creation into this code path
			return nil, fmt.Errorf("code path not supported")
		}

	case util.StringsContain(deprecatedIndexTypes, periodCfg.IndexType):
		level.Warn(logger).Log("msg", fmt.Sprintf("%s is deprecated. Consider migrating to tsdb", periodCfg.IndexType))

		switch periodCfg.IndexType {
		case config.StorageTypeAWS, config.StorageTypeAWSDynamo:
			if cfg.AWSStorageConfig.DynamoDB.URL == nil {
				return nil, fmt.Errorf("Must set -dynamodb.url in aws mode")
			}
			path := strings.TrimPrefix(cfg.AWSStorageConfig.DynamoDB.URL.Path, "/")
			if len(path) > 0 {
				level.Warn(logger).Log("msg", "ignoring DynamoDB URL path", "path", path)
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
		}
	}

	return nil, fmt.Errorf("unrecognized index client type %s, choose one of: %s", periodCfg.IndexType, strings.Join(supportedIndexTypes, ","))
}

// NewChunkClient makes a new chunk.Client of the desired types.
func NewChunkClient(name string, cfg Config, schemaCfg config.SchemaConfig, cc congestion.Controller, registerer prometheus.Registerer, clientMetrics ClientMetrics, logger log.Logger) (client.Client, error) {
	var storeType = name

	// lookup storeType for named stores
	if nsType, ok := cfg.NamedStores.storeType[name]; ok {
		storeType = nsType
	}

	switch true {

	case util.StringsContain(testingStorageTypes, storeType):
		switch storeType {
		case config.StorageTypeInMemory:
			c, err := NewObjectClient(name, cfg, clientMetrics)
			if err != nil {
				return nil, err
			}
			return client.NewClientWithMaxParallel(c, nil, 1, schemaCfg), nil
		}

	case util.StringsContain(supportedStorageTypes, storeType):
		switch storeType {
		case config.StorageTypeFileSystem:
			c, err := NewObjectClient(name, cfg, clientMetrics)
			if err != nil {
				return nil, err
			}
			return client.NewClientWithMaxParallel(c, client.FSEncoder, cfg.MaxParallelGetChunk, schemaCfg), nil

		case config.StorageTypeAWS, config.StorageTypeS3, config.StorageTypeAzure, config.StorageTypeBOS, config.StorageTypeSwift, config.StorageTypeCOS, config.StorageTypeAlibabaCloud:
			c, err := NewObjectClient(name, cfg, clientMetrics)
			if err != nil {
				return nil, err
			}
			return client.NewClientWithMaxParallel(c, nil, cfg.MaxParallelGetChunk, schemaCfg), nil

		case config.StorageTypeGCS:
			c, err := NewObjectClient(name, cfg, clientMetrics)
			if err != nil {
				return nil, err
			}
			// TODO(dannyk): expand congestion control to all other object clients
			// this switch statement can be simplified; all the branches like this one are alike
			if cfg.CongestionControl.Enabled {
				c = cc.Wrap(c)
			}
			return client.NewClientWithMaxParallel(c, nil, cfg.MaxParallelGetChunk, schemaCfg), nil
		}

	case util.StringsContain(deprecatedStorageTypes, storeType):
		level.Warn(logger).Log("msg", fmt.Sprintf("%s is deprecated. Please use one of the supported object stores: %s", storeType, strings.Join(supportedStorageTypes, ", ")))

		switch storeType {
		case config.StorageTypeAWSDynamo:
			if cfg.AWSStorageConfig.DynamoDB.URL == nil {
				return nil, fmt.Errorf("Must set -dynamodb.url in aws mode")
			}
			path := strings.TrimPrefix(cfg.AWSStorageConfig.DynamoDB.URL.Path, "/")
			if len(path) > 0 {
				level.Warn(logger).Log("msg", "ignoring DynamoDB URL path", "path", path)
			}
			return aws.NewDynamoDBChunkClient(cfg.AWSStorageConfig.DynamoDBConfig, schemaCfg, registerer)

		case config.StorageTypeGCP, config.StorageTypeGCPColumnKey, config.StorageTypeBigTable, config.StorageTypeBigTableHashed:
			return gcp.NewBigtableObjectClient(context.Background(), cfg.GCPStorageConfig, schemaCfg)

		case config.StorageTypeCassandra:
			return cassandra.NewObjectClient(cfg.CassandraStorageConfig, schemaCfg, registerer, cfg.MaxParallelGetChunk)

		case config.StorageTypeGrpc:
			return grpc.NewStorageClient(cfg.GrpcConfig, schemaCfg)
		}
	}

	return nil, fmt.Errorf("unrecognized chunk client type %s, choose one of: %s", name, strings.Join(supportedStorageTypes, ", "))
}

// NewTableClient makes a new table client based on the configuration.
func NewTableClient(name string, periodCfg config.PeriodConfig, cfg Config, cm ClientMetrics, registerer prometheus.Registerer, logger log.Logger) (index.TableClient, error) {
	switch true {
	case util.StringsContain(testingStorageTypes, name):
		switch name {
		case config.StorageTypeInMemory:
			return testutils.NewMockStorage(), nil
		}

	case util.StringsContain(supportedIndexTypes, name):
		objectClient, err := NewObjectClient(periodCfg.ObjectType, cfg, cm)
		if err != nil {
			return nil, err
		}
		return indexshipper.NewTableClient(objectClient, periodCfg.IndexTables.PathPrefix), nil

	case util.StringsContain(deprecatedIndexTypes, name):
		switch name {
		case config.StorageTypeAWS, config.StorageTypeAWSDynamo:
			if cfg.AWSStorageConfig.DynamoDB.URL == nil {
				return nil, fmt.Errorf("Must set -dynamodb.url in aws mode")
			}
			path := strings.TrimPrefix(cfg.AWSStorageConfig.DynamoDB.URL.Path, "/")
			if len(path) > 0 {
				level.Warn(logger).Log("msg", "ignoring DynamoDB URL path", "path", path)
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
		}
	}

	return nil, fmt.Errorf("unrecognized table client type %s, choose one of: %s", name, strings.Join(supportedIndexTypes, ", "))
}

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

// NewObjectClient makes a new StorageClient with the prefix in the front.
func NewObjectClient(name string, cfg Config, clientMetrics ClientMetrics) (client.ObjectClient, error) {
	actual, err := internalNewObjectClient(name, cfg, clientMetrics)
	if err != nil {
		return nil, err
	}

	if cfg.ObjectPrefix == "" {
		return actual, nil
	} else {
		prefix := strings.Trim(cfg.ObjectPrefix, "/") + "/"
		return client.NewPrefixedObjectClient(actual, prefix), nil
	}
}

// internalNewObjectClient makes the underlying StorageClient of the desired types.
func internalNewObjectClient(name string, cfg Config, clientMetrics ClientMetrics) (client.ObjectClient, error) {
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
	case config.StorageTypeInMemory:
		return testutils.NewMockStorage(), nil

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
			nsCfg, ok := cfg.NamedStores.AlibabaCloud[namedStore]
			if !ok {
				return nil, fmt.Errorf("Unrecognized named alibabacloud oss storage config %s", name)
			}

			ossCfg = (alibaba.OssConfig)(nsCfg)
		}
		return alibaba.NewOssObjectClient(context.Background(), ossCfg)

	case config.StorageTypeGCS:
		gcsCfg := cfg.GCSConfig
		if namedStore != "" {
			nsCfg, ok := cfg.NamedStores.GCS[namedStore]
			if !ok {
				return nil, fmt.Errorf("Unrecognized named gcs storage config %s", name)
			}
			gcsCfg = (gcp.GCSConfig)(nsCfg)
		}
		// ensure the GCS client's internal retry mechanism is disabled if we're using congestion control,
		// which has its own retry mechanism
		// TODO(dannyk): implement hedging in controller
		if cfg.CongestionControl.Enabled {
			gcsCfg.EnableRetries = false
		}
		return gcp.NewGCSObjectClient(context.Background(), gcsCfg, cfg.Hedging)

	case config.StorageTypeAzure:
		azureCfg := cfg.AzureStorageConfig
		if namedStore != "" {
			nsCfg, ok := cfg.NamedStores.Azure[namedStore]
			if !ok {
				return nil, fmt.Errorf("Unrecognized named azure storage config %s", name)
			}
			azureCfg = (azure.BlobStorageConfig)(nsCfg)
		}
		return azure.NewBlobStorage(&azureCfg, clientMetrics.AzureMetrics, cfg.Hedging)

	case config.StorageTypeSwift:
		swiftCfg := cfg.Swift
		if namedStore != "" {
			nsCfg, ok := cfg.NamedStores.Swift[namedStore]
			if !ok {
				return nil, fmt.Errorf("Unrecognized named swift storage config %s", name)
			}
			swiftCfg = (openstack.SwiftConfig)(nsCfg)
		}
		return openstack.NewSwiftObjectClient(swiftCfg, cfg.Hedging)

	case config.StorageTypeFileSystem:
		fsCfg := cfg.FSConfig
		if namedStore != "" {
			nsCfg, ok := cfg.NamedStores.Filesystem[namedStore]
			if !ok {
				return nil, fmt.Errorf("Unrecognized named filesystem storage config %s", name)
			}
			fsCfg = (local.FSConfig)(nsCfg)
		}
		return local.NewFSObjectClient(fsCfg)

	case config.StorageTypeBOS:
		bosCfg := cfg.BOSStorageConfig
		if namedStore != "" {
			nsCfg, ok := cfg.NamedStores.BOS[namedStore]
			if !ok {
				return nil, fmt.Errorf("Unrecognized named bos storage config %s", name)
			}

			bosCfg = (baidubce.BOSStorageConfig)(nsCfg)
		}
		return baidubce.NewBOSObjectStorage(&bosCfg)

	case config.StorageTypeCOS:
		cosCfg := cfg.COSConfig
		if namedStore != "" {
			nsCfg, ok := cfg.NamedStores.COS[namedStore]
			if !ok {
				return nil, fmt.Errorf("Unrecognized named cos storage config %s", name)
			}

			cosCfg = (ibmcloud.COSConfig)(nsCfg)
		}
		return ibmcloud.NewCOSObjectClient(cosCfg, cfg.Hedging)

	default:
		return nil, fmt.Errorf("Unrecognized storage client %v, choose one of: %v, %v, %v, %v, %v, %v, %v, %v, %v", name, config.StorageTypeAWS, config.StorageTypeS3, config.StorageTypeGCS, config.StorageTypeAzure, config.StorageTypeAlibabaCloud, config.StorageTypeSwift, config.StorageTypeBOS, config.StorageTypeCOS, config.StorageTypeFileSystem)
	}
}
