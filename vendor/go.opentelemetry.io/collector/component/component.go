// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

// Package component outlines the abstraction of components within the OpenTelemetry Collector. It provides details on the component
// lifecycle as well as defining the interface that components must fulfill.
package component // import "go.opentelemetry.io/collector/component"

import (
	"context"
	"fmt"
	"strings"
)

// Component is either a receiver, exporter, processor, connector, or an extension.
//
// A component's lifecycle has the following phases:
//
//  1. Creation: The component is created using its respective factory, via a Create* call.
//  2. Start: The component's Start method is called.
//  3. Running: The component is up and running.
//  4. Shutdown: The component's Shutdown method is called and the lifecycle is complete.
//
// Once the lifecycle is complete it may be repeated, in which case a new component
// is created, starts, runs and is shutdown again.
type Component interface {
	// Start tells the component to start. Host parameter can be used for communicating
	// with the host after Start() has already returned. If an error is returned by
	// Start() then the collector startup will be aborted.
	// If this is an exporter component it may prepare for exporting
	// by connecting to the endpoint.
	//
	// If the component needs to perform a long-running starting operation then it is recommended
	// that Start() returns quickly and the long-running operation is performed in background.
	// In that case make sure that the long-running operation does not use the context passed
	// to Start() function since that context will be cancelled soon and can abort the long-running
	// operation. Create a new context from the context.Background() for long-running operations.
	Start(ctx context.Context, host Host) error

	// Shutdown is invoked during service shutdown. After Shutdown() is called, if the component
	// accepted data in any way, it should not accept it anymore.
	//
	// This method must be safe to call:
	//   - without Start() having been called
	//   - if the component is in a shutdown state already
	//
	// If there are any background operations running by the component they must be aborted before
	// this function returns. Remember that if you started any long-running background operations from
	// the Start() method, those operations must be also cancelled. If there are any buffers in the
	// component, they should be cleared and the data sent immediately to the next component.
	//
	// The component's lifecycle is completed once the Shutdown() method returns. No other
	// methods of the component are called after that. If necessary a new component with
	// the same or different configuration may be created and started (this may happen
	// for example if we want to restart the component).
	Shutdown(ctx context.Context) error
}

// StartFunc specifies the function invoked when the component.Component is being started.
type StartFunc func(context.Context, Host) error

// Start starts the component.
func (f StartFunc) Start(ctx context.Context, host Host) error {
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

// Kind represents component kinds.
type Kind struct {
	name string
}

var (
	KindReceiver  = Kind{name: "Receiver"}
	KindProcessor = Kind{name: "Processor"}
	KindExporter  = Kind{name: "Exporter"}
	KindExtension = Kind{name: "Extension"}
	KindConnector = Kind{name: "Connector"}
)

func (k Kind) String() string {
	return k.name
}

// StabilityLevel represents the stability level of the component created by the factory.
// The stability level is used to determine if the component should be used in production
// or not. For more details see:
// https://github.com/open-telemetry/opentelemetry-collector/blob/main/docs/component-stability.md#stability-levels
type StabilityLevel int

const (
	StabilityLevelUndefined StabilityLevel = iota // skip 0, start types from 1.
	StabilityLevelUnmaintained
	StabilityLevelDeprecated
	StabilityLevelDevelopment
	StabilityLevelAlpha
	StabilityLevelBeta
	StabilityLevelStable
)

func (sl *StabilityLevel) UnmarshalText(in []byte) error {
	str := strings.ToLower(string(in))
	switch str {
	case "undefined":
		*sl = StabilityLevelUndefined
	case "unmaintained":
		*sl = StabilityLevelUnmaintained
	case "deprecated":
		*sl = StabilityLevelDeprecated
	case "development":
		*sl = StabilityLevelDevelopment
	case "alpha":
		*sl = StabilityLevelAlpha
	case "beta":
		*sl = StabilityLevelBeta
	case "stable":
		*sl = StabilityLevelStable
	default:
		return fmt.Errorf("unsupported stability level: %q", string(in))
	}
	return nil
}

func (sl StabilityLevel) String() string {
	switch sl {
	case StabilityLevelUndefined:
		return "Undefined"
	case StabilityLevelUnmaintained:
		return "Unmaintained"
	case StabilityLevelDeprecated:
		return "Deprecated"
	case StabilityLevelDevelopment:
		return "Development"
	case StabilityLevelAlpha:
		return "Alpha"
	case StabilityLevelBeta:
		return "Beta"
	case StabilityLevelStable:
		return "Stable"
	}
	return ""
}

func (sl StabilityLevel) LogMessage() string {
	switch sl {
	case StabilityLevelUnmaintained:
		return "Unmaintained component. Actively looking for contributors. Component will become deprecated after 3 months of remaining unmaintained."
	case StabilityLevelDeprecated:
		return "Deprecated component. Will be removed in future releases."
	case StabilityLevelDevelopment:
		return "Development component. May change in the future."
	case StabilityLevelAlpha:
		return "Alpha component. May change in the future."
	case StabilityLevelBeta:
		return "Beta component. May change in the future."
	case StabilityLevelStable:
		return "Stable component."
	default:
		return "Stability level of component is undefined"
	}
}

// Factory is implemented by all Component factories.
type Factory interface {
	// Type gets the type of the component created by this factory.
	Type() Type

	// CreateDefaultConfig creates the default configuration for the Component.
	// This method can be called multiple times depending on the pipeline
	// configuration and should not cause side effects that prevent the creation
	// of multiple instances of the Component.
	// The object returned by this method needs to pass the checks implemented by
	// 'componenttest.CheckConfigStruct'. It is recommended to have these checks in the
	// tests of any implementation of the Factory interface.
	CreateDefaultConfig() Config
}

// CreateDefaultConfigFunc is the equivalent of Factory.CreateDefaultConfig().
type CreateDefaultConfigFunc func() Config

// CreateDefaultConfig implements Factory.CreateDefaultConfig().
func (f CreateDefaultConfigFunc) CreateDefaultConfig() Config {
	return f()
}
