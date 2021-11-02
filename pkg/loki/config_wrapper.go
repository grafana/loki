package loki

import (
	"flag"
	"fmt"
	"reflect"
	"strings"

	cortexcache "github.com/cortexproject/cortex/pkg/chunk/cache"
	"github.com/grafana/dskit/flagext"
	"github.com/pkg/errors"

	"github.com/grafana/loki/pkg/storage/chunk/cache"
	"github.com/grafana/loki/pkg/util/cfg"

	loki_storage "github.com/grafana/loki/pkg/storage"
	chunk_storage "github.com/grafana/loki/pkg/storage/chunk/storage"
	loki_net "github.com/grafana/loki/pkg/util/net"
)

// ConfigWrapper is a struct containing the Loki config along with other values that can be set on the command line
// for interacting with the config file or the application directly.
// ConfigWrapper implements cfg.DynamicCloneable, allowing configuration to be dynamically set based
// on the logic in ApplyDynamicConfig, which receives values set in config file
type ConfigWrapper struct {
	Config          `yaml:",inline"`
	PrintVersion    bool
	VerifyConfig    bool
	PrintConfig     bool
	LogConfig       bool
	ConfigFile      string
	ConfigExpandEnv bool
}

func (c *ConfigWrapper) RegisterFlags(f *flag.FlagSet) {
	f.BoolVar(&c.PrintVersion, "version", false, "Print this builds version information")
	f.BoolVar(&c.VerifyConfig, "verify-config", false, "Verify config file and exits")
	f.BoolVar(&c.PrintConfig, "print-config-stderr", false, "Dump the entire Loki config object to stderr")
	f.BoolVar(&c.LogConfig, "log-config-reverse-order", false, "Dump the entire Loki config object at Info log "+
		"level with the order reversed, reversing the order makes viewing the entries easier in Grafana.")
	f.StringVar(&c.ConfigFile, "config.file", "", "yaml file to load")
	f.BoolVar(&c.ConfigExpandEnv, "config.expand-env", false, "Expands ${var} in config according to the values of the environment variables.")
	c.Config.RegisterFlags(f)
}

// Clone takes advantage of pass-by-value semantics to return a distinct *Config.
// This is primarily used to parse a different flag set without mutating the original *Config.
func (c *ConfigWrapper) Clone() flagext.Registerer {
	return func(c ConfigWrapper) *ConfigWrapper {
		return &c
	}(*c)
}

const memberlistStr = "memberlist"

// ApplyDynamicConfig satisfies WithCommonCloneable interface, and applies all rules for setting Loki
// config values from the common section of the Loki config file.
// This method's purpose is to simplify Loki's config in an opinionated way so that Loki can be run
// with the minimal amount of config options for most use cases. It also aims to reduce redundancy where
// some values are set multiple times through the Loki config.
func (c *ConfigWrapper) ApplyDynamicConfig() cfg.Source {
	defaults := ConfigWrapper{}
	flagext.DefaultValues(&defaults)

	return func(dst cfg.Cloneable) error {
		r, ok := dst.(*ConfigWrapper)
		if !ok {
			return errors.New("dst is not a Loki ConfigWrapper")
		}

		// If nobody has defined any frontend address or scheduler address
		// we can default to using the query scheduler ring for scheduler discovery.
		if r.Worker.FrontendAddress == "" &&
			r.Worker.SchedulerAddress == "" &&
			r.Frontend.FrontendV2.SchedulerAddress == "" {
			r.QueryScheduler.UseSchedulerRing = true
		}

		applyPathPrefixDefaults(r, defaults)
		if err := applyIngesterRingConfig(r, &defaults); err != nil {
			return err
		}
		applyMemberlistConfig(r)

		if err := applyStorageConfig(r, &defaults); err != nil {
			return err
		}

		if len(r.SchemaConfig.Configs) > 0 && loki_storage.UsingBoltdbShipper(r.SchemaConfig.Configs) {
			betterBoltdbShipperDefaults(r, &defaults)
		}

		applyFIFOCacheConfig(r)

		return nil
	}
}

