package loki

import (
	"flag"
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"time"

	"github.com/grafana/dskit/flagext"
	"github.com/pkg/errors"

	"github.com/grafana/loki/v3/pkg/loki/common"
	"github.com/grafana/loki/v3/pkg/storage/chunk/cache"
	"github.com/grafana/loki/v3/pkg/storage/config"
	"github.com/grafana/loki/v3/pkg/storage/types"
	"github.com/grafana/loki/v3/pkg/util/cfg"
	lokiring "github.com/grafana/loki/v3/pkg/util/ring"

	"github.com/grafana/loki/v3/pkg/ruler/rulestore/local"
	loki_net "github.com/grafana/loki/v3/pkg/util/net"
)

const versionFlag = "version"

// ConfigWrapper is a struct containing the Loki config along with other values that can be set on the command line
// for interacting with the config file or the application directly.
// ConfigWrapper implements cfg.DynamicCloneable, allowing configuration to be dynamically set based
// on the logic in ApplyDynamicConfig, which receives values set in config file
type ConfigWrapper struct {
	Config          `yaml:",inline"`
	printVersion    bool
	VerifyConfig    bool
	PrintConfig     bool
	ListTargets     bool
	LogConfig       bool
	ConfigFile      string
	ConfigExpandEnv bool
}

func PrintVersion(args []string) bool {
	pattern := regexp.MustCompile(`^-+` + versionFlag + `$`)
	for _, a := range args {
		if pattern.MatchString(a) {
			return true
		}
	}
	return false
}

func (c *ConfigWrapper) RegisterFlags(f *flag.FlagSet) {
	f.BoolVar(&c.printVersion, versionFlag, false, "Print this builds version information")
	f.BoolVar(&c.VerifyConfig, "verify-config", false, "Verify config file and exits")
	f.BoolVar(&c.PrintConfig, "print-config-stderr", false, "Dump the entire Loki config object to stderr")
	f.BoolVar(&c.ListTargets, "list-targets", false, "List available targets")
	f.BoolVar(&c.LogConfig, "log-config-reverse-order", false, "Dump the entire Loki config object at Info log "+
		"level with the order reversed, reversing the order makes viewing the entries easier in Grafana.")
	f.StringVar(&c.ConfigFile, "config.file", "config.yaml,config/config.yaml", "configuration file to load, can be a comma separated list of paths, first existing file will be used")
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

		applyInstanceConfigs(r, &defaults)

		applyCommonReplicationFactor(r, &defaults)

		applyDynamicRingConfigs(r, &defaults)

		appendLoopbackInterface(r, &defaults)

		if err := applyTokensFilePath(r); err != nil {
			return err
		}

		if err := applyStorageConfig(r, &defaults); err != nil {
			return err
		}

		if i := lastBoltdbShipperConfig(r.SchemaConfig.Configs); i != len(r.SchemaConfig.Configs) {
			betterBoltdbShipperDefaults(r)
		}

		if i := lastTSDBConfig(r.SchemaConfig.Configs); i != len(r.SchemaConfig.Configs) {
			betterTSDBShipperDefaults(r)
		}

		applyEmbeddedCacheConfig(r)
		applyIngesterFinalSleep(r)
		applyIngesterReplicationFactor(r)
		applyChunkRetain(r, &defaults)
		if err := applyCommonQuerierWorkerGRPCConfig(r, &defaults); err != nil {
			return err
		}

		return nil
	}
}

func lastConfigFor(configs []config.PeriodConfig, predicate func(config.PeriodConfig) bool) int {
	for i := len(configs) - 1; i >= 0; i-- {
		if predicate(configs[i]) {
			return i
		}
	}
	return len(configs)
}

func lastBoltdbShipperConfig(configs []config.PeriodConfig) int {
	return lastConfigFor(configs, func(p config.PeriodConfig) bool {
		return p.IndexType == types.BoltDBShipperType
	})
}

func lastTSDBConfig(configs []config.PeriodConfig) int {
	return lastConfigFor(configs, func(p config.PeriodConfig) bool {
		return p.IndexType == types.TSDBType
	})
}

