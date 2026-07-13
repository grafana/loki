// Copyright 2026 The go-yaml Project Contributors
// SPDX-License-Identifier: Apache-2.0

// Package limit provides a configurable safety limit plugin for go-yaml.
//
// The limit plugin controls the maximum nesting depth and alias expansion
// ratio during YAML parsing.
// By default, go-yaml enforces conservative limits to prevent DoS attacks.
// This plugin lets you relax or tighten those limits for your use case.
//
// # Usage
//
//	import (
//	    "go.yaml.in/yaml/v4"
//	    "go.yaml.in/yaml/v4/plugin/limit"
//	)
//
//	// Default limits
//	loader := yaml.NewLoader(data, yaml.WithPlugin(limit.New()))
//
//	// Disable alias checking (e.g. for 11,000 programmatic aliases)
//	loader := yaml.NewLoader(data, yaml.WithPlugin(limit.New(limit.AliasNone())))
//
//	// Custom depth limit
//	loader := yaml.NewLoader(data, yaml.WithPlugin(limit.New(limit.DepthValue(50))))
//
// # Third-Party Plugins
//
// You can implement [yaml.LimitPlugin] directly instead of using this package:
//
//	type StrictLimit struct{}
//	func (s *StrictLimit) CheckDepth(depth int, ctx *yaml.DepthContext) error { ... }
//	func (s *StrictLimit) CheckAlias(aliasCount, constructCount int) error { ... }
//	yaml.NewLoader(data, yaml.WithPlugin(&StrictLimit{}))
package limit

import (
	"fmt"

	"go.yaml.in/yaml/v4/internal/libyaml"
)

// DepthContext is an alias for the type used in depth check callbacks.
// See [yaml.DepthContext] for field documentation.
type DepthContext = libyaml.DepthContext

// Plugin implements configurable safety limits for YAML parsing.
type Plugin struct {
	depthLimit    *int
	depthDisabled bool
	depthFn       func(int, *DepthContext) error
	aliasLimit    *int
	aliasDisabled bool
	aliasFn       func(int, int) error
}

// Option configures a [Plugin].
type Option func(*Plugin)

// New creates a limit plugin with the given options.
// With no options, it uses the same defaults as the bare go-yaml library.
func New(opts ...Option) *Plugin {
	p := &Plugin{}
	for _, o := range opts {
		o(p)
	}
	return p
}

// DepthValue sets a maximum nesting depth (both flow and block).
func DepthValue(n int) Option {
	return func(p *Plugin) {
		p.depthLimit = &n
		p.depthDisabled = false
		p.depthFn = nil
	}
}

// DepthNone disables depth checking entirely.
func DepthNone() Option {
	return func(p *Plugin) {
		p.depthDisabled = true
		p.depthLimit = nil
		p.depthFn = nil
	}
}

// DepthFunc sets a custom depth check function.
func DepthFunc(fn func(depth int, ctx *DepthContext) error) Option {
	return func(p *Plugin) {
		p.depthFn = fn
		p.depthLimit = nil
		p.depthDisabled = false
	}
}

// AliasValue sets a simple alias expansion count threshold.
func AliasValue(n int) Option {
	return func(p *Plugin) {
		p.aliasLimit = &n
		p.aliasDisabled = false
		p.aliasFn = nil
	}
}

// AliasNone disables alias checking entirely.
func AliasNone() Option {
	return func(p *Plugin) {
		p.aliasDisabled = true
		p.aliasLimit = nil
		p.aliasFn = nil
	}
}

// AliasFunc sets a custom alias check function.
func AliasFunc(fn func(aliasCount, constructCount int) error) Option {
	return func(p *Plugin) {
		p.aliasFn = fn
		p.aliasLimit = nil
		p.aliasDisabled = false
	}
}

// CheckDepth implements [yaml.LimitPlugin].
func (p *Plugin) CheckDepth(depth int, ctx *DepthContext) error {
	if p.depthFn != nil {
		return p.depthFn(depth, ctx)
	}
	if p.depthDisabled {
		return nil
	}
	if p.depthLimit != nil {
		if depth > *p.depthLimit {
			return fmt.Errorf("exceeded max depth of %d", *p.depthLimit)
		}
		return nil
	}
	return libyaml.DefaultDepthCheck(depth, ctx)
}

// CheckAlias implements [yaml.LimitPlugin].
func (p *Plugin) CheckAlias(aliasCount, constructCount int) error {
	if p.aliasFn != nil {
		return p.aliasFn(aliasCount, constructCount)
	}
	if p.aliasDisabled {
		return nil
	}
	if p.aliasLimit != nil {
		if aliasCount > *p.aliasLimit {
			return fmt.Errorf("exceeded max alias count of %d", *p.aliasLimit)
		}
		return nil
	}
	return libyaml.DefaultAliasCheck(aliasCount, constructCount)
}

// NewFromYAML creates a limit plugin from a YAML config map.
// Keys: "depth" (int or null), "alias" (int or null).
// Null values disable the corresponding check.
// Omitted keys use defaults.
func NewFromYAML(cfg map[string]any) (*Plugin, error) {
	var opts []Option
	for key, val := range cfg {
		switch key {
		case "depth":
			if val == nil {
				opts = append(opts, DepthNone())
			} else {
				n, ok := val.(int)
				if !ok {
					return nil, fmt.Errorf("limit: depth must be int or null, got %T", val)
				}
				opts = append(opts, DepthValue(n))
			}
		case "alias":
			if val == nil {
				opts = append(opts, AliasNone())
			} else {
				n, ok := val.(int)
				if !ok {
					return nil, fmt.Errorf("limit: alias must be int or null, got %T", val)
				}
				opts = append(opts, AliasValue(n))
			}
		default:
			return nil, fmt.Errorf("limit: unknown key %q", key)
		}
	}
	return New(opts...), nil
}
