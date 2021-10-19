package loki

import (
	"flag"
	"fmt"
	"reflect"

	"github.com/grafana/dskit/flagext"
	"github.com/pkg/errors"

	"github.com/grafana/loki/pkg/storage/chunk/storage"
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

		applyMemberlistConfig(r)
		applyStorageConfig(r, &defaults)

		return nil
	}
}

// applyMemberlistConfig will change the default ingester, distributor, and ruler ring configurations to use memberlist
// if the -memberlist.join_members config is provided. The idea here is that if a user explicitly configured the
// memberlist configuration section, they probably want to be using memberlist for all their ring configurations.
// Since a user can still explicitly override a specific ring configuration (for example, use consul for the distributor),
// it seems harmless to take a guess at better defaults here.
func applyMemberlistConfig(r *ConfigWrapper) {
	if len(r.MemberlistKV.JoinMembers) > 0 {
		r.Ingester.LifecyclerConfig.RingConfig.KVStore.Store = memberlistStr
		r.Distributor.DistributorRing.KVStore.Store = memberlistStr
		r.Ruler.Ring.KVStore.Store = memberlistStr
	}
}

// applyStorageConfig will attempt to apply a common storage config for either
// s3, gcs, azure, or swift to all the places we create a storage client.
// If any specific configs for an object storage client have been provided elsewhere in the
// configuration file, applyStorageConfig will not override them.
// If multiple storage configurations are provided, applyStorageConfig will apply
// all of them, and will set the value for the Ruler's StoreConfig `type` to the
// last one (alphabetically) that was defined.
func applyStorageConfig(cfg, defaults *ConfigWrapper) {
	rulerStoreConfigsToApply := make([]func(*ConfigWrapper), 0, 4)
	chunkStorageConfigsToApply := make([]func(*ConfigWrapper), 0, 4)

	if cfg.Common.Storage.Azure != nil {
		rulerStoreConfigsToApply = append(rulerStoreConfigsToApply, func(r *ConfigWrapper) {
			r.Ruler.StoreConfig.Type = "azure"
			r.Ruler.StoreConfig.Azure = r.Common.Storage.Azure.ToCortexAzureConfig()
		})

		chunkStorageConfigsToApply = append(chunkStorageConfigsToApply, func(r *ConfigWrapper) {
			r.StorageConfig.AzureStorageConfig = *r.Common.Storage.Azure
			r.CompactorConfig.SharedStoreType = storage.StorageTypeAzure
		})
	}

	if cfg.Common.Storage.GCS != nil {
		rulerStoreConfigsToApply = append(rulerStoreConfigsToApply, func(r *ConfigWrapper) {
			r.Ruler.StoreConfig.Type = "gcs"
			r.Ruler.StoreConfig.GCS = r.Common.Storage.GCS.ToCortexGCSConfig()
		})

		chunkStorageConfigsToApply = append(chunkStorageConfigsToApply, func(r *ConfigWrapper) {
			r.StorageConfig.GCSConfig = *r.Common.Storage.GCS
			r.CompactorConfig.SharedStoreType = storage.StorageTypeGCS
		})
	}

	if cfg.Common.Storage.FSConfig != nil {
		rulerStoreConfigsToApply = append(rulerStoreConfigsToApply, func(r *ConfigWrapper) {
			r.Ruler.StoreConfig.Type = "local"
			r.Ruler.StoreConfig.Local = r.Common.Storage.FSConfig.ToCortexLocalConfig()
		})

		chunkStorageConfigsToApply = append(chunkStorageConfigsToApply, func(r *ConfigWrapper) {
			r.StorageConfig.FSConfig = *r.Common.Storage.FSConfig
			r.CompactorConfig.SharedStoreType = storage.StorageTypeFileSystem
		})
	}

	if cfg.Common.Storage.S3 != nil {
		rulerStoreConfigsToApply = append(rulerStoreConfigsToApply, func(r *ConfigWrapper) {
			r.Ruler.StoreConfig.Type = "s3"
			r.Ruler.StoreConfig.S3 = r.Common.Storage.S3.ToCortexS3Config()
		})

		chunkStorageConfigsToApply = append(chunkStorageConfigsToApply, func(r *ConfigWrapper) {
			r.StorageConfig.AWSStorageConfig.S3Config = *r.Common.Storage.S3
			r.CompactorConfig.SharedStoreType = storage.StorageTypeS3
		})
	}

	if cfg.Common.Storage.Swift != nil {
		rulerStoreConfigsToApply = append(rulerStoreConfigsToApply, func(r *ConfigWrapper) {
			r.Ruler.StoreConfig.Type = "swift"
			r.Ruler.StoreConfig.Swift = r.Common.Storage.Swift.ToCortexSwiftConfig()
		})

		chunkStorageConfigsToApply = append(chunkStorageConfigsToApply, func(r *ConfigWrapper) {
			r.StorageConfig.Swift = *r.Common.Storage.Swift
			r.CompactorConfig.SharedStoreType = storage.StorageTypeSwift
		})
	}

	// store change funcs in slices and apply all at once, because once we change the
	// config we can no longer compare it to the default, this allows us to only
	// do that comparison once
	applyRulerStoreConfigs(cfg, defaults, rulerStoreConfigsToApply)
	applyChunkStorageConfigs(cfg, defaults, chunkStorageConfigsToApply)
}

func applyRulerStoreConfigs(cfg, defaults *ConfigWrapper, apply []func(*ConfigWrapper)) {
	if reflect.DeepEqual(cfg.Ruler.StoreConfig, defaults.Ruler.StoreConfig) {
		for _, ap := range apply {
			ap(cfg)
		}
	}
}

func applyChunkStorageConfigs(cfg, defaults *ConfigWrapper, apply []func(*ConfigWrapper)) {
	if reflect.DeepEqual(cfg.StorageConfig, defaults.StorageConfig) {
		for _, ap := range apply {
			ap(cfg)
		}
	}
}
