// Copyright 2026 The go-yaml Project Contributors
// SPDX-License-Identifier: Apache-2.0

package yaml

import (
	"errors"

	"github.com/pb33f/go-yaml/internal/libyaml"
	pluginreg "github.com/pb33f/go-yaml/internal/plugin"
	"github.com/pb33f/go-yaml/plugin/limit"
)

// LimitPlugin configures safety limits for YAML parsing.
//
// When registered, CheckDepth is called on each nesting depth increase,
// and CheckAlias is called on each alias expansion to detect excessive
// aliasing.
//
// Example usage:
//
//	import "github.com/pb33f/go-yaml/plugin/limit"
//	loader := yaml.NewLoader(data, yaml.WithPlugin(limit.New(limit.AliasNone())))
type LimitPlugin interface {
	// CheckDepth is called when the parser increases nesting depth.
	// depth is the current nesting level; ctx.Kind is "flow" or "block".
	// Return an error to abort parsing.
	CheckDepth(depth int, ctx *DepthContext) error

	// CheckAlias is called during alias expansion.
	// Return an error to abort construction.
	CheckAlias(aliasCount, constructCount int) error
}

// ParserPlugin supplies a complete event stream in place of native parsing.
type ParserPlugin = libyaml.ParserPlugin

// JSONCommentsPlugin sanitizes JSON-style comments before parsing.
type JSONCommentsPlugin = libyaml.JSONCommentsPlugin

// IndentMode supplies loading and dumping defaults.
type IndentMode = libyaml.IndentMode

const (
	IndentModeAuto = libyaml.IndentModeAuto
	IndentModeTabs = libyaml.IndentModeTabs
)

// IndentStyle identifies the characters used for structural indentation.
type IndentStyle = libyaml.IndentStyle

const (
	IndentStyleAuto   = libyaml.IndentStyleAuto
	IndentStyleSpaces = libyaml.IndentStyleSpaces
	IndentStyleTabs   = libyaml.IndentStyleTabs
)

// IndentScope controls how long an auto-detected style remains active.
type IndentScope = libyaml.IndentScope

const (
	IndentScopeDocument = libyaml.IndentScopeDocument
	IndentScopeStream   = libyaml.IndentScopeStream
)

// IndentConfig configures tab-indentation loading and dumping.
type IndentConfig = libyaml.IndentConfig

// TabIndentPlugin enables tab-aware structural indentation.
type TabIndentPlugin = libyaml.TabIndentPlugin

// PluginEvent carries source-independent YAML syntax information.
type PluginEvent = libyaml.PluginEvent

// PluginFactory constructs a plugin from its plugin-specific configuration.
type PluginFactory = pluginreg.Factory

// PluginRegistration describes one named implementation of a plugin API.
// Version is empty for an unversioned implementation.
// Default selects the implementation used by boolean configuration.
type PluginRegistration = pluginreg.Registration

type nativeParserPlugin struct{}

var pluginRegistry = pluginreg.NewRegistry(
	PluginRegistration{
		API: "limit", Name: "limit", Default: true,
		Factory: func(cfg map[string]any) (any, error) {
			return limit.NewFromYAML(cfg)
		},
	},
	PluginRegistration{
		API: "parser", Name: "go-yaml", Default: true,
		Factory: func(cfg map[string]any) (any, error) {
			if len(cfg) != 0 {
				return nil, errors.New(
					"yaml: go-yaml parser configuration must be empty")
			}
			return nativeParserPlugin{}, nil
		},
	},
)

// RegisterPlugin registers a named implementation for [OptsYAML] and
// [WithNamedPlugin]. Register during application startup, before parsing
// configuration. Duplicate API and implementation name pairs are errors.
func RegisterPlugin(registration PluginRegistration) error {
	return pluginRegistry.Register(registration)
}

func namedPlugin(api string, cfg map[string]any) (any, error) {
	return pluginRegistry.Create(api, cfg)
}

// WithNamedPlugin selects the implementation whose name matches the API.
func WithNamedPlugin(api string) Option {
	return func(o *libyaml.Options) error {
		p, err := namedPlugin(api, map[string]any{})
		if err != nil {
			return err
		}
		return WithPlugin(p)(o)
	}
}
