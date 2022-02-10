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

// Package sharedcomponent exposes util functionality for receivers and exporters
// that need to share state between different signal types instances such as net.Listener or os.File.
package sharedcomponent // import "go.opentelemetry.io/collector/internal/sharedcomponent"

import (
	"context"
	"sync"

	"go.opentelemetry.io/collector/component"
)

// SharedComponents a map that keeps reference of all created instances for a given configuration,
// and ensures that the shared state is started and stopped only once.
type SharedComponents struct {
	comps map[interface{}]*SharedComponent
}

// NewSharedComponents returns a new empty SharedComponents.
func NewSharedComponents() *SharedComponents {
	return &SharedComponents{
		comps: make(map[interface{}]*SharedComponent),
	}
}

// GetOrAdd returns the already created instance if exists, otherwise creates a new instance
// and adds it to the map of references.
func (scs *SharedComponents) GetOrAdd(key interface{}, create func() component.Component) *SharedComponent {
	if c, ok := scs.comps[key]; ok {
		return c
	}
	newComp := &SharedComponent{
		Component: create(),
		removeFunc: func() {
			delete(scs.comps, key)
		},
	}
	scs.comps[key] = newComp
	return newComp
}

// SharedComponent ensures that the wrapped component is started and stopped only once.
// When stopped it is removed from the SharedComponents map.
type SharedComponent struct {
	component.Component

	startOnce  sync.Once
	stopOnce   sync.Once
	removeFunc func()
}

// Unwrap returns the original component.
func (r *SharedComponent) Unwrap() component.Component {
	return r.Component
}

// Start implements component.Component.
func (r *SharedComponent) Start(ctx context.Context, host component.Host) error {
	var err error
	r.startOnce.Do(func() {
		err = r.Component.Start(ctx, host)
	})
	return err
}

// Shutdown implements component.Component.
func (r *SharedComponent) Shutdown(ctx context.Context) error {
	var err error
	r.stopOnce.Do(func() {
		err = r.Component.Shutdown(ctx)
		r.removeFunc()
	})
	return err
}
