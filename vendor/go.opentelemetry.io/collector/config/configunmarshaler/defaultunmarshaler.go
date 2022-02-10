// Copyright The OpenTelemetry Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//       http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package configunmarshaler // import "go.opentelemetry.io/collector/config/configunmarshaler"

import (
	"fmt"
	"reflect"

	"go.uber.org/zap/zapcore"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/config"
	"go.opentelemetry.io/collector/config/configtelemetry"
)

// These are errors that can be returned by Unmarshal(). Note that error codes are not part
// of Unmarshal()'s public API, they are for internal unit testing only.
type configErrorCode int

const (
	// Skip 0, start errors codes from 1.
	_ configErrorCode = iota

	errUnmarshalTopLevelStructure
	errUnmarshalExtension
	errUnmarshalReceiver
	errUnmarshalProcessor
	errUnmarshalExporter
	errUnmarshalService
)

type configError struct {
	// the original error.
	error

	// internal error code.
	code configErrorCode
}

// YAML top-level configuration keys.
const (
	// extensionsKeyName is the configuration key name for extensions section.
	extensionsKeyName = "extensions"

	// receiversKeyName is the configuration key name for receivers section.
	receiversKeyName = "receivers"

	// exportersKeyName is the configuration key name for exporters section.
	exportersKeyName = "exporters"

	// processorsKeyName is the configuration key name for processors section.
	processorsKeyName = "processors"

	// pipelinesKeyName is the configuration key name for pipelines section.
	pipelinesKeyName = "pipelines"
)

type configSettings struct {
	Receivers  map[config.ComponentID]map[string]interface{} `mapstructure:"receivers"`
	Processors map[config.ComponentID]map[string]interface{} `mapstructure:"processors"`
	Exporters  map[config.ComponentID]map[string]interface{} `mapstructure:"exporters"`
	Extensions map[config.ComponentID]map[string]interface{} `mapstructure:"extensions"`
	Service    map[string]interface{}                        `mapstructure:"service"`
}

type defaultUnmarshaler struct{}

// NewDefault returns a default ConfigUnmarshaler that unmarshalls every configuration
// using the custom unmarshaler if present or default to strict
func NewDefault() ConfigUnmarshaler {
	return &defaultUnmarshaler{}
}

// Unmarshal the Config from a config.Map.
// After the config is unmarshalled, `Validate()` must be called to validate.
func (*defaultUnmarshaler) Unmarshal(v *config.Map, factories component.Factories) (*config.Config, error) {
	var cfg config.Config

	// Unmarshal top level sections and validate.
	rawCfg := configSettings{}
	if err := v.UnmarshalExact(&rawCfg); err != nil {
		return nil, &configError{
			error: fmt.Errorf("error reading top level configuration sections: %w", err),
			code:  errUnmarshalTopLevelStructure,
		}
	}

	var err error
	if cfg.Extensions, err = unmarshalExtensions(rawCfg.Extensions, factories.Extensions); err != nil {
		return nil, &configError{
			error: err,
			code:  errUnmarshalExtension,
		}
	}

	if cfg.Receivers, err = unmarshalReceivers(rawCfg.Receivers, factories.Receivers); err != nil {
		return nil, &configError{
			error: err,
			code:  errUnmarshalReceiver,
		}
	}

	if cfg.Processors, err = unmarshalProcessors(rawCfg.Processors, factories.Processors); err != nil {
		return nil, &configError{
			error: err,
			code:  errUnmarshalProcessor,
		}
	}

	if cfg.Exporters, err = unmarshalExporters(rawCfg.Exporters, factories.Exporters); err != nil {
		return nil, &configError{
			error: err,
			code:  errUnmarshalExporter,
		}
	}

	if cfg.Service, err = unmarshalService(rawCfg.Service); err != nil {
		return nil, &configError{
			error: err,
			code:  errUnmarshalService,
		}
	}

	return &cfg, nil
}

func unmarshalExtensions(exts map[config.ComponentID]map[string]interface{}, factories map[config.Type]component.ExtensionFactory) (map[config.ComponentID]config.Extension, error) {
	// Prepare resulting map.
	extensions := make(map[config.ComponentID]config.Extension)

	// Iterate over extensions and create a config for each.
	for id, value := range exts {
		// Find extension factory based on "type" that we read from config source.
		factory := factories[id.Type()]
		if factory == nil {
			return nil, errorUnknownType(extensionsKeyName, id, reflect.ValueOf(factories).MapKeys())
		}

		// Create the default config for this extension.
		extensionCfg := factory.CreateDefaultConfig()
		extensionCfg.SetIDName(id.Name())

		// Now that the default config struct is created we can Unmarshal into it,
		// and it will apply user-defined config on top of the default.
		if err := unmarshal(config.NewMapFromStringMap(value), extensionCfg); err != nil {
			return nil, errorUnmarshalError(extensionsKeyName, id, err)
		}

		extensions[id] = extensionCfg
	}

	return extensions, nil
}

