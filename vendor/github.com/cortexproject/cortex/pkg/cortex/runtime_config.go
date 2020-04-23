package cortex

import (
	"os"

	"gopkg.in/yaml.v2"

	"github.com/cortexproject/cortex/pkg/ring/kv"
	"github.com/cortexproject/cortex/pkg/util/runtimeconfig"
	"github.com/cortexproject/cortex/pkg/util/validation"
)

// runtimeConfigValues are values that can be reloaded from configuration file while Cortex is running.
// Reloading is done by runtime_config.Manager, which also keeps the currently loaded config.
// These values are then pushed to the components that are interested in them.
type runtimeConfigValues struct {
	TenantLimits map[string]*validation.Limits `yaml:"overrides"`

	Multi kv.MultiRuntimeConfig `yaml:"multi_kv_config"`
}

func loadRuntimeConfig(filename string) (interface{}, error) {
	f, err := os.Open(filename)
	if err != nil {
		return nil, err
	}

	var overrides = &runtimeConfigValues{}

	decoder := yaml.NewDecoder(f)
	decoder.SetStrict(true)
	if err := decoder.Decode(&overrides); err != nil {
		return nil, err
	}

	return overrides, nil
}

func tenantLimitsFromRuntimeConfig(c *runtimeconfig.Manager) validation.TenantLimits {
	return func(userID string) *validation.Limits {
		cfg, ok := c.GetConfig().(*runtimeConfigValues)
		if !ok || cfg == nil {
			return nil
		}

		return cfg.TenantLimits[userID]
	}
}

func multiClientRuntimeConfigChannel(manager *runtimeconfig.Manager) func() <-chan kv.MultiRuntimeConfig {
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