// applyIngesterRingConfig will use whatever config is setup for the ingester ring and use it everywhere else
// we have a ring configured. The reason for centralizing on the ingester ring as this is been set in basically
// all of our provided config files for all of time, usually set to `inmemory` for all the single binary Loki's
// and is the most central ring config for Loki.
//
// If the ingester ring has its interface names sets to a value equal to the default (["eth0", en0"]), it will try to append
// the loopback interface at the end of it.
func applyIngesterRingConfig(r *ConfigWrapper, defaults *ConfigWrapper) error {
	if reflect.DeepEqual(r.Ingester.LifecyclerConfig.InfNames, defaults.Ingester.LifecyclerConfig.InfNames) {
		appendLoopbackInterface(r)
	}

	lc := r.Ingester.LifecyclerConfig
	rc := r.Ingester.LifecyclerConfig.RingConfig
	s := rc.KVStore.Store
	sc := r.Ingester.LifecyclerConfig.RingConfig.KVStore.StoreConfig

	f, err := tokensFile(r, "ingester.tokens")
	if err != nil {
		return err
	}
	r.Ingester.LifecyclerConfig.TokensFilePath = f

	// This gets ugly because we use a separate struct for configuring each ring...

	// Distributor
	r.Distributor.DistributorRing.HeartbeatTimeout = rc.HeartbeatTimeout
	r.Distributor.DistributorRing.HeartbeatPeriod = lc.HeartbeatPeriod
	r.Distributor.DistributorRing.InstancePort = lc.Port
	r.Distributor.DistributorRing.InstanceAddr = lc.Addr
	r.Distributor.DistributorRing.InstanceID = lc.ID
	r.Distributor.DistributorRing.InstanceInterfaceNames = lc.InfNames
	r.Distributor.DistributorRing.KVStore.Store = s
	r.Distributor.DistributorRing.KVStore.StoreConfig = sc

	// Ruler
	r.Ruler.Ring.HeartbeatTimeout = rc.HeartbeatTimeout
	r.Ruler.Ring.HeartbeatPeriod = lc.HeartbeatPeriod
	r.Ruler.Ring.InstancePort = lc.Port
	r.Ruler.Ring.InstanceAddr = lc.Addr
	r.Ruler.Ring.InstanceID = lc.ID
	r.Ruler.Ring.InstanceInterfaceNames = lc.InfNames
	r.Ruler.Ring.NumTokens = lc.NumTokens
	r.Ruler.Ring.KVStore.Store = s
	r.Ruler.Ring.KVStore.StoreConfig = sc

	// Query Scheduler
	r.QueryScheduler.SchedulerRing.HeartbeatTimeout = rc.HeartbeatTimeout
	r.QueryScheduler.SchedulerRing.HeartbeatPeriod = lc.HeartbeatPeriod
	r.QueryScheduler.SchedulerRing.InstancePort = lc.Port
	r.QueryScheduler.SchedulerRing.InstanceAddr = lc.Addr
	r.QueryScheduler.SchedulerRing.InstanceID = lc.ID
	r.QueryScheduler.SchedulerRing.InstanceInterfaceNames = lc.InfNames
	r.QueryScheduler.SchedulerRing.InstanceZone = lc.Zone
	r.QueryScheduler.SchedulerRing.ZoneAwarenessEnabled = rc.ZoneAwarenessEnabled
	r.QueryScheduler.SchedulerRing.KVStore.Store = s
	r.QueryScheduler.SchedulerRing.KVStore.StoreConfig = sc
	f, err = tokensFile(r, "scheduler.tokens")
	if err != nil {
		return err
	}
	r.QueryScheduler.SchedulerRing.TokensFilePath = f

	// Compactor
	r.CompactorConfig.CompactorRing.HeartbeatTimeout = rc.HeartbeatTimeout
	r.CompactorConfig.CompactorRing.HeartbeatPeriod = lc.HeartbeatPeriod
	r.CompactorConfig.CompactorRing.InstancePort = lc.Port
	r.CompactorConfig.CompactorRing.InstanceAddr = lc.Addr
	r.CompactorConfig.CompactorRing.InstanceID = lc.ID
	r.CompactorConfig.CompactorRing.InstanceInterfaceNames = lc.InfNames
	r.CompactorConfig.CompactorRing.InstanceZone = lc.Zone
	r.CompactorConfig.CompactorRing.ZoneAwarenessEnabled = rc.ZoneAwarenessEnabled
	r.CompactorConfig.CompactorRing.KVStore.Store = s
	r.CompactorConfig.CompactorRing.KVStore.StoreConfig = sc
	f, err = tokensFile(r, "compactor.tokens")
	if err != nil {
		return err
	}
	r.CompactorConfig.CompactorRing.TokensFilePath = f
	return nil
}

// tokensFile will create a tokens file with the provided name in the common config /tokens directory
// if and only if:
// * the common config persist_tokens == true
// * the common config path_prefix is defined.
func tokensFile(cfg *ConfigWrapper, file string) (string, error) {
	if !cfg.Common.PersistTokens {
		return "", nil
	}
	if cfg.Common.PathPrefix == "" {
		return "", errors.New("if persist_tokens is true, path_prefix MUST be defined")
	}

	return cfg.Common.PathPrefix + "/" + file, nil
}

