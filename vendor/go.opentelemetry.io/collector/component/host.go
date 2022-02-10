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

package component // import "go.opentelemetry.io/collector/component"

import (
	"go.opentelemetry.io/collector/config"
)

// Host represents the entity that is hosting a Component. It is used to allow communication
// between the Component and its host (normally the service.Collector is the host).
type Host interface {
	// ReportFatalError is used to report to the host that the component
	// encountered a fatal error (i.e.: an error that the instance can't recover
	// from) after its start function had already returned.
	//
	// ReportFatalError should be called by the component anytime after Component.Start() ends and
	// before Component.Shutdown() begins.
	ReportFatalError(err error)

	// GetFactory of the specified kind. Returns the factory for a component type.
	// This allows components to create other components. For example:
	//   func (r MyReceiver) Start(host component.Host) error {
	//     apacheFactory := host.GetFactory(KindReceiver,"apache").(component.ReceiverFactory)
	//     receiver, err := apacheFactory.CreateMetricsReceiver(...)
	//     ...
	//   }
	//
	// GetFactory can be called by the component anytime after Component.Start() begins and
	// until Component.Shutdown() ends. Note that the component is responsible for destroying
	// other components that it creates.
	GetFactory(kind Kind, componentType config.Type) Factory

	// GetExtensions returns the map of extensions. Only enabled and created extensions will be returned.
	// Typically is used to find an extension by type or by full config name. Both cases
	// can be done by iterating the returned map. There are typically very few extensions,
	// so there are no performance implications due to iteration.
	//
	// GetExtensions can be called by the component anytime after Component.Start() begins and
	// until Component.Shutdown() ends.
	GetExtensions() map[config.ComponentID]Extension

	// GetExporters returns the map of exporters. Only enabled and created exporters will be returned.
	// Typically is used to find exporters by type or by full config name. Both cases
	// can be done by iterating the returned map. There are typically very few exporters,
	// so there are no performance implications due to iteration.
	// This returns a map by DataType of maps by exporter configs to the exporter instance.
	// Note that an exporter with the same name may be attached to multiple pipelines and
	// thus we may have an instance of the exporter for multiple data types.
	// This is an experimental function that may change or even be removed completely.
	//
	// GetExporters can be called by the component anytime after Component.Start() begins and
	// until Component.Shutdown() ends.
	GetExporters() map[config.DataType]map[config.ComponentID]Exporter
}