// applyInstanceConfigs apply to Loki components instance-related configurations under the common
// config section.
//
// The list of components making usage of these instance-related configurations are: compactor's ring,
// ruler's ring, distributor's ring, ingester's ring, query scheduler's ring, and the query frontend.
//
// The list of implement common configurations are:
// - "instance-addr", the address advertised to be used by other components.
// - "instance-interface-names", a list of net interfaces used when looking for addresses.
func applyInstanceConfigs(r, defaults *ConfigWrapper) {
	if !reflect.DeepEqual(r.Common.InstanceAddr, defaults.Common.InstanceAddr) {
		if reflect.DeepEqual(r.Common.Ring.InstanceAddr, defaults.Common.Ring.InstanceAddr) {
			r.Common.Ring.InstanceAddr = r.Common.InstanceAddr
		}
		r.Frontend.FrontendV2.Addr = r.Common.InstanceAddr
		r.IndexGateway.Ring.InstanceAddr = r.Common.InstanceAddr
		r.MemberlistKV.AdvertiseAddr = r.Common.InstanceAddr
	}

	if !reflect.DeepEqual(r.Common.InstanceInterfaceNames, defaults.Common.InstanceInterfaceNames) {
		if reflect.DeepEqual(r.Common.Ring.InstanceInterfaceNames, defaults.Common.Ring.InstanceInterfaceNames) {
			r.Common.Ring.InstanceInterfaceNames = r.Common.InstanceInterfaceNames
		}
		r.Frontend.FrontendV2.InfNames = r.Common.InstanceInterfaceNames
		r.IndexGateway.Ring.InstanceInterfaceNames = r.Common.InstanceInterfaceNames
	}
}

// applyCommonReplicationFactor apply the common replication factor to the Index Gateway and Bloom Gateway rings.
func applyCommonReplicationFactor(r, defaults *ConfigWrapper) {
	if !reflect.DeepEqual(r.Common.ReplicationFactor, defaults.Common.ReplicationFactor) {
		r.IndexGateway.Ring.ReplicationFactor = r.Common.ReplicationFactor
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
//
// When using the ingester or common ring config, the loopback interface will be appended to the end of
// the list of default interface names.
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
		ingesterRingCfg := lokiring.CortexLifecyclerConfigToRingConfig(r.Ingester.LifecyclerConfig)
		applyConfigToRings(r, defaults, ingesterRingCfg, false)
	}
}