func applyPathPrefixDefaults(r *ConfigWrapper, defaults ConfigWrapper) {
	if r.Common.PathPrefix != "" {
		prefix := strings.TrimSuffix(r.Common.PathPrefix, "/")

		if r.Ruler.RulePath == defaults.Ruler.RulePath {
			r.Ruler.RulePath = fmt.Sprintf("%s/rules", prefix)
		}

		if r.Ingester.WAL.Dir == defaults.Ingester.WAL.Dir {
			r.Ingester.WAL.Dir = fmt.Sprintf("%s/wal", prefix)
		}

		if r.CompactorConfig.WorkingDirectory == defaults.CompactorConfig.WorkingDirectory {
			r.CompactorConfig.WorkingDirectory = fmt.Sprintf("%s/compactor", prefix)
		}
	}
}

func appendLoopbackInterface(r *ConfigWrapper) {
	if loopbackIface, err := loki_net.LoopbackInterfaceName(); err == nil {
		r.Ingester.LifecyclerConfig.InfNames = append(r.Ingester.LifecyclerConfig.InfNames, loopbackIface)
		r.Config.Frontend.FrontendV2.InfNames = append(r.Config.Frontend.FrontendV2.InfNames, loopbackIface)
	}
}

// applyMemberlistConfig will change the default ingester, distributor, ruler, and query scheduler ring configurations to use memberlist
// if the -memberlist.join_members config is provided. The idea here is that if a user explicitly configured the
// memberlist configuration section, they probably want to be using memberlist for all their ring configurations.
// Since a user can still explicitly override a specific ring configuration (for example, use consul for the distributor),
// it seems harmless to take a guess at better defaults here.
func applyMemberlistConfig(r *ConfigWrapper) {
	if len(r.MemberlistKV.JoinMembers) > 0 {
		r.Ingester.LifecyclerConfig.RingConfig.KVStore.Store = memberlistStr
		r.Distributor.DistributorRing.KVStore.Store = memberlistStr
		r.Ruler.Ring.KVStore.Store = memberlistStr
		r.QueryScheduler.SchedulerRing.KVStore.Store = memberlistStr
		r.CompactorConfig.CompactorRing.KVStore.Store = memberlistStr
	}
}

var ErrTooManyStorageConfigs = errors.New("too many storage configs provided in the common config, please only define one storage backend")

// applyStorageConfig will attempt to apply a common storage config for either
// s3, gcs, azure, or swift to all the places we create a storage client.
// If any specific configs for an object storage client have been provided elsewhere in the
// configuration file, applyStorageConfig will not override them.
// If multiple storage configurations are provided, applyStorageConfig will return an error
func applyStorageConfig(cfg, defaults *ConfigWrapper) error {
	var applyConfig func(*ConfigWrapper)

	//only one config is allowed
	configsFound := 0

	if !reflect.DeepEqual(cfg.Common.Storage.Azure, defaults.StorageConfig.AzureStorageConfig) {
		configsFound++

		applyConfig = func(r *ConfigWrapper) {
			r.Ruler.StoreConfig.Type = "azure"
			r.Ruler.StoreConfig.Azure = r.Common.Storage.Azure.ToCortexAzureConfig()
			r.StorageConfig.AzureStorageConfig = r.Common.Storage.Azure
			r.CompactorConfig.SharedStoreType = chunk_storage.StorageTypeAzure
		}
	}

	if !reflect.DeepEqual(cfg.Common.Storage.FSConfig, defaults.StorageConfig.FSConfig) {
		configsFound++

		applyConfig = func(r *ConfigWrapper) {
			r.Ruler.StoreConfig.Type = "local"
			r.Ruler.StoreConfig.Local = r.Common.Storage.FSConfig.ToCortexLocalConfig()
			r.StorageConfig.FSConfig = r.Common.Storage.FSConfig
			r.CompactorConfig.SharedStoreType = chunk_storage.StorageTypeFileSystem
		}
	}

	if !reflect.DeepEqual(cfg.Common.Storage.GCS, defaults.StorageConfig.GCSConfig) {
		configsFound++

		applyConfig = func(r *ConfigWrapper) {
			r.Ruler.StoreConfig.Type = "gcs"
			r.Ruler.StoreConfig.GCS = r.Common.Storage.GCS.ToCortexGCSConfig()
			r.StorageConfig.GCSConfig = r.Common.Storage.GCS
			r.CompactorConfig.SharedStoreType = chunk_storage.StorageTypeGCS
		}
	}

	if !reflect.DeepEqual(cfg.Common.Storage.S3, defaults.StorageConfig.AWSStorageConfig.S3Config) {
		configsFound++

		applyConfig = func(r *ConfigWrapper) {
			r.Ruler.StoreConfig.Type = "s3"
			r.Ruler.StoreConfig.S3 = r.Common.Storage.S3.ToCortexS3Config()
			r.StorageConfig.AWSStorageConfig.S3Config = r.Common.Storage.S3
			r.CompactorConfig.SharedStoreType = chunk_storage.StorageTypeS3
		}
	}

	if !reflect.DeepEqual(cfg.Common.Storage.Swift, defaults.StorageConfig.Swift) {
		configsFound++

		applyConfig = func(r *ConfigWrapper) {
			r.Ruler.StoreConfig.Type = "swift"
			r.Ruler.StoreConfig.Swift = r.Common.Storage.Swift.ToCortexSwiftConfig()
			r.StorageConfig.Swift = r.Common.Storage.Swift
			r.CompactorConfig.SharedStoreType = chunk_storage.StorageTypeSwift
		}
	}

	if configsFound > 1 {
		return ErrTooManyStorageConfigs
	}

	if applyConfig != nil {
		applyConfig(cfg)
	}

	return nil
}

