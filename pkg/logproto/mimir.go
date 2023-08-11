// SPDX-License-Identifier: AGPL-3.0-only
// Provenance-includes-location: https://github.com/cortexproject/cortex/blob/master/pkg/cortex/cortex.go
// Provenance-includes-license: Apache-2.0
// Provenance-includes-copyright: The Cortex Authors.

package mimir

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/flagext"
	"github.com/grafana/dskit/grpcutil"
	"github.com/grafana/dskit/kv/memberlist"
	"github.com/grafana/dskit/modules"
	"github.com/grafana/dskit/multierror"
	"github.com/grafana/dskit/ring"
	"github.com/grafana/dskit/runtimeconfig"
	"github.com/grafana/dskit/services"
	"github.com/opentracing/opentracing-go"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/prometheus/promql"
	prom_storage "github.com/prometheus/prometheus/storage"
	"github.com/weaveworks/common/server"
	"github.com/weaveworks/common/signals"
	"go.opentelemetry.io/otel"
	"go.uber.org/atomic"
	"google.golang.org/grpc/health/grpc_health_v1"
	"gopkg.in/yaml.v3"

	"github.com/grafana/dskit/tenant"

	"github.com/grafana/mimir/pkg/alertmanager"
	"github.com/grafana/mimir/pkg/alertmanager/alertstore"
	alertbucketclient "github.com/grafana/mimir/pkg/alertmanager/alertstore/bucketclient"
	alertstorelocal "github.com/grafana/mimir/pkg/alertmanager/alertstore/local"
	"github.com/grafana/mimir/pkg/api"
	"github.com/grafana/mimir/pkg/compactor"
	"github.com/grafana/mimir/pkg/distributor"
	"github.com/grafana/mimir/pkg/flusher"
	"github.com/grafana/mimir/pkg/frontend"
	"github.com/grafana/mimir/pkg/frontend/querymiddleware"
	frontendv1 "github.com/grafana/mimir/pkg/frontend/v1"
	"github.com/grafana/mimir/pkg/ingester"
	"github.com/grafana/mimir/pkg/ingester/client"
	"github.com/grafana/mimir/pkg/querier"
	"github.com/grafana/mimir/pkg/querier/tenantfederation"
	querier_worker "github.com/grafana/mimir/pkg/querier/worker"
	"github.com/grafana/mimir/pkg/ruler"
	"github.com/grafana/mimir/pkg/ruler/rulestore"
	rulebucketclient "github.com/grafana/mimir/pkg/ruler/rulestore/bucketclient"
	rulestorelocal "github.com/grafana/mimir/pkg/ruler/rulestore/local"
	"github.com/grafana/mimir/pkg/scheduler"
	"github.com/grafana/mimir/pkg/storage/bucket"
	"github.com/grafana/mimir/pkg/storage/tsdb"
	"github.com/grafana/mimir/pkg/storegateway"
	"github.com/grafana/mimir/pkg/usagestats"
	"github.com/grafana/mimir/pkg/util"
	"github.com/grafana/mimir/pkg/util/activitytracker"
	util_log "github.com/grafana/mimir/pkg/util/log"
	"github.com/grafana/mimir/pkg/util/noauth"
	"github.com/grafana/mimir/pkg/util/process"
	"github.com/grafana/mimir/pkg/util/validation"
)

var errInvalidBucketConfig = errors.New("invalid bucket config")

// The design pattern for Mimir is a series of config objects, which are
// registered for command line flags, and then a series of components that
// are instantiated and composed.  Some rules of thumb:
// - Config types should only contain 'simple' types (ints, strings, urls etc).
// - Flag validation should be done by the flag; use a flag.Value where
//   appropriate.
// - Config types should map 1:1 with a component type.
// - Config types should define flags with a common prefix.
// - It's fine to nest configs within configs, but this should match the
//   nesting of components within components.
// - Limit as much is possible sharing of configuration between config types.
//   Where necessary, use a pointer for this - avoid repetition.
// - Where a nesting of components its not obvious, it's fine to pass
//   references to other components constructors to compose them.
// - First argument for a components constructor should be its matching config
//   object.