// applyConfigToRings will reuse a given RingConfig everywhere else we have a ring configured.
// `mergeWithExisting` will be true when applying the common config, false when applying the ingester
// config. This decision was made since the ingester ring copying behavior is likely to be less intuitive,
// and was added as a stop-gap to prevent the new rings in 2.4 from breaking existing configs before 2.4 that only had an ingester
// ring defined. When `mergeWithExisting` is false, we will not apply any of the ring config to a ring that has
// any deviations from defaults. When mergeWithExisting is true, the ring config is overlaid on top of any specified
// derivations, with the derivations taking precedence.
func applyConfigToRings(r, defaults *ConfigWrapper, rc lokiring.RingConfig, mergeWithExisting bool) {
	// Ingester - mergeWithExisting is false when applying the ingester config, and we only want to
	// change ingester ring values when applying the common config, so there's no need for the DeepEqual
	// check here.
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
	}

	if mergeWithExisting {
		r.IngesterRF1.LifecyclerConfig.RingConfig.KVStore = rc.KVStore
		r.IngesterRF1.LifecyclerConfig.HeartbeatPeriod = rc.HeartbeatPeriod
		r.IngesterRF1.LifecyclerConfig.RingConfig.HeartbeatTimeout = rc.HeartbeatTimeout
		r.IngesterRF1.LifecyclerConfig.TokensFilePath = rc.TokensFilePath
		r.IngesterRF1.LifecyclerConfig.RingConfig.ZoneAwarenessEnabled = rc.ZoneAwarenessEnabled
		r.IngesterRF1.LifecyclerConfig.ID = rc.InstanceID
		r.IngesterRF1.LifecyclerConfig.InfNames = rc.InstanceInterfaceNames
		r.IngesterRF1.LifecyclerConfig.Port = rc.InstancePort
		r.IngesterRF1.LifecyclerConfig.Addr = rc.InstanceAddr
		r.IngesterRF1.LifecyclerConfig.Zone = rc.InstanceZone
		r.IngesterRF1.LifecyclerConfig.ListenPort = rc.ListenPort
		r.IngesterRF1.LifecyclerConfig.ObservePeriod = rc.ObservePeriod
	}

	if mergeWithExisting {
		r.Pattern.LifecyclerConfig.RingConfig.KVStore = rc.KVStore
		r.Pattern.LifecyclerConfig.HeartbeatPeriod = rc.HeartbeatPeriod
		r.Pattern.LifecyclerConfig.RingConfig.HeartbeatTimeout = rc.HeartbeatTimeout
		r.Pattern.LifecyclerConfig.TokensFilePath = rc.TokensFilePath
		r.Pattern.LifecyclerConfig.RingConfig.ZoneAwarenessEnabled = rc.ZoneAwarenessEnabled
		r.Pattern.LifecyclerConfig.ID = rc.InstanceID
		r.Pattern.LifecyclerConfig.InfNames = rc.InstanceInterfaceNames
		r.Pattern.LifecyclerConfig.Port = rc.InstancePort
		r.Pattern.LifecyclerConfig.Addr = rc.InstanceAddr
		r.Pattern.LifecyclerConfig.Zone = rc.InstanceZone
		r.Pattern.LifecyclerConfig.ListenPort = rc.ListenPort
		r.Pattern.LifecyclerConfig.ObservePeriod = rc.ObservePeriod
	}

	if mergeWithExisting {
		r.KafkaIngester.LifecyclerConfig.RingConfig.KVStore = rc.KVStore
		r.KafkaIngester.LifecyclerConfig.HeartbeatPeriod = rc.HeartbeatPeriod
		r.KafkaIngester.LifecyclerConfig.RingConfig.HeartbeatTimeout = rc.HeartbeatTimeout
		r.KafkaIngester.LifecyclerConfig.TokensFilePath = rc.TokensFilePath
		r.KafkaIngester.LifecyclerConfig.RingConfig.ZoneAwarenessEnabled = rc.ZoneAwarenessEnabled
		r.KafkaIngester.LifecyclerConfig.ID = rc.InstanceID
		r.KafkaIngester.LifecyclerConfig.InfNames = rc.InstanceInterfaceNames
		r.KafkaIngester.LifecyclerConfig.Port = rc.InstancePort
		r.KafkaIngester.LifecyclerConfig.Addr = rc.InstanceAddr
		r.KafkaIngester.LifecyclerConfig.Zone = rc.InstanceZone
		r.KafkaIngester.LifecyclerConfig.ListenPort = rc.ListenPort
		r.KafkaIngester.LifecyclerConfig.ObservePeriod = rc.ObservePeriod
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

	// IndexGateway
	if mergeWithExisting || reflect.DeepEqual(r.IndexGateway.Ring, defaults.IndexGateway.Ring) {
		r.IndexGateway.Ring.HeartbeatTimeout = rc.HeartbeatTimeout
		r.IndexGateway.Ring.HeartbeatPeriod = rc.HeartbeatPeriod
		r.IndexGateway.Ring.InstancePort = rc.InstancePort
		r.IndexGateway.Ring.InstanceAddr = rc.InstanceAddr
		r.IndexGateway.Ring.InstanceID = rc.InstanceID
		r.IndexGateway.Ring.InstanceInterfaceNames = rc.InstanceInterfaceNames
		r.IndexGateway.Ring.InstanceZone = rc.InstanceZone
		r.IndexGateway.Ring.ZoneAwarenessEnabled = rc.ZoneAwarenessEnabled
		r.IndexGateway.Ring.KVStore = rc.KVStore
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

	// Index Gateway
	f, err = tokensFile(cfg, "indexgateway.tokens")
	if err != nil {
		return err
	}
	cfg.IndexGateway.Ring.TokensFilePath = f

	// Pattern
	f, err = tokensFile(cfg, "pattern.tokens")
	if err != nil {
		return err
	}
	cfg.Pattern.LifecyclerConfig.TokensFilePath = f

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
		if len(r.StorageConfig.BloomShipperConfig.WorkingDirectory) == 1 &&
			len(r.StorageConfig.BloomShipperConfig.WorkingDirectory) == len(defaults.StorageConfig.BloomShipperConfig.WorkingDirectory) &&
			r.StorageConfig.BloomShipperConfig.WorkingDirectory[0] == defaults.StorageConfig.BloomShipperConfig.WorkingDirectory[0] {
			_ = r.StorageConfig.BloomShipperConfig.WorkingDirectory.Set(fmt.Sprintf("%s/blooms", prefix))
		}
	}
}

