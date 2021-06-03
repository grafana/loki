package cortex

import (
	"errors"
	"io"
	"net/http"

	"gopkg.in/yaml.v2"

	"github.com/cortexproject/cortex/pkg/ingester"
	"github.com/cortexproject/cortex/pkg/ring/kv"
	"github.com/cortexproject/cortex/pkg/util"
	"github.com/cortexproject/cortex/pkg/util/runtimeconfig"
	"github.com/cortexproject/cortex/pkg/util/validation"
)

var (
	errMultipleDocuments = errors.New("the provided runtime configuration contains multiple documents")
)

// runtimeConfigValues are values that can be reloaded from configuration file while Cortex is running.
// Reloading is done by runtime_config.Manager, which also keeps the currently loaded config.
// These values are then pushed to the components that are interested in them.
type runtimeConfigValues struct {
	TenantLimits map[string]*validation.Limits `yaml:"overrides"`

	Multi kv.MultiRuntimeConfig `yaml:"multi_kv_config"`

	IngesterChunkStreaming *bool `yaml:"ingester_stream_chunks_when_using_blocks"`

	IngesterLimits ingester.InstanceLimits `yaml:"ingester_limits"`
}

// runtimeConfigTenantLimits provides per-tenant limit overrides based on a runtimeconfig.Manager
// that reads limits from a configuration file on disk and periodically reloads them.
type runtimeConfigTenantLimits struct {
	manager *runtimeconfig.Manager
}

// newTenantLimits creates a new validation.TenantLimits that loads per-tenant limit overrides from
// a runtimeconfig.Manager
func newTenantLimits(manager *runtimeconfig.Manager) validation.TenantLimits {
	return &runtimeConfigTenantLimits{
		manager: manager,
	}
}

func (l *runtimeConfigTenantLimits) ByUserID(userID string) *validation.Limits {
	return l.AllByUserID()[userID]
}

func (l *runtimeConfigTenantLimits) AllByUserID() map[string]*validation.Limits {
	cfg, ok := l.manager.GetConfig().(*runtimeConfigValues)
	if cfg != nil && ok {
		return cfg.TenantLimits
	}

	return nil
}

func loadRuntimeConfig(r io.Reader) (interface{}, error) {
	var overrides = &runtimeConfigValues{}

	decoder := yaml.NewDecoder(r)
	decoder.SetStrict(true)

	// Decode the first document. An empty document (EOF) is OK.
	if err := decoder.Decode(&overrides); err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}

	// Ensure the provided YAML config is not composed of multiple documents,
	if err := decoder.Decode(&runtimeConfigValues{}); !errors.Is(err, io.EOF) {
		return nil, errMultipleDocuments
	}

	return overrides, nil
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

func ingesterChunkStreaming(manager *runtimeconfig.Manager) func() ingester.QueryStreamType {
	if manager == nil {
		return nil
	}

	return func() ingester.QueryStreamType {
		val := manager.GetConfig()
		if cfg, ok := val.(*runtimeConfigValues); ok && cfg != nil {
			if cfg.IngesterChunkStreaming == nil {
				return ingester.QueryStreamDefault
			}

			if *cfg.IngesterChunkStreaming {
				return ingester.QueryStreamChunks
			}
			return ingester.QueryStreamSamples
		}

		return ingester.QueryStreamDefault
	}
}

func ingesterInstanceLimits(manager *runtimeconfig.Manager) func() *ingester.InstanceLimits {
	if manager == nil {
		return nil
	}

	return func() *ingester.InstanceLimits {
		val := manager.GetConfig()
		if cfg, ok := val.(*runtimeConfigValues); ok && cfg != nil {
			return &cfg.IngesterLimits
		}
		return nil
	}
}

func runtimeConfigHandler(runtimeCfgManager *runtimeconfig.Manager, defaultLimits validation.Limits) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cfg, ok := runtimeCfgManager.GetConfig().(*runtimeConfigValues)
		if !ok || cfg == nil {
			util.WriteTextResponse(w, "runtime config file doesn't exist")
			return
		}

		var output interface{}
		switch r.URL.Query().Get("mode") {
		case "diff":
			// Default runtime config is just empty struct, but to make diff work,
			// we set defaultLimits for every tenant that exists in runtime config.
			defaultCfg := runtimeConfigValues{}
			defaultCfg.TenantLimits = map[string]*validation.Limits{}
			for k, v := range cfg.TenantLimits {
				if v != nil {
					defaultCfg.TenantLimits[k] = &defaultLimits
				}
			}

			cfgYaml, err := util.YAMLMarshalUnmarshal(cfg)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			defaultCfgYaml, err := util.YAMLMarshalUnmarshal(defaultCfg)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			output, err = util.DiffConfig(defaultCfgYaml, cfgYaml)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

		default:
			output = cfg
		}
		util.WriteYAMLResponse(w, output)
	}
}