func betterBoltdbShipperDefaults(cfg, defaults *ConfigWrapper) {
	currentSchemaIdx := loki_storage.ActivePeriodConfig(cfg.SchemaConfig.Configs)
	currentSchema := cfg.SchemaConfig.Configs[currentSchemaIdx]

	if cfg.StorageConfig.BoltDBShipperConfig.SharedStoreType == defaults.StorageConfig.BoltDBShipperConfig.SharedStoreType {
		cfg.StorageConfig.BoltDBShipperConfig.SharedStoreType = currentSchema.ObjectType
	}

	if cfg.CompactorConfig.SharedStoreType == defaults.CompactorConfig.SharedStoreType {
		cfg.CompactorConfig.SharedStoreType = currentSchema.ObjectType
	}

	if cfg.Common.PathPrefix != "" {
		prefix := strings.TrimSuffix(cfg.Common.PathPrefix, "/")

		if cfg.StorageConfig.BoltDBShipperConfig.ActiveIndexDirectory == "" {
			cfg.StorageConfig.BoltDBShipperConfig.ActiveIndexDirectory = fmt.Sprintf("%s/boltdb-shipper-active", prefix)
		}

		if cfg.StorageConfig.BoltDBShipperConfig.CacheLocation == "" {
			cfg.StorageConfig.BoltDBShipperConfig.CacheLocation = fmt.Sprintf("%s/boltdb-shipper-cache", prefix)
		}
	}
}

// applyFIFOCacheConfig turns on FIFO cache for the chunk store and for the query range results,
// but only if no other cache storage is configured (redis or memcache).
//
// This behavior is only applied for the chunk store cache and for the query range results cache
// (i.e: not applicable for the index queries cache or for the write dedupe cache).
func applyFIFOCacheConfig(r *ConfigWrapper) {
	chunkCacheConfig := r.ChunkStoreConfig.ChunkCacheConfig
	if !cache.IsRedisSet(chunkCacheConfig) && !cache.IsMemcacheSet(chunkCacheConfig) {
		r.ChunkStoreConfig.ChunkCacheConfig.EnableFifoCache = true
	}

	resultsCacheConfig := r.QueryRange.ResultsCacheConfig.CacheConfig
	if !isRedisSet(resultsCacheConfig) && !isMemcacheSet(resultsCacheConfig) {
		r.QueryRange.ResultsCacheConfig.CacheConfig.EnableFifoCache = true
	}
}

// isRedisSet is a duplicate of cache.IsRedisSet.
//
// We had to duplicate this implementation because we have code relying on
// loki/pkg/storage/chunk/cache and cortex/pkg/chunk/cache at the same time.
func isRedisSet(cfg cortexcache.Config) bool {
	return cfg.Redis.Endpoint != ""
}

// isMemcacheSet is a duplicate of cache.IsMemcacheSet.
//
// We had to duplicate this implementation because we have code relying on
// loki/pkg/storage/chunk/cache and cortex/pkg/chunk/cache at the same time.
func isMemcacheSet(cfg cortexcache.Config) bool {
	return cfg.MemcacheClient.Addresses != "" || cfg.MemcacheClient.Host != ""
}
