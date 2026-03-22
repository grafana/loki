// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package component // import "go.opentelemetry.io/collector/component"

// Host represents the entity that is hosting a Component. It is used to allow communication
// between the Component and its host (normally the service.Collector is the host).
//
// Components may require `component.Host` to implement additional interfaces to properly function.
// The component is expected to cast the `component.Host` to the interface it needs and return
// an error if the type assertion fails.
type Host interface {
	// GetExtensions returns the map of extensions. Only enabled and created extensions will be returned.
	// Typically, it is used to find an extension by type or by full config name. Both cases
	// can be done by iterating the returned map. There are typically very few extensions,
	// so there are no performance implications due to iteration.
	//
	// GetExtensions can be called by the component anytime after Component.Start() begins and
	// until Component.Shutdown() ends.
	//
	// The returned map should only be nil if the host does not support extensions at all.
	GetExtensions() map[ID]Component
}