// Config is the root config for Mimir.
type Config struct {
	Target              flagext.StringSliceCSV `yaml:"target"`
	MultitenancyEnabled bool                   `yaml:"multitenancy_enabled"`
	NoAuthTenant        string                 `yaml:"no_auth_tenant" category:"advanced"`
	ShutdownDelay       time.Duration          `yaml:"shutdown_delay" category:"experimental"`
	PrintConfig         bool                   `yaml:"-"`
	ApplicationName     string                 `yaml:"-"`

	API              api.Config                      `yaml:"api"`
	Server           server.Config                   `yaml:"server"`
	Distributor      distributor.Config              `yaml:"distributor"`
	Querier          querier.Config                  `yaml:"querier"`
	IngesterClient   client.Config                   `yaml:"ingester_client"`
	Ingester         ingester.Config                 `yaml:"ingester"`
	Flusher          flusher.Config                  `yaml:"flusher"`
	LimitsConfig     validation.Limits               `yaml:"limits"`
	Worker           querier_worker.Config           `yaml:"frontend_worker"`
	Frontend         frontend.CombinedFrontendConfig `yaml:"frontend"`
	BlocksStorage    tsdb.BlocksStorageConfig        `yaml:"blocks_storage"`
	Compactor        compactor.Config                `yaml:"compactor"`
	StoreGateway     storegateway.Config             `yaml:"store_gateway"`
	TenantFederation tenantfederation.Config         `yaml:"tenant_federation"`
	ActivityTracker  activitytracker.Config          `yaml:"activity_tracker"`

	Ruler               ruler.Config                               `yaml:"ruler"`
	RulerStorage        rulestore.Config                           `yaml:"ruler_storage"`
	Alertmanager        alertmanager.MultitenantAlertmanagerConfig `yaml:"alertmanager"`
	AlertmanagerStorage alertstore.Config                          `yaml:"alertmanager_storage"`
	RuntimeConfig       runtimeconfig.Config                       `yaml:"runtime_config"`
	MemberlistKV        memberlist.KVConfig                        `yaml:"memberlist"`
	QueryScheduler      scheduler.Config                           `yaml:"query_scheduler"`
	UsageStats          usagestats.Config                          `yaml:"usage_stats"`

	Common CommonConfig `yaml:"common"`
}

// RegisterFlags registers flag.
func (c *Config) RegisterFlags(f *flag.FlagSet, logger log.Logger) {
	c.ApplicationName = "Grafana Mimir"
	c.Server.MetricsNamespace = "cortex"
	c.Server.ExcludeRequestInLog = true
	c.Server.DisableRequestSuccessLog = true

	// Set the default module list to 'all'
	c.Target = []string{All}

	f.Var(&c.Target, "target", "Comma-separated list of components to include in the instantiated process. "+
		"The default value 'all' includes all components that are required to form a functional Grafana Mimir instance in single-binary mode. "+
		"Use the '-modules' command line flag to get a list of available components, and to see which components are included with 'all'.")

	f.BoolVar(&c.MultitenancyEnabled, "auth.multitenancy-enabled", true, "When set to true, incoming HTTP requests must specify tenant ID in HTTP X-Scope-OrgId header. When set to false, tenant ID from -auth.no-auth-tenant is used instead.")
	f.StringVar(&c.NoAuthTenant, "auth.no-auth-tenant", "anonymous", "Tenant ID to use when multitenancy is disabled.")
	f.BoolVar(&c.PrintConfig, "print.config", false, "Print the config and exit.")
	f.DurationVar(&c.ShutdownDelay, "shutdown-delay", 0, "How long to wait between SIGTERM and shutdown. After receiving SIGTERM, Mimir will report not-ready status via /ready endpoint.")

	c.API.RegisterFlags(f)
	c.registerServerFlagsWithChangedDefaultValues(f)
	c.Distributor.RegisterFlags(f, logger)
	c.Querier.RegisterFlags(f)
	c.IngesterClient.RegisterFlags(f)
	c.Ingester.RegisterFlags(f, logger)
	c.Flusher.RegisterFlags(f)
	c.LimitsConfig.RegisterFlags(f)
	c.Worker.RegisterFlags(f)
	c.Frontend.RegisterFlags(f, logger)
	c.BlocksStorage.RegisterFlags(f, logger)
	c.Compactor.RegisterFlags(f, logger)
	c.StoreGateway.RegisterFlags(f, logger)
	c.TenantFederation.RegisterFlags(f)

	c.Ruler.RegisterFlags(f, logger)
	c.RulerStorage.RegisterFlags(f, logger)
	c.Alertmanager.RegisterFlags(f, logger)
	c.AlertmanagerStorage.RegisterFlags(f, logger)
	c.RuntimeConfig.RegisterFlags(f)
	c.MemberlistKV.RegisterFlags(f)
	c.ActivityTracker.RegisterFlags(f)
	c.QueryScheduler.RegisterFlags(f, logger)
	c.UsageStats.RegisterFlags(f)

	c.Common.RegisterFlags(f, logger)
}

func (c *Config) CommonConfigInheritance() CommonConfigInheritance {
	return CommonConfigInheritance{
		Storage: map[string]*bucket.StorageBackendConfig{
			"blocks_storage":       &c.BlocksStorage.Bucket.StorageBackendConfig,
			"ruler_storage":        &c.RulerStorage.StorageBackendConfig,
			"alertmanager_storage": &c.AlertmanagerStorage.StorageBackendConfig,
		},
	}
}

