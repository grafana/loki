package loki

import (
	"flag"
	"fmt"
	"reflect"
	"strings"
	"time"

	cortexcache "github.com/cortexproject/cortex/pkg/chunk/cache"
	"github.com/cortexproject/cortex/pkg/ruler/rulestore/local"
	"github.com/grafana/dskit/flagext"
	"github.com/pkg/errors"

	"github.com/grafana/loki/pkg/loki/common"
	"github.com/grafana/loki/pkg/storage/chunk/cache"
	"github.com/grafana/loki/pkg/util"
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

		// If nobody has defined any frontend address, scheduler address, or downstream url
		// we can default to using the query scheduler ring for scheduler discovery.
		if r.Worker.FrontendAddress == "" &&
			r.Worker.SchedulerAddress == "" &&
			r.Frontend.DownstreamURL == "" &&
			r.Frontend.FrontendV2.SchedulerAddress == "" {
			r.QueryScheduler.UseSchedulerRing = true
		}

		applyPathPrefixDefaults(r, &defaults)

		applyDynamicRingConfigs(r, &defaults)

		appendLoopbackInterface(r, &defaults)

		if err := applyTokensFilePath(r); err != nil {
			return err
		}

		if err := applyStorageConfig(r, &defaults); err != nil {
			return err
		}

		if len(r.SchemaConfig.Configs) > 0 && loki_storage.UsingBoltdbShipper(r.SchemaConfig.Configs) {
			betterBoltdbShipperDefaults(r, &defaults)
		}

		applyFIFOCacheConfig(r)
		applyIngesterFinalSleep(r)
		applyIngesterReplicationFactor(r)
		applyChunkRetain(r, &defaults)

		return nil
	}
}

// applyDynamicRingConfigs checks the current config and, depending on the given values, reuse ring configs accordingly.
//
// 1. Gives preference to any explicit ring config set. For instance, if the user explicitly configures Distributor's ring,
// that config will prevail. This rule is enforced by the fact that the config file and command line args are parsed
// again after the dynamic config has been applied, so will take higher precedence.
// 2. If no explicit ring config is set, use the common ring configured if provided.
// 3. If no common ring was provided, use the memberlist config if provided.
// 4. If no common ring or memberlist were provided, use the ingester's ring configuration.

// When using the ingester or common ring config, the loopback interface will be appended to the end of
// the list of default interface names
func applyDynamicRingConfigs(r, defaults *ConfigWrapper) {
	if !reflect.DeepEqual(r.Common.Ring, defaults.Common.Ring) {
		// common ring is provided, use that for all rings, merging with
		// any specific configs provided for each ring
		applyConfigToRings(r, defaults, r.Common.Ring, true)
		return
	}

	// common ring not set, so use memberlist for all rings if set
	if len(r.MemberlistKV.JoinMembers) > 0 {
		applyMemberlistConfig(r)
	} else {
		// neither common ring nor memberlist set, use ingester ring configuration for all rings
		// that have not been configured. Don't merge any ingester ring configurations for rings
		// that deviate from the default in any way.
		ingesterRingCfg := util.CortexLifecyclerConfigToRingConfig(r.Ingester.LifecyclerConfig)
		applyConfigToRings(r, defaults, ingesterRingCfg, false)
	}
}

