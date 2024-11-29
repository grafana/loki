package runtime

import (
	"flag"
)

type Config struct {
	LogStreamCreation      bool `yaml:"log_stream_creation"`
	LogPushRequest         bool `yaml:"log_push_request"`
	LogPushRequestStreams  bool `yaml:"log_push_request_streams"`
	LogDuplicateMetrics    bool `yaml:"log_duplicate_metrics"`
	LogDuplicateStreamInfo bool `yaml:"log_duplicate_stream_info"`

	// LimitedLogPushErrors is to be implemented and will allow logging push failures at a controlled pace.
	LimitedLogPushErrors bool `yaml:"limited_log_push_errors"`
}

// RegisterFlags adds the flags required to config this to the given FlagSet
func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	f.BoolVar(&cfg.LogStreamCreation, "operation-config.log-stream-creation", false, "Log every new stream created by a push request (very verbose, recommend to enable via runtime config only).")
	f.BoolVar(&cfg.LogPushRequest, "operation-config.log-push-request", false, "Log every push request (very verbose, recommend to enable via runtime config only).")
	f.BoolVar(&cfg.LogPushRequestStreams, "operation-config.log-push-request-streams", false, "Log every stream in a push request (very verbose, recommend to enable via runtime config only).")
	f.BoolVar(&cfg.LogDuplicateMetrics, "operation-config.log-duplicate-metrics", false, "Log metrics for duplicate lines received.")
	f.BoolVar(&cfg.LogDuplicateStreamInfo, "operation-config.log-duplicate-stream-info", false, "Log stream info for duplicate lines received")
	f.BoolVar(&cfg.LimitedLogPushErrors, "operation-config.limited-log-push-errors", true, "Log push errors with a rate limited logger, will show client push errors without overly spamming logs.")
}

// When we load YAML from disk, we want the various per-customer limits
// to default to any values specified on the command line, not default
// command line values.  This global contains those values.  I (Tom) cannot
// find a nicer way I'm afraid.
var defaultConfig *Config

// SetDefaultLimitsForYAMLUnmarshalling sets global default limits, used when loading
// Limits from YAML files. This is used to ensure per-tenant limits are defaulted to
// those values.
func SetDefaultLimitsForYAMLUnmarshalling(defaults Config) {
	defaultConfig = &defaults
}

// TenantConfigProvider serves a tenant or default config.
type TenantConfigProvider interface {
	TenantConfig(userID string) *Config
}

// TenantConfigs periodically fetch a set of per-user configs, and provides convenience
// functions for fetching the correct value.
type TenantConfigs struct {
	TenantConfigProvider
}

// DefaultTenantConfigs creates and returns a new TenantConfigs with the defaults populated.
// Only useful for testing, the provider will ignore any tenants passed in.
func DefaultTenantConfigs() *TenantConfigs {
	return &TenantConfigs{
		TenantConfigProvider: &defaultsOnlyConfigProvider{},
	}
}

type defaultsOnlyConfigProvider struct {
}

// TenantConfig implementation for defaultsOnlyConfigProvider, ignores the tenant input and only returns a default config
func (t *defaultsOnlyConfigProvider) TenantConfig(_ string) *Config {
	if defaultConfig == nil {
		defaultConfig = &Config{}
		defaultConfig.RegisterFlags(flag.NewFlagSet("", flag.PanicOnError))
	}
	return defaultConfig
}

// NewTenantConfigs makes a new TenantConfigs
func NewTenantConfigs(configProvider TenantConfigProvider) (*TenantConfigs, error) {
	return &TenantConfigs{
		TenantConfigProvider: configProvider,
	}, nil
}

func (o *TenantConfigs) getOverridesForUser(userID string) *Config {
	if o.TenantConfigProvider != nil {
		l := o.TenantConfigProvider.TenantConfig(userID)
		if l != nil {
			return l
		}
	}
	return defaultConfig
}

func (o *TenantConfigs) LogStreamCreation(userID string) bool {
	return o.getOverridesForUser(userID).LogStreamCreation
}

func (o *TenantConfigs) LogPushRequest(userID string) bool {
	return o.getOverridesForUser(userID).LogPushRequest
}

func (o *TenantConfigs) LogPushRequestStreams(userID string) bool {
	return o.getOverridesForUser(userID).LogPushRequestStreams
}

func (o *TenantConfigs) LogDuplicateMetrics(userID string) bool {
	return o.getOverridesForUser(userID).LogDuplicateMetrics
}

func (o *TenantConfigs) LogDuplicateStreamInfo(userID string) bool {
	return o.getOverridesForUser(userID).LogDuplicateStreamInfo
}

func (o *TenantConfigs) LimitedLogPushErrors(userID string) bool {
	return o.getOverridesForUser(userID).LimitedLogPushErrors
}