// ConfigWithCommon should be passed to yaml.Unmarshal to properly unmarshal Common values.
// We don't implement UnmarshalYAML on Config itself because that would disallow inlining it in other configs.
type ConfigWithCommon Config

func (c *ConfigWithCommon) UnmarshalYAML(value *yaml.Node) error {
	if err := UnmarshalCommonYAML(value, (*Config)(c)); err != nil {
		return err
	}

	// Then unmarshal config in a standard way.
	// This will override previously set common values by the specific ones, if they're provided.
	// (YAML specific takes precedence over YAML common)
	return value.DecodeWithOptions((*Config)(c), yaml.DecodeOptions{KnownFields: true})
}

// Validate the mimir config and return an error if the validation
// doesn't pass
func (c *Config) Validate(log log.Logger) error {
	if err := c.validateBucketConfigs(); err != nil {
		return fmt.Errorf("%w: %s", errInvalidBucketConfig, err)
	}
	if err := c.validateFilesystemPaths(log); err != nil {
		return err
	}
	if err := c.RulerStorage.Validate(); err != nil {
		return errors.Wrap(err, "invalid rulestore config")
	}
	if err := c.Ruler.Validate(c.LimitsConfig, log); err != nil {
		return errors.Wrap(err, "invalid ruler config")
	}
	if err := c.BlocksStorage.Validate(); err != nil {
		return errors.Wrap(err, "invalid TSDB config")
	}
	if err := c.Distributor.Validate(c.LimitsConfig); err != nil {
		return errors.Wrap(err, "invalid distributor config")
	}
	if err := c.Querier.Validate(); err != nil {
		return errors.Wrap(err, "invalid querier config")
	}
	if c.Querier.EngineConfig.Timeout > c.Server.HTTPServerWriteTimeout {
		return fmt.Errorf("querier timeout (%s) must be lower than or equal to HTTP server write timeout (%s)",
			c.Querier.EngineConfig.Timeout, c.Server.HTTPServerWriteTimeout)
	}
	if err := c.IngesterClient.Validate(log); err != nil {
		return errors.Wrap(err, "invalid ingester_client config")
	}
	if err := c.Worker.Validate(log); err != nil {
		return errors.Wrap(err, "invalid frontend_worker config")
	}
	if err := c.Frontend.Validate(log); err != nil {
		return errors.Wrap(err, "invalid query-frontend config")
	}
	if err := c.StoreGateway.Validate(c.LimitsConfig); err != nil {
		return errors.Wrap(err, "invalid store-gateway config")
	}
	if err := c.Compactor.Validate(); err != nil {
		return errors.Wrap(err, "invalid compactor config")
	}
	if err := c.AlertmanagerStorage.Validate(); err != nil {
		return errors.Wrap(err, "invalid alertmanager storage config")
	}
	if err := c.QueryScheduler.Validate(); err != nil {
		return errors.Wrap(err, "invalid query-scheduler config")
	}
	if err := c.UsageStats.Validate(); err != nil {
		return errors.Wrap(err, "invalid usage stats config")
	}
	if c.isAnyModuleEnabled(AlertManager, Backend) {
		if err := c.Alertmanager.Validate(); err != nil {
			return errors.Wrap(err, "invalid alertmanager config")
		}
	}
	return nil
}

func (c *Config) isModuleEnabled(m string) bool {
	return util.StringsContain(c.Target, m)
}

func (c *Config) isAnyModuleEnabled(modules ...string) bool {
	for _, m := range modules {
		if c.isModuleEnabled(m) {
			return true
		}
	}

	return false
}

func (c *Config) validateBucketConfigs() error {
	errs := multierror.New()

	// Validate alertmanager bucket config.
	if c.isAnyModuleEnabled(AlertManager, Backend) && c.AlertmanagerStorage.Backend != alertstorelocal.Name {
		errs.Add(errors.Wrap(validateBucketConfig(c.AlertmanagerStorage.Config, c.BlocksStorage.Bucket), "alertmanager storage"))
	}

	// Validate ruler bucket config.
	if c.isAnyModuleEnabled(All, Ruler, Backend) && c.RulerStorage.Backend != rulestorelocal.Name {
		errs.Add(errors.Wrap(validateBucketConfig(c.RulerStorage.Config, c.BlocksStorage.Bucket), "ruler storage"))
	}

	return errs.Err()
}