//applyConfigToRings will reuse a given RingConfig everywhere else we have a ring configured.
//`mergeWithExisting` will be true when applying the common config, false when applying the ingester
//config. This decision was made since the ingester ring copying behavior is likely to be less intuitive,
//and was added as a stop-gap to prevent the new rings in 2.4 from breaking existing configs before 2.4 that only had an ingester
//ring defined. When `mergeWithExisting` is false, we will not apply any of the ring config to a ring that has
//any deviations from defaults. When mergeWithExisting is true, the ring config is overlaid on top of any specified
//derivations, with the derivations taking precedence.
func applyConfigToRings(r, defaults *ConfigWrapper, rc util.RingConfig, mergeWithExisting bool) {
	//Ingester - mergeWithExisting is false when applying the ingester config, and we only want to
	//change ingester ring values when applying the common config, so there's no need for the DeepEqual
	//check here.
	if mergeWithExisting {
		r.Ingester.LifecyclerConfig.RingConfig.KVStore = rc.KVStore
		r.Ingester.LifecyclerConfig.HeartbeatPeriod = rc.HeartbeatPeriod
		r.Ingester.LifecyclerConfig.RingConfig.HeartbeatTimeout = rc.HeartbeatTimeout
		r.Ingester.LifecyclerConfig.TokensFilePath = rc.TokensFilePath
		r.Ingester.LifecyclerConfig.RingConfig.ZoneAwarenessEnabled = rc.ZoneAwarenessEnabled
		r.Ingester.LifecyclerConfig.ID = rc.InstanceID
		r.Ingester.LifecyclerConfig.InfNames = rc.InstanceInterfaceNames
		r.Ingester.LifecyclerConfig.Port = rc.InstancePort
		r.Ingester.LifecyclerConfig.Addr = rc.InstanceAddr
		r.Ingester.LifecyclerConfig.Zone = rc.InstanceZone
		r.Ingester.LifecyclerConfig.ListenPort = rc.ListenPort
		r.Ingester.LifecyclerConfig.ObservePeriod = rc.ObservePeriod
		r.Ingester.LifecyclerConfig.RingConfig.ReplicationFactor = r.Common.ReplicationFactor
	}

	// Distributor
	if mergeWithExisting || reflect.DeepEqual(r.Distributor.DistributorRing, defaults.Distributor.DistributorRing) {
		r.Distributor.DistributorRing.HeartbeatTimeout = rc.HeartbeatTimeout
		r.Distributor.DistributorRing.HeartbeatPeriod = rc.HeartbeatPeriod
		r.Distributor.DistributorRing.InstancePort = rc.InstancePort
		r.Distributor.DistributorRing.InstanceAddr = rc.InstanceAddr
		r.Distributor.DistributorRing.InstanceID = rc.InstanceID
		r.Distributor.DistributorRing.InstanceInterfaceNames = rc.InstanceInterfaceNames
		r.Distributor.DistributorRing.KVStore = rc.KVStore
	}

	// Ruler
	if mergeWithExisting || reflect.DeepEqual(r.Ruler.Ring, defaults.Ruler.Ring) {
		r.Ruler.Ring.HeartbeatTimeout = rc.HeartbeatTimeout
		r.Ruler.Ring.HeartbeatPeriod = rc.HeartbeatPeriod
		r.Ruler.Ring.InstancePort = rc.InstancePort
		r.Ruler.Ring.InstanceAddr = rc.InstanceAddr
		r.Ruler.Ring.InstanceID = rc.InstanceID
		r.Ruler.Ring.InstanceInterfaceNames = rc.InstanceInterfaceNames
		r.Ruler.Ring.KVStore = rc.KVStore

		// TODO(tjw): temporary fix until dskit is updated: https://github.com/grafana/dskit/pull/101
		// The ruler's default ring key is "ring", so if if registers under the same common prefix
		// as the ingester, queriers will try to query it, resulting in failed queries.
		r.Ruler.Ring.KVStore.Prefix = "/rulers"
	}

	// Query Scheduler
	if mergeWithExisting || reflect.DeepEqual(r.QueryScheduler.SchedulerRing, defaults.QueryScheduler.SchedulerRing) {
		r.QueryScheduler.SchedulerRing.HeartbeatTimeout = rc.HeartbeatTimeout
		r.QueryScheduler.SchedulerRing.HeartbeatPeriod = rc.HeartbeatPeriod
		r.QueryScheduler.SchedulerRing.InstancePort = rc.InstancePort
		r.QueryScheduler.SchedulerRing.InstanceAddr = rc.InstanceAddr
		r.QueryScheduler.SchedulerRing.InstanceID = rc.InstanceID
		r.QueryScheduler.SchedulerRing.InstanceInterfaceNames = rc.InstanceInterfaceNames
		r.QueryScheduler.SchedulerRing.InstanceZone = rc.InstanceZone
		r.QueryScheduler.SchedulerRing.ZoneAwarenessEnabled = rc.ZoneAwarenessEnabled
		r.QueryScheduler.SchedulerRing.KVStore = rc.KVStore
	}

	// Compactor
	if mergeWithExisting || reflect.DeepEqual(r.CompactorConfig.CompactorRing, defaults.CompactorConfig.CompactorRing) {
		r.CompactorConfig.CompactorRing.HeartbeatTimeout = rc.HeartbeatTimeout
		r.CompactorConfig.CompactorRing.HeartbeatPeriod = rc.HeartbeatPeriod
		r.CompactorConfig.CompactorRing.InstancePort = rc.InstancePort
		r.CompactorConfig.CompactorRing.InstanceAddr = rc.InstanceAddr
		r.CompactorConfig.CompactorRing.InstanceID = rc.InstanceID
		r.CompactorConfig.CompactorRing.InstanceInterfaceNames = rc.InstanceInterfaceNames
		r.CompactorConfig.CompactorRing.InstanceZone = rc.InstanceZone
		r.CompactorConfig.CompactorRing.ZoneAwarenessEnabled = rc.ZoneAwarenessEnabled
		r.CompactorConfig.CompactorRing.KVStore = rc.KVStore
	}
}

