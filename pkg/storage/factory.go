package storage

import (
	"context"
	"flag"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/flagext"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/loki/v3/pkg/indexgateway"
	"github.com/grafana/loki/v3/pkg/storage/bucket"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/alibaba"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/aws"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/azure"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/baidubce"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/congestion"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/gcp"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/hedging"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/ibmcloud"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/local"
	"github.com/grafana/loki/v3/pkg/storage/chunk/client/openstack"
	"github.com/grafana/loki/v3/pkg/storage/config"
	"github.com/grafana/loki/v3/pkg/storage/stores"
	bloomshipperconfig "github.com/grafana/loki/v3/pkg/storage/stores/shipper/bloomshipper/config"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/downloads"
	"github.com/grafana/loki/v3/pkg/storage/types"
	"github.com/grafana/loki/v3/pkg/util"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
)

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

// We cannot implement Unmarshaler directly on aws.S3Config or other stores
// as it would end up overriding values set as part of ApplyDynamicConfig().
// Note: we unmarshal a second time after applying dynamic configs
//
// Implementing the Unmarshaler for Named*StorageConfig types is fine as
// we do not apply any dynamic config on them.

type NamedAWSStorageConfig aws.S3Config

// UnmarshalYAML implements the yaml.Unmarshaler interface.
func (cfg *NamedAWSStorageConfig) UnmarshalYAML(unmarshal func(interface{}) error) error {
	flagext.DefaultValues((*aws.S3Config)(cfg))
	return unmarshal((*aws.S3Config)(cfg))
}