func unmarshalService(srvRaw map[string]interface{}) (config.Service, error) {
	// Setup default telemetry values as in service/logger.go.
	// TODO: Add a component.ServiceFactory to allow this to be defined by the Service.
	srv := config.Service{
		Telemetry: config.ServiceTelemetry{
			Logs: config.ServiceTelemetryLogs{
				Level:             zapcore.InfoLevel,
				Development:       false,
				Encoding:          "console",
				OutputPaths:       []string{"stderr"},
				ErrorOutputPaths:  []string{"stderr"},
				DisableCaller:     false,
				DisableStacktrace: false,
				InitialFields:     map[string]interface{}(nil),
			},
			Metrics: defaultServiceTelemetryMetricsSettings(),
		},
	}

	if err := unmarshal(config.NewMapFromStringMap(srvRaw), &srv); err != nil {
		return srv, fmt.Errorf("error reading service configuration: %w", err)
	}

	for id := range srv.Pipelines {
		if id.Type() != config.TracesDataType && id.Type() != config.MetricsDataType && id.Type() != config.LogsDataType {
			return srv, fmt.Errorf("unknown %q datatype %q for %v", pipelinesKeyName, id.Type(), id)
		}
	}
	return srv, nil
}

func defaultServiceTelemetryMetricsSettings() config.ServiceTelemetryMetrics {
	return config.ServiceTelemetryMetrics{
		Level:   configtelemetry.LevelBasic, //nolint:staticcheck
		Address: ":8888",
	}
}

// LoadReceiver loads a receiver config from componentConfig using the provided factories.
func LoadReceiver(componentConfig *config.Map, id config.ComponentID, factory component.ReceiverFactory) (config.Receiver, error) {
	// Create the default config for this receiver.
	receiverCfg := factory.CreateDefaultConfig()
	receiverCfg.SetIDName(id.Name())

	// Now that the default config struct is created we can Unmarshal into it,
	// and it will apply user-defined config on top of the default.
	if err := unmarshal(componentConfig, receiverCfg); err != nil {
		return nil, errorUnmarshalError(receiversKeyName, id, err)
	}

	return receiverCfg, nil
}

func unmarshalReceivers(recvs map[config.ComponentID]map[string]interface{}, factories map[config.Type]component.ReceiverFactory) (map[config.ComponentID]config.Receiver, error) {
	// Prepare resulting map.
	receivers := make(map[config.ComponentID]config.Receiver)

	// Iterate over input map and create a config for each.
	for id, value := range recvs {
		// Find receiver factory based on "type" that we read from config source.
		factory := factories[id.Type()]
		if factory == nil {
			return nil, errorUnknownType(receiversKeyName, id, reflect.ValueOf(factories).MapKeys())
		}

		receiverCfg, err := LoadReceiver(config.NewMapFromStringMap(value), id, factory)
		if err != nil {
			// LoadReceiver already wraps the error.
			return nil, err
		}

		receivers[id] = receiverCfg
	}

	return receivers, nil
}

func unmarshalExporters(exps map[config.ComponentID]map[string]interface{}, factories map[config.Type]component.ExporterFactory) (map[config.ComponentID]config.Exporter, error) {
	// Prepare resulting map.
	exporters := make(map[config.ComponentID]config.Exporter)

	// Iterate over Exporters and create a config for each.
	for id, value := range exps {
		// Find exporter factory based on "type" that we read from config source.
		factory := factories[id.Type()]
		if factory == nil {
			return nil, errorUnknownType(exportersKeyName, id, reflect.ValueOf(factories).MapKeys())
		}

		// Create the default config for this exporter.
		exporterCfg := factory.CreateDefaultConfig()
		exporterCfg.SetIDName(id.Name())

		// Now that the default config struct is created we can Unmarshal into it,
		// and it will apply user-defined config on top of the default.
		if err := unmarshal(config.NewMapFromStringMap(value), exporterCfg); err != nil {
			return nil, errorUnmarshalError(exportersKeyName, id, err)
		}

		exporters[id] = exporterCfg
	}

	return exporters, nil
}

func unmarshalProcessors(procs map[config.ComponentID]map[string]interface{}, factories map[config.Type]component.ProcessorFactory) (map[config.ComponentID]config.Processor, error) {
	// Prepare resulting map.
	processors := make(map[config.ComponentID]config.Processor)

	// Iterate over processors and create a config for each.
	for id, value := range procs {
		// Find processor factory based on "type" that we read from config source.
		factory := factories[id.Type()]
		if factory == nil {
			return nil, errorUnknownType(processorsKeyName, id, reflect.ValueOf(factories).MapKeys())
		}

		// Create the default config for this processor.
		processorCfg := factory.CreateDefaultConfig()
		processorCfg.SetIDName(id.Name())

		// Now that the default config struct is created we can Unmarshal into it,
		// and it will apply user-defined config on top of the default.
		if err := unmarshal(config.NewMapFromStringMap(value), processorCfg); err != nil {
			return nil, errorUnmarshalError(processorsKeyName, id, err)
		}

		processors[id] = processorCfg
	}

	return processors, nil
}

func unmarshal(componentSection *config.Map, intoCfg interface{}) error {
	if cu, ok := intoCfg.(config.Unmarshallable); ok {
		return cu.Unmarshal(componentSection)
	}

	return componentSection.UnmarshalExact(intoCfg)
}

func errorUnknownType(component string, id config.ComponentID, factories []reflect.Value) error {
	return fmt.Errorf("unknown %s type %q for %q (valid values: %v)", component, id.Type(), id, factories)
}

func errorUnmarshalError(component string, id config.ComponentID, err error) error {
	return fmt.Errorf("error reading %s configuration for %q: %w", component, id, err)
}