func validateBucketConfig(cfg bucket.Config, blockStorageBucketCfg bucket.Config) error {
	if cfg.Backend != blockStorageBucketCfg.Backend {
		return nil
	}

	if cfg.StoragePrefix != blockStorageBucketCfg.StoragePrefix {
		return nil
	}

	switch cfg.Backend {
	case bucket.S3:
		if cfg.S3.BucketName == blockStorageBucketCfg.S3.BucketName {
			return errors.New("S3 bucket name and storage prefix cannot be the same as the one used in blocks storage config")
		}

	case bucket.GCS:
		if cfg.GCS.BucketName == blockStorageBucketCfg.GCS.BucketName {
			return errors.New("GCS bucket name and storage prefix cannot be the same as the one used in blocks storage config")
		}

	case bucket.Azure:
		if cfg.Azure.ContainerName == blockStorageBucketCfg.Azure.ContainerName && cfg.Azure.StorageAccountName == blockStorageBucketCfg.Azure.StorageAccountName {
			return errors.New("Azure container and account names and storage prefix cannot be the same as the ones used in blocks storage config")
		}

	// To keep it simple here we only check that container and project names are not the same.
	// We could also verify both configuration endpoints to determine uniqueness,
	// however different auth URLs do not imply different clusters, since a single cluster
	// may have several configured endpoints.
	case bucket.Swift:
		if cfg.Swift.ContainerName == blockStorageBucketCfg.Swift.ContainerName && cfg.Swift.ProjectName == blockStorageBucketCfg.Swift.ProjectName {
			return errors.New("Swift container and project names and storage prefix cannot be the same as the ones used in blocks storage config")
		}
	}
	return nil
}

// validateFilesystemPaths checks all configured filesystem paths and return error if it finds two of them
// overlapping (Mimir expects all filesystem paths to not overlap).
func (c *Config) validateFilesystemPaths(logger log.Logger) error {
	type pathConfig struct {
		name       string
		cfgValue   string
		checkValue string
	}

	var paths []pathConfig

	// Blocks storage (check only for components using it).
	if c.isAnyModuleEnabled(All, Write, Read, Backend, Ingester, Querier, StoreGateway, Compactor, Ruler) && c.BlocksStorage.Bucket.Backend == bucket.Filesystem {
		// Add the optional prefix to the path, because that's the actual location where blocks will be stored.
		paths = append(paths, pathConfig{
			name:       "blocks storage filesystem directory",
			cfgValue:   c.BlocksStorage.Bucket.Filesystem.Directory,
			checkValue: filepath.Join(c.BlocksStorage.Bucket.Filesystem.Directory, c.BlocksStorage.Bucket.StoragePrefix),
		})
	}

	// Ingester.
	if c.isAnyModuleEnabled(All, Ingester, Write) {
		paths = append(paths, pathConfig{
			name:       "tsdb directory",
			cfgValue:   c.BlocksStorage.TSDB.Dir,
			checkValue: c.BlocksStorage.TSDB.Dir,
		})
	}

	// Store-gateway.
	if c.isAnyModuleEnabled(All, StoreGateway, Backend) {
		paths = append(paths, pathConfig{
			name:       "bucket store sync directory",
			cfgValue:   c.BlocksStorage.BucketStore.SyncDir,
			checkValue: c.BlocksStorage.BucketStore.SyncDir,
		})
	}

	// Compactor.
	if c.isAnyModuleEnabled(All, Compactor, Backend) {
		paths = append(paths, pathConfig{
			name:       "compactor data directory",
			cfgValue:   c.Compactor.DataDir,
			checkValue: c.Compactor.DataDir,
		})
	}

	// Ruler.
	if c.isAnyModuleEnabled(All, Ruler, Backend) {
		paths = append(paths, pathConfig{
			name:       "ruler data directory",
			cfgValue:   c.Ruler.RulePath,
			checkValue: c.Ruler.RulePath,
		})

		if c.RulerStorage.Backend == bucket.Filesystem {
			// All ruler configuration is stored under an hardcoded prefix that we're taking in account here.
			paths = append(paths, pathConfig{
				name:       "ruler storage filesystem directory",
				cfgValue:   c.RulerStorage.Filesystem.Directory,
				checkValue: filepath.Join(c.RulerStorage.Filesystem.Directory, rulebucketclient.RulesPrefix),
			})
		}
		if c.RulerStorage.Backend == rulestorelocal.Name {
			paths = append(paths, pathConfig{
				name:       "ruler storage local directory",
				cfgValue:   c.RulerStorage.Local.Directory,
				checkValue: c.RulerStorage.Local.Directory,
			})
		}
	}

	// Alertmanager.
	if c.isAnyModuleEnabled(AlertManager) {
		paths = append(paths, pathConfig{
			name:       "alertmanager data directory",
			cfgValue:   c.Alertmanager.DataDir,
			checkValue: c.Alertmanager.DataDir,
		})

		if c.AlertmanagerStorage.Backend == bucket.Filesystem {
			var (
				name     = "alertmanager storage filesystem directory"
				cfgValue = c.AlertmanagerStorage.Filesystem.Directory
			)

			// All ruler configuration is stored under an hardcoded prefix that we're taking into account here.
			paths = append(paths, pathConfig{name: name, cfgValue: cfgValue, checkValue: filepath.Join(c.AlertmanagerStorage.Filesystem.Directory, alertbucketclient.AlertsPrefix)})
			paths = append(paths, pathConfig{name: name, cfgValue: cfgValue, checkValue: filepath.Join(c.AlertmanagerStorage.Filesystem.Directory, alertbucketclient.AlertmanagerPrefix)})
		}

		if c.AlertmanagerStorage.Backend == alertstorelocal.Name {
			paths = append(paths, pathConfig{
				name:       "alertmanager storage local directory",
				cfgValue:   c.AlertmanagerStorage.Local.Path,
				checkValue: c.AlertmanagerStorage.Local.Path,
			})
		}
	}

	// Convert all check paths to absolute clean paths.
	for idx, path := range paths {
		abs, err := filepath.Abs(path.checkValue)
		if err != nil {
			// We prefer to log a warning instead of returning an error to ensure that if we're unable to
			// run the sanity check Mimir could start anyway.
			level.Warn(logger).Log("msg", "the configuration sanity check for the filesystem directory has been skipped because can't get the absolute path", "path", path, "err", err)
			paths[idx].checkValue = ""
			continue
		}

		paths[idx].checkValue = abs
	}

	for _, firstPath := range paths {
		for _, secondPath := range paths {
			// Skip the same config field.
			if firstPath.name == secondPath.name {
				continue
			}

			// Skip if we've been unable to get the absolute path of one of the two paths.
			if firstPath.checkValue == "" || secondPath.checkValue == "" {
				continue
			}

			if isAbsPathOverlapping(firstPath.checkValue, secondPath.checkValue) {
				// Report the configured path in the error message, otherwise it's harder for the user to spot it.
				return fmt.Errorf("the configured %s %q cannot overlap with the configured %s %q; please set different paths, also ensuring one is not a subdirectory of the other one", firstPath.name, firstPath.cfgValue, secondPath.name, secondPath.cfgValue)
			}
		}
	}

	return nil
}

