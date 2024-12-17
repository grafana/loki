package loki

import (
	"fmt"
	"io"

	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/kv"
	"github.com/grafana/dskit/runtimeconfig"
	"gopkg.in/yaml.v2"

	"github.com/grafana/loki/v3/pkg/runtime"
	util_log "github.com/grafana/loki/v3/pkg/util/log"
	"github.com/grafana/loki/v3/pkg/validation"
)

// runtimeConfigValues are values that can be reloaded from configuration file while Loki is running.
// Reloading is done by runtimeconfig.Manager, which also keeps the currently loaded config.
// These values are then pushed to the components that are interested in them.
type runtimeConfigValues struct {
	TenantLimits map[string]*validation.Limits `yaml:"overrides"`
	TenantConfig map[string]*runtime.Config    `yaml:"configs"`

	Multi kv.MultiRuntimeConfig `yaml:"multi_kv_config"`
}

func (r runtimeConfigValues) validate() error {
	for t, c := range r.TenantLimits {
		if c == nil {
			level.Warn(util_log.Logger).Log("msg", "skipping empty tenant limit definition", "tenant", t)
			continue
		}

		if err := c.Validate(); err != nil {
			return fmt.Errorf("invalid override for tenant %s: %w", t, err)
		}
	}
	return nil
}

func loadRuntimeConfig(r io.Reader) (interface{}, error) {
	overrides := &runtimeConfigValues{}

	decoder := yaml.NewDecoder(r)
	decoder.SetStrict(true)
	if err := decoder.Decode(&overrides); err != nil {
		return nil, err
	}
	if err := overrides.validate(); err != nil {
		return nil, err
	}
	return overrides, nil
}

// tenantLimitsFromRuntimeConfig implements validation.Limits
type tenantLimitsFromRuntimeConfig struct {
	c *runtimeconfig.Manager
}

func (t *tenantLimitsFromRuntimeConfig) AllByUserID() map[string]*validation.Limits {
	if t.c == nil {
		return nil
	}

	cfg, ok := t.c.GetConfig().(*runtimeConfigValues)
	if cfg != nil && ok {
		return cfg.TenantLimits
	}

	return nil
}

func (t *tenantLimitsFromRuntimeConfig) TenantLimits(userID string) *validation.Limits {
	allByUserID := t.AllByUserID()
	if allByUserID == nil {
		return nil
	}

	return allByUserID[userID]
}

func newtenantLimitsFromRuntimeConfig(c *runtimeconfig.Manager) validation.TenantLimits {
	return &tenantLimitsFromRuntimeConfig{c: c}
}

type tenantConfigProvider struct {
	c *runtimeconfig.Manager
}

func newTenantConfigProvider(c *runtimeconfig.Manager) runtime.TenantConfigProvider {
	return &tenantConfigProvider{c: c}
}

// TenantConfig returns the user config or default config if none was defined.
func (t *tenantConfigProvider) TenantConfig(userID string) *runtime.Config {
	if t.c == nil {
		return nil
	}

	cfg, ok := t.c.GetConfig().(*runtimeConfigValues)
	if !ok || cfg == nil {
		return nil
	}
	if tenantCfg, ok := cfg.TenantConfig[userID]; ok {
		return tenantCfg
	}
	return nil
}

func multiClientRuntimeConfigChannel(manager *runtimeconfig.Manager) func() <-chan kv.MultiRuntimeConfig {
	if manager == nil {
		return nil
	}
	// returns function that can be used in MultiConfig.ConfigProvider
	return func() <-chan kv.MultiRuntimeConfig {
		outCh := make(chan kv.MultiRuntimeConfig, 1)

		// push initial config to the channel
		val := manager.GetConfig()
		if cfg, ok := val.(*runtimeConfigValues); ok && cfg != nil {
			outCh <- cfg.Multi
		}

		ch := manager.CreateListenerChannel(1)
		go func() {
			for val := range ch {
				if cfg, ok := val.(*runtimeConfigValues); ok && cfg != nil {
					outCh <- cfg.Multi
				}
			}
		}()

		return outCh
	}
}
