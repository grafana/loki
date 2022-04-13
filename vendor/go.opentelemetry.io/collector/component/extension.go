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
	"context"

	"go.opentelemetry.io/collector/config"
)

// Extension is the interface for objects hosted by the OpenTelemetry Collector that
// don't participate directly on data pipelines but provide some functionality
// to the service, examples: health check endpoint, z-pages, etc.
type Extension interface {
	Component
}

// PipelineWatcher is an extra interface for Extension hosted by the OpenTelemetry
// Collector that is to be implemented by extensions interested in changes to pipeline
// states. Typically this will be used by extensions that change their behavior if data is
// being ingested or not, e.g.: a k8s readiness probe.
type PipelineWatcher interface {
	// Ready notifies the Extension that all pipelines were built and the
	// receivers were started, i.e.: the service is ready to receive data
	// (note that it may already have received data when this method is called).
	Ready() error

	// NotReady notifies the Extension that all receivers are about to be stopped,
	// i.e.: pipeline receivers will not accept new data.
	// This is sent before receivers are stopped, so the Extension can take any
	// appropriate actions before that happens.
	NotReady() error
}

// ExtensionCreateSettings is passed to ExtensionFactory.Create* functions.
type ExtensionCreateSettings struct {
	TelemetrySettings

	// BuildInfo can be used by components for informational purposes
	BuildInfo BuildInfo
}

// ExtensionDefaultConfigFunc is the equivalent of component.ExtensionFactory.CreateDefaultConfig()
type ExtensionDefaultConfigFunc func() config.Extension

// CreateDefaultConfig implements ExtensionFactory.CreateDefaultConfig()
func (f ExtensionDefaultConfigFunc) CreateDefaultConfig() config.Extension {
	return f()
}

// CreateExtensionFunc is the equivalent of component.ExtensionFactory.CreateExtension()
type CreateExtensionFunc func(context.Context, ExtensionCreateSettings, config.Extension) (Extension, error)

// CreateExtension implements ExtensionFactory.CreateExtension.
func (f CreateExtensionFunc) CreateExtension(ctx context.Context, set ExtensionCreateSettings, cfg config.Extension) (Extension, error) {
	return f(ctx, set, cfg)
}

// ExtensionFactory is a factory for extensions to the service.
type ExtensionFactory interface {
	Factory

	// CreateDefaultConfig creates the default configuration for the Extension.
	// This method can be called multiple times depending on the pipeline
	// configuration and should not cause side-effects that prevent the creation
	// of multiple instances of the Extension.
	// The object returned by this method needs to pass the checks implemented by
	// 'configtest.CheckConfigStruct'. It is recommended to have these checks in the
	// tests of any implementation of the Factory interface.
	CreateDefaultConfig() config.Extension

	// CreateExtension creates an extension based on the given config.
	CreateExtension(ctx context.Context, set ExtensionCreateSettings, cfg config.Extension) (Extension, error)
}

type extensionFactory struct {
	baseFactory
	ExtensionDefaultConfigFunc
	CreateExtensionFunc
}

func NewExtensionFactory(
	cfgType config.Type,
	createDefaultConfig ExtensionDefaultConfigFunc,
	createServiceExtension CreateExtensionFunc) ExtensionFactory {
	return &extensionFactory{
		baseFactory:                baseFactory{cfgType: cfgType},
		ExtensionDefaultConfigFunc: createDefaultConfig,
		CreateExtensionFunc:        createServiceExtension,
	}
}