// isAbsPathOverlapping returns whether the two input absolute paths overlap.
func isAbsPathOverlapping(firstAbsPath, secondAbsPath string) bool {
	firstBase, firstName := filepath.Split(firstAbsPath)
	secondBase, secondName := filepath.Split(secondAbsPath)

	if firstBase == secondBase {
		// The base directories are the same, so they overlap if the last segment of the path (name)
		// is the same or it's missing (happens when the input path is the root "/").
		return firstName == secondName || firstName == "" || secondName == ""
	}

	// The base directories are different, but they could still overlap if one is the child of the other one.
	return strings.HasPrefix(firstAbsPath, secondAbsPath) || strings.HasPrefix(secondAbsPath, firstAbsPath)
}

func (c *Config) registerServerFlagsWithChangedDefaultValues(fs *flag.FlagSet) {
	throwaway := flag.NewFlagSet("throwaway", flag.PanicOnError)

	// Register to throwaway flags first. Default values are remembered during registration and cannot be changed,
	// but we can take values from throwaway flag set and re-register into supplied flag set with new default values.
	c.Server.RegisterFlags(throwaway)

	defaultsOverrides := map[string]string{
		"server.http-write-timeout":                         "2m",
		"server.grpc.keepalive.min-time-between-pings":      "10s",
		"server.grpc.keepalive.ping-without-stream-allowed": "true",
		"server.http-listen-port":                           "8080",
		"server.grpc-max-recv-msg-size-bytes":               strconv.Itoa(100 * 1024 * 1024),
		"server.grpc-max-send-msg-size-bytes":               strconv.Itoa(100 * 1024 * 1024),
	}

	throwaway.VisitAll(func(f *flag.Flag) {
		if defaultValue, overridden := defaultsOverrides[f.Name]; overridden {
			// Ignore errors when setting new values. We have a test to verify that it works.
			_ = f.Value.Set(defaultValue)
		}

		fs.Var(f.Value, f.Name, f.Usage)
	})
}

// CommonConfigInheriter abstracts config that inherit common config values.
type CommonConfigInheriter interface {
	CommonConfigInheritance() CommonConfigInheritance
}