func (cfg *NamedAWSStorageConfig) Validate() error {
	return (*aws.S3Config)(cfg).Validate()
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
		if slices.Contains(types.SupportedStorageTypes, name) {
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
		ns.storeType[name] = types.StorageTypeAWS
	}

	for name := range ns.Azure {
		if err := checkForDuplicates(name); err != nil {
			return err
		}
		ns.storeType[name] = types.StorageTypeAzure
	}
	for name := range ns.AlibabaCloud {
		if err := checkForDuplicates(name); err != nil {
			return err
		}
		ns.storeType[name] = types.StorageTypeAlibabaCloud
	}
	for name := range ns.BOS {
		if err := checkForDuplicates(name); err != nil {
			return err
		}
		ns.storeType[name] = types.StorageTypeBOS
	}

	for name := range ns.Filesystem {
		if err := checkForDuplicates(name); err != nil {
			return err
		}
		ns.storeType[name] = types.StorageTypeFileSystem
	}

	for name := range ns.GCS {
		if err := checkForDuplicates(name); err != nil {
			return err
		}
		ns.storeType[name] = types.StorageTypeGCS
	}

	for name := range ns.Swift {
		if err := checkForDuplicates(name); err != nil {
			return err
		}
		ns.storeType[name] = types.StorageTypeSwift
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

func (ns *NamedStores) Exists(name string) bool {
	_, ok := ns.storeType[name]
	return ok
}

// Config chooses which storage client to use.
type Config struct {
	AlibabaStorageConfig alibaba.OssConfig         `yaml:"alibabacloud"`
	S3Config             aws.S3Config              `yaml:"aws"`
	AzureStorageConfig   azure.BlobStorageConfig   `yaml:"azure"`
	BOSStorageConfig     baidubce.BOSStorageConfig `yaml:"bos"`
	GCSConfig            gcp.GCSConfig             `yaml:"gcs" doc:"description=Configures storing chunks in GCS. Required fields only required when gcs is defined in config."`
	FSConfig             local.FSConfig            `yaml:"filesystem" doc:"description=Configures storing the chunks on the local file system. Required fields only required when filesystem is present in the configuration."`
	Swift                openstack.SwiftConfig     `yaml:"swift"`
	Hedging              hedging.Config            `yaml:"hedging"`
	NamedStores          NamedStores               `yaml:"named_stores"`
	COSConfig            ibmcloud.COSConfig        `yaml:"cos"`
	IndexCacheValidity   time.Duration             `yaml:"index_cache_validity"`
	CongestionControl    congestion.Config         `yaml:"congestion_control,omitempty"`
	ObjectPrefix         string                    `yaml:"object_prefix" doc:"description=Experimental. Sets a constant prefix for all keys inserted into object storage. Example: loki/"`

	DisableBroadIndexQueries bool `yaml:"disable_broad_index_queries"`
	MaxParallelGetChunk      int  `yaml:"max_parallel_get_chunk"`

	UseThanosObjstore bool                         `yaml:"use_thanos_objstore"`
	ObjectStore       bucket.ConfigWithNamedStores `yaml:"object_store"`

	MaxChunkBatchSize int `yaml:"max_chunk_batch_size"`

	TSDBShipperConfig  indexshipper.Config       `yaml:"tsdb_shipper" doc:"description=Configures storing index in an Object Store (GCS/S3/Azure/Swift/COS/Filesystem) in a prometheus TSDB-like format. Required fields only required when TSDB is defined in config."`
	BloomShipperConfig bloomshipperconfig.Config `yaml:"bloom_shipper" category:"experimental" doc:"description=Experimental: Configures the bloom shipper component, which contains the store abstraction to fetch bloom filters from and put them to object storage."`

	// Config for using AsyncStore when using async index stores like `tsdb`.
	// It is required for getting chunk ids of recently flushed chunks from the ingesters.
	EnableAsyncStore bool          `yaml:"-"`
	AsyncStoreConfig AsyncStoreCfg `yaml:"-"`

	// ObjectClientDecorator, if set, wraps every ObjectClient after creation.
	// This is intended for testing (e.g. injecting latency simulation).
	ObjectClientDecorator func(client.ObjectClient) client.ObjectClient `yaml:"-"`
}

// RegisterFlags adds the flags required to configure this flag set.
func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	cfg.AlibabaStorageConfig.RegisterFlags(f)
	cfg.S3Config.RegisterFlags(f)
	cfg.AzureStorageConfig.RegisterFlags(f)
	cfg.BOSStorageConfig.RegisterFlags(f)
	cfg.COSConfig.RegisterFlags(f)
	cfg.GCSConfig.RegisterFlags(f)
	cfg.FSConfig.RegisterFlags(f)
	cfg.Swift.RegisterFlags(f)
	cfg.Hedging.RegisterFlagsWithPrefix("store.", f)
	cfg.CongestionControl.RegisterFlagsWithPrefix("store.", f)

	f.BoolVar(&cfg.UseThanosObjstore, "use-thanos-objstore", false, "Enables the use of thanos-io/objstore clients for connecting to object storage. When set to true, the configuration inside `storage_config.object_store` or `common.storage.object_store` block takes effect.")
	cfg.ObjectStore.RegisterFlagsWithPrefix("object-store.", f)

	f.DurationVar(&cfg.IndexCacheValidity, "store.index-cache-validity", 5*time.Minute, "Cache validity for active index entries. Should be no higher than -ingester.max-chunk-idle.")
	f.StringVar(&cfg.ObjectPrefix, "store.object-prefix", "", "The prefix to all keys inserted in object storage. Example: loki-instances/west/")
	f.BoolVar(&cfg.DisableBroadIndexQueries, "store.disable-broad-index-queries", false, "Disable broad index queries which results in reduced cache usage and faster query performance at the expense of somewhat higher QPS on the index store.")
	f.IntVar(&cfg.MaxParallelGetChunk, "store.max-parallel-get-chunk", 150, "Maximum number of parallel chunk reads.")

	f.IntVar(&cfg.MaxChunkBatchSize, "store.max-chunk-batch-size", 50, "The maximum number of chunks to fetch per batch.")
	cfg.TSDBShipperConfig.RegisterFlagsWithPrefix("tsdb.", f)
	cfg.BloomShipperConfig.RegisterFlagsWithPrefix("bloom.", f)
}

// Validate config and returns error on failure
func (cfg *Config) Validate() error {
	if err := cfg.Swift.Validate(); err != nil {
		return errors.Wrap(err, "invalid Swift Storage config")
	}
	if err := cfg.AzureStorageConfig.Validate(); err != nil {
		return errors.Wrap(err, "invalid Azure Storage config")
	}
	if err := cfg.S3Config.Validate(); err != nil {
		return errors.Wrap(err, "invalid AWS Storage config")
	}
	if err := cfg.TSDBShipperConfig.Validate(); err != nil {
		return errors.Wrap(err, "invalid tsdb config")
	}
	if err := cfg.BloomShipperConfig.Validate(); err != nil {
		return errors.Wrap(err, "invalid bloom shipper config")
	}
	if err := cfg.ObjectStore.Validate(); err != nil {
		return errors.Wrap(err, "invalid object store config")
	}
	if err := cfg.AlibabaStorageConfig.Validate(); err != nil {
		return errors.Wrap(err, "invalid Alibaba Storage config")
	}

	return cfg.NamedStores.Validate()
}

// NewChunkClient makes a new chunk.Client of the desired types.
func NewChunkClient(name, component string, cfg Config, schemaCfg config.SchemaConfig, p config.PeriodConfig, registerer prometheus.Registerer, clientMetrics ClientMetrics, logger log.Logger) (client.Client, error) {
	var cc congestion.Controller
	ccCfg := cfg.CongestionControl

	if ccCfg.Enabled {
		cc = congestion.NewController(
			ccCfg,
			logger,
			congestion.NewMetrics(fmt.Sprintf("%s-%s", name, p.From.String()), ccCfg),
		)
	}

	var storeType = name

	if cfg.UseThanosObjstore {
		// Check if this is a named store and get its type
		if st, ok := cfg.ObjectStore.NamedStores.LookupStoreType(name); ok {
			storeType = st
		}

		var (
			c   client.ObjectClient
			err error
		)
		c, err = NewObjectClient(name, component, cfg, clientMetrics)
		if err != nil {
			return nil, err
		}

		var encoder client.KeyEncoder
		if storeType == bucket.Filesystem {
			encoder = client.FSEncoder
		} else if cfg.CongestionControl.Enabled {
			// Apply congestion control wrapper for non-filesystem storage
			c = cc.Wrap(c)
		}

		return client.NewClientWithMaxParallel(c, encoder, cfg.MaxParallelGetChunk, schemaCfg), nil
	}

	// lookup storeType for named stores
	if nsType, ok := cfg.NamedStores.storeType[name]; ok {
		storeType = nsType
	}

	switch true {

	case util.StringsContain(types.SupportedStorageTypes, storeType):
		switch storeType {
		case types.StorageTypeFileSystem:
			c, err := NewObjectClient(name, component, cfg, clientMetrics)
			if err != nil {
				return nil, err
			}
			return client.NewClientWithMaxParallel(c, client.FSEncoder, cfg.MaxParallelGetChunk, schemaCfg), nil

		case types.StorageTypeAWS, types.StorageTypeS3, types.StorageTypeAzure, types.StorageTypeBOS, types.StorageTypeSwift, types.StorageTypeCOS, types.StorageTypeAlibabaCloud:
			c, err := NewObjectClient(name, component, cfg, clientMetrics)
			if err != nil {
				return nil, err
			}
			if cfg.CongestionControl.Enabled {
				c = cc.Wrap(c)
			}
			return client.NewClientWithMaxParallel(c, nil, cfg.MaxParallelGetChunk, schemaCfg), nil

		case types.StorageTypeGCS:
			c, err := NewObjectClient(name, component, cfg, clientMetrics)
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

	}

	return nil, fmt.Errorf("unrecognized chunk client type %s, choose one of: %s", name, strings.Join(types.SupportedStorageTypes, ", "))
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
func NewObjectClient(name, component string, cfg Config, clientMetrics ClientMetrics) (client.ObjectClient, error) {
	if cfg.UseThanosObjstore {
		return bucket.NewObjectClient(context.Background(), name, cfg.ObjectStore, component, cfg.Hedging, false, util_log.Logger)
	}

	actual, err := internalNewObjectClient(name, cfg, clientMetrics)
	if err != nil {
		return nil, err
	}

	if cfg.ObjectClientDecorator != nil {
		actual = cfg.ObjectClientDecorator(actual)
	}

	if cfg.ObjectPrefix == "" {
		return actual, nil
	} else {
		prefix := strings.Trim(cfg.ObjectPrefix, "/") + "/"
		return client.NewPrefixedObjectClient(actual, prefix), nil
	}
}

// internalNewObjectClient makes the underlying StorageClient of the desired types.
func internalNewObjectClient(storeName string, cfg Config, clientMetrics ClientMetrics) (client.ObjectClient, error) {
	var (
		namedStore string
		storeType  = storeName
	)

	// lookup storeType for named stores
	if nsType, ok := cfg.NamedStores.storeType[storeName]; ok {
		storeType = nsType
		namedStore = storeName
	}

	switch storeType {

	case types.StorageTypeAWS, types.StorageTypeS3:
		s3Cfg := cfg.S3Config
		if namedStore != "" {
			awsCfg, ok := cfg.NamedStores.AWS[namedStore]
			if !ok {
				return nil, fmt.Errorf("Unrecognized named aws storage config %s", storeName)
			}
			s3Cfg = (aws.S3Config)(awsCfg)
		}

		if cfg.CongestionControl.Enabled {
			s3Cfg.BackoffConfig.MaxRetries = 1
		}

		return aws.NewS3ObjectClient(s3Cfg, cfg.Hedging)

	case types.StorageTypeAlibabaCloud:
		ossCfg := cfg.AlibabaStorageConfig
		if namedStore != "" {
			nsCfg, ok := cfg.NamedStores.AlibabaCloud[namedStore]
			if !ok {
				return nil, fmt.Errorf("Unrecognized named alibabacloud oss storage config %s", storeName)
			}

			ossCfg = (alibaba.OssConfig)(nsCfg)
		}
		return alibaba.NewOssObjectClient(context.Background(), ossCfg)

	case types.StorageTypeGCS:
		gcsCfg := cfg.GCSConfig
		if namedStore != "" {
			nsCfg, ok := cfg.NamedStores.GCS[namedStore]
			if !ok {
				return nil, fmt.Errorf("Unrecognized named gcs storage config %s", storeName)
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

	case types.StorageTypeAzure:
		azureCfg := cfg.AzureStorageConfig
		if namedStore != "" {
			nsCfg, ok := cfg.NamedStores.Azure[namedStore]
			if !ok {
				return nil, fmt.Errorf("Unrecognized named azure storage config %s", storeName)
			}
			azureCfg = (azure.BlobStorageConfig)(nsCfg)
		}
		return azure.NewBlobStorage(&azureCfg, clientMetrics.AzureMetrics, cfg.Hedging)

	case types.StorageTypeSwift:
		swiftCfg := cfg.Swift
		if namedStore != "" {
			nsCfg, ok := cfg.NamedStores.Swift[namedStore]
			if !ok {
				return nil, fmt.Errorf("Unrecognized named swift storage config %s", storeName)
			}
			swiftCfg = (openstack.SwiftConfig)(nsCfg)
		}
		return openstack.NewSwiftObjectClient(swiftCfg, cfg.Hedging)

	case types.StorageTypeFileSystem:
		fsCfg := cfg.FSConfig
		if namedStore != "" {
			nsCfg, ok := cfg.NamedStores.Filesystem[namedStore]
			if !ok {
				return nil, fmt.Errorf("Unrecognized named filesystem storage config %s", storeName)
			}
			fsCfg = (local.FSConfig)(nsCfg)
		}
		return local.NewFSObjectClient(fsCfg)

	case types.StorageTypeBOS:
		bosCfg := cfg.BOSStorageConfig
		if namedStore != "" {
			nsCfg, ok := cfg.NamedStores.BOS[namedStore]
			if !ok {
				return nil, fmt.Errorf("Unrecognized named bos storage config %s", storeName)
			}

			bosCfg = (baidubce.BOSStorageConfig)(nsCfg)
		}
		return baidubce.NewBOSObjectStorage(&bosCfg)

	case types.StorageTypeCOS:
		cosCfg := cfg.COSConfig
		if namedStore != "" {
			nsCfg, ok := cfg.NamedStores.COS[namedStore]
			if !ok {
				return nil, fmt.Errorf("Unrecognized named cos storage config %s", storeName)
			}

			cosCfg = (ibmcloud.COSConfig)(nsCfg)
		}
		return ibmcloud.NewCOSObjectClient(cosCfg, cfg.Hedging)

	default:
		return nil, fmt.Errorf("Unrecognized storage client %s, choose one of: %s", storeName, strings.Join(types.SupportedStorageTypes, ", "))
	}
}
