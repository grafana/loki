package loki

import (
	"flag"
	"fmt"
	"reflect"

	"github.com/grafana/dskit/flagext"
	"github.com/pkg/errors"

	loki_storage "github.com/grafana/loki/pkg/storage"
	chunk_storage "github.com/grafana/loki/pkg/storage/chunk/storage"

	"github.com/grafana/loki/pkg/util/cfg"
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

		// Apply all our custom logic here to set values in the Loki config from values in the common config
		if r.Common.PathPrefix != "" {
			if r.Ruler.RulePath == defaults.Ruler.RulePath {
				r.Ruler.RulePath = fmt.Sprintf("%s/rules", r.Common.PathPrefix)
			}

			if r.Ingester.WAL.Dir == defaults.Ingester.WAL.Dir {
				r.Ingester.WAL.Dir = fmt.Sprintf("%s/wal", r.Common.PathPrefix)
			}
		}

		// If nobody has defined any frontend address or scheduler address
		// we can default to using the query scheduler ring for scheduler discovery.
		if r.Worker.FrontendAddress == "" &&
			r.Worker.SchedulerAddress == "" &&
			r.Frontend.FrontendV2.SchedulerAddress == "" {
			r.QueryScheduler.UseSchedulerRing = true
		}

		applyIngesterRingConfig(r)
		applyMemberlistConfig(r)
		err := applyStorageConfig(r, &defaults)
		if err != nil {
			return err
		}

		if len(r.SchemaConfig.Configs) > 0 && loki_storage.UsingBoltdbShipper(r.SchemaConfig.Configs) {
			betterBoltdbShipperDefaults(r, &defaults)
		}

		return nil
	}
}

// applyIngesterRingConfig will use whatever config is setup for the ingester ring and use it everywhere else
// we have a ring configured. The reason for centralizing on the ingester ring as this is been set in basically
// all of our provided config files for all of time, usually set to `inmemory` for all the single binary Loki's
// and is the most central ring config for Loki.
func applyIngesterRingConfig(r *ConfigWrapper) {
	lc := r.Ingester.LifecyclerConfig
	rc := r.Ingester.LifecyclerConfig.RingConfig
	s := rc.KVStore.Store
	sc := r.Ingester.LifecyclerConfig.RingConfig.KVStore.StoreConfig

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
	r.QueryScheduler.SchedulerRing.TokensFilePath = lc.TokensFilePath
	r.QueryScheduler.SchedulerRing.KVStore.Store = s
	r.QueryScheduler.SchedulerRing.KVStore.StoreConfig = sc
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
	}
}

var ErrTooManyStorageConfigs = errors.New("too many storage configs provided in the common config, please only define one storage backend")

// applyStorageConfig will attempt to apply a common storage config for either
// s3, gcs, azure, or swift to all the places we create a storage client.
// If any specific configs for an object storage client have been provided elsewhere in the
// configuration file, applyStorageConfig will not override them.
// If multiple storage configurations are provided, applyStorageConfig will return an error
func applyStorageConfig(cfg, defaults *ConfigWrapper) error {
	var applyRulerStoreConfig func(*ConfigWrapper)
	var applyChunkStorageConfig func(*ConfigWrapper)

	//only one config is allowed
	configsFound := 0

	if cfg.Common.Storage.Azure != nil {
		configsFound++

		applyRulerStoreConfig = func(r *ConfigWrapper) {
			r.Ruler.StoreConfig.Type = "azure"
			r.Ruler.StoreConfig.Azure = r.Common.Storage.Azure.ToCortexAzureConfig()
		}

		applyChunkStorageConfig = func(r *ConfigWrapper) {
			r.StorageConfig.AzureStorageConfig = *r.Common.Storage.Azure
			r.CompactorConfig.SharedStoreType = chunk_storage.StorageTypeAzure
		}
	}

	if cfg.Common.Storage.FSConfig != nil {
		configsFound++

		applyRulerStoreConfig = func(r *ConfigWrapper) {
			r.Ruler.StoreConfig.Type = "local"
			r.Ruler.StoreConfig.Local = r.Common.Storage.FSConfig.ToCortexLocalConfig()
		}

		applyChunkStorageConfig = func(r *ConfigWrapper) {
			r.StorageConfig.FSConfig = *r.Common.Storage.FSConfig
			r.CompactorConfig.SharedStoreType = chunk_storage.StorageTypeFileSystem
		}
	}

	if cfg.Common.Storage.GCS != nil {
		configsFound++

		applyRulerStoreConfig = func(r *ConfigWrapper) {
			r.Ruler.StoreConfig.Type = "gcs"
			r.Ruler.StoreConfig.GCS = r.Common.Storage.GCS.ToCortexGCSConfig()
		}

		applyChunkStorageConfig = func(r *ConfigWrapper) {
			r.StorageConfig.GCSConfig = *r.Common.Storage.GCS
			r.CompactorConfig.SharedStoreType = chunk_storage.StorageTypeGCS
		}
	}

	if cfg.Common.Storage.S3 != nil {
		configsFound++

		applyRulerStoreConfig = func(r *ConfigWrapper) {
			r.Ruler.StoreConfig.Type = "s3"
			r.Ruler.StoreConfig.S3 = r.Common.Storage.S3.ToCortexS3Config()
		}

		applyChunkStorageConfig = func(r *ConfigWrapper) {
			r.StorageConfig.AWSStorageConfig.S3Config = *r.Common.Storage.S3
			r.CompactorConfig.SharedStoreType = chunk_storage.StorageTypeS3
		}
	}

	if cfg.Common.Storage.Swift != nil {
		configsFound++

		applyRulerStoreConfig = func(r *ConfigWrapper) {
			r.Ruler.StoreConfig.Type = "swift"
			r.Ruler.StoreConfig.Swift = r.Common.Storage.Swift.ToCortexSwiftConfig()
		}

		applyChunkStorageConfig = func(r *ConfigWrapper) {
			r.StorageConfig.Swift = *r.Common.Storage.Swift
			r.CompactorConfig.SharedStoreType = chunk_storage.StorageTypeSwift
		}
	}

	if configsFound > 1 {
		return ErrTooManyStorageConfigs
	}

	applyRulerStoreConfigs(cfg, defaults, applyRulerStoreConfig)
	applyChunkStorageConfigs(cfg, defaults, applyChunkStorageConfig)

	return nil
}

func applyRulerStoreConfigs(cfg, defaults *ConfigWrapper, apply func(*ConfigWrapper)) {
	if apply != nil && reflect.DeepEqual(cfg.Ruler.StoreConfig, defaults.Ruler.StoreConfig) {
		apply(cfg)
	}
}

func applyChunkStorageConfigs(cfg, defaults *ConfigWrapper, apply func(*ConfigWrapper)) {
	if apply != nil && reflect.DeepEqual(cfg.StorageConfig, defaults.StorageConfig) {
		apply(cfg)
	}
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
}