// UnmarshalCommonYAML provides the implementation for UnmarshalYAML functions to unmarshal the CommonConfig.
// A list of CommonConfig inheriters can be provided
func UnmarshalCommonYAML(value *yaml.Node, inheriters ...CommonConfigInheriter) error {
	for _, inh := range inheriters {
		specificStorageLocations := specificLocationsUnmarshaler{}
		inheritance := inh.CommonConfigInheritance()
		for name, loc := range inheritance.Storage {
			specificStorageLocations[name] = loc
		}

		common := configWithCustomCommonUnmarshaler{
			Common: &commonConfigUnmarshaler{
				Storage: &specificStorageLocations,
			},
		}

		if err := value.DecodeWithOptions(&common, yaml.DecodeOptions{KnownFields: true}); err != nil {
			return fmt.Errorf("can't unmarshal common config: %w", err)
		}
	}

	return nil
}

// InheritCommonFlagValues will inherit the values of the provided common flags to all the inheriters.
func InheritCommonFlagValues(log log.Logger, fs *flag.FlagSet, common CommonConfig, inheriters ...CommonConfigInheriter) error {
	setFlags := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { setFlags[f.Name] = true })

	for _, inh := range inheriters {
		inheritance := inh.CommonConfigInheritance()
		for desc, loc := range inheritance.Storage {
			if err := inheritFlags(log, common.Storage.RegisteredFlags, loc.RegisteredFlags, setFlags); err != nil {
				return fmt.Errorf("can't inherit common flags for %q: %w", desc, err)
			}
		}
	}

	return nil
}

// inheritFlags takes flags from the origin set and sets them to the equivalent flags in the dest set, unless those are already set.
func inheritFlags(log log.Logger, orig util.RegisteredFlags, dest util.RegisteredFlags, set map[string]bool) error {
	for f, o := range orig.Flags {
		d, ok := dest.Flags[f]
		if !ok {
			return fmt.Errorf("implementation error: flag %q was in flags prefixed with %q (%q) but was not in flags prefixed with %q (%q)", f, orig.Prefix, o.Name, dest.Prefix, d.Name)
		}
		if !set[o.Name] {
			// Nothing to inherit because origin was not set.
			continue
		}
		if set[d.Name] {
			// Can't inherit because destination was set.
			continue
		}
		if o.Value.String() == d.Value.String() {
			// Already the same, no need to touch.
			continue
		}
		level.Debug(log).Log(
			"msg", "Inheriting flag value",
			"origin_flag", o.Name, "origin_value", o.Value,
			"destination_flag", d.Name, "destination_value", d.Value,
		)
		if err := d.Value.Set(o.Value.String()); err != nil {
			return fmt.Errorf("can't set flag %q to flag's %q value %q: %s", d.Name, o.Name, o.Value, err)
		}
	}
	return nil
}

type CommonConfig struct {
	Storage bucket.StorageBackendConfig `yaml:"storage"`
}

type CommonConfigInheritance struct {
	Storage map[string]*bucket.StorageBackendConfig
}

// RegisterFlags registers flag.
func (c *CommonConfig) RegisterFlags(f *flag.FlagSet, logger log.Logger) {
	c.Storage.RegisterFlagsWithPrefix("common.storage.", f, logger)
}

// configWithCustomCommonUnmarshaler unmarshals config with custom unmarshaler for the `common` field.
type configWithCustomCommonUnmarshaler struct {
	// Common will unmarshal `common` yaml key using a custom unmarshaler.
	Common *commonConfigUnmarshaler `yaml:"common"`
	// Throwaway will contain the rest of the configuration options,
	// so we can still use strict unmarshaling for common,
	// but we won't complain about not knowing the rest of the config keys.
	Throwaway map[string]interface{} `yaml:",inline"`
}

// commonConfigUnmarshaler will unmarshal each field of the common config into specific locations.
type commonConfigUnmarshaler struct {
	Storage *specificLocationsUnmarshaler `yaml:"storage"`
}

// specificLocationsUnmarshaler will unmarshal yaml into specific locations.
// Keys are names (used to provide meaningful errors) and values are references to places
// where this should be unmarshaled.
type specificLocationsUnmarshaler map[string]interface{}

func (m specificLocationsUnmarshaler) UnmarshalYAML(value *yaml.Node) error {
	for l, v := range m {
		if err := value.DecodeWithOptions(v, yaml.DecodeOptions{KnownFields: true}); err != nil {
			return fmt.Errorf("key %q: %w", l, err)
		}
	}
	return nil
}

