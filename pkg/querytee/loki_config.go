package querytee

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/drone/envsubst"
	"github.com/go-kit/log"
	"github.com/grafana/dskit/flagext"
	"github.com/grafana/dskit/runtimeconfig"
	"github.com/grafana/dskit/services"
	"github.com/prometheus/client_golang/prometheus"
	"go.yaml.in/yaml/v4"

	"github.com/grafana/loki/v4/pkg/validation"
)

// LimitsConfig configures how querytee loads the limits of a Loki cluster. The flag names match the
// ones in Loki. The config file is read for the limits_config block only. The runtime config is read
// for the overrides only. When neither file is set, querytee loads no limits.
type LimitsConfig struct {
	ConfigFile    string
	ExpandEnv     bool
	RuntimeConfig runtimeconfig.Config
}

// RegisterFlags registers the flags of the limits config.
func (cfg *LimitsConfig) RegisterFlags(f *flag.FlagSet) {
	f.StringVar(&cfg.ConfigFile, "config.file", "", "Path of a Loki config file. querytee reads only the limits_config block.")
	f.BoolVar(&cfg.ExpandEnv, "config.expand-env", false, "Expand ${var} or $var in the Loki config file with the value of the environment variable.")
	cfg.RuntimeConfig.RegisterFlags(f)
}

// lokiConfig is the part of a Loki config file that querytee reads.
type lokiConfig struct {
	LimitsConfig validation.Limits `yaml:"limits_config"`
}

// overridesConfig is the part of a Loki runtime config file that querytee reads.
type overridesConfig struct {
	TenantLimits map[string]*validation.Limits `yaml:"overrides"`
}

// LokiLimits holds the limits that querytee loaded from the config files of a Loki cluster.
//
// The runtime config reloads while querytee runs, so Overrides sees changed per-tenant limits.
// The limits_config defaults stay as loaded at start-up.
type LokiLimits struct {
	// Overrides resolves the limits of a tenant: the per-tenant overrides of the runtime config on
	// top of the defaults of limits_config.
	Overrides *validation.Overrides

	runtimeConfig *runtimeconfig.Manager
}

// LoadLokiLimits loads the limits from the Loki config and runtime config files that the config names.
// It waits for the first load of the runtime config. It returns nil when the config names no file.
func LoadLokiLimits(ctx context.Context, cfg LimitsConfig, logger log.Logger, registerer prometheus.Registerer) (*LokiLimits, error) {
	if cfg.ConfigFile == "" && len(cfg.RuntimeConfig.LoadPath) == 0 {
		return nil, nil
	}

	var fileCfg lokiConfig
	flagext.DefaultValues(&fileCfg.LimitsConfig)
	if cfg.ConfigFile != "" {
		if err := readLokiConfig(cfg.ConfigFile, cfg.ExpandEnv, &fileCfg); err != nil {
			return nil, fmt.Errorf("reading the Loki config file %s: %w", cfg.ConfigFile, err)
		}
	}

	// A tenant override inherits the defaults of limits_config, as it does in Loki.
	validation.SetDefaultLimitsForYAMLUnmarshalling(fileCfg.LimitsConfig)

	parsed := &LokiLimits{}

	var tenantLimits validation.TenantLimits
	if len(cfg.RuntimeConfig.LoadPath) > 0 {
		if cfg.RuntimeConfig.ReloadPeriod <= 0 {
			return nil, fmt.Errorf("-runtime-config.reload-period must be positive, got %s", cfg.RuntimeConfig.ReloadPeriod)
		}
		cfg.RuntimeConfig.Loader = loadOverrides
		manager, err := runtimeconfig.New(cfg.RuntimeConfig, "loki-querytee", prometheus.WrapRegistererWithPrefix("loki_querytee_", registerer), logger)
		if err != nil {
			return nil, fmt.Errorf("creating the runtime config manager: %w", err)
		}
		if err := services.StartAndAwaitRunning(ctx, manager); err != nil {
			return nil, fmt.Errorf("loading the runtime config: %w", err)
		}
		parsed.runtimeConfig = manager
		tenantLimits = &tenantLimitsFromRuntimeConfig{manager: manager}
	}

	overrides, err := validation.NewOverrides(fileCfg.LimitsConfig, tenantLimits)
	if err != nil {
		return nil, err
	}
	parsed.Overrides = overrides

	return parsed, nil
}

// Stop stops the reload of the runtime config. It is safe to call on a nil LokiLimits.
func (l *LokiLimits) Stop(ctx context.Context) error {
	if l == nil || l.runtimeConfig == nil {
		return nil
	}
	return services.StopAndAwaitTerminated(ctx, l.runtimeConfig)
}

// readLokiConfig decodes the file into dst. It ignores the keys that querytee does not read.
func readLokiConfig(path string, expandEnv bool, dst *lokiConfig) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	if expandEnv {
		expanded, err := envsubst.EvalEnv(string(raw))
		if err != nil {
			return err
		}
		raw = []byte(expanded)
	}

	if err := yaml.NewDecoder(bytes.NewReader(raw)).Decode(dst); err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	return nil
}

func loadOverrides(r io.Reader) (interface{}, error) {
	cfg := &overridesConfig{}
	if err := yaml.NewDecoder(r).Decode(cfg); err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	return cfg, nil
}

// tenantLimitsFromRuntimeConfig implements validation.TenantLimits.
type tenantLimitsFromRuntimeConfig struct {
	manager *runtimeconfig.Manager
}

func (t *tenantLimitsFromRuntimeConfig) AllByUserID() map[string]*validation.Limits {
	cfg, ok := t.manager.GetConfig().(*overridesConfig)
	if !ok || cfg == nil {
		return nil
	}
	return cfg.TenantLimits
}

func (t *tenantLimitsFromRuntimeConfig) TenantLimits(userID string) *validation.Limits {
	return t.AllByUserID()[userID]
}