func applyTokensFilePath(cfg *ConfigWrapper) error {
	// Ingester
	f, err := tokensFile(cfg, "ingester.tokens")
	if err != nil {
		return err
	}
	cfg.Ingester.LifecyclerConfig.TokensFilePath = f

	// Compactor
	f, err = tokensFile(cfg, "compactor.tokens")
	if err != nil {
		return err
	}
	cfg.CompactorConfig.CompactorRing.TokensFilePath = f

	// Query Scheduler
	f, err = tokensFile(cfg, "scheduler.tokens")
	if err != nil {
		return err
	}
	cfg.QueryScheduler.SchedulerRing.TokensFilePath = f

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

func applyPathPrefixDefaults(r, defaults *ConfigWrapper) {
	if r.Common.PathPrefix != "" {
		prefix := strings.TrimSuffix(r.Common.PathPrefix, "/")

		if r.Ruler.RulePath == defaults.Ruler.RulePath {
			r.Ruler.RulePath = fmt.Sprintf("%s/rules-temp", prefix)
		}

		if r.Ingester.WAL.Dir == defaults.Ingester.WAL.Dir {
			r.Ingester.WAL.Dir = fmt.Sprintf("%s/wal", prefix)
		}

		if r.CompactorConfig.WorkingDirectory == defaults.CompactorConfig.WorkingDirectory {
			r.CompactorConfig.WorkingDirectory = fmt.Sprintf("%s/compactor", prefix)
		}
	}
}

// appendLoopbackInterface will append the loopback interface to the interface names used for the ingester ring,
// v2 frontend, and common ring config unless an explicit list of names was provided.
func appendLoopbackInterface(cfg, defaults *ConfigWrapper) {
	loopbackIface, err := loki_net.LoopbackInterfaceName()
	if err != nil {
		return
	}

	if reflect.DeepEqual(cfg.Ingester.LifecyclerConfig.InfNames, defaults.Ingester.LifecyclerConfig.InfNames) {
		cfg.Ingester.LifecyclerConfig.InfNames = append(cfg.Ingester.LifecyclerConfig.InfNames, loopbackIface)
	}

	if reflect.DeepEqual(cfg.Frontend.FrontendV2.InfNames, defaults.Frontend.FrontendV2.InfNames) {
		cfg.Frontend.FrontendV2.InfNames = append(cfg.Config.Frontend.FrontendV2.InfNames, loopbackIface)
	}

	if reflect.DeepEqual(cfg.Distributor.DistributorRing.InstanceInterfaceNames, defaults.Distributor.DistributorRing.InstanceInterfaceNames) {
		cfg.Distributor.DistributorRing.InstanceInterfaceNames = append(cfg.Distributor.DistributorRing.InstanceInterfaceNames, loopbackIface)
	}

	if reflect.DeepEqual(cfg.Common.Ring.InstanceInterfaceNames, defaults.Common.Ring.InstanceInterfaceNames) {
		cfg.Common.Ring.InstanceInterfaceNames = append(cfg.Common.Ring.InstanceInterfaceNames, loopbackIface)
	}

	if reflect.DeepEqual(cfg.CompactorConfig.CompactorRing.InstanceInterfaceNames, defaults.CompactorConfig.CompactorRing.InstanceInterfaceNames) {
		cfg.CompactorConfig.CompactorRing.InstanceInterfaceNames = append(cfg.CompactorConfig.CompactorRing.InstanceInterfaceNames, loopbackIface)
	}

	if reflect.DeepEqual(cfg.QueryScheduler.SchedulerRing.InstanceInterfaceNames, defaults.QueryScheduler.SchedulerRing.InstanceInterfaceNames) {
		cfg.QueryScheduler.SchedulerRing.InstanceInterfaceNames = append(cfg.QueryScheduler.SchedulerRing.InstanceInterfaceNames, loopbackIface)
	}

	if reflect.DeepEqual(cfg.Ruler.Ring.InstanceInterfaceNames, defaults.Ruler.Ring.InstanceInterfaceNames) {
		cfg.Ruler.Ring.InstanceInterfaceNames = append(cfg.Ruler.Ring.InstanceInterfaceNames, loopbackIface)
	}
}

// applyMemberlistConfig will change the default ingester, distributor, ruler, and query scheduler ring configurations to use memberlist.
// The idea here is that if a user explicitly configured the memberlist configuration section, they probably want to be using memberlist
// for all their ring configurations. Since a user can still explicitly override a specific ring configuration
// (for example, use consul for the distributor), it seems harmless to take a guess at better defaults here.
func applyMemberlistConfig(r *ConfigWrapper) {
	r.Ingester.LifecyclerConfig.RingConfig.KVStore.Store = memberlistStr
	r.Distributor.DistributorRing.KVStore.Store = memberlistStr
	r.Ruler.Ring.KVStore.Store = memberlistStr
	r.QueryScheduler.SchedulerRing.KVStore.Store = memberlistStr
	r.CompactorConfig.CompactorRing.KVStore.Store = memberlistStr
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

	filesystemDefaults := common.FilesystemConfig{}
	throwaway := flag.NewFlagSet("throwaway", flag.PanicOnError)
	filesystemDefaults.RegisterFlagsWithPrefix("", throwaway)

	if !reflect.DeepEqual(cfg.Common.Storage.FSConfig, filesystemDefaults) {
		configsFound++

		applyConfig = func(r *ConfigWrapper) {
			r.Ruler.StoreConfig.Type = "local"
			r.Ruler.StoreConfig.Local = local.Config{Directory: r.Common.Storage.FSConfig.RulesDirectory}
			r.StorageConfig.FSConfig.Directory = r.Common.Storage.FSConfig.ChunksDirectory
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
		// The query results fifocache is still in Cortex so we couldn't change the flag defaults
		// so instead we will override them here.
		r.QueryRange.ResultsCacheConfig.CacheConfig.Fifocache.MaxSizeBytes = "1GB"
		r.QueryRange.ResultsCacheConfig.CacheConfig.Fifocache.Validity = 1 * time.Hour
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

func applyIngesterFinalSleep(cfg *ConfigWrapper) {
	cfg.Ingester.LifecyclerConfig.FinalSleep = 0 * time.Second
}

func applyIngesterReplicationFactor(cfg *ConfigWrapper) {
	cfg.Ingester.LifecyclerConfig.RingConfig.ReplicationFactor = cfg.Common.ReplicationFactor
}

// applyChunkRetain is used to set chunk retain based on having an index query cache configured
// We retain chunks for at least as long as the index queries cache TTL. When an index entry is
// cached, any chunks flushed after that won't be in the cached entry. To make sure their data is
// available the RetainPeriod keeps them available in the ingesters live data. We want to retain them
// for at least as long as the TTL on the index queries cache.
func applyChunkRetain(cfg, defaults *ConfigWrapper) {
	if !reflect.DeepEqual(cfg.StorageConfig.IndexQueriesCacheConfig, defaults.StorageConfig.IndexQueriesCacheConfig) {
		// Set the retain period to the cache validity plus one minute. One minute is arbitrary but leaves some
		// buffer to make sure the chunks are there until the index entries expire.
		cfg.Ingester.RetainPeriod = cfg.StorageConfig.IndexCacheValidity + 1*time.Minute
	}
}