// Mimir is the root datastructure for Mimir.
type Mimir struct {
	Cfg        Config
	Registerer prometheus.Registerer

	// set during initialization
	ServiceMap    map[string]services.Service
	ModuleManager *modules.Manager

	API                      *api.API
	Server                   *server.Server
	Ring                     *ring.Ring
	TenantLimits             validation.TenantLimits
	Overrides                *validation.Overrides
	Distributor              *distributor.Distributor
	Ingester                 *ingester.Ingester
	Flusher                  *flusher.Flusher
	Frontend                 *frontendv1.Frontend
	RuntimeConfig            *runtimeconfig.Manager
	QuerierQueryable         prom_storage.SampleAndChunkQueryable
	ExemplarQueryable        prom_storage.ExemplarQueryable
	MetadataSupplier         querier.MetadataSupplier
	QuerierEngine            *promql.Engine
	QueryFrontendTripperware querymiddleware.Tripperware
	Ruler                    *ruler.Ruler
	RulerStorage             rulestore.RuleStore
	Alertmanager             *alertmanager.MultitenantAlertmanager
	Compactor                *compactor.MultitenantCompactor
	StoreGateway             *storegateway.StoreGateway
	MemberlistKV             *memberlist.KVInitService
	ActivityTracker          *activitytracker.ActivityTracker
	UsageStatsReporter       *usagestats.Reporter
	BuildInfoHandler         http.Handler

	// Queryables that the querier should use to query the long term storage.
	StoreQueryables []querier.QueryableWithFilter
}

// New makes a new Mimir.
func New(cfg Config, reg prometheus.Registerer) (*Mimir, error) {
	if cfg.PrintConfig {
		if err := yaml.NewEncoder(os.Stdout).Encode(&cfg); err != nil {
			fmt.Println("Error encoding config:", err)
		}
		os.Exit(0)
	}

	// Swap out the default resolver to support multiple tenant IDs separated by a '|'
	if cfg.TenantFederation.Enabled {
		tenant.WithDefaultResolver(tenant.NewMultiResolver())

		if cfg.Ruler.TenantFederation.Enabled {
			util_log.WarnExperimentalUse("ruler.tenant-federation")
		}
	}

	cfg.API.HTTPAuthMiddleware = noauth.SetupAuthMiddleware(&cfg.Server, cfg.MultitenancyEnabled,
		// Also don't check auth for these gRPC methods, since single call is used for multiple users (or no user like health check).
		[]string{
			"/grpc.health.v1.Health/Check",
			"/frontend.Frontend/Process",
			"/frontend.Frontend/NotifyClientShutdown",
			"/schedulerpb.SchedulerForFrontend/FrontendLoop",
			"/schedulerpb.SchedulerForQuerier/QuerierLoop",
			"/schedulerpb.SchedulerForQuerier/NotifyQuerierShutdown",
		}, cfg.NoAuthTenant)

	// Inject the registerer in the Server config too.
	cfg.Server.Registerer = reg

	mimir := &Mimir{
		Cfg:        cfg,
		Registerer: reg,
	}

	mimir.setupObjstoreTracing()
	otel.SetTracerProvider(NewOpenTelemetryProviderBridge(opentracing.GlobalTracer()))

	if err := mimir.setupModuleManager(); err != nil {
		return nil, err
	}

	return mimir, nil
}

// setupObjstoreTracing appends a gRPC middleware used to inject our tracer into the custom
// context used by thanos-io/objstore, in order to get Objstore spans correctly attached to our traces.
func (t *Mimir) setupObjstoreTracing() {
	t.Cfg.Server.GRPCMiddleware = append(t.Cfg.Server.GRPCMiddleware, ThanosTracerUnaryInterceptor)
	t.Cfg.Server.GRPCStreamMiddleware = append(t.Cfg.Server.GRPCStreamMiddleware, ThanosTracerStreamInterceptor)
}