// appendLoopbackInterface will append the loopback interface to the interface names used by the Loki components
// (ex: rings, v2 frontend, etc).
//
// The append won't occur for an specific component if an explicit list of net interface names is provided for that component.
func appendLoopbackInterface(cfg, defaults *ConfigWrapper) {
	loopbackIface, err := loki_net.LoopbackInterfaceName()
	if err != nil {
		return
	}

	if reflect.DeepEqual(cfg.Ingester.LifecyclerConfig.InfNames, defaults.Ingester.LifecyclerConfig.InfNames) {
		cfg.Ingester.LifecyclerConfig.InfNames = append(cfg.Ingester.LifecyclerConfig.InfNames, loopbackIface)
	}
	if reflect.DeepEqual(cfg.Pattern.LifecyclerConfig.InfNames, defaults.Pattern.LifecyclerConfig.InfNames) {
		cfg.Pattern.LifecyclerConfig.InfNames = append(cfg.Pattern.LifecyclerConfig.InfNames, loopbackIface)
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

	if reflect.DeepEqual(cfg.IndexGateway.Ring.InstanceInterfaceNames, defaults.IndexGateway.Ring.InstanceInterfaceNames) {
		cfg.IndexGateway.Ring.InstanceInterfaceNames = append(cfg.IndexGateway.Ring.InstanceInterfaceNames, loopbackIface)
	}
}

// applyMemberlistConfig will change the default ingester, distributor, ruler, and query scheduler ring configurations to use memberlist.
// The idea here is that if a user explicitly configured the memberlist configuration section, they probably want to be using memberlist
// for all their ring configurations. Since a user can still explicitly override a specific ring configuration
// (for example, use consul for the distributor), it seems harmless to take a guess at better defaults here.
func applyMemberlistConfig(r *ConfigWrapper) {
	r.Ingester.LifecyclerConfig.RingConfig.KVStore.Store = memberlistStr
	r.Pattern.LifecyclerConfig.RingConfig.KVStore.Store = memberlistStr
	r.Distributor.DistributorRing.KVStore.Store = memberlistStr
	r.Ruler.Ring.KVStore.Store = memberlistStr
	r.QueryScheduler.SchedulerRing.KVStore.Store = memberlistStr
	r.CompactorConfig.CompactorRing.KVStore.Store = memberlistStr
	r.IndexGateway.Ring.KVStore.Store = memberlistStr
}

var ErrTooManyStorageConfigs = errors.New("too many storage configs provided in the common config, please only define one storage backend")

// applyStorageConfig will attempt to apply a common storage config for either
// s3, gcs, azure, or swift to all the places we create a storage client.
// If any specific configs for an object storage client have been provided elsewhere in the
// configuration file, applyStorageConfig will not override them.
// If multiple storage configurations are provided, applyStorageConfig will return an error
func applyStorageConfig(cfg, defaults *ConfigWrapper) error {
	var applyConfig func(*ConfigWrapper)

	// only one config is allowed
	configsFound := 0

	if !reflect.DeepEqual(cfg.Common.Storage.Azure, defaults.StorageConfig.AzureStorageConfig) {
		configsFound++

		applyConfig = func(r *ConfigWrapper) {
			r.Ruler.StoreConfig.Type = "azure"
			r.Ruler.StoreConfig.Azure = r.Common.Storage.Azure
			r.StorageConfig.AzureStorageConfig = r.Common.Storage.Azure
			r.StorageConfig.Hedging = r.Common.Storage.Hedging
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
		}
	}

	if !reflect.DeepEqual(cfg.Common.Storage.GCS, defaults.StorageConfig.GCSConfig) {
		configsFound++

		applyConfig = func(r *ConfigWrapper) {
			r.Ruler.StoreConfig.Type = "gcs"
			r.Ruler.StoreConfig.GCS = r.Common.Storage.GCS
			r.StorageConfig.GCSConfig = r.Common.Storage.GCS
			r.StorageConfig.Hedging = r.Common.Storage.Hedging
		}
	}

	if !reflect.DeepEqual(cfg.Common.Storage.S3, defaults.StorageConfig.AWSStorageConfig.S3Config) {
		configsFound++

		applyConfig = func(r *ConfigWrapper) {
			r.Ruler.StoreConfig.Type = "s3"
			r.Ruler.StoreConfig.S3 = r.Common.Storage.S3
			r.StorageConfig.AWSStorageConfig.S3Config = r.Common.Storage.S3
			r.StorageConfig.Hedging = r.Common.Storage.Hedging
		}
	}

	if !reflect.DeepEqual(cfg.Common.Storage.BOS, defaults.StorageConfig.BOSStorageConfig) {
		configsFound++
		applyConfig = func(r *ConfigWrapper) {
			r.Ruler.StoreConfig.Type = "bos"
			r.Ruler.StoreConfig.BOS = r.Common.Storage.BOS
			r.StorageConfig.BOSStorageConfig = r.Common.Storage.BOS
		}
	}

	if !reflect.DeepEqual(cfg.Common.Storage.Swift, defaults.StorageConfig.Swift) {
		configsFound++

		applyConfig = func(r *ConfigWrapper) {
			r.Ruler.StoreConfig.Type = "swift"
			r.Ruler.StoreConfig.Swift = r.Common.Storage.Swift
			r.StorageConfig.Swift = r.Common.Storage.Swift
			r.StorageConfig.Hedging = r.Common.Storage.Hedging
		}
	}

	if !reflect.DeepEqual(cfg.Common.Storage.CongestionControl, defaults.StorageConfig.CongestionControl) {
		applyConfig = func(r *ConfigWrapper) {
			r.StorageConfig.CongestionControl = r.Common.Storage.CongestionControl
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

func betterBoltdbShipperDefaults(cfg *ConfigWrapper) {
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

func betterTSDBShipperDefaults(cfg *ConfigWrapper) {
	if cfg.Common.PathPrefix != "" {
		prefix := strings.TrimSuffix(cfg.Common.PathPrefix, "/")

		if cfg.StorageConfig.TSDBShipperConfig.ActiveIndexDirectory == "" {
			cfg.StorageConfig.TSDBShipperConfig.ActiveIndexDirectory = fmt.Sprintf("%s/tsdb-shipper-active", prefix)
		}

		if cfg.StorageConfig.TSDBShipperConfig.CacheLocation == "" {
			cfg.StorageConfig.TSDBShipperConfig.CacheLocation = fmt.Sprintf("%s/tsdb-shipper-cache", prefix)
		}
	}
}

// applyEmbeddedCacheConfig turns on Embedded cache for the chunk store, query range results,
// index stats and volume results only if no other cache storage is configured (redis or memcache).
// Not applicable for the index queries cache or for the write dedupe cache.
func applyEmbeddedCacheConfig(r *ConfigWrapper) {
	chunkCacheConfig := r.ChunkStoreConfig.ChunkCacheConfig
	if !cache.IsCacheConfigured(chunkCacheConfig) {
		r.ChunkStoreConfig.ChunkCacheConfig.EmbeddedCache.Enabled = true
	}

	resultsCacheConfig := r.QueryRange.ResultsCacheConfig.CacheConfig
	if !cache.IsCacheConfigured(resultsCacheConfig) {
		r.QueryRange.ResultsCacheConfig.CacheConfig.EmbeddedCache.Enabled = true
	}

	indexStatsCacheConfig := r.QueryRange.StatsCacheConfig.CacheConfig
	if !cache.IsCacheConfigured(indexStatsCacheConfig) {
		prefix := indexStatsCacheConfig.Prefix
		// We use the same config as the query range results cache.
		r.QueryRange.StatsCacheConfig.CacheConfig = r.QueryRange.ResultsCacheConfig.CacheConfig
		r.QueryRange.StatsCacheConfig.CacheConfig.Prefix = prefix
	}

	volumeCacheConfig := r.QueryRange.VolumeCacheConfig.CacheConfig
	if !cache.IsCacheConfigured(volumeCacheConfig) {
		prefix := volumeCacheConfig.Prefix
		// We use the same config as the query range results cache.
		r.QueryRange.VolumeCacheConfig.CacheConfig = r.QueryRange.ResultsCacheConfig.CacheConfig
		r.QueryRange.VolumeCacheConfig.CacheConfig.Prefix = prefix
	}

	seriesCacheConfig := r.QueryRange.SeriesCacheConfig.CacheConfig
	if !cache.IsCacheConfigured(seriesCacheConfig) {
		prefix := seriesCacheConfig.Prefix
		r.QueryRange.SeriesCacheConfig.CacheConfig = r.QueryRange.ResultsCacheConfig.CacheConfig
		r.QueryRange.SeriesCacheConfig.CacheConfig.Prefix = prefix
	}

	labelsCacheConfig := r.QueryRange.LabelsCacheConfig.CacheConfig
	if !cache.IsCacheConfigured(labelsCacheConfig) {
		prefix := labelsCacheConfig.Prefix
		r.QueryRange.LabelsCacheConfig.CacheConfig = r.QueryRange.ResultsCacheConfig.CacheConfig
		r.QueryRange.LabelsCacheConfig.CacheConfig.Prefix = prefix
	}

	instantMetricCacheConfig := r.QueryRange.InstantMetricCacheConfig.CacheConfig
	if !cache.IsCacheConfigured(instantMetricCacheConfig) {
		prefix := instantMetricCacheConfig.Prefix
		r.QueryRange.InstantMetricCacheConfig.CacheConfig = r.QueryRange.ResultsCacheConfig.CacheConfig
		r.QueryRange.InstantMetricCacheConfig.CacheConfig.Prefix = prefix
	}
}

func applyIngesterFinalSleep(cfg *ConfigWrapper) {
	cfg.Ingester.LifecyclerConfig.FinalSleep = 0 * time.Second
}

func applyIngesterReplicationFactor(cfg *ConfigWrapper) {
	cfg.Ingester.LifecyclerConfig.RingConfig.ReplicationFactor = cfg.Common.ReplicationFactor
	cfg.IngesterRF1.LifecyclerConfig.RingConfig.ReplicationFactor = cfg.Common.ReplicationFactor
}

// applyChunkRetain is used to set chunk retain based on having an index query cache configured
// We retain chunks for at least as long as the index queries cache TTL. When an index entry is
// cached, any chunks flushed after that won't be in the cached entry. To make sure their data is
// available the RetainPeriod keeps them available in the ingesters live data. We want to retain them
// for at least as long as the TTL on the index queries cache.
func applyChunkRetain(cfg, defaults *ConfigWrapper) {
	if !reflect.DeepEqual(cfg.StorageConfig.IndexQueriesCacheConfig, defaults.StorageConfig.IndexQueriesCacheConfig) {
		// Only apply this change if the active index period is for boltdb-shipper
		p := config.ActivePeriodConfig(cfg.SchemaConfig.Configs)
		if cfg.SchemaConfig.Configs[p].IndexType == types.BoltDBShipperType {
			// Set the retain period to the cache validity plus one minute. One minute is arbitrary but leaves some
			// buffer to make sure the chunks are there until the index entries expire.
			cfg.Ingester.RetainPeriod = cfg.StorageConfig.IndexCacheValidity + 1*time.Minute
		}
	}
}

func applyCommonQuerierWorkerGRPCConfig(cfg, defaults *ConfigWrapper) error {
	usingNewFrontendCfg := !reflect.DeepEqual(cfg.Worker.NewQueryFrontendGRPCClientConfig, defaults.Worker.NewQueryFrontendGRPCClientConfig)
	usingNewSchedulerCfg := !reflect.DeepEqual(cfg.Worker.QuerySchedulerGRPCClientConfig, defaults.Worker.QuerySchedulerGRPCClientConfig)
	usingOldFrontendCfg := !reflect.DeepEqual(cfg.Worker.OldQueryFrontendGRPCClientConfig, defaults.Worker.OldQueryFrontendGRPCClientConfig)

	if usingOldFrontendCfg {
		if usingNewFrontendCfg || usingNewSchedulerCfg {
			return fmt.Errorf("both `grpc_client_config` and (`query_frontend_grpc_client` or `query_scheduler_grpc_client`) are set at the same time. Please use only `query_frontend_grpc_client` and `query_scheduler_grpc_client`")
		}
		cfg.Worker.NewQueryFrontendGRPCClientConfig = cfg.Worker.OldQueryFrontendGRPCClientConfig
		cfg.Worker.QuerySchedulerGRPCClientConfig = cfg.Worker.OldQueryFrontendGRPCClientConfig
	}

	return nil
}
