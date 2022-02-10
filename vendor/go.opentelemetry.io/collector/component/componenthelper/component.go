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

package componenthelper // import "go.opentelemetry.io/collector/component/componenthelper"

import (
	"context"

	"go.opentelemetry.io/collector/component"
)

// StartFunc specifies the function invoked when the component.Component is being started.
type StartFunc func(context.Context, component.Host) error

// Start starts the component.
func (f StartFunc) Start(ctx context.Context, host component.Host) error {
	if f == nil {
		return nil
	}
	return f(ctx, host)
}

// ShutdownFunc specifies the function invoked when the component.Component is being shutdown.
type ShutdownFunc func(context.Context) error

// Shutdown shuts down the component.
func (f ShutdownFunc) Shutdown(ctx context.Context) error {
	if f == nil {
		return nil
	}
	return f(ctx)
}

// Option represents the possible options for New.
type Option func(*baseComponent)

// WithStart overrides the default `Start` function for a component.Component.
// The default always returns nil.
func WithStart(startFunc StartFunc) Option {
	return func(o *baseComponent) {
		o.StartFunc = startFunc
	}
}

// WithShutdown overrides the default `Shutdown` function for a component.Component.
// The default always returns nil.
func WithShutdown(shutdownFunc ShutdownFunc) Option {
	return func(o *baseComponent) {
		o.ShutdownFunc = shutdownFunc
	}
}

type baseComponent struct {
	StartFunc
	ShutdownFunc
}

// New returns a component.Component configured with the provided options.
func New(options ...Option) component.Component {
	bc := &baseComponent{}

	for _, op := range options {
		op(bc)
	}

	return bc
}