// Run starts Mimir running, and blocks until a Mimir stops.
func (t *Mimir) Run() error {
	// Register custom process metrics.
	if c, err := process.NewProcessCollector(); err == nil {
		if t.Registerer != nil {
			t.Registerer.MustRegister(c)
		}
	} else {
		level.Warn(util_log.Logger).Log("msg", "skipped registration of custom process metrics collector", "err", err)
	}

	// Update the usage stats before we initialize modules.
	usagestats.SetTarget(t.Cfg.Target.String())

	for _, module := range t.Cfg.Target {
		if !t.ModuleManager.IsUserVisibleModule(module) {
			return fmt.Errorf("selected target (%s) is an internal module, which is not allowed", module)
		}
	}

	var err error
	t.ServiceMap, err = t.ModuleManager.InitModuleServices(t.Cfg.Target...)
	if err != nil {
		return err
	}

	t.API.RegisterServiceMapHandler(http.HandlerFunc(t.servicesHandler))

	// register ingester ring handlers, if they exists prefer the full ring
	// implementation provided by module.Ring over the BasicLifecycler
	// available in ingesters
	if t.Ring != nil {
		t.API.RegisterRing(t.Ring)
	} else if t.Ingester != nil {
		t.API.RegisterRing(t.Ingester.RingHandler())
	}

	// get all services, create service manager and tell it to start
	servs := []services.Service(nil)
	for _, s := range t.ServiceMap {
		servs = append(servs, s)
	}

	sm, err := services.NewManager(servs...)
	if err != nil {
		return err
	}

	// Used to delay shutdown but return "not ready" during this delay.
	shutdownRequested := atomic.NewBool(false)

	// before starting servers, register /ready handler and gRPC health check service.
	t.Server.HTTP.Path("/ready").Handler(t.readyHandler(sm, shutdownRequested))
	grpc_health_v1.RegisterHealthServer(t.Server.GRPC, grpcutil.NewHealthCheckFrom(
		grpcutil.WithShutdownRequested(shutdownRequested),
		grpcutil.WithManager(sm),
	))

	// Let's listen for events from this manager, and log them.
	healthy := func() { level.Info(util_log.Logger).Log("msg", "Application started") }
	stopped := func() { level.Info(util_log.Logger).Log("msg", "Application stopped") }
	serviceFailed := func(service services.Service) {
		// if any service fails, stop entire Mimir
		sm.StopAsync()

		// let's find out which module failed
		for m, s := range t.ServiceMap {
			if s == service {
				if errors.Is(service.FailureCase(), modules.ErrStopProcess) {
					level.Info(util_log.Logger).Log("msg", "received stop signal via return error", "module", m, "err", service.FailureCase())
				} else {
					level.Error(util_log.Logger).Log("msg", "module failed", "module", m, "err", service.FailureCase())
				}
				return
			}
		}

		level.Error(util_log.Logger).Log("msg", "module failed", "module", "unknown", "err", service.FailureCase())
	}

	sm.AddListener(services.NewManagerListener(healthy, stopped, serviceFailed))

	// Setup signal handler to gracefully shutdown in response to SIGTERM or SIGINT
	handler := signals.NewHandler(t.Server.Log)
	go func() {
		handler.Loop()

		shutdownRequested.Store(true)
		t.Server.HTTPServer.SetKeepAlivesEnabled(false)

		if t.Cfg.ShutdownDelay > 0 {
			time.Sleep(t.Cfg.ShutdownDelay)
		}

		sm.StopAsync()
	}()

	// Start all services. This can really only fail if some service is already
	// in other state than New, which should not be the case.
	err = sm.StartAsync(context.Background())
	if err == nil {
		// Wait until service manager stops. It can stop in two ways:
		// 1) Signal is received and manager is stopped.
		// 2) Any service fails.
		err = sm.AwaitStopped(context.Background())
	}

	// If there is no error yet (= service manager started and then stopped without problems),
	// but any service failed, report that failure as an error to caller.
	if err == nil {
		if failed := sm.ServicesByState()[services.Failed]; len(failed) > 0 {
			for _, f := range failed {
				if !errors.Is(f.FailureCase(), modules.ErrStopProcess) {
					// Details were reported via failure listener before
					err = errors.New("failed services")
					break
				}
			}
		}
	}
	return err
}

func (t *Mimir) readyHandler(sm *services.Manager, shutdownRequested *atomic.Bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if shutdownRequested.Load() {
			level.Debug(util_log.Logger).Log("msg", "application is stopping")
			http.Error(w, "Application is stopping", http.StatusServiceUnavailable)
			return
		}

		if !sm.IsHealthy() {
			var serviceNamesStates []string
			for name, s := range t.ServiceMap {
				if s.State() != services.Running {
					serviceNamesStates = append(serviceNamesStates, fmt.Sprintf("%s: %s", name, s.State()))
				}
			}

			level.Debug(util_log.Logger).Log("msg", "some services are not Running", "services", strings.Join(serviceNamesStates, ", "))
			httpResponse := "Some services are not Running:\n" + strings.Join(serviceNamesStates, "\n")
			http.Error(w, httpResponse, http.StatusServiceUnavailable)
			return
		}

		// Ingester has a special check that makes sure that it was able to register into the ring,
		// and that all other ring entries are OK too.
		if t.Ingester != nil {
			if err := t.Ingester.CheckReady(r.Context()); err != nil {
				http.Error(w, "Ingester not ready: "+err.Error(), http.StatusServiceUnavailable)
				return
			}
		}

		// Query Frontend has a special check that makes sure that a querier is attached before it signals
		// itself as ready
		if t.Frontend != nil {
			if err := t.Frontend.CheckReady(r.Context()); err != nil {
				http.Error(w, "Query Frontend not ready: "+err.Error(), http.StatusServiceUnavailable)
				return
			}
		}

		util.WriteTextResponse(w, "ready")
	}
}
